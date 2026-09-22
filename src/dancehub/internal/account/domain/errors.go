package domain

import "errors"

var (
	ErrInvalidArgument  = errors.New("invalid argument")
	ErrPermissionDenied = errors.New("permission denied")
	// ErrAlreadyActive is returned by GlobalRoleAssignment.Grant when the
	// assignment is already active.
	ErrAlreadyActive = errors.New("global role assignment is already active")
	// ErrAlreadyInactive is returned by GlobalRoleAssignment.Revoke when the
	// assignment has already been revoked.
	ErrAlreadyInactive = errors.New("global role assignment is already inactive")
	// ErrSelfRevoke is returned when a platform administrator attempts to
	// revoke their own assignment.
	ErrSelfRevoke = errors.New("platform administrators cannot revoke themselves")
	// ErrLastAdmin is returned when revoking an assignment would leave the
	// platform with no active platform_admin.
	ErrLastAdmin = errors.New("the final platform administrator cannot be revoked")
)
