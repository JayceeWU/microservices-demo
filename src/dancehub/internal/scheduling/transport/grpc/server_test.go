package grpctransport

import (
	"context"
	"testing"
	"time"

	schedulingv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/scheduling/v1"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/platform"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/scheduling/application"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/scheduling/domain"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/testsupport"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

type schedulingApplicationStub struct {
	schedulingApplication
	actor application.ActorContext
}

func (s *schedulingApplicationStub) GetSession(_ context.Context, actor application.ActorContext, id string) (application.SessionView, error) {
	s.actor = actor
	return application.SessionView{
		ID: id, StudioID: actor.StudioID, RoomID: "room-1", TeacherID: "teacher-1", Title: "Ballet",
		StartsAt: time.Unix(10, 0), EndsAt: time.Unix(3610, 0), Capacity: 20, MinimumStudents: 4,
		CreditCost: 4, ConfirmedCount: 3, Status: domain.ClassOpen,
	}, nil
}

func TestGetClassSessionMapsTrustedMetadataOverGRPC(t *testing.T) {
	stub := &schedulingApplicationStub{}
	connection := testsupport.GRPCConnection(t, []grpc.ServerOption{grpc.UnaryInterceptor(platform.IdentityUnaryServerInterceptor())}, func(server *grpc.Server) {
		schedulingv1.RegisterSchedulingServiceServer(server, NewServer(stub))
	})
	client := schedulingv1.NewSchedulingServiceClient(connection)
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(
		"x-user-id", "teacher-1", "x-studio-id", "studio-1", "x-tenant-roles", "teacher,studio_admin", "x-actor-kind", "human", "x-request-id", "request-2",
	))

	result, err := client.GetClassSession(ctx, &schedulingv1.GetClassSessionRequest{Id: "class-1"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Id != "class-1" || result.CreditCost.Units != 4 || stub.actor.UserID != "teacher-1" || !stub.actor.HasTenantRole("studio_admin") {
		t.Fatalf("unexpected mapping: result=%v actor=%+v", result, stub.actor)
	}
}
