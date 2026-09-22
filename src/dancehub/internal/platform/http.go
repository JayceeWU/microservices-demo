package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/JayceeWU/microservices-demo/dancehub/internal/appcore"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type contextKey string

const (
	actorContextKey  contextKey = "actor"
	authorizationKey contextKey = "authorization"
	idempotencyKey   contextKey = "idempotency_key"
)

type ErrorResponse struct {
	Type    string `json:"type"`
	Title   string `json:"title"`
	Status  int    `json:"status"`
	Detail  string `json:"detail"`
	Error   string `json:"error"`
	Message string `json:"message"`
}

func ContextWithTimeout(duration time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), duration)
}

func JSON(w http.ResponseWriter, status int, value any) {
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
	}
	w.WriteHeader(status)
	if value != nil {
		_ = json.NewEncoder(w).Encode(value)
	}
}

func Problem(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/problem+json")
	JSON(w, status, ErrorResponse{Type: "https://bayareadancehub.com/problems/" + code, Title: code, Status: status, Detail: message, Error: code, Message: message})
}

func DecodeJSON(r *http.Request, target any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return nil
}

func RequestContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := strings.TrimSpace(r.Header.Get("X-Request-Id"))
		if requestID == "" {
			requestID = fmt.Sprintf("req-%d", time.Now().UnixNano())
		}
		w.Header().Set("X-Request-Id", requestID)
		actor := appcore.ActorContext{UserID: strings.TrimSpace(r.Header.Get("X-User-Id")), StudioID: strings.TrimSpace(r.Header.Get("X-Studio-Id")), RequestID: requestID}
		ctx := context.WithValue(r.Context(), actorContextKey, actor)
		ctx = context.WithValue(ctx, authorizationKey, strings.TrimSpace(r.Header.Get("Authorization")))
		ctx = context.WithValue(ctx, idempotencyKey, strings.TrimSpace(r.Header.Get("Idempotency-Key")))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func Authorization(ctx context.Context) string {
	value, _ := ctx.Value(authorizationKey).(string)
	return value
}
func IdempotencyKey(ctx context.Context) string {
	value, _ := ctx.Value(idempotencyKey).(string)
	return value
}

// IdentityContext replaces browser-supplied identity with the values validated
// by Gateway. Downstream gRPC metadata and RLS must use this context only.
func HumanIdentityContext(ctx context.Context, userID, studioID string, tenantRoles, globalRoles []string) context.Context {
	actor := Actor(ctx)
	actor.UserID, actor.StudioID = strings.TrimSpace(userID), strings.TrimSpace(studioID)
	actor.TenantRoles, actor.GlobalRoles = tenantRoles, globalRoles
	actor.ActorKind, actor.ServicePrincipal = "human", ""
	return context.WithValue(ctx, actorContextKey, actor)
}

func ServiceIdentityContext(ctx context.Context, servicePrincipal string) context.Context {
	actor := Actor(ctx)
	actor.UserID, actor.StudioID = "", ""
	actor.TenantRoles, actor.GlobalRoles = nil, nil
	actor.ActorKind, actor.ServicePrincipal = "service", strings.TrimSpace(servicePrincipal)
	return context.WithValue(ctx, actorContextKey, actor)
}

func Actor(ctx context.Context) appcore.ActorContext {
	actor, _ := ctx.Value(actorContextKey).(appcore.ActorContext)
	return actor
}

func WithActor(ctx context.Context, actor appcore.ActorContext) context.Context {
	return context.WithValue(ctx, actorContextKey, actor)
}

func TenantRoles(ctx context.Context) []string    { return Actor(ctx).TenantRoles }
func GlobalRoles(ctx context.Context) []string    { return Actor(ctx).GlobalRoles }
func ActorKind(ctx context.Context) string        { return Actor(ctx).ActorKind }
func ServicePrincipal(ctx context.Context) string { return Actor(ctx).ServicePrincipal }
func RequestID(ctx context.Context) string        { return Actor(ctx).RequestID }

func HasTenantRole(ctx context.Context, wanted ...string) bool {
	return Actor(ctx).HasTenantRole(wanted...)
}
func HasGlobalRole(ctx context.Context, wanted ...string) bool {
	return Actor(ctx).HasGlobalRole(wanted...)
}
func IsPlatformAdmin(ctx context.Context) bool {
	return Actor(ctx).IsPlatformAdmin()
}
func IsService(ctx context.Context, value string) bool {
	return Actor(ctx).IsService(value)
}

func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if value := recover(); value != nil {
				slog.Error("request panic", "path", r.URL.Path, "panic", value)
				Problem(w, http.StatusInternalServerError, "internal_error", "An internal error occurred")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func UserID(ctx context.Context) (string, error) {
	value := Actor(ctx).UserID
	if value == "" {
		return "", errors.New("missing authenticated user")
	}
	return value, nil
}

func StudioID(ctx context.Context) (string, error) {
	value := Actor(ctx).StudioID
	if value == "" {
		return "", errors.New("missing studio context")
	}
	return value, nil
}

func Listen(serviceName string, handler http.Handler) error {
	ConfigureJSONLogging(serviceName)
	shutdown := InitTelemetry(serviceName)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdown(ctx)
	}()
	address := ":8080"
	server := &http.Server{
		Addr:              address,
		Handler:           Recover(RequestContext(otelhttp.NewHandler(handler, serviceName+".http"))),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	slog.Info("service listening", "service", serviceName, "address", address)
	return server.ListenAndServe()
}
