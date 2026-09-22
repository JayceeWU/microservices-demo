\set ON_ERROR_STOP on

DO $$
DECLARE insecure_roles integer;
BEGIN
  SELECT count(*) INTO insecure_roles
  FROM pg_roles
  WHERE rolname IN (
    'account_runtime','catalog_runtime','scheduling_runtime','credit_runtime',
    'orders_runtime','payment_runtime','payroll_runtime','recommendation_runtime',
    'outbox_worker','projection_worker','chat_runtime','chat_relay','chat_media_worker'
  ) AND (rolsuper OR rolbypassrls);
  IF insecure_roles <> 0 THEN
    RAISE EXCEPTION 'runtime or worker role has SUPERUSER/BYPASSRLS';
  END IF;
END $$;

DO $$
BEGIN
  IF has_table_privilege('account_runtime', 'account.global_role_audit', 'UPDATE')
     OR has_table_privilege('account_runtime', 'account.global_role_audit', 'DELETE')
     OR has_table_privilege('account_runtime', 'account.global_role_audit', 'TRUNCATE') THEN
    RAISE EXCEPTION 'global role audit must be immutable to account_runtime';
  END IF;
  IF NOT EXISTS (
    SELECT 1 FROM pg_trigger
    WHERE tgrelid='account.global_role_audit'::regclass
      AND tgname='global_role_audit_immutable'
      AND tgenabled <> 'D'
      AND NOT tgisinternal
  ) THEN
    RAISE EXCEPTION 'global role audit immutability trigger is missing or disabled';
  END IF;
END $$;

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_roles
    WHERE rolname='chat_invariant_definer' AND NOT rolcanlogin AND rolbypassrls AND NOT rolsuper
  ) THEN
    RAISE EXCEPTION 'chat invariant definer must be a non-login, non-superuser BYPASSRLS role';
  END IF;
END $$;

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_roles
    WHERE rolname='account_chat_definer' AND NOT rolcanlogin AND rolbypassrls AND NOT rolsuper
  ) THEN
    RAISE EXCEPTION 'chat definer must be a non-login, non-superuser BYPASSRLS role';
  END IF;
END $$;

BEGIN;
SET LOCAL ROLE account_runtime;
SELECT set_config('app.user_id', '30000000-0000-0000-0000-000000000001', true);
SELECT set_config('app.studio_id', '10000000-0000-0000-0000-000000000001', true);
SELECT set_config('app.tenant_roles', 'student', true);
SELECT set_config('app.global_roles', '', true);
SELECT set_config('app.actor_kind', 'human', true);
SELECT set_config('app.service', 'accountservice', true);
DO $$
BEGIN
  IF (SELECT count(*) FROM account.users) <> 1 THEN
    RAISE EXCEPTION 'student can read another user through account RLS';
  END IF;
  IF (SELECT count(*) FROM account.studio_memberships WHERE user_id <> app_security.user_id()) <> 0 THEN
    RAISE EXCEPTION 'student can read another membership through account RLS';
  END IF;
  IF (SELECT count(*) FROM account.get_chat_principal('30000000-0000-0000-0000-000000000002')) <> 1 THEN
    RAISE EXCEPTION 'minimal chat principal lookup cannot resolve an active teacher';
  END IF;
  IF (SELECT count(*) FROM account.search_public_teachers('10000000-0000-0000-0000-000000000001','%',20)) < 1 THEN
    RAISE EXCEPTION 'public teacher lookup is hidden by account RLS';
  END IF;
  IF EXISTS (
    SELECT 1 FROM pg_class c
    JOIN pg_namespace n ON n.oid = c.relnamespace
    WHERE n.nspname = 'catalog' AND c.relname = 'studios'
      AND has_table_privilege(current_user, c.oid, 'SELECT')
  ) THEN
    RAISE EXCEPTION 'account runtime role can cross the catalog schema boundary';
  END IF;
END $$;
ROLLBACK;

BEGIN;
SELECT set_config('app.actor_kind', 'human', true);
SELECT set_config('app.tenant_roles', 'platform_admin', true);
SELECT set_config('app.global_roles', '', true);
DO $$ BEGIN
  IF app_security.platform_admin() THEN
    RAISE EXCEPTION 'tenant role was accepted as a global platform administrator';
  END IF;
END $$;
SELECT set_config('app.tenant_roles', '', true);
SELECT set_config('app.global_roles', 'platform_admin', true);
DO $$ BEGIN
  IF NOT app_security.platform_admin() THEN
    RAISE EXCEPTION 'global platform administrator role was not recognized';
  END IF;
END $$;
SELECT set_config('app.actor_kind', 'service', true);
SELECT set_config('app.global_roles', '', true);
SELECT set_config('app.service', 'schedulingservice', true);
SELECT set_config('app.service_principal', 'scheduling-worker', true);
DO $$ BEGIN
  IF NOT app_security.service_principal_is('scheduling-worker') THEN
    RAISE EXCEPTION 'matching scheduling worker principal was not recognized';
  END IF;
END $$;
SELECT set_config('app.service_principal', 'orderservice', true);
DO $$ BEGIN
  IF app_security.service_principal_is('scheduling-worker') THEN
    RAISE EXCEPTION 'mismatched service principal was accepted';
  END IF;
END $$;
ROLLBACK;

BEGIN;
INSERT INTO orders.orders(id,user_id,studio_id,issuer_scope,status,total_amount_cents,idempotency_key)
VALUES
  ('95000000-0000-0000-0000-000000000001','30000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000001','STUDIO','PAID',1000,'rls-order-1'),
  ('95000000-0000-0000-0000-000000000002','30000000-0000-0000-0000-000000000002','10000000-0000-0000-0000-000000000001','STUDIO','PAID',1000,'rls-order-2');
SET LOCAL ROLE orders_runtime;
SELECT set_config('app.user_id', '30000000-0000-0000-0000-000000000001', true);
SELECT set_config('app.studio_id', '10000000-0000-0000-0000-000000000001', true);
SELECT set_config('app.tenant_roles', 'student', true);
SELECT set_config('app.global_roles', '', true);
SELECT set_config('app.actor_kind', 'human', true);
SELECT set_config('app.service_principal', '', true);
SELECT set_config('app.service', 'orderservice', true);
DO $$ BEGIN
  IF (SELECT count(*) FROM orders.orders) <> 1 THEN
    RAISE EXCEPTION 'human request inherited the orderservice worker bypass';
  END IF;
END $$;
SELECT set_config('app.user_id', '', true);
SELECT set_config('app.studio_id', '', true);
SELECT set_config('app.tenant_roles', '', true);
SELECT set_config('app.actor_kind', 'service', true);
SELECT set_config('app.service_principal', 'orderservice', true);
DO $$ BEGIN
  IF (SELECT count(*) FROM orders.orders) <> 2 THEN
    RAISE EXCEPTION 'orders worker cannot access fulfillment records';
  END IF;
END $$;
SELECT set_config('app.service_principal', 'scheduling-worker', true);
DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM orders.orders) THEN
    RAISE EXCEPTION 'unrelated worker principal can access order records';
  END IF;
END $$;
SELECT set_config('app.service_principal', 'payment-order-saga', true);
DO $$ BEGIN
  IF (SELECT count(*) FROM orders.orders) <> 2 THEN
    RAISE EXCEPTION 'payment saga cannot read the order records it updates';
  END IF;
END $$;
ROLLBACK;

BEGIN;
SET LOCAL ROLE scheduling_runtime;
SELECT set_config('app.user_id', '30000000-0000-0000-0000-000000000001', true);
SELECT set_config('app.studio_id', '10000000-0000-0000-0000-000000000001', true);
SELECT set_config('app.tenant_roles', 'student', true);
SELECT set_config('app.global_roles', '', true);
SELECT set_config('app.actor_kind', 'human', true);
SELECT set_config('app.service', 'schedulingservice', true);
DO $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM scheduling.class_sessions
    WHERE studio_id <> app_security.studio_id()
  ) THEN
    RAISE EXCEPTION 'cross-studio class session leaked through RLS';
  END IF;
  IF EXISTS (
    SELECT 1 FROM scheduling.bookings
    WHERE student_id <> app_security.user_id()
  ) THEN
    RAISE EXCEPTION 'another student booking leaked through RLS';
  END IF;
END $$;
ROLLBACK;

BEGIN;
SET LOCAL ROLE outbox_worker;
SELECT set_config('app.service', 'outbox-relay', true);
SELECT count(*) FROM scheduling.outbox_events;
SELECT count(*) FROM payment.outbox_events;
ROLLBACK;

BEGIN;
INSERT INTO chat.conversations(
  id,kind,studio_id,student_id,teacher_id,direct_low_user_id,direct_high_user_id,
  owner_user_id,title,member_count,last_sequence,created_at
) VALUES (
  '91000000-0000-0000-0000-000000000001','DIRECT_TEACHER',NULL,
  '30000000-0000-0000-0000-000000000001','30000000-0000-0000-0000-000000000099',
  '30000000-0000-0000-0000-000000000001','30000000-0000-0000-0000-000000000099',
  NULL,'',2,1,now()
),(
  '91000000-0000-0000-0000-000000000002','STUDIO_GROUP','10000000-0000-0000-0000-000000000001',
  NULL,NULL,NULL,NULL,'30000000-0000-0000-0000-000000000003','RLS fixture group',1,2,now()
);
INSERT INTO chat.conversation_participants(
  conversation_id,user_id,studio_id,owner_user_id,role,state,visible_from_sequence
) VALUES
  ('91000000-0000-0000-0000-000000000001','30000000-0000-0000-0000-000000000001',NULL,'30000000-0000-0000-0000-000000000001','MEMBER','ACTIVE',1),
  ('91000000-0000-0000-0000-000000000001','30000000-0000-0000-0000-000000000099',NULL,'30000000-0000-0000-0000-000000000001','MEMBER','ACTIVE',1),
  ('91000000-0000-0000-0000-000000000002','30000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000001','30000000-0000-0000-0000-000000000003','MEMBER','ACTIVE',2);
INSERT INTO chat.messages(
  id,conversation_id,studio_id,conversation_kind,sequence,sender_id,
  sender_display_name,client_message_id,kind,body,created_at
) VALUES
  ('92000000-0000-0000-0000-000000000001','91000000-0000-0000-0000-000000000001',NULL,'DIRECT_TEACHER',1,'30000000-0000-0000-0000-000000000099','Teacher','93000000-0000-0000-0000-000000000001','TEXT','private fixture',now()),
  ('92000000-0000-0000-0000-000000000002','91000000-0000-0000-0000-000000000002','10000000-0000-0000-0000-000000000001','STUDIO_GROUP',1,'30000000-0000-0000-0000-000000000003','Admin','93000000-0000-0000-0000-000000000002','TEXT','before join',now()),
  ('92000000-0000-0000-0000-000000000003','91000000-0000-0000-0000-000000000002','10000000-0000-0000-0000-000000000001','STUDIO_GROUP',2,'30000000-0000-0000-0000-000000000003','Admin','93000000-0000-0000-0000-000000000003','TEXT','after join',now());

SET LOCAL ROLE chat_runtime;
SELECT set_config('app.user_id', '30000000-0000-0000-0000-000000000001', true);
SELECT set_config('app.studio_id', '10000000-0000-0000-0000-000000000001', true);
SELECT set_config('app.tenant_roles', 'student', true);
SELECT set_config('app.global_roles', '', true);
SELECT set_config('app.actor_kind', 'human', true);
SELECT set_config('app.service', 'chatservice', true);
DO $$
DECLARE deleted_rows integer;
BEGIN
  IF (SELECT count(*) FROM chat.messages WHERE conversation_id='91000000-0000-0000-0000-000000000001') <> 1 THEN
    RAISE EXCEPTION 'direct conversation participant cannot read their history';
  END IF;
  IF (SELECT count(*) FROM chat.messages WHERE conversation_id='91000000-0000-0000-0000-000000000002') <> 1 THEN
    RAISE EXCEPTION 'group member history is not limited by visible_from_sequence';
  END IF;
  DELETE FROM chat.conversations WHERE id='91000000-0000-0000-0000-000000000002';
  GET DIAGNOSTICS deleted_rows = ROW_COUNT;
  IF deleted_rows <> 0 THEN
    RAISE EXCEPTION 'ordinary group member can delete a group';
  END IF;
END $$;

RESET ROLE;
SET LOCAL ROLE chat_runtime;
SELECT set_config('app.user_id', '30000000-0000-0000-0000-000000000098', true);
SELECT set_config('app.studio_id', '', true);
SELECT set_config('app.tenant_roles', '', true);
SELECT set_config('app.global_roles', 'platform_admin', true);
SELECT set_config('app.actor_kind', 'human', true);
SELECT set_config('app.service', 'chatservice', true);
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM chat.messages) THEN
    RAISE EXCEPTION 'platform admin can browse private chat content without a report snapshot';
  END IF;
END $$;

RESET ROLE;
SET LOCAL ROLE chat_runtime;
SELECT set_config('app.user_id', '30000000-0000-0000-0000-000000000003', true);
SELECT set_config('app.studio_id', '10000000-0000-0000-0000-000000000001', true);
SELECT set_config('app.tenant_roles', 'studio_admin', true);
SELECT set_config('app.global_roles', '', true);
SELECT set_config('app.actor_kind', 'human', true);
SELECT set_config('app.service', 'chatservice', true);
DO $$
BEGIN
  IF (SELECT count(*) FROM chat.messages WHERE conversation_id='91000000-0000-0000-0000-000000000002') <> 2 THEN
    RAISE EXCEPTION 'studio admin cannot inspect complete history of their studio group';
  END IF;
  IF EXISTS (SELECT 1 FROM chat.messages WHERE conversation_id='91000000-0000-0000-0000-000000000001') THEN
    RAISE EXCEPTION 'studio admin can browse an unrelated teacher private conversation';
  END IF;
END $$;
ROLLBACK;

DO $$
DECLARE missing integer;
BEGIN
  SELECT count(*) INTO missing
  FROM (VALUES
    ('account','users'),('account','studio_memberships'),('account','teacher_profiles'),('account','invitations'),
    ('account','global_role_assignments'),('account','global_role_audit'),
    ('catalog','studios'),('catalog','rooms'),('catalog','credit_products'),('catalog','credit_product_versions'),('catalog','campaigns'),
    ('scheduling','class_sessions'),('scheduling','bookings'),('scheduling','room_reservations'),('scheduling','attendance_audit'),
    ('credit','grants'),('credit','holds'),('credit','hold_allocations'),('credit','ledger_entries'),('credit','grant_operations'),
    ('orders','orders'),('orders','order_lines'),('orders','flashsale_requests'),('orders','flashsale_inventory_ledger'),
    ('payment','payments'),('payment','webhook_events'),('payroll','monthly_class_facts'),
    ('recommendation','studio_metrics')
    ,('chat','conversations'),('chat','conversation_participants'),('chat','messages'),
    ('chat','attachments'),('chat','user_blocks'),('chat','group_bans'),('chat','message_reports'),
    ('chat','moderation_audit'),('chat','realtime_outbox'),('chat','inbox_events'),
    ('chat','media_deletion_jobs')
  ) expected(schema_name, table_name)
  LEFT JOIN pg_namespace n ON n.nspname = expected.schema_name
  LEFT JOIN pg_class c ON c.relnamespace = n.oid AND c.relname = expected.table_name
  WHERE NOT COALESCE(c.relrowsecurity AND c.relforcerowsecurity, false);
  IF missing <> 0 THEN
    RAISE EXCEPTION '% expected tables are not FORCE RLS', missing;
  END IF;
END $$;

SELECT 'RLS policy and least-privilege checks passed' AS result;

-- The chat SECURITY DEFINER functions evaluate tenant predicates as their owner; a fresh
-- database must therefore let that owner execute the functions PUBLIC may not call.
DO $$
BEGIN
  IF NOT has_function_privilege('chat_invariant_definer', 'app_security.has_tenant_role(text)', 'EXECUTE')
     OR NOT has_function_privilege('chat_invariant_definer', 'app_security.has_global_role(text)', 'EXECUTE')
     OR NOT has_function_privilege('chat_invariant_definer', 'app_security.service_principal_is(text)', 'EXECUTE') THEN
    RAISE EXCEPTION 'chat_invariant_definer cannot execute the app_security predicates used by chat.next_message_sequence';
  END IF;
END $$;
