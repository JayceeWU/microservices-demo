package platform

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/JayceeWU/microservices-demo/dancehub/internal/appcore"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// GRPCRegister registers one versioned business service on the shared server.
type GRPCRegister func(*grpc.Server)

// ServeGRPCAndHealth exposes business APIs only over gRPC and keeps HTTP for
// Kubernetes/Docker health probes. Identity metadata is copied into the same
// context keys used by the RLS transaction wrapper.
func ServeGRPCAndHealth(serviceName string, register GRPCRegister, check func(context.Context) error) error {
	ConfigureJSONLogging(serviceName)
	policy, err := MeshPolicyFromEnv()
	if err != nil {
		return fmt.Errorf("mesh peer policy: %w", err)
	}
	meshPolicy = policy
	slog.Info("mesh peer enforcement", "service", serviceName, "enabled", policy.Enforce, "trust_domain", policy.TrustDomain)
	shutdown := InitTelemetry(serviceName)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdown(ctx)
	}()

	listener, err := net.Listen("tcp", ":9090")
	if err != nil {
		return fmt.Errorf("listen for gRPC: %w", err)
	}
	server := grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
		grpc.UnaryInterceptor(grpcIdentityInterceptor),
		grpc.StreamInterceptor(grpcMeshStreamInterceptor),
	)
	register(server)
	healthServer := health.NewServer()
	healthpb.RegisterHealthServer(server, healthServer)
	healthServer.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	go func() {
		slog.Info("gRPC service listening", "service", serviceName, "address", listener.Addr().String())
		if serveErr := server.Serve(listener); serveErr != nil && !errors.Is(serveErr, grpc.ErrServerStopped) {
			slog.Error("gRPC server stopped", "service", serviceName, "error", serveErr)
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		if check != nil && check(ctx) != nil {
			Problem(w, http.StatusServiceUnavailable, "dependency_unavailable", "A required dependency is unavailable")
			return
		}
		JSON(w, http.StatusOK, map[string]string{"status": "ok", "service": serviceName})
	})
	httpServer := &http.Server{
		Addr:              ":8080",
		Handler:           Recover(RequestContext(otelhttp.NewHandler(mux, serviceName+".health"))),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	slog.Info("health endpoint listening", "service", serviceName, "address", httpServer.Addr)
	err = httpServer.ListenAndServe()
	server.GracefulStop()
	return err
}

// meshPolicy is loaded once by ServeGRPCAndHealth; the zero value never enforces, so
// tests and environments without a mesh keep the plain metadata contract.
var meshPolicy MeshPolicy

const grpcHealthServicePrefix = "/grpc.health.v1.Health/"

func grpcIdentityInterceptor(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	actorKind := firstMetadata(md, "x-actor-kind")
	userID := firstMetadata(md, "x-user-id")
	studioID := firstMetadata(md, "x-studio-id")
	tenantRoles := firstMetadata(md, "x-tenant-roles")
	globalRoles := firstMetadata(md, "x-global-roles")
	servicePrincipal := firstMetadata(md, "x-service-principal")
	if actorKind != "" && actorKind != "human" && actorKind != "service" {
		return nil, status.Error(codes.Unauthenticated, "invalid actor kind")
	}
	if actorKind == "service" && (servicePrincipal == "" || userID != "" || studioID != "" || tenantRoles != "" || globalRoles != "") {
		return nil, status.Error(codes.Unauthenticated, "invalid service identity")
	}
	if actorKind == "human" && servicePrincipal != "" {
		return nil, status.Error(codes.Unauthenticated, "human identity cannot include a service principal")
	}
	if actorKind == "" && (tenantRoles != "" || globalRoles != "" || servicePrincipal != "") {
		return nil, status.Error(codes.Unauthenticated, "scoped authorization requires an actor kind")
	}
	if !strings.HasPrefix(info.FullMethod, grpcHealthServicePrefix) {
		if err := meshPolicy.Authorize(firstMetadata(md, "x-forwarded-client-cert"), actorKind, servicePrincipal); err != nil {
			return nil, err
		}
	}
	requestID := firstMetadata(md, "x-request-id")
	if requestID == "" {
		requestID = fmt.Sprintf("req-%d", time.Now().UnixNano())
	}
	actor := appcore.NewActorContext(userID, studioID, requestID, splitRoles(tenantRoles), splitRoles(globalRoles), actorKind, servicePrincipal)
	ctx = WithActor(ctx, actor)
	ctx = context.WithValue(ctx, authorizationKey, firstMetadata(md, "authorization"))
	ctx = context.WithValue(ctx, idempotencyKey, firstMetadata(md, "idempotency-key"))
	return handler(ctx, req)
}

// grpcMeshStreamInterceptor applies the same peer binding to streaming RPCs (chat
// Connect); the stream authenticates its user through a ticket, so only the peer
// workload is checked here.
func grpcMeshStreamInterceptor(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	md, _ := metadata.FromIncomingContext(stream.Context())
	if err := meshPolicy.Authorize(firstMetadata(md, "x-forwarded-client-cert"), firstMetadata(md, "x-actor-kind"), firstMetadata(md, "x-service-principal")); err != nil {
		return err
	}
	return handler(srv, stream)
}

func splitRoles(value string) []string {
	if value == "" {
		return nil
	}
	roles := strings.Split(value, ",")
	for index := range roles {
		roles[index] = strings.TrimSpace(roles[index])
	}
	return roles
}

func IdentityUnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return grpcIdentityInterceptor
}

func firstMetadata(md metadata.MD, key string) string {
	values := md.Get(key)
	if len(values) == 0 {
		return ""
	}
	return strings.TrimSpace(values[0])
}

// OutgoingGRPCContext propagates only Gateway-validated identity and request
// metadata. OpenTelemetry propagates trace context through its gRPC handler.
func OutgoingGRPCContext(ctx context.Context) context.Context {
	pairs := []string{"x-request-id", RequestID(ctx)}
	if userID, err := UserID(ctx); err == nil {
		pairs = append(pairs, "x-user-id", userID)
	}
	if studioID, err := StudioID(ctx); err == nil {
		pairs = append(pairs, "x-studio-id", studioID)
	}
	if roles := TenantRoles(ctx); len(roles) > 0 {
		pairs = append(pairs, "x-tenant-roles", strings.Join(roles, ","))
	}
	if roles := GlobalRoles(ctx); len(roles) > 0 {
		pairs = append(pairs, "x-global-roles", strings.Join(roles, ","))
	}
	if kind := ActorKind(ctx); kind != "" {
		pairs = append(pairs, "x-actor-kind", kind)
	}
	if service := ServicePrincipal(ctx); service != "" {
		pairs = append(pairs, "x-service-principal", service)
	}
	if authorization := Authorization(ctx); authorization != "" {
		pairs = append(pairs, "authorization", authorization)
	}
	if idempotency := IdempotencyKey(ctx); idempotency != "" {
		pairs = append(pairs, "idempotency-key", idempotency)
	}
	return metadata.NewOutgoingContext(ctx, metadata.Pairs(pairs...))
}

func DialGRPC(address string) (*grpc.ClientConn, error) {
	return grpc.NewClient(address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
		grpc.WithUnaryInterceptor(grpcClientPolicyInterceptor),
	)
}

func grpcClientPolicyInterceptor(ctx context.Context, method string, req, reply any, connection *grpc.ClientConn, invoker grpc.UnaryInvoker, options ...grpc.CallOption) error {
	operation := method[strings.LastIndex(method, "/")+1:]
	query := strings.HasPrefix(operation, "Get") || strings.HasPrefix(operation, "List") || strings.HasPrefix(operation, "Search")
	timeout := 10 * time.Second
	if query {
		timeout = 3 * time.Second
	} else if strings.Contains(method, ".payments.") || strings.Contains(strings.ToLower(operation), "payment") || strings.Contains(strings.ToLower(operation), "refund") {
		timeout = 15 * time.Second
	}
	if _, set := ctx.Deadline(); !set {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	retryable := query || firstMetadataFromOutgoing(ctx, "idempotency-key") != ""
	for attempt := 0; ; attempt++ {
		err := invoker(ctx, method, req, reply, connection, options...)
		if err == nil || !retryable || status.Code(err) != codes.Unavailable || attempt >= 2 {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt+1) * 25 * time.Millisecond):
		}
	}
}

func firstMetadataFromOutgoing(ctx context.Context, key string) string {
	md, _ := metadata.FromOutgoingContext(ctx)
	return firstMetadata(md, key)
}
