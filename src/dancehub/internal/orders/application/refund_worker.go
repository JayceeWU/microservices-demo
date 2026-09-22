package application

import (
	"context"
	"fmt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"log/slog"
	"time"
)

type RefundAttempt struct {
	ID, OrderID, Kind, Phase, Decision, Reason, LeaseToken string
	Attempts                                               int
}
type RefundRepository interface {
	Begin(context.Context, *OrderRecord, RefundCommand, string, string, bool) error
	Claim(context.Context) (*RefundAttempt, error)
	Advance(context.Context, *RefundAttempt, string, string) error
	ExpirePending(context.Context) error
	SetPaidAt(context.Context, string, time.Time) error
}
type RefundRepositories interface{ Refunds() RefundRepository }
type AtomicRefundPort interface {
	ChangeRefund(context.Context, string, string, string, []RefundLineRequirement, string, AuditContext) error
}

func (s *Service) beginRefund(ctx context.Context, c RefundCommand, decision, kind string, system bool) (OrderView, error) {
	var result OrderView
	err := s.Work.Do(ctx, c.Actor, "orderservice", func(ctx context.Context, r Repositories) error {
		record, err := r.Orders().Get(ctx, c.OrderID, true)
		if err != nil {
			return err
		}
		view := record.View()
		if !system {
			if decision == "REQUEST" && view.UserID != c.Actor.UserID {
				return NotFound("order not found")
			}
			if kind == "CREDIT" && decision != "REQUEST" && !c.Actor.IsPlatformAdmin() && (c.Actor.StudioID == "" || c.Actor.StudioID != view.StudioID) {
				return Denied("select the issuing studio")
			}
			if kind == "CREDIT" {
				for _, line := range view.Lines {
					if line.Type != "CREDIT_PRODUCT" || line.FinalSale {
						return Conflict("order is not refundable")
					}
				}
			}
		}
		if err = r.(RefundRepositories).Refunds().Begin(ctx, record, c, decision, kind, system); err != nil {
			return err
		}
		record, err = r.Orders().Get(ctx, c.OrderID, false)
		if err == nil {
			result = record.View()
		}
		return err
	})
	return result, err
}

func refundRequirements(record *OrderRecord) []RefundLineRequirement {
	result := make([]RefundLineRequirement, 0, len(record.Lines))
	for _, line := range record.Lines {
		result = append(result, RefundLineRequirement{line.ID, line.Quantity})
	}
	return result
}

func (s *Service) ProcessRefunds(ctx context.Context) error {
	actor := ActorContext{ActorKind: "service", ServicePrincipal: "orderservice", RequestID: "order-refund-worker"}
	if err := s.Work.Do(ctx, actor, "orderservice", func(ctx context.Context, r Repositories) error {
		return r.(RefundRepositories).Refunds().ExpirePending(ctx)
	}); err != nil {
		return err
	}
	for i := 0; i < 20; i++ {
		var job *RefundAttempt
		var record *OrderRecord
		err := s.Work.Do(ctx, actor, "orderservice", func(ctx context.Context, r Repositories) error {
			var err error
			job, err = r.(RefundRepositories).Refunds().Claim(ctx)
			if err != nil || job == nil {
				return err
			}
			record, err = r.Orders().Get(ctx, job.OrderID, false)
			return err
		})
		if err != nil || job == nil {
			return err
		}
		phase, runErr := s.runRefundPhase(ctx, actor, job, record)
		message := ""
		if runErr != nil {
			message = runErr.Error()
			phase = job.Phase
			if job.Phase == "RESERVING" && (status.Code(runErr) == codes.FailedPrecondition || status.Code(runErr) == codes.InvalidArgument) {
				phase = "DENIED"
			}
			slog.Warn("refund attempt pending", "attempt_id", job.ID, "phase", phase, "error", runErr)
		}
		if err = s.Work.Do(ctx, actor, "orderservice", func(ctx context.Context, r Repositories) error {
			return r.(RefundRepositories).Refunds().Advance(ctx, job, phase, message)
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) runRefundPhase(ctx context.Context, actor ActorContext, j *RefundAttempt, r *OrderRecord) (string, error) {
	call, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	audit := AuditContext{IdempotencyKey: "refund:" + j.OrderID, Reason: j.Reason}
	change := func(op string) error {
		return s.Credits.(AtomicRefundPort).ChangeRefund(call, r.View().UserID, j.OrderID, j.ID, refundRequirements(r), op, audit)
	}
	switch j.Phase {
	case "RESERVING":
		if err := change("RESERVE"); err != nil {
			return "", err
		}
		if j.Decision == "APPROVE" {
			return "REFUNDING", nil
		}
		return "AWAITING_APPROVAL", nil
	case "REFUNDING":
		payment, err := s.Payments.GetByOrder(call, actor, j.OrderID)
		if err != nil {
			return "", err
		}
		if err = s.Payments.Refund(call, actor, payment.ID, r.View().TotalCents, audit); err != nil {
			return "", err
		}
		return "REVOKING", nil
	case "REVOKING":
		if j.Kind == "CREDIT" {
			if err := change("REVOKE"); err != nil {
				return "", err
			}
		}
		if j.Kind == "ROOM" {
			for _, line := range r.Lines {
				if line.RoomReservationID != "" {
					if err := s.Scheduling.ReleaseRoom(call, actor, line.RoomReservationID, audit); err != nil {
						return "", err
					}
				}
			}
		}
		return "COMPLETED", nil
	case "RELEASING":
		if err := change("RELEASE"); err != nil {
			return "", err
		}
		return "REJECTED", nil
	default:
		return "", fmt.Errorf("unsupported refund phase %s", j.Phase)
	}
}

func (s *Service) compensateOrder(ctx context.Context, actor ActorContext, record *OrderRecord, reason string) error {
	kind := "UNFULFILLED"
	for _, line := range record.Lines {
		if line.Type == "ROOM_RESERVATION" {
			kind = "ROOM"
		}
	}
	_, err := s.beginRefund(ctx, RefundCommand{Actor: actor, OrderID: record.View().ID, Audit: AuditContext{IdempotencyKey: "system-refund:" + record.View().ID, Reason: reason}}, "APPROVE", kind, true)
	return err
}
