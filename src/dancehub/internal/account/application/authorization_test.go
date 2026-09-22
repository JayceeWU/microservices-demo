package application_test

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/JayceeWU/microservices-demo/dancehub/internal/account/application"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/account/domain"
	postgresadapter "github.com/JayceeWU/microservices-demo/dancehub/internal/account/infrastructure/postgres"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/platform"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Authorization suite for application.Service, backed by a real
// platform.NewUnitOfWork rather than a mocked repository. The integration
// test requires AUTHZ_TEST_DATABASE_URL and skips cleanly without it, so
// `go test ./...` passes in a sandbox with no database.

func actorContext(userID string, globalRoles ...string) application.ActorContext {
	return application.ActorContext{UserID: userID, ActorKind: "human", GlobalRoles: globalRoles}
}

func TestGlobalRoleLifecycleIntegration(t *testing.T) {
	databaseURL := os.Getenv("AUTHZ_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("AUTHZ_TEST_DATABASE_URL is not set")
	}
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	service := application.NewService(
		platform.NewUnitOfWork[application.ActorContext, application.Repositories](pool, postgresadapter.NewRepositories),
		application.SystemClock{},
	)

	const platformUser = "30000000-0000-0000-0000-000000000004"
	const targetUser = "30000000-0000-0000-0000-000000000001"
	admin := actorContext(platformUser, "platform_admin")

	lookup, err := service.LookupUserByEmail(context.Background(), admin, "STUDENT@BAYAREADANCEHUB.LOCAL")
	if err != nil || lookup.User.UserID != targetUser {
		t.Fatalf("case-insensitive lookup = %+v, err=%v", lookup, err)
	}

	key := time.Now().UTC().Format("20060102150405.000000000")
	grantCommand := application.GrantGlobalRoleCommand{Actor: admin, UserID: targetUser, Role: domain.PlatformAdmin, Audit: application.AuditContext{Reason: "authorization integration test", IdempotencyKey: "grant:" + key}}
	granted, err := service.GrantGlobalRole(context.Background(), grantCommand)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := service.GrantGlobalRole(context.Background(), grantCommand)
	if err != nil || repeated.ID != granted.ID {
		t.Fatalf("idempotent grant = %+v, err=%v", repeated, err)
	}

	revokeCommand := application.RevokeGlobalRoleCommand{Actor: admin, AssignmentID: granted.ID, Audit: application.AuditContext{Reason: "authorization integration test", IdempotencyKey: "revoke:" + key}}
	revoked, err := service.RevokeGlobalRole(context.Background(), revokeCommand)
	if err != nil || revoked.Active {
		t.Fatalf("revocation = %+v, err=%v", revoked, err)
	}
	repeatedRevoke, err := service.RevokeGlobalRole(context.Background(), revokeCommand)
	if err != nil || repeatedRevoke.ID != granted.ID {
		t.Fatalf("idempotent revoke = %+v, err=%v", repeatedRevoke, err)
	}

	assignments, err := service.ListGlobalRoleAssignments(context.Background(), admin, application.PageRequest{})
	if err != nil || len(assignments) != 1 {
		t.Fatalf("active assignments = %+v, err=%v", assignments, err)
	}
	seedAssignment := assignments[0]
	_, err = service.RevokeGlobalRole(context.Background(), application.RevokeGlobalRoleCommand{Actor: admin, AssignmentID: seedAssignment.ID, Audit: application.AuditContext{Reason: "self protection test", IdempotencyKey: "self:" + key}})
	if !errors.Is(err, application.ErrConflict) {
		t.Fatalf("self revocation error = %v, want ErrConflict", err)
	}
	otherAdmin := actorContext("30000000-0000-0000-0000-000000000003", "platform_admin")
	_, err = service.RevokeGlobalRole(context.Background(), application.RevokeGlobalRoleCommand{Actor: otherAdmin, AssignmentID: seedAssignment.ID, Audit: application.AuditContext{Reason: "last administrator protection test", IdempotencyKey: "last:" + key}})
	if !errors.Is(err, application.ErrConflict) {
		t.Fatalf("last administrator error = %v, want ErrConflict", err)
	}

	concurrentAssignments := make(map[string]string)
	for _, userID := range []string{targetUser, "30000000-0000-0000-0000-000000000002"} {
		assignment, grantErr := service.GrantGlobalRole(context.Background(), application.GrantGlobalRoleCommand{
			Actor: admin, UserID: userID, Role: domain.PlatformAdmin,
			Audit: application.AuditContext{Reason: "concurrent revocation setup", IdempotencyKey: "concurrent-grant:" + userID + ":" + key},
		})
		if grantErr != nil {
			t.Fatal(grantErr)
		}
		concurrentAssignments[userID] = assignment.ID
	}
	firstAdmin := actorContext(targetUser, "platform_admin")
	if _, err = service.RevokeGlobalRole(context.Background(), application.RevokeGlobalRoleCommand{
		Actor: firstAdmin, AssignmentID: seedAssignment.ID,
		Audit: application.AuditContext{Reason: "concurrent revocation setup", IdempotencyKey: "concurrent-seed-revoke:" + key},
	}); err != nil {
		t.Fatal(err)
	}

	type revokeAttempt struct{ err error }
	attempts := make(chan revokeAttempt, 2)
	var wait sync.WaitGroup
	users := []string{targetUser, "30000000-0000-0000-0000-000000000002"}
	for index, actorID := range users {
		wait.Add(1)
		go func(actorID, targetID string, attempt int) {
			defer wait.Done()
			revokeActor := actorContext(actorID, "platform_admin")
			_, revokeErr := service.RevokeGlobalRole(context.Background(), application.RevokeGlobalRoleCommand{
				Actor: revokeActor, AssignmentID: concurrentAssignments[targetID],
				Audit: application.AuditContext{Reason: "concurrent last administrator test", IdempotencyKey: "concurrent-revoke:" + string(rune('0'+attempt)) + ":" + key},
			})
			attempts <- revokeAttempt{err: revokeErr}
		}(actorID, users[1-index], index)
	}
	wait.Wait()
	close(attempts)
	succeeded, protected := 0, 0
	for attempt := range attempts {
		switch {
		case attempt.err == nil:
			succeeded++
		case errors.Is(attempt.err, application.ErrConflict):
			protected++
		default:
			t.Fatalf("concurrent revocation error = %v", attempt.err)
		}
	}
	if succeeded != 1 || protected != 1 {
		t.Fatalf("concurrent revocations: succeeded=%d protected=%d", succeeded, protected)
	}
	remaining, err := service.ListGlobalRoleAssignments(context.Background(), admin, application.PageRequest{})
	if err != nil || len(remaining) != 1 {
		t.Fatalf("active assignments after concurrent revocation = %+v, err=%v", remaining, err)
	}
}

func TestStudioAdministratorCannotManageGlobalRoles(t *testing.T) {
	actor := application.ActorContext{UserID: "user-1", StudioID: "studio-1", TenantRoles: []string{"studio_admin"}, ActorKind: "human"}
	service := application.NewService(nil, application.SystemClock{})
	_, err := service.LookupUserByEmail(context.Background(), actor, "user@example.com")
	if !errors.Is(err, application.ErrPermissionDenied) {
		t.Fatalf("error = %v, want ErrPermissionDenied", err)
	}
}

func TestLookupRequiresCompleteEmailAddress(t *testing.T) {
	actor := actorContext("user-1", "platform_admin")
	service := application.NewService(nil, application.SystemClock{})
	_, err := service.LookupUserByEmail(context.Background(), actor, "user@")
	if !errors.Is(err, application.ErrInvalidArgument) {
		t.Fatalf("error = %v, want ErrInvalidArgument", err)
	}
}
