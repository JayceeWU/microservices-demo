package application

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/JayceeWU/microservices-demo/dancehub/internal/scheduling/domain"
)

type Worker struct {
	Service  *Service
	Interval time.Duration
}

func (w Worker) Run(ctx context.Context) {
	interval := w.Interval
	if interval <= 0 {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := w.TransitionOnce(ctx); err != nil {
			slog.Warn("scheduling transition failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w Worker) TransitionOnce(ctx context.Context) error {
	actor := ActorContext{ActorKind: "service", ServicePrincipal: "scheduling-worker", RequestID: "scheduling-worker"}
	if err := w.compensateOnce(ctx, actor); err != nil {
		return err
	}
	if err := w.attendanceOnce(ctx); err != nil {
		return err
	}
	var minimumIDs, endingIDs []domain.ClassSessionID
	var roomIDs []domain.RoomReservationID
	err := w.Service.Work.Do(ctx, actor, "scheduling-worker", func(ctx context.Context, repositories Repositories) error {
		var err error
		minimumIDs, err = repositories.Sessions().DueMinimumIDs(ctx, 50)
		if err != nil {
			return err
		}
		endingIDs, err = repositories.Sessions().DueEndingIDs(ctx, 50)
		if err != nil {
			return err
		}
		roomIDs, err = repositories.Rooms().DueExpiredIDs(ctx, 50)
		return err
	})
	if err != nil {
		return err
	}
	for _, id := range minimumIDs {
		if err = w.evaluateMinimum(ctx, actor, id); err != nil {
			return err
		}
	}
	for _, id := range endingIDs {
		if err = w.awaitConfirmation(ctx, actor, id); err != nil {
			return err
		}
	}
	for _, id := range roomIDs {
		if err = w.expireRoom(ctx, actor, id); err != nil {
			return err
		}
	}
	return w.reconcile(ctx, actor)
}

func (w Worker) evaluateMinimum(ctx context.Context, actor ActorContext, id domain.ClassSessionID) error {
	var actions []*BookingRecord
	var cancelled bool
	err := w.Service.Work.Do(ctx, actor, "scheduling-worker", func(ctx context.Context, repositories Repositories) error {
		session, err := repositories.Sessions().Get(ctx, id, true)
		if err != nil {
			return err
		}
		before := session.Aggregate.Snapshot().Status
		decision := domain.DefaultClassMinimumPolicy().Evaluate(session.Aggregate.Snapshot(), w.Service.Clock.Now())
		if decision == domain.MinimumNotDue {
			return nil
		}
		actions, err = repositories.Bookings().ListForSession(ctx, id, true)
		if err != nil {
			return err
		}
		if decision == domain.MinimumCancel {
			cancelled = true
			if err = session.Aggregate.Cancel("minimum_not_met"); err != nil {
				return err
			}
		} else if err = session.Aggregate.ConfirmMinimum(); err != nil {
			return err
		}
		session, err = repositories.Sessions().Save(ctx, session, before)
		if err != nil {
			return err
		}
		view := session.View()
		event := "ClassMinimumConfirmed"
		payload := map[string]any{"class_session_id": view.ID, "confirmed_count": view.ConfirmedCount}
		if cancelled {
			event = "ClassCancelled"
			payload = map[string]any{"class_session_id": view.ID, "reason": "minimum_not_met"}
		}
		if cancelled {
			for _, booking := range actions {
				before := booking.Aggregate.Snapshot().Status
				if err = cancelForCompensation(booking.Aggregate); err != nil {
					return err
				}
				booking, err = repositories.Bookings().Save(ctx, booking, before)
				if err != nil {
					return err
				}
				if err = enqueueCompensation(ctx, repositories, booking, "minimum not met"); err != nil {
					return err
				}
			}
		}
		return repositories.Outbox().Append(ctx, DomainEvent{Type: event, AggregateType: "class_session", AggregateID: view.ID, StudioID: view.StudioID, Payload: payload})
	})
	if err != nil {
		return err
	}
	for _, booking := range actions {
		if booking.Aggregate.Snapshot().Status != domain.BookingConfirmed || booking.Aggregate.Snapshot().HoldID == "" {
			continue
		}
		if cancelled {
			if err = w.Service.compensateCancelledBooking(ctx, actor, booking, "minimum not met"); err != nil {
				return err
			}
		} else {
			snapshot := booking.Aggregate.Snapshot()
			student := actorForStudent(actor, string(snapshot.StudentID), booking.StudioID)
			if err = w.Service.Credits.Capture(ctx, student, snapshot.HoldID, AuditContext{IdempotencyKey: "minimum-capture:" + string(snapshot.ID), Reason: "class minimum confirmed"}); err != nil {
				return err
			}
			if err = w.Service.setBookingCaptured(ctx, actor, string(snapshot.ID)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (w Worker) awaitConfirmation(ctx context.Context, actor ActorContext, id domain.ClassSessionID) error {
	return w.Service.Work.Do(ctx, actor, "scheduling-worker", func(ctx context.Context, repositories Repositories) error {
		session, err := repositories.Sessions().Get(ctx, id, true)
		if err != nil {
			return err
		}
		before := session.Aggregate.Snapshot().Status
		if err = session.Aggregate.AwaitAdminConfirmation(w.Service.Clock.Now()); err != nil {
			return err
		}
		_, err = repositories.Sessions().Save(ctx, session, before)
		return err
	})
}

func (w Worker) expireRoom(ctx context.Context, actor ActorContext, id domain.RoomReservationID) error {
	return w.Service.Work.Do(ctx, actor, "scheduling-worker", func(ctx context.Context, repositories Repositories) error {
		room, err := repositories.Rooms().Get(ctx, id, true)
		if err != nil {
			return err
		}
		before := room.Aggregate.Snapshot().Status
		if err = room.Aggregate.Expire(w.Service.Clock.Now()); err != nil {
			return err
		}
		_, err = repositories.Rooms().Save(ctx, room, before)
		return err
	})
}

func (w Worker) reconcile(ctx context.Context, actor ActorContext) error {
	var ids []domain.BookingID
	err := w.Service.Work.Do(ctx, actor, "scheduling-worker", func(ctx context.Context, repositories Repositories) error {
		var err error
		ids, err = repositories.Bookings().ReconciliationIDs(ctx, 50)
		return err
	})
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err = w.reconcileBooking(ctx, actor, id); err != nil && !errors.Is(err, ErrConflict) {
			slog.Warn("booking reconciliation pending", "booking_id", id, "error", err)
		}
	}
	return nil
}

func (w Worker) reconcileBooking(ctx context.Context, actor ActorContext, id domain.BookingID) error {
	var booking *BookingRecord
	var session *SessionRecord
	err := w.Service.Work.Do(ctx, actor, "scheduling-worker", func(ctx context.Context, repositories Repositories) error {
		var err error
		session, booking, err = lockClassBooking(ctx, repositories, id)
		if err != nil {
			return err
		}
		// Rotate attempted bookings behind untouched work, so one unavailable
		// credit account cannot monopolize the next reconciliation batch.
		booking, err = repositories.Bookings().Save(ctx, booking, booking.Aggregate.Snapshot().Status)
		return err
	})
	if err != nil {
		return err
	}
	b, c := booking.Aggregate.Snapshot(), session.Aggregate.Snapshot()
	student := actorForStudent(actor, string(b.StudentID), booking.StudioID)
	if c.Status == domain.ClassCancelled {
		return w.Service.compensateCancelledBooking(ctx, actor, booking, "class cancelled")
	}
	if b.Status == domain.BookingPendingCredit {
		hold, err := w.Service.Credits.PlaceHold(ctx, student, booking.StudioID, string(b.ID), int32(c.CreditCost), c.TimeRange.StartsAt(), AuditContext{IdempotencyKey: "reconcile-hold:" + string(b.ID), Reason: "booking reconciliation"})
		if err != nil {
			if permanentCreditError(err) {
				_, cancelErr := w.Service.cancelBookingDurably(ctx, actor, id, err.Error(), false)
				return cancelErr
			}
			return err
		}
		return w.Service.Work.Do(ctx, actor, "scheduling-worker", func(ctx context.Context, repositories Repositories) error {
			session, current, err := lockClassBooking(ctx, repositories, id)
			if err != nil {
				return err
			}
			before := current.Aggregate.Snapshot().Status
			if before != domain.BookingPendingCredit {
				return nil
			}
			if session.View().Status == domain.ClassCancelled {
				return Conflict("class was cancelled")
			}
			if err = current.Aggregate.ConfirmHold(hold); err != nil {
				return Conflict(err.Error())
			}
			current, err = repositories.Bookings().Save(ctx, current, before)
			if err != nil {
				return err
			}
			v := current.View()
			return repositories.Outbox().Append(ctx, DomainEvent{Type: "BookingCreated", AggregateType: "booking", AggregateID: v.ID, StudioID: v.StudioID, Payload: map[string]any{"booking_id": v.ID, "class_session_id": v.ClassSessionID, "student_id": v.StudentID}})
		})
	}
	if (c.Status == domain.ClassMinimumConfirmed || c.Status == domain.ClassAwaitingAdminConfirmation || c.Status == domain.ClassCompleted) && b.Status == domain.BookingConfirmed {
		if err := w.Service.Credits.Capture(ctx, student, b.HoldID, AuditContext{IdempotencyKey: "minimum-capture:" + string(b.ID), Reason: "class minimum confirmed"}); err != nil {
			if permanentCreditError(err) {
				_, cancelErr := w.Service.cancelBookingDurably(ctx, actor, id, err.Error(), false)
				return cancelErr
			}
			return err
		}
		return w.Service.setBookingCaptured(ctx, actor, string(b.ID))
	}
	if c.Status == domain.ClassCancelled {
		return w.Service.compensateCancelledBooking(ctx, actor, booking, "class cancelled")
	}
	return nil
}
