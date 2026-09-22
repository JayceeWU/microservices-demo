package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/JayceeWU/microservices-demo/dancehub/internal/appcore"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/scheduling/domain"
)

var (
	ErrUnauthenticated  = appcore.ErrUnauthenticated
	ErrPermissionDenied = appcore.ErrPermissionDenied
	ErrInvalidArgument  = appcore.ErrInvalidArgument
	ErrNotFound         = appcore.ErrNotFound
	ErrConflict         = appcore.ErrConflict
	ErrCapacity         = errors.New("capacity exceeded")
	ErrUnavailable      = appcore.ErrUnavailable
	Invalid             = appcore.Invalid
	Denied              = appcore.Denied
	NotFound            = appcore.NotFound
	Conflict            = appcore.Conflict
	Unavailable         = appcore.Unavailable
	NewAuditContext     = appcore.NewAuditContext
)

func Capacity(message string) error { return fmt.Errorf("%w: %s", ErrCapacity, message) }

type ActorContext = appcore.ActorContext
type AuditContext = appcore.AuditContext
type PageRequest = appcore.PageRequest
type SearchSessionsQuery struct {
	StudioID, TeacherID string
	Page                PageRequest
	BookableOnly        *bool
	ViewerID            string
	TenantID            string
}

type PageResult[T any] struct {
	Items         []T
	NextPageToken string
}

type SessionView struct {
	ID, StudioID, RoomID, TeacherID, Title, Description   string
	StartsAt, EndsAt                                      time.Time
	Capacity, MinimumStudents, CreditCost, ConfirmedCount int32
	Status                                                domain.ClassStatus
	VideoURL                                              string
}

type BookingView struct {
	ID, StudioID, ClassSessionID, StudentID string
	Status                                  domain.BookingStatus
	WalkIn                                  bool
	CreditCompensationPending               bool
}

type RoomReservationView struct {
	ID, StudioID, RoomID, StudentID string
	StartsAt, EndsAt, HoldExpiresAt time.Time
	AmountCents                     int64
	Status                          domain.RoomReservationStatus
}

type SessionRecord struct {
	Aggregate                      *domain.ClassSession
	Description, VideoURL          string
	TeacherDeadline, AdminDeadline time.Time
	StudioTimezone                 string
	PayrollFactVersion             int64
}

func (r SessionRecord) View() SessionView {
	s := r.Aggregate.Snapshot()
	return SessionView{
		ID: string(s.ID), StudioID: string(s.StudioID), RoomID: s.RoomID,
		TeacherID: string(s.TeacherID), Title: s.Title, Description: r.Description,
		StartsAt: s.TimeRange.StartsAt(), EndsAt: s.TimeRange.EndsAt(), Capacity: int32(s.Capacity),
		MinimumStudents: int32(s.Minimum), CreditCost: int32(s.CreditCost), ConfirmedCount: s.Confirmed,
		Status: s.Status, VideoURL: r.VideoURL,
	}
}

type BookingRecord struct {
	Aggregate                 *domain.Booking
	StudioID                  string
	IdempotencyKey            string
	Created                   bool
	CreditCompensationPending bool
}

func (r BookingRecord) View() BookingView {
	b := r.Aggregate.Snapshot()
	return BookingView{
		ID: string(b.ID), StudioID: r.StudioID, ClassSessionID: string(b.SessionID),
		StudentID: string(b.StudentID), Status: b.Status, WalkIn: b.WalkIn, CreditCompensationPending: r.CreditCompensationPending,
	}
}

type RoomReservationRecord struct {
	Aggregate                   *domain.RoomReservation
	StudioID, RoomID, StudentID string
	AmountCents                 int64
}

func (r RoomReservationRecord) View() RoomReservationView {
	v := r.Aggregate.Snapshot()
	return RoomReservationView{
		ID: string(v.ID), StudioID: r.StudioID, RoomID: r.RoomID, StudentID: r.StudentID,
		StartsAt: v.TimeRange.StartsAt(), EndsAt: v.TimeRange.EndsAt(), AmountCents: r.AmountCents,
		Status: v.Status, HoldExpiresAt: v.HoldExpiresAt,
	}
}

type AttendanceAudit struct {
	StudioID, SessionID, BookingID, StudentID, ActorID string
	Action, Reason, BeforeStatus, AfterStatus          string
}

type DomainEvent struct {
	Type, AggregateType, AggregateID, StudioID string
	Payload                                    map[string]any
}

type Clock interface{ Now() time.Time }
type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now().UTC() }

// UnitOfWork creates transaction-scoped repositories after applying the RLS
// identity with SET LOCAL. No remote port may be called from inside Do.
type UnitOfWork interface {
	Do(context.Context, ActorContext, string, func(context.Context, Repositories) error) error
}

type Repositories interface {
	Sessions() ClassSessionRepository
	Bookings() BookingRepository
	Rooms() RoomReservationRepository
	Outbox() OutboxPort
}

type ClassSessionRepository interface {
	Search(context.Context, SearchSessionsQuery) (PageResult[SessionView], error)
	Get(context.Context, domain.ClassSessionID, bool) (*SessionRecord, error)
	Add(context.Context, *domain.ClassSession, string, string) (*SessionRecord, error)
	Save(context.Context, *SessionRecord, domain.ClassStatus) (*SessionRecord, error)
	UpdateVideo(context.Context, domain.ClassSessionID, string, string, bool, string) (*SessionRecord, error)
	DueMinimumIDs(context.Context, int) ([]domain.ClassSessionID, error)
	DueEndingIDs(context.Context, int) ([]domain.ClassSessionID, error)
}

type BookingRepository interface {
	ListByStudent(context.Context, domain.UserID, PageRequest) (PageResult[BookingView], error)
	ListBySession(context.Context, domain.ClassSessionID, PageRequest) (PageResult[BookingView], error)
	Get(context.Context, domain.BookingID, bool) (*BookingRecord, error)
	FindByIdempotencyKey(context.Context, string, string) (*BookingRecord, error)
	Add(context.Context, string, *domain.Booking, string) (*BookingRecord, error)
	Save(context.Context, *BookingRecord, domain.BookingStatus) (*BookingRecord, error)
	Delete(context.Context, domain.BookingID) error
	ListForSession(context.Context, domain.ClassSessionID, bool) ([]*BookingRecord, error)
	CountPayrollEligible(context.Context, domain.ClassSessionID) (int, error)
	HasPendingCredits(context.Context, domain.ClassSessionID) (bool, error)
	RecordAttendance(context.Context, AttendanceAudit) error
	ReconciliationIDs(context.Context, int) ([]domain.BookingID, error)
}

type RoomReservationRepository interface {
	Get(context.Context, domain.RoomReservationID, bool) (*RoomReservationRecord, error)
	Add(context.Context, *RoomReservationRecord, string) (*RoomReservationRecord, error)
	Save(context.Context, *RoomReservationRecord, domain.RoomReservationStatus) (*RoomReservationRecord, error)
	DueExpiredIDs(context.Context, int) ([]domain.RoomReservationID, error)
}

type OutboxPort interface {
	Append(context.Context, DomainEvent) error
}

type CreditPort interface {
	PlaceHold(context.Context, ActorContext, string, string, int32, time.Time, AuditContext) (string, error)
	Capture(context.Context, ActorContext, string, AuditContext) error
	Release(context.Context, ActorContext, string, AuditContext) error
	Reverse(context.Context, ActorContext, string, AuditContext) error
}

type RoomSnapshot struct {
	ID, StudioID           string
	RentalRateCentsPerHour int64
}
type CatalogPort interface {
	GetRoom(context.Context, ActorContext, string, string) (RoomSnapshot, error)
	GetStudioTimezone(context.Context, ActorContext, string) (string, error)
}
