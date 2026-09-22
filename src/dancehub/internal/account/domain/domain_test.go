package domain

import (
	"errors"
	"testing"
	"time"
)

func TestGrantRejectsAlreadyActiveAssignment(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	assignment, err := NewGlobalRoleAssignment("user-1", PlatformAdmin, "admin-1", now)
	if err != nil {
		t.Fatal(err)
	}
	if err = assignment.Grant("admin-1", now); !errors.Is(err, ErrAlreadyActive) {
		t.Fatalf("granting an active assignment: err=%v, want ErrAlreadyActive", err)
	}
}

func TestGrantReactivatesPreviouslyRevokedAssignment(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	assignment := RehydrateGlobalRoleAssignment(GlobalRoleAssignmentSnapshot{
		ID: "assignment-1", UserID: "user-1", Role: PlatformAdmin, Active: false,
		RevokedBy: "admin-2", RevokedAt: now.Add(-time.Hour),
	})
	if err := assignment.Grant("admin-3", now); err != nil {
		t.Fatal(err)
	}
	snapshot := assignment.Snapshot()
	if !snapshot.Active || snapshot.GrantedBy != "admin-3" || !snapshot.GrantedAt.Equal(now) {
		t.Fatalf("unexpected snapshot after grant: %+v", snapshot)
	}
	if snapshot.RevokedBy != "" || !snapshot.RevokedAt.IsZero() {
		t.Fatalf("revocation state should be cleared: %+v", snapshot)
	}
}

func TestRevokeRejectsSelfRevocation(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	assignment := RehydrateGlobalRoleAssignment(GlobalRoleAssignmentSnapshot{ID: "a", UserID: "admin-1", Role: PlatformAdmin, Active: true})
	if err := assignment.Revoke("admin-1", 5, now); !errors.Is(err, ErrSelfRevoke) {
		t.Fatalf("self revoke: err=%v, want ErrSelfRevoke", err)
	}
}

func TestRevokeRejectsAlreadyInactiveAssignment(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	assignment := RehydrateGlobalRoleAssignment(GlobalRoleAssignmentSnapshot{ID: "a", UserID: "user-1", Role: PlatformAdmin, Active: false})
	if err := assignment.Revoke("admin-1", 5, now); !errors.Is(err, ErrAlreadyInactive) {
		t.Fatalf("already inactive: err=%v, want ErrAlreadyInactive", err)
	}
}

func TestRevokeRejectsLastActiveAdministrator(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	assignment := RehydrateGlobalRoleAssignment(GlobalRoleAssignmentSnapshot{ID: "a", UserID: "user-1", Role: PlatformAdmin, Active: true})
	if err := assignment.Revoke("admin-1", 1, now); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("last admin: err=%v, want ErrLastAdmin", err)
	}
	// Self-revocation is checked before the last-administrator count, so an
	// admin acting on someone else's assignment still hits ErrLastAdmin even
	// when they are also the sole remaining administrator.
	if err := assignment.Revoke("admin-2", 1, now); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("last admin (different actor): err=%v, want ErrLastAdmin", err)
	}
}

func TestRevokeSucceedsWhenAnotherAdministratorRemains(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	assignment := RehydrateGlobalRoleAssignment(GlobalRoleAssignmentSnapshot{ID: "a", UserID: "user-1", Role: PlatformAdmin, Active: true})
	if err := assignment.Revoke("admin-2", 2, now); err != nil {
		t.Fatal(err)
	}
	snapshot := assignment.Snapshot()
	if snapshot.Active || snapshot.RevokedBy != "admin-2" || !snapshot.RevokedAt.Equal(now) {
		t.Fatalf("unexpected snapshot after revoke: %+v", snapshot)
	}
}

func TestNewGlobalRoleAssignmentRequiresFields(t *testing.T) {
	if _, err := NewGlobalRoleAssignment("", PlatformAdmin, "admin-1", time.Now()); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("missing user: err=%v, want ErrInvalidArgument", err)
	}
	if _, err := NewGlobalRoleAssignment("user-1", GlobalRoleUnspecified, "admin-1", time.Now()); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("missing role: err=%v, want ErrInvalidArgument", err)
	}
}

func TestProfileUpdateRequiresDisplayNameAndTimezone(t *testing.T) {
	profile := RehydrateProfile(ProfileSnapshot{UserID: "user-1", Email: "user@example.com"})
	if err := profile.Update("", "https://example.com/a.png", "America/Los_Angeles", "", ""); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("missing display name: err=%v, want ErrInvalidArgument", err)
	}
	if err := profile.Update("Jane", "", "", "", ""); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("missing timezone: err=%v, want ErrInvalidArgument", err)
	}
	if err := profile.Update("Jane", "https://example.com/a.png", "America/Los_Angeles", "bio", "https://example.com"); err != nil {
		t.Fatal(err)
	}
	snapshot := profile.Snapshot()
	if snapshot.DisplayName != "Jane" || snapshot.Timezone != "America/Los_Angeles" || snapshot.Email != "user@example.com" {
		t.Fatalf("unexpected profile after update: %+v", snapshot)
	}
}
