package domain

import (
	"fmt"
	"time"
)

type BookingStatus string

const (
	BookingPendingCredit BookingStatus = "PENDING_CREDIT"
	BookingConfirmed     BookingStatus = "CONFIRMED"
	BookingCharged       BookingStatus = "CHARGED"
	BookingAttended      BookingStatus = "ATTENDED"
	BookingNoShow        BookingStatus = "NO_SHOW"
	BookingCancelled     BookingStatus = "CANCELLED"
	BookingReversed      BookingStatus = "REVERSED"
)

type Booking struct {
	id        BookingID
	sessionID ClassSessionID
	studentID UserID
	status    BookingStatus
	holdID    string
	walkIn    bool
}
type BookingSnapshot struct {
	ID        BookingID
	SessionID ClassSessionID
	StudentID UserID
	Status    BookingStatus
	HoldID    string
	WalkIn    bool
}

func NewBooking(id BookingID, session ClassSessionID, student UserID, walkIn bool) *Booking {
	return &Booking{id: id, sessionID: session, studentID: student, status: BookingPendingCredit, walkIn: walkIn}
}
func RehydrateBooking(v BookingSnapshot) *Booking {
	return &Booking{id: v.ID, sessionID: v.SessionID, studentID: v.StudentID, status: v.Status, holdID: v.HoldID, walkIn: v.WalkIn}
}
func (b *Booking) Snapshot() BookingSnapshot {
	return BookingSnapshot{ID: b.id, SessionID: b.sessionID, StudentID: b.studentID, Status: b.status, HoldID: b.holdID, WalkIn: b.walkIn}
}
func (b *Booking) ConfirmHold(holdID string) error {
	if b.status != BookingPendingCredit || holdID == "" {
		return fmt.Errorf("%w: cannot confirm credit hold", ErrInvalidTransition)
	}
	b.holdID = holdID
	b.status = BookingConfirmed
	return nil
}
func (b *Booking) Capture() error {
	if b.status != BookingConfirmed {
		return fmt.Errorf("%w: booking is not confirmed", ErrInvalidTransition)
	}
	b.status = BookingCharged
	return nil
}
func (b *Booking) Cancel(now time.Time, session ClassTimeRange) error {
	if !now.Before(session.StartsAt().Add(-4 * time.Hour)) {
		return ErrWindowClosed
	}
	if b.status != BookingPendingCredit && b.status != BookingConfirmed {
		return fmt.Errorf("%w: booking cannot be cancelled", ErrInvalidTransition)
	}
	b.status = BookingCancelled
	return nil
}

// CancelByClass is a system compensation caused by the class itself being
// cancelled. Unlike a student cancellation it is intentionally not subject to
// the four-hour cutoff.
func (b *Booking) CancelByClass() error {
	if b.status != BookingPendingCredit && b.status != BookingConfirmed {
		return fmt.Errorf("%w: booking cannot be cancelled with its class", ErrInvalidTransition)
	}
	b.status = BookingCancelled
	return nil
}
func (b *Booking) MarkAttended() error {
	if b.status != BookingCharged && b.status != BookingNoShow && b.status != BookingAttended {
		return fmt.Errorf("%w: attendance requires a charged booking", ErrInvalidTransition)
	}
	b.status = BookingAttended
	return nil
}
func (b *Booking) MarkNoShow() error {
	if b.status != BookingCharged && b.status != BookingAttended && b.status != BookingNoShow {
		return fmt.Errorf("%w: no-show requires a charged booking", ErrInvalidTransition)
	}
	b.status = BookingNoShow
	return nil
}
func (b *Booking) Reverse() error {
	if b.status != BookingCharged && b.status != BookingAttended && b.status != BookingNoShow {
		return fmt.Errorf("%w: booking charge cannot be reversed", ErrInvalidTransition)
	}
	b.status = BookingReversed
	return nil
}
