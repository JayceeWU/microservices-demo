package domain

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrForbidden    = errors.New("chat action is forbidden")
	ErrInvalid      = errors.New("invalid chat command")
	ErrBlocked      = errors.New("direct conversation is blocked")
	ErrGroupFull    = errors.New("group has reached 1000 members")
	ErrWindowClosed = errors.New("message editing window is closed")
)

type ConversationKind string

const (
	DirectTeacher ConversationKind = "DIRECT_TEACHER"
	StudioSupport ConversationKind = "STUDIO_SUPPORT"
	StudioGroup   ConversationKind = "STUDIO_GROUP"
	TeacherGroup  ConversationKind = "TEACHER_GROUP"
)

type MessageKind string

const (
	TextMessage   MessageKind = "TEXT"
	ImageMessage  MessageKind = "IMAGE"
	VideoMessage  MessageKind = "VIDEO"
	SystemMessage MessageKind = "SYSTEM"
)

type Conversation struct {
	ID, StudioID, TeacherID, StudentID, OwnerID string
	Kind                                        ConversationKind
	Title, Description, AvatarURL               string
	MemberCount                                 int32
	LastSequence                                int64
	Active                                      bool
	CreatedAt                                   time.Time
}

func NewGroup(kind ConversationKind, studioID, ownerID, title, description, avatar string, teacher bool) (*Conversation, error) {
	if studioID == "" || ownerID == "" || strings.TrimSpace(title) == "" {
		return nil, ErrInvalid
	}
	if (teacher && kind != TeacherGroup) || (!teacher && kind != StudioGroup) {
		return nil, ErrForbidden
	}
	group := &Conversation{Kind: kind, StudioID: studioID, OwnerID: ownerID, Title: strings.TrimSpace(title), Description: strings.TrimSpace(description), AvatarURL: strings.TrimSpace(avatar), MemberCount: 1, Active: true}
	if teacher {
		group.TeacherID = ownerID
	}
	return group, nil
}

func (c *Conversation) Join(banned bool) error {
	if !c.Active || banned {
		return ErrForbidden
	}
	if c.Kind != StudioGroup && c.Kind != TeacherGroup {
		return ErrInvalid
	}
	if c.MemberCount >= 1000 {
		return ErrGroupFull
	}
	c.MemberCount++
	return nil
}

func (c *Conversation) Leave() error {
	if c.Kind != StudioGroup && c.Kind != TeacherGroup {
		return ErrInvalid
	}
	if c.MemberCount > 0 {
		c.MemberCount--
	}
	return nil
}

type Message struct {
	ID, ConversationID, StudioID, SenderID, SenderDisplayName, ClientMessageID, Body, ReplyToID string
	Kind                                                                                        MessageKind
	Sequence                                                                                    int64
	CreatedAt                                                                                   time.Time
	EditedAt, WithdrawnAt                                                                       *time.Time
}

func NewMessage(conversationID, studioID, senderID, clientID string, kind MessageKind, body, reply string, now time.Time) (*Message, error) {
	if conversationID == "" || senderID == "" || clientID == "" {
		return nil, ErrInvalid
	}
	if len([]rune(body)) > 4000 {
		return nil, ErrInvalid
	}
	if kind == TextMessage && strings.TrimSpace(body) == "" {
		return nil, ErrInvalid
	}
	if kind != TextMessage && kind != ImageMessage && kind != VideoMessage {
		return nil, ErrInvalid
	}
	return &Message{ConversationID: conversationID, StudioID: studioID, SenderID: senderID, ClientMessageID: clientID, Kind: kind, Body: strings.TrimSpace(body), ReplyToID: reply, CreatedAt: now}, nil
}

func (m *Message) Edit(actorID, body string, now time.Time) error {
	if actorID != m.SenderID {
		return ErrForbidden
	}
	if now.After(m.CreatedAt.Add(15*time.Minute)) || m.WithdrawnAt != nil {
		return ErrWindowClosed
	}
	if strings.TrimSpace(body) == "" || len([]rune(body)) > 4000 {
		return ErrInvalid
	}
	m.Body = strings.TrimSpace(body)
	m.EditedAt = &now
	return nil
}

func (m *Message) Withdraw(actorID string, moderator bool, now time.Time) error {
	if m.WithdrawnAt != nil {
		return nil
	}
	if actorID != m.SenderID && !moderator {
		return ErrForbidden
	}
	if !moderator && now.After(m.CreatedAt.Add(24*time.Hour)) {
		return ErrWindowClosed
	}
	m.Body = ""
	m.WithdrawnAt = &now
	return nil
}

type Principal struct {
	ID, DisplayName, AvatarURL    string
	Student, Teacher, StudioAdmin bool
	TeacherStudios, AdminStudios  map[string]bool
}

type GroupMembership struct {
	ConversationID, UserID string
	State                  string
	VisibleFromSequence    int64
}

func (m *GroupMembership) Join(lastSequence int64, banned bool) error {
	if banned || m.State == "BANNED" {
		return ErrForbidden
	}
	m.State = "ACTIVE"
	m.VisibleFromSequence = lastSequence + 1
	return nil
}

func (m *GroupMembership) Leave(owner bool) error {
	if owner || m.State != "ACTIVE" {
		return ErrForbidden
	}
	m.State = "LEFT"
	return nil
}

func (m *GroupMembership) Ban(manager bool) error {
	if !manager {
		return ErrForbidden
	}
	m.State = "BANNED"
	return nil
}

func (m *GroupMembership) Unban(manager bool) error {
	if !manager || m.State != "BANNED" {
		return ErrForbidden
	}
	m.State = "LEFT"
	return nil
}

type BlockRelation struct{ BlockerID, BlockedID string }

func NewBlockRelation(blockerID, blockedID string) (BlockRelation, error) {
	if blockerID == "" || blockedID == "" || blockerID == blockedID {
		return BlockRelation{}, ErrInvalid
	}
	return BlockRelation{BlockerID: blockerID, BlockedID: blockedID}, nil
}

func CanStartDirect(a, b Principal) bool {
	return a.ID != b.ID && ((a.Student && b.Teacher) || (a.Teacher && b.Student))
}
