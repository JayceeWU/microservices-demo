package postgres

import (
	"context"
	"errors"
	"strconv"

	"github.com/JayceeWU/microservices-demo/dancehub/internal/appcore"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/chat/application"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/chat/domain"
	"github.com/jackc/pgx/v5"
)

type messageRepository struct{ tx pgx.Tx }

func (r messageRepository) List(ctx context.Context, conversationID string, before int64, page application.Page) ([]application.MessageView, string, error) {
	if before <= 0 {
		before = 1 << 62
	}
	rows, err := r.tx.Query(ctx, `SELECT id::text,conversation_id::text,COALESCE(studio_id::text,''),sequence,sender_id::text,sender_display_name,client_message_id::text,kind::text,body,COALESCE(reply_to_message_id::text,''),created_at,edited_at,withdrawn_at FROM chat.messages WHERE conversation_id=$1 AND sequence<$2 ORDER BY sequence DESC LIMIT $3`, conversationID, before, page.Size+1)
	if err != nil {
		return nil, "", err
	}
	messages := make([]*domain.Message, 0, page.Size+1)
	for rows.Next() {
		m, scanErr := scanMessage(rows)
		if scanErr != nil {
			rows.Close()
			return nil, "", scanErr
		}
		messages = append(messages, m)
	}
	rowsErr := rows.Err()
	rows.Close()
	if rowsErr != nil {
		return nil, "", rowsErr
	}
	next := ""
	if len(messages) > int(page.Size) {
		next = strconv.FormatInt(messages[page.Size-1].Sequence, 10)
		messages = messages[:page.Size]
	}
	items := make([]application.MessageView, 0, len(messages))
	for _, message := range messages {
		attachments, loadErr := r.attachments(ctx, message.ID)
		if loadErr != nil {
			return nil, "", loadErr
		}
		items = append(items, application.MessageView{Message: message, Attachments: attachments})
	}
	return items, next, nil
}

func (r messageRepository) Get(ctx context.Context, id string, lock bool) (*application.MessageView, error) {
	query := `SELECT id::text,conversation_id::text,COALESCE(studio_id::text,''),sequence,sender_id::text,sender_display_name,client_message_id::text,kind::text,body,COALESCE(reply_to_message_id::text,''),created_at,edited_at,withdrawn_at FROM chat.messages WHERE id=$1`
	if lock {
		query += ` FOR UPDATE`
	}
	m, err := scanMessage(r.tx.QueryRow(ctx, query, id))
	if err != nil {
		return nil, translate(err, "message not found")
	}
	attachments, err := r.attachments(ctx, id)
	return &application.MessageView{Message: m, Attachments: attachments}, err
}

func scanMessage(row scanner) (*domain.Message, error) {
	m := &domain.Message{}
	var kind string
	err := row.Scan(&m.ID, &m.ConversationID, &m.StudioID, &m.Sequence, &m.SenderID, &m.SenderDisplayName, &m.ClientMessageID, &kind, &m.Body, &m.ReplyToID, &m.CreatedAt, &m.EditedAt, &m.WithdrawnAt)
	m.Kind = domain.MessageKind(kind)
	return m, err
}

func (r messageRepository) Add(ctx context.Context, m *domain.Message, attachmentIDs []string) (*application.MessageView, bool, error) {
	var existing string
	err := r.tx.QueryRow(ctx, `SELECT id::text FROM chat.messages WHERE sender_id=$1 AND client_message_id=$2`, m.SenderID, m.ClientMessageID).Scan(&existing)
	if err == nil {
		view, loadErr := r.Get(ctx, existing, false)
		return view, false, loadErr
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, false, err
	}
	err = r.tx.QueryRow(ctx, `SELECT chat.next_message_sequence($1)`, m.ConversationID).Scan(&m.Sequence)
	if err != nil {
		return nil, false, translate(err, "conversation not found")
	}
	tag, err := r.tx.Exec(ctx, `INSERT INTO chat.messages(id,conversation_id,studio_id,conversation_kind,sequence,sender_id,sender_display_name,client_message_id,kind,body,reply_to_message_id,created_at)
		SELECT $1,c.id,c.studio_id,c.kind,$2,$3,$4,$5,$6::chat.message_kind,$7,NULLIF($8,'')::uuid,$9 FROM chat.conversations c
		WHERE c.id=$10 AND (NULLIF($8,'')::uuid IS NULL OR EXISTS(SELECT 1 FROM chat.messages reply WHERE reply.id=NULLIF($8,'')::uuid AND reply.conversation_id=c.id AND reply.withdrawn_at IS NULL))`, m.ID, m.Sequence, m.SenderID, m.SenderDisplayName, m.ClientMessageID, m.Kind, m.Body, m.ReplyToID, m.CreatedAt, m.ConversationID)
	if err != nil {
		return nil, false, translate(err, "unable to send message")
	}
	if tag.RowsAffected() == 0 {
		return nil, false, appcore.Invalid("reply target is not available in this conversation")
	}
	if len(attachmentIDs) > 0 {
		tag, attachErr := r.tx.Exec(ctx, `UPDATE chat.attachments SET message_id=$1 WHERE id=ANY($2::uuid[]) AND conversation_id=$3 AND uploader_id=$4 AND status='CLEAN' AND message_id IS NULL`, m.ID, attachmentIDs, m.ConversationID, m.SenderID)
		if attachErr != nil {
			return nil, false, attachErr
		}
		if tag.RowsAffected() != int64(len(attachmentIDs)) {
			return nil, false, appcore.Conflict("one or more attachments are not ready")
		}
	}
	view, err := r.Get(ctx, m.ID, false)
	return view, true, err
}

func (r messageRepository) Save(ctx context.Context, m *domain.Message) (*application.MessageView, error) {
	_, err := r.tx.Exec(ctx, `UPDATE chat.messages SET body=$2,edited_at=$3,withdrawn_at=$4 WHERE id=$1`, m.ID, m.Body, m.EditedAt, m.WithdrawnAt)
	if err != nil {
		return nil, err
	}
	return r.Get(ctx, m.ID, false)
}

func (r messageRepository) AuditModeration(ctx context.Context, message *domain.Message, actorID, beforeBody, reason string) error {
	_, err := r.tx.Exec(ctx, `INSERT INTO chat.moderation_audit(conversation_id,studio_id,actor_id,message_id,action,reason,before_state,after_state,request_id) VALUES($1,NULLIF($2,'')::uuid,$3,$4,'withdraw_message',$5,jsonb_build_object('sender_id',$6,'body',$7),jsonb_build_object('withdrawn',true,'body',''),app_security.setting('app.request_id'))`, message.ConversationID, message.StudioID, actorID, message.ID, reason, message.SenderID, beforeBody)
	return err
}

const participantColumns = `p.conversation_id::text,p.user_id::text,p.role::text,p.state::text,p.visible_from_sequence,p.last_read_sequence,p.joined_at`

// MarkRead advances the caller's read receipt and reports whether it moved. A receipt that
// is already at or beyond the requested sequence is returned unchanged without a write, so
// clients that re-send the same receipt after every frame do not trigger another broadcast.
func (r messageRepository) MarkRead(ctx context.Context, conversationID, userID string, sequence int64) (application.Participant, bool, error) {
	var p application.Participant
	_, err := r.tx.Exec(ctx, `INSERT INTO chat.conversation_participants(conversation_id,user_id,studio_id,owner_user_id,role,state,visible_from_sequence)
		SELECT c.id,$2,c.studio_id,$2,'MODERATOR','ACTIVE',1 FROM chat.conversations c
		WHERE c.id=$1 AND c.studio_id=app_security.studio_id() AND app_security.has_tenant_role('studio_admin')
		ON CONFLICT(conversation_id,user_id) DO NOTHING`, conversationID, userID)
	if err != nil {
		return p, false, err
	}
	err = r.tx.QueryRow(ctx, `UPDATE chat.conversation_participants p SET last_read_sequence=LEAST($3,c.last_sequence),updated_at=now() FROM chat.conversations c WHERE p.conversation_id=$1 AND p.user_id=$2 AND p.state='ACTIVE' AND c.id=p.conversation_id AND LEAST($3,c.last_sequence)>p.last_read_sequence RETURNING `+participantColumns, conversationID, userID, sequence).Scan(&p.ConversationID, &p.UserID, &p.Role, &p.State, &p.VisibleFrom, &p.LastRead, &p.JoinedAt)
	if err == nil {
		return p, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return p, false, err
	}
	err = r.tx.QueryRow(ctx, `SELECT `+participantColumns+` FROM chat.conversation_participants p WHERE p.conversation_id=$1 AND p.user_id=$2 AND p.state='ACTIVE'`, conversationID, userID).Scan(&p.ConversationID, &p.UserID, &p.Role, &p.State, &p.VisibleFrom, &p.LastRead, &p.JoinedAt)
	return p, false, translate(err, "participant not found")
}

func (r messageRepository) Report(ctx context.Context, messageID, reporterID, reason string) (application.Report, error) {
	var result application.Report
	err := r.tx.QueryRow(ctx, `INSERT INTO chat.message_reports(message_id,conversation_id,reporter_id,reason,reported_body_snapshot) SELECT id,conversation_id,$2,$3,body FROM chat.messages WHERE id=$1 ON CONFLICT(message_id,reporter_id) DO UPDATE SET reason=EXCLUDED.reason RETURNING id::text,message_id::text,reporter_id::text,reason,created_at`, messageID, reporterID, reason).Scan(&result.ID, &result.MessageID, &result.ReporterID, &result.Reason, &result.CreatedAt)
	return result, translate(err, "message not found")
}

func (r messageRepository) attachments(ctx context.Context, messageID string) ([]application.Attachment, error) {
	rows, err := r.tx.Query(ctx, `SELECT id::text,conversation_id::text,uploader_id::text,kind::text,mime_type,size_bytes,sha256,object_key,status::text FROM chat.attachments WHERE message_id=$1`, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []application.Attachment{}
	for rows.Next() {
		var a application.Attachment
		var kind string
		if err = rows.Scan(&a.ID, &a.ConversationID, &a.UploaderID, &kind, &a.MimeType, &a.SizeBytes, &a.SHA256, &a.ObjectKey, &a.Status); err != nil {
			return nil, err
		}
		a.Kind = domain.MessageKind(kind)
		result = append(result, a)
	}
	return result, rows.Err()
}
