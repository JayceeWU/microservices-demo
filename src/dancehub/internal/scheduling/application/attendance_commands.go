package application

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/JayceeWU/microservices-demo/dancehub/internal/scheduling/domain"
)

func (s *Service) CorrectAttendance(ctx context.Context, command CorrectAttendanceCommand) (BookingView, error) {
	actorID, err := requireUser(command.Actor)
	if err != nil {
		return BookingView{}, err
	}
	if strings.TrimSpace(command.Audit.Reason) == "" || strings.TrimSpace(command.Audit.IdempotencyKey) == "" {
		return BookingView{}, Invalid("reason and idempotency_key are required")
	}
	id, err := domain.NewBookingID(command.BookingID)
	if err != nil {
		return BookingView{}, Invalid(err.Error())
	}
	action := "NO_SHOW"
	if command.Reverse {
		action = "REMOVE_REDEMPTION"
	} else if command.Attended {
		action = "ATTEND"
	}
	var operation *AttendanceOperation
	err = s.Work.Do(ctx, command.Actor, "schedulingservice", func(ctx context.Context, r Repositories) error {
		booking, err := r.Bookings().Get(ctx, id, false)
		if err != nil {
			return err
		}
		session, err := r.Sessions().Get(ctx, booking.Aggregate.Snapshot().SessionID, true)
		if err != nil {
			return err
		}
		// The class lock serializes every booking mutation. Acceptance only
		// reads the booking: teachers have roster SELECT rights, while row locks
		// require UPDATE visibility reserved for the recovery worker.
		booking, err = r.Bookings().Get(ctx, id, false)
		if err != nil {
			return err
		}
		request := newAttendanceOperation(booking.View(), string(actorID), action, command.Audit)
		operations := r.(AttendanceRepositories).AttendanceOperations()
		if operation, err = operations.FindByKey(ctx, request.StudioID, request.IdempotencyKey); err != nil || operation != nil {
			if err != nil {
				return err
			}
			return sameAttendanceRequest(operation, &request)
		}
		admin := command.Actor.HasTenantRole("studio_admin") || command.Actor.IsPlatformAdmin()
		v := session.Aggregate.Snapshot()
		policyContext := domain.AttendanceContext{Actor: actorID, Teacher: v.TeacherID, IsAdmin: admin, Period: v.TimeRange, TeacherDeadline: session.TeacherDeadline, AdminDeadline: session.AdminDeadline}
		var policy domain.AttendancePolicy = domain.TeacherAttendancePolicy{}
		if admin {
			policy = domain.AdminAttendancePolicy{}
		}
		if err = policy.Authorize(policyContext, s.Clock.Now()); err != nil {
			if errors.Is(err, domain.ErrPermissionDenied) {
				return Denied(err.Error())
			}
			return Conflict(err.Error())
		}
		if v.Status == domain.ClassCancelled {
			return Conflict("class was cancelled")
		}
		if command.Reverse && (!admin || booking.Aggregate.Snapshot().HoldID == "") {
			return Denied("administrator reversal is required")
		}
		// Validate on a copy before accepting a remote side effect.
		copy := domain.RehydrateBooking(booking.Aggregate.Snapshot())
		if err = applyAttendance(copy, action, ""); err != nil {
			return Conflict(err.Error())
		}
		operation, err = operations.Add(ctx, request)
		return err
	})
	if err != nil {
		return BookingView{}, err
	}
	return s.finishAttendanceRequest(ctx, command.Actor, operation)
}

func (s *Service) AddWalkIn(ctx context.Context, command AddWalkInCommand) (BookingView, error) {
	actorID, err := requireUser(command.Actor)
	if err != nil || (!command.Actor.HasTenantRole("studio_admin") && !command.Actor.IsPlatformAdmin()) {
		return BookingView{}, Denied("studio administrator role required")
	}
	if strings.TrimSpace(command.Audit.IdempotencyKey) == "" || strings.TrimSpace(command.Audit.Reason) == "" {
		return BookingView{}, Invalid("reason and idempotency_key are required")
	}
	student, err := domain.NewUserID(command.StudentID)
	if err != nil {
		return BookingView{}, Invalid(err.Error())
	}
	sessionID, err := domain.NewClassSessionID(command.SessionID)
	if err != nil {
		return BookingView{}, Invalid(err.Error())
	}
	var operation *AttendanceOperation
	err = s.Work.Do(ctx, command.Actor, "schedulingservice", func(ctx context.Context, r Repositories) error {
		session, err := r.Sessions().Get(ctx, sessionID, true)
		if err != nil {
			return err
		}
		view := session.View()
		request := newAttendanceOperation(BookingView{StudioID: view.StudioID, ClassSessionID: view.ID, StudentID: string(student)}, string(actorID), "ADD_WALK_IN", command.Audit)
		operations := r.(AttendanceRepositories).AttendanceOperations()
		if operation, err = operations.FindByKey(ctx, request.StudioID, request.IdempotencyKey); err != nil || operation != nil {
			if err != nil {
				return err
			}
			request.BookingID = operation.BookingID
			return sameAttendanceRequest(operation, &request)
		}
		if s.Clock.Now().Before(view.StartsAt) || s.Clock.Now().After(view.EndsAt.Add(24*time.Hour)) {
			return Conflict("walk-in correction is allowed from class start until 24 hours after class end")
		}
		if view.Status != domain.ClassMinimumConfirmed && view.Status != domain.ClassAwaitingAdminConfirmation && view.Status != domain.ClassCompleted {
			return Conflict("class does not accept walk-in corrections")
		}
		booking, err := r.Bookings().Add(ctx, view.StudioID, domain.NewBooking("", sessionID, student, true), command.Audit.IdempotencyKey)
		if err != nil {
			return err
		}
		if !booking.Created {
			return Conflict("idempotency_key was already used for a different command")
		}
		request.BookingID = booking.View().ID
		operation, err = operations.Add(ctx, request)
		return err
	})
	if err != nil {
		return BookingView{}, err
	}
	return s.finishAttendanceRequest(ctx, command.Actor, operation)
}
