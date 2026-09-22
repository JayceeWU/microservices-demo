package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	ordersv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/orders/v1"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/platform"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type paymentOrderClient struct {
	ordersv1.OrderServiceClient
	owner string
}

func (c paymentOrderClient) GetOrder(context.Context, *ordersv1.GetOrderRequest, ...grpc.CallOption) (*ordersv1.Order, error) {
	return &ordersv1.Order{Id: "order-1", UserId: c.owner}, nil
}

func TestPaymentOwnershipDoesNotInheritAdministratorOrderVisibility(t *testing.T) {
	g := gateway{orderClient: paymentOrderClient{owner: "alice"}}
	for _, roles := range [][]string{nil, {"platform_admin"}} {
		ctx := platform.HumanIdentityContext(context.Background(), "bob", "", nil, roles)
		if _, err := g.ownedOrder(ctx, "order-1"); status.Code(err) != codes.NotFound {
			t.Fatalf("unowned payment exposed: %v", err)
		}
	}
	ctx := platform.HumanIdentityContext(context.Background(), "alice", "", nil, nil)
	if _, err := g.ownedOrder(ctx, "order-1"); err != nil {
		t.Fatal(err)
	}
}

func TestSimulationConfigurationIsFailClosed(t *testing.T) {
	for _, value := range []struct {
		environment, mode, flag string
		enabled                 bool
		invalid                 bool
	}{
		{"local", "fake", "true", true, false}, {"test", "fake", "true", true, false},
		{"production", "fake", "true", false, true}, {"local", "stripe", "true", false, true},
		{"local", "fake", "", false, false}, {"production", "stripe", "false", false, false},
	} {
		enabled, err := simulatedPaymentsEnabled(value.environment, value.mode, value.flag)
		if enabled != value.enabled || (err != nil) != value.invalid {
			t.Fatalf("unexpected simulation policy for %+v: %v %v", value, enabled, err)
		}
	}
}

func TestFakeWebhookIsClosedAndOversizedStripeWebhookIsRejected(t *testing.T) {
	g := gateway{}
	t.Setenv("PAYMENT_MODE", "fake")
	response := httptest.NewRecorder()
	g.stripeWebhook(response, httptest.NewRequest(http.MethodPost, "/webhooks/stripe", strings.NewReader("{}")))
	if response.Code != http.StatusNotFound {
		t.Fatalf("fake webhook status %d", response.Code)
	}
	t.Setenv("PAYMENT_MODE", "stripe")
	response = httptest.NewRecorder()
	g.stripeWebhook(response, httptest.NewRequest(http.MethodPost, "/webhooks/stripe", strings.NewReader(strings.Repeat("x", (1<<20)+1))))
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized webhook status %d", response.Code)
	}
}
