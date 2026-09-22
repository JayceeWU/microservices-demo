package grpcadapter

import (
	"context"
	"strings"
	"time"

	catalogv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/catalog/v1"
	commonv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/common/v1"
	creditsv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/credits/v1"
	paymentsv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/payments/v1"
	schedulingv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/scheduling/v1"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/orders/application"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/platform"
)

type CatalogAdapter struct {
	Client catalogv1.CatalogServiceClient
}

func (a CatalogAdapter) Product(ctx context.Context, actor application.ActorContext, id string) (application.ProductSnapshot, error) {
	call, cancel := outgoing(ctx, actor, 3*time.Second)
	defer cancel()
	value, err := a.Client.GetProductSnapshot(call, &catalogv1.GetProductSnapshotRequest{ProductVersionId: id})
	if err != nil || value.Version == nil {
		return application.ProductSnapshot{}, err
	}
	v := value.Version
	return application.ProductSnapshot{ProductVersionID: v.Id, StudioID: v.StudioId, IssuerScope: strings.TrimPrefix(v.IssuerScope.String(), "ISSUER_SCOPE_"), Kind: strings.TrimPrefix(v.Kind.String(), "PRODUCT_KIND_"), Name: v.Name, AmountCents: v.AmountCents, CreditAmount: v.CreditAmount, ValidityDays: v.ValidityDays, FinalSale: v.FinalSale}, nil
}

func (a CatalogAdapter) Campaign(ctx context.Context, actor application.ActorContext, id string) (application.CampaignSnapshot, error) {
	call, cancel := outgoing(ctx, actor, 3*time.Second)
	defer cancel()
	v, err := a.Client.GetCampaign(call, &catalogv1.GetCampaignRequest{Id: id})
	if err != nil {
		return application.CampaignSnapshot{}, err
	}
	return application.CampaignSnapshot{ID: v.Id, StudioID: v.StudioId, ProductVersionID: v.ProductVersionId, Inventory: v.Inventory, PerUserLimit: v.PerUserLimit, PaymentTTLMinutes: v.PaymentTtlMinutes, Name: v.Name, StartsAt: v.StartsAt.AsTime(), EndsAt: v.EndsAt.AsTime(), Active: v.Active}, nil
}

type SchedulingAdapter struct {
	Client schedulingv1.SchedulingServiceClient
}

func (a SchedulingAdapter) Room(ctx context.Context, actor application.ActorContext, id string) (application.RoomReservationSnapshot, error) {
	call, cancel := outgoing(ctx, actor, 3*time.Second)
	defer cancel()
	v, err := a.Client.GetRoomReservation(call, &schedulingv1.GetRoomReservationRequest{RoomReservationId: id})
	if err != nil {
		return application.RoomReservationSnapshot{}, err
	}
	return application.RoomReservationSnapshot{ID: v.Id, StudioID: v.StudioId, StudentID: v.StudentId, StartsAt: v.GetTimeRange().GetStartsAt().AsTime(), EndsAt: v.GetTimeRange().GetEndsAt().AsTime(), HoldExpiresAt: v.HoldExpiresAt.AsTime(), AmountCents: v.GetAmount().GetAmountCents(), Status: strings.TrimPrefix(v.Status.String(), "ROOM_RESERVATION_STATUS_")}, nil
}
func (a SchedulingAdapter) ConfirmRoom(ctx context.Context, actor application.ActorContext, id, orderID string, audit application.AuditContext) error {
	call, cancel := outgoing(ctx, actor, 10*time.Second)
	defer cancel()
	_, err := a.Client.ConfirmRoomReservation(call, &schedulingv1.ConfirmRoomReservationRequest{RoomReservationId: id, OrderId: orderID, Audit: auditProto(audit)})
	return err
}
func (a SchedulingAdapter) ReleaseRoom(ctx context.Context, actor application.ActorContext, id string, audit application.AuditContext) error {
	call, cancel := outgoing(ctx, actor, 10*time.Second)
	defer cancel()
	_, err := a.Client.ReleaseRoomReservation(call, &schedulingv1.ReleaseRoomReservationRequest{RoomReservationId: id, Audit: auditProto(audit)})
	return err
}

type CreditAdapter struct{ Client creditsv1.CreditServiceClient }

func (a CreditAdapter) ChangeRefund(ctx context.Context, userID, orderID, attemptID string, requirements []application.RefundLineRequirement, operation string, audit application.AuditContext) error {
	call, cancel := outgoing(ctx, application.ActorContext{ActorKind: "service", ServicePrincipal: "orderservice"}, 10*time.Second)
	defer cancel()
	lines := make([]*creditsv1.RefundLineRequirement, 0, len(requirements))
	for _, line := range requirements {
		lines = append(lines, &creditsv1.RefundLineRequirement{OrderLineId: line.OrderLineID, Quantity: line.Quantity})
	}
	var err error
	switch operation {
	case "RESERVE":
		_, err = a.Client.ReserveRefund(call, &creditsv1.ReserveRefundRequest{UserId: userID, OrderId: orderID, AttemptId: attemptID, Requirements: lines, Audit: auditProto(audit)})
	case "RELEASE":
		_, err = a.Client.ReleaseRefund(call, &creditsv1.ReleaseRefundRequest{UserId: userID, OrderId: orderID, AttemptId: attemptID, Requirements: lines, Audit: auditProto(audit)})
	case "REVOKE":
		_, err = a.Client.RevokeRefundedGrant(call, &creditsv1.RevokeRefundedGrantRequest{UserId: userID, OrderId: orderID, AttemptId: attemptID, Requirements: lines, Audit: auditProto(audit)})
	}
	return err
}

func (a CreditAdapter) RefundEligibility(ctx context.Context, actor application.ActorContext, orderID, userID string, lineIDs []string, audit application.AuditContext) (bool, string, error) {
	call, cancel := outgoing(ctx, actor, 10*time.Second)
	defer cancel()
	v, err := a.Client.RequestUnusedProductRefund(call, &creditsv1.RequestUnusedProductRefundRequest{OrderId: orderID, UserId: userID, OrderLineIds: lineIDs, Audit: auditProto(audit)})
	if err != nil {
		return false, "", err
	}
	return v.Eligible, v.Reason, nil
}
func (a CreditAdapter) ReserveRefund(ctx context.Context, actor application.ActorContext, orderID, userID string, lineIDs []string, audit application.AuditContext) error {
	call, cancel := outgoing(ctx, actor, 10*time.Second)
	defer cancel()
	_, err := a.Client.ReserveRefund(call, &creditsv1.ReserveRefundRequest{OrderId: orderID, UserId: userID, OrderLineIds: lineIDs, Audit: auditProto(audit)})
	return err
}
func (a CreditAdapter) ReleaseRefund(ctx context.Context, actor application.ActorContext, orderID, userID string, lineIDs []string, audit application.AuditContext) error {
	call, cancel := outgoing(ctx, actor, 10*time.Second)
	defer cancel()
	_, err := a.Client.ReleaseRefund(call, &creditsv1.ReleaseRefundRequest{OrderId: orderID, UserId: userID, OrderLineIds: lineIDs, Audit: auditProto(audit)})
	return err
}
func (a CreditAdapter) RevokeRefunded(ctx context.Context, actor application.ActorContext, orderID, userID string, lineIDs []string, audit application.AuditContext) error {
	call, cancel := outgoing(ctx, actor, 10*time.Second)
	defer cancel()
	_, err := a.Client.RevokeRefundedGrant(call, &creditsv1.RevokeRefundedGrantRequest{OrderId: orderID, UserId: userID, OrderLineIds: lineIDs, Audit: auditProto(audit)})
	return err
}
func (a CreditAdapter) Grant(ctx context.Context, actor application.ActorContext, orderLineID, userID string, line application.FrozenLine, audit application.AuditContext) (string, error) {
	call, cancel := outgoing(ctx, actor, 10*time.Second)
	defer cancel()
	scope := creditsv1.IssuerScope_ISSUER_SCOPE_PLATFORM
	if line.IssuerScope == "STUDIO" {
		scope = creditsv1.IssuerScope_ISSUER_SCOPE_STUDIO
	}
	entitlement := &creditsv1.EntitlementSnapshot{StudioId: line.StudioID, IssuerScope: scope, FinalSale: line.FinalSale}
	if line.Kind == "UNLIMITED" {
		entitlement.Entitlement = &creditsv1.EntitlementSnapshot_Unlimited{Unlimited: &creditsv1.UnlimitedEntitlement{ValidityDays: line.ValidityDays}}
	} else {
		entitlement.Entitlement = &creditsv1.EntitlementSnapshot_Credits{Credits: &creditsv1.CreditsEntitlement{Amount: &commonv1.CreditAmount{Units: line.CreditAmount}, ValidityDays: line.ValidityDays}}
	}
	v, err := a.Client.GrantFromOrder(call, &creditsv1.GrantFromOrderRequest{OrderLineId: orderLineID, ProductVersionId: line.ProductVersionID, UserId: userID, Entitlement: entitlement, Audit: auditProto(audit)})
	if err != nil {
		return "", err
	}
	return v.Id, nil
}

type PaymentAdapter struct {
	Client paymentsv1.PaymentServiceClient
}

func (a PaymentAdapter) GetByOrder(ctx context.Context, actor application.ActorContext, orderID string) (application.PaymentSnapshot, error) {
	call, cancel := outgoing(ctx, application.ActorContext{ActorKind: "service", ServicePrincipal: "orderservice"}, 10*time.Second)
	defer cancel()
	v, err := a.Client.GetPaymentByOrder(call, &paymentsv1.GetPaymentByOrderRequest{OrderId: orderID})
	if err != nil {
		return application.PaymentSnapshot{}, err
	}
	return application.PaymentSnapshot{ID: v.Id, AmountCents: v.AmountCents, Status: v.Status, SucceededAt: v.GetSucceededAt().AsTime()}, nil
}
func (a PaymentAdapter) Refund(ctx context.Context, actor application.ActorContext, id string, amount int64, audit application.AuditContext) error {
	call, cancel := outgoing(ctx, application.ActorContext{ActorKind: "service", ServicePrincipal: "orderservice"}, 10*time.Second)
	defer cancel()
	_, err := a.Client.RefundPayment(call, &paymentsv1.RefundPaymentRequest{Id: id, AmountCents: amount, Audit: auditProto(audit)})
	return err
}

func outgoing(ctx context.Context, actor application.ActorContext, timeout time.Duration) (context.Context, context.CancelFunc) {
	identity := platform.Actor(ctx)
	if actor.RequestID != "" {
		identity.RequestID = actor.RequestID
	} else if identity.RequestID == "" {
		identity.RequestID = "orderservice-worker"
	}
	ctx = platform.WithActor(ctx, identity)
	if actor.ActorKind == "service" {
		ctx = platform.ServiceIdentityContext(ctx, actor.ServicePrincipal)
	} else {
		ctx = platform.HumanIdentityContext(ctx, actor.UserID, actor.StudioID, actor.TenantRoles, actor.GlobalRoles)
	}
	return context.WithTimeout(platform.OutgoingGRPCContext(ctx), timeout)
}
func auditProto(value application.AuditContext) *commonv1.AuditContext {
	return &commonv1.AuditContext{IdempotencyKey: value.IdempotencyKey, Reason: value.Reason}
}
