package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	accountv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/account/v1"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/platform"
)

// validateAuthMode confines header-asserted identities (AUTH_MODE=dev accepts
// X-User-Id) to the local and test environments, mirroring the simulated-payment rule.
func validateAuthMode(environment, mode string) error {
	switch {
	case mode == "oidc":
		return nil
	case mode == "dev" && (environment == "local" || environment == "test"):
		return nil
	case mode == "dev":
		return fmt.Errorf("AUTH_MODE=dev requires ENVIRONMENT=local or test")
	default:
		return fmt.Errorf("AUTH_MODE must be dev or oidc")
	}
}

func (g *gateway) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Catalogs allow anonymous browsing, but supplied credentials must be
		// verified so personalized filters and cursors retain the caller's scope.
		hasCredentials := strings.TrimSpace(r.Header.Get("Authorization")) != "" ||
			(g.authMode == "dev" && strings.TrimSpace(r.Header.Get("X-User-Id")) != "")
		if isPublic(r) && (!isPublicCatalog(r) || !hasCredentials) {
			next.ServeHTTP(w, r.WithContext(platform.HumanIdentityContext(r.Context(), "", "", nil, nil)))
			return
		}
		studioID := strings.TrimSpace(r.Header.Get("X-Studio-Id"))
		for _, header := range []string{"X-Tenant-Roles", "X-Global-Roles", "X-Actor-Kind", "X-Service-Principal"} {
			r.Header.Del(header)
		}
		if g.authMode == "dev" {
			userID := strings.TrimSpace(r.Header.Get("X-User-Id"))
			if userID == "" {
				platform.Problem(w, http.StatusUnauthorized, "authentication_required", "X-User-Id is required in dev auth mode")
				return
			}
			ctx, cancel := context.WithTimeout(platform.OutgoingGRPCContext(platform.HumanIdentityContext(r.Context(), userID, studioID, nil, nil)), 3*time.Second)
			_, err := g.accountClient.GetMyProfile(ctx, &accountv1.GetMyProfileRequest{})
			memberships, membershipErr := g.accountClient.ListStudioMemberships(ctx, &accountv1.ListStudioMembershipsRequest{})
			cancel()
			if err != nil || membershipErr != nil {
				platform.Problem(w, http.StatusForbidden, "identity_not_provisioned", "Development identity is not provisioned")
				return
			}
			tenantRoles, globalRoles, tenantAllowed := scopedAuthorization(memberships.Memberships, memberships.GlobalRoles, studioID)
			if !tenantAllowed {
				platform.Problem(w, http.StatusForbidden, "studio_access_denied", "User is not a member of this studio")
				return
			}
			r.Header.Set("X-User-Id", userID)
			next.ServeHTTP(w, r.WithContext(platform.HumanIdentityContext(r.Context(), userID, studioID, tenantRoles, globalRoles)))
			return
		}
		tokenText := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if tokenText == "" || g.verifier == nil {
			platform.Problem(w, http.StatusUnauthorized, "authentication_required", "Bearer token is required")
			return
		}
		token, err := g.verifier.Verify(r.Context(), tokenText)
		if err != nil {
			platform.Problem(w, http.StatusUnauthorized, "invalid_token", "Bearer token is invalid")
			return
		}
		var claims struct {
			Subject string `json:"sub"`
		}
		if token.Claims(&claims) != nil || claims.Subject == "" {
			platform.Problem(w, http.StatusUnauthorized, "invalid_claims", "Token subject is missing")
			return
		}
		cleanContext := platform.HumanIdentityContext(r.Context(), "", "", nil, nil)
		ctx, cancel := context.WithTimeout(platform.OutgoingGRPCContext(cleanContext), 3*time.Second)
		identity, err := g.accountClient.ResolveIdentity(ctx, &accountv1.ResolveIdentityRequest{OidcSubject: claims.Subject})
		cancel()
		if err != nil || identity.GetProfile().GetId() == "" {
			platform.Problem(w, http.StatusForbidden, "identity_not_provisioned", "Account is not provisioned")
			return
		}
		tenantRoles, globalRoles, tenantAllowed := scopedAuthorization(identity.Memberships, identity.GlobalRoles, studioID)
		if !tenantAllowed {
			platform.Problem(w, http.StatusForbidden, "studio_access_denied", "User is not a member of this studio")
			return
		}
		userID := identity.Profile.Id
		r.Header.Set("X-User-Id", userID)
		next.ServeHTTP(w, r.WithContext(platform.HumanIdentityContext(r.Context(), userID, studioID, tenantRoles, globalRoles)))
	})
}

func scopedAuthorization(memberships []*accountv1.Membership, assignedGlobalRoles []accountv1.GlobalRole, studioID string) ([]string, []string, bool) {
	tenantRoles := make([]string, 0, len(memberships))
	globalRoles := make([]string, 0, len(assignedGlobalRoles))
	for _, role := range assignedGlobalRoles {
		if value := globalRoleText(role); value == "platform_admin" {
			globalRoles = append(globalRoles, value)
		}
	}
	tenantAllowed := studioID == "" || contains(globalRoles, "platform_admin")
	for _, membership := range memberships {
		if !membership.Active || studioID == "" || membership.StudioId != studioID {
			continue
		}
		role := accountRoleText(membership.Role)
		if role != "student" && role != "teacher" && role != "studio_admin" {
			continue
		}
		tenantRoles = append(tenantRoles, role)
		tenantAllowed = true
	}
	return unique(tenantRoles), unique(globalRoles), tenantAllowed
}

func accountRoleText(role accountv1.Role) string {
	return strings.ToLower(strings.TrimPrefix(role.String(), "ROLE_"))
}

func globalRoleText(role accountv1.GlobalRole) string {
	return strings.ToLower(strings.TrimPrefix(role.String(), "GLOBAL_ROLE_"))
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func isPublic(r *http.Request) bool {
	if r.URL.Path == "/healthz" || r.URL.Path == "/webhooks/stripe" || r.URL.Path == "/v1/chat/ws" || strings.HasPrefix(r.URL.Path, "/v1/telemetry/") {
		return true
	}
	return isPublicCatalog(r)
}

func isPublicCatalog(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	return strings.HasPrefix(r.URL.Path, "/v1/studios") || r.URL.Path == "/v1/credit-products" || r.URL.Path == "/v1/class-sessions" || r.URL.Path == "/v1/chat/groups"
}

func unique(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && allowedBrowserOrigin(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key, X-Request-Id, X-User-Id, X-Studio-Id, Traceparent, Tracestate, Baggage")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

var browserOrigins map[string]struct{}

func configureBrowserOrigins(value string) {
	browserOrigins = make(map[string]struct{})
	for _, origin := range strings.Split(value, ",") {
		browserOrigins[strings.TrimSpace(origin)] = struct{}{}
	}
}

func allowedBrowserOrigin(origin string) bool {
	_, allowed := browserOrigins[origin]
	return allowed
}
