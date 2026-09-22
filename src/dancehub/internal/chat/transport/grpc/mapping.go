package grpc

import (
	"context"
	"encoding/json"
	"time"

	chatv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/chat/v1"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/chat/application"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/chat/domain"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *Server) eventFrame(ctx context.Context, actor application.ActorContext, raw []byte) *chatv1.ServerFrame {
	var event map[string]any
	if json.Unmarshal(raw, &event) != nil {
		return nil
	}
	conversationID, _ := event["conversation_id"].(string)
	if conversationID == "" {
		return nil
	}
	allowed, err := s.service.AuthorizeConversation(ctx, actor, conversationID)
	if err != nil || !allowed {
		return nil
	}
	eventType, _ := event["event_type"].(string)
	frame := &chatv1.ServerFrame{Version: 1, EventId: stringValue(event["event_id"]), OccurredAt: timestamppb.Now()}
	switch eventType {
	case "message_created":
		created, _ := time.Parse(time.RFC3339Nano, stringValue(event["created_at"]))
		frame.Payload = &chatv1.ServerFrame_MessageCreated{MessageCreated: &chatv1.Message{Id: stringValue(event["message_id"]), ConversationId: conversationID, Sequence: int64Value(event["sequence"]), SenderId: stringValue(event["sender_id"]), SenderDisplayName: stringValue(event["sender_display_name"]), Kind: protoMessageKind(domain.MessageKind(stringValue(event["kind"]))), Body: stringValue(event["body"]), CreatedAt: timestamppb.New(created)}}
	case "message_updated":
		frame.Payload = &chatv1.ServerFrame_MessageUpdated{MessageUpdated: &chatv1.Message{Id: stringValue(event["message_id"]), ConversationId: conversationID}}
	case "message_withdrawn":
		frame.Payload = &chatv1.ServerFrame_MessageWithdrawnId{MessageWithdrawnId: stringValue(event["message_id"])}
	case "read_updated":
		frame.Payload = &chatv1.ServerFrame_ReadUpdated{ReadUpdated: &chatv1.ReadUpdated{ConversationId: conversationID, UserId: stringValue(event["user_id"]), Sequence: int64Value(event["sequence"])}}
	case "typing":
		frame.Payload = &chatv1.ServerFrame_Typing{Typing: &chatv1.TypingUpdated{ConversationId: conversationID, UserId: stringValue(event["user_id"]), Typing: boolValue(event["typing"])}}
	case "presence":
		frame.Payload = &chatv1.ServerFrame_Presence{Presence: &chatv1.PresenceUpdated{UserId: stringValue(event["user_id"]), Online: boolValue(event["online"])}}
	case "membership_changed":
		frame.Payload = &chatv1.ServerFrame_MembershipChanged{MembershipChanged: &chatv1.MembershipChanged{Participant: &chatv1.Participant{ConversationId: conversationID, UserId: stringValue(event["user_id"]), State: memberState(stringValue(event["state"]))}}}
	case "attachment_scanned":
		frame.Payload = &chatv1.ServerFrame_AttachmentUpdated{AttachmentUpdated: &chatv1.Attachment{Id: stringValue(event["attachment_id"]), ConversationId: conversationID, Status: attachmentStatus(stringValue(event["status"]))}}
	default:
		return nil
	}
	return frame
}
