package grpctransport

import (
	"context"
	"testing"

	catalogv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/catalog/v1"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/catalog/application"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/catalog/domain"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/platform"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/testsupport"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

type catalogApplicationStub struct {
	catalogApplication
	actor application.ActorContext
}

func (s *catalogApplicationStub) GetStudio(_ context.Context, actor application.ActorContext, id string) (domain.Studio, error) {
	s.actor = actor
	return domain.Studio{
		ID: id, Slug: "downtown", Name: "Downtown Studio", Description: "A studio", AddressLine: "1 Main St",
		City: "San Jose", State: "CA", PostalCode: "95112", Timezone: "America/Los_Angeles", ImageURL: "https://example.com/s.png",
	}, nil
}

func TestGetStudioMapsTrustedMetadataOverGRPC(t *testing.T) {
	stub := &catalogApplicationStub{}
	connection := testsupport.GRPCConnection(t, []grpc.ServerOption{grpc.UnaryInterceptor(platform.IdentityUnaryServerInterceptor())}, func(server *grpc.Server) {
		catalogv1.RegisterCatalogServiceServer(server, NewServer(stub))
	})
	client := catalogv1.NewCatalogServiceClient(connection)
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(
		"x-user-id", "student-1", "x-studio-id", "studio-1", "x-tenant-roles", "student", "x-actor-kind", "human", "x-request-id", "request-3",
	))

	result, err := client.GetStudio(ctx, &catalogv1.GetStudioRequest{Id: "studio-1"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Id != "studio-1" || result.Name != "Downtown Studio" || stub.actor.UserID != "student-1" || !stub.actor.HasTenantRole("student") {
		t.Fatalf("unexpected mapping: result=%v actor=%+v", result, stub.actor)
	}
}
