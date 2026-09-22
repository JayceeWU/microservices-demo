package application

import (
	"context"
	"testing"
	"time"

	"github.com/JayceeWU/microservices-demo/dancehub/internal/scheduling/domain"
)

const (
	testStudio  = "10000000-0000-0000-0000-000000000001"
	testUser    = "70000000-0000-0000-0000-000000000001"
	testClass   = "40000000-0000-0000-0000-000000000001"
	testBooking = "50000000-0000-0000-0000-000000000001"
)

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

type fakeWork struct {
	repositories  Repositories
	inTransaction bool
}

func (w *fakeWork) Do(ctx context.Context, _ ActorContext, _ string, execute func(context.Context, Repositories) error) error {
	w.inTransaction = true
	defer func() { w.inTransaction = false }()
	return execute(ctx, w.repositories)
}

type fakeRepositories struct {
	sessions *fakeSessions
	bookings *fakeBookings
	outbox   *recordingOutbox
}

func (r fakeRepositories) Sessions() ClassSessionRepository { return r.sessions }
func (r fakeRepositories) Bookings() BookingRepository      { return r.bookings }
func (fakeRepositories) Rooms() RoomReservationRepository   { return unusedRooms{} }
func (r fakeRepositories) Outbox() OutboxPort {
	if r.outbox != nil {
		return r.outbox
	}
	return discardOutbox{}
}

type recordingOutbox struct{ events []DomainEvent }

func (o *recordingOutbox) Append(_ context.Context, event DomainEvent) error {
	o.events = append(o.events, event)
	return nil
}

type fakeSessions struct{ record *SessionRecord }

func (r *fakeSessions) Get(context.Context, domain.ClassSessionID, bool) (*SessionRecord, error) {
	return r.record, nil
}
func (*fakeSessions) Search(context.Context, SearchSessionsQuery) (PageResult[SessionView], error) {
	panic("unused")
}
func (*fakeSessions) Add(context.Context, *domain.ClassSession, string, string) (*SessionRecord, error) {
	panic("unused")
}
func (r *fakeSessions) Save(_ context.Context, value *SessionRecord, _ domain.ClassStatus) (*SessionRecord, error) {
	r.record = value
	return value, nil
}
func (*fakeSessions) UpdateVideo(context.Context, domain.ClassSessionID, string, string, bool, string) (*SessionRecord, error) {
	panic("unused")
}
func (*fakeSessions) DueMinimumIDs(context.Context, int) ([]domain.ClassSessionID, error) {
	panic("unused")
}
func (*fakeSessions) DueEndingIDs(context.Context, int) ([]domain.ClassSessionID, error) {
	panic("unused")
}

type fakeBookings struct{ record *BookingRecord }

func (r *fakeBookings) FindByIdempotencyKey(_ context.Context, studioID, key string) (*BookingRecord, error) {
	if r.record == nil || r.record.StudioID != studioID || r.record.IdempotencyKey != key {
		return nil, NotFound("booking not found")
	}
	return r.record, nil
}

func (r *fakeBookings) HasPendingCredits(context.Context, domain.ClassSessionID) (bool, error) {
	return r.record != nil && (r.record.View().Status == domain.BookingPendingCredit || r.record.View().Status == domain.BookingConfirmed || r.record.CreditCompensationPending), nil
}

func (*fakeBookings) ListByStudent(context.Context, domain.UserID, PageRequest) (PageResult[BookingView], error) {
	panic("unused")
}
func (*fakeBookings) ListBySession(context.Context, domain.ClassSessionID, PageRequest) (PageResult[BookingView], error) {
	panic("unused")
}
func (r *fakeBookings) Get(context.Context, domain.BookingID, bool) (*BookingRecord, error) {
	return r.record, nil
}
func (r *fakeBookings) Add(_ context.Context, studio string, value *domain.Booking, key string) (*BookingRecord, error) {
	snapshot := value.Snapshot()
	snapshot.ID = domain.BookingID(testBooking)
	r.record = &BookingRecord{Aggregate: domain.RehydrateBooking(snapshot), StudioID: studio, IdempotencyKey: key, Created: true}
	return r.record, nil
}
func (r *fakeBookings) Save(_ context.Context, value *BookingRecord, _ domain.BookingStatus) (*BookingRecord, error) {
	r.record = value
	return value, nil
}
func (*fakeBookings) Delete(context.Context, domain.BookingID) error { panic("unused") }
func (*fakeBookings) ListForSession(context.Context, domain.ClassSessionID, bool) ([]*BookingRecord, error) {
	panic("unused")
}
func (*fakeBookings) CountPayrollEligible(context.Context, domain.ClassSessionID) (int, error) {
	panic("unused")
}
func (*fakeBookings) RecordAttendance(context.Context, AttendanceAudit) error { panic("unused") }
func (*fakeBookings) ReconciliationIDs(context.Context, int) ([]domain.BookingID, error) {
	panic("unused")
}

type checkingCredits struct {
	t         *testing.T
	work      *fakeWork
	holdCalls int
}

func (c *checkingCredits) PlaceHold(context.Context, ActorContext, string, string, int32, time.Time, AuditContext) (string, error) {
	if c.work.inTransaction {
		c.t.Fatal("credit hold was called inside the database transaction")
	}
	c.holdCalls++
	return "60000000-0000-0000-0000-000000000001", nil
}
func (*checkingCredits) Capture(context.Context, ActorContext, string, AuditContext) error {
	panic("unused")
}
func (*checkingCredits) Release(context.Context, ActorContext, string, AuditContext) error {
	panic("unused")
}
func (*checkingCredits) Reverse(context.Context, ActorContext, string, AuditContext) error {
	panic("unused")
}

type unusedRooms struct{}

func (unusedRooms) Get(context.Context, domain.RoomReservationID, bool) (*RoomReservationRecord, error) {
	panic("unused")
}
func (unusedRooms) Add(context.Context, *RoomReservationRecord, string) (*RoomReservationRecord, error) {
	panic("unused")
}
func (unusedRooms) Save(context.Context, *RoomReservationRecord, domain.RoomReservationStatus) (*RoomReservationRecord, error) {
	panic("unused")
}
func (unusedRooms) DueExpiredIDs(context.Context, int) ([]domain.RoomReservationID, error) {
	panic("unused")
}

type discardOutbox struct{}

func (discardOutbox) Append(context.Context, DomainEvent) error { return nil }

func TestBookClassCallsCreditOutsideTransactions(t *testing.T) {
	now := time.Date(2026, time.September, 2, 12, 0, 0, 0, time.UTC)
	period, err := domain.RehydrateClassTimeRange(now.Add(8*time.Hour), now.Add(9*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	aggregate, err := domain.RehydrateClassSession(domain.ClassSessionSnapshot{
		ID: domain.ClassSessionID(testClass), StudioID: domain.StudioID(testStudio), RoomID: "20000000-0000-0000-0000-000000000001",
		TeacherID: "70000000-0000-0000-0000-000000000002", Title: "Ballet", TimeRange: period,
		Capacity: 20, Minimum: 4, CreditCost: 4, Status: domain.ClassOpen,
	})
	if err != nil {
		t.Fatal(err)
	}
	sessions, bookings, outbox := &fakeSessions{record: &SessionRecord{Aggregate: aggregate}}, &fakeBookings{}, &recordingOutbox{}
	work := &fakeWork{repositories: fakeRepositories{sessions: sessions, bookings: bookings, outbox: outbox}}
	credits := &checkingCredits{t: t, work: work}
	service := NewService(work, credits, nil, fixedClock{now: now})

	result, err := service.BookClass(context.Background(), BookClassCommand{
		Actor:     ActorContext{UserID: testUser, StudioID: testStudio, TenantRoles: []string{"student"}, ActorKind: "human"},
		SessionID: testClass, Audit: AuditContext{IdempotencyKey: "book-test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != domain.BookingConfirmed || credits.holdCalls != 1 {
		t.Fatalf("expected one confirmed hold, got status=%s calls=%d", result.Status, credits.holdCalls)
	}
	if sessions.record.Aggregate.Snapshot().Confirmed != 1 {
		t.Fatal("seat count was not committed")
	}
	// The projection's bookings metric depends on this event; it carries the studio as the
	// tenant and the class the consumer requires.
	if len(outbox.events) != 1 || outbox.events[0].Type != "BookingCreated" || outbox.events[0].StudioID != testStudio || outbox.events[0].Payload["class_session_id"] != testClass {
		t.Fatalf("expected one BookingCreated event for the studio and class, got %+v", outbox.events)
	}
}
