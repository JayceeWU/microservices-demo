package main

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	chatv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/chat/v1"
	commonv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/common/v1"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/platform"
	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

var chatJSON = protojson.MarshalOptions{UseProtoNames: false, EmitUnpopulated: true}
var chatJSONInput = protojson.UnmarshalOptions{DiscardUnknown: false}

func (g *gateway) chat(w http.ResponseWriter, r *http.Request) {
	if r.Pattern == "GET /v1/chat/ws" {
		g.chatWebSocket(w, r)
		return
	}
	ctx, cancel := grpcRequestContext(r, 10*time.Second)
	defer cancel()
	page := &commonv1.PageRequest{PageSize: queryPageSize(r), PageToken: r.URL.Query().Get("page_token")}
	switch r.Pattern {
	case "GET /v1/chat/conversations":
		response, err := g.chatClient.ListConversations(ctx, &chatv1.ListConversationsRequest{Page: page})
		writeChatResponse(w, response, err, http.StatusOK)
	case "POST /v1/chat/direct-conversations":
		var input struct {
			RecipientUserID string `json:"recipientUserId"`
			IdempotencyKey  string `json:"idempotencyKey"`
		}
		if platform.DecodeJSON(r, &input) != nil {
			platform.Problem(w, 400, "invalid_chat_request", "Invalid request")
			return
		}
		response, err := g.chatClient.CreateDirectConversation(ctx, &chatv1.CreateDirectConversationRequest{RecipientUserId: input.RecipientUserID, Audit: audit(input.IdempotencyKey, "")})
		writeChatResponse(w, response, err, http.StatusCreated)
	case "POST /v1/chat/studio-conversations":
		var input struct {
			StudioID       string `json:"studioId"`
			StudentID      string `json:"studentId"`
			IdempotencyKey string `json:"idempotencyKey"`
		}
		if platform.DecodeJSON(r, &input) != nil {
			platform.Problem(w, 400, "invalid_chat_request", "Invalid request")
			return
		}
		response, err := g.chatClient.CreateStudioConversation(ctx, &chatv1.CreateStudioConversationRequest{StudioId: input.StudioID, StudentId: input.StudentID, Audit: audit(input.IdempotencyKey, "")})
		writeChatResponse(w, response, err, http.StatusCreated)
	case "GET /v1/chat/groups":
		response, err := g.chatClient.ListGroups(ctx, &chatv1.ListGroupsRequest{StudioId: r.URL.Query().Get("studio_id"), TeacherId: r.URL.Query().Get("teacher_id"), Page: page})
		writeChatResponse(w, response, err, http.StatusOK)
	case "POST /v1/chat/groups":
		var input struct {
			Kind           chatv1.ConversationKind `json:"kind"`
			StudioID       string                  `json:"studioId"`
			Title          string                  `json:"title"`
			Description    string                  `json:"description"`
			AvatarURL      string                  `json:"avatarUrl"`
			IdempotencyKey string                  `json:"idempotencyKey"`
		}
		if platform.DecodeJSON(r, &input) != nil {
			platform.Problem(w, 400, "invalid_chat_request", "Invalid request")
			return
		}
		response, err := g.chatClient.CreateGroup(ctx, &chatv1.CreateGroupRequest{Kind: input.Kind, StudioId: input.StudioID, Title: input.Title, Description: input.Description, AvatarUrl: input.AvatarURL, Audit: audit(input.IdempotencyKey, "")})
		writeChatResponse(w, response, err, http.StatusCreated)
	case "POST /v1/chat/groups/{groupAction}", "PUT /v1/chat/groups/{conversationId}/members/{userId}", "POST /v1/chat/groups/{conversationId}/members/{memberAction}":
		g.chatGroupCommand(ctx, w, r)
	case "GET /v1/chat/conversations/{conversationId}/messages":
		before, _ := strconv.ParseInt(r.URL.Query().Get("before_sequence"), 10, 64)
		response, err := g.chatClient.ListMessages(ctx, &chatv1.ListMessagesRequest{ConversationId: r.PathValue("conversationId"), BeforeSequence: before, Page: page})
		writeChatResponse(w, response, err, http.StatusOK)
	case "POST /v1/chat/blocks":
		var input struct {
			BlockedUserID  string `json:"blockedUserId"`
			IdempotencyKey string `json:"idempotencyKey"`
		}
		if platform.DecodeJSON(r, &input) != nil {
			platform.Problem(w, http.StatusBadRequest, "invalid_chat_request", "Invalid block request")
			return
		}
		response, err := g.chatClient.BlockUser(ctx, &chatv1.BlockUserRequest{BlockedUserId: input.BlockedUserID, Audit: audit(input.IdempotencyKey, "")})
		writeChatResponse(w, response, err, http.StatusOK)
	case "DELETE /v1/chat/blocks/{blockedUserId}":
		response, err := g.chatClient.UnblockUser(ctx, &chatv1.BlockUserRequest{BlockedUserId: r.PathValue("blockedUserId"), Audit: audit(r.Header.Get("Idempotency-Key"), "")})
		writeChatResponse(w, response, err, http.StatusOK)
	case "POST /v1/chat/attachments:prepare":
		var input struct {
			ConversationID string `json:"conversationId"`
			Kind           string `json:"kind"`
			MimeType       string `json:"mimeType"`
			SizeBytes      int64  `json:"sizeBytes"`
			SHA256         string `json:"sha256"`
			IdempotencyKey string `json:"idempotencyKey"`
		}
		if platform.DecodeJSON(r, &input) != nil {
			platform.Problem(w, 400, "invalid_chat_request", "Invalid attachment request")
			return
		}
		request := &chatv1.PrepareAttachmentRequest{ConversationId: input.ConversationID, Kind: chatMessageKind(input.Kind), MimeType: input.MimeType, SizeBytes: input.SizeBytes, Sha256: input.SHA256, Audit: audit(input.IdempotencyKey, "")}
		response, err := g.chatClient.PrepareAttachment(ctx, request)
		writeChatResponse(w, response, err, http.StatusCreated)
	case "POST /v1/chat/attachments:complete":
		var input struct {
			AttachmentID   string `json:"attachmentId"`
			IdempotencyKey string `json:"idempotencyKey"`
		}
		if platform.DecodeJSON(r, &input) != nil {
			platform.Problem(w, 400, "invalid_chat_request", "Invalid attachment request")
			return
		}
		request := &chatv1.CompleteAttachmentRequest{AttachmentId: input.AttachmentID, Audit: audit(input.IdempotencyKey, "")}
		response, err := g.chatClient.CompleteAttachment(ctx, request)
		writeChatResponse(w, response, err, http.StatusAccepted)
	case "GET /v1/chat/attachments/{attachmentAction}":
		id := strings.TrimSuffix(r.PathValue("attachmentAction"), ":download")
		if id == r.PathValue("attachmentAction") {
			platform.Problem(w, http.StatusNotFound, "chat_route_not_found", "Chat route not found")
			return
		}
		response, err := g.chatClient.GetAttachmentDownload(ctx, &chatv1.GetAttachmentDownloadRequest{AttachmentId: id})
		writeChatResponse(w, response, err, http.StatusOK)
	case "POST /v1/chat/messages/{messageAction}":
		id := strings.TrimSuffix(r.PathValue("messageAction"), ":report")
		if id == r.PathValue("messageAction") {
			platform.Problem(w, http.StatusNotFound, "chat_route_not_found", "Chat route not found")
			return
		}
		var input struct{ Reason, IdempotencyKey string }
		if platform.DecodeJSON(r, &input) != nil {
			platform.Problem(w, http.StatusBadRequest, "invalid_chat_request", "Invalid report request")
			return
		}
		response, err := g.chatClient.ReportMessage(ctx, &chatv1.ReportMessageRequest{MessageId: id, Reason: input.Reason, Audit: audit(input.IdempotencyKey, input.Reason)})
		writeChatResponse(w, response, err, http.StatusCreated)
	case "POST /v1/chat/ws-ticket":
		response, err := g.chatClient.CreateWebSocketTicket(ctx, &chatv1.CreateWebSocketTicketRequest{})
		writeChatResponse(w, response, err, http.StatusCreated)
	}
}

func (g *gateway) chatGroupCommand(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	if r.Pattern == "POST /v1/chat/groups/{groupAction}" {
		action := r.PathValue("groupAction")
		id := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(action, ":delete"), ":join"), ":leave")
		if id == action {
			platform.Problem(w, http.StatusNotFound, "chat_group_route_not_found", "Chat group route not found")
			return
		}
		if strings.HasSuffix(action, ":delete") {
			var input struct{ Reason, IdempotencyKey string }
			if platform.DecodeJSON(r, &input) != nil {
				platform.Problem(w, 400, "invalid_chat_request", "Invalid group deletion request")
				return
			}
			response, err := g.chatClient.DeleteGroup(ctx, &chatv1.GroupCommandRequest{ConversationId: id, Audit: audit(input.IdempotencyKey, input.Reason)})
			writeChatResponse(w, response, err, http.StatusOK)
			return
		}
		if strings.HasSuffix(action, ":join") {
			response, err := g.chatClient.JoinGroup(ctx, &chatv1.GroupCommandRequest{ConversationId: id, Audit: audit(r.Header.Get("Idempotency-Key"), "")})
			writeChatResponse(w, response, err, http.StatusOK)
			return
		}
		response, err := g.chatClient.LeaveGroup(ctx, &chatv1.GroupCommandRequest{ConversationId: id, Audit: audit(r.Header.Get("Idempotency-Key"), "")})
		writeChatResponse(w, response, err, http.StatusOK)
		return
	}
	if r.Pattern == "PUT /v1/chat/groups/{conversationId}/members/{userId}" {
		request := &chatv1.PutGroupMemberRequest{ConversationId: r.PathValue("conversationId"), UserId: r.PathValue("userId"), Audit: audit(r.Header.Get("Idempotency-Key"), "")}
		response, err := g.chatClient.PutGroupMember(ctx, request)
		writeChatResponse(w, response, err, http.StatusOK)
		return
	}
	if r.Pattern == "POST /v1/chat/groups/{conversationId}/members/{memberAction}" {
		action := r.PathValue("memberAction")
		userID := strings.TrimSuffix(strings.TrimSuffix(action, ":ban"), ":unban")
		if userID == action {
			platform.Problem(w, http.StatusNotFound, "chat_group_route_not_found", "Chat group route not found")
			return
		}
		request := &chatv1.PutGroupMemberRequest{ConversationId: r.PathValue("conversationId"), UserId: userID}
		var input struct{ Reason, IdempotencyKey string }
		if platform.DecodeJSON(r, &input) != nil {
			platform.Problem(w, http.StatusBadRequest, "invalid_chat_request", "Invalid group member request")
			return
		}
		request.Audit = audit(input.IdempotencyKey, input.Reason)
		if strings.HasSuffix(action, ":ban") {
			response, err := g.chatClient.BanGroupMember(ctx, request)
			writeChatResponse(w, response, err, http.StatusOK)
			return
		}
		response, err := g.chatClient.UnbanGroupMember(ctx, request)
		writeChatResponse(w, response, err, http.StatusOK)
		return
	}
}

func chatMessageKind(value string) chatv1.MessageKind {
	switch value {
	case "MESSAGE_KIND_IMAGE":
		return chatv1.MessageKind_MESSAGE_KIND_IMAGE
	case "MESSAGE_KIND_VIDEO":
		return chatv1.MessageKind_MESSAGE_KIND_VIDEO
	case "MESSAGE_KIND_TEXT":
		return chatv1.MessageKind_MESSAGE_KIND_TEXT
	default:
		return chatv1.MessageKind_MESSAGE_KIND_UNSPECIFIED
	}
}

func (g *gateway) chatWebSocket(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if origin == "" || !allowedBrowserOrigin(origin) {
		platform.Problem(w, http.StatusForbidden, "websocket_origin_denied", "WebSocket origin is not allowed")
		return
	}
	ticket := r.URL.Query().Get("ticket")
	if ticket == "" {
		platform.Problem(w, http.StatusUnauthorized, "websocket_ticket_required", "WebSocket ticket is required")
		return
	}
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }, HandshakeTimeout: 5 * time.Second}
	connection, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer connection.Close()
	connection.SetReadLimit(32 << 10)
	_ = connection.SetReadDeadline(time.Now().Add(75 * time.Second))
	stream, err := g.chatClient.Connect(r.Context())
	if err != nil {
		return
	}
	if err = stream.Send(&chatv1.ClientFrame{Version: 1, Ticket: ticket}); err != nil {
		return
	}
	var writeMu sync.Mutex
	errorsChannel := make(chan error, 2)
	go func() {
		for {
			_, data, readErr := connection.ReadMessage()
			if readErr != nil {
				errorsChannel <- readErr
				return
			}
			_ = connection.SetReadDeadline(time.Now().Add(75 * time.Second))
			frame := &chatv1.ClientFrame{}
			if unmarshalErr := chatJSONInput.Unmarshal(data, frame); unmarshalErr != nil {
				continue
			}
			if sendErr := stream.Send(frame); sendErr != nil {
				errorsChannel <- sendErr
				return
			}
		}
	}()
	go func() {
		for {
			frame, receiveErr := stream.Recv()
			if receiveErr != nil {
				errorsChannel <- receiveErr
				return
			}
			data, marshalErr := chatJSON.Marshal(frame)
			if marshalErr != nil {
				continue
			}
			writeMu.Lock()
			_ = connection.SetWriteDeadline(time.Now().Add(10 * time.Second))
			writeErr := connection.WriteMessage(websocket.TextMessage, data)
			writeMu.Unlock()
			if writeErr != nil {
				errorsChannel <- writeErr
				return
			}
		}
	}()
	<-errorsChannel
	_ = stream.CloseSend()
	writeMu.Lock()
	_ = connection.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "reconnect"), time.Now().Add(time.Second))
	writeMu.Unlock()
}

func writeChatResponse(w http.ResponseWriter, value proto.Message, err error, statusCode int) {
	if err != nil {
		grpcProblem(w, err)
		return
	}
	data, marshalErr := chatJSON.Marshal(value)
	if marshalErr != nil {
		platform.Problem(w, 500, "chat_response_error", "Unable to encode chat response")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_, _ = w.Write(data)
}
