package application

import (
	"context"
	"fmt"
	"time"

	"github.com/JayceeWU/microservices-demo/dancehub/internal/appcore"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/chat/domain"
)

func (s *Service) ListMessages(ctx context.Context, actor ActorContext, conversationID string, before int64, page Page) ([]MessageView, string, error) {
	if err := requireActor(actor); err != nil {
		return nil, "", err
	}
	var result []MessageView
	var next string
	err := s.uow.Do(ctx, actor, func(ctx context.Context, repos Repositories) error {
		allowed, _, err := repos.Conversations().CanAccess(ctx, conversationID, actor.UserID)
		if err != nil {
			return err
		}
		if !allowed {
			return appcore.Denied("conversation membership required")
		}
		result, next, err = repos.Messages().List(ctx, conversationID, before, pageOrDefault(page))
		return err
	})
	return result, next, err
}

func (s *Service) SendMessage(ctx context.Context, actor ActorContext, conversationID, clientID string, kind domain.MessageKind, body, reply string, attachments []string) (*MessageView, bool, error) {
	if err := requireActor(actor); err != nil {
		return nil, false, err
	}
	allowed, retry, err := s.bus.Allow(ctx, actor.UserID, "message", 60, time.Minute)
	if err != nil {
		return nil, false, appcore.Unavailable("chat rate limiter unavailable", err)
	}
	if !allowed {
		return nil, false, fmt.Errorf("%w: rate limited for %s", appcore.ErrUnavailable, retry)
	}
	message, err := domain.NewMessage(conversationID, "", actor.UserID, clientID, kind, body, reply, s.clock.Now())
	if err != nil {
		return nil, false, translateDomain(err)
	}
	message.ID = s.ids.New()
	sender, err := s.accounts.GetPrincipal(ctx, actor, actor.UserID)
	if err != nil {
		return nil, false, err
	}
	message.SenderDisplayName = sender.DisplayName
	var result *MessageView
	var created bool
	err = s.uow.Do(ctx, actor, func(ctx context.Context, repos Repositories) error {
		conversation, loadErr := repos.Conversations().Get(ctx, conversationID, true)
		if loadErr != nil {
			return loadErr
		}
		allowed, moderator, accessErr := repos.Conversations().CanAccess(ctx, conversationID, actor.UserID)
		_ = moderator
		if accessErr != nil {
			return accessErr
		}
		if !allowed {
			return appcore.Denied("conversation membership required")
		}
		if conversation.Conversation.Kind == domain.DirectTeacher {
			blocked, blockErr := repos.Blocks().ExistsEitherDirection(ctx, conversation.Conversation.StudentID, conversation.Conversation.TeacherID)
			if blockErr != nil {
				return blockErr
			}
			if blocked {
				return appcore.Denied(domain.ErrBlocked.Error())
			}
		}
		message.StudioID = conversation.Conversation.StudioID
		result, created, loadErr = repos.Messages().Add(ctx, message, attachments)
		if loadErr != nil || !created {
			return loadErr
		}
		realtimePayload := map[string]any{"message_id": result.Message.ID, "conversation_id": conversationID, "sequence": result.Message.Sequence, "kind": result.Message.Kind, "sender_id": result.Message.SenderID, "sender_display_name": result.Message.SenderDisplayName, "body": result.Message.Body, "created_at": result.Message.CreatedAt.Format(time.RFC3339Nano)}
		if loadErr = repos.Events().AppendRealtime(ctx, conversationID, "message_created", realtimePayload); loadErr != nil {
			return loadErr
		}
		return nil
	})
	return result, created, err
}

func (s *Service) EditMessage(ctx context.Context, actor ActorContext, messageID, body string) (*MessageView, error) {
	var result *MessageView
	err := s.uow.Do(ctx, actor, func(ctx context.Context, repos Repositories) error {
		view, err := repos.Messages().Get(ctx, messageID, true)
		if err != nil {
			return err
		}
		if err = view.Message.Edit(actor.UserID, body, s.clock.Now()); err != nil {
			return translateDomain(err)
		}
		result, err = repos.Messages().Save(ctx, view.Message)
		if err == nil {
			err = repos.Events().AppendRealtime(ctx, view.Message.ConversationID, "message_updated", map[string]any{"message_id": messageID})
		}
		return err
	})
	return result, err
}

func (s *Service) WithdrawMessage(ctx context.Context, actor ActorContext, messageID string) (*MessageView, error) {
	var result *MessageView
	err := s.uow.Do(ctx, actor, func(ctx context.Context, repos Repositories) error {
		view, err := repos.Messages().Get(ctx, messageID, true)
		if err != nil {
			return err
		}
		_, moderator, err := repos.Conversations().CanAccess(ctx, view.Message.ConversationID, actor.UserID)
		if err != nil {
			return err
		}
		beforeBody := view.Message.Body
		if err = view.Message.Withdraw(actor.UserID, moderator, s.clock.Now()); err != nil {
			return translateDomain(err)
		}
		result, err = repos.Messages().Save(ctx, view.Message)
		if err == nil && moderator && actor.UserID != view.Message.SenderID {
			err = repos.Messages().AuditModeration(ctx, view.Message, actor.UserID, beforeBody, "Moderator removed a group message")
		}
		if err == nil {
			err = repos.Events().AppendRealtime(ctx, view.Message.ConversationID, "message_withdrawn", map[string]any{"message_id": messageID})
		}
		return err
	})
	return result, err
}

func (s *Service) MarkRead(ctx context.Context, actor ActorContext, conversationID string, sequence int64) (Participant, error) {
	var result Participant
	err := s.uow.Do(ctx, actor, func(ctx context.Context, repos Repositories) error {
		var advanced bool
		var err error
		result, advanced, err = repos.Messages().MarkRead(ctx, conversationID, actor.UserID, sequence)
		// Only a receipt that moved is broadcast; echoing unchanged receipts made every
		// client refresh and re-send its receipt, which looped without user activity.
		if err == nil && advanced {
			err = repos.Events().AppendRealtime(ctx, conversationID, "read_updated", map[string]any{"user_id": actor.UserID, "sequence": result.LastRead})
		}
		return err
	})
	return result, err
}

func (s *Service) SetBlock(ctx context.Context, actor ActorContext, blockedID string, active bool) error {
	if err := requireActor(actor); err != nil {
		return err
	}
	if blockedID == "" || blockedID == actor.UserID {
		return appcore.Invalid("another user is required")
	}
	return s.uow.Do(ctx, actor, func(ctx context.Context, repos Repositories) error {
		return repos.Blocks().Set(ctx, actor.UserID, blockedID, active)
	})
}
