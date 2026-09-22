package domain

// Role is a tenant-scoped role held through a studio membership.
type Role string

const (
	RoleUnspecified Role = ""
	RoleStudent     Role = "student"
	RoleTeacher     Role = "teacher"
	RoleStudioAdmin Role = "studio_admin"
)

// GlobalRole is a platform-scoped role, independent of any studio.
type GlobalRole string

const (
	GlobalRoleUnspecified GlobalRole = ""
	PlatformAdmin         GlobalRole = "platform_admin"
)

// Membership is a user's tenant-scoped role within a studio (or the
// platform-wide teacher pool, when StudioID is empty). It is a simple value
// object: activation and deactivation are driven entirely by invitations and
// administrative action recorded elsewhere, so it has no lifecycle methods
// of its own.
type Membership struct {
	ID       string
	StudioID string
	UserID   string
	Role     Role
	Active   bool
}
