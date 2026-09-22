package application

import (
	"context"
	"log/slog"
	"time"

	"github.com/JayceeWU/microservices-demo/dancehub/internal/scheduling/domain"
)

type CreditCompensation struct {
	BookingID, UserID, StudioID, Reason, LeaseToken string
	Attempts                                        int
}
type CompensationRepository interface {
	Enqueue(context.Context, *BookingRecord, string) error
	Claim(context.Context) (*CreditCompensation, error)
	Finish(context.Context, *CreditCompensation, string) error
}
type CompensationRepositories interface{ Compensations() CompensationRepository }
type BookingCompensator interface {
	CompensateBooking(context.Context, string, string, string, AuditContext) error
}

func enqueueCompensation(ctx context.Context, repositories Repositories, booking *BookingRecord, reason string) error {
	return repositories.(CompensationRepositories).Compensations().Enqueue(ctx, booking, reason)
}

func (w Worker) compensateOnce(ctx context.Context, actor ActorContext) error {
	for i := 0; i < 50; i++ {
		var job *CreditCompensation
		err := w.Service.Work.Do(ctx, actor, "scheduling-worker", func(ctx context.Context, r Repositories) error {
			var err error
			job, err = r.(CompensationRepositories).Compensations().Claim(ctx)
			return err
		})
		if err != nil || job == nil {
			return err
		}
		call, cancel := context.WithTimeout(ctx, 10*time.Second)
		err = w.Service.Credits.(BookingCompensator).CompensateBooking(call, job.UserID, job.StudioID, job.BookingID, AuditContext{IdempotencyKey: "cancel:" + job.BookingID, Reason: job.Reason})
		cancel()
		message := ""
		if err != nil {
			message = err.Error()
			slog.Warn("booking compensation pending", "booking_id", job.BookingID, "error", err)
		}
		if err = w.Service.Work.Do(ctx, actor, "scheduling-worker", func(ctx context.Context, r Repositories) error {
			return r.(CompensationRepositories).Compensations().Finish(ctx, job, message)
		}); err != nil {
			return err
		}
	}
	return nil
}

func cancelForCompensation(booking *domain.Booking) error {
	switch booking.Snapshot().Status {
	case domain.BookingCancelled, domain.BookingReversed:
		return nil
	case domain.BookingPendingCredit, domain.BookingConfirmed:
		return booking.CancelByClass()
	default:
		return booking.Reverse()
	}
}
