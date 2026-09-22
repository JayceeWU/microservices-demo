package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/JayceeWU/microservices-demo/dancehub/internal/scheduling/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type AttendanceOperation struct {
	ID, StudioID, ClassSessionID, BookingID, StudentID, ActorID  string
	Action, IdempotencyKey, Reason, Phase, LeaseToken, LastError string
	Attempts                                                     int
}

type AttendanceOperationRepository interface {
	FindByKey(context.Context, string, string) (*AttendanceOperation, error)
	Add(context.Context, AttendanceOperation) (*AttendanceOperation, error)
	Claim(context.Context, string) (*AttendanceOperation, error)
	OwnsClaim(context.Context, *AttendanceOperation) (bool, error)
	Finish(context.Context, *AttendanceOperation, string, string) error
}
type AttendanceRepositories interface {
	AttendanceOperations() AttendanceOperationRepository
}

func newAttendanceOperation(booking BookingView, actorID, action string, audit AuditContext) AttendanceOperation {
	return AttendanceOperation{StudioID: booking.StudioID, ClassSessionID: booking.ClassSessionID, BookingID: booking.ID, StudentID: booking.StudentID,
		ActorID: actorID, Action: action, IdempotencyKey: audit.IdempotencyKey, Reason: strings.TrimSpace(audit.Reason), Phase: "PENDING"}
}

func sameAttendanceRequest(stored, request *AttendanceOperation) error {
	if stored.StudioID != request.StudioID || stored.ClassSessionID != request.ClassSessionID || stored.BookingID != request.BookingID || stored.StudentID != request.StudentID || stored.ActorID != request.ActorID || stored.Action != request.Action || stored.Reason != request.Reason {
		return Conflict("idempotency_key was already used for a different attendance command")
	}
	return nil
}

func (s *Service) finishAttendanceRequest(ctx context.Context, actor ActorContext, operation *AttendanceOperation) (BookingView, error) {
	if operation.Phase == "FAILED" {
		return BookingView{}, Conflict(operation.LastError)
	}
	if operation.Phase != "DONE" {
		claimed, err := s.claimAttendanceOperation(ctx, operation.ID)
		if err != nil {
			return BookingView{}, Unavailable("attendance command accepted; recovery is pending", err)
		}
		if claimed == nil {
			return BookingView{}, Unavailable("attendance command accepted; recovery is pending", nil)
		}
		if err = s.runAttendanceOperation(ctx, claimed); err != nil {
			if !errors.Is(err, ErrConflict) && !errors.Is(err, ErrUnavailable) {
				return BookingView{}, Unavailable("attendance command accepted; recovery is pending", err)
			}
			return BookingView{}, err
		}
	}
	var result BookingView
	err := s.Work.Do(ctx, actor, "schedulingservice", func(ctx context.Context, r Repositories) error {
		booking, err := r.Bookings().Get(ctx, domain.BookingID(operation.BookingID), false)
		if err == nil {
			result = booking.View()
		}
		return err
	})
	return result, err
}

func schedulingWorkerActor() ActorContext {
	return ActorContext{ActorKind: "service", ServicePrincipal: "scheduling-worker", RequestID: "scheduling-worker"}
}

func (s *Service) claimAttendanceOperation(ctx context.Context, id string) (*AttendanceOperation, error) {
	var operation *AttendanceOperation
	err := s.Work.Do(ctx, schedulingWorkerActor(), "scheduling-worker", func(ctx context.Context, r Repositories) error {
		var err error
		operation, err = r.(AttendanceRepositories).AttendanceOperations().Claim(ctx, id)
		return err
	})
	return operation, err
}

func (w Worker) attendanceOnce(ctx context.Context) error {
	for i := 0; i < 50; i++ {
		operation, err := w.Service.claimAttendanceOperation(ctx, "")
		if err != nil || operation == nil {
			return err
		}
		if err = w.Service.runAttendanceOperation(ctx, operation); err != nil {
			// Persisted backoff, or an expiring lease after a database outage,
			// lets unrelated bookings continue making progress.
			slog.Warn("attendance operation pending or failed", "operation_id", operation.ID, "error", err)
		}
	}
	return nil
}

func (s *Service) runAttendanceOperation(ctx context.Context, operation *AttendanceOperation) error {
	var booking *BookingRecord
	var session *SessionRecord
	err := s.Work.Do(ctx, schedulingWorkerActor(), "scheduling-worker", func(ctx context.Context, r Repositories) error {
		var err error
		booking, err = r.Bookings().Get(ctx, domain.BookingID(operation.BookingID), false)
		if err != nil {
			return err
		}
		session, err = r.Sessions().Get(ctx, domain.ClassSessionID(operation.ClassSessionID), false)
		return err
	})
	if err != nil {
		return err
	}
	if session.View().Status == domain.ClassCancelled || booking.View().Status == domain.BookingCancelled {
		return s.failAttendanceOperation(ctx, operation, Conflict("class or booking was cancelled"))
	}
	holdID := booking.Aggregate.Snapshot().HoldID
	switch operation.Action {
	case "ADD_WALK_IN":
		student := actorForStudent(schedulingWorkerActor(), operation.StudentID, operation.StudioID)
		holdID, err = s.Credits.PlaceHold(ctx, student, operation.StudioID, operation.BookingID, session.View().CreditCost, session.View().StartsAt, AuditContext{IdempotencyKey: "attendance:" + operation.ID + ":hold", Reason: operation.Reason})
		if err == nil {
			err = s.Credits.Capture(ctx, student, holdID, AuditContext{IdempotencyKey: "attendance:" + operation.ID + ":capture", Reason: operation.Reason})
		}
	case "REMOVE_REDEMPTION":
		// Authorization was checked before acceptance; retain the requesting
		// administrator and audit reason while resuming the accepted command.
		actor := ActorContext{UserID: operation.ActorID, StudioID: operation.StudioID, TenantRoles: []string{"studio_admin"}, ActorKind: "human", RequestID: operation.ID}
		err = s.Credits.Reverse(ctx, actor, holdID, AuditContext{IdempotencyKey: "attendance:" + operation.ID + ":reverse", Reason: operation.Reason})
	}
	if err != nil {
		if permanentCreditError(err) {
			return s.failAttendanceOperation(ctx, operation, err)
		}
		if saveErr := s.Work.Do(ctx, schedulingWorkerActor(), "scheduling-worker", func(ctx context.Context, r Repositories) error {
			return r.(AttendanceRepositories).AttendanceOperations().Finish(ctx, operation, "PENDING", err.Error())
		}); saveErr != nil {
			return saveErr
		}
		return Unavailable("attendance command accepted; credit recovery is pending", err)
	}
	var cancelled bool
	err = s.Work.Do(ctx, schedulingWorkerActor(), "scheduling-worker", func(ctx context.Context, r Repositories) error {
		session, booking, err := lockClassBooking(ctx, r, domain.BookingID(operation.BookingID))
		if err != nil {
			return err
		}
		operations := r.(AttendanceRepositories).AttendanceOperations()
		owned, err := operations.OwnsClaim(ctx, operation)
		if err != nil {
			return err
		}
		if !owned {
			return Unavailable("attendance command is being recovered by another worker", nil)
		}
		if session.View().Status == domain.ClassCancelled || booking.View().Status == domain.BookingCancelled {
			cancelled = true
			return s.failAttendanceInTransaction(ctx, r, operation, booking, "class or booking was cancelled")
		}
		before := booking.Aggregate.Snapshot().Status
		if err = applyAttendance(booking.Aggregate, operation.Action, holdID); err != nil {
			return Conflict(err.Error())
		}
		booking, err = r.Bookings().Save(ctx, booking, before)
		if err != nil {
			return err
		}
		v := booking.View()
		if err = r.Bookings().RecordAttendance(ctx, AttendanceAudit{StudioID: v.StudioID, SessionID: v.ClassSessionID, BookingID: v.ID, StudentID: v.StudentID, ActorID: operation.ActorID, Action: operation.Action, Reason: operation.Reason, BeforeStatus: string(before), AfterStatus: string(v.Status)}); err != nil {
			return err
		}
		event := DomainEvent{Type: "AttendanceCorrected", AggregateType: "booking", AggregateID: v.ID, StudioID: v.StudioID, Payload: map[string]any{"booking_id": v.ID, "class_session_id": v.ClassSessionID, "status": string(v.Status)}}
		if operation.Action == "ADD_WALK_IN" {
			event.Type = "WalkInCreditCaptured"
			event.Payload["student_id"] = v.StudentID
		}
		if err = r.Outbox().Append(ctx, event); err != nil {
			return err
		}
		if payrollEligible(before) != payrollEligible(v.Status) {
			if err = publishPayrollCorrection(ctx, r, session); err != nil {
				return err
			}
		}
		return operations.Finish(ctx, operation, "DONE", "")
	})
	if err == nil && cancelled {
		return Conflict("class or booking was cancelled")
	}
	return err
}

func applyAttendance(booking *domain.Booking, action, holdID string) error {
	switch action {
	case "ATTEND":
		return booking.MarkAttended()
	case "NO_SHOW":
		return booking.MarkNoShow()
	case "REMOVE_REDEMPTION":
		return booking.Reverse()
	case "ADD_WALK_IN":
		if booking.Snapshot().Status == domain.BookingPendingCredit {
			if err := booking.ConfirmHold(holdID); err != nil {
				return err
			}
		}
		if booking.Snapshot().Status == domain.BookingConfirmed {
			if err := booking.Capture(); err != nil {
				return err
			}
		}
		return booking.MarkAttended()
	default:
		return fmt.Errorf("unknown attendance action %q", action)
	}
}

func permanentCreditError(err error) bool {
	switch status.Code(err) {
	case codes.InvalidArgument, codes.PermissionDenied, codes.Unauthenticated, codes.NotFound, codes.FailedPrecondition, codes.AlreadyExists:
		return true
	default:
		return false
	}
}

func (s *Service) failAttendanceOperation(ctx context.Context, operation *AttendanceOperation, cause error) error {
	err := s.Work.Do(ctx, schedulingWorkerActor(), "scheduling-worker", func(ctx context.Context, r Repositories) error {
		_, booking, err := lockClassBooking(ctx, r, domain.BookingID(operation.BookingID))
		if err != nil {
			return err
		}
		owned, err := r.(AttendanceRepositories).AttendanceOperations().OwnsClaim(ctx, operation)
		if err != nil || !owned {
			return err
		}
		return s.failAttendanceInTransaction(ctx, r, operation, booking, cause.Error())
	})
	if err != nil {
		return err
	}
	return Conflict(cause.Error())
}

func (s *Service) failAttendanceInTransaction(ctx context.Context, r Repositories, operation *AttendanceOperation, booking *BookingRecord, message string) error {
	if operation.Action == "ADD_WALK_IN" {
		before := booking.Aggregate.Snapshot().Status
		if err := cancelForCompensation(booking.Aggregate); err != nil {
			return err
		}
		var err error
		booking, err = r.Bookings().Save(ctx, booking, before)
		if err != nil {
			return err
		}
		if err = enqueueCompensation(ctx, r, booking, message); err != nil {
			return err
		}
	}
	return r.(AttendanceRepositories).AttendanceOperations().Finish(ctx, operation, "FAILED", message)
}
