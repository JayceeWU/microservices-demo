package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/JayceeWU/microservices-demo/dancehub/internal/appcore"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/chat/application"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/chat/domain"
	"github.com/jackc/pgx/v5"
)

const conversationColumns = `c.id::text,c.kind::text,COALESCE(c.studio_id::text,''),COALESCE(c.teacher_id::text,''),COALESCE(c.student_id::text,''),COALESCE(c.owner_user_id::text,''),c.title,c.description,COALESCE(c.avatar_url,''),c.member_count,c.last_sequence,c.active,c.created_at`

func scanConversation(row scanner) (*domain.Conversation, error) {
	c := &domain.Conversation{}
	var kind string
	err := row.Scan(&c.ID, &kind, &c.StudioID, &c.TeacherID, &c.StudentID, &c.OwnerID, &c.Title, &c.Description, &c.AvatarURL, &c.MemberCount, &c.LastSequence, &c.Active, &c.CreatedAt)
	c.Kind = domain.ConversationKind(kind)
	return c, err
}

type conversationRepository struct{ tx pgx.Tx }

func (r conversationRepository) List(ctx context.Context, userID string, page application.Page) ([]application.ConversationView, string, error) {
	offset := decodeOffset(page.Token)
	// A studio admin sees a new STUDIO_SUPPORT conversation before joining it, so the
	// participant row may be absent: the joined flag must scan as false, not NULL.
	rows, err := r.tx.Query(ctx, `SELECT `+conversationColumns+`,COALESCE(p.last_read_sequence,0),COALESCE(p.state='ACTIVE',false)
		FROM chat.conversations c LEFT JOIN chat.conversation_participants p ON p.conversation_id=c.id AND p.user_id=$1
		WHERE c.active AND (p.state='ACTIVE' OR (c.kind='STUDIO_SUPPORT' AND c.studio_id=app_security.studio_id() AND app_security.has_tenant_role('studio_admin')))
		ORDER BY c.created_at DESC,c.id LIMIT $2 OFFSET $3`, userID, page.Size+1, offset)
	if err != nil {
		return nil, "", err
	}
	return scanConversationViews(rows, page.Size, offset)
}

func (r conversationRepository) ListGroups(ctx context.Context, userID, studioID, teacherID string, page application.Page) ([]application.ConversationView, string, error) {
	offset := decodeOffset(page.Token)
	rows, err := r.tx.Query(ctx, `SELECT `+conversationColumns+`,COALESCE(p.last_read_sequence,0),COALESCE(p.state='ACTIVE',false)
		FROM chat.conversations c LEFT JOIN chat.conversation_participants p ON p.conversation_id=c.id AND p.user_id=NULLIF($1,'')::uuid
		WHERE c.active AND c.kind IN('STUDIO_GROUP','TEACHER_GROUP')
		AND (NULLIF($2,'')::uuid IS NULL OR c.studio_id=NULLIF($2,'')::uuid)
		AND (NULLIF($3,'')::uuid IS NULL OR c.teacher_id=NULLIF($3,'')::uuid)
		ORDER BY c.created_at DESC,c.id LIMIT $4 OFFSET $5`, userID, studioID, teacherID, page.Size+1, offset)
	if err != nil {
		return nil, "", err
	}
	return scanConversationViews(rows, page.Size, offset)
}

func scanConversationViews(rows pgx.Rows, pageSize int32, offset int) ([]application.ConversationView, string, error) {
	defer rows.Close()
	items := make([]application.ConversationView, 0, pageSize)
	for rows.Next() {
		c := &domain.Conversation{}
		var kind string
		var view application.ConversationView
		if err := rows.Scan(&c.ID, &kind, &c.StudioID, &c.TeacherID, &c.StudentID, &c.OwnerID, &c.Title, &c.Description, &c.AvatarURL, &c.MemberCount, &c.LastSequence, &c.Active, &c.CreatedAt, &view.LastRead, &view.Joined); err != nil {
			return nil, "", err
		}
		c.Kind, view.Conversation = domain.ConversationKind(kind), c
		items = append(items, view)
	}
	return paginateConversations(items, pageSize, offset), nextOffset(len(items), pageSize, offset), rows.Err()
}

func (r conversationRepository) Get(ctx context.Context, id string, lock bool) (*application.ConversationView, error) {
	// chat_runtime deliberately has no UPDATE privilege on conversations. A row
	// lock would require that privilege even for read-only aggregate loading, so
	// serialize membership commands with a transaction-scoped advisory lock.
	// Message sequence allocation remains atomic in next_message_sequence().
	if lock {
		if _, err := r.tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, id); err != nil {
			return nil, err
		}
	}
	query := `SELECT ` + conversationColumns + ` FROM chat.conversations c WHERE c.id=$1`
	c, err := scanConversation(r.tx.QueryRow(ctx, query, id))
	if err != nil {
		return nil, translate(err, "conversation not found")
	}
	return &application.ConversationView{Conversation: c}, nil
}

func (r conversationRepository) FindDirect(ctx context.Context, student, teacher string) (*application.ConversationView, error) {
	low, high := sortedPair(student, teacher)
	c, err := scanConversation(r.tx.QueryRow(ctx, `SELECT `+conversationColumns+` FROM chat.conversations c WHERE c.kind='DIRECT_TEACHER' AND c.direct_low_user_id=$1 AND c.direct_high_user_id=$2 AND c.active`, low, high))
	if err != nil {
		return nil, translate(err, "direct conversation not found")
	}
	return &application.ConversationView{Conversation: c, Joined: true}, nil
}

func (r conversationRepository) FindStudioSupport(ctx context.Context, studio, student string) (*application.ConversationView, error) {
	c, err := scanConversation(r.tx.QueryRow(ctx, `SELECT `+conversationColumns+` FROM chat.conversations c WHERE c.kind='STUDIO_SUPPORT' AND c.studio_id=$1 AND c.student_id=$2 AND c.active`, studio, student))
	if err != nil {
		return nil, translate(err, "studio conversation not found")
	}
	return &application.ConversationView{Conversation: c, Joined: true}, nil
}

func (r conversationRepository) AddDirect(ctx context.Context, c *domain.Conversation, student, teacher, creator string) (*application.ConversationView, error) {
	low, high := sortedPair(student, teacher)
	tag, err := r.tx.Exec(ctx, `INSERT INTO chat.conversations(id,kind,student_id,teacher_id,direct_low_user_id,direct_high_user_id,member_count,created_at) VALUES($1,'DIRECT_TEACHER',$2,$3,$4,$5,2,$6)
		ON CONFLICT(direct_low_user_id,direct_high_user_id) WHERE kind='DIRECT_TEACHER' AND active DO NOTHING`, c.ID, student, teacher, low, high, c.CreatedAt)
	if err != nil {
		return nil, translate(err, "unable to create direct conversation")
	}
	if tag.RowsAffected() == 0 {
		existing, loadErr := r.FindDirect(ctx, student, teacher)
		if loadErr != nil {
			return nil, loadErr
		}
		c.ID = existing.Conversation.ID
	}
	_, err = r.tx.Exec(ctx, `INSERT INTO chat.conversation_participants(conversation_id,user_id,owner_user_id,visible_from_sequence) VALUES($1,$2,$4,1),($1,$3,$4,1) ON CONFLICT(conversation_id,user_id) DO NOTHING`, c.ID, student, teacher, creator)
	if err != nil {
		return nil, err
	}
	return r.Get(ctx, c.ID, false)
}

func (r conversationRepository) AddStudioSupport(ctx context.Context, c *domain.Conversation, creator string) (*application.ConversationView, error) {
	tag, err := r.tx.Exec(ctx, `INSERT INTO chat.conversations(id,kind,studio_id,student_id,member_count,created_at) VALUES($1,'STUDIO_SUPPORT',$2,$3,1,$4)
		ON CONFLICT(studio_id,student_id) WHERE kind='STUDIO_SUPPORT' AND active DO NOTHING`, c.ID, c.StudioID, c.StudentID, c.CreatedAt)
	if err != nil {
		return nil, translate(err, "unable to create studio conversation")
	}
	if tag.RowsAffected() == 0 {
		existing, loadErr := r.FindStudioSupport(ctx, c.StudioID, c.StudentID)
		if loadErr != nil {
			return nil, loadErr
		}
		c.ID = existing.Conversation.ID
	}
	_, err = r.tx.Exec(ctx, `INSERT INTO chat.conversation_participants(conversation_id,user_id,studio_id,owner_user_id,visible_from_sequence) VALUES($1,$2,$3,$4,1) ON CONFLICT(conversation_id,user_id) DO NOTHING`, c.ID, c.StudentID, c.StudioID, creator)
	if err != nil {
		return nil, err
	}
	return r.Get(ctx, c.ID, false)
}

func (r conversationRepository) AddGroup(ctx context.Context, c *domain.Conversation, owner string) (*application.ConversationView, error) {
	_, err := r.tx.Exec(ctx, `INSERT INTO chat.conversations(id,kind,studio_id,teacher_id,owner_user_id,title,description,avatar_url,member_count,created_at) VALUES($1,$2::chat.conversation_kind,$3,NULLIF($4,'')::uuid,$5,$6,$7,NULLIF($8,''),1,$9)`, c.ID, c.Kind, c.StudioID, c.TeacherID, owner, c.Title, c.Description, c.AvatarURL, c.CreatedAt)
	if err != nil {
		return nil, translate(err, "unable to create group")
	}
	_, err = r.tx.Exec(ctx, `INSERT INTO chat.conversation_participants(conversation_id,user_id,studio_id,owner_user_id,role,state,visible_from_sequence) VALUES($1,$2,$3,$2,'OWNER','ACTIVE',1)`, c.ID, owner, c.StudioID)
	if err != nil {
		return nil, err
	}
	return r.Get(ctx, c.ID, false)
}

func (r conversationRepository) DeleteGroup(ctx context.Context, c *domain.Conversation) error {
	if _, err := r.tx.Exec(ctx, `INSERT INTO chat.media_deletion_jobs(object_key)
		SELECT clean_object_key FROM chat.attachments WHERE conversation_id=$1 AND clean_object_key IS NOT NULL`, c.ID); err != nil {
		return err
	}
	tag, err := r.tx.Exec(ctx, `DELETE FROM chat.conversations WHERE id=$1 AND kind IN('STUDIO_GROUP','TEACHER_GROUP')`, c.ID)
	if err != nil {
		return translate(err, "unable to delete group")
	}
	if tag.RowsAffected() != 1 {
		return appcore.Conflict("group changed concurrently")
	}
	return nil
}

func (r conversationRepository) Join(ctx context.Context, c *domain.Conversation, userID string, manager bool) (application.Participant, error) {
	var p application.Participant
	var joined time.Time
	err := r.tx.QueryRow(ctx, `INSERT INTO chat.conversation_participants(conversation_id,user_id,studio_id,owner_user_id,state,joined_at,visible_from_sequence,updated_at)
		VALUES($1,$2,$3,$4,'ACTIVE',now(),$5,now()) ON CONFLICT(conversation_id,user_id) DO UPDATE SET state='ACTIVE',joined_at=now(),visible_from_sequence=$5,updated_at=now()
		WHERE chat.conversation_participants.state<>'BANNED'
		RETURNING conversation_id::text,user_id::text,role::text,state::text,visible_from_sequence,last_read_sequence,joined_at`, c.ID, userID, c.StudioID, c.OwnerID, c.LastSequence+1).Scan(&p.ConversationID, &p.UserID, &p.Role, &p.State, &p.VisibleFrom, &p.LastRead, &joined)
	p.JoinedAt = joined
	if err != nil {
		return p, translate(err, "unable to join group")
	}
	err = r.tx.QueryRow(ctx, `SELECT chat.refresh_member_count($1)`, c.ID).Scan(&c.MemberCount)
	return p, err
}

func (r conversationRepository) Leave(ctx context.Context, c *domain.Conversation, userID string, manager bool) (application.Participant, error) {
	return r.setParticipant(ctx, c, userID, "LEFT")
}

func (r conversationRepository) SetMemberState(ctx context.Context, c *domain.Conversation, userID, action, actorID, reason string) (application.Participant, error) {
	state := "BANNED"
	if action == "unban" {
		state = "LEFT"
	}
	p, err := r.setParticipant(ctx, c, userID, state)
	if err != nil {
		return p, err
	}
	if action == "ban" {
		_, err = r.tx.Exec(ctx, `INSERT INTO chat.group_bans(conversation_id,user_id,studio_id,owner_user_id,banned_by,reason) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(conversation_id,user_id) DO UPDATE SET banned_by=EXCLUDED.banned_by,reason=EXCLUDED.reason,created_at=now(),lifted_at=NULL,lifted_by=NULL`, c.ID, userID, c.StudioID, c.OwnerID, actorID, reason)
	} else {
		_, err = r.tx.Exec(ctx, `UPDATE chat.group_bans SET lifted_at=now(),lifted_by=$3 WHERE conversation_id=$1 AND user_id=$2`, c.ID, userID, actorID)
	}
	if err == nil {
		_, err = r.tx.Exec(ctx, `INSERT INTO chat.moderation_audit(conversation_id,studio_id,actor_id,target_user_id,action,reason,request_id) VALUES($1,$2,$3,$4,$5,$6,app_security.setting('app.request_id'))`, c.ID, c.StudioID, actorID, userID, action, reason)
	}
	return p, err
}

func (r conversationRepository) setParticipant(ctx context.Context, c *domain.Conversation, userID, state string) (application.Participant, error) {
	var p application.Participant
	err := r.tx.QueryRow(ctx, `UPDATE chat.conversation_participants SET state=$3::chat.member_state,updated_at=now() WHERE conversation_id=$1 AND user_id=$2 RETURNING conversation_id::text,user_id::text,role::text,state::text,visible_from_sequence,last_read_sequence,joined_at`, c.ID, userID, state).Scan(&p.ConversationID, &p.UserID, &p.Role, &p.State, &p.VisibleFrom, &p.LastRead, &p.JoinedAt)
	if err != nil {
		return p, translate(err, "participant not found")
	}
	err = r.tx.QueryRow(ctx, `SELECT chat.refresh_member_count($1)`, c.ID).Scan(&c.MemberCount)
	return p, err
}

func (r conversationRepository) CanAccess(ctx context.Context, conversationID, userID string) (bool, bool, error) {
	var kind, owner, studio string
	var active bool
	err := r.tx.QueryRow(ctx, `SELECT kind::text,COALESCE(owner_user_id::text,''),COALESCE(studio_id::text,''),active FROM chat.conversations WHERE id=$1`, conversationID).Scan(&kind, &owner, &studio, &active)
	if err != nil {
		return false, false, translate(err, "conversation not found")
	}
	if !active {
		return false, false, nil
	}
	if studio != "" {
		var studioManager bool
		if err = r.tx.QueryRow(ctx, `SELECT $1::uuid=app_security.studio_id() AND app_security.has_tenant_role('studio_admin')`, studio).Scan(&studioManager); err != nil {
			return false, false, err
		}
		if studioManager {
			return true, true, nil
		}
	}
	var state, role string
	err = r.tx.QueryRow(ctx, `SELECT state::text,role::text FROM chat.conversation_participants WHERE conversation_id=$1 AND user_id=$2`, conversationID, userID).Scan(&state, &role)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, false, nil
	}
	return state == "ACTIVE", role == "OWNER" || role == "MODERATOR" || owner == userID, err
}

func (r conversationRepository) Participant(ctx context.Context, conversationID, userID string) (application.Participant, error) {
	var p application.Participant
	err := r.tx.QueryRow(ctx, `SELECT conversation_id::text,user_id::text,role::text,state::text,visible_from_sequence,last_read_sequence,joined_at FROM chat.conversation_participants WHERE conversation_id=$1 AND user_id=$2`, conversationID, userID).Scan(&p.ConversationID, &p.UserID, &p.Role, &p.State, &p.VisibleFrom, &p.LastRead, &p.JoinedAt)
	return p, translate(err, "participant not found")
}
