package catalog

import (
	"context"

	catalogv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/catalog/v1"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/appcore"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/chat/application"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Client struct {
	client catalogv1.CatalogServiceClient
}

func New(client catalogv1.CatalogServiceClient) Client { return Client{client: client} }

func (c Client) RequireStudio(ctx context.Context, actor application.ActorContext, studioID string) error {
	if studioID == "" {
		return appcore.Invalid("studio is required")
	}
	outgoing := platform.OutgoingGRPCContext(platform.HumanIdentityContext(ctx, actor.UserID, actor.StudioID, actor.TenantRoles, actor.GlobalRoles))
	_, err := c.client.GetStudio(outgoing, &catalogv1.GetStudioRequest{Id: studioID})
	if status.Code(err) == codes.NotFound {
		return appcore.NotFound("studio not found")
	}
	return err
}

var _ application.CatalogPort = Client{}
