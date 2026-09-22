package platform

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// WithTenantTx is the only supported entry point for RLS-protected database
// work. SET LOCAL guarantees that identity state cannot leak through pgxpool.
func WithTenantTx(ctx context.Context, pool *pgxpool.Pool, service string, fn func(pgx.Tx) error) error {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	userID, _ := UserID(ctx)
	studioID, _ := StudioID(ctx)
	_, err = tx.Exec(ctx, `SELECT
		set_config('app.user_id', $1, true),
		set_config('app.studio_id', $2, true),
		set_config('app.tenant_roles', $3, true),
		set_config('app.global_roles', $4, true),
		set_config('app.actor_kind', $5, true),
		set_config('app.service_principal', $6, true),
		set_config('app.service', $7, true),
		set_config('app.request_id', $8, true)`,
		userID, studioID, strings.Join(TenantRoles(ctx), ","), strings.Join(GlobalRoles(ctx), ","), ActorKind(ctx), ServicePrincipal(ctx), service, RequestID(ctx))
	if err != nil {
		return err
	}
	if err = fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
