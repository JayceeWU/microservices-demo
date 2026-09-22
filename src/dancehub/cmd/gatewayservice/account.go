package main

import (
	"net/http"
	"strings"
	"time"

	accountv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/account/v1"
	catalogv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/catalog/v1"
	commonv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/common/v1"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/platform"
)

func (g *gateway) account(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := grpcRequestContext(r, 10*time.Second)
	defer cancel()
	switch r.Pattern {
	case "GET /v1/me":
		profile, err := g.accountClient.GetMyProfile(ctx, &accountv1.GetMyProfileRequest{})
		if err != nil {
			grpcProblem(w, err)
			return
		}
		platform.JSON(w, http.StatusOK, profileJSON(profile))
	case "PATCH /v1/me":
		var input struct {
			DisplayName    string `json:"displayName"`
			AvatarURL      string `json:"avatarUrl"`
			Timezone       string `json:"timezone"`
			Bio            string `json:"bio"`
			PortfolioURL   string `json:"portfolioUrl"`
			IdempotencyKey string `json:"idempotencyKey"`
		}
		if platform.DecodeJSON(r, &input) != nil {
			platform.Problem(w, http.StatusBadRequest, "invalid_profile", "Invalid profile payload")
			return
		}
		profile, err := g.accountClient.UpdateMyProfile(ctx, &accountv1.UpdateMyProfileRequest{DisplayName: input.DisplayName, AvatarUrl: input.AvatarURL, Timezone: input.Timezone, Bio: input.Bio, PortfolioUrl: input.PortfolioURL, Audit: audit(input.IdempotencyKey, "")})
		if err != nil {
			grpcProblem(w, err)
			return
		}
		platform.JSON(w, http.StatusOK, profileJSON(profile))
	case "GET /v1/me/memberships":
		response, err := g.accountClient.ListStudioMemberships(ctx, &accountv1.ListStudioMembershipsRequest{})
		if err != nil {
			grpcProblem(w, err)
			return
		}
		studioIDs := make([]string, 0, len(response.Memberships))
		for _, membership := range response.Memberships {
			if membership.StudioId != "" {
				studioIDs = append(studioIDs, membership.StudioId)
			}
		}
		studios, _ := g.catalogClient.BatchGetStudios(ctx, &catalogv1.BatchGetStudiosRequest{Ids: unique(studioIDs)})
		names := map[string]string{}
		if studios != nil {
			for _, studio := range studios.Studios {
				names[studio.Id] = studio.Name
			}
		}
		items := make([]map[string]any, 0, len(response.Memberships))
		for _, item := range response.Memberships {
			name := names[item.StudioId]
			if name == "" {
				name = "Platform"
			}
			items = append(items, membershipJSON(item, name))
		}
		globalRoles := make([]string, 0, len(response.GlobalRoles))
		for _, role := range response.GlobalRoles {
			globalRoles = append(globalRoles, globalRoleText(role))
		}
		platform.JSON(w, http.StatusOK, map[string]any{"memberships": items, "globalRoles": unique(globalRoles)})
	case "GET /v1/platform/users:lookup":
		response, err := g.accountClient.LookupUserByEmail(ctx, &accountv1.LookupUserByEmailRequest{Email: r.URL.Query().Get("email")})
		if err != nil {
			grpcProblem(w, err)
			return
		}
		roles := make([]string, 0, len(response.GlobalRoles))
		for _, role := range response.GlobalRoles {
			roles = append(roles, globalRoleText(role))
		}
		platform.JSON(w, http.StatusOK, map[string]any{"user": platformUserJSON(response.User), "globalRoles": unique(roles)})
	case "GET /v1/platform/global-role-assignments":
		response, err := g.accountClient.ListGlobalRoleAssignments(ctx, &accountv1.ListGlobalRoleAssignmentsRequest{Page: &commonv1.PageRequest{PageSize: queryPageSize(r), PageToken: r.URL.Query().Get("page_token")}})
		if err != nil {
			grpcProblem(w, err)
			return
		}
		items := make([]map[string]any, 0, len(response.Assignments))
		for _, item := range response.Assignments {
			items = append(items, globalRoleAssignmentJSON(item))
		}
		platform.JSON(w, http.StatusOK, map[string]any{"assignments": items, "nextPageToken": response.GetPage().GetNextPageToken()})
	case "POST /v1/platform/global-role-assignments":
		var input struct {
			UserID         string `json:"userId"`
			Role           string `json:"role"`
			Reason         string `json:"reason"`
			IdempotencyKey string `json:"idempotencyKey"`
		}
		if platform.DecodeJSON(r, &input) != nil {
			platform.Problem(w, http.StatusBadRequest, "invalid_global_role", "Invalid global role payload")
			return
		}
		role := accountv1.GlobalRole_GLOBAL_ROLE_UNSPECIFIED
		if strings.EqualFold(input.Role, "platform_admin") {
			role = accountv1.GlobalRole_GLOBAL_ROLE_PLATFORM_ADMIN
		}
		item, err := g.accountClient.GrantGlobalRole(ctx, &accountv1.GrantGlobalRoleRequest{UserId: input.UserID, Role: role, Audit: audit(input.IdempotencyKey, input.Reason)})
		if err != nil {
			grpcProblem(w, err)
			return
		}
		platform.JSON(w, http.StatusCreated, globalRoleAssignmentJSON(item))
	case "POST /v1/platform/global-role-assignments/{assignmentAction}":
		var input struct {
			Reason         string `json:"reason"`
			IdempotencyKey string `json:"idempotencyKey"`
		}
		if platform.DecodeJSON(r, &input) != nil {
			platform.Problem(w, http.StatusBadRequest, "invalid_global_role_revocation", "Invalid global role revocation payload")
			return
		}
		assignmentID := strings.TrimSuffix(r.PathValue("assignmentAction"), ":revoke")
		if assignmentID == r.PathValue("assignmentAction") {
			platform.Problem(w, http.StatusNotFound, "account_route_not_found", "Account route not found")
			return
		}
		item, err := g.accountClient.RevokeGlobalRole(ctx, &accountv1.RevokeGlobalRoleRequest{AssignmentId: assignmentID, Audit: audit(input.IdempotencyKey, input.Reason)})
		if err != nil {
			grpcProblem(w, err)
			return
		}
		platform.JSON(w, http.StatusOK, globalRoleAssignmentJSON(item))
	case "GET /v1/teachers":
		response, err := g.accountClient.SearchTeachers(ctx, &accountv1.SearchTeachersRequest{StudioId: r.URL.Query().Get("studio_id"), Query: r.URL.Query().Get("query"), Page: &commonv1.PageRequest{PageSize: queryPageSize(r)}})
		if err != nil {
			grpcProblem(w, err)
			return
		}
		items := make([]map[string]any, 0, len(response.Teachers))
		for _, item := range response.Teachers {
			items = append(items, map[string]any{"id": item.Id, "displayName": item.DisplayName, "avatarUrl": item.AvatarUrl, "bio": item.Bio, "portfolioUrl": item.PortfolioUrl, "studioId": item.StudioId})
		}
		platform.JSON(w, http.StatusOK, map[string]any{"teachers": items, "nextPageToken": response.GetPage().GetNextPageToken()})
	case "POST /v1/teachers/invitations", "POST /v1/studios/{studioId}/teachers:invite":
		var input struct {
			StudioID       string `json:"studioId"`
			Email          string `json:"email"`
			Reason         string `json:"reason"`
			IdempotencyKey string `json:"idempotencyKey"`
		}
		if platform.DecodeJSON(r, &input) != nil {
			platform.Problem(w, http.StatusBadRequest, "invalid_invitation", "Invalid invitation payload")
			return
		}
		studioID := input.StudioID
		if r.Pattern == "POST /v1/studios/{studioId}/teachers:invite" {
			studioID = r.PathValue("studioId")
		}
		item, err := g.accountClient.InviteTeacher(ctx, &accountv1.InviteTeacherRequest{StudioId: studioID, Email: input.Email, Audit: audit(input.IdempotencyKey, input.Reason)})
		if err != nil {
			grpcProblem(w, err)
			return
		}
		platform.JSON(w, http.StatusCreated, map[string]any{"id": item.Id, "studioId": item.StudioId, "email": item.Email, "role": accountRoleText(item.Role), "status": item.Status, "expiresAt": item.ExpiresAt.AsTime()})
	}
}
