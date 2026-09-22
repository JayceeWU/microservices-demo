#!/bin/sh
set -eu

: "${DATABASE_ADMIN_URL:?DATABASE_ADMIN_URL is required}"
: "${DATABASE_URL:?DATABASE_URL is required}"
: "${DATABASE_MIGRATOR_PASSWORD:?DATABASE_MIGRATOR_PASSWORD is required}"
: "${ACCOUNT_DB_PASSWORD:?ACCOUNT_DB_PASSWORD is required}"
: "${CATALOG_DB_PASSWORD:?CATALOG_DB_PASSWORD is required}"
: "${SCHEDULING_DB_PASSWORD:?SCHEDULING_DB_PASSWORD is required}"
: "${CREDIT_DB_PASSWORD:?CREDIT_DB_PASSWORD is required}"
: "${ORDERS_DB_PASSWORD:?ORDERS_DB_PASSWORD is required}"
: "${PAYMENT_DB_PASSWORD:?PAYMENT_DB_PASSWORD is required}"
: "${PAYROLL_DB_PASSWORD:?PAYROLL_DB_PASSWORD is required}"
: "${RECOMMENDATION_DB_PASSWORD:?RECOMMENDATION_DB_PASSWORD is required}"
: "${OUTBOX_DB_PASSWORD:?OUTBOX_DB_PASSWORD is required}"
: "${PROJECTION_DB_PASSWORD:?PROJECTION_DB_PASSWORD is required}"
: "${KEYCLOAK_DB_PASSWORD:?KEYCLOAK_DB_PASSWORD is required}"
: "${CHAT_DB_PASSWORD:?CHAT_DB_PASSWORD is required}"
: "${CHAT_RELAY_DB_PASSWORD:?CHAT_RELAY_DB_PASSWORD is required}"
: "${CHAT_MEDIA_DB_PASSWORD:?CHAT_MEDIA_DB_PASSWORD is required}"

psql "$DATABASE_ADMIN_URL" -v ON_ERROR_STOP=1 \
  -v migrator_password="$DATABASE_MIGRATOR_PASSWORD" \
  -v account_password="$ACCOUNT_DB_PASSWORD" \
  -v catalog_password="$CATALOG_DB_PASSWORD" \
  -v scheduling_password="$SCHEDULING_DB_PASSWORD" \
  -v credit_password="$CREDIT_DB_PASSWORD" \
  -v orders_password="$ORDERS_DB_PASSWORD" \
  -v payment_password="$PAYMENT_DB_PASSWORD" \
  -v payroll_password="$PAYROLL_DB_PASSWORD" \
  -v recommendation_password="$RECOMMENDATION_DB_PASSWORD" \
  -v outbox_password="$OUTBOX_DB_PASSWORD" \
  -v projection_password="$PROJECTION_DB_PASSWORD" \
  -v keycloak_password="$KEYCLOAK_DB_PASSWORD" \
  -v chat_password="$CHAT_DB_PASSWORD" \
  -v chat_relay_password="$CHAT_RELAY_DB_PASSWORD" \
  -v chat_media_password="$CHAT_MEDIA_DB_PASSWORD" <<'SQL'
SELECT format('CREATE ROLE %I LOGIN PASSWORD %L NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS', role_name, role_password)
FROM (VALUES
  ('dancehub_migrator', :'migrator_password'),
  ('account_runtime', :'account_password'),
  ('catalog_runtime', :'catalog_password'),
  ('scheduling_runtime', :'scheduling_password'),
  ('credit_runtime', :'credit_password'),
  ('orders_runtime', :'orders_password'),
  ('payment_runtime', :'payment_password'),
  ('payroll_runtime', :'payroll_password'),
  ('recommendation_runtime', :'recommendation_password'),
  ('outbox_worker', :'outbox_password'),
  ('projection_worker', :'projection_password'),
  ('keycloak_runtime', :'keycloak_password'),
  ('chat_runtime', :'chat_password'),
  ('chat_relay', :'chat_relay_password'),
  ('chat_media_worker', :'chat_media_password')
) AS roles(role_name, role_password)
WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname=role_name) \gexec

-- SECURITY DEFINER functions used by AccountService return only the minimum
-- public chat identity fields. Their non-login owner can cross FORCE RLS,
-- while every connectable runtime and worker role remains subject to RLS.
DO $$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname='account_chat_definer') THEN
    CREATE ROLE account_chat_definer NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE BYPASSRLS;
  ELSE
    ALTER ROLE account_chat_definer NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE BYPASSRLS;
  END IF;
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname='chat_invariant_definer') THEN
    CREATE ROLE chat_invariant_definer NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE BYPASSRLS;
  ELSE
    ALTER ROLE chat_invariant_definer NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE BYPASSRLS;
  END IF;
END $$;

SELECT format('ALTER ROLE %I LOGIN PASSWORD %L NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS', role_name, role_password)
FROM (VALUES
  ('dancehub_migrator', :'migrator_password'),
  ('account_runtime', :'account_password'),
  ('catalog_runtime', :'catalog_password'),
  ('scheduling_runtime', :'scheduling_password'),
  ('credit_runtime', :'credit_password'),
  ('orders_runtime', :'orders_password'),
  ('payment_runtime', :'payment_password'),
  ('payroll_runtime', :'payroll_password'),
  ('recommendation_runtime', :'recommendation_password'),
  ('outbox_worker', :'outbox_password'),
  ('projection_worker', :'projection_password'),
  ('keycloak_runtime', :'keycloak_password'),
  ('chat_runtime', :'chat_password'),
  ('chat_relay', :'chat_relay_password'),
  ('chat_media_worker', :'chat_media_password')
) AS roles(role_name, role_password) \gexec

SELECT format('GRANT CREATE ON DATABASE %I TO dancehub_migrator', current_database()) \gexec
GRANT CREATE, USAGE ON SCHEMA public TO dancehub_migrator;

-- The restricted migrator owns its bookkeeping table.
CREATE TABLE IF NOT EXISTS public.schema_migrations (
  version text PRIMARY KEY,
  applied_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE public.schema_migrations OWNER TO dancehub_migrator;
GRANT SELECT, INSERT, UPDATE, DELETE ON public.schema_migrations TO dancehub_migrator;

GRANT account_runtime,catalog_runtime,scheduling_runtime,credit_runtime,
  orders_runtime,payment_runtime,payroll_runtime,recommendation_runtime,
  outbox_worker,projection_worker,keycloak_runtime,chat_runtime,chat_relay,chat_media_worker
  TO dancehub_migrator WITH ADMIN OPTION;
GRANT account_chat_definer TO dancehub_migrator WITH ADMIN OPTION;
GRANT chat_invariant_definer TO dancehub_migrator WITH ADMIN OPTION;
ALTER ROLE keycloak_runtime SET search_path TO keycloak;
SQL

psql "$DATABASE_URL" -v ON_ERROR_STOP=1 <<'SQL'
CREATE TABLE IF NOT EXISTS public.schema_migrations (
  version text PRIMARY KEY,
  applied_at timestamptz NOT NULL DEFAULT now()
);
SQL

for migration in /migrations/*.sql; do
  version="$(basename "$migration")"
  applied="$(psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -tAc "SELECT 1 FROM public.schema_migrations WHERE version='$version'")"
  if [ "$applied" = "1" ]; then
    continue
  fi
  # The migration file and its bookkeeping row commit together, so a failure half-way
  # cannot leave a partially applied schema that the next run would refuse to re-apply.
  psql "$DATABASE_URL" -v ON_ERROR_STOP=1 --single-transaction \
    -f "$migration" \
    -c "INSERT INTO public.schema_migrations(version) VALUES ('$version')"
done
