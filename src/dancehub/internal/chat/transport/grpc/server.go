package grpc

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	chatv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/chat/v1"
	commonv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/common/v1"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/appcore"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/chat/application"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/chat/domain"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Server struct {
	chatv1.UnimplementedChatServiceServer
	service *application.Service
}

func New(service *application.Service) *Server { return &Server{service: service} }

func actor(ctx context.Context) application.ActorContext {
	return platform.Actor(ctx)
}
func page(value *commonv1.PageRequest) application.Page {
	if value == nil {
		return application.Page{}
	}
	return application.Page{Size: value.PageSize, Token: value.PageToken}
}
func pageResponse(next string) *commonv1.PageResponse {
	return &commonv1.PageResponse{NextPageToken: next}
}

func (s *Server) ListConversations(ctx context.Context, request *chatv1.ListConversationsRequest) (*chatv1.ListConversationsResponse, error) {
	items, next, err := s.service.ListConversations(ctx, actor(ctx), page(request.Page))
	if err != nil {
		return nil, rpcError(err)
	}
	return &chatv1.ListConversationsResponse{Conversations: mapConversations(items), Page: pageResponse(next)}, nil
}
func (s *Server) CreateDirectConversation(ctx context.Context, request *chatv1.CreateDirectConversationRequest) (*chatv1.Conversation, error) {
	value, err := s.service.CreateDirect(ctx, actor(ctx), request.RecipientUserId)
	if err != nil {
		return nil, rpcError(err)
	}
	return mapConversation(*value), nil
}
func (s *Server) CreateStudioConversation(ctx context.Context, request *chatv1.CreateStudioConversationRequest) (*chatv1.Conversation, error) {
	value, err := s.service.CreateStudioSupport(ctx, actor(ctx), request.StudioId, request.StudentId)
	if err != nil {
		return nil, rpcError(err)
	}
	return mapConversation(*value), nil
}
func (s *Server) ListGroups(ctx context.Context, request *chatv1.ListGroupsRequest) (*chatv1.ListGroupsResponse, error) {
	items, next, err := s.service.ListGroups(ctx, actor(ctx), request.StudioId, request.TeacherId, page(request.Page))
	if err != nil {
		return nil, rpcError(err)
	}
	return &chatv1.ListGroupsResponse{Groups: mapConversations(items), Page: pageResponse(next)}, nil
}
func (s *Server) CreateGroup(ctx context.Context, request *chatv1.CreateGroupRequest) (*chatv1.Conversation, error) {
	value, err := s.service.CreateGroup(ctx, actor(ctx), domainKind(request.Kind), request.StudioId, request.Title, request.Description, request.AvatarUrl)
	if err != nil {
		return nil, rpcError(err)
	}
	return mapConversation(*value), nil
}

func (s *Server) DeleteGroup(ctx context.Context, request *chatv1.GroupCommandRequest) (*emptypb.Empty, error) {
	if err := s.service.DeleteGroup(ctx, actor(ctx), request.ConversationId, auditReason(request.Audit)); err != nil {
		return nil, rpcError(err)
	}
	return &emptypb.Empty{}, nil
}
func (s *Server) JoinGroup(ctx context.Context, request *chatv1.GroupCommandRequest) (*chatv1.Conversation, error) {
	if _, err := s.service.GroupMembership(ctx, actor(ctx), request.ConversationId, "", "join", ""); err != nil {
		return nil, rpcError(err)
	}
	return s.getConversation(ctx, request.ConversationId)
}
func (s *Server) LeaveGroup(ctx context.Context, request *chatv1.GroupCommandRequest) (*chatv1.Conversation, error) {
	if _, err := s.service.GroupMembership(ctx, actor(ctx), request.ConversationId, "", "leave", ""); err != nil {
		return nil, rpcError(err)
	}
	return s.getConversation(ctx, request.ConversationId)
}
func (s *Server) PutGroupMember(ctx context.Context, request *chatv1.PutGroupMemberRequest) (*chatv1.Participant, error) {
	value, err := s.service.GroupMembership(ctx, actor(ctx), request.ConversationId, request.UserId, "join", auditReason(request.Audit))
	if err != nil {
		return nil, rpcError(err)
	}
	return mapParticipant(value), nil
}
func (s *Server) BanGroupMember(ctx context.Context, request *chatv1.PutGroupMemberRequest) (*chatv1.Participant, error) {
	value, err := s.service.GroupMembership(ctx, actor(ctx), request.ConversationId, request.UserId, "ban", auditReason(request.Audit))
	if err != nil {
		return nil, rpcError(err)
	}
	return mapParticipant(value), nil
}
func (s *Server) UnbanGroupMember(ctx context.Context, request *chatv1.PutGroupMemberRequest) (*chatv1.Participant, error) {
	value, err := s.service.GroupMembership(ctx, actor(ctx), request.ConversationId, request.UserId, "unban", auditReason(request.Audit))
	if err != nil {
		return nil, rpcError(err)
	}
	return mapParticipant(value), nil
}
func (s *Server) ListMessages(ctx context.Context, request *chatv1.ListMessagesRequest) (*chatv1.ListMessagesResponse, error) {
	items, next, err := s.service.ListMessages(ctx, actor(ctx), request.ConversationId, request.BeforeSequence, page(request.Page))
	if err != nil {
		slog.Error("chat gRPC query failed", "operation", "list_messages", "conversation_id", request.ConversationId, "error", err)
		return nil, rpcError(err)
	}
	return &chatv1.ListMessagesResponse{Messages: mapMessages(items), Page: pageResponse(next)}, nil
}
func (s *Server) BlockUser(ctx context.Context, request *chatv1.BlockUserRequest) (*chatv1.BlockRelation, error) {
	value := actor(ctx)
	if err := s.service.SetBlock(ctx, value, request.BlockedUserId, true); err != nil {
		return nil, rpcError(err)
	}
	return &chatv1.BlockRelation{BlockerUserId: value.UserID, BlockedUserId: request.BlockedUserId, Active: true}, nil
}
func (s *Server) UnblockUser(ctx context.Context, request *chatv1.BlockUserRequest) (*chatv1.BlockRelation, error) {
	value := actor(ctx)
	if err := s.service.SetBlock(ctx, value, request.BlockedUserId, false); err != nil {
		return nil, rpcError(err)
	}
	return &chatv1.BlockRelation{BlockerUserId: value.UserID, BlockedUserId: request.BlockedUserId, Active: false}, nil
}
func (s *Server) PrepareAttachment(ctx context.Context, request *chatv1.PrepareAttachmentRequest) (*chatv1.AttachmentUpload, error) {
	value, url, expires, err := s.service.PrepareAttachment(ctx, actor(ctx), request.ConversationId, domainMessageKind(request.Kind), request.MimeType, request.Sha256, request.SizeBytes)
	if err != nil {
		return nil, rpcError(err)
	}
	return &chatv1.AttachmentUpload{Attachment: mapAttachment(value), UploadUrl: url, ExpiresAt: timestamppb.New(expires)}, nil
}
func (s *Server) CompleteAttachment(ctx context.Context, request *chatv1.CompleteAttachmentRequest) (*chatv1.Attachment, error) {
	value, err := s.service.CompleteAttachment(ctx, actor(ctx), request.AttachmentId)
	if err != nil {
		return nil, rpcError(err)
	}
	return mapAttachment(value), nil
}
func (s *Server) GetAttachmentDownload(ctx context.Context, request *chatv1.GetAttachmentDownloadRequest) (*chatv1.AttachmentDownload, error) {
	url, expires, err := s.service.AttachmentDownload(ctx, actor(ctx), request.AttachmentId)
	if err != nil {
		return nil, rpcError(err)
	}
	return &chatv1.AttachmentDownload{DownloadUrl: url, ExpiresAt: timestamppb.New(expires)}, nil
}
func (s *Server) ReportMessage(ctx context.Context, request *chatv1.ReportMessageRequest) (*chatv1.MessageReport, error) {
	value, err := s.service.ReportMessage(ctx, actor(ctx), request.MessageId, request.Reason)
	if err != nil {
		return nil, rpcError(err)
	}
	return &chatv1.MessageReport{Id: value.ID, MessageId: value.MessageID, ReporterId: value.ReporterID, Reason: value.Reason, CreatedAt: timestamppb.New(value.CreatedAt)}, nil
}
func (s *Server) CreateWebSocketTicket(ctx context.Context, _ *chatv1.CreateWebSocketTicketRequest) (*chatv1.WebSocketTicket, error) {
	ticket, expires, err := s.service.CreateTicket(ctx, actor(ctx))
	if err != nil {
		return nil, rpcError(err)
	}
	return &chatv1.WebSocketTicket{Ticket: ticket, ExpiresAt: timestamppb.New(expires)}, nil
}

func (s *Server) getConversation(ctx context.Context, id string) (*chatv1.Conversation, error) {
	items, _, err := s.service.ListConversations(ctx, actor(ctx), application.Page{Size: 100})
	if err != nil {
		return nil, rpcError(err)
	}
	for _, item := range items {
		if item.Conversation.ID == id {
			return mapConversation(item), nil
		}
	}
	return nil, status.Error(codes.NotFound, "conversation not found")
}

func mapConversations(values []application.ConversationView) []*chatv1.Conversation {
	result := make([]*chatv1.Conversation, 0, len(values))
	for _, value := range values {
		result = append(result, mapConversation(value))
	}
	return result
}
func mapConversation(value application.ConversationView) *chatv1.Conversation {
	c := value.Conversation
	return &chatv1.Conversation{Id: c.ID, Kind: protoKind(c.Kind), StudioId: c.StudioID, TeacherId: c.TeacherID, StudentId: c.StudentID, Title: c.Title, Description: c.Description, AvatarUrl: c.AvatarURL, MemberCount: c.MemberCount, LastSequence: c.LastSequence, LastReadSequence: value.LastRead, Joined: value.Joined, CreatedAt: timestamppb.New(c.CreatedAt)}
}
func mapMessages(values []application.MessageView) []*chatv1.Message {
	result := make([]*chatv1.Message, 0, len(values))
	for _, value := range values {
		result = append(result, mapMessage(value))
	}
	return result
}
func mapMessage(value application.MessageView) *chatv1.Message {
	m := value.Message
	attachments := make([]*chatv1.Attachment, 0, len(value.Attachments))
	for _, item := range value.Attachments {
		attachments = append(attachments, mapAttachment(item))
	}
	return &chatv1.Message{Id: m.ID, ConversationId: m.ConversationID, Sequence: m.Sequence, SenderId: m.SenderID, SenderDisplayName: m.SenderDisplayName, Kind: protoMessageKind(m.Kind), Body: m.Body, ReplyToMessageId: m.ReplyToID, Attachments: attachments, CreatedAt: timestamppb.New(m.CreatedAt), EditedAt: optionalTime(m.EditedAt), WithdrawnAt: optionalTime(m.WithdrawnAt)}
}
func mapAttachment(a application.Attachment) *chatv1.Attachment {
	return &chatv1.Attachment{Id: a.ID, ConversationId: a.ConversationID, UploaderId: a.UploaderID, Kind: protoMessageKind(a.Kind), MimeType: a.MimeType, SizeBytes: a.SizeBytes, Sha256: a.SHA256, Status: attachmentStatus(a.Status)}
}
func mapParticipant(p application.Participant) *chatv1.Participant {
	return &chatv1.Participant{ConversationId: p.ConversationID, UserId: p.UserID, Role: participantRole(p.Role), State: memberState(p.State), VisibleFromSequence: p.VisibleFrom, LastReadSequence: p.LastRead, JoinedAt: timestamppb.New(p.JoinedAt)}
}
func optionalTime(value *time.Time) *timestamppb.Timestamp {
	if value == nil {
		return nil
	}
	return timestamppb.New(*value)
}
func domainKind(value chatv1.ConversationKind) domain.ConversationKind {
	switch value {
	case chatv1.ConversationKind_CONVERSATION_KIND_DIRECT_TEACHER:
		return domain.DirectTeacher
	case chatv1.ConversationKind_CONVERSATION_KIND_STUDIO_SUPPORT:
		return domain.StudioSupport
	case chatv1.ConversationKind_CONVERSATION_KIND_STUDIO_GROUP:
		return domain.StudioGroup
	case chatv1.ConversationKind_CONVERSATION_KIND_TEACHER_GROUP:
		return domain.TeacherGroup
	}
	return ""
}
func protoKind(value domain.ConversationKind) chatv1.ConversationKind {
	switch value {
	case domain.DirectTeacher:
		return chatv1.ConversationKind_CONVERSATION_KIND_DIRECT_TEACHER
	case domain.StudioSupport:
		return chatv1.ConversationKind_CONVERSATION_KIND_STUDIO_SUPPORT
	case domain.StudioGroup:
		return chatv1.ConversationKind_CONVERSATION_KIND_STUDIO_GROUP
	case domain.TeacherGroup:
		return chatv1.ConversationKind_CONVERSATION_KIND_TEACHER_GROUP
	}
	return 0
}
func domainMessageKind(value chatv1.MessageKind) domain.MessageKind {
	switch value {
	case chatv1.MessageKind_MESSAGE_KIND_TEXT:
		return domain.TextMessage
	case chatv1.MessageKind_MESSAGE_KIND_IMAGE:
		return domain.ImageMessage
	case chatv1.MessageKind_MESSAGE_KIND_VIDEO:
		return domain.VideoMessage
	}
	return ""
}
func protoMessageKind(value domain.MessageKind) chatv1.MessageKind {
	switch value {
	case domain.TextMessage:
		return chatv1.MessageKind_MESSAGE_KIND_TEXT
	case domain.ImageMessage:
		return chatv1.MessageKind_MESSAGE_KIND_IMAGE
	case domain.VideoMessage:
		return chatv1.MessageKind_MESSAGE_KIND_VIDEO
	case domain.SystemMessage:
		return chatv1.MessageKind_MESSAGE_KIND_SYSTEM
	}
	return 0
}
func attachmentStatus(value string) chatv1.AttachmentStatus {
	values := map[string]chatv1.AttachmentStatus{"PENDING_UPLOAD": chatv1.AttachmentStatus_ATTACHMENT_STATUS_PENDING_UPLOAD, "PENDING_SCAN": chatv1.AttachmentStatus_ATTACHMENT_STATUS_PENDING_SCAN, "CLEAN": chatv1.AttachmentStatus_ATTACHMENT_STATUS_CLEAN, "REJECTED": chatv1.AttachmentStatus_ATTACHMENT_STATUS_REJECTED}
	return values[value]
}
func participantRole(value string) chatv1.ParticipantRole {
	values := map[string]chatv1.ParticipantRole{"MEMBER": chatv1.ParticipantRole_PARTICIPANT_ROLE_MEMBER, "OWNER": chatv1.ParticipantRole_PARTICIPANT_ROLE_OWNER, "MODERATOR": chatv1.ParticipantRole_PARTICIPANT_ROLE_MODERATOR}
	return values[value]
}
func memberState(value string) chatv1.MemberState {
	values := map[string]chatv1.MemberState{"ACTIVE": chatv1.MemberState_MEMBER_STATE_ACTIVE, "LEFT": chatv1.MemberState_MEMBER_STATE_LEFT, "BANNED": chatv1.MemberState_MEMBER_STATE_BANNED}
	return values[value]
}
func auditReason(value *commonv1.AuditContext) string {
	if value == nil {
		return ""
	}
	return value.Reason
}
func frameReady() *chatv1.ServerFrame {
	return &chatv1.ServerFrame{Version: 1, OccurredAt: timestamppb.Now(), Payload: &chatv1.ServerFrame_Ready{Ready: true}}
}
func errorFrame(commandID, code, message string, retry int64) *chatv1.ServerFrame {
	return &chatv1.ServerFrame{Version: 1, OccurredAt: timestamppb.Now(), Payload: &chatv1.ServerFrame_Error{Error: &chatv1.ChatError{CommandId: commandID, Code: code, Message: message, RetryAfterMs: retry}}}
}
func rpcError(err error) error {
	switch {
	case errors.Is(err, appcore.ErrUnauthenticated):
		return status.Error(codes.Unauthenticated, err.Error())
	case errors.Is(err, appcore.ErrPermissionDenied):
		return status.Error(codes.PermissionDenied, err.Error())
	case errors.Is(err, appcore.ErrInvalidArgument):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, appcore.ErrNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, appcore.ErrConflict):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, appcore.ErrUnavailable):
		return status.Error(codes.ResourceExhausted, err.Error())
	default:
		return status.Error(codes.Internal, "chat operation failed")
	}
}
func codeFor(err error) string { return strings.ToUpper(status.Code(rpcError(err)).String()) }
func retryAfter(err error) int64 {
	parts := strings.Split(err.Error(), "for ")
	if len(parts) < 2 {
		return 0
	}
	value, parseErr := time.ParseDuration(parts[len(parts)-1])
	if parseErr != nil {
		return 0
	}
	return value.Milliseconds()
}
func stringValue(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}
func int64Value(value any) int64 {
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case string:
		result, _ := strconv.ParseInt(typed, 10, 64)
		return result
	}
	return 0
}
func boolValue(value any) bool { result, _ := value.(bool); return result }
