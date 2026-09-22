package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type DatabaseErrorKind uint8

const (
	DatabaseErrorOther DatabaseErrorKind = iota
	DatabaseErrorNotFound
	DatabaseErrorConflict
)

func WithActorTx(ctx context.Context, pool *pgxpool.Pool, service, userID, studioID, actorKind, servicePrincipal string, tenantRoles, globalRoles []string, fn func(context.Context, pgx.Tx) error) error {
	identity := HumanIdentityContext(ctx, userID, studioID, tenantRoles, globalRoles)
	if actorKind == "service" {
		identity = ServiceIdentityContext(ctx, servicePrincipal)
	}
	return WithTenantTx(identity, pool, service, func(tx pgx.Tx) error {
		return fn(identity, tx)
	})
}

type AuthorizationActor interface {
	AuthorizationIdentity() (string, string, string, string, []string, []string)
}

type UnitOfWork[Actor AuthorizationActor, Repositories any] struct {
	Pool            *pgxpool.Pool
	NewRepositories func(pgx.Tx) Repositories
}

func (u UnitOfWork[Actor, Repositories]) Do(ctx context.Context, actor Actor, service string, fn func(context.Context, Repositories) error) error {
	userID, studioID, actorKind, principal, tenantRoles, globalRoles := actor.AuthorizationIdentity()
	return WithActorTx(ctx, u.Pool, service, userID, studioID, actorKind, principal, tenantRoles, globalRoles, func(txContext context.Context, tx pgx.Tx) error {
		return fn(txContext, u.NewRepositories(tx))
	})
}

func NewUnitOfWork[Actor AuthorizationActor, Repositories any](pool *pgxpool.Pool, factory func(pgx.Tx) Repositories) UnitOfWork[Actor, Repositories] {
	return UnitOfWork[Actor, Repositories]{Pool: pool, NewRepositories: factory}
}

func ClassifyDatabaseError(err error) DatabaseErrorKind {
	if errors.Is(err, pgx.ErrNoRows) {
		return DatabaseErrorNotFound
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23505", "23P01", "23514":
			return DatabaseErrorConflict
		}
	}
	return DatabaseErrorOther
}

func OutboxFields(ctx context.Context, value map[string]any) ([]byte, string, string, string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, "", "", "", err
	}
	traceparent, tracestate, baggage := TraceHeaders(ctx)
	return payload, traceparent, tracestate, baggage, nil
}

func AppendOutbox(ctx context.Context, tx pgx.Tx, statement string, payloadValue map[string]any, args ...any) error {
	payload, traceparent, tracestate, baggage, err := OutboxFields(ctx, payloadValue)
	if err != nil {
		return err
	}
	args = append(args, payload, traceparent, tracestate, baggage)
	_, err = tx.Exec(ctx, statement, args...)
	return err
}

func TranslateDatabaseError(err error, message string, notFound, conflict func(string) error) error {
	if err == nil {
		return nil
	}
	switch ClassifyDatabaseError(err) {
	case DatabaseErrorNotFound:
		return notFound(message)
	case DatabaseErrorConflict:
		return conflict(message)
	default:
		return fmt.Errorf("%s: %w", message, err)
	}
}
