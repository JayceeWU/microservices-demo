package domain

import (
	"fmt"
	"time"
)

type RoomReservationStatus string

const (
	RoomHold           RoomReservationStatus = "HOLD"
	RoomPendingPayment RoomReservationStatus = "PENDING_PAYMENT"
	RoomConfirmed      RoomReservationStatus = "CONFIRMED"
	RoomExpired        RoomReservationStatus = "EXPIRED"
	RoomRefundPending  RoomReservationStatus = "REFUND_PENDING"
	RoomRefunded       RoomReservationStatus = "REFUNDED"
	RoomCancelled      RoomReservationStatus = "CANCELLED"
)

type RoomReservation struct {
	id            RoomReservationID
	period        ClassTimeRange
	status        RoomReservationStatus
	holdExpiresAt time.Time
	orderID       string
}

type RoomReservationSnapshot struct {
	ID            RoomReservationID
	TimeRange     ClassTimeRange
	Status        RoomReservationStatus
	HoldExpiresAt time.Time
	OrderID       string
}

func NewRoomReservation(id RoomReservationID, period ClassTimeRange, now time.Time) *RoomReservation {
	return &RoomReservation{id: id, period: period, status: RoomHold, holdExpiresAt: now.Add(15 * time.Minute)}
}

func RehydrateRoomReservation(snapshot RoomReservationSnapshot) *RoomReservation {
	return &RoomReservation{id: snapshot.ID, period: snapshot.TimeRange, status: snapshot.Status, holdExpiresAt: snapshot.HoldExpiresAt, orderID: snapshot.OrderID}
}

func (r *RoomReservation) Snapshot() RoomReservationSnapshot {
	return RoomReservationSnapshot{ID: r.id, TimeRange: r.period, Status: r.status, HoldExpiresAt: r.holdExpiresAt, OrderID: r.orderID}
}
func (r *RoomReservation) BeginPayment(orderID string, now time.Time) error {
	if r.status != RoomHold || !now.Before(r.holdExpiresAt) || orderID == "" {
		return fmt.Errorf("%w: room hold cannot enter payment", ErrInvalidTransition)
	}
	r.status = RoomPendingPayment
	r.orderID = orderID
	return nil
}
func (r *RoomReservation) Confirm(now time.Time) error {
	if r.status != RoomPendingPayment || !now.Before(r.holdExpiresAt) {
		return fmt.Errorf("%w: room reservation cannot be confirmed", ErrInvalidTransition)
	}
	r.status = RoomConfirmed
	return nil
}

// ConfirmFromPayment accepts both the explicit pending-payment state and the
// original hold. Orders currently confirms the room after payment succeeds in
// one idempotent command, so no transport-specific intermediate state leaks
// into the domain.
func (r *RoomReservation) ConfirmFromPayment(orderID string, now time.Time) error {
	if r.status == RoomConfirmed && r.orderID == orderID {
		return nil
	}
	if (r.status != RoomHold && r.status != RoomPendingPayment) || !now.Before(r.holdExpiresAt) || orderID == "" {
		return fmt.Errorf("%w: room reservation cannot be confirmed", ErrInvalidTransition)
	}
	r.status, r.orderID = RoomConfirmed, orderID
	return nil
}

func (r *RoomReservation) Cancel() error {
	if r.status == RoomCancelled || r.status == RoomExpired || r.status == RoomRefunded {
		return nil
	}
	if r.status != RoomHold && r.status != RoomPendingPayment && r.status != RoomConfirmed {
		return fmt.Errorf("%w: room reservation cannot be cancelled", ErrInvalidTransition)
	}
	r.status = RoomCancelled
	return nil
}
func (r *RoomReservation) Expire(now time.Time) error {
	if r.status != RoomHold && r.status != RoomPendingPayment {
		return fmt.Errorf("%w: room reservation cannot expire", ErrInvalidTransition)
	}
	if now.Before(r.holdExpiresAt) {
		return fmt.Errorf("%w: room hold has not expired", ErrInvalidTransition)
	}
	r.status = RoomExpired
	return nil
}
func (r *RoomReservation) RequestRefund(now time.Time, admin bool) error {
	if r.status != RoomConfirmed {
		return fmt.Errorf("%w: only confirmed reservation can be refunded", ErrInvalidTransition)
	}
	if !admin && !now.Before(r.period.StartsAt().Add(-24*time.Hour)) {
		return ErrWindowClosed
	}
	r.status = RoomRefundPending
	return nil
}
func (r *RoomReservation) MarkRefunded() error {
	if r.status != RoomRefundPending {
		return fmt.Errorf("%w: refund is not pending", ErrInvalidTransition)
	}
	r.status = RoomRefunded
	return nil
}
func (r *RoomReservation) Status() RoomReservationStatus { return r.status }
