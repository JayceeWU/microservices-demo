package domain

import (
	"fmt"
	"strings"
	"time"
)

type ClassStatus string

const (
	ClassPendingApproval           ClassStatus = "PENDING_APPROVAL"
	ClassApproved                  ClassStatus = "APPROVED"
	ClassOpen                      ClassStatus = "OPEN"
	ClassMinimumConfirmed          ClassStatus = "MINIMUM_CONFIRMED"
	ClassAwaitingAdminConfirmation ClassStatus = "AWAITING_ADMIN_CONFIRMATION"
	ClassCompleted                 ClassStatus = "COMPLETED"
	ClassCancelled                 ClassStatus = "CANCELLED"
)

type ClassSession struct {
	id              ClassSessionID
	studioID        StudioID
	roomID          string
	teacherID       UserID
	title           string
	timeRange       ClassTimeRange
	capacity        Capacity
	minimum         MinimumStudents
	creditCost      CreditCost
	confirmed       int32
	status          ClassStatus
	cancelledReason string
	completedBy     UserID
}

type ClassSessionSnapshot struct {
	ID              ClassSessionID
	StudioID        StudioID
	RoomID          string
	TeacherID       UserID
	Title           string
	TimeRange       ClassTimeRange
	Capacity        Capacity
	Minimum         MinimumStudents
	CreditCost      CreditCost
	Confirmed       int32
	Status          ClassStatus
	CancelledReason string
	CompletedBy     UserID
}

func NewClassSession(id ClassSessionID, studio StudioID, roomID string, teacher UserID, title string, period ClassTimeRange, capacity Capacity) (*ClassSession, error) {
	minimum, _ := NewMinimumStudents(4, capacity)
	if capacity < 4 {
		minimum = MinimumStudents(capacity)
	}
	if strings.TrimSpace(roomID) == "" || strings.TrimSpace(title) == "" {
		return nil, fmt.Errorf("%w: room and title are required", ErrInvalidArgument)
	}
	return &ClassSession{id: id, studioID: studio, roomID: roomID, teacherID: teacher, title: strings.TrimSpace(title), timeRange: period, capacity: capacity, minimum: minimum, creditCost: period.CreditCost(), status: ClassPendingApproval}, nil
}

func RehydrateClassSession(snapshot ClassSessionSnapshot) (*ClassSession, error) {
	if snapshot.Confirmed < 0 || snapshot.Confirmed > int32(snapshot.Capacity) {
		return nil, fmt.Errorf("%w: confirmed count is outside capacity", ErrInvalidArgument)
	}
	if _, ok := validClassStatuses[snapshot.Status]; !ok {
		return nil, fmt.Errorf("%w: unknown class status %q", ErrInvalidArgument, snapshot.Status)
	}
	return &ClassSession{id: snapshot.ID, studioID: snapshot.StudioID, roomID: snapshot.RoomID, teacherID: snapshot.TeacherID, title: snapshot.Title, timeRange: snapshot.TimeRange, capacity: snapshot.Capacity, minimum: snapshot.Minimum, creditCost: snapshot.CreditCost, confirmed: snapshot.Confirmed, status: snapshot.Status, cancelledReason: snapshot.CancelledReason, completedBy: snapshot.CompletedBy}, nil
}

var validClassStatuses = map[ClassStatus]struct{}{ClassPendingApproval: {}, ClassApproved: {}, ClassOpen: {}, ClassMinimumConfirmed: {}, ClassAwaitingAdminConfirmation: {}, ClassCompleted: {}, ClassCancelled: {}}

func (s *ClassSession) Snapshot() ClassSessionSnapshot {
	return ClassSessionSnapshot{ID: s.id, StudioID: s.studioID, RoomID: s.roomID, TeacherID: s.teacherID, Title: s.title, TimeRange: s.timeRange, Capacity: s.capacity, Minimum: s.minimum, CreditCost: s.creditCost, Confirmed: s.confirmed, Status: s.status, CancelledReason: s.cancelledReason, CompletedBy: s.completedBy}
}

func (s *ClassSession) Approve(minimum MinimumStudents) error {
	if s.status != ClassPendingApproval {
		return transitionError(s.status, ClassOpen)
	}
	if minimum > MinimumStudents(s.capacity) {
		return fmt.Errorf("%w: minimum exceeds capacity", ErrInvalidArgument)
	}
	s.minimum, s.status = minimum, ClassOpen
	return nil
}

func (s *ClassSession) Reject(reason string) error { return s.cancelFrom(ClassPendingApproval, reason) }

func (s *ClassSession) ReserveSeat() error {
	if s.status != ClassOpen && s.status != ClassMinimumConfirmed {
		return transitionError(s.status, s.status)
	}
	if s.confirmed >= int32(s.capacity) {
		return ErrCapacityExceeded
	}
	s.confirmed++
	return nil
}

func (s *ClassSession) ReleaseSeat() error {
	if s.confirmed == 0 {
		return fmt.Errorf("%w: no seat to release", ErrInvalidTransition)
	}
	s.confirmed--
	return nil
}

func (s *ClassSession) ConfirmMinimum() error {
	if s.status != ClassOpen {
		return transitionError(s.status, ClassMinimumConfirmed)
	}
	if s.confirmed < int32(s.minimum) {
		return fmt.Errorf("%w: minimum attendance not met", ErrInvalidTransition)
	}
	s.status = ClassMinimumConfirmed
	return nil
}

func (s *ClassSession) AwaitAdminConfirmation(now time.Time) error {
	if s.status != ClassMinimumConfirmed || now.Before(s.timeRange.EndsAt()) {
		return transitionError(s.status, ClassAwaitingAdminConfirmation)
	}
	s.status = ClassAwaitingAdminConfirmation
	return nil
}

func (s *ClassSession) Complete(now time.Time, actor UserID) error {
	if s.status != ClassAwaitingAdminConfirmation || now.Before(s.timeRange.EndsAt()) {
		return transitionError(s.status, ClassCompleted)
	}
	s.status, s.completedBy = ClassCompleted, actor
	return nil
}

func (s *ClassSession) Cancel(reason string) error {
	if s.status == ClassCompleted || s.status == ClassCancelled {
		return transitionError(s.status, ClassCancelled)
	}
	if strings.TrimSpace(reason) == "" {
		return fmt.Errorf("%w: cancellation reason is required", ErrInvalidArgument)
	}
	s.status, s.cancelledReason = ClassCancelled, strings.TrimSpace(reason)
	return nil
}

func (s *ClassSession) cancelFrom(expected ClassStatus, reason string) error {
	if s.status != expected {
		return transitionError(s.status, ClassCancelled)
	}
	return s.Cancel(reason)
}

func transitionError(from, to ClassStatus) error {
	return fmt.Errorf("%w: %s to %s", ErrInvalidTransition, from, to)
}
