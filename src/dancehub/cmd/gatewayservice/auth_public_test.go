package main

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	accountv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/account/v1"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/platform"
)

func TestPublicCatalogRetainsVerifiedIdentity(t *testing.T) {
	g := &gateway{authMode: "dev", accountClient: authorizationAccountClient{memberships: &accountv1.ListStudioMembershipsResponse{
		Memberships: []*accountv1.Membership{{StudioId: "studio-a", Role: accountv1.Role_ROLE_STUDENT, Active: true}},
	}}}
	for _, path := range []string{"/v1/class-sessions?bookable_only=true", "/v1/studios", "/v1/credit-products", "/v1/chat/groups"} {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, path, nil)
			request.Header.Set("X-User-Id", "user-1")
			request.Header.Set("X-Studio-Id", "studio-a")
			response := httptest.NewRecorder()
			g.authenticate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				actor := platform.Actor(r.Context())
				if actor.UserID != "user-1" || actor.StudioID != "studio-a" || !reflect.DeepEqual(actor.TenantRoles, []string{"student"}) {
					t.Fatalf("authenticated catalog lost caller scope: %+v", actor)
				}
				w.WriteHeader(http.StatusNoContent)
			})).ServeHTTP(response, request)
			if response.Code != http.StatusNoContent {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func TestPublicCatalogStillAllowsAnonymousBrowsing(t *testing.T) {
	for _, mode := range []string{"oidc", "dev"} {
		t.Run(mode, func(t *testing.T) {
			g := &gateway{authMode: mode}
			request := httptest.NewRequest(http.MethodGet, "/v1/class-sessions", nil)
			request.Header.Set("X-Studio-Id", "unverified-studio")
			if mode == "oidc" {
				request.Header.Set("X-User-Id", "spoofed-user")
			}
			response := httptest.NewRecorder()
			handler := platform.RequestContext(g.authenticate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				actor := platform.Actor(r.Context())
				if actor.UserID != "" || actor.StudioID != "" || len(actor.TenantRoles) != 0 || len(actor.GlobalRoles) != 0 {
					t.Fatalf("anonymous request retained unverified identity: %+v", actor)
				}
				w.WriteHeader(http.StatusNoContent)
			})))
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusNoContent {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func TestPublicCatalogRejectsInvalidCredentialsAndForeignStudio(t *testing.T) {
	for _, authorization := range []string{"Bearer invalid", "Bearer ", "Basic invalid"} {
		request := httptest.NewRequest(http.MethodGet, "/v1/class-sessions", nil)
		request.Header.Set("Authorization", authorization)
		response := httptest.NewRecorder()
		g := &gateway{authMode: "oidc"}
		g.authenticate(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			t.Fatal("supplied invalid credentials fell back to anonymous browsing")
		})).ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("authorization=%q status=%d", authorization, response.Code)
		}
	}
	g := &gateway{authMode: "dev", accountClient: authorizationAccountClient{memberships: &accountv1.ListStudioMembershipsResponse{
		Memberships: []*accountv1.Membership{{StudioId: "studio-a", Role: accountv1.Role_ROLE_STUDENT, Active: true}},
	}}}
	request := httptest.NewRequest(http.MethodGet, "/v1/class-sessions", nil)
	request.Header.Set("X-User-Id", "user-1")
	request.Header.Set("X-Studio-Id", "studio-foreign")
	response := httptest.NewRecorder()
	g.authenticate(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("public catalog bypassed membership verification")
	})).ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestDevAuthModeIsConfinedToLocalEnvironments(t *testing.T) {
	for _, value := range []struct {
		environment, mode string
		invalid           bool
	}{
		{"local", "oidc", false}, {"production", "oidc", false}, {"local", "dev", false}, {"test", "dev", false},
		{"production", "dev", true}, {"", "dev", true}, {"local", "header", true},
	} {
		if err := validateAuthMode(value.environment, value.mode); (err != nil) != value.invalid {
			t.Fatalf("unexpected auth mode policy for %+v: %v", value, err)
		}
	}
}
