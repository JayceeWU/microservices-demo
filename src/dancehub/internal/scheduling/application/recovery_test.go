package application

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/JayceeWU/microservices-demo/dancehub/internal/scheduling/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// This transaction fake rolls back local state while leaving remote credit
// effects intact, reproducing the failure window that durable commands repair.
type recoveryState struct {
	session    *SessionRecord
	bookings   map[string]*BookingRecord
	operations map[string]*AttendanceOperation
	audits     []AttendanceAudit
	events     []DomainEvent
	next       int
}

func (s recoveryState) copy() recoveryState {
	result := s
	session := *s.session
	session.Aggregate, _ = domain.RehydrateClassSession(s.session.Aggregate.Snapshot())
	result.session = &session
	result.bookings = make(map[string]*BookingRecord)
	for key, value := range s.bookings {
		copy := *value
		copy.Aggregate = domain.RehydrateBooking(value.Aggregate.Snapshot())
		result.bookings[key] = &copy
	}
	result.operations = make(map[string]*AttendanceOperation)
	for key, value := range s.operations {
		copy := *value
		result.operations[key] = &copy
	}
	result.audits = append([]AttendanceAudit(nil), s.audits...)
	result.events = append([]DomainEvent(nil), s.events...)
	return result
}

type recoveryWork struct {
	t                                         *testing.T
	state                                     recoveryState
	inTransaction, classLocked, bookingLocked bool
	failNextEvent                             bool
	actor                                     ActorContext
}

func (w *recoveryWork) Do(ctx context.Context, actor ActorContext, _ string, execute func(context.Context, Repositories) error) error {
	w.t.Helper()
	if w.inTransaction {
		w.t.Fatal("nested transaction")
	}
	before := w.state.copy()
	w.inTransaction, w.classLocked, w.bookingLocked = true, false, false
	w.actor = actor
	err := execute(ctx, recoveryRepositories{w: w})
	w.inTransaction = false
	if err != nil {
		w.state = before
	}
	return err
}

type recoveryRepositories struct{ w *recoveryWork }

func (r recoveryRepositories) Sessions() ClassSessionRepository { return recoverySessions{w: r.w} }
func (r recoveryRepositories) Bookings() BookingRepository      { return recoveryBookings{w: r.w} }
func (r recoveryRepositories) Rooms() RoomReservationRepository { return unusedRooms{} }
func (r recoveryRepositories) Outbox() OutboxPort               { return recoveryOutbox{r.w} }
func (r recoveryRepositories) AttendanceOperations() AttendanceOperationRepository {
	return recoveryOperations{r.w}
}
func (r recoveryRepositories) Compensations() CompensationRepository {
	return recoveryCompensations{w: r.w}
}

type recoverySessions struct {
	ClassSessionRepository
	w *recoveryWork
}

func (r recoverySessions) Get(_ context.Context, id domain.ClassSessionID, lock bool) (*SessionRecord, error) {
	if string(id) != r.w.state.session.View().ID {
		return nil, NotFound("class not found")
	}
	if lock {
		r.w.classLocked = true
	}
	return r.w.state.session, nil
}
func (r recoverySessions) Save(_ context.Context, value *SessionRecord, _ domain.ClassStatus) (*SessionRecord, error) {
	if !r.w.classLocked {
		r.w.t.Fatal("class write without class lock")
	}
	r.w.state.session = value
	return value, nil
}
func (r recoverySessions) Add(_ context.Context, value *domain.ClassSession, description, timezone string) (*SessionRecord, error) {
	r.w.state.session = &SessionRecord{Aggregate: value, Description: description, StudioTimezone: timezone}
	return r.w.state.session, nil
}

type recoveryBookings struct {
	BookingRepository
	w *recoveryWork
}

func (r recoveryBookings) Get(_ context.Context, id domain.BookingID, lock bool) (*BookingRecord, error) {
	v := r.w.state.bookings[string(id)]
	if v == nil {
		return nil, NotFound("booking not found")
	}
	// PostgreSQL row locks require UPDATE visibility in addition to SELECT.
	// The teacher roster policy deliberately grants only SELECT access.
	if lock && r.w.actor.ActorKind == "human" && r.w.actor.HasTenantRole("teacher") && !r.w.actor.HasTenantRole("studio_admin") && !r.w.actor.IsPlatformAdmin() && r.w.actor.UserID != v.View().StudentID {
		return nil, NotFound("booking not found")
	}
	if lock {
		if !r.w.classLocked {
			r.w.t.Fatal("booking lock taken before class lock")
		}
		r.w.bookingLocked = true
	}
	return v, nil
}
func (r recoveryBookings) FindByIdempotencyKey(_ context.Context, studio, key string) (*BookingRecord, error) {
	for _, value := range r.w.state.bookings {
		if value.StudioID == studio && value.IdempotencyKey == key {
			return value, nil
		}
	}
	return nil, NotFound("booking not found")
}
func (r recoveryBookings) Add(_ context.Context, studio string, value *domain.Booking, key string) (*BookingRecord, error) {
	if !r.w.classLocked {
		r.w.t.Fatal("booking created without class lock")
	}
	r.w.state.next++
	snapshot := value.Snapshot()
	snapshot.ID = domain.BookingID(fmt.Sprintf("50000000-0000-0000-0000-%012d", r.w.state.next))
	record := &BookingRecord{Aggregate: domain.RehydrateBooking(snapshot), StudioID: studio, IdempotencyKey: key, Created: true}
	r.w.state.bookings[string(snapshot.ID)] = record
	return record, nil
}
func (r recoveryBookings) Save(_ context.Context, value *BookingRecord, _ domain.BookingStatus) (*BookingRecord, error) {
	if !r.w.classLocked || !r.w.bookingLocked {
		r.w.t.Fatal("booking write without class then booking locks")
	}
	r.w.state.bookings[value.View().ID] = value
	return value, nil
}
func (r recoveryBookings) CountPayrollEligible(context.Context, domain.ClassSessionID) (int, error) {
	count := 0
	for _, value := range r.w.state.bookings {
		if payrollEligible(value.View().Status) {
			count++
		}
	}
	return count, nil
}
func (r recoveryBookings) HasPendingCredits(context.Context, domain.ClassSessionID) (bool, error) {
	for _, value := range r.w.state.bookings {
		if value.View().Status == domain.BookingPendingCredit || value.View().Status == domain.BookingConfirmed || value.CreditCompensationPending {
			return true, nil
		}
	}
	for _, value := range r.w.state.operations {
		if value.Phase == "PENDING" {
			return true, nil
		}
	}
	return false, nil
}
func (r recoveryBookings) RecordAttendance(_ context.Context, value AttendanceAudit) error {
	r.w.state.audits = append(r.w.state.audits, value)
	return nil
}

type recoveryOutbox struct{ w *recoveryWork }

func (r recoveryOutbox) Append(_ context.Context, value DomainEvent) error {
	if r.w.failNextEvent {
		r.w.failNextEvent = false
		return errors.New("database connection failed before outbox commit")
	}
	r.w.state.events = append(r.w.state.events, value)
	return nil
}

type recoveryOperations struct{ w *recoveryWork }

func (r recoveryOperations) FindByKey(_ context.Context, studio, key string) (*AttendanceOperation, error) {
	for _, value := range r.w.state.operations {
		if value.StudioID == studio && value.IdempotencyKey == key {
			copy := *value
			return &copy, nil
		}
	}
	return nil, nil
}
func (r recoveryOperations) Add(_ context.Context, value AttendanceOperation) (*AttendanceOperation, error) {
	for _, existing := range r.w.state.operations {
		if existing.BookingID == value.BookingID && existing.Phase == "PENDING" {
			return nil, Conflict("another attendance operation is pending")
		}
	}
	value.ID = fmt.Sprintf("operation-%d", len(r.w.state.operations)+1)
	r.w.state.operations[value.ID] = &value
	copy := value
	return &copy, nil
}
func (r recoveryOperations) Claim(_ context.Context, id string) (*AttendanceOperation, error) {
	for _, value := range r.w.state.operations {
		if (id == "" || value.ID == id) && value.Phase == "PENDING" && value.LeaseToken == "" {
			value.Attempts++
			value.LeaseToken = fmt.Sprintf("lease-%d", value.Attempts)
			copy := *value
			return &copy, nil
		}
	}
	return nil, nil
}
func (r recoveryOperations) OwnsClaim(_ context.Context, value *AttendanceOperation) (bool, error) {
	stored := r.w.state.operations[value.ID]
	return stored != nil && stored.Phase == "PENDING" && stored.LeaseToken == value.LeaseToken, nil
}
func (r recoveryOperations) Finish(_ context.Context, value *AttendanceOperation, phase, message string) error {
	stored := r.w.state.operations[value.ID]
	if stored.LeaseToken != value.LeaseToken || stored.Phase != "PENDING" {
		return Unavailable("lease changed", nil)
	}
	stored.Phase, stored.LastError, stored.LeaseToken = phase, message, ""
	return nil
}

type recoveryCompensations struct {
	CompensationRepository
	w *recoveryWork
}

func (r recoveryCompensations) Enqueue(_ context.Context, value *BookingRecord, _ string) error {
	value.CreditCompensationPending = true
	return nil
}

type recoveryCredits struct {
	w                                                          *recoveryWork
	holdCalls, captureCalls, reverseCalls, captures, reversals int
	captured, reversed                                         bool
	holdError, captureError, reverseError                      error
}

func (c *recoveryCredits) outside() {
	if c.w.inTransaction {
		c.w.t.Fatal("remote credit call inside transaction")
	}
}
func (c *recoveryCredits) PlaceHold(context.Context, ActorContext, string, string, int32, time.Time, AuditContext) (string, error) {
	c.outside()
	c.holdCalls++
	if c.holdError != nil {
		return "", c.holdError
	}
	return "60000000-0000-0000-0000-000000000001", nil
}
func (c *recoveryCredits) Capture(context.Context, ActorContext, string, AuditContext) error {
	c.outside()
	c.captureCalls++
	if !c.captured {
		c.captured = true
		c.captures++
	}
	err := c.captureError
	c.captureError = nil
	return err
}
func (c *recoveryCredits) Reverse(context.Context, ActorContext, string, AuditContext) error {
	c.outside()
	c.reverseCalls++
	if !c.reversed {
		c.reversed = true
		c.reversals++
	}
	err := c.reverseError
	c.reverseError = nil
	return err
}
func (c *recoveryCredits) Release(context.Context, ActorContext, string, AuditContext) error {
	panic("unused")
}

func recoveryService(t *testing.T, state domain.ClassStatus) (*Service, *recoveryWork, *recoveryCredits) {
	t.Helper()
	now := time.Date(2026, time.September, 1, 7, 30, 0, 0, time.UTC)
	period, _ := domain.RehydrateClassTimeRange(now.Add(-90*time.Minute), now.Add(-30*time.Minute))
	aggregate, err := domain.RehydrateClassSession(domain.ClassSessionSnapshot{ID: domain.ClassSessionID(testClass), StudioID: domain.StudioID(testStudio), RoomID: "20000000-0000-0000-0000-000000000001", TeacherID: domain.UserID(testUser), Title: "Ballet", TimeRange: period, Capacity: 20, Minimum: 4, CreditCost: 4, Confirmed: 1, Status: state})
	if err != nil {
		t.Fatal(err)
	}
	version := int64(0)
	if state == domain.ClassCompleted {
		version = 1
	}
	w := &recoveryWork{t: t, state: recoveryState{session: &SessionRecord{Aggregate: aggregate, StudioTimezone: "America/Los_Angeles", PayrollFactVersion: version, TeacherDeadline: period.EndsAt().Add(4 * time.Hour), AdminDeadline: period.EndsAt().Add(24 * time.Hour)}, bookings: map[string]*BookingRecord{}, operations: map[string]*AttendanceOperation{}}}
	c := &recoveryCredits{w: w}
	return NewService(w, c, nil, fixedClock{now: now}), w, c
}

func recoveryAdmin() ActorContext {
	return ActorContext{UserID: "70000000-0000-0000-0000-000000000003", StudioID: testStudio, TenantRoles: []string{"studio_admin"}, ActorKind: "human"}
}
func recoveryWalkIn() AddWalkInCommand {
	return AddWalkInCommand{Actor: recoveryAdmin(), SessionID: testClass, StudentID: testUser, Audit: AuditContext{IdempotencyKey: "walk-in-1", Reason: "front desk correction"}}
}
func addRecoveryBooking(w *recoveryWork, state domain.BookingStatus) {
	w.state.bookings[testBooking] = &BookingRecord{Aggregate: domain.RehydrateBooking(domain.BookingSnapshot{ID: domain.BookingID(testBooking), SessionID: domain.ClassSessionID(testClass), StudentID: domain.UserID(testUser), Status: state, HoldID: "60000000-0000-0000-0000-000000000001"}), StudioID: testStudio, IdempotencyKey: "book-1"}
	w.state.next = 1
}
func payrollEvents(w *recoveryWork) []DomainEvent {
	var events []DomainEvent
	for _, event := range w.state.events {
		if event.Type == "ClassCompleted" {
			events = append(events, event)
		}
	}
	return events
}

func TestCompleteClassBlocksUnsettledCreditsAndAttendance(t *testing.T) {
	for _, pending := range []string{"hold", "capture", "compensation", "attendance"} {
		t.Run(pending, func(t *testing.T) {
			s, w, _ := recoveryService(t, domain.ClassAwaitingAdminConfirmation)
			addRecoveryBooking(w, domain.BookingCharged)
			switch pending {
			case "hold":
				addRecoveryBooking(w, domain.BookingPendingCredit)
			case "capture":
				addRecoveryBooking(w, domain.BookingConfirmed)
			case "compensation":
				w.state.bookings[testBooking].CreditCompensationPending = true
			case "attendance":
				w.state.operations["pending"] = &AttendanceOperation{Phase: "PENDING"}
			}
			_, err := s.CompleteClass(context.Background(), CompleteClassCommand{Actor: recoveryAdmin(), SessionID: testClass})
			if !errors.Is(err, ErrConflict) || w.state.session.View().Status != domain.ClassAwaitingAdminConfirmation || len(w.state.events) != 0 {
				t.Fatalf("unsettled class completed: error=%v state=%+v", err, w.state)
			}
		})
	}
}

func TestTeacherAttendanceRequiresOnlyRosterReadBeforeWorkerMutation(t *testing.T) {
	for _, attended := range []bool{true, false} {
		t.Run(fmt.Sprintf("attended=%t", attended), func(t *testing.T) {
			s, w, credits := recoveryService(t, domain.ClassCompleted)
			addRecoveryBooking(w, domain.BookingCharged)
			snapshot := w.state.bookings[testBooking].Aggregate.Snapshot()
			snapshot.StudentID = "30000000-0000-0000-0000-000000000099"
			w.state.bookings[testBooking].Aggregate = domain.RehydrateBooking(snapshot)
			actor := ActorContext{UserID: testUser, StudioID: testStudio, TenantRoles: []string{"teacher"}, ActorKind: "human"}
			result, err := s.CorrectAttendance(context.Background(), CorrectAttendanceCommand{Actor: actor, BookingID: testBooking, Attended: attended, Audit: AuditContext{IdempotencyKey: "teacher-correction", Reason: "roster correction"}})
			want := domain.BookingNoShow
			if attended {
				want = domain.BookingAttended
			}
			if err != nil || result.Status != want {
				t.Fatalf("teacher attendance result=%+v error=%v", result, err)
			}
			if len(w.state.audits) != 1 || w.state.audits[0].ActorID != testUser || w.state.operations["operation-1"].Phase != "DONE" {
				t.Fatalf("teacher audit or durable command missing: %+v", w.state)
			}
			if credits.captureCalls != 0 || credits.reverseCalls != 0 || len(payrollEvents(w)) != 0 {
				t.Fatal("attendance-only correction changed the credit or payroll amount")
			}
		})
	}
}

func TestCompleteClassPublishesStudioMonthAndDoesNotRepublishOnRetry(t *testing.T) {
	s, w, _ := recoveryService(t, domain.ClassAwaitingAdminConfirmation)
	addRecoveryBooking(w, domain.BookingNoShow)
	command := CompleteClassCommand{Actor: recoveryAdmin(), SessionID: testClass}
	if _, err := s.CompleteClass(context.Background(), command); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CompleteClass(context.Background(), command); err != nil {
		t.Fatal(err)
	}
	events := payrollEvents(w)
	if len(events) != 1 {
		t.Fatalf("wanted one completion event, got %d", len(events))
	}
	e := events[0]
	if e.AggregateID != testClass || e.StudioID != testStudio || e.Payload["class_session_id"] != testClass || e.Payload["local_month"] != "2026-08-01" || e.Payload["fact_version"] != int64(1) || e.Payload["redemption_count"] != 1 || e.Payload["approved_duration_minutes"] != 60 {
		t.Fatalf("invalid payroll fact: %+v", e)
	}
}

func TestWalkInRecoversLostCaptureResponseAfterCorrectionWindow(t *testing.T) {
	s, w, c := recoveryService(t, domain.ClassCompleted)
	c.captureError = fmt.Errorf("wrapped transport: %w", status.Error(codes.DeadlineExceeded, "response lost after commit"))
	command := recoveryWalkIn()
	if _, err := s.AddWalkIn(context.Background(), command); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("wanted pending, got %v", err)
	}
	if len(w.state.bookings) != 1 || w.state.bookings[testBooking].View().Status != domain.BookingPendingCredit || len(w.state.audits) != 0 {
		t.Fatal("uncertain capture removed booking or committed attendance")
	}
	s.Clock = fixedClock{now: s.Clock.Now().Add(48 * time.Hour)}
	result, err := s.AddWalkIn(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != domain.BookingAttended || c.captures != 1 || len(w.state.audits) != 1 || w.state.audits[0].ActorID != command.Actor.UserID || w.state.audits[0].Reason != command.Audit.Reason {
		t.Fatalf("recovery did not commit exactly once: result=%+v credits=%+v audits=%+v", result, c, w.state.audits)
	}
	if _, err = s.AddWalkIn(context.Background(), command); err != nil {
		t.Fatal(err)
	}
	events := payrollEvents(w)
	if c.captureCalls != 2 || len(w.state.audits) != 1 || len(events) != 1 || events[0].Payload["fact_version"] != int64(2) || events[0].Payload["redemption_count"] != 1 {
		t.Fatalf("duplicate replay changed facts: credits=%+v events=%+v", c, events)
	}
}

func TestWalkInRecoversDatabaseFailureAfterCreditCapture(t *testing.T) {
	s, w, c := recoveryService(t, domain.ClassCompleted)
	w.failNextEvent = true
	if _, err := s.AddWalkIn(context.Background(), recoveryWalkIn()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("accepted command must report pending recovery after database failure, got %v", err)
	}
	if c.captures != 1 || w.state.bookings[testBooking].View().Status != domain.BookingPendingCredit || len(w.state.audits) != 0 {
		t.Fatal("database transaction did not roll back independently of credits")
	}
	for _, operation := range w.state.operations {
		operation.LeaseToken = ""
	}
	if err := (Worker{Service: s}).attendanceOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c.captures != 1 || w.state.bookings[testBooking].View().Status != domain.BookingAttended || len(w.state.audits) != 1 || len(payrollEvents(w)) != 1 {
		t.Fatal("durable worker did not complete the original booking exactly once")
	}
}

func TestReverseRecoversLostResponseAndUpdatesCompletedPayroll(t *testing.T) {
	s, w, c := recoveryService(t, domain.ClassCompleted)
	addRecoveryBooking(w, domain.BookingAttended)
	c.reverseError = status.Error(codes.Unavailable, "response lost")
	command := CorrectAttendanceCommand{Actor: recoveryAdmin(), BookingID: testBooking, Reverse: true, Audit: AuditContext{IdempotencyKey: "reverse-1", Reason: "duplicate redemption"}}
	if _, err := s.CorrectAttendance(context.Background(), command); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("wanted pending, got %v", err)
	}
	if w.state.bookings[testBooking].View().Status != domain.BookingAttended || len(w.state.audits) != 0 {
		t.Fatal("unknown reverse was reported complete")
	}
	if err := (Worker{Service: s}).attendanceOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CorrectAttendance(context.Background(), command); err != nil {
		t.Fatal(err)
	}
	events := payrollEvents(w)
	if c.reversals != 1 || c.reverseCalls != 2 || len(w.state.audits) != 1 || w.state.bookings[testBooking].View().Status != domain.BookingReversed || len(events) != 1 || events[0].Payload["redemption_count"] != 0 || events[0].Payload["fact_version"] != int64(2) {
		t.Fatalf("reverse recovery duplicated or lost work: credits=%+v events=%+v", c, events)
	}
}

func TestAttendanceRejectsReusedKeyWithDifferentPayload(t *testing.T) {
	s, _, c := recoveryService(t, domain.ClassCompleted)
	command := recoveryWalkIn()
	if _, err := s.AddWalkIn(context.Background(), command); err != nil {
		t.Fatal(err)
	}
	for _, change := range []string{"student", "reason", "actor"} {
		changed := command
		switch change {
		case "student":
			changed.StudentID = "70000000-0000-0000-0000-000000000002"
		case "reason":
			changed.Audit.Reason = "another reason"
		case "actor":
			changed.Actor.UserID = "70000000-0000-0000-0000-000000000004"
		}
		if _, err := s.AddWalkIn(context.Background(), changed); !errors.Is(err, ErrConflict) {
			t.Fatalf("%s mismatch accepted: %v", change, err)
		}
	}
	if c.captureCalls != 1 {
		t.Fatal("reused key caused another remote command")
	}
}

func TestPermanentWalkInFailureKeepsBookingAndEnqueuesCompensation(t *testing.T) {
	s, w, c := recoveryService(t, domain.ClassCompleted)
	c.holdError = fmt.Errorf("credit hold failed: %w", status.Error(codes.FailedPrecondition, "insufficient credits"))
	if _, err := s.AddWalkIn(context.Background(), recoveryWalkIn()); !errors.Is(err, ErrConflict) {
		t.Fatalf("wanted rejection, got %v", err)
	}
	booking := w.state.bookings[testBooking]
	if booking == nil || booking.View().Status != domain.BookingCancelled || !booking.CreditCompensationPending || len(w.state.audits) != 0 || len(w.state.events) != 0 {
		t.Fatalf("failed command lost cleanup anchor: %+v", w.state)
	}
	for _, op := range w.state.operations {
		if op.Phase != "FAILED" {
			t.Fatal("permanent failure will retry forever")
		}
	}
}

func TestReconcileCaptureContinuesAfterClassEnd(t *testing.T) {
	for _, state := range []domain.ClassStatus{domain.ClassAwaitingAdminConfirmation, domain.ClassCompleted} {
		t.Run(string(state), func(t *testing.T) {
			s, w, c := recoveryService(t, state)
			addRecoveryBooking(w, domain.BookingConfirmed)
			worker := Worker{Service: s}
			if err := worker.reconcileBooking(context.Background(), schedulingWorkerActor(), domain.BookingID(testBooking)); err != nil {
				t.Fatal(err)
			}
			if err := worker.reconcileBooking(context.Background(), schedulingWorkerActor(), domain.BookingID(testBooking)); err != nil {
				t.Fatal(err)
			}
			if c.captures != 1 || c.captureCalls != 1 || w.state.bookings[testBooking].View().Status != domain.BookingCharged {
				t.Fatal("capture was stranded or duplicated")
			}
			want := 0
			if state == domain.ClassCompleted {
				want = 1
			}
			if len(payrollEvents(w)) != want {
				t.Fatal("completed payroll was not refreshed exactly once")
			}
		})
	}
}

func TestStaleAttendanceLeaseCannotCommitBookingOrPayroll(t *testing.T) {
	s, w, c := recoveryService(t, domain.ClassCompleted)
	c.captureError = status.Error(codes.Unavailable, "response lost")
	_, _ = s.AddWalkIn(context.Background(), recoveryWalkIn())
	op, err := s.claimAttendanceOperation(context.Background(), "operation-1")
	if err != nil || op == nil {
		t.Fatalf("claim failed: %v", err)
	}
	w.state.operations[op.ID].LeaseToken = "new-worker-lease"
	if err = s.runAttendanceOperation(context.Background(), op); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("stale worker committed: %v", err)
	}
	if w.state.bookings[testBooking].View().Status != domain.BookingPendingCredit || len(w.state.audits) != 0 || len(w.state.events) != 0 {
		t.Fatal("stale lease changed local facts")
	}
}

type recoveryCatalog struct {
	CatalogPort
	w        *recoveryWork
	timezone string
}

func (c recoveryCatalog) GetStudioTimezone(context.Context, ActorContext, string) (string, error) {
	if c.w.inTransaction {
		c.w.t.Fatal("catalog RPC executed inside transaction")
	}
	return c.timezone, nil
}

func TestSubmitScheduleFreezesCatalogTimezone(t *testing.T) {
	s, w, _ := recoveryService(t, domain.ClassCompleted)
	s.Catalog = recoveryCatalog{w: w, timezone: "America/New_York"}
	start := s.Clock.Now().Add(8 * 24 * time.Hour)
	command := SubmitScheduleCommand{Actor: ActorContext{UserID: testUser, StudioID: testStudio, TenantRoles: []string{"teacher"}, ActorKind: "human"}, StudioID: testStudio, RoomID: "20000000-0000-0000-0000-000000000001", Title: "Ballet", StartsAt: start, EndsAt: start.Add(time.Hour), Capacity: 20}
	if _, err := s.SubmitSchedule(context.Background(), command); err != nil {
		t.Fatal(err)
	}
	if w.state.session.StudioTimezone != "America/New_York" {
		t.Fatal("studio timezone was not frozen with the class")
	}
	s.Catalog = recoveryCatalog{w: w, timezone: "not/a/timezone"}
	if _, err := s.SubmitSchedule(context.Background(), command); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("invalid timezone accepted: %v", err)
	}
}

func TestNoShowDoesNotRemoveRedemptionAndRepeatedCommandDoesNotRepeatAudit(t *testing.T) {
	s, w, c := recoveryService(t, domain.ClassCompleted)
	addRecoveryBooking(w, domain.BookingAttended)
	command := CorrectAttendanceCommand{Actor: recoveryAdmin(), BookingID: testBooking, Audit: AuditContext{IdempotencyKey: "no-show-1", Reason: "absent"}}
	for i := 0; i < 2; i++ {
		if _, err := s.CorrectAttendance(context.Background(), command); err != nil {
			t.Fatal(err)
		}
	}
	if w.state.bookings[testBooking].View().Status != domain.BookingNoShow || len(w.state.audits) != 1 || c.reverseCalls != 0 || len(payrollEvents(w)) != 0 || w.state.session.PayrollFactVersion != 1 {
		t.Fatal("no-show incorrectly reversed credits, changed payroll, or duplicated its audit")
	}
	command.Reverse = true
	if _, err := s.CorrectAttendance(context.Background(), command); !errors.Is(err, ErrConflict) {
		t.Fatalf("same key accepted a different action: %v", err)
	}
}

func TestBookingRetryReturnsOriginalAfterClassIsFullAndStarted(t *testing.T) {
	s, w, c := recoveryService(t, domain.ClassMinimumConfirmed)
	addRecoveryBooking(w, domain.BookingCharged)
	snapshot := w.state.session.Aggregate.Snapshot()
	snapshot.Confirmed = int32(snapshot.Capacity)
	w.state.session.Aggregate, _ = domain.RehydrateClassSession(snapshot)
	result, err := s.BookClass(context.Background(), BookClassCommand{Actor: ActorContext{UserID: testUser, StudioID: testStudio, TenantRoles: []string{"student"}, ActorKind: "human"}, SessionID: testClass, Audit: AuditContext{IdempotencyKey: "book-1"}})
	if err != nil || result.ID != testBooking || result.Status != domain.BookingCharged || c.holdCalls != 0 || w.state.session.Aggregate.Snapshot().Confirmed != int32(snapshot.Capacity) {
		t.Fatalf("idempotent retry tried to reserve another seat: result=%+v error=%v", result, err)
	}
}
