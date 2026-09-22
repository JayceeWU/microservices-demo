package application

import (
	"context"
	"time"

	"github.com/JayceeWU/microservices-demo/dancehub/internal/account/domain"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/appcore"
)

var (
	ErrUnauthenticated  = appcore.ErrUnauthenticated
	ErrPermissionDenied = appcore.ErrPermissionDenied
	ErrInvalidArgument  = appcore.ErrInvalidArgument
	ErrNotFound         = appcore.ErrNotFound
	ErrConflict         = appcore.ErrConflict
	ErrAlreadyExists    = appcore.ErrAlreadyExists
	ErrUnavailable      = appcore.ErrUnavailable
	Invalid             = appcore.Invalid
	Denied              = appcore.Denied
	NotFound            = appcore.NotFound
	Conflict            = appcore.Conflict
	AlreadyExists       = appcore.AlreadyExists
	Unavailable         = appcore.Unavailable
	NewAuditContext     = appcore.NewAuditContext
)

type ActorContext = appcore.ActorContext
type AuditContext = appcore.AuditContext
type PageRequest = appcore.PageRequest

type Clock interface{ Now() time.Time }
type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now().UTC() }

// UnitOfWork creates transaction-scoped repositories after applying the RLS
// identity with SET LOCAL. No remote port may be called from inside Do.
type UnitOfWork interface {
	Do(context.Context, ActorContext, string, func(context.Context, Repositories) error) error
}

type Repositories interface {
	Profiles() ProfileRepository
	Memberships() MembershipRepository
	GlobalRoles() GlobalRoleRepository
	Invitations() InvitationRepository
	Chat() ChatRepository
	Identity() IdentityRepository
}

// ProfileView, MembershipView, TeacherView, InvitationView, and
// GlobalRoleAssignmentView are the read-shapes handed back to transport.
// They are plain data, distinct from the domain aggregates that enforce
// invariants over the same underlying rows.
type ProfileView struct {
	UserID, Email, DisplayName, AvatarURL, Timezone, Bio, PortfolioURL string
}

type MembershipView struct {
	ID, StudioID, UserID string
	Role                 domain.Role
	Active               bool
}

type TeacherView struct {
	UserID, StudioID, DisplayName, AvatarURL, Bio, PortfolioURL string
}

type InvitationView struct {
	ID, StudioID, Email string
	Role                domain.Role
	Status              string
	ExpiresAt           time.Time
}

type PlatformUserView struct {
	UserID, Email, DisplayName string
}

type GlobalRoleAssignmentView struct {
	ID, UserID, UserEmail, UserDisplayName string
	Role                                   domain.GlobalRole
	Active                                 bool
	GrantedAt, RevokedAt                   time.Time
}

type ChatPrincipalView struct {
	Profile     ProfileView
	Memberships []MembershipView
}

type ResolvedIdentityView struct {
	Profile     ProfileView
	Memberships []MembershipView
	GlobalRoles []domain.GlobalRole
}

type StudioMembershipsView struct {
	Memberships []MembershipView
	GlobalRoles []domain.GlobalRole
}

// ProfileRecord pairs the Profile aggregate with the profile's user ID so
// repositories can save partial mutations without re-deriving it.
type ProfileRecord struct {
	Aggregate *domain.Profile
}

func (r ProfileRecord) View() ProfileView {
	v := r.Aggregate.Snapshot()
	return ProfileView{
		UserID: v.UserID, Email: v.Email, DisplayName: v.DisplayName,
		AvatarURL: v.AvatarURL, Timezone: v.Timezone, Bio: v.Bio, PortfolioURL: v.PortfolioURL,
	}
}

// GlobalRoleAssignmentRecord pairs the aggregate with the denormalized user
// fields needed to render a view without a second round trip.
type GlobalRoleAssignmentRecord struct {
	Aggregate                  *domain.GlobalRoleAssignment
	UserEmail, UserDisplayName string
}

func (r GlobalRoleAssignmentRecord) View() GlobalRoleAssignmentView {
	v := r.Aggregate.Snapshot()
	return GlobalRoleAssignmentView{
		ID: v.ID, UserID: v.UserID, UserEmail: r.UserEmail, UserDisplayName: r.UserDisplayName,
		Role: v.Role, Active: v.Active, GrantedAt: v.GrantedAt, RevokedAt: v.RevokedAt,
	}
}

type GlobalRoleAudit struct {
	AssignmentID, ActorID, TargetUserID string
	Role                                domain.GlobalRole
	Action                              string
	Reason, IdempotencyKey              string
}

type ProfileRepository interface {
	GetByUserID(context.Context, string) (*ProfileRecord, error)
	Update(context.Context, string, *domain.Profile) (*ProfileRecord, error)
	SearchTeachers(context.Context, string, string, int32) ([]TeacherView, error)
	FindByEmail(context.Context, string) (PlatformUserView, error)
}

type MembershipRepository interface {
	ListActiveByUser(context.Context, string) ([]MembershipView, error)
}

type GlobalRoleRepository interface {
	ListActiveByUser(context.Context, string) ([]domain.GlobalRole, error)
	ListActive(context.Context, int32) ([]GlobalRoleAssignmentView, error)
	// BeginMutation serializes concurrent grant/revoke calls with an
	// advisory lock and reports whether idempotencyKey was already used by
	// actorID, returning the assignment ID it produced if so.
	BeginMutation(ctx context.Context, actorID, idempotencyKey string) (assignmentID string, repeated bool, err error)
	Get(context.Context, string) (*GlobalRoleAssignmentRecord, error)
	GetForUpdate(context.Context, string) (*GlobalRoleAssignmentRecord, error)
	FindByUserAndRole(context.Context, string, domain.GlobalRole) (*GlobalRoleAssignmentRecord, error)
	CountActiveByRole(context.Context, domain.GlobalRole) (int, error)
	Insert(context.Context, *domain.GlobalRoleAssignment) (*GlobalRoleAssignmentRecord, error)
	Save(context.Context, *GlobalRoleAssignmentRecord) (*GlobalRoleAssignmentRecord, error)
	RecordAudit(context.Context, GlobalRoleAudit) error
}

type InvitationRepository interface {
	Create(ctx context.Context, studioID, email, role, invitedBy, reason, idempotencyKey string) (InvitationView, error)
}

type ChatRepository interface {
	GetPrincipal(context.Context, string) (ChatPrincipalView, error)
}

// IdentityRepository resolves the platform's opaque OIDC subject into an
// internal user ID. It is queried anonymously (outside any resolved actor
// identity) which is why it is not folded into ProfileRepository.
type IdentityRepository interface {
	ResolveOIDCSubject(context.Context, string) (string, error)
}
