package domain

import "time"

type MinimumDecision int

const (
	MinimumNotDue MinimumDecision = iota
	MinimumConfirm
	MinimumCancel
)

type ClassMinimumPolicy struct{ LeadTime time.Duration }

func DefaultClassMinimumPolicy() ClassMinimumPolicy {
	return ClassMinimumPolicy{LeadTime: 4 * time.Hour}
}
func (p ClassMinimumPolicy) Evaluate(session ClassSessionSnapshot, now time.Time) MinimumDecision {
	if session.Status != ClassOpen || now.Before(session.TimeRange.StartsAt().Add(-p.LeadTime)) {
		return MinimumNotDue
	}
	if session.Confirmed < int32(session.Minimum) {
		return MinimumCancel
	}
	return MinimumConfirm
}

type AttendanceContext struct {
	Actor           UserID
	Teacher         UserID
	IsAdmin         bool
	Period          ClassTimeRange
	TeacherDeadline time.Time
	AdminDeadline   time.Time
}
type AttendancePolicy interface {
	Authorize(AttendanceContext, time.Time) error
}
type TeacherAttendancePolicy struct{}

func (TeacherAttendancePolicy) Authorize(v AttendanceContext, now time.Time) error {
	if v.Actor != v.Teacher {
		return ErrPermissionDenied
	}
	if now.Before(v.Period.StartsAt()) || now.After(v.TeacherDeadline) {
		return ErrWindowClosed
	}
	return nil
}

type AdminAttendancePolicy struct{}

func (AdminAttendancePolicy) Authorize(v AttendanceContext, now time.Time) error {
	if !v.IsAdmin {
		return ErrPermissionDenied
	}
	if now.After(v.AdminDeadline) {
		return ErrWindowClosed
	}
	return nil
}
