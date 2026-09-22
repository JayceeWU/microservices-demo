package domain

import (
	"fmt"
	"time"
)

// GlobalRoleAssignment is the aggregate that owns the grant/revoke lifecycle
// for a platform-scoped role such as platform_admin. It encapsulates the
// following invariants so the gRPC handlers stay free of branching:
//
//   - an administrator cannot revoke their own assignment;
//   - the last active platform_admin cannot be revoked, leaving the
//     platform with no administrator;
//   - granting an already-active assignment, or revoking an already-inactive
//     one, are rejected rather than silently accepted.
//
// The aggregate cannot query anything: whether a revoke would remove the
// last active administrator is a fact about every other assignment in the
// system, so the application layer must compute it (typically with a
// `SELECT count(*) ... FOR UPDATE`-guarded query) and pass it in.
type GlobalRoleAssignment struct {
	id        string
	userID    string
	role      GlobalRole
	active    bool
	grantedBy string
	grantedAt time.Time
	revokedBy string
	revokedAt time.Time
}

type GlobalRoleAssignmentSnapshot struct {
	ID, UserID, GrantedBy, RevokedBy string
	Role                             GlobalRole
	Active                           bool
	GrantedAt, RevokedAt             time.Time
}

// NewGlobalRoleAssignment creates a brand new, active assignment. Use this
// when the user has never held the role before; reactivating a previously
// revoked assignment goes through Rehydrate + Grant instead, so the history
// (grant/revoke audit trail) stays attached to the same row.
func NewGlobalRoleAssignment(userID string, role GlobalRole, grantedBy string, grantedAt time.Time) (*GlobalRoleAssignment, error) {
	if userID == "" || role == GlobalRoleUnspecified || grantedBy == "" {
		return nil, fmt.Errorf("%w: user, role and granting actor are required", ErrInvalidArgument)
	}
	return &GlobalRoleAssignment{userID: userID, role: role, active: true, grantedBy: grantedBy, grantedAt: grantedAt}, nil
}

func RehydrateGlobalRoleAssignment(v GlobalRoleAssignmentSnapshot) *GlobalRoleAssignment {
	return &GlobalRoleAssignment{
		id: v.ID, userID: v.UserID, role: v.Role, active: v.Active,
		grantedBy: v.GrantedBy, grantedAt: v.GrantedAt, revokedBy: v.RevokedBy, revokedAt: v.RevokedAt,
	}
}

func (a *GlobalRoleAssignment) Snapshot() GlobalRoleAssignmentSnapshot {
	return GlobalRoleAssignmentSnapshot{
		ID: a.id, UserID: a.userID, Role: a.role, Active: a.active,
		GrantedBy: a.grantedBy, GrantedAt: a.grantedAt, RevokedBy: a.revokedBy, RevokedAt: a.revokedAt,
	}
}

func (a *GlobalRoleAssignment) UserID() string { return a.userID }
func (a *GlobalRoleAssignment) Active() bool   { return a.active }

// Grant (re)activates the assignment on behalf of grantedBy. It fails with
// ErrAlreadyActive if the assignment is already active; reactivating a
// previously revoked assignment clears the prior revocation.
func (a *GlobalRoleAssignment) Grant(grantedBy string, now time.Time) error {
	if a.active {
		return ErrAlreadyActive
	}
	a.active = true
	a.grantedBy = grantedBy
	a.grantedAt = now
	a.revokedBy = ""
	a.revokedAt = time.Time{}
	return nil
}

// Revoke deactivates the assignment on behalf of revokedBy. activeAdminCount
// is the number of currently active platform_admin assignments across the
// whole platform, including this one; it is a fact the application layer
// must supply, since the domain layer must not query anything.
//
// Checks run in this order, matching the previous handler behavior exactly:
// self-revocation, already-inactive, then last-administrator protection.
func (a *GlobalRoleAssignment) Revoke(revokedBy string, activeAdminCount int, now time.Time) error {
	if a.userID == revokedBy {
		return ErrSelfRevoke
	}
	if !a.active {
		return ErrAlreadyInactive
	}
	if activeAdminCount <= 1 {
		return ErrLastAdmin
	}
	a.active = false
	a.revokedBy = revokedBy
	a.revokedAt = now
	return nil
}
