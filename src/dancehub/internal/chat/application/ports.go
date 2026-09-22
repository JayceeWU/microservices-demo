package application

import (
	"context"
	"time"

	"github.com/JayceeWU/microservices-demo/dancehub/internal/appcore"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/chat/domain"
)

type ActorContext = appcore.ActorContext
type AuditContext = appcore.AuditContext

type Page struct {
	Size  int32
	Token string
}

type ConversationView struct {
	Conversation *domain.Conversation
	LastRead     int64
	Joined       bool
}

type Participant struct {
	ConversationID, UserID, Role, State string
	VisibleFrom, LastRead               int64
	JoinedAt                            time.Time
}

type Attachment struct {
	ID, ConversationID, UploaderID, MimeType, SHA256, ObjectKey, CleanObjectKey, Status string
	Kind                                                                                domain.MessageKind
	SizeBytes                                                                           int64
}

type MessageView struct {
	Message     *domain.Message
	Attachments []Attachment
}

type Report struct {
	ID, MessageID, ReporterID, Reason string
	CreatedAt                         time.Time
}

type ConversationRepository interface {
	List(context.Context, string, Page) ([]ConversationView, string, error)
	ListGroups(context.Context, string, string, string, Page) ([]ConversationView, string, error)
	Get(context.Context, string, bool) (*ConversationView, error)
	FindDirect(context.Context, string, string) (*ConversationView, error)
	FindStudioSupport(context.Context, string, string) (*ConversationView, error)
	AddDirect(context.Context, *domain.Conversation, string, string, string) (*ConversationView, error)
	AddStudioSupport(context.Context, *domain.Conversation, string) (*ConversationView, error)
	AddGroup(context.Context, *domain.Conversation, string) (*ConversationView, error)
	DeleteGroup(context.Context, *domain.Conversation) error
	Join(context.Context, *domain.Conversation, string, bool) (Participant, error)
	Leave(context.Context, *domain.Conversation, string, bool) (Participant, error)
	SetMemberState(context.Context, *domain.Conversation, string, string, string, string) (Participant, error)
	CanAccess(context.Context, string, string) (bool, bool, error)
	Participant(context.Context, string, string) (Participant, error)
}

type MessageRepository interface {
	List(context.Context, string, int64, Page) ([]MessageView, string, error)
	Get(context.Context, string, bool) (*MessageView, error)
	Add(context.Context, *domain.Message, []string) (*MessageView, bool, error)
	Save(context.Context, *domain.Message) (*MessageView, error)
	AuditModeration(context.Context, *domain.Message, string, string, string) error
	// MarkRead returns the participant and whether the read receipt actually advanced.
	MarkRead(context.Context, string, string, int64) (Participant, bool, error)
	Report(context.Context, string, string, string) (Report, error)
}

type BlockRepository interface {
	ExistsEitherDirection(context.Context, string, string) (bool, error)
	Set(context.Context, string, string, bool) error
}

type AttachmentRepository interface {
	Add(context.Context, Attachment) (Attachment, error)
	Get(context.Context, string, bool) (Attachment, error)
	MarkPendingScan(context.Context, string) (Attachment, error)
	DeletePending(context.Context, string) error
}

type EventRepository interface {
	AppendRealtime(context.Context, string, string, map[string]any) error
}

type Repositories interface {
	Conversations() ConversationRepository
	Messages() MessageRepository
	Blocks() BlockRepository
	Attachments() AttachmentRepository
	Events() EventRepository
}

type UnitOfWork interface {
	Do(context.Context, ActorContext, func(context.Context, Repositories) error) error
}

type AccountPort interface {
	GetPrincipal(context.Context, ActorContext, string) (domain.Principal, error)
}

type CatalogPort interface {
	RequireStudio(context.Context, ActorContext, string) error
}

type ObjectStore interface {
	PresignUpload(context.Context, string, string, time.Duration) (string, error)
	PresignDownload(context.Context, string, time.Duration) (string, error)
}

type MediaQueue interface {
	RequestScan(context.Context, Attachment) error
}

type TicketStore interface {
	Create(context.Context, ActorContext, time.Duration) (string, error)
	Consume(context.Context, string) (ActorContext, error)
}

type RealtimeBus interface {
	Publish(context.Context, []byte) error
	Subscribe(context.Context) (<-chan []byte, func() error, error)
	Allow(context.Context, string, string, int, time.Duration) (bool, time.Duration, error)
	AcquireUpload(context.Context, string, string, int, time.Duration) (bool, error)
	ReleaseUpload(context.Context, string, string) error
	SetPresence(context.Context, string, time.Duration) error
}

type Clock interface{ Now() time.Time }
type IDGenerator interface{ New() string }

type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now().UTC() }
