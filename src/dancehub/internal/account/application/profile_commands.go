package application

import (
	"context"
	"fmt"
	"strings"

	"github.com/JayceeWU/microservices-demo/dancehub/internal/account/domain"
)

func requireUser(actor ActorContext) (string, error) {
	if strings.TrimSpace(actor.UserID) == "" {
		return "", fmt.Errorf("%w: user identity is required", ErrUnauthenticated)
	}
	return actor.UserID, nil
}

// pageSize mirrors the flat handlers' fixed 24-item default and 100-item cap.
func pageSize(page PageRequest) int32 {
	if page.Size < 1 || page.Size > 100 {
		return 24
	}
	return page.Size
}

func (s *Service) GetMyProfile(ctx context.Context, actor ActorContext) (ProfileView, error) {
	userID, err := requireUser(actor)
	if err != nil {
		return ProfileView{}, err
	}
	var result ProfileView
	err = s.Work.Do(ctx, actor, serviceName, func(ctx context.Context, repositories Repositories) error {
		record, err := repositories.Profiles().GetByUserID(ctx, userID)
		if err != nil {
			return err
		}
		result = record.View()
		return nil
	})
	return result, err
}

type UpdateMyProfileCommand struct {
	Actor                                               ActorContext
	DisplayName, AvatarURL, Timezone, Bio, PortfolioURL string
}

func (s *Service) UpdateMyProfile(ctx context.Context, command UpdateMyProfileCommand) (ProfileView, error) {
	userID, err := requireUser(command.Actor)
	if err != nil {
		return ProfileView{}, err
	}
	profile := domain.RehydrateProfile(domain.ProfileSnapshot{UserID: userID})
	if err = profile.Update(command.DisplayName, command.AvatarURL, command.Timezone, command.Bio, command.PortfolioURL); err != nil {
		return ProfileView{}, Invalid(err.Error())
	}
	var result ProfileView
	err = s.Work.Do(ctx, command.Actor, serviceName, func(ctx context.Context, repositories Repositories) error {
		record, err := repositories.Profiles().Update(ctx, userID, profile)
		if err == nil {
			result = record.View()
		}
		return err
	})
	return result, err
}

func (s *Service) ListStudioMemberships(ctx context.Context, actor ActorContext) (StudioMembershipsView, error) {
	userID, err := requireUser(actor)
	if err != nil {
		return StudioMembershipsView{}, err
	}
	var result StudioMembershipsView
	err = s.Work.Do(ctx, actor, serviceName, func(ctx context.Context, repositories Repositories) error {
		var err error
		result.Memberships, err = repositories.Memberships().ListActiveByUser(ctx, userID)
		if err != nil {
			return err
		}
		result.GlobalRoles, err = repositories.GlobalRoles().ListActiveByUser(ctx, userID)
		return err
	})
	return result, err
}

type SearchTeachersQuery struct {
	StudioID, Query string
	Page            PageRequest
}

func (s *Service) SearchTeachers(ctx context.Context, actor ActorContext, query SearchTeachersQuery) ([]TeacherView, error) {
	if strings.TrimSpace(query.StudioID) == "" {
		return nil, Invalid("studio_id is required")
	}
	limit := pageSize(query.Page)
	likeQuery := "%" + strings.TrimSpace(query.Query) + "%"
	var result []TeacherView
	err := s.Work.Do(ctx, actor, serviceName, func(ctx context.Context, repositories Repositories) error {
		var err error
		result, err = repositories.Profiles().SearchTeachers(ctx, query.StudioID, likeQuery, limit)
		return err
	})
	return result, err
}

type InviteTeacherCommand struct {
	Actor           ActorContext
	StudioID, Email string
	Audit           AuditContext
}

func (s *Service) InviteTeacher(ctx context.Context, command InviteTeacherCommand) (InvitationView, error) {
	actorID, err := requireUser(command.Actor)
	if err != nil || strings.TrimSpace(command.Actor.StudioID) == "" {
		return InvitationView{}, fmt.Errorf("%w: authenticated selected studio is required", ErrUnauthenticated)
	}
	if !command.Actor.HasTenantRole("studio_admin") && !command.Actor.IsPlatformAdmin() {
		return InvitationView{}, Denied("studio administrator role required")
	}
	if command.StudioID != "" && command.StudioID != command.Actor.StudioID {
		return InvitationView{}, Denied("invitation must use the selected studio")
	}
	if strings.TrimSpace(command.Email) == "" || command.Audit.IdempotencyKey == "" {
		return InvitationView{}, Invalid("email and idempotency_key are required")
	}
	reason := command.Audit.Reason
	if reason == "" {
		reason = "Administrator invitation"
	}
	var result InvitationView
	err = s.Work.Do(ctx, command.Actor, serviceName, func(ctx context.Context, repositories Repositories) error {
		var err error
		result, err = repositories.Invitations().Create(ctx, command.Actor.StudioID, strings.ToLower(command.Email), string(domain.RoleTeacher), actorID, reason, command.Audit.IdempotencyKey)
		return err
	})
	return result, err
}
