package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	accountv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/account/v1"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/platform"
	"google.golang.org/grpc"
)

type authorizationAccountClient struct {
	accountv1.AccountServiceClient
	memberships *accountv1.ListStudioMembershipsResponse
}

func (c authorizationAccountClient) GetMyProfile(context.Context, *accountv1.GetMyProfileRequest, ...grpc.CallOption) (*accountv1.Profile, error) {
	return &accountv1.Profile{Id: "user-1"}, nil
}

func (c authorizationAccountClient) ListStudioMemberships(context.Context, *accountv1.ListStudioMembershipsRequest, ...grpc.CallOption) (*accountv1.ListStudioMembershipsResponse, error) {
	return c.memberships, nil
}

func TestScopedAuthorizationDoesNotLeakRolesAcrossStudios(t *testing.T) {
	memberships := []*accountv1.Membership{
		{StudioId: "studio-a", Role: accountv1.Role_ROLE_STUDIO_ADMIN, Active: true},
		{StudioId: "studio-b", Role: accountv1.Role_ROLE_STUDENT, Active: true},
	}
	tenantRoles, globalRoles, allowed := scopedAuthorization(memberships, nil, "studio-b")
	if !allowed || !reflect.DeepEqual(tenantRoles, []string{"student"}) || len(globalRoles) != 0 {
		t.Fatalf("authorization = tenant:%v global:%v allowed:%v", tenantRoles, globalRoles, allowed)
	}
}

func TestScopedAuthorizationStacksOnlySelectedStudioRoles(t *testing.T) {
	memberships := []*accountv1.Membership{
		{StudioId: "studio-a", Role: accountv1.Role_ROLE_TEACHER, Active: true},
		{StudioId: "studio-a", Role: accountv1.Role_ROLE_STUDIO_ADMIN, Active: true},
		{StudioId: "studio-b", Role: accountv1.Role_ROLE_STUDENT, Active: true},
		{StudioId: "studio-a", Role: accountv1.Role_ROLE_STUDENT, Active: false},
	}
	tenantRoles, _, allowed := scopedAuthorization(memberships, nil, "studio-a")
	if !allowed || !reflect.DeepEqual(tenantRoles, []string{"teacher", "studio_admin"}) {
		t.Fatalf("tenant roles = %v allowed=%v", tenantRoles, allowed)
	}
}

func TestScopedAuthorizationKeepsGlobalRolesSeparate(t *testing.T) {
	tenantRoles, globalRoles, allowed := scopedAuthorization(nil, []accountv1.GlobalRole{accountv1.GlobalRole_GLOBAL_ROLE_PLATFORM_ADMIN}, "studio-any")
	if !allowed || len(tenantRoles) != 0 || !reflect.DeepEqual(globalRoles, []string{"platform_admin"}) {
		t.Fatalf("authorization = tenant:%v global:%v allowed:%v", tenantRoles, globalRoles, allowed)
	}
	tenantRoles, _, allowed = scopedAuthorization([]*accountv1.Membership{{StudioId: "studio-a", Role: accountv1.Role_ROLE_STUDIO_ADMIN, Active: true}}, nil, "")
	if !allowed || len(tenantRoles) != 0 {
		t.Fatalf("empty studio must have no tenant roles: %v", tenantRoles)
	}
}

func TestAuthenticateDiscardsBrowserAuthorizationClaims(t *testing.T) {
	g := &gateway{authMode: "dev", accountClient: authorizationAccountClient{memberships: &accountv1.ListStudioMembershipsResponse{
		Memberships: []*accountv1.Membership{
			{StudioId: "studio-a", Role: accountv1.Role_ROLE_STUDIO_ADMIN, Active: true},
			{StudioId: "studio-b", Role: accountv1.Role_ROLE_STUDENT, Active: true},
		},
	}}}
	handler := g.authenticate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := platform.TenantRoles(r.Context()); !reflect.DeepEqual(got, []string{"student"}) {
			t.Fatalf("tenant roles = %v", got)
		}
		if len(platform.GlobalRoles(r.Context())) != 0 || platform.ActorKind(r.Context()) != "human" || platform.ServicePrincipal(r.Context()) != "" {
			t.Fatalf("browser authorization claims survived: globals=%v kind=%q service=%q", platform.GlobalRoles(r.Context()), platform.ActorKind(r.Context()), platform.ServicePrincipal(r.Context()))
		}
		for _, header := range []string{"X-Tenant-Roles", "X-Global-Roles", "X-Actor-Kind", "X-Service-Principal"} {
			if r.Header.Get(header) != "" {
				t.Fatalf("untrusted header %s was not removed", header)
			}
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	request.Header.Set("X-User-Id", "user-1")
	request.Header.Set("X-Studio-Id", "studio-b")
	request.Header.Set("X-Tenant-Roles", "studio_admin")
	request.Header.Set("X-Global-Roles", "platform_admin")
	request.Header.Set("X-Actor-Kind", "service")
	request.Header.Set("X-Service-Principal", "scheduling-worker")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
	}
}
