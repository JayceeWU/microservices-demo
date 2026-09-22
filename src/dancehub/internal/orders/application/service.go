package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/JayceeWU/microservices-demo/dancehub/internal/orders/domain"
)

type Service struct {
	Work       UnitOfWork
	Catalog    CatalogPort
	Scheduling SchedulingPort
	Credits    CreditPort
	Payments   PaymentPort
	Admission  FlashSaleAdmission
	Clock      Clock
}

func NewService(work UnitOfWork, catalog CatalogPort, scheduling SchedulingPort, credits CreditPort, payments PaymentPort, admission FlashSaleAdmission, clock Clock) *Service {
	return &Service{Work: work, Catalog: catalog, Scheduling: scheduling, Credits: credits, Payments: payments, Admission: admission, Clock: clock}
}

type CreateOrderCommand struct {
	Actor ActorContext
	Items []OrderItemInput
	Audit AuditContext
}
type CreateRoomOrderCommand struct {
	Actor             ActorContext
	RoomReservationID string
	Audit             AuditContext
}
type RefundCommand struct {
	Actor   ActorContext
	OrderID string
	Audit   AuditContext
}
type CancelRoomCommand struct {
	Actor             ActorContext
	RoomReservationID string
	Audit             AuditContext
}
type SagaCommand struct {
	PaidAt                   time.Time
	Actor                    ActorContext
	OrderID, EventID, Reason string
	Event                    domain.PaymentEvent
}
type ReserveFlashSaleCommand struct {
	Actor      ActorContext
	CampaignID string
	Audit      AuditContext
}

func (s *Service) CreateOrder(ctx context.Context, command CreateOrderCommand) (OrderView, error) {
	if err := requireUser(command.Actor); err != nil {
		return OrderView{}, err
	}
	if command.Audit.IdempotencyKey == "" || len(command.Items) == 0 {
		return OrderView{}, Invalid("items and idempotency_key are required")
	}
	domainLines := make([]domain.CreditProductLine, 0, len(command.Items))
	frozen := make([]FrozenLine, 0, len(command.Items))
	for _, input := range command.Items {
		if input.Quantity < 1 || input.Quantity > 20 {
			return OrderView{}, Invalid("quantity must be between 1 and 20")
		}
		product, err := s.Catalog.Product(ctx, command.Actor, input.ProductVersionID)
		if err != nil {
			return OrderView{}, Conflict("product is unavailable")
		}
		money, err := domain.NewMoney(product.AmountCents)
		if err != nil {
			return OrderView{}, Invalid(err.Error())
		}
		domainLines = append(domainLines, domain.CreditProductLine{ProductVersionID: product.ProductVersionID, Description: product.Name, UnitPrice: money, Count: input.Quantity, IsFinalSale: product.FinalSale, Scope: domain.IssuerScope(product.IssuerScope), StudioID: product.StudioID})
		frozen = append(frozen, freezeProduct(product))
	}
	aggregate, err := (domain.OrderFactory{}).Membership("", command.Actor.UserID, domainLines, s.Clock.Now().Add(15*time.Minute))
	if err != nil {
		return OrderView{}, Conflict(err.Error())
	}
	return s.persistNewOrder(ctx, command.Actor, aggregate, frozen, command.Audit.IdempotencyKey)
}

func (s *Service) CreateRoomOrder(ctx context.Context, command CreateRoomOrderCommand) (OrderView, error) {
	if err := requireUser(command.Actor); err != nil {
		return OrderView{}, err
	}
	if command.RoomReservationID == "" || command.Audit.IdempotencyKey == "" {
		return OrderView{}, Invalid("room_reservation_id and idempotency_key are required")
	}
	reservation, err := s.Scheduling.Room(ctx, command.Actor, command.RoomReservationID)
	if err != nil || reservation.StudentID != command.Actor.UserID || reservation.Status != "HOLD" || !reservation.HoldExpiresAt.After(s.Clock.Now()) {
		return OrderView{}, Conflict("room hold is missing or expired")
	}
	money, err := domain.NewMoney(reservation.AmountCents)
	if err != nil {
		return OrderView{}, Invalid(err.Error())
	}
	aggregate, err := (domain.OrderFactory{}).Room("", command.Actor.UserID, reservation.StudioID, domain.RoomReservationLine{ReservationID: reservation.ID, Description: "Room reservation", UnitPrice: money}, reservation.HoldExpiresAt)
	if err != nil {
		return OrderView{}, Conflict(err.Error())
	}
	frozen := []FrozenLine{{RoomReservationID: reservation.ID, StudioID: reservation.StudioID, AmountCents: reservation.AmountCents, Name: "Room reservation"}}
	return s.persistNewOrder(ctx, command.Actor, aggregate, frozen, command.Audit.IdempotencyKey)
}

func (s *Service) persistNewOrder(ctx context.Context, actor ActorContext, aggregate *domain.Order, frozen []FrozenLine, idempotencyKey string) (OrderView, error) {
	var result OrderView
	err := s.Work.Do(ctx, actor, "orderservice", func(ctx context.Context, repositories Repositories) error {
		existing, err := repositories.Orders().FindByIdempotencyKey(ctx, actor.UserID, idempotencyKey)
		if err == nil {
			result = existing.View()
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		record, err := repositories.Orders().Add(ctx, aggregate, idempotencyKey, frozen)
		if err != nil {
			return err
		}
		result = record.View()
		return nil
	})
	return result, err
}

func (s *Service) GetOrder(ctx context.Context, actor ActorContext, id string) (OrderView, error) {
	var result OrderView
	err := s.Work.Do(ctx, actor, "orderservice", func(ctx context.Context, repositories Repositories) error {
		record, err := repositories.Orders().Get(ctx, id, false)
		if err == nil {
			result = record.View()
		}
		return err
	})
	return result, err
}

func (s *Service) refundInfo(ctx context.Context, actor ActorContext, orderID string) (RefundInfo, error) {
	var result RefundInfo
	err := s.Work.Do(ctx, actor, "orderservice", func(ctx context.Context, repositories Repositories) error {
		var err error
		result, err = repositories.Orders().RefundInfo(ctx, orderID)
		return err
	})
	return result, err
}

func validateMembershipRefund(info RefundInfo) error {
	if info.FinalSale {
		return Conflict("final-sale cards cannot be refunded")
	}
	if info.HasNonCreditLine {
		return Conflict("room order is not a membership refund")
	}
	if info.Status != domain.Fulfilled && info.Status != domain.RefundPending {
		return Conflict("order status is not refundable")
	}
	return nil
}

func freezeProduct(product ProductSnapshot) FrozenLine {
	return FrozenLine{ProductVersionID: product.ProductVersionID, StudioID: product.StudioID, IssuerScope: product.IssuerScope, Kind: product.Kind, Name: product.Name, AmountCents: product.AmountCents, CreditAmount: product.CreditAmount, ValidityDays: product.ValidityDays, FinalSale: product.FinalSale}
}

func requireUser(actor ActorContext) error {
	if strings.TrimSpace(actor.UserID) == "" {
		return fmt.Errorf("%w: user identity is required", ErrUnauthenticated)
	}
	return nil
}

func validAudit(audit AuditContext, reason bool) bool {
	return strings.TrimSpace(audit.IdempotencyKey) != "" && (!reason || strings.TrimSpace(audit.Reason) != "")
}
