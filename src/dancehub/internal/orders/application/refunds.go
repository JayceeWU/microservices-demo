package application

import (
	"context"

	"github.com/JayceeWU/microservices-demo/dancehub/internal/orders/domain"
)

func (s *Service) RequestRefund(ctx context.Context, command RefundCommand) (OrderView, error) {
	if err := requireUser(command.Actor); err != nil {
		return OrderView{}, err
	}
	if !validAudit(command.Audit, true) {
		return OrderView{}, Invalid("reason and idempotency_key are required")
	}
	return s.beginRefund(ctx, command, "REQUEST", "CREDIT", false)
}
func (s *Service) ApproveRefund(ctx context.Context, command RefundCommand) (OrderView, error) {
	if !command.Actor.HasTenantRole("studio_admin") && !command.Actor.IsPlatformAdmin() {
		return OrderView{}, Denied("administrator role required")
	}
	if !validAudit(command.Audit, true) {
		return OrderView{}, Invalid("reason and idempotency_key are required")
	}
	return s.beginRefund(ctx, command, "APPROVE", "CREDIT", false)
}
func (s *Service) RejectRefund(ctx context.Context, command RefundCommand) (OrderView, error) {
	if !command.Actor.HasTenantRole("studio_admin") && !command.Actor.IsPlatformAdmin() {
		return OrderView{}, Denied("administrator role required")
	}
	if !validAudit(command.Audit, true) {
		return OrderView{}, Invalid("reason and idempotency_key are required")
	}
	return s.beginRefund(ctx, command, "REJECT", "CREDIT", false)
}

func (s *Service) validateAdminRefund(ctx context.Context, command RefundCommand) (RefundInfo, error) {
	if !command.Actor.HasTenantRole("studio_admin") && !command.Actor.IsPlatformAdmin() {
		return RefundInfo{}, Denied("administrator role required")
	}
	if !validAudit(command.Audit, true) {
		return RefundInfo{}, Invalid("reason and idempotency_key are required")
	}
	info, err := s.refundInfo(ctx, command.Actor, command.OrderID)
	if err != nil {
		return RefundInfo{}, Conflict(err.Error())
	}
	if !command.Actor.IsPlatformAdmin() && (command.Actor.StudioID == "" || command.Actor.StudioID != info.StudioID) {
		return RefundInfo{}, Denied("select the issuing studio")
	}
	if err = validateMembershipRefund(info); err != nil {
		return RefundInfo{}, err
	}
	return info, nil
}

func (s *Service) CancelRoomReservation(ctx context.Context, command CancelRoomCommand) (OrderView, error) {
	if err := requireUser(command.Actor); err != nil {
		return OrderView{}, err
	}
	if !validAudit(command.Audit, true) {
		return OrderView{}, Invalid("reason and idempotency_key are required")
	}
	reservation, err := s.Scheduling.Room(ctx, command.Actor, command.RoomReservationID)
	if err != nil {
		return OrderView{}, NotFound("room reservation not found")
	}
	admin := command.Actor.HasTenantRole("studio_admin") || command.Actor.IsPlatformAdmin()
	if reservation.StudentID != command.Actor.UserID && !admin {
		return OrderView{}, NotFound("room reservation not found")
	}
	if err = domain.EnsureRoomRefundEligible(s.Clock.Now(), reservation.StartsAt, admin); err != nil {
		return OrderView{}, Conflict("cancellation window is closed")
	}
	var orderID string
	err = s.Work.Do(ctx, command.Actor, "orderservice", func(ctx context.Context, repositories Repositories) error {
		var err error
		orderID, err = repositories.Orders().FindRoomOrder(ctx, command.RoomReservationID)
		return err
	})
	if err != nil {
		return OrderView{}, NotFound("room order not found")
	}
	return s.beginRefund(ctx, RefundCommand{Actor: command.Actor, OrderID: orderID, Audit: command.Audit}, "APPROVE", "ROOM", false)
}
