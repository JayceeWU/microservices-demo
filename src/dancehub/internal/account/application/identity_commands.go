package application

import (
	"context"
	"strings"
)

// ResolveIdentity turns a validated OIDC subject into the platform's
// internal identity: the resolving lookup runs anonymously (under whatever
// actor called in, typically the Gateway), and once the user ID is known a
// second, identity-scoped transaction loads their profile and roles under
// RLS as that user. This mirrors the two separate transactions the flat
// handler used; both are still required because RLS is enforced per
// transaction based on the identity SET LOCAL at its start.
func (s *Service) ResolveIdentity(ctx context.Context, actor ActorContext, oidcSubject string) (ResolvedIdentityView, error) {
	if strings.TrimSpace(oidcSubject) == "" {
		return ResolvedIdentityView{}, Invalid("oidc_subject is required")
	}
	var userID string
	err := s.Work.Do(ctx, actor, serviceName, func(ctx context.Context, repositories Repositories) error {
		var err error
		userID, err = repositories.Identity().ResolveOIDCSubject(ctx, oidcSubject)
		return err
	})
	if err != nil {
		return ResolvedIdentityView{}, err
	}

	resolvedActor := ActorContext{UserID: userID, RequestID: actor.RequestID, ActorKind: "human"}
	var result ResolvedIdentityView
	err = s.Work.Do(ctx, resolvedActor, serviceName, func(ctx context.Context, repositories Repositories) error {
		profile, err := repositories.Profiles().GetByUserID(ctx, userID)
		if err != nil {
			return err
		}
		memberships, err := repositories.Memberships().ListActiveByUser(ctx, userID)
		if err != nil {
			return err
		}
		globalRoles, err := repositories.GlobalRoles().ListActiveByUser(ctx, userID)
		if err != nil {
			return err
		}
		result = ResolvedIdentityView{Profile: profile.View(), Memberships: memberships, GlobalRoles: globalRoles}
		return nil
	})
	return result, err
}
