SET row_security = off;

INSERT INTO catalog.studios (id, slug, name, description, address_line, city, postal_code)
VALUES
  ('10000000-0000-0000-0000-000000000001', 'ha-dance-studio', 'HA Dance Studio', 'A welcoming Bay Area studio for dancers of every level.', '1080 Dance Ave', 'San Jose', '95112'),
  ('10000000-0000-0000-0000-000000000002', '85fun-dance-studio', '85Fun Dance Studio', 'High-energy K-pop, jazz and urban choreography.', '85 Rhythm Street', 'Santa Clara', '95050'),
  ('10000000-0000-0000-0000-000000000003', 'enjoy-dance-studio', 'Enjoy Dance Studio', 'Community classes, workshops and rehearsal rooms.', '320 Joy Lane', 'Sunnyvale', '94086'),
  ('10000000-0000-0000-0000-000000000004', 'lilian-dance-studio', 'Lilian Dance Studio', 'Technique-focused training and performance programs.', '410 Blossom Road', 'Cupertino', '95014'),
  ('10000000-0000-0000-0000-000000000005', 'eva-dance-studio', 'Eva Dance Studio', 'Creative movement and social dance in the Bay Area.', '520 Violet Way', 'Fremont', '94538')
ON CONFLICT (id) DO NOTHING;

INSERT INTO catalog.rooms (studio_id, name, capacity, rental_rate_cents_per_hour, facilities)
SELECT id, 'Main Room', 24, 6000, ARRAY['mirrors', 'sound_system', 'sprung_floor'] FROM catalog.studios
ON CONFLICT (studio_id, name) DO NOTHING;

-- Fixed ids keep the seed idempotent: credit_products has no natural key, so a random
-- id would insert four new products every time the seed job runs.
INSERT INTO catalog.credit_products (id, studio_id, issuer_scope, kind, name)
VALUES
  ('21000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000001', 'STUDIO', 'CREDITS', 'HA 800 Credits'),
  ('21000000-0000-0000-0000-000000000002', '10000000-0000-0000-0000-000000000001', 'STUDIO', 'CREDITS', 'HA 400 Credits'),
  ('21000000-0000-0000-0000-000000000003', '10000000-0000-0000-0000-000000000001', 'STUDIO', 'UNLIMITED', 'HA Annual Unlimited'),
  ('21000000-0000-0000-0000-000000000004', '10000000-0000-0000-0000-000000000001', 'STUDIO', 'UNLIMITED', 'HA Monthly Unlimited')
ON CONFLICT (id) DO NOTHING;

INSERT INTO catalog.credit_products (id, studio_id, issuer_scope, kind, name)
VALUES
  ('20000000-0000-0000-0000-000000000001', NULL, 'PLATFORM', 'CREDITS', 'Universal 200 Credits'),
  ('20000000-0000-0000-0000-000000000002', NULL, 'PLATFORM', 'CREDITS', 'Universal 4 Credits')
ON CONFLICT (id) DO NOTHING;

INSERT INTO catalog.credit_product_versions (product_id, version, amount_cents, credit_amount, validity_days, final_sale)
SELECT id, 1,
  CASE name
    WHEN 'HA 800 Credits' THEN 280000
    WHEN 'HA 400 Credits' THEN 160000
    WHEN 'HA Annual Unlimited' THEN 399900
    WHEN 'HA Monthly Unlimited' THEN 66600
  END,
  CASE WHEN kind = 'CREDITS' THEN regexp_replace(name, '\D', '', 'g')::integer ELSE NULL END,
  CASE WHEN name LIKE '%Monthly%' THEN 30 ELSE 365 END,
  false
FROM catalog.credit_products
WHERE studio_id = '10000000-0000-0000-0000-000000000001'
ON CONFLICT (product_id, version) DO NOTHING;

INSERT INTO catalog.credit_product_versions (product_id, version, amount_cents, credit_amount, validity_days, final_sale)
VALUES
  ('20000000-0000-0000-0000-000000000001', 1, 90000, 200, NULL, false),
  ('20000000-0000-0000-0000-000000000002', 1, 2000, 4, NULL, false)
ON CONFLICT (product_id, version) DO NOTHING;

INSERT INTO account.users(id, oidc_subject, email, display_name, timezone)
VALUES
  ('30000000-0000-0000-0000-000000000001', '70000000-0000-0000-0000-000000000001', 'student@bayareadancehub.local', 'Mia Student', 'America/Los_Angeles'),
  ('30000000-0000-0000-0000-000000000002', '70000000-0000-0000-0000-000000000002', 'teacher@bayareadancehub.local', 'Lina Chen', 'America/Los_Angeles'),
  ('30000000-0000-0000-0000-000000000003', '70000000-0000-0000-0000-000000000003', 'admin@bayareadancehub.local', 'Alex Studio Admin', 'America/Los_Angeles'),
  ('30000000-0000-0000-0000-000000000004', '70000000-0000-0000-0000-000000000004', 'platform@bayareadancehub.local', 'Parker Platform Admin', 'America/Los_Angeles')
ON CONFLICT (id) DO NOTHING;

INSERT INTO account.teacher_profiles(user_id, bio, portfolio_url)
VALUES ('30000000-0000-0000-0000-000000000002', 'Bay Area choreographer teaching K-pop and jazz funk.', 'https://example.com/lina')
ON CONFLICT (user_id) DO NOTHING;

INSERT INTO account.studio_memberships(studio_id, user_id, role)
VALUES
  ('10000000-0000-0000-0000-000000000001', '30000000-0000-0000-0000-000000000001', 'student'),
  ('10000000-0000-0000-0000-000000000001', '30000000-0000-0000-0000-000000000002', 'teacher'),
  ('10000000-0000-0000-0000-000000000001', '30000000-0000-0000-0000-000000000003', 'studio_admin')
ON CONFLICT (studio_id, user_id, role) DO NOTHING;

INSERT INTO account.global_role_assignments(user_id, role, active)
VALUES ('30000000-0000-0000-0000-000000000004', 'platform_admin', true)
ON CONFLICT (user_id, role) DO NOTHING;

INSERT INTO credit.grants(user_id, studio_id, source_order_line_id, fulfillment_key, product_version_id, kind, granted_credits, remaining_credits, valid_from, expires_at)
SELECT
  '30000000-0000-0000-0000-000000000001',
  NULL,
  '40000000-0000-0000-0000-000000000001',
  '40000000-0000-0000-0000-000000000001',
  pv.id,
  'CREDITS',
  200,
  200,
  now(),
  NULL
FROM catalog.credit_product_versions pv
WHERE pv.product_id = '20000000-0000-0000-0000-000000000001'
ON CONFLICT (fulfillment_key) DO NOTHING;

INSERT INTO credit.ledger_entries(user_id, studio_id, grant_id, kind, credit_delta, idempotency_key, reason)
SELECT user_id, studio_id, id, 'GRANT', granted_credits, 'seed:universal-credits', 'development seed'
FROM credit.grants
WHERE source_order_line_id = '40000000-0000-0000-0000-000000000001'
ON CONFLICT (idempotency_key) DO NOTHING;

INSERT INTO scheduling.class_sessions(
  studio_id, room_id, teacher_id, title, description, starts_at, ends_at,
  capacity, minimum_students, credit_cost, status, cancellation_cutoff_at,
  attendance_teacher_deadline, attendance_admin_deadline, studio_timezone
)
SELECT
  studio.id,
  room.id,
  '30000000-0000-0000-0000-000000000002',
  'K-pop Choreography Foundations',
  'Learn a complete chorus with musicality, texture and performance details.',
  date_trunc('day', now() AT TIME ZONE studio.timezone + interval '8 days') AT TIME ZONE studio.timezone + interval '19 hours',
  date_trunc('day', now() AT TIME ZONE studio.timezone + interval '8 days') AT TIME ZONE studio.timezone + interval '20 hours 30 minutes',
  LEAST(20, room.capacity),
  4,
  6,
  'OPEN',
  date_trunc('day', now() AT TIME ZONE studio.timezone + interval '8 days') AT TIME ZONE studio.timezone + interval '15 hours',
  date_trunc('day', now() AT TIME ZONE studio.timezone + interval '9 days') AT TIME ZONE studio.timezone + interval '30 minutes',
  date_trunc('day', now() AT TIME ZONE studio.timezone + interval '9 days') AT TIME ZONE studio.timezone + interval '20 hours 30 minutes',
  studio.timezone
FROM catalog.studios studio
JOIN catalog.rooms room ON room.studio_id = studio.id AND room.name = 'Main Room'
WHERE studio.id = '10000000-0000-0000-0000-000000000001'
  AND NOT EXISTS (SELECT 1 FROM scheduling.class_sessions class_session WHERE class_session.title = 'K-pop Choreography Foundations');

INSERT INTO catalog.campaigns(id, studio_id, product_version_id, name, starts_at, ends_at, inventory, per_user_limit, payment_ttl_minutes)
SELECT
  '50000000-0000-0000-0000-000000000001',
  NULL,
  id,
  'Double 11 Universal Credits Flash Sale',
  now() - interval '1 hour',
  now() + interval '30 days',
  100,
  1,
  10
FROM catalog.credit_product_versions
WHERE product_id = '20000000-0000-0000-0000-000000000001'
ON CONFLICT (id) DO NOTHING;

CREATE TABLE IF NOT EXISTS public.local_seed (
  version text PRIMARY KEY,
  applied_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO public.local_seed(version)
VALUES ('001')
ON CONFLICT (version) DO UPDATE SET applied_at = now();
