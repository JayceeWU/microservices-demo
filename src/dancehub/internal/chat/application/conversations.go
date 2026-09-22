package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/JayceeWU/microservices-demo/dancehub/internal/appcore"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/chat/domain"
)

func (s *Service) CreateDirect(ctx context.Context, actor ActorContext, recipient string) (*ConversationView, error) {
	if err := requireActor(actor); err != nil {
		return nil, err
	}
	self, err := s.accounts.GetPrincipal(ctx, actor, actor.UserID)
	if err != nil {
		return nil, err
	}
	other, err := s.accounts.GetPrincipal(ctx, actor, recipient)
	if err != nil {
		return nil, err
	}
	if !domain.CanStartDirect(self, other) {
		return nil, appcore.Denied("a direct chat must pair one student and one teacher")
	}
	student, teacher := self.ID, other.ID
	if self.Teacher {
		student, teacher = other.ID, self.ID
	}
	conversation := &domain.Conversation{ID: s.ids.New(), Kind: domain.DirectTeacher, StudentID: student, TeacherID: teacher, MemberCount: 2, Active: true, CreatedAt: s.clock.Now()}
	var result *ConversationView
	var missing bool
	err = s.uow.Do(ctx, actor, func(ctx context.Context, repos Repositories) error {
		blocked, checkErr := repos.Blocks().ExistsEitherDirection(ctx, student, teacher)
		if checkErr != nil {
			return checkErr
		}
		if blocked {
			return appcore.Denied(domain.ErrBlocked.Error())
		}
		result, checkErr = repos.Conversations().FindDirect(ctx, student, teacher)
		if checkErr == nil {
			return nil
		}
		if !errors.Is(checkErr, appcore.ErrNotFound) {
			return checkErr
		}
		missing = true
		return nil
	})
	if err != nil || !missing {
		return result, err
	}
	allowed, retryAfter, err := s.bus.Allow(ctx, actor.UserID, "new-direct", 20, time.Hour)
	if err != nil {
		return nil, appcore.Unavailable("chat rate limiter unavailable", err)
	}
	if !allowed {
		return nil, fmt.Errorf("%w: new direct conversation limit reached for %s", appcore.ErrUnavailable, retryAfter)
	}
	err = s.uow.Do(ctx, actor, func(ctx context.Context, repos Repositories) error {
		var saveErr error
		result, saveErr = repos.Conversations().AddDirect(ctx, conversation, student, teacher, actor.UserID)
		return saveErr
	})
	return result, err
}

func (s *Service) CreateStudioSupport(ctx context.Context, actor ActorContext, studioID, requestedStudent string) (*ConversationView, error) {
	if err := requireActor(actor); err != nil {
		return nil, err
	}
	if err := s.catalog.RequireStudio(ctx, actor, studioID); err != nil {
		return nil, err
	}
	self, err := s.accounts.GetPrincipal(ctx, actor, actor.UserID)
	if err != nil {
		return nil, err
	}
	studentID := actor.UserID
	if requestedStudent != "" {
		studentID = requestedStudent
	}
	if studentID != actor.UserID && !self.AdminStudios[studioID] {
		return nil, appcore.Denied("studio membership required")
	}
	student, err := s.accounts.GetPrincipal(ctx, actor, studentID)
	if err != nil || !student.Student {
		return nil, appcore.Invalid("an active student is required")
	}
	conversation := &domain.Conversation{ID: s.ids.New(), Kind: domain.StudioSupport, StudioID: studioID, StudentID: studentID, MemberCount: 1, Active: true, CreatedAt: s.clock.Now()}
	var result *ConversationView
	err = s.uow.Do(ctx, actor, func(ctx context.Context, repos Repositories) error {
		result, err = repos.Conversations().FindStudioSupport(ctx, studioID, studentID)
		if err == nil {
			return nil
		}
		if !errors.Is(err, appcore.ErrNotFound) {
			return err
		}
		result, err = repos.Conversations().AddStudioSupport(ctx, conversation, actor.UserID)
		return err
	})
	return result, err
}

func (s *Service) CreateGroup(ctx context.Context, actor ActorContext, kind domain.ConversationKind, studioID, title, description, avatar string) (*ConversationView, error) {
	if err := requireActor(actor); err != nil {
		return nil, err
	}
	if err := s.catalog.RequireStudio(ctx, actor, studioID); err != nil {
		return nil, err
	}
	principal, err := s.accounts.GetPrincipal(ctx, actor, actor.UserID)
	if err != nil {
		return nil, err
	}
	teacher := kind == domain.TeacherGroup
	if teacher && !principal.TeacherStudios[studioID] {
		return nil, appcore.Denied("active teacher membership required")
	}
	if !teacher && !principal.AdminStudios[studioID] {
		return nil, appcore.Denied("studio admin membership required")
	}
	group, err := domain.NewGroup(kind, studioID, actor.UserID, title, description, avatar, teacher)
	if err != nil {
		return nil, translateDomain(err)
	}
	group.ID, group.CreatedAt = s.ids.New(), s.clock.Now()
	var result *ConversationView
	err = s.uow.Do(ctx, actor, func(ctx context.Context, repos Repositories) error {
		var saveErr error
		result, saveErr = repos.Conversations().AddGroup(ctx, group, actor.UserID)
		return saveErr
	})
	return result, err
}

func (s *Service) DeleteGroup(ctx context.Context, actor ActorContext, conversationID, reason string) error {
	if err := requireActor(actor); err != nil {
		return err
	}
	if strings.TrimSpace(reason) == "" {
		return appcore.Invalid("audit reason is required")
	}
	return s.uow.Do(ctx, actor, func(ctx context.Context, repos Repositories) error {
		view, err := repos.Conversations().Get(ctx, conversationID, true)
		if err != nil {
			return err
		}
		group := view.Conversation
		if group.Kind != domain.StudioGroup && group.Kind != domain.TeacherGroup {
			return appcore.Invalid("only groups can be deleted")
		}
		isManager := group.OwnerID == actor.UserID ||
			(actor.HasTenantRole("studio_admin") && actor.StudioID == group.StudioID)
		if !isManager {
			return appcore.Denied("group manager required")
		}
		if err = repos.Conversations().DeleteGroup(ctx, group); err != nil {
			return err
		}
		return nil
	})
}

func (s *Service) GroupMembership(ctx context.Context, actor ActorContext, conversationID, targetID, action, reason string) (Participant, error) {
	if err := requireActor(actor); err != nil {
		return Participant{}, err
	}
	if targetID == "" {
		targetID = actor.UserID
	}
	var result Participant
	err := s.uow.Do(ctx, actor, func(ctx context.Context, repos Repositories) error {
		view, err := repos.Conversations().Get(ctx, conversationID, true)
		if err != nil {
			return err
		}
		group := view.Conversation
		isManager := group.OwnerID == actor.UserID || (actor.HasTenantRole("studio_admin") && actor.StudioID == group.StudioID)
		switch action {
		case "join":
			if targetID != actor.UserID && !isManager {
				return appcore.Denied("group manager required")
			}
			existing, participantErr := repos.Conversations().Participant(ctx, group.ID, targetID)
			if participantErr == nil && existing.State == "ACTIVE" {
				result = existing
				return nil
			}
			if participantErr == nil && existing.State == "BANNED" {
				return appcore.Denied("member is banned")
			}
			returnError := group.Join(false)
			if returnError != nil {
				return translateDomain(returnError)
			}
			result, err = repos.Conversations().Join(ctx, group, targetID, isManager)
		case "leave":
			if targetID != actor.UserID && !isManager {
				return appcore.Denied("group manager required")
			}
			if group.OwnerID == targetID {
				return appcore.Conflict("group owner cannot leave")
			}
			if returnError := group.Leave(); returnError != nil {
				return translateDomain(returnError)
			}
			result, err = repos.Conversations().Leave(ctx, group, targetID, isManager)
		case "ban", "unban":
			if !isManager || strings.TrimSpace(reason) == "" {
				return appcore.Denied("group manager and reason required")
			}
			result, err = repos.Conversations().SetMemberState(ctx, group, targetID, action, actor.UserID, reason)
		default:
			return appcore.Invalid("unknown membership action")
		}
		if err == nil {
			err = repos.Events().AppendRealtime(ctx, group.ID, "membership_changed", map[string]any{"conversation_id": group.ID, "user_id": targetID, "state": result.State})
		}
		return err
	})
	return result, err
}
