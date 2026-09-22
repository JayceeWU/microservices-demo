package platform

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestGRPCIdentityMetadataRoundTrip(t *testing.T) {
	incoming := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		"x-user-id", "user-1", "x-studio-id", "studio-1", "x-tenant-roles", "teacher,studio_admin",
		"x-global-roles", "platform_admin", "x-actor-kind", "human",
		"x-request-id", "request-1", "authorization", "Bearer token", "idempotency-key", "key-1",
	))
	_, err := grpcIdentityInterceptor(incoming, nil, &grpc.UnaryServerInfo{}, func(ctx context.Context, _ any) (any, error) {
		outgoing := OutgoingGRPCContext(ctx)
		md, _ := metadata.FromOutgoingContext(outgoing)
		for key, expected := range map[string]string{"x-user-id": "user-1", "x-studio-id": "studio-1", "x-tenant-roles": "teacher,studio_admin", "x-global-roles": "platform_admin", "x-actor-kind": "human", "x-request-id": "request-1", "authorization": "Bearer token", "idempotency-key": "key-1"} {
			if actual := firstMetadata(md, key); actual != expected {
				t.Fatalf("metadata %s = %q, want %q", key, actual, expected)
			}
		}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestGRPCIdentityRejectsMixedServiceAndHumanClaims(t *testing.T) {
	incoming := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		"x-actor-kind", "service", "x-service-principal", "scheduling-worker", "x-user-id", "user-1",
	))
	_, err := grpcIdentityInterceptor(incoming, nil, &grpc.UnaryServerInfo{}, func(context.Context, any) (any, error) {
		t.Fatal("mixed identity reached handler")
		return nil, nil
	})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("mixed identity error = %v, want Unauthenticated", err)
	}
}

func TestGRPCClientPolicyRetriesQueriesAndSetsDeadline(t *testing.T) {
	attempts := 0
	err := grpcClientPolicyInterceptor(context.Background(), "/dancehub.catalog.v1.CatalogService/GetStudio", nil, nil, nil, func(ctx context.Context, _ string, _, _ any, _ *grpc.ClientConn, _ ...grpc.CallOption) error {
		attempts++
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 3*time.Second || time.Until(deadline) < 2*time.Second {
			t.Fatalf("query deadline is not approximately three seconds: %v", deadline)
		}
		if attempts < 3 {
			return status.Error(codes.Unavailable, "retry")
		}
		return nil
	})
	if err != nil || attempts != 3 {
		t.Fatalf("query attempts=%d err=%v, want three successful attempts", attempts, err)
	}
}

func TestGRPCClientPolicyDoesNotRetryNonIdempotentCommand(t *testing.T) {
	attempts := 0
	err := grpcClientPolicyInterceptor(context.Background(), "/dancehub.scheduling.v1.SchedulingService/BookClass", nil, nil, nil, func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
		attempts++
		return status.Error(codes.Unavailable, "do not retry")
	})
	if status.Code(err) != codes.Unavailable || attempts != 1 {
		t.Fatalf("command attempts=%d err=%v, want one unavailable attempt", attempts, err)
	}
}
