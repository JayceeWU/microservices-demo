package application

import (
	"context"
	"errors"
	"net/mail"
	"strings"

	"github.com/JayceeWU/microservices-demo/dancehub/internal/account/domain"
)

func validAudit(audit AuditContext) bool {
	return strings.TrimSpace(audit.Reason) != "" && strings.TrimSpace(audit.IdempotencyKey) != ""
}

type LookupUserByEmailResult struct {
	User        PlatformUserView
	GlobalRoles []domain.GlobalRole
}

func (s *Service) LookupUserByEmail(ctx context.Context, actor ActorContext, email string) (LookupUserByEmailResult, error) {
	if !actor.IsPlatformAdmin() {
		return LookupUserByEmailResult{}, Denied("platform administrator role required")
	}
	email = strings.ToLower(strings.TrimSpace(email))
	address, parseErr := mail.ParseAddress(email)
	if parseErr != nil || address.Name != "" || !strings.EqualFold(address.Address, email) {
		return LookupUserByEmailResult{}, Invalid("a complete email address is required")
	}
	var result LookupUserByEmailResult
	err := s.Work.Do(ctx, actor, serviceName, func(ctx context.Context, repositories Repositories) error {
		user, err := repositories.Profiles().FindByEmail(ctx, email)
		if err != nil {
			return err
		}
		globalRoles, err := repositories.GlobalRoles().ListActiveByUser(ctx, user.UserID)
		if err != nil {
			return err
		}
		result = LookupUserByEmailResult{User: user, GlobalRoles: globalRoles}
		return nil
	})
	return result, err
}

func (s *Service) ListGlobalRoleAssignments(ctx context.Context, actor ActorContext, page PageRequest) ([]GlobalRoleAssignmentView, error) {
	if !actor.IsPlatformAdmin() {
		return nil, Denied("platform administrator role required")
	}
	var result []GlobalRoleAssignmentView
	err := s.Work.Do(ctx, actor, serviceName, func(ctx context.Context, repositories Repositories) error {
		var err error
		result, err = repositories.GlobalRoles().ListActive(ctx, pageSize(page))
		return err
	})
	return result, err
}

type GrantGlobalRoleCommand struct {
	Actor  ActorContext
	UserID string
	Role   domain.GlobalRole
	Audit  AuditContext
}

func (s *Service) GrantGlobalRole(ctx context.Context, command GrantGlobalRoleCommand) (GlobalRoleAssignmentView, error) {
	actorID, err := requireUser(command.Actor)
	if err != nil || !command.Actor.IsPlatformAdmin() {
		return GlobalRoleAssignmentView{}, Denied("platform administrator role required")
	}
	if command.Role != domain.PlatformAdmin || strings.TrimSpace(command.UserID) == "" || !validAudit(command.Audit) {
		return GlobalRoleAssignmentView{}, Invalid("user_id, platform_admin role, reason and idempotency_key are required")
	}
	var result GlobalRoleAssignmentView
	err = s.Work.Do(ctx, command.Actor, serviceName, func(ctx context.Context, repositories Repositories) error {
		roles := repositories.GlobalRoles()
		existingID, repeated, err := roles.BeginMutation(ctx, actorID, command.Audit.IdempotencyKey)
		if err != nil {
			return err
		}
		if repeated {
			record, err := roles.Get(ctx, existingID)
			if err == nil {
				result = record.View()
			}
			return err
		}
		record, err := roles.FindByUserAndRole(ctx, command.UserID, domain.PlatformAdmin)
		switch {
		case errors.Is(err, ErrNotFound):
			assignment, buildErr := domain.NewGlobalRoleAssignment(command.UserID, domain.PlatformAdmin, actorID, s.Clock.Now())
			if buildErr != nil {
				return Invalid(buildErr.Error())
			}
			record, err = roles.Insert(ctx, assignment)
		case err == nil:
			if grantErr := record.Aggregate.Grant(actorID, s.Clock.Now()); grantErr != nil {
				if errors.Is(grantErr, domain.ErrAlreadyActive) {
					return AlreadyExists("global role is already active")
				}
				return grantErr
			}
			record, err = roles.Save(ctx, record)
		}
		if err != nil {
			return err
		}
		if err = roles.RecordAudit(ctx, GlobalRoleAudit{
			AssignmentID: record.View().ID, ActorID: actorID, TargetUserID: command.UserID,
			Role: domain.PlatformAdmin, Action: "GRANT", Reason: strings.TrimSpace(command.Audit.Reason), IdempotencyKey: command.Audit.IdempotencyKey,
		}); err != nil {
			return err
		}
		result = record.View()
		return nil
	})
	return result, err
}

type RevokeGlobalRoleCommand struct {
	Actor        ActorContext
	AssignmentID string
	Audit        AuditContext
}

func (s *Service) RevokeGlobalRole(ctx context.Context, command RevokeGlobalRoleCommand) (GlobalRoleAssignmentView, error) {
	actorID, err := requireUser(command.Actor)
	if err != nil || !command.Actor.IsPlatformAdmin() {
		return GlobalRoleAssignmentView{}, Denied("platform administrator role required")
	}
	if strings.TrimSpace(command.AssignmentID) == "" || !validAudit(command.Audit) {
		return GlobalRoleAssignmentView{}, Invalid("assignment_id, reason and idempotency_key are required")
	}
	var result GlobalRoleAssignmentView
	err = s.Work.Do(ctx, command.Actor, serviceName, func(ctx context.Context, repositories Repositories) error {
		roles := repositories.GlobalRoles()
		existingID, repeated, err := roles.BeginMutation(ctx, actorID, command.Audit.IdempotencyKey)
		if err != nil {
			return err
		}
		if repeated {
			record, err := roles.Get(ctx, existingID)
			if err == nil {
				result = record.View()
			}
			return err
		}
		record, err := roles.GetForUpdate(ctx, command.AssignmentID)
		if err != nil {
			return err
		}
		activeCount, err := roles.CountActiveByRole(ctx, domain.PlatformAdmin)
		if err != nil {
			return err
		}
		targetUserID := record.Aggregate.UserID()
		if revokeErr := record.Aggregate.Revoke(actorID, activeCount, s.Clock.Now()); revokeErr != nil {
			switch {
			case errors.Is(revokeErr, domain.ErrSelfRevoke), errors.Is(revokeErr, domain.ErrAlreadyInactive), errors.Is(revokeErr, domain.ErrLastAdmin):
				return Conflict(revokeErr.Error())
			}
			return revokeErr
		}
		record, err = roles.Save(ctx, record)
		if err != nil {
			return err
		}
		if err = roles.RecordAudit(ctx, GlobalRoleAudit{
			AssignmentID: record.View().ID, ActorID: actorID, TargetUserID: targetUserID,
			Role: domain.PlatformAdmin, Action: "REVOKE", Reason: strings.TrimSpace(command.Audit.Reason), IdempotencyKey: command.Audit.IdempotencyKey,
		}); err != nil {
			return err
		}
		result = record.View()
		return nil
	})
	return result, err
}
