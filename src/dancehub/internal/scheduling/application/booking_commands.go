package application

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/JayceeWU/microservices-demo/dancehub/internal/scheduling/domain"
)

func (s *Service) BookClass(ctx context.Context, command BookClassCommand) (BookingView, error) {
	user, err := requireUser(command.Actor)
	if err != nil {
		return BookingView{}, err
	}
	if strings.TrimSpace(command.Audit.IdempotencyKey) == "" {
		return BookingView{}, Invalid("idempotency_key is required")
	}
	sessionID, err := domain.NewClassSessionID(command.SessionID)
	if err != nil {
		return BookingView{}, Invalid(err.Error())
	}
	var booking *BookingRecord
	var session SessionView
	err = s.Work.Do(ctx, command.Actor, "schedulingservice", func(ctx context.Context, repositories Repositories) error {
		record, err := repositories.Sessions().Get(ctx, sessionID, true)
		if err != nil {
			return err
		}
		booking, err = repositories.Bookings().FindByIdempotencyKey(ctx, record.View().StudioID, command.Audit.IdempotencyKey)
		if err == nil {
			if booking.View().ClassSessionID != command.SessionID || booking.View().StudentID != string(user) || booking.View().WalkIn {
				return Conflict("idempotency_key was already used for a different booking")
			}
			booking.Created = false
			session = record.View()
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		if !s.Clock.Now().Before(record.View().StartsAt) {
			return Conflict("class has already started")
		}
		before := record.Aggregate.Snapshot().Status
		if err = record.Aggregate.ReserveSeat(); err != nil {
			if errors.Is(err, domain.ErrCapacityExceeded) {
				return Capacity("class is full")
			}
			return Conflict("class is not open")
		}
		created := domain.NewBooking("", sessionID, user, false)
		booking, err = repositories.Bookings().Add(ctx, string(record.Aggregate.Snapshot().StudioID), created, command.Audit.IdempotencyKey)
		if err != nil {
			return err
		}
		if !booking.Created {
			session = record.View()
			return nil
		}
		record, err = repositories.Sessions().Save(ctx, record, before)
		if err == nil {
			session = record.View()
		}
		return err
	})
	if err != nil {
		return BookingView{}, err
	}
	if !booking.Created && booking.Aggregate.Snapshot().Status != domain.BookingPendingCredit {
		return booking.View(), nil
	}

	holdID, holdErr := s.Credits.PlaceHold(ctx, command.Actor, session.StudioID, booking.View().ID, session.CreditCost, session.StartsAt, command.Audit)
	if holdErr != nil {
		_ = s.cancelPendingBooking(ctx, command.Actor, booking.View().ID)
		return BookingView{}, Conflict(holdErr.Error())
	}
	err = s.Work.Do(ctx, command.Actor, "schedulingservice", func(ctx context.Context, repositories Repositories) error {
		_, current, err := lockClassBooking(ctx, repositories, domain.BookingID(booking.View().ID))
		if err != nil {
			return err
		}
		before := current.Aggregate.Snapshot().Status
		if before != domain.BookingPendingCredit {
			booking = current
			if before == domain.BookingCancelled || before == domain.BookingReversed {
				return Conflict("booking has been cancelled")
			}
			return nil
		}
		if err = current.Aggregate.ConfirmHold(holdID); err != nil {
			return Conflict(err.Error())
		}
		if booking, err = repositories.Bookings().Save(ctx, current, before); err != nil {
			return err
		}
		// The booking only counts once its credit hold is confirmed; the Recommendation
		// projection consumes this event for the studio's bookings metric.
		return repositories.Outbox().Append(ctx, DomainEvent{Type: "BookingCreated", AggregateType: "booking", AggregateID: booking.View().ID, StudioID: session.StudioID, Payload: map[string]any{"booking_id": booking.View().ID, "class_session_id": string(sessionID), "student_id": string(user)}})
	})
	if err != nil {
		return BookingView{}, err
	}
	if session.Status == domain.ClassMinimumConfirmed || !s.Clock.Now().Before(session.StartsAt.Add(-4*time.Hour)) {
		if err = s.Credits.Capture(ctx, command.Actor, holdID, AuditContext{IdempotencyKey: command.Audit.IdempotencyKey + ":capture", Reason: "booking after minimum confirmation"}); err != nil {
			return BookingView{}, Unavailable("seat reserved; credit capture pending", err)
		}
		err = s.setBookingCaptured(ctx, command.Actor, booking.View().ID)
		if err != nil {
			return BookingView{}, err
		}
		booking.Aggregate = domain.RehydrateBooking(domain.BookingSnapshot{ID: domain.BookingID(booking.View().ID), SessionID: sessionID, StudentID: user, Status: domain.BookingCharged, HoldID: holdID})
	}
	return booking.View(), nil
}

func (s *Service) cancelPendingBooking(ctx context.Context, actor ActorContext, bookingID string) error {
	_, err := s.cancelBookingDurably(ctx, actor, domain.BookingID(bookingID), "credit hold failed", false)
	return err
}

func (s *Service) setBookingCaptured(ctx context.Context, actor ActorContext, bookingID string) error {
	return s.Work.Do(ctx, actor, "schedulingservice", func(ctx context.Context, repositories Repositories) error {
		session, booking, err := lockClassBooking(ctx, repositories, domain.BookingID(bookingID))
		if err != nil {
			return err
		}
		before := booking.Aggregate.Snapshot().Status
		if payrollEligible(before) {
			return nil
		}
		if session.Aggregate.Snapshot().Status == domain.ClassCancelled || before == domain.BookingCancelled || before == domain.BookingReversed {
			return Conflict("booking was cancelled while credit capture was pending")
		}
		if err = booking.Aggregate.Capture(); err != nil {
			return Conflict(err.Error())
		}
		if _, err = repositories.Bookings().Save(ctx, booking, before); err != nil {
			return err
		}
		return publishPayrollCorrection(ctx, repositories, session)
	})
}

func (s *Service) CancelBooking(ctx context.Context, command CancelBookingCommand) (BookingView, error) {
	if _, err := requireUser(command.Actor); err != nil {
		return BookingView{}, err
	}
	id, err := domain.NewBookingID(command.BookingID)
	if err != nil {
		return BookingView{}, Invalid(err.Error())
	}
	return s.cancelBookingDurably(ctx, command.Actor, id, command.Audit.Reason, true)
}

func (s *Service) cancelBookingDurably(ctx context.Context, actor ActorContext, id domain.BookingID, reason string, studentCancellation bool) (BookingView, error) {
	var result BookingView
	err := s.Work.Do(ctx, actor, "schedulingservice", func(ctx context.Context, r Repositories) error {
		first, err := r.Bookings().Get(ctx, id, false)
		if err != nil {
			return err
		}
		if studentCancellation && string(first.Aggregate.Snapshot().StudentID) != actor.UserID {
			return NotFound("booking not found")
		}
		session, err := r.Sessions().Get(ctx, first.Aggregate.Snapshot().SessionID, true)
		if err != nil {
			return err
		}
		booking, err := r.Bookings().Get(ctx, id, true)
		if err != nil {
			return err
		}
		before := booking.Aggregate.Snapshot().Status
		if before != domain.BookingCancelled && before != domain.BookingReversed {
			if studentCancellation {
				err = booking.Aggregate.Cancel(s.Clock.Now(), session.Aggregate.Snapshot().TimeRange)
			} else {
				err = cancelForCompensation(booking.Aggregate)
			}
			if err != nil {
				return Conflict(err.Error())
			}
			if !booking.Aggregate.Snapshot().WalkIn {
				sessionBefore := session.Aggregate.Snapshot().Status
				if err = session.Aggregate.ReleaseSeat(); err != nil {
					return err
				}
				if _, err = r.Sessions().Save(ctx, session, sessionBefore); err != nil {
					return err
				}
			}
			booking, err = r.Bookings().Save(ctx, booking, before)
			if err != nil {
				return err
			}
		}
		if err = enqueueCompensation(ctx, r, booking, reason); err != nil {
			return err
		}
		current, err := r.Bookings().Get(ctx, id, false)
		if err == nil {
			result = current.View()
		}
		return err
	})
	return result, err
}
