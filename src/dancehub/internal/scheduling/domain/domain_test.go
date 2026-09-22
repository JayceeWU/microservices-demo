package domain

import (
	"errors"
	"testing"
	"time"
)

func TestClassSessionStateAndMinimumPolicy(t *testing.T) {
	now := time.Date(2026, 11, 11, 8, 0, 0, 0, time.UTC)
	period, _ := NewClassTimeRange(now.Add(8*time.Hour), now.Add(9*time.Hour), now)
	cap, _ := NewCapacity(10)
	minimum, _ := NewMinimumStudents(4, cap)
	s, err := NewClassSession("11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222", "room", "33333333-3333-4333-8333-333333333333", "Class", period, cap)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Approve(minimum); err != nil {
		t.Fatal(err)
	}
	for range 4 {
		if err = s.ReserveSeat(); err != nil {
			t.Fatal(err)
		}
	}
	if got := DefaultClassMinimumPolicy().Evaluate(s.Snapshot(), now.Add(4*time.Hour)); got != MinimumConfirm {
		t.Fatalf("decision=%v", got)
	}
	if err = s.ConfirmMinimum(); err != nil {
		t.Fatal(err)
	}
	if err = s.AwaitAdminConfirmation(period.EndsAt()); err != nil {
		t.Fatal(err)
	}
	if err = s.Complete(period.EndsAt(), "44444444-4444-4444-8444-444444444444"); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(s.Cancel("late"), ErrInvalidTransition) {
		t.Fatal("completed class cancellation should fail")
	}
}

func TestAttendancePoliciesUseDifferentWindows(t *testing.T) {
	start := time.Date(2026, 11, 11, 18, 0, 0, 0, time.UTC)
	period, _ := RehydrateClassTimeRange(start, start.Add(time.Hour))
	ctx := AttendanceContext{Actor: "a", Teacher: "a", Period: period, TeacherDeadline: start.Add(5 * time.Hour), AdminDeadline: start.Add(25 * time.Hour)}
	if err := (TeacherAttendancePolicy{}).Authorize(ctx, start); err != nil {
		t.Fatal(err)
	}
	if !errors.Is((TeacherAttendancePolicy{}).Authorize(ctx, start.Add(6*time.Hour)), ErrWindowClosed) {
		t.Fatal("teacher window should close")
	}
	ctx.IsAdmin = true
	if err := (AdminAttendancePolicy{}).Authorize(ctx, start.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
}

func TestBookingCancellationBoundary(t *testing.T) {
	start := time.Date(2026, 11, 11, 18, 0, 0, 0, time.UTC)
	period, _ := RehydrateClassTimeRange(start, start.Add(time.Hour))
	b := NewBooking("b", "s", "u", false)
	if err := b.ConfirmHold("h"); err != nil {
		t.Fatal(err)
	}
	if err := b.Cancel(start.Add(-4*time.Hour), period); !errors.Is(err, ErrWindowClosed) {
		t.Fatalf("expected closed boundary, got %v", err)
	}
}

func TestMinimumPolicyAtFourHourBoundary(t *testing.T) {
	start := time.Date(2026, 11, 11, 18, 0, 0, 0, time.UTC)
	period, _ := RehydrateClassTimeRange(start, start.Add(time.Hour))
	capacity, _ := NewCapacity(8)
	minimum, _ := NewMinimumStudents(4, capacity)
	session, _ := NewClassSession("11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222", "room", "33333333-3333-4333-8333-333333333333", "Class", period, capacity)
	if err := session.Approve(minimum); err != nil {
		t.Fatal(err)
	}
	policy := DefaultClassMinimumPolicy()
	if got := policy.Evaluate(session.Snapshot(), start.Add(-4*time.Hour-time.Nanosecond)); got != MinimumNotDue {
		t.Fatalf("before cutoff decision=%v", got)
	}
	if got := policy.Evaluate(session.Snapshot(), start.Add(-4*time.Hour)); got != MinimumCancel {
		t.Fatalf("at cutoff decision=%v", got)
	}
}

func TestClassCancellationCanCompensateBookingAfterStudentCutoff(t *testing.T) {
	booking := NewBooking("booking", "session", "student", false)
	if err := booking.ConfirmHold("hold"); err != nil {
		t.Fatal(err)
	}
	if err := booking.CancelByClass(); err != nil {
		t.Fatal(err)
	}
	if booking.Snapshot().Status != BookingCancelled {
		t.Fatalf("status=%s", booking.Snapshot().Status)
	}
}

func TestRoomRefundWindowAndAdminOverride(t *testing.T) {
	start := time.Date(2026, 11, 20, 18, 0, 0, 0, time.UTC)
	period, _ := RehydrateClassTimeRange(start, start.Add(time.Hour))
	reservation := NewRoomReservation("reservation", period, start.Add(-48*time.Hour))
	if err := reservation.BeginPayment("order", start.Add(-48*time.Hour+5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := reservation.Confirm(start.Add(-48*time.Hour + 10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := reservation.RequestRefund(start.Add(-24*time.Hour), false); !errors.Is(err, ErrWindowClosed) {
		t.Fatalf("ordinary cancellation should close at T-24, got %v", err)
	}
	if err := reservation.RequestRefund(start.Add(-2*time.Hour), true); err != nil {
		t.Fatalf("admin override failed: %v", err)
	}
}
