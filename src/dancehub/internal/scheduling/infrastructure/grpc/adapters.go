package grpcadapter

import (
	"context"
	"time"

	catalogv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/catalog/v1"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/creditclient"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/platform"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/scheduling/application"
)

type CreditAdapter struct{ Client *creditclient.Client }

func (a CreditAdapter) CompensateBooking(ctx context.Context, userID, studioID, bookingID string, audit application.AuditContext) error {
	return a.Client.CompensateBooking(ctx, userID, studioID, bookingID, audit.IdempotencyKey, audit.Reason)
}

func (a CreditAdapter) PlaceHold(ctx context.Context, actor application.ActorContext, studioID, bookingID string, amount int32, startsAt time.Time, audit application.AuditContext) (string, error) {
	hold, err := a.Client.PlaceHold(identity(ctx, actor), actor.UserID, studioID, bookingID, int(amount), startsAt, audit.IdempotencyKey)
	return hold.ID, err
}
func (a CreditAdapter) Capture(ctx context.Context, actor application.ActorContext, holdID string, audit application.AuditContext) error {
	return a.Client.Capture(identity(ctx, actor), actor.UserID, holdID, audit.IdempotencyKey, audit.Reason)
}
func (a CreditAdapter) Release(ctx context.Context, actor application.ActorContext, holdID string, audit application.AuditContext) error {
	return a.Client.Release(identity(ctx, actor), actor.UserID, holdID, audit.IdempotencyKey, audit.Reason)
}
func (a CreditAdapter) Reverse(ctx context.Context, actor application.ActorContext, holdID string, audit application.AuditContext) error {
	return a.Client.Reverse(identity(ctx, actor), actor.UserID, holdID, audit.IdempotencyKey, audit.Reason)
}

type CatalogAdapter struct {
	Client catalogv1.CatalogServiceClient
}

func (a CatalogAdapter) GetStudioTimezone(ctx context.Context, actor application.ActorContext, studioID string) (string, error) {
	call, cancel := context.WithTimeout(platform.OutgoingGRPCContext(identity(ctx, actor)), 3*time.Second)
	defer cancel()
	studio, err := a.Client.GetStudio(call, &catalogv1.GetStudioRequest{Id: studioID})
	if err != nil {
		return "", err
	}
	return studio.Timezone, nil
}

func (a CatalogAdapter) GetRoom(ctx context.Context, actor application.ActorContext, roomID, studioID string) (application.RoomSnapshot, error) {
	call, cancel := context.WithTimeout(platform.OutgoingGRPCContext(identity(ctx, actor)), 3*time.Second)
	defer cancel()
	room, err := a.Client.GetRoom(call, &catalogv1.GetRoomRequest{Id: roomID, StudioId: studioID})
	if err != nil {
		return application.RoomSnapshot{}, err
	}
	return application.RoomSnapshot{ID: room.Id, StudioID: room.StudioId, RentalRateCentsPerHour: room.RentalRateCentsPerHour}, nil
}

func identity(ctx context.Context, actor application.ActorContext) context.Context {
	if actor.ActorKind == "service" {
		return platform.ServiceIdentityContext(ctx, actor.ServicePrincipal)
	}
	return platform.HumanIdentityContext(ctx, actor.UserID, actor.StudioID, actor.TenantRoles, actor.GlobalRoles)
}
