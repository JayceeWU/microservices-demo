package grpctransport

import (
	"context"
	"errors"
	"strings"

	accountv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/account/v1"
	commonv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/common/v1"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/account/application"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/account/domain"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Server struct {
	accountv1.UnimplementedAccountServiceServer
	service accountApplication
}

type accountApplication interface {
	GetMyProfile(context.Context, application.ActorContext) (application.ProfileView, error)
	UpdateMyProfile(context.Context, application.UpdateMyProfileCommand) (application.ProfileView, error)
	ListStudioMemberships(context.Context, application.ActorContext) (application.StudioMembershipsView, error)
	ResolveIdentity(context.Context, application.ActorContext, string) (application.ResolvedIdentityView, error)
	SearchTeachers(context.Context, application.ActorContext, application.SearchTeachersQuery) ([]application.TeacherView, error)
	InviteTeacher(context.Context, application.InviteTeacherCommand) (application.InvitationView, error)
	LookupUserByEmail(context.Context, application.ActorContext, string) (application.LookupUserByEmailResult, error)
	ListGlobalRoleAssignments(context.Context, application.ActorContext, application.PageRequest) ([]application.GlobalRoleAssignmentView, error)
	GrantGlobalRole(context.Context, application.GrantGlobalRoleCommand) (application.GlobalRoleAssignmentView, error)
	RevokeGlobalRole(context.Context, application.RevokeGlobalRoleCommand) (application.GlobalRoleAssignmentView, error)
	GetChatPrincipal(context.Context, application.ActorContext, string) (application.ChatPrincipalView, error)
}

func NewServer(service accountApplication) *Server { return &Server{service: service} }

func (s *Server) GetMyProfile(ctx context.Context, _ *accountv1.GetMyProfileRequest) (*accountv1.Profile, error) {
	item, err := s.service.GetMyProfile(ctx, actor(ctx))
	if err != nil {
		return nil, grpcError(err)
	}
	return profile(item), nil
}

func (s *Server) UpdateMyProfile(ctx context.Context, request *accountv1.UpdateMyProfileRequest) (*accountv1.Profile, error) {
	item, err := s.service.UpdateMyProfile(ctx, application.UpdateMyProfileCommand{
		Actor: actor(ctx), DisplayName: request.DisplayName, AvatarURL: request.AvatarUrl,
		Timezone: request.Timezone, Bio: request.Bio, PortfolioURL: request.PortfolioUrl,
	})
	if err != nil {
		return nil, grpcError(err)
	}
	return profile(item), nil
}

func (s *Server) ListStudioMemberships(ctx context.Context, _ *accountv1.ListStudioMembershipsRequest) (*accountv1.ListStudioMembershipsResponse, error) {
	item, err := s.service.ListStudioMemberships(ctx, actor(ctx))
	if err != nil {
		return nil, grpcError(err)
	}
	response := &accountv1.ListStudioMembershipsResponse{}
	for _, m := range item.Memberships {
		response.Memberships = append(response.Memberships, membership(m))
	}
	for _, g := range item.GlobalRoles {
		response.GlobalRoles = append(response.GlobalRoles, globalRole(g))
	}
	return response, nil
}

func (s *Server) ResolveIdentity(ctx context.Context, request *accountv1.ResolveIdentityRequest) (*accountv1.ResolvedIdentity, error) {
	item, err := s.service.ResolveIdentity(ctx, actor(ctx), request.OidcSubject)
	if err != nil {
		return nil, grpcError(err)
	}
	response := &accountv1.ResolvedIdentity{Profile: profile(item.Profile)}
	for _, m := range item.Memberships {
		response.Memberships = append(response.Memberships, membership(m))
	}
	for _, g := range item.GlobalRoles {
		response.GlobalRoles = append(response.GlobalRoles, globalRole(g))
	}
	return response, nil
}

func (s *Server) SearchTeachers(ctx context.Context, request *accountv1.SearchTeachersRequest) (*accountv1.SearchTeachersResponse, error) {
	items, err := s.service.SearchTeachers(ctx, actor(ctx), application.SearchTeachersQuery{StudioID: request.StudioId, Query: request.Query, Page: page(request.Page)})
	if err != nil {
		return nil, grpcError(err)
	}
	response := &accountv1.SearchTeachersResponse{Page: &commonv1.PageResponse{}}
	for _, item := range items {
		response.Teachers = append(response.Teachers, teacher(item))
	}
	return response, nil
}

func (s *Server) InviteTeacher(ctx context.Context, request *accountv1.InviteTeacherRequest) (*accountv1.Invitation, error) {
	item, err := s.service.InviteTeacher(ctx, application.InviteTeacherCommand{Actor: actor(ctx), StudioID: request.StudioId, Email: request.Email, Audit: audit(request.Audit)})
	if err != nil {
		return nil, grpcError(err)
	}
	return invitation(item), nil
}

func (s *Server) LookupUserByEmail(ctx context.Context, request *accountv1.LookupUserByEmailRequest) (*accountv1.LookupUserByEmailResponse, error) {
	item, err := s.service.LookupUserByEmail(ctx, actor(ctx), request.Email)
	if err != nil {
		return nil, grpcError(err)
	}
	response := &accountv1.LookupUserByEmailResponse{User: platformUser(item.User)}
	for _, g := range item.GlobalRoles {
		response.GlobalRoles = append(response.GlobalRoles, globalRole(g))
	}
	return response, nil
}

func (s *Server) ListGlobalRoleAssignments(ctx context.Context, request *accountv1.ListGlobalRoleAssignmentsRequest) (*accountv1.ListGlobalRoleAssignmentsResponse, error) {
	items, err := s.service.ListGlobalRoleAssignments(ctx, actor(ctx), page(request.Page))
	if err != nil {
		return nil, grpcError(err)
	}
	response := &accountv1.ListGlobalRoleAssignmentsResponse{Page: &commonv1.PageResponse{}}
	for _, item := range items {
		response.Assignments = append(response.Assignments, globalRoleAssignment(item))
	}
	return response, nil
}

func (s *Server) GrantGlobalRole(ctx context.Context, request *accountv1.GrantGlobalRoleRequest) (*accountv1.GlobalRoleAssignment, error) {
	item, err := s.service.GrantGlobalRole(ctx, application.GrantGlobalRoleCommand{Actor: actor(ctx), UserID: request.UserId, Role: parseGlobalRole(request.Role), Audit: audit(request.Audit)})
	if err != nil {
		return nil, grpcError(err)
	}
	return globalRoleAssignment(item), nil
}

func (s *Server) RevokeGlobalRole(ctx context.Context, request *accountv1.RevokeGlobalRoleRequest) (*accountv1.GlobalRoleAssignment, error) {
	item, err := s.service.RevokeGlobalRole(ctx, application.RevokeGlobalRoleCommand{Actor: actor(ctx), AssignmentID: request.AssignmentId, Audit: audit(request.Audit)})
	if err != nil {
		return nil, grpcError(err)
	}
	return globalRoleAssignment(item), nil
}

func (s *Server) GetChatPrincipal(ctx context.Context, request *accountv1.GetChatPrincipalRequest) (*accountv1.ChatPrincipal, error) {
	item, err := s.service.GetChatPrincipal(ctx, actor(ctx), request.UserId)
	if err != nil {
		return nil, grpcError(err)
	}
	response := &accountv1.ChatPrincipal{Profile: profile(item.Profile)}
	for _, m := range item.Memberships {
		response.Memberships = append(response.Memberships, membership(m))
	}
	return response, nil
}

func actor(ctx context.Context) application.ActorContext {
	return platform.Actor(ctx)
}

func audit(value *commonv1.AuditContext) application.AuditContext {
	if value == nil {
		return application.AuditContext{}
	}
	return application.NewAuditContext(value.IdempotencyKey, value.Reason)
}

func page(value *commonv1.PageRequest) application.PageRequest {
	if value == nil {
		return application.PageRequest{}
	}
	return application.PageRequest{Size: value.PageSize, Token: value.PageToken}
}

func profile(value application.ProfileView) *accountv1.Profile {
	return &accountv1.Profile{
		Id: value.UserID, Email: value.Email, DisplayName: value.DisplayName,
		AvatarUrl: value.AvatarURL, Timezone: value.Timezone, Bio: value.Bio, PortfolioUrl: value.PortfolioURL,
	}
}

func membership(value application.MembershipView) *accountv1.Membership {
	return &accountv1.Membership{Id: value.ID, StudioId: value.StudioID, UserId: value.UserID, Role: role(value.Role), Active: value.Active}
}

func teacher(value application.TeacherView) *accountv1.Teacher {
	return &accountv1.Teacher{
		Id: value.UserID, StudioId: value.StudioID, DisplayName: value.DisplayName,
		AvatarUrl: value.AvatarURL, Bio: value.Bio, PortfolioUrl: value.PortfolioURL,
	}
}

func invitation(value application.InvitationView) *accountv1.Invitation {
	return &accountv1.Invitation{
		Id: value.ID, StudioId: value.StudioID, Email: value.Email, Role: role(value.Role),
		Status: value.Status, ExpiresAt: timestamppb.New(value.ExpiresAt),
	}
}

func platformUser(value application.PlatformUserView) *accountv1.PlatformUser {
	return &accountv1.PlatformUser{Id: value.UserID, Email: value.Email, DisplayName: value.DisplayName}
}

func globalRoleAssignment(value application.GlobalRoleAssignmentView) *accountv1.GlobalRoleAssignment {
	result := &accountv1.GlobalRoleAssignment{
		Id:        value.ID,
		User:      &accountv1.PlatformUser{Id: value.UserID, Email: value.UserEmail, DisplayName: value.UserDisplayName},
		Role:      globalRole(value.Role),
		Active:    value.Active,
		GrantedAt: timestamppb.New(value.GrantedAt),
	}
	if !value.RevokedAt.IsZero() {
		result.RevokedAt = timestamppb.New(value.RevokedAt)
	}
	return result
}

func role(value domain.Role) accountv1.Role {
	switch value {
	case domain.RoleStudent:
		return accountv1.Role_ROLE_STUDENT
	case domain.RoleTeacher:
		return accountv1.Role_ROLE_TEACHER
	case domain.RoleStudioAdmin:
		return accountv1.Role_ROLE_STUDIO_ADMIN
	default:
		return accountv1.Role_ROLE_UNSPECIFIED
	}
}

func globalRole(value domain.GlobalRole) accountv1.GlobalRole {
	if value == domain.PlatformAdmin {
		return accountv1.GlobalRole_GLOBAL_ROLE_PLATFORM_ADMIN
	}
	return accountv1.GlobalRole_GLOBAL_ROLE_UNSPECIFIED
}

func parseGlobalRole(value accountv1.GlobalRole) domain.GlobalRole {
	if value == accountv1.GlobalRole_GLOBAL_ROLE_PLATFORM_ADMIN {
		return domain.PlatformAdmin
	}
	return domain.GlobalRoleUnspecified
}

func grpcError(err error) error {
	switch {
	case errors.Is(err, application.ErrUnauthenticated):
		return status.Error(codes.Unauthenticated, clean(err, application.ErrUnauthenticated))
	case errors.Is(err, application.ErrPermissionDenied), errors.Is(err, domain.ErrPermissionDenied):
		return status.Error(codes.PermissionDenied, clean(err, application.ErrPermissionDenied))
	case errors.Is(err, application.ErrInvalidArgument), errors.Is(err, domain.ErrInvalidArgument):
		return status.Error(codes.InvalidArgument, clean(err, application.ErrInvalidArgument))
	case errors.Is(err, application.ErrNotFound):
		return status.Error(codes.NotFound, clean(err, application.ErrNotFound))
	case errors.Is(err, application.ErrAlreadyExists), errors.Is(err, domain.ErrAlreadyActive):
		return status.Error(codes.AlreadyExists, clean(err, application.ErrAlreadyExists))
	case errors.Is(err, application.ErrConflict), errors.Is(err, domain.ErrSelfRevoke), errors.Is(err, domain.ErrAlreadyInactive), errors.Is(err, domain.ErrLastAdmin):
		return status.Error(codes.FailedPrecondition, clean(err, application.ErrConflict))
	case errors.Is(err, application.ErrUnavailable):
		return status.Error(codes.Unavailable, clean(err, application.ErrUnavailable))
	default:
		return status.Error(codes.Internal, "account operation failed")
	}
}

func clean(err error, marker error) string {
	return strings.TrimSpace(strings.TrimPrefix(err.Error(), marker.Error()+":"))
}
