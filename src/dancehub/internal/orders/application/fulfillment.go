package application

import (
	"context"
	"fmt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/JayceeWU/microservices-demo/dancehub/internal/orders/domain"
)

func (s *Service) ApplySaga(ctx context.Context, command SagaCommand) (OrderView, error) {
	if command.EventID == "" {
		return OrderView{}, Invalid("event idempotency key is required")
	}
	var result OrderView
	err := s.Work.Do(ctx, command.Actor, "orderservice", func(ctx context.Context, repositories Repositories) error {
		added, err := repositories.Inbox().TryAdd(ctx, command.EventID, "orders-saga")
		if err != nil {
			return err
		}
		record, err := repositories.Orders().Get(ctx, command.OrderID, added)
		if err != nil {
			return err
		}
		if !added {
			result = record.View()
			return nil
		}
		if command.Event == domain.PaymentSucceededEvent && record.View().Status != domain.RefundPending && record.View().Status != domain.Refunded {
			paidAt := command.PaidAt
			if paidAt.IsZero() {
				paidAt = s.Clock.Now()
			}
			if extra, ok := repositories.(RefundRepositories); ok {
				if err = extra.Refunds().SetPaidAt(ctx, command.OrderID, paidAt); err != nil {
					return err
				}
				state := record.View().Status
				if (state == domain.PendingPayment || state == domain.PaymentFailed || state == domain.Expired) && !paidAt.Before(record.View().PaymentExpiresAt) {
					kind := "UNFULFILLED"
					for _, line := range record.Lines {
						if line.Type == "ROOM_RESERVATION" {
							kind = "ROOM"
						}
					}
					c := RefundCommand{Actor: command.Actor, OrderID: command.OrderID, Audit: AuditContext{IdempotencyKey: "system-refund:" + command.OrderID, Reason: "payment arrived after the order deadline"}}
					if err = extra.Refunds().Begin(ctx, record, c, "APPROVE", kind, true); err != nil {
						return err
					}
					record, err = repositories.Orders().Get(ctx, command.OrderID, false)
					if err == nil {
						result = record.View()
					}
					return err
				}
			}
		}
		if record.View().Status == domain.RefundPending || record.View().Status == domain.Refunded {
			result = record.View()
			return nil
		}
		before := record.Aggregate.Snapshot().Status
		if err = (domain.PaymentSaga{}).Apply(record.Aggregate, command.Event); err != nil {
			return Conflict(err.Error())
		}
		record, err = repositories.Orders().Save(ctx, record, before)
		if err != nil {
			return err
		}
		result = record.View()
		return nil
	})
	return result, err
}

func (s *Service) transition(ctx context.Context, actor ActorContext, orderID string, target domain.Status) (OrderView, error) {
	var result OrderView
	err := s.Work.Do(ctx, actor, "orderservice", func(ctx context.Context, repositories Repositories) error {
		record, err := repositories.Orders().Get(ctx, orderID, true)
		if err != nil {
			return err
		}
		before := record.Aggregate.Snapshot().Status
		switch target {
		case domain.Fulfilled:
			err = record.Aggregate.RejectRefund()
		case domain.Refunded:
			err = record.Aggregate.MarkExternallyRefunded()
		default:
			err = Conflict("unsupported order transition")
		}
		if err != nil {
			return Conflict(err.Error())
		}
		record, err = repositories.Orders().Save(ctx, record, before)
		if err != nil {
			return err
		}
		result = record.View()
		return nil
	})
	return result, err
}

func (s *Service) ClaimAndFulfillOne(ctx context.Context) error {
	actor := ActorContext{ActorKind: "service", ServicePrincipal: "orderservice", RequestID: "order-fulfillment"}
	var record *OrderRecord
	err := s.Work.Do(ctx, actor, "orderservice", func(ctx context.Context, repositories Repositories) error {
		var err error
		record, err = repositories.Orders().ClaimFulfillment(ctx)
		return err
	})
	if err != nil || record == nil {
		return err
	}
	view := record.View()
	student := ActorContext{UserID: view.UserID, StudioID: view.StudioID, TenantRoles: []string{"student"}, ActorKind: "human", RequestID: actor.RequestID}
	for _, line := range view.Lines {
		if line.Type == "CREDIT_PRODUCT" {
			for index := int32(0); index < line.Quantity; index++ {
				key := fmt.Sprintf("fulfill:%s:%d", line.ID, index)
				_, err := s.Credits.Grant(ctx, student, line.ID+fmt.Sprintf(":%d", index), view.UserID, line.Snapshot, AuditContext{IdempotencyKey: key, Reason: "order fulfillment"})
				if err != nil {
					return err
				}
			}
		} else if err := s.Scheduling.ConfirmRoom(ctx, student, line.RoomReservationID, view.ID, AuditContext{IdempotencyKey: "fulfill:" + view.ID, Reason: "payment succeeded"}); err != nil {
			if status.Code(err) == codes.FailedPrecondition || status.Code(err) == codes.NotFound {
				return s.compensateOrder(ctx, actor, record, "paid room reservation can no longer be fulfilled")
			}
			return err
		}
	}
	return s.completeFulfillment(ctx, student, view.ID)
}

func (s *Service) completeFulfillment(ctx context.Context, actor ActorContext, orderID string) error {
	return s.Work.Do(ctx, actor, "orderservice", func(ctx context.Context, repositories Repositories) error {
		record, err := repositories.Orders().Get(ctx, orderID, true)
		if err != nil {
			return err
		}
		before := record.Aggregate.Snapshot().Status
		if err = record.Aggregate.Fulfill(); err != nil {
			return Conflict(err.Error())
		}
		_, err = repositories.Orders().Save(ctx, record, before)
		return err
	})
}
