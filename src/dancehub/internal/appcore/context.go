package appcore

import (
	"errors"
	"fmt"
)

var (
	ErrUnauthenticated  = errors.New("unauthenticated")
	ErrPermissionDenied = errors.New("permission denied")
	ErrInvalidArgument  = errors.New("invalid argument")
	ErrNotFound         = errors.New("not found")
	ErrConflict         = errors.New("conflict")
	ErrAlreadyExists    = errors.New("already exists")
	ErrUnavailable      = errors.New("unavailable")
)

func Invalid(message string) error       { return fmt.Errorf("%w: %s", ErrInvalidArgument, message) }
func Denied(message string) error        { return fmt.Errorf("%w: %s", ErrPermissionDenied, message) }
func NotFound(message string) error      { return fmt.Errorf("%w: %s", ErrNotFound, message) }
func Conflict(message string) error      { return fmt.Errorf("%w: %s", ErrConflict, message) }
func AlreadyExists(message string) error { return fmt.Errorf("%w: %s", ErrAlreadyExists, message) }
func Unavailable(message string, cause error) error {
	return fmt.Errorf("%w: %s: %v", ErrUnavailable, message, cause)
}

type ActorContext struct {
	UserID, StudioID, RequestID string
	TenantRoles, GlobalRoles    []string
	ActorKind, ServicePrincipal string
}

func NewActorContext(userID, studioID, requestID string, tenantRoles, globalRoles []string, actorKind, servicePrincipal string) ActorContext {
	return ActorContext{
		UserID: userID, StudioID: studioID, RequestID: requestID,
		TenantRoles: tenantRoles, GlobalRoles: globalRoles,
		ActorKind: actorKind, ServicePrincipal: servicePrincipal,
	}
}

func (a ActorContext) AuthorizationIdentity() (string, string, string, string, []string, []string) {
	return a.UserID, a.StudioID, a.ActorKind, a.ServicePrincipal, a.TenantRoles, a.GlobalRoles
}

func hasRole(actualRoles []string, values ...string) bool {
	for _, actual := range actualRoles {
		for _, expected := range values {
			if actual == expected {
				return true
			}
		}
	}
	return false
}

func (a ActorContext) HasTenantRole(values ...string) bool { return hasRole(a.TenantRoles, values...) }
func (a ActorContext) HasGlobalRole(values ...string) bool { return hasRole(a.GlobalRoles, values...) }
func (a ActorContext) IsPlatformAdmin() bool {
	return a.ActorKind == "human" && a.HasGlobalRole("platform_admin")
}
func (a ActorContext) IsService(value string) bool {
	return a.ActorKind == "service" && a.ServicePrincipal == value
}

type AuditContext struct{ IdempotencyKey, Reason string }

func NewAuditContext(idempotencyKey, reason string) AuditContext {
	return AuditContext{IdempotencyKey: idempotencyKey, Reason: reason}
}

type PageRequest struct {
	Size  int32
	Token string
}
