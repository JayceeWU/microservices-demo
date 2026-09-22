package application

import (
	"context"
	"strings"

	"github.com/JayceeWU/microservices-demo/dancehub/internal/scheduling/domain"
)

func (s *Service) CompleteClass(ctx context.Context, command CompleteClassCommand) (SessionView, error) {
	actor, id, err := requireAdminSession(command.Actor, command.SessionID)
	if err != nil {
		return SessionView{}, err
	}
	return s.mutateSession(ctx, command.Actor, id, func(ctx context.Context, repositories Repositories, record *SessionRecord) (*DomainEvent, error) {
		if record.View().Status == domain.ClassCompleted {
			return nil, nil
		}
		pending, err := repositories.Bookings().HasPendingCredits(ctx, id)
		if err != nil {
			return nil, err
		}
		if pending {
			return nil, Conflict("credit or attendance operations are pending; complete the class after recovery")
		}
		if err = record.Aggregate.Complete(s.Clock.Now(), actor); err != nil {
			return nil, Conflict(err.Error())
		}
		return payrollFact(ctx, repositories, record)
	})
}

func (s *Service) CancelClass(ctx context.Context, command CancelClassCommand) (SessionView, error) {
	if strings.TrimSpace(command.Audit.Reason) == "" {
		return SessionView{}, Invalid("reason is required")
	}
	_, id, err := requireAdminSession(command.Actor, command.SessionID)
	if err != nil {
		return SessionView{}, err
	}
	var result SessionView
	var bookings []*BookingRecord
	err = s.Work.Do(ctx, command.Actor, "schedulingservice", func(ctx context.Context, repositories Repositories) error {
		record, err := repositories.Sessions().Get(ctx, id, true)
		if err != nil {
			return err
		}
		before := record.Aggregate.Snapshot().Status
		if err = record.Aggregate.Cancel(command.Audit.Reason); err != nil {
			return Conflict(err.Error())
		}
		record, err = repositories.Sessions().Save(ctx, record, before)
		if err != nil {
			return err
		}
		result = record.View()
		bookings, err = repositories.Bookings().ListForSession(ctx, id, true)
		if err != nil {
			return err
		}
		for _, booking := range bookings {
			before := booking.Aggregate.Snapshot().Status
			if err = cancelForCompensation(booking.Aggregate); err != nil {
				return err
			}
			booking, err = repositories.Bookings().Save(ctx, booking, before)
			if err != nil {
				return err
			}
			if err = enqueueCompensation(ctx, repositories, booking, command.Audit.Reason); err != nil {
				return err
			}
		}
		return repositories.Outbox().Append(ctx, DomainEvent{Type: "ClassCancelled", AggregateType: "class_session", AggregateID: result.ID, StudioID: result.StudioID, Payload: map[string]any{"class_session_id": result.ID, "reason": command.Audit.Reason}})
	})
	if err != nil {
		return SessionView{}, err
	}
	return result, nil
}

func (s *Service) compensateCancelledBooking(ctx context.Context, actor ActorContext, booking *BookingRecord, reason string) error {
	return s.Work.Do(ctx, actor, "schedulingservice", func(ctx context.Context, r Repositories) error {
		_, current, err := lockClassBooking(ctx, r, booking.Aggregate.Snapshot().ID)
		if err != nil {
			return err
		}
		before := current.Aggregate.Snapshot().Status
		if err = cancelForCompensation(current.Aggregate); err != nil {
			return err
		}
		current, err = r.Bookings().Save(ctx, current, before)
		if err != nil {
			return err
		}
		return enqueueCompensation(ctx, r, current, reason)
	})
}
