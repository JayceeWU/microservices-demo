package grpcadapter

import (
	"context"
	"testing"
	"time"

	"github.com/JayceeWU/microservices-demo/dancehub/internal/orders/application"
	"google.golang.org/grpc/metadata"
)

func TestBackgroundCallsCarryCompleteIdentity(t *testing.T) {
	for _, actor := range []application.ActorContext{
		{UserID: "buyer", ActorKind: "human", RequestID: "fulfillment-request"},
		{ActorKind: "service", ServicePrincipal: "orderservice"},
	} {
		ctx, cancel := outgoing(context.Background(), actor, time.Second)
		md, ok := metadata.FromOutgoingContext(ctx)
		cancel()
		if !ok || len(md.Get("x-request-id")) != 1 || md.Get("x-request-id")[0] == "" {
			t.Fatalf("background RPC missing request identity: %v", md)
		}
		if actor.RequestID != "" && md.Get("x-request-id")[0] != actor.RequestID {
			t.Fatalf("request ID not propagated: %v", md)
		}
		if md.Get("x-actor-kind")[0] != actor.ActorKind {
			t.Fatalf("actor kind not propagated: %v", md)
		}
	}
}
