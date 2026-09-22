package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/JayceeWU/microservices-demo/dancehub/internal/account/application"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/account/domain"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/platform"
	"github.com/jackc/pgx/v5"
)

func NewRepositories(tx pgx.Tx) application.Repositories {
	return repositories{tx: tx}
}

type repositories struct{ tx pgx.Tx }

func (r repositories) Profiles() application.ProfileRepository { return profileRepository{tx: r.tx} }
func (r repositories) Memberships() application.MembershipRepository {
	return membershipRepository{tx: r.tx}
}
func (r repositories) GlobalRoles() application.GlobalRoleRepository {
	return globalRoleRepository{tx: r.tx}
}
func (r repositories) Invitations() application.InvitationRepository {
	return invitationRepository{tx: r.tx}
}
func (r repositories) Chat() application.ChatRepository         { return chatRepository{tx: r.tx} }
func (r repositories) Identity() application.IdentityRepository { return identityRepository{tx: r.tx} }

type scanner interface{ Scan(...any) error }

// profileRepository -----------------------------------------------------

type profileRepository struct{ tx pgx.Tx }

const profileColumns = `u.id,u.email,u.display_name,COALESCE(u.avatar_url,''),u.timezone,COALESCE(tp.bio,''),COALESCE(tp.portfolio_url,'')`
const profileFrom = `FROM account.users u LEFT JOIN account.teacher_profiles tp ON tp.user_id=u.id`

func scanProfile(row scanner) (*application.ProfileRecord, error) {
	var userID, email, displayName, avatarURL, timezone, bio, portfolioURL string
	if err := row.Scan(&userID, &email, &displayName, &avatarURL, &timezone, &bio, &portfolioURL); err != nil {
		return nil, translate(err, "profile not found")
	}
	return &application.ProfileRecord{Aggregate: domain.RehydrateProfile(domain.ProfileSnapshot{
		UserID: userID, Email: email, DisplayName: displayName, AvatarURL: avatarURL, Timezone: timezone, Bio: bio, PortfolioURL: portfolioURL,
	})}, nil
}

func (r profileRepository) GetByUserID(ctx context.Context, userID string) (*application.ProfileRecord, error) {
	query := `SELECT ` + profileColumns + ` ` + profileFrom + ` WHERE u.id::text=$1`
	return scanProfile(r.tx.QueryRow(ctx, query, userID))
}

func (r profileRepository) Update(ctx context.Context, userID string, aggregate *domain.Profile) (*application.ProfileRecord, error) {
	v := aggregate.Snapshot()
	if _, err := r.tx.Exec(ctx, `UPDATE account.users SET display_name=$2,avatar_url=NULLIF($3,''),timezone=$4,updated_at=now() WHERE id=$1`, userID, v.DisplayName, v.AvatarURL, v.Timezone); err != nil {
		return nil, translate(err, "unable to update profile")
	}
	if _, err := r.tx.Exec(ctx, `INSERT INTO account.teacher_profiles(user_id,bio,portfolio_url) VALUES($1,$2,NULLIF($3,'')) ON CONFLICT(user_id) DO UPDATE SET bio=EXCLUDED.bio,portfolio_url=EXCLUDED.portfolio_url,updated_at=now()`, userID, v.Bio, v.PortfolioURL); err != nil {
		return nil, translate(err, "unable to update profile")
	}
	return r.GetByUserID(ctx, userID)
}

func (r profileRepository) SearchTeachers(ctx context.Context, studioID, query string, limit int32) ([]application.TeacherView, error) {
	rows, err := r.tx.Query(ctx, `SELECT id,display_name,avatar_url,bio,portfolio_url FROM account.search_public_teachers($1,$2,$3)`, studioID, query, limit)
	if err != nil {
		return nil, translate(err, "unable to load teachers")
	}
	defer rows.Close()
	result := make([]application.TeacherView, 0)
	for rows.Next() {
		teacher := application.TeacherView{StudioID: studioID}
		if err = rows.Scan(&teacher.UserID, &teacher.DisplayName, &teacher.AvatarURL, &teacher.Bio, &teacher.PortfolioURL); err != nil {
			return nil, err
		}
		result = append(result, teacher)
	}
	return result, translate(rows.Err(), "unable to load teachers")
}

func (r profileRepository) FindByEmail(ctx context.Context, email string) (application.PlatformUserView, error) {
	var user application.PlatformUserView
	err := r.tx.QueryRow(ctx, `SELECT id::text,email,display_name FROM account.users WHERE lower(email)=lower($1)`, email).Scan(&user.UserID, &user.Email, &user.DisplayName)
	return user, translate(err, "user not found")
}

// membershipRepository ----------------------------------------------------

type membershipRepository struct{ tx pgx.Tx }

func scanMembership(row scanner) (application.MembershipView, error) {
	var item application.MembershipView
	var role string
	err := row.Scan(&item.ID, &item.StudioID, &item.UserID, &role, &item.Active)
	item.Role = domain.Role(role)
	return item, err
}

func (r membershipRepository) ListActiveByUser(ctx context.Context, userID string) ([]application.MembershipView, error) {
	rows, err := r.tx.Query(ctx, `SELECT id,COALESCE(studio_id::text,''),user_id,role::text,active FROM account.studio_memberships WHERE user_id=$1 AND active ORDER BY studio_id NULLS FIRST,role`, userID)
	if err != nil {
		return nil, translate(err, "unable to load memberships")
	}
	defer rows.Close()
	result := make([]application.MembershipView, 0)
	for rows.Next() {
		item, err := scanMembership(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, translate(rows.Err(), "unable to load memberships")
}

// globalRoleRepository ------------------------------------------------------

type globalRoleRepository struct{ tx pgx.Tx }

const globalRoleAssignmentColumns = `assignment.id::text,user_account.id::text,user_account.email,user_account.display_name,assignment.role::text,assignment.active,assignment.granted_at,assignment.revoked_at`
const globalRoleAssignmentFrom = `FROM account.global_role_assignments assignment JOIN account.users user_account ON user_account.id=assignment.user_id`

func scanGlobalRoleAssignment(row scanner) (*application.GlobalRoleAssignmentRecord, error) {
	var id, userID, email, displayName, role string
	var active bool
	var grantedAt time.Time
	var revokedAt sql.NullTime
	if err := row.Scan(&id, &userID, &email, &displayName, &role, &active, &grantedAt, &revokedAt); err != nil {
		return nil, translate(err, "global role assignment not found")
	}
	snapshot := domain.GlobalRoleAssignmentSnapshot{ID: id, UserID: userID, Role: domain.GlobalRole(role), Active: active, GrantedAt: grantedAt}
	if revokedAt.Valid {
		snapshot.RevokedAt = revokedAt.Time
	}
	return &application.GlobalRoleAssignmentRecord{
		Aggregate: domain.RehydrateGlobalRoleAssignment(snapshot), UserEmail: email, UserDisplayName: displayName,
	}, nil
}

func (r globalRoleRepository) ListActiveByUser(ctx context.Context, userID string) ([]domain.GlobalRole, error) {
	rows, err := r.tx.Query(ctx, `SELECT role::text FROM account.global_role_assignments WHERE user_id=$1 AND active ORDER BY role`, userID)
	if err != nil {
		return nil, translate(err, "unable to load global roles")
	}
	defer rows.Close()
	roles := make([]domain.GlobalRole, 0)
	for rows.Next() {
		var role string
		if err = rows.Scan(&role); err != nil {
			return nil, err
		}
		roles = append(roles, domain.GlobalRole(role))
	}
	return roles, translate(rows.Err(), "unable to load global roles")
}

func (r globalRoleRepository) ListActive(ctx context.Context, limit int32) ([]application.GlobalRoleAssignmentView, error) {
	query := `SELECT ` + globalRoleAssignmentColumns + ` ` + globalRoleAssignmentFrom + ` WHERE assignment.active ORDER BY user_account.email LIMIT $1`
	rows, err := r.tx.Query(ctx, query, limit)
	if err != nil {
		return nil, translate(err, "unable to list global role assignments")
	}
	defer rows.Close()
	result := make([]application.GlobalRoleAssignmentView, 0)
	for rows.Next() {
		record, err := scanGlobalRoleAssignment(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, record.View())
	}
	return result, translate(rows.Err(), "unable to list global role assignments")
}

// BeginMutation reproduces the original handler's idempotency guard: an
// advisory transaction lock serializes every grant/revoke call for the
// platform_admin role, then the audit table (keyed by actor + idempotency
// key) is checked so a retried request returns the same assignment instead
// of re-running the mutation.
func (r globalRoleRepository) BeginMutation(ctx context.Context, actorID, idempotencyKey string) (string, bool, error) {
	if _, err := r.tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('account.global_role.platform_admin'))`); err != nil {
		return "", false, err
	}
	var assignmentID string
	err := r.tx.QueryRow(ctx, `SELECT assignment_id::text FROM account.global_role_audit WHERE actor_id=$1 AND idempotency_key=$2`, actorID, idempotencyKey).Scan(&assignmentID)
	if err == nil {
		return assignmentID, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", false, err
	}
	return "", false, nil
}

func (r globalRoleRepository) Get(ctx context.Context, id string) (*application.GlobalRoleAssignmentRecord, error) {
	query := `SELECT ` + globalRoleAssignmentColumns + ` ` + globalRoleAssignmentFrom + ` WHERE assignment.id=$1`
	return scanGlobalRoleAssignment(r.tx.QueryRow(ctx, query, id))
}

// GetForUpdate locks the assignment row exactly like the original handler
// (only the platform_admin row itself, not the joined user); the fuller,
// joined view is reloaded through Get after the mutation commits.
func (r globalRoleRepository) GetForUpdate(ctx context.Context, id string) (*application.GlobalRoleAssignmentRecord, error) {
	var userID string
	var active bool
	err := r.tx.QueryRow(ctx, `SELECT user_id::text,active FROM account.global_role_assignments WHERE id=$1 AND role='platform_admin' FOR UPDATE`, id).Scan(&userID, &active)
	if err != nil {
		return nil, translate(err, "global role assignment not found")
	}
	return &application.GlobalRoleAssignmentRecord{
		Aggregate: domain.RehydrateGlobalRoleAssignment(domain.GlobalRoleAssignmentSnapshot{ID: id, UserID: userID, Role: domain.PlatformAdmin, Active: active}),
	}, nil
}

// FindByUserAndRole only ever needs to support platform_admin today (the
// only global role the schema defines), matching the original handler's
// hard-coded 'platform_admin' literal.
func (r globalRoleRepository) FindByUserAndRole(ctx context.Context, userID string, _ domain.GlobalRole) (*application.GlobalRoleAssignmentRecord, error) {
	var id string
	var active bool
	err := r.tx.QueryRow(ctx, `SELECT id::text,active FROM account.global_role_assignments WHERE user_id=$1 AND role='platform_admin'`, userID).Scan(&id, &active)
	if err != nil {
		return nil, translate(err, "global role assignment not found")
	}
	return &application.GlobalRoleAssignmentRecord{
		Aggregate: domain.RehydrateGlobalRoleAssignment(domain.GlobalRoleAssignmentSnapshot{ID: id, UserID: userID, Role: domain.PlatformAdmin, Active: active}),
	}, nil
}

func (r globalRoleRepository) CountActiveByRole(ctx context.Context, _ domain.GlobalRole) (int, error) {
	var count int
	err := r.tx.QueryRow(ctx, `SELECT count(*) FROM account.global_role_assignments WHERE role='platform_admin' AND active`).Scan(&count)
	return count, err
}

func (r globalRoleRepository) Insert(ctx context.Context, assignment *domain.GlobalRoleAssignment) (*application.GlobalRoleAssignmentRecord, error) {
	v := assignment.Snapshot()
	var id string
	if err := r.tx.QueryRow(ctx, `INSERT INTO account.global_role_assignments(user_id,role,active,granted_by) VALUES($1,'platform_admin',true,$2) RETURNING id::text`, v.UserID, v.GrantedBy).Scan(&id); err != nil {
		return nil, translate(err, "unable to grant global role")
	}
	return r.Get(ctx, id)
}

// Save applies whichever of the two original mutations matches the
// aggregate's current state: reactivating (Grant) or deactivating (Revoke).
func (r globalRoleRepository) Save(ctx context.Context, record *application.GlobalRoleAssignmentRecord) (*application.GlobalRoleAssignmentRecord, error) {
	v := record.Aggregate.Snapshot()
	var err error
	if v.Active {
		_, err = r.tx.Exec(ctx, `UPDATE account.global_role_assignments SET active=true,granted_by=$2,granted_at=now(),revoked_by=NULL,revoked_at=NULL WHERE id=$1`, v.ID, v.GrantedBy)
	} else {
		_, err = r.tx.Exec(ctx, `UPDATE account.global_role_assignments SET active=false,revoked_by=$2,revoked_at=now() WHERE id=$1`, v.ID, v.RevokedBy)
	}
	if err != nil {
		return nil, translate(err, "unable to save global role assignment")
	}
	return r.Get(ctx, v.ID)
}

func (r globalRoleRepository) RecordAudit(ctx context.Context, audit application.GlobalRoleAudit) error {
	_, err := r.tx.Exec(ctx, `INSERT INTO account.global_role_audit(assignment_id,actor_id,target_user_id,role,action,reason,idempotency_key) VALUES($1,$2,$3,'platform_admin',$4,$5,$6)`,
		audit.AssignmentID, audit.ActorID, audit.TargetUserID, audit.Action, audit.Reason, audit.IdempotencyKey)
	return err
}

// invitationRepository ------------------------------------------------------

type invitationRepository struct{ tx pgx.Tx }

func (r invitationRepository) Create(ctx context.Context, studioID, email, role, invitedBy, reason, idempotencyKey string) (application.InvitationView, error) {
	invitation := application.InvitationView{StudioID: studioID, Email: email, Role: domain.Role(role)}
	var expiresAt time.Time
	err := r.tx.QueryRow(ctx, `INSERT INTO account.invitations(studio_id,email,role,invited_by,reason,idempotency_key) VALUES($1,lower($2),'teacher',$3,$4,$5) ON CONFLICT(studio_id,idempotency_key) DO UPDATE SET idempotency_key=EXCLUDED.idempotency_key RETURNING id::text,status,expires_at`,
		studioID, email, invitedBy, reason, idempotencyKey).Scan(&invitation.ID, &invitation.Status, &expiresAt)
	if err != nil {
		return application.InvitationView{}, translate(err, "unable to create invitation")
	}
	invitation.ExpiresAt = expiresAt
	return invitation, nil
}

// chatRepository --------------------------------------------------------

type chatRepository struct{ tx pgx.Tx }

func (r chatRepository) GetPrincipal(ctx context.Context, userID string) (application.ChatPrincipalView, error) {
	var profile application.ProfileView
	if err := r.tx.QueryRow(ctx, `SELECT id::text,display_name,avatar_url FROM account.get_chat_principal($1)`, userID).Scan(&profile.UserID, &profile.DisplayName, &profile.AvatarURL); err != nil {
		return application.ChatPrincipalView{}, translate(err, "chat principal not found")
	}
	rows, err := r.tx.Query(ctx, `SELECT id::text,studio_id::text,user_id::text,role::text,active FROM account.get_chat_memberships($1)`, userID)
	if err != nil {
		return application.ChatPrincipalView{}, translate(err, "chat principal not found")
	}
	defer rows.Close()
	memberships := make([]application.MembershipView, 0)
	for rows.Next() {
		item, err := scanMembership(rows)
		if err != nil {
			return application.ChatPrincipalView{}, err
		}
		memberships = append(memberships, item)
	}
	if err = rows.Err(); err != nil {
		return application.ChatPrincipalView{}, translate(err, "chat principal not found")
	}
	return application.ChatPrincipalView{Profile: profile, Memberships: memberships}, nil
}

// identityRepository ------------------------------------------------------

type identityRepository struct{ tx pgx.Tx }

func (r identityRepository) ResolveOIDCSubject(ctx context.Context, oidcSubject string) (string, error) {
	if _, err := r.tx.Exec(ctx, `SELECT set_config('app.oidc_subject',$1,true)`, oidcSubject); err != nil {
		return "", translate(err, "identity not provisioned")
	}
	var userID string
	err := r.tx.QueryRow(ctx, `SELECT account.resolve_oidc_subject($1)::text`, oidcSubject).Scan(&userID)
	return userID, translate(err, "identity not provisioned")
}

func translate(err error, message string) error {
	return platform.TranslateDatabaseError(err, message, application.NotFound, application.Conflict)
}
