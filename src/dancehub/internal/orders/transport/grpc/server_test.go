package grpctransport

import (
	"context"
	"testing"
	"time"

	commonv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/common/v1"
	ordersv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/orders/v1"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/orders/application"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/orders/domain"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/platform"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/testsupport"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type orderApplicationStub struct {
	orderApplication
	actor application.ActorContext
}

func (s *orderApplicationStub) GetOrder(_ context.Context, actor application.ActorContext, id string) (application.OrderView, error) {
	s.actor = actor
	if id == "missing" {
		return application.OrderView{}, application.NotFound("order not found")
	}
	return application.OrderView{ID: id, UserID: actor.UserID, StudioID: actor.StudioID, Status: domain.Paid, TotalCents: 2500, PaymentExpiresAt: time.Unix(1, 0)}, nil
}

func (s *orderApplicationStub) ApplySaga(_ context.Context, command application.SagaCommand) (application.OrderView, error) {
	s.actor = command.Actor
	return application.OrderView{ID: command.OrderID, UserID: "user-1", Status: domain.Paid, TotalCents: 2500, PaymentExpiresAt: time.Unix(1, 0)}, nil
}

func TestGetOrderMapsMetadataResultAndErrorsOverGRPC(t *testing.T) {
	stub := &orderApplicationStub{}
	connection := testsupport.GRPCConnection(t, []grpc.ServerOption{grpc.UnaryInterceptor(platform.IdentityUnaryServerInterceptor())}, func(server *grpc.Server) {
		ordersv1.RegisterOrderServiceServer(server, NewServer(stub))
	})
	client := ordersv1.NewOrderServiceClient(connection)
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(
		"x-user-id", "user-1", "x-studio-id", "studio-1", "x-tenant-roles", "student", "x-actor-kind", "human", "x-request-id", "request-1",
	))

	result, err := client.GetOrder(ctx, &ordersv1.GetOrderRequest{Id: "order-1"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Id != "order-1" || result.Total.AmountCents != 2500 || stub.actor.RequestID != "request-1" {
		t.Fatalf("unexpected mapping: result=%v actor=%+v", result, stub.actor)
	}
	_, err = client.GetOrder(ctx, &ordersv1.GetOrderRequest{Id: "missing"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("expected NotFound, got %v", err)
	}
}

func TestPaymentSagaRequiresExactServicePrincipal(t *testing.T) {
	stub := &orderApplicationStub{}
	connection := testsupport.GRPCConnection(t, []grpc.ServerOption{grpc.UnaryInterceptor(platform.IdentityUnaryServerInterceptor())}, func(server *grpc.Server) {
		ordersv1.RegisterOrderServiceServer(server, NewServer(stub))
	})
	client := ordersv1.NewOrderServiceClient(connection)
	request := &ordersv1.MarkPaymentSucceededRequest{OrderId: "order-1", PaymentId: "payment-1", Audit: &commonv1.AuditContext{IdempotencyKey: "event-1"}}

	wrongContext := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(
		"x-actor-kind", "service", "x-service-principal", "scheduling-worker", "x-request-id", "request-1",
	))
	if _, err := client.MarkPaymentSucceeded(wrongContext, request); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("wrong principal error = %v, want PermissionDenied", err)
	}

	paymentContext := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(
		"x-actor-kind", "service", "x-service-principal", "payment-order-saga", "x-request-id", "request-2",
	))
	if _, err := client.MarkPaymentSucceeded(paymentContext, request); err != nil {
		t.Fatal(err)
	}
	if !stub.actor.IsService("payment-order-saga") {
		t.Fatalf("unexpected saga actor: %+v", stub.actor)
	}
}
