package grpc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"time"

	chatv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/chat/v1"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/chat/application"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *Server) Connect(stream chatv1.ChatService_ConnectServer) error {
	ctx := stream.Context()
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	if first.Version != 1 || first.Ticket == "" {
		return status.Error(codes.Unauthenticated, "a version 1 WebSocket ticket is required")
	}
	connectedActor, err := s.service.ConsumeTicket(ctx, first.Ticket)
	if err != nil {
		return rpcError(err)
	}
	_ = s.service.Bus().SetPresence(ctx, connectedActor.UserID, 60*time.Second)
	events, closeSubscription, err := s.service.Bus().Subscribe(ctx)
	if err != nil {
		return status.Error(codes.Unavailable, "realtime service unavailable")
	}
	defer closeSubscription()
	_ = s.service.PublishPresence(ctx, connectedActor, true)
	var sendMu sync.Mutex
	send := func(frame *chatv1.ServerFrame) error { sendMu.Lock(); defer sendMu.Unlock(); return stream.Send(frame) }
	if err = send(frameReady()); err != nil {
		return err
	}
	receiveErrors := make(chan error, 1)
	go func() {
		for {
			frame, receiveErr := stream.Recv()
			if receiveErr != nil {
				receiveErrors <- receiveErr
				return
			}
			if frame.Version != 1 {
				_ = send(errorFrame(frame.CommandId, "UNSUPPORTED_VERSION", "only version 1 is supported", 0))
				continue
			}
			response := s.handleFrame(ctx, connectedActor, frame)
			if response != nil && send(response) != nil {
				receiveErrors <- io.EOF
				return
			}
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return nil
		case receiveErr := <-receiveErrors:
			if errors.Is(receiveErr, io.EOF) {
				return nil
			}
			return receiveErr
		case payload, open := <-events:
			if !open {
				return status.Error(codes.Unavailable, "realtime subscription closed")
			}
			frame := s.eventFrame(ctx, connectedActor, payload)
			if frame != nil {
				if err = send(frame); err != nil {
					return err
				}
			}
		}
	}
}

func (s *Server) handleFrame(ctx context.Context, actor application.ActorContext, frame *chatv1.ClientFrame) *chatv1.ServerFrame {
	now := timestamppb.Now()
	if command := frame.GetSendMessage(); command != nil {
		message, _, err := s.service.SendMessage(ctx, actor, command.ConversationId, command.ClientMessageId, domainMessageKind(command.Kind), command.Body, command.ReplyToMessageId, command.AttachmentIds)
		if err != nil {
			slog.Error("chat WebSocket command failed", "operation", "send_message", "conversation_id", command.ConversationId, "error", err)
			return errorFrame(frame.CommandId, codeFor(err), err.Error(), retryAfter(err))
		}
		return &chatv1.ServerFrame{Version: 1, OccurredAt: now, Payload: &chatv1.ServerFrame_Ack{Ack: &chatv1.Ack{CommandId: frame.CommandId, Message: mapMessage(*message)}}}
	}
	if command := frame.GetEditMessage(); command != nil {
		message, err := s.service.EditMessage(ctx, actor, command.MessageId, command.Body)
		if err != nil {
			return errorFrame(frame.CommandId, codeFor(err), err.Error(), 0)
		}
		return &chatv1.ServerFrame{Version: 1, OccurredAt: now, Payload: &chatv1.ServerFrame_Ack{Ack: &chatv1.Ack{CommandId: frame.CommandId, Message: mapMessage(*message)}}}
	}
	if command := frame.GetWithdrawMessage(); command != nil {
		message, err := s.service.WithdrawMessage(ctx, actor, command.MessageId)
		if err != nil {
			return errorFrame(frame.CommandId, codeFor(err), err.Error(), 0)
		}
		return &chatv1.ServerFrame{Version: 1, OccurredAt: now, Payload: &chatv1.ServerFrame_Ack{Ack: &chatv1.Ack{CommandId: frame.CommandId, Message: mapMessage(*message)}}}
	}
	if command := frame.GetMarkRead(); command != nil {
		value, err := s.service.MarkRead(ctx, actor, command.ConversationId, command.Sequence)
		if err != nil {
			return errorFrame(frame.CommandId, codeFor(err), err.Error(), 0)
		}
		return &chatv1.ServerFrame{Version: 1, OccurredAt: now, Payload: &chatv1.ServerFrame_ReadUpdated{ReadUpdated: &chatv1.ReadUpdated{ConversationId: value.ConversationID, UserId: actor.UserID, Sequence: value.LastRead}}}
	}
	if command := frame.GetTyping(); command != nil {
		if allowed, err := s.service.AuthorizeConversation(ctx, actor, command.ConversationId); err != nil || !allowed {
			return errorFrame(frame.CommandId, "PERMISSION_DENIED", "conversation membership required", 0)
		}
		allowed, _, err := s.service.Bus().Allow(ctx, actor.UserID+":"+command.ConversationId, "typing", 1, time.Second)
		if err != nil || !allowed {
			return errorFrame(frame.CommandId, "RATE_LIMITED", "typing rate limited", 1000)
		}
		payload, _ := json.Marshal(map[string]any{"event_type": "typing", "conversation_id": command.ConversationId, "user_id": actor.UserID, "typing": command.Typing})
		_ = s.service.Bus().Publish(ctx, payload)
		return &chatv1.ServerFrame{Version: 1, OccurredAt: now, Payload: &chatv1.ServerFrame_Ack{Ack: &chatv1.Ack{CommandId: frame.CommandId}}}
	}
	if frame.GetHeartbeat() {
		_ = s.service.Bus().SetPresence(ctx, actor.UserID, 60*time.Second)
		return &chatv1.ServerFrame{Version: 1, OccurredAt: now, Payload: &chatv1.ServerFrame_Ack{Ack: &chatv1.Ack{CommandId: frame.CommandId}}}
	}
	return errorFrame(frame.CommandId, "INVALID_ARGUMENT", "frame command is required", 0)
}
