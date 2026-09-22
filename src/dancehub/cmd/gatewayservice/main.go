package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"

	accountv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/account/v1"
	cartv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/cart/v1"
	catalogv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/catalog/v1"
	chatv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/chat/v1"
	creditsv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/credits/v1"
	ordersv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/orders/v1"
	paymentsv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/payments/v1"
	payrollv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/payroll/v1"
	recommendationsv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/recommendations/v1"
	schedulingv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/scheduling/v1"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/platform"
	"github.com/coreos/go-oidc/v3/oidc"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

type gateway struct {
	client               *http.Client
	collectorURL         string
	paymentWebhookURL    *url.URL
	authMode             string
	verifier             *oidc.IDTokenVerifier
	cart                 cartv1.CartServiceClient
	accountClient        accountv1.AccountServiceClient
	catalogClient        catalogv1.CatalogServiceClient
	payrollClient        payrollv1.PayrollServiceClient
	creditClient         creditsv1.CreditServiceClient
	scheduleClient       schedulingv1.SchedulingServiceClient
	orderClient          ordersv1.OrderServiceClient
	paymentClient        paymentsv1.PaymentServiceClient
	recommendationClient recommendationsv1.RecommendationServiceClient
	chatClient           chatv1.ChatServiceClient
	grpcConns            []*grpc.ClientConn
}

func main() {
	if _, err := simulatedPaymentsEnabled(os.Getenv("ENVIRONMENT"), os.Getenv("PAYMENT_MODE"), os.Getenv("ENABLE_SIMULATED_PAYMENTS")); err != nil {
		log.Fatal(err)
	}
	configureBrowserOrigins(platform.MustEnv("WEB_ORIGINS"))
	authMode := platform.MustEnv("AUTH_MODE")
	if err := validateAuthMode(os.Getenv("ENVIRONMENT"), authMode); err != nil {
		log.Fatal(err)
	}
	paymentWebhookURL, err := url.Parse(platform.MustEnv("PAYMENT_WEBHOOK_URL"))
	if err != nil || paymentWebhookURL.Scheme == "" || paymentWebhookURL.Host == "" {
		log.Fatal("PAYMENT_WEBHOOK_URL must be an absolute URL")
	}
	g := &gateway{
		client:            &http.Client{Timeout: 12 * time.Second, Transport: otelhttp.NewTransport(http.DefaultTransport)},
		collectorURL:      platform.MustEnv("OTEL_COLLECTOR_HTTP_URL"),
		paymentWebhookURL: paymentWebhookURL,
		authMode:          authMode,
	}
	for _, target := range []struct {
		address string
		assign  func(*grpc.ClientConn)
	}{
		{platform.MustEnv("ACCOUNT_SERVICE_ADDR"), func(conn *grpc.ClientConn) { g.accountClient = accountv1.NewAccountServiceClient(conn) }},
		{platform.MustEnv("CATALOG_SERVICE_ADDR"), func(conn *grpc.ClientConn) { g.catalogClient = catalogv1.NewCatalogServiceClient(conn) }},
		{platform.MustEnv("PAYROLL_SERVICE_ADDR"), func(conn *grpc.ClientConn) { g.payrollClient = payrollv1.NewPayrollServiceClient(conn) }},
		{platform.MustEnv("CREDIT_SERVICE_ADDR"), func(conn *grpc.ClientConn) { g.creditClient = creditsv1.NewCreditServiceClient(conn) }},
		{platform.MustEnv("SCHEDULING_SERVICE_ADDR"), func(conn *grpc.ClientConn) { g.scheduleClient = schedulingv1.NewSchedulingServiceClient(conn) }},
		{platform.MustEnv("ORDER_SERVICE_ADDR"), func(conn *grpc.ClientConn) { g.orderClient = ordersv1.NewOrderServiceClient(conn) }},
		{platform.MustEnv("PAYMENT_SERVICE_ADDR"), func(conn *grpc.ClientConn) { g.paymentClient = paymentsv1.NewPaymentServiceClient(conn) }},
		{platform.MustEnv("RECOMMENDATION_SERVICE_ADDR"), func(conn *grpc.ClientConn) {
			g.recommendationClient = recommendationsv1.NewRecommendationServiceClient(conn)
		}},
		{platform.MustEnv("CART_SERVICE_ADDR"), func(conn *grpc.ClientConn) { g.cart = cartv1.NewCartServiceClient(conn) }},
		{platform.MustEnv("CHAT_SERVICE_ADDR"), func(conn *grpc.ClientConn) { g.chatClient = chatv1.NewChatServiceClient(conn) }},
	} {
		conn, dialErr := platform.DialGRPC(target.address)
		if dialErr != nil {
			log.Fatalf("gRPC configuration failed for %s: %v", target.address, dialErr)
		}
		g.grpcConns = append(g.grpcConns, conn)
		target.assign(conn)
	}
	defer func() {
		for _, conn := range g.grpcConns {
			_ = conn.Close()
		}
	}()
	if g.authMode == "oidc" {
		issuer := platform.MustEnv("OIDC_ISSUER")
		jwks := platform.MustEnv("OIDC_JWKS_URL")
		g.verifier = oidc.NewVerifier(issuer, oidc.NewRemoteKeySet(context.Background(), jwks), &oidc.Config{ClientID: platform.MustEnv("OIDC_AUDIENCE")})
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := checkGRPCDependencies(ctx, g.grpcConns); err != nil {
			platform.Problem(w, http.StatusServiceUnavailable, "dependency_unavailable", err.Error())
			return
		}
		platform.JSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "gatewayservice"})
	})
	registerRoutes(mux, g.catalog,
		"GET /v1/studios", "GET /v1/studios/{id}", "GET /v1/studios/{studioId}/rooms",
		"GET /v1/credit-products", "GET /v1/credit-products/{id}")
	registerRoutes(mux, g.scheduling,
		"GET /v1/class-sessions", "GET /v1/class-sessions/{id}", "GET /v1/class-sessions/{classSessionId}/roster",
		"GET /v1/bookings", "POST /v1/class-sessions", "POST /v1/class-sessions/{classSessionId}/approve",
		"POST /v1/class-sessions/{classSessionId}/bookings", "POST /v1/class-sessions/{classSessionId}/walk-ins",
		"POST /v1/class-sessions/{classSessionId}/complete", "POST /v1/class-sessions/{classSessionId}/cancel",
		"PATCH /v1/class-sessions/{classSessionId}/video", "POST /v1/bookings/{bookingId}/cancel",
		"PATCH /v1/bookings/{bookingId}/attendance", "POST /v1/bookings/{bookingId}/reverse", "POST /v1/room-reservations")
	registerRoutes(mux, g.orders,
		"POST /v1/orders", "POST /v1/orders/room", "GET /v1/orders/{id}", "POST /v1/orders/{id}/refund",
		"POST /v1/orders/{id}/refund/approve", "POST /v1/orders/{id}/refund/reject",
		"POST /v1/room-reservations/{roomReservationId}/cancel", "POST /v1/flash-sales/{campaignId}/reservations",
		"GET /v1/flash-sale-requests/{requestId}")
	mux.HandleFunc("GET /v1/payroll/monthly", g.payroll)
	registerRoutes(mux, g.account,
		"GET /v1/me", "PATCH /v1/me", "GET /v1/me/memberships", "GET /v1/teachers",
		"POST /v1/teachers/invitations", "POST /v1/studios/{studioId}/teachers:invite",
		"GET /v1/platform/users:lookup", "GET /v1/platform/global-role-assignments",
		"POST /v1/platform/global-role-assignments", "POST /v1/platform/global-role-assignments/{assignmentAction}")
	registerRoutes(mux, g.credit,
		"GET /v1/credits/balances", "GET /v1/credits/grants", "GET /v1/credits/ledger",
		"POST /v1/credits/grants/{grantId}/transfer", "POST /v1/credits/grants/{grantId}/pause", "POST /v1/credits/grants/{grantId}/resume")
	mux.HandleFunc("GET /v1/cart", g.getCart)
	mux.HandleFunc("POST /v1/cart/items", g.addCartItem)
	mux.HandleFunc("DELETE /v1/cart/items/{productVersionId}", g.removeCartItem)
	mux.HandleFunc("DELETE /v1/cart", g.emptyCart)
	registerRoutes(mux, g.payment, "POST /v1/payments", "GET /v1/payments/{id}", "GET /v1/orders/{orderId}/payment", "POST /v1/payments/{id}/simulate")
	mux.HandleFunc("GET /v1/analytics", g.insights)
	registerRoutes(mux, g.chat,
		"GET /v1/chat/ws", "GET /v1/chat/conversations", "POST /v1/chat/direct-conversations",
		"POST /v1/chat/studio-conversations", "GET /v1/chat/groups", "POST /v1/chat/groups",
		"POST /v1/chat/groups/{groupAction}", "PUT /v1/chat/groups/{conversationId}/members/{userId}",
		"POST /v1/chat/groups/{conversationId}/members/{memberAction}",
		"GET /v1/chat/conversations/{conversationId}/messages", "POST /v1/chat/blocks", "DELETE /v1/chat/blocks/{blockedUserId}",
		"POST /v1/chat/attachments:prepare", "POST /v1/chat/attachments:complete", "GET /v1/chat/attachments/{attachmentAction}",
		"POST /v1/chat/messages/{messageAction}", "POST /v1/chat/ws-ticket")
	mux.HandleFunc("POST /webhooks/stripe", g.stripeWebhook)
	registerRoutes(mux, g.browserTelemetry, "POST /v1/telemetry/v1/traces", "POST /v1/telemetry/v1/metrics")
	log.Fatal(platform.Listen("gatewayservice", cors(g.authenticate(mux))))
}

func registerRoutes(mux *http.ServeMux, handler http.HandlerFunc, patterns ...string) {
	for _, pattern := range patterns {
		mux.HandleFunc(pattern, handler)
	}
}

func checkGRPCDependencies(ctx context.Context, connections []*grpc.ClientConn) error {
	results := make(chan error, len(connections))
	for _, connection := range connections {
		go func(conn *grpc.ClientConn) {
			response, err := healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{})
			if err == nil && response.GetStatus() != healthpb.HealthCheckResponse_SERVING {
				err = status.Errorf(codes.Unavailable, "dependency status is %s", response.GetStatus())
			}
			if err != nil {
				err = fmt.Errorf("%s: %w", conn.Target(), err)
			}
			results <- err
		}(connection)
	}
	for range connections {
		if err := <-results; err != nil {
			return err
		}
	}
	return nil
}

func (g *gateway) stripeWebhook(w http.ResponseWriter, r *http.Request) {
	if os.Getenv("PAYMENT_MODE") == "fake" {
		http.NotFound(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	body, bodyErr := io.ReadAll(r.Body)
	if bodyErr != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(bodyErr, &tooLarge) {
			platform.Problem(w, http.StatusRequestEntityTooLarge, "request_too_large", "Webhook body exceeds 1 MiB")
		} else {
			platform.Problem(w, http.StatusBadRequest, "invalid_request", "Unable to read webhook")
		}
		return
	}
	request, err := http.NewRequestWithContext(r.Context(), r.Method, g.paymentWebhookURL.String(), bytes.NewReader(body))
	if err != nil {
		platform.Problem(w, http.StatusBadRequest, "invalid_request", "Unable to create upstream request")
		return
	}
	for _, header := range []string{"Content-Type", "Stripe-Signature", "X-Request-Id", "traceparent", "tracestate", "baggage"} {
		if value := r.Header.Get(header); value != "" {
			request.Header.Set(header, value)
		}
	}
	if request.Header.Get("X-Request-Id") == "" {
		bytes := make([]byte, 12)
		_, _ = rand.Read(bytes)
		request.Header.Set("X-Request-Id", hex.EncodeToString(bytes))
	}
	response, err := g.client.Do(request)
	if err != nil {
		platform.Problem(w, http.StatusBadGateway, "upstream_unavailable", "A required service is unavailable")
		return
	}
	defer response.Body.Close()
	for key, values := range response.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(response.StatusCode)
	_, _ = io.Copy(w, response.Body)
}
