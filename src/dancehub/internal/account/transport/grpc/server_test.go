package grpctransport

import (
	"context"
	"testing"

	accountv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/account/v1"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/account/application"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/platform"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/testsupport"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

type accountApplicationStub struct {
	accountApplication
	actor application.ActorContext
}

func (s *accountApplicationStub) GetMyProfile(_ context.Context, actor application.ActorContext) (application.ProfileView, error) {
	s.actor = actor
	return application.ProfileView{
		UserID: actor.UserID, Email: "teacher@bayareadancehub.local", DisplayName: "Teacher One",
		AvatarURL: "https://example.com/a.png", Timezone: "America/Los_Angeles", Bio: "bio", PortfolioURL: "https://example.com",
	}, nil
}

func TestGetMyProfileMapsTrustedMetadataOverGRPC(t *testing.T) {
	stub := &accountApplicationStub{}
	connection := testsupport.GRPCConnection(t, []grpc.ServerOption{grpc.UnaryInterceptor(platform.IdentityUnaryServerInterceptor())}, func(server *grpc.Server) {
		accountv1.RegisterAccountServiceServer(server, NewServer(stub))
	})
	client := accountv1.NewAccountServiceClient(connection)
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(
		"x-user-id", "teacher-1", "x-studio-id", "studio-1", "x-tenant-roles", "teacher,studio_admin", "x-actor-kind", "human", "x-request-id", "request-2",
	))

	result, err := client.GetMyProfile(ctx, &accountv1.GetMyProfileRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Id != "teacher-1" || result.DisplayName != "Teacher One" || stub.actor.UserID != "teacher-1" || !stub.actor.HasTenantRole("studio_admin") {
		t.Fatalf("unexpected mapping: result=%v actor=%+v", result, stub.actor)
	}
}
