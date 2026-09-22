--
-- PostgreSQL database dump
--

\restrict QxdVuctbvq7onrDH3aqbEdyBiCgAxmKPH4IP3uF81EnX7XYqaUwnFYZ6H5s8oiz

-- Dumped from database version 17.11
-- Dumped by pg_dump version 17.11

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET transaction_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

--
-- Name: account; Type: SCHEMA; Schema: -; Owner: -
--

CREATE SCHEMA account;


--
-- Name: app_security; Type: SCHEMA; Schema: -; Owner: -
--

CREATE SCHEMA app_security;


--
-- Name: catalog; Type: SCHEMA; Schema: -; Owner: -
--

CREATE SCHEMA catalog;


--
-- Name: chat; Type: SCHEMA; Schema: -; Owner: -
--

CREATE SCHEMA chat;


--
-- Name: credit; Type: SCHEMA; Schema: -; Owner: -
--

CREATE SCHEMA credit;


--
-- Name: keycloak; Type: SCHEMA; Schema: -; Owner: -
--

CREATE SCHEMA keycloak;

ALTER SCHEMA keycloak OWNER TO keycloak_runtime;


--
-- Name: orders; Type: SCHEMA; Schema: -; Owner: -
--

CREATE SCHEMA orders;


--
-- Name: payment; Type: SCHEMA; Schema: -; Owner: -
--

CREATE SCHEMA payment;


--
-- Name: payroll; Type: SCHEMA; Schema: -; Owner: -
--

CREATE SCHEMA payroll;


--
-- Name: recommendation; Type: SCHEMA; Schema: -; Owner: -
--

CREATE SCHEMA recommendation;


--
-- Name: scheduling; Type: SCHEMA; Schema: -; Owner: -
--

CREATE SCHEMA scheduling;


--
-- Name: btree_gist; Type: EXTENSION; Schema: -; Owner: -
--

CREATE EXTENSION IF NOT EXISTS btree_gist WITH SCHEMA public;


--
-- Name: pgcrypto; Type: EXTENSION; Schema: -; Owner: -
--

CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA public;


--
-- Name: global_role; Type: TYPE; Schema: account; Owner: -
--

CREATE TYPE account.global_role AS ENUM (
    'platform_admin'
);


--
-- Name: membership_role; Type: TYPE; Schema: account; Owner: -
--

CREATE TYPE account.membership_role AS ENUM (
    'student',
    'teacher',
    'studio_admin'
);


--
-- Name: issuer_scope; Type: TYPE; Schema: catalog; Owner: -
--

CREATE TYPE catalog.issuer_scope AS ENUM (
    'STUDIO',
    'PLATFORM'
);


--
-- Name: product_kind; Type: TYPE; Schema: catalog; Owner: -
--

CREATE TYPE catalog.product_kind AS ENUM (
    'CREDITS',
    'UNLIMITED'
);


--
-- Name: attachment_status; Type: TYPE; Schema: chat; Owner: -
--

CREATE TYPE chat.attachment_status AS ENUM (
    'PENDING_UPLOAD',
    'PENDING_SCAN',
    'CLEAN',
    'REJECTED'
);


--
-- Name: conversation_kind; Type: TYPE; Schema: chat; Owner: -
--

CREATE TYPE chat.conversation_kind AS ENUM (
    'DIRECT_TEACHER',
    'STUDIO_SUPPORT',
    'STUDIO_GROUP',
    'TEACHER_GROUP'
);


--
-- Name: member_state; Type: TYPE; Schema: chat; Owner: -
--

CREATE TYPE chat.member_state AS ENUM (
    'ACTIVE',
    'LEFT',
    'BANNED'
);


--
-- Name: message_kind; Type: TYPE; Schema: chat; Owner: -
--

CREATE TYPE chat.message_kind AS ENUM (
    'TEXT',
    'IMAGE',
    'VIDEO',
    'SYSTEM'
);


--
-- Name: participant_role; Type: TYPE; Schema: chat; Owner: -
--

CREATE TYPE chat.participant_role AS ENUM (
    'MEMBER',
    'OWNER',
    'MODERATOR'
);


--
-- Name: ledger_kind; Type: TYPE; Schema: credit; Owner: -
--

CREATE TYPE credit.ledger_kind AS ENUM (
    'GRANT',
    'HOLD',
    'RELEASE',
    'CAPTURE',
    'REVERSAL',
    'REFUND',
    'ADJUSTMENT',
    'ENTITLEMENT_USE',
    'TRANSFER_OUT',
    'TRANSFER_IN'
);


--
-- Name: order_status; Type: TYPE; Schema: orders; Owner: -
--

CREATE TYPE orders.order_status AS ENUM (
    'DRAFT',
    'PENDING_PAYMENT',
    'PAID',
    'PAID_NOT_FULFILLED',
    'FULFILLED',
    'PAYMENT_FAILED',
    'EXPIRED',
    'REFUND_PENDING',
    'REFUNDED'
);


--
-- Name: booking_status; Type: TYPE; Schema: scheduling; Owner: -
--

CREATE TYPE scheduling.booking_status AS ENUM (
    'PENDING_CREDIT',
    'CONFIRMED',
    'CHARGED',
    'ATTENDED',
    'NO_SHOW',
    'CANCELLED',
    'REVERSED'
);


--
-- Name: class_status; Type: TYPE; Schema: scheduling; Owner: -
--

CREATE TYPE scheduling.class_status AS ENUM (
    'PENDING_APPROVAL',
    'APPROVED',
    'OPEN',
    'MINIMUM_CONFIRMED',
    'AWAITING_ADMIN_CONFIRMATION',
    'COMPLETED',
    'CANCELLED'
);


--
-- Name: room_reservation_status; Type: TYPE; Schema: scheduling; Owner: -
--

CREATE TYPE scheduling.room_reservation_status AS ENUM (
    'HOLD',
    'PENDING_PAYMENT',
    'CONFIRMED',
    'EXPIRED',
    'REFUND_PENDING',
    'REFUNDED',
    'CANCELLED'
);


--
-- Name: get_chat_memberships(uuid); Type: FUNCTION; Schema: account; Owner: -
--

CREATE FUNCTION account.get_chat_memberships(requested_user uuid) RETURNS TABLE(id uuid, studio_id uuid, user_id uuid, role account.membership_role, active boolean)
    LANGUAGE sql STABLE SECURITY DEFINER
    SET search_path TO 'pg_catalog', 'account'
    AS $$
  SELECT membership.id,membership.studio_id,membership.user_id,membership.role,membership.active
  FROM account.studio_memberships membership
  WHERE membership.user_id = requested_user AND membership.active
$$;


--
-- Name: get_chat_principal(uuid); Type: FUNCTION; Schema: account; Owner: -
--

CREATE FUNCTION account.get_chat_principal(requested_user uuid) RETURNS TABLE(id uuid, display_name text, avatar_url text)
    LANGUAGE sql STABLE SECURITY DEFINER
    SET search_path TO 'pg_catalog', 'account'
    AS $$
  SELECT u.id,u.display_name,COALESCE(u.avatar_url,'')
  FROM account.users u
  WHERE u.id=requested_user
    AND EXISTS (SELECT 1 FROM account.studio_memberships m WHERE m.user_id=u.id AND m.active)
$$;


--
-- Name: reject_global_role_audit_mutation(); Type: FUNCTION; Schema: account; Owner: -
--

CREATE FUNCTION account.reject_global_role_audit_mutation() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
  RAISE EXCEPTION 'account.global_role_audit is immutable';
END
$$;


--
-- Name: resolve_oidc_subject(text); Type: FUNCTION; Schema: account; Owner: -
--

CREATE FUNCTION account.resolve_oidc_subject(subject text) RETURNS uuid
    LANGUAGE sql STABLE SECURITY DEFINER
    SET search_path TO 'pg_catalog', 'account'
    AS $$ SELECT id FROM account.users WHERE oidc_subject=subject $$;


--
-- Name: search_public_teachers(uuid, text, integer); Type: FUNCTION; Schema: account; Owner: -
--

CREATE FUNCTION account.search_public_teachers(selected_studio uuid, name_query text, max_rows integer) RETURNS TABLE(id uuid, display_name text, avatar_url text, bio text, portfolio_url text)
    LANGUAGE sql STABLE SECURITY DEFINER
    SET search_path TO 'pg_catalog', 'account'
    AS $$
  SELECT u.id,u.display_name,COALESCE(u.avatar_url,''),COALESCE(tp.bio,''),COALESCE(tp.portfolio_url,'')
  FROM account.studio_memberships membership
  JOIN account.users u ON u.id=membership.user_id
  LEFT JOIN account.teacher_profiles tp ON tp.user_id=u.id
  WHERE membership.studio_id=selected_studio
    AND membership.role='teacher'
    AND membership.active
    AND u.display_name ILIKE name_query
  ORDER BY u.display_name
  LIMIT LEAST(GREATEST(max_rows,1),100)
$$;


--
-- Name: has_global_role(text); Type: FUNCTION; Schema: app_security; Owner: -
--

CREATE FUNCTION app_security.has_global_role(role_name text) RETURNS boolean
    LANGUAGE sql STABLE
    AS $$
  SELECT role_name = ANY(string_to_array(COALESCE(app_security.setting('app.global_roles'), ''), ','))
$$;


--
-- Name: has_tenant_role(text); Type: FUNCTION; Schema: app_security; Owner: -
--

CREATE FUNCTION app_security.has_tenant_role(role_name text) RETURNS boolean
    LANGUAGE sql STABLE
    AS $$
  SELECT role_name = ANY(string_to_array(COALESCE(app_security.setting('app.tenant_roles'), ''), ','))
$$;


--
-- Name: owns_studio(uuid); Type: FUNCTION; Schema: app_security; Owner: -
--

CREATE FUNCTION app_security.owns_studio(row_studio_id uuid) RETURNS boolean
    LANGUAGE sql STABLE
    AS $$
  SELECT app_security.platform_admin()
      OR (row_studio_id = app_security.studio_id()
          AND app_security.has_tenant_role('studio_admin'))
$$;


--
-- Name: owns_user(uuid); Type: FUNCTION; Schema: app_security; Owner: -
--

CREATE FUNCTION app_security.owns_user(row_user_id uuid) RETURNS boolean
    LANGUAGE sql STABLE
    AS $$
  SELECT row_user_id = app_security.user_id() OR app_security.platform_admin()
$$;


--
-- Name: platform_admin(); Type: FUNCTION; Schema: app_security; Owner: -
--

CREATE FUNCTION app_security.platform_admin() RETURNS boolean
    LANGUAGE sql STABLE
    AS $$
  SELECT app_security.setting('app.actor_kind')='human'
     AND app_security.has_global_role('platform_admin')
$$;


--
-- Name: service_is(text); Type: FUNCTION; Schema: app_security; Owner: -
--

CREATE FUNCTION app_security.service_is(service_name text) RETURNS boolean
    LANGUAGE sql STABLE
    AS $$
  SELECT COALESCE(app_security.setting('app.service'), '') = service_name
$$;


--
-- Name: service_principal_is(text); Type: FUNCTION; Schema: app_security; Owner: -
--

CREATE FUNCTION app_security.service_principal_is(service_name text) RETURNS boolean
    LANGUAGE sql STABLE
    AS $$
  SELECT app_security.setting('app.actor_kind')='service'
     AND app_security.setting('app.service_principal')=service_name
$$;


--
-- Name: setting(text); Type: FUNCTION; Schema: app_security; Owner: -
--

CREATE FUNCTION app_security.setting(name text) RETURNS text
    LANGUAGE sql STABLE
    AS $$
  SELECT NULLIF(current_setting(name, true), '')
$$;


--
-- Name: studio_id(); Type: FUNCTION; Schema: app_security; Owner: -
--

CREATE FUNCTION app_security.studio_id() RETURNS uuid
    LANGUAGE sql STABLE
    AS $$
  SELECT app_security.setting('app.studio_id')::uuid
$$;


--
-- Name: user_id(); Type: FUNCTION; Schema: app_security; Owner: -
--

CREATE FUNCTION app_security.user_id() RETURNS uuid
    LANGUAGE sql STABLE
    AS $$
  SELECT app_security.setting('app.user_id')::uuid
$$;


--
-- Name: next_message_sequence(uuid); Type: FUNCTION; Schema: chat; Owner: -
--

CREATE FUNCTION chat.next_message_sequence(requested_conversation uuid) RETURNS bigint
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'pg_catalog', 'chat', 'app_security'
    AS $$
DECLARE next_sequence bigint;
BEGIN
  UPDATE chat.conversations conversation
  SET last_sequence=conversation.last_sequence+1
  WHERE conversation.id=requested_conversation
    AND conversation.active
    AND (
      (conversation.studio_id=app_security.studio_id() AND app_security.has_tenant_role('studio_admin'))
      OR EXISTS (
        SELECT 1 FROM chat.conversation_participants participant
        WHERE participant.conversation_id=conversation.id
          AND participant.user_id=app_security.user_id()
          AND participant.state='ACTIVE'
      )
    )
  RETURNING conversation.last_sequence INTO next_sequence;
  IF next_sequence IS NULL THEN
    RAISE EXCEPTION 'active conversation membership required' USING ERRCODE='42501';
  END IF;
  RETURN next_sequence;
END $$;


--
-- Name: refresh_member_count(uuid); Type: FUNCTION; Schema: chat; Owner: -
--

CREATE FUNCTION chat.refresh_member_count(requested_conversation uuid) RETURNS integer
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'pg_catalog', 'chat', 'app_security'
    AS $$
DECLARE active_members integer;
BEGIN
  SELECT count(*)::integer INTO active_members
  FROM chat.conversation_participants participant
  WHERE participant.conversation_id=requested_conversation AND participant.state='ACTIVE';

  UPDATE chat.conversations conversation
  SET member_count=active_members
  WHERE conversation.id=requested_conversation
    AND conversation.active
    AND (
      (conversation.studio_id=app_security.studio_id() AND app_security.has_tenant_role('studio_admin'))
      OR app_security.owns_user(conversation.owner_user_id)
      OR EXISTS (
        SELECT 1 FROM chat.conversation_participants participant
        WHERE participant.conversation_id=conversation.id
          AND participant.user_id=app_security.user_id()
      )
    );
  IF NOT FOUND THEN
    RAISE EXCEPTION 'conversation membership required' USING ERRCODE='42501';
  END IF;
  RETURN active_members;
END $$;


SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: global_role_assignments; Type: TABLE; Schema: account; Owner: -
--

CREATE TABLE account.global_role_assignments (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    role account.global_role NOT NULL,
    active boolean DEFAULT true NOT NULL,
    granted_by uuid,
    granted_at timestamp with time zone DEFAULT now() NOT NULL,
    revoked_by uuid,
    revoked_at timestamp with time zone,
    CONSTRAINT global_role_assignments_check CHECK (((active AND (revoked_by IS NULL) AND (revoked_at IS NULL)) OR (NOT active)))
);

ALTER TABLE ONLY account.global_role_assignments FORCE ROW LEVEL SECURITY;


--
-- Name: global_role_audit; Type: TABLE; Schema: account; Owner: -
--

CREATE TABLE account.global_role_audit (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    assignment_id uuid NOT NULL,
    actor_id uuid,
    target_user_id uuid NOT NULL,
    role account.global_role NOT NULL,
    action text NOT NULL,
    reason text NOT NULL,
    idempotency_key text NOT NULL,
    occurred_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT global_role_audit_action_check CHECK ((action = ANY (ARRAY['GRANT'::text, 'REVOKE'::text])))
);

ALTER TABLE ONLY account.global_role_audit FORCE ROW LEVEL SECURITY;


--
-- Name: inbox_events; Type: TABLE; Schema: account; Owner: -
--

CREATE TABLE account.inbox_events (
    event_id uuid NOT NULL,
    consumer text NOT NULL,
    processed_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: invitations; Type: TABLE; Schema: account; Owner: -
--

CREATE TABLE account.invitations (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    studio_id uuid NOT NULL,
    email text NOT NULL,
    role account.membership_role NOT NULL,
    invited_by uuid NOT NULL,
    status text DEFAULT 'PENDING'::text NOT NULL,
    reason text DEFAULT 'Administrator invitation'::text NOT NULL,
    idempotency_key text NOT NULL,
    expires_at timestamp with time zone DEFAULT (now() + '7 days'::interval) NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT invitations_status_check CHECK ((status = ANY (ARRAY['PENDING'::text, 'ACCEPTED'::text, 'REVOKED'::text, 'EXPIRED'::text])))
);

ALTER TABLE ONLY account.invitations FORCE ROW LEVEL SECURITY;


--
-- Name: studio_memberships; Type: TABLE; Schema: account; Owner: -
--

CREATE TABLE account.studio_memberships (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    studio_id uuid NOT NULL,
    user_id uuid NOT NULL,
    role account.membership_role NOT NULL,
    active boolean DEFAULT true NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE ONLY account.studio_memberships FORCE ROW LEVEL SECURITY;


--
-- Name: teacher_profiles; Type: TABLE; Schema: account; Owner: -
--

CREATE TABLE account.teacher_profiles (
    user_id uuid NOT NULL,
    bio text DEFAULT ''::text NOT NULL,
    portfolio_url text,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE ONLY account.teacher_profiles FORCE ROW LEVEL SECURITY;


--
-- Name: users; Type: TABLE; Schema: account; Owner: -
--

CREATE TABLE account.users (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    oidc_subject text NOT NULL,
    email text NOT NULL,
    display_name text NOT NULL,
    avatar_url text,
    timezone text DEFAULT 'America/Los_Angeles'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE ONLY account.users FORCE ROW LEVEL SECURITY;


--
-- Name: campaigns; Type: TABLE; Schema: catalog; Owner: -
--

CREATE TABLE catalog.campaigns (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    studio_id uuid,
    product_version_id uuid NOT NULL,
    name text NOT NULL,
    starts_at timestamp with time zone NOT NULL,
    ends_at timestamp with time zone NOT NULL,
    inventory integer NOT NULL,
    per_user_limit integer DEFAULT 1 NOT NULL,
    payment_ttl_minutes integer DEFAULT 10 NOT NULL,
    active boolean DEFAULT true NOT NULL,
    CONSTRAINT campaigns_check CHECK ((ends_at > starts_at)),
    CONSTRAINT campaigns_inventory_check CHECK ((inventory >= 0)),
    CONSTRAINT campaigns_payment_ttl_minutes_check CHECK (((payment_ttl_minutes >= 1) AND (payment_ttl_minutes <= 60))),
    CONSTRAINT campaigns_per_user_limit_check CHECK ((per_user_limit > 0))
);

ALTER TABLE ONLY catalog.campaigns FORCE ROW LEVEL SECURITY;


--
-- Name: credit_product_versions; Type: TABLE; Schema: catalog; Owner: -
--

CREATE TABLE catalog.credit_product_versions (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    product_id uuid NOT NULL,
    version integer NOT NULL,
    amount_cents bigint NOT NULL,
    credit_amount integer,
    validity_days integer,
    final_sale boolean DEFAULT false NOT NULL,
    valid_from timestamp with time zone DEFAULT now() NOT NULL,
    valid_until timestamp with time zone,
    CONSTRAINT credit_product_versions_amount_cents_check CHECK ((amount_cents >= 0)),
    CONSTRAINT credit_product_versions_credit_amount_check CHECK (((credit_amount IS NULL) OR (credit_amount > 0))),
    CONSTRAINT credit_product_versions_validity_days_check CHECK (((validity_days IS NULL) OR (validity_days > 0)))
);

ALTER TABLE ONLY catalog.credit_product_versions FORCE ROW LEVEL SECURITY;


--
-- Name: credit_products; Type: TABLE; Schema: catalog; Owner: -
--

CREATE TABLE catalog.credit_products (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    studio_id uuid,
    issuer_scope catalog.issuer_scope NOT NULL,
    kind catalog.product_kind NOT NULL,
    name text NOT NULL,
    active boolean DEFAULT true NOT NULL,
    CONSTRAINT credit_products_check CHECK ((((issuer_scope = 'PLATFORM'::catalog.issuer_scope) AND (studio_id IS NULL)) OR ((issuer_scope = 'STUDIO'::catalog.issuer_scope) AND (studio_id IS NOT NULL))))
);

ALTER TABLE ONLY catalog.credit_products FORCE ROW LEVEL SECURITY;


--
-- Name: inbox_events; Type: TABLE; Schema: catalog; Owner: -
--

CREATE TABLE catalog.inbox_events (
    event_id uuid NOT NULL,
    consumer text NOT NULL,
    processed_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: rooms; Type: TABLE; Schema: catalog; Owner: -
--

CREATE TABLE catalog.rooms (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    studio_id uuid NOT NULL,
    name text NOT NULL,
    capacity integer NOT NULL,
    rental_rate_cents_per_hour bigint NOT NULL,
    rentable boolean DEFAULT true NOT NULL,
    facilities text[] DEFAULT '{}'::text[] NOT NULL,
    image_url text,
    CONSTRAINT rooms_capacity_check CHECK ((capacity > 0)),
    CONSTRAINT rooms_rental_rate_cents_per_hour_check CHECK ((rental_rate_cents_per_hour >= 0))
);

ALTER TABLE ONLY catalog.rooms FORCE ROW LEVEL SECURITY;


--
-- Name: studios; Type: TABLE; Schema: catalog; Owner: -
--

CREATE TABLE catalog.studios (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    slug text NOT NULL,
    name text NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    address_line text NOT NULL,
    city text NOT NULL,
    state text DEFAULT 'CA'::text NOT NULL,
    postal_code text NOT NULL,
    timezone text DEFAULT 'America/Los_Angeles'::text NOT NULL,
    image_url text,
    active boolean DEFAULT true NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE ONLY catalog.studios FORCE ROW LEVEL SECURITY;


--
-- Name: attachments; Type: TABLE; Schema: chat; Owner: -
--

CREATE TABLE chat.attachments (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    conversation_id uuid NOT NULL,
    message_id uuid,
    studio_id uuid,
    uploader_id uuid NOT NULL,
    kind chat.message_kind NOT NULL,
    object_key text NOT NULL,
    mime_type text NOT NULL,
    size_bytes bigint NOT NULL,
    sha256 text NOT NULL,
    status chat.attachment_status DEFAULT 'PENDING_UPLOAD'::chat.attachment_status NOT NULL,
    rejection_reason text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    uploaded_at timestamp with time zone,
    scanned_at timestamp with time zone,
    CONSTRAINT attachments_kind_check CHECK ((kind = ANY (ARRAY['IMAGE'::chat.message_kind, 'VIDEO'::chat.message_kind]))),
    CONSTRAINT attachments_sha256_check CHECK ((sha256 ~ '^[a-f0-9]{64}$'::text)),
    CONSTRAINT attachments_size_bytes_check CHECK (((size_bytes > 0) AND (size_bytes <= 524288000)))
);

ALTER TABLE ONLY chat.attachments FORCE ROW LEVEL SECURITY;


--
-- Name: conversation_participants; Type: TABLE; Schema: chat; Owner: -
--

CREATE TABLE chat.conversation_participants (
    conversation_id uuid NOT NULL,
    user_id uuid NOT NULL,
    studio_id uuid,
    owner_user_id uuid,
    role chat.participant_role DEFAULT 'MEMBER'::chat.participant_role NOT NULL,
    state chat.member_state DEFAULT 'ACTIVE'::chat.member_state NOT NULL,
    joined_at timestamp with time zone DEFAULT now() NOT NULL,
    visible_from_sequence bigint DEFAULT 1 NOT NULL,
    last_read_sequence bigint DEFAULT 0 NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE ONLY chat.conversation_participants FORCE ROW LEVEL SECURITY;


--
-- Name: conversations; Type: TABLE; Schema: chat; Owner: -
--

CREATE TABLE chat.conversations (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    kind chat.conversation_kind NOT NULL,
    studio_id uuid,
    teacher_id uuid,
    student_id uuid,
    direct_low_user_id uuid,
    direct_high_user_id uuid,
    owner_user_id uuid,
    title text DEFAULT ''::text NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    avatar_url text,
    member_count integer DEFAULT 0 NOT NULL,
    last_sequence bigint DEFAULT 0 NOT NULL,
    active boolean DEFAULT true NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone,
    CONSTRAINT conversations_check CHECK ((((kind = 'DIRECT_TEACHER'::chat.conversation_kind) AND (direct_low_user_id IS NOT NULL) AND (direct_high_user_id IS NOT NULL) AND (teacher_id IS NOT NULL) AND (student_id IS NOT NULL)) OR ((kind = 'STUDIO_SUPPORT'::chat.conversation_kind) AND (studio_id IS NOT NULL) AND (student_id IS NOT NULL)) OR ((kind = 'STUDIO_GROUP'::chat.conversation_kind) AND (studio_id IS NOT NULL) AND (owner_user_id IS NOT NULL)) OR ((kind = 'TEACHER_GROUP'::chat.conversation_kind) AND (studio_id IS NOT NULL) AND (teacher_id IS NOT NULL) AND (owner_user_id = teacher_id)))),
    CONSTRAINT conversations_last_sequence_check CHECK ((last_sequence >= 0)),
    CONSTRAINT conversations_member_count_check CHECK (((member_count >= 0) AND (member_count <= 1000)))
);

ALTER TABLE ONLY chat.conversations FORCE ROW LEVEL SECURITY;


--
-- Name: group_bans; Type: TABLE; Schema: chat; Owner: -
--

CREATE TABLE chat.group_bans (
    conversation_id uuid NOT NULL,
    user_id uuid NOT NULL,
    studio_id uuid,
    owner_user_id uuid,
    banned_by uuid NOT NULL,
    reason text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    lifted_at timestamp with time zone,
    lifted_by uuid
);

ALTER TABLE ONLY chat.group_bans FORCE ROW LEVEL SECURITY;


--
-- Name: inbox_events; Type: TABLE; Schema: chat; Owner: -
--

CREATE TABLE chat.inbox_events (
    event_id text NOT NULL,
    consumer text NOT NULL,
    processed_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE ONLY chat.inbox_events FORCE ROW LEVEL SECURITY;


--
-- Name: media_deletion_jobs; Type: TABLE; Schema: chat; Owner: -
--

CREATE TABLE chat.media_deletion_jobs (
    object_key text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    attempts integer DEFAULT 0 NOT NULL,
    last_error text
);

ALTER TABLE ONLY chat.media_deletion_jobs FORCE ROW LEVEL SECURITY;


--
-- Name: message_reports; Type: TABLE; Schema: chat; Owner: -
--

CREATE TABLE chat.message_reports (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    message_id uuid NOT NULL,
    conversation_id uuid NOT NULL,
    reporter_id uuid NOT NULL,
    reason text NOT NULL,
    reported_body_snapshot text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE ONLY chat.message_reports FORCE ROW LEVEL SECURITY;


--
-- Name: messages; Type: TABLE; Schema: chat; Owner: -
--

CREATE TABLE chat.messages (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    conversation_id uuid NOT NULL,
    studio_id uuid,
    conversation_kind chat.conversation_kind NOT NULL,
    sequence bigint NOT NULL,
    sender_id uuid NOT NULL,
    sender_display_name text DEFAULT ''::text NOT NULL,
    client_message_id uuid NOT NULL,
    kind chat.message_kind NOT NULL,
    body text DEFAULT ''::text NOT NULL,
    reply_to_message_id uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    edited_at timestamp with time zone,
    withdrawn_at timestamp with time zone,
    deleted_by_moderator uuid,
    CONSTRAINT messages_body_check CHECK ((char_length(body) <= 4000)),
    CONSTRAINT messages_sequence_check CHECK ((sequence > 0))
);

ALTER TABLE ONLY chat.messages FORCE ROW LEVEL SECURITY;


--
-- Name: moderation_audit; Type: TABLE; Schema: chat; Owner: -
--

CREATE TABLE chat.moderation_audit (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    conversation_id uuid NOT NULL,
    studio_id uuid,
    actor_id uuid NOT NULL,
    target_user_id uuid,
    message_id uuid,
    action text NOT NULL,
    reason text NOT NULL,
    before_state jsonb DEFAULT '{}'::jsonb NOT NULL,
    after_state jsonb DEFAULT '{}'::jsonb NOT NULL,
    request_id text NOT NULL,
    occurred_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE ONLY chat.moderation_audit FORCE ROW LEVEL SECURITY;


--
-- Name: realtime_outbox; Type: TABLE; Schema: chat; Owner: -
--

CREATE TABLE chat.realtime_outbox (
    event_id uuid DEFAULT gen_random_uuid() NOT NULL,
    conversation_id uuid NOT NULL,
    payload jsonb NOT NULL,
    occurred_at timestamp with time zone DEFAULT now() NOT NULL,
    published_at timestamp with time zone,
    attempts integer DEFAULT 0 NOT NULL
);

ALTER TABLE ONLY chat.realtime_outbox FORCE ROW LEVEL SECURITY;


--
-- Name: user_blocks; Type: TABLE; Schema: chat; Owner: -
--

CREATE TABLE chat.user_blocks (
    blocker_user_id uuid NOT NULL,
    blocked_user_id uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT user_blocks_check CHECK ((blocker_user_id <> blocked_user_id))
);

ALTER TABLE ONLY chat.user_blocks FORCE ROW LEVEL SECURITY;


--
-- Name: grant_operations; Type: TABLE; Schema: credit; Owner: -
--

CREATE TABLE credit.grant_operations (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    grant_id uuid NOT NULL,
    successor_grant_id uuid,
    operation text NOT NULL,
    source_user_id uuid NOT NULL,
    target_user_id uuid,
    studio_id uuid,
    actor_id uuid NOT NULL,
    reason text NOT NULL,
    before_state jsonb NOT NULL,
    after_state jsonb NOT NULL,
    idempotency_key text NOT NULL,
    request_id text NOT NULL,
    occurred_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT grant_operations_operation_check CHECK ((operation = ANY (ARRAY['TRANSFER'::text, 'PAUSE'::text, 'RESUME'::text])))
);

ALTER TABLE ONLY credit.grant_operations FORCE ROW LEVEL SECURITY;


--
-- Name: grants; Type: TABLE; Schema: credit; Owner: -
--

CREATE TABLE credit.grants (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    studio_id uuid,
    source_order_line_id uuid NOT NULL,
    fulfillment_key text NOT NULL,
    product_version_id uuid NOT NULL,
    kind text NOT NULL,
    granted_credits integer,
    remaining_credits integer,
    valid_from timestamp with time zone NOT NULL,
    expires_at timestamp with time zone,
    status text DEFAULT 'ACTIVE'::text NOT NULL,
    paused_at timestamp with time zone,
    remaining_validity_seconds bigint,
    predecessor_grant_id uuid,
    transferred_at timestamp with time zone,
    final_sale boolean DEFAULT false NOT NULL,
    nonrefundable_after_transfer boolean DEFAULT false NOT NULL,
    revoked_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    refund_pending_at timestamp with time zone,
    CONSTRAINT credit_grant_status_check CHECK ((status = ANY (ARRAY['ACTIVE'::text, 'PAUSED'::text, 'TRANSFERRED'::text, 'REVOKED'::text]))),
    CONSTRAINT grants_kind_check CHECK ((kind = ANY (ARRAY['CREDITS'::text, 'UNLIMITED'::text]))),
    CONSTRAINT grants_remaining_credits_check CHECK (((remaining_credits IS NULL) OR (remaining_credits >= 0)))
);

ALTER TABLE ONLY credit.grants FORCE ROW LEVEL SECURITY;


--
-- Name: hold_allocations; Type: TABLE; Schema: credit; Owner: -
--

CREATE TABLE credit.hold_allocations (
    hold_id uuid NOT NULL,
    grant_id uuid NOT NULL,
    reserved_units integer NOT NULL,
    credit_delta integer NOT NULL,
    unlimited boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT hold_allocations_check CHECK (((unlimited AND (credit_delta = 0)) OR ((NOT unlimited) AND (credit_delta = (- reserved_units))))),
    CONSTRAINT hold_allocations_credit_delta_check CHECK ((credit_delta <= 0)),
    CONSTRAINT hold_allocations_reserved_units_check CHECK ((reserved_units > 0))
);

ALTER TABLE ONLY credit.hold_allocations FORCE ROW LEVEL SECURITY;


--
-- Name: holds; Type: TABLE; Schema: credit; Owner: -
--

CREATE TABLE credit.holds (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    studio_id uuid NOT NULL,
    booking_id uuid NOT NULL,
    amount integer NOT NULL,
    status text NOT NULL,
    class_starts_at timestamp with time zone NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT holds_amount_check CHECK ((amount > 0)),
    CONSTRAINT holds_status_check CHECK ((status = ANY (ARRAY['ACTIVE'::text, 'CAPTURED'::text, 'RELEASED'::text, 'REVERSED'::text])))
);

ALTER TABLE ONLY credit.holds FORCE ROW LEVEL SECURITY;


--
-- Name: inbox_events; Type: TABLE; Schema: credit; Owner: -
--

CREATE TABLE credit.inbox_events (
    event_id uuid NOT NULL,
    consumer text NOT NULL,
    processed_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: ledger_entries; Type: TABLE; Schema: credit; Owner: -
--

CREATE TABLE credit.ledger_entries (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    studio_id uuid,
    grant_id uuid,
    hold_id uuid,
    booking_id uuid,
    kind credit.ledger_kind NOT NULL,
    credit_delta integer NOT NULL,
    idempotency_key text NOT NULL,
    reason text NOT NULL,
    actor_id uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE ONLY credit.ledger_entries FORCE ROW LEVEL SECURITY;


--
-- Name: flashsale_inventory_ledger; Type: TABLE; Schema: orders; Owner: -
--

CREATE TABLE orders.flashsale_inventory_ledger (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    campaign_id uuid NOT NULL,
    request_id uuid NOT NULL,
    user_id uuid NOT NULL,
    quantity integer NOT NULL,
    reason text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT flashsale_inventory_ledger_quantity_check CHECK ((quantity <> 0))
);

ALTER TABLE ONLY orders.flashsale_inventory_ledger FORCE ROW LEVEL SECURITY;


--
-- Name: flashsale_requests; Type: TABLE; Schema: orders; Owner: -
--

CREATE TABLE orders.flashsale_requests (
    request_id uuid NOT NULL,
    campaign_id uuid NOT NULL,
    product_version_id uuid NOT NULL,
    user_id uuid NOT NULL,
    status text NOT NULL,
    order_id uuid,
    rejection_reason text,
    idempotency_key text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT flashsale_requests_status_check CHECK ((status = ANY (ARRAY['QUEUED'::text, 'ORDER_CREATED'::text, 'REJECTED'::text, 'EXPIRED'::text])))
);

ALTER TABLE ONLY orders.flashsale_requests FORCE ROW LEVEL SECURITY;


--
-- Name: inbox_events; Type: TABLE; Schema: orders; Owner: -
--

CREATE TABLE orders.inbox_events (
    event_id uuid NOT NULL,
    consumer text NOT NULL,
    processed_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: order_lines; Type: TABLE; Schema: orders; Owner: -
--

CREATE TABLE orders.order_lines (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    order_id uuid NOT NULL,
    line_type text NOT NULL,
    product_version_id uuid,
    room_reservation_id uuid,
    description text NOT NULL,
    quantity integer NOT NULL,
    unit_amount_cents bigint NOT NULL,
    final_sale boolean DEFAULT false NOT NULL,
    snapshot jsonb NOT NULL,
    CONSTRAINT order_lines_line_type_check CHECK ((line_type = ANY (ARRAY['CREDIT_PRODUCT'::text, 'ROOM_RESERVATION'::text]))),
    CONSTRAINT order_lines_quantity_check CHECK ((quantity > 0)),
    CONSTRAINT order_lines_unit_amount_cents_check CHECK ((unit_amount_cents >= 0))
);

ALTER TABLE ONLY orders.order_lines FORCE ROW LEVEL SECURITY;


--
-- Name: orders; Type: TABLE; Schema: orders; Owner: -
--

CREATE TABLE orders.orders (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    studio_id uuid,
    issuer_scope text NOT NULL,
    status orders.order_status DEFAULT 'DRAFT'::orders.order_status NOT NULL,
    total_amount_cents bigint NOT NULL,
    idempotency_key text NOT NULL,
    payment_expires_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    fulfillment_attempted_at timestamp with time zone,
    fulfillment_attempt_count integer DEFAULT 0 NOT NULL,
    refund_idempotency_key text,
    refund_reason text,
    refund_requested_by uuid,
    refund_approved_by uuid,
    refund_requested_at timestamp with time zone,
    CONSTRAINT orders_issuer_scope_check CHECK ((issuer_scope = ANY (ARRAY['STUDIO'::text, 'PLATFORM'::text]))),
    CONSTRAINT orders_total_amount_cents_check CHECK ((total_amount_cents >= 0))
);

ALTER TABLE ONLY orders.orders FORCE ROW LEVEL SECURITY;


--
-- Name: inbox_events; Type: TABLE; Schema: payment; Owner: -
--

CREATE TABLE payment.inbox_events (
    event_id uuid NOT NULL,
    consumer text NOT NULL,
    processed_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: outbox_events; Type: TABLE; Schema: payment; Owner: -
--

CREATE TABLE payment.outbox_events (
    event_id uuid DEFAULT gen_random_uuid() NOT NULL,
    event_type text NOT NULL,
    aggregate_type text NOT NULL,
    aggregate_id uuid NOT NULL,
    tenant_id uuid,
    payload jsonb NOT NULL,
    occurred_at timestamp with time zone DEFAULT now() NOT NULL,
    published_at timestamp with time zone,
    traceparent text,
    tracestate text,
    baggage text
);


--
-- Name: payments; Type: TABLE; Schema: payment; Owner: -
--

CREATE TABLE payment.payments (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    order_id uuid NOT NULL,
    provider text DEFAULT 'stripe'::text NOT NULL,
    provider_payment_id text,
    amount_cents bigint NOT NULL,
    status text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    provider_refund_id text,
    refunded_at timestamp with time zone,
    provider_event_created_at bigint,
    CONSTRAINT payments_amount_cents_check CHECK ((amount_cents >= 0))
);

ALTER TABLE ONLY payment.payments FORCE ROW LEVEL SECURITY;


--
-- Name: webhook_events; Type: TABLE; Schema: payment; Owner: -
--

CREATE TABLE payment.webhook_events (
    provider_event_id text NOT NULL,
    event_type text NOT NULL,
    payload jsonb NOT NULL,
    processed_at timestamp with time zone DEFAULT now(),
    processing_status text DEFAULT 'PROCESSED'::text NOT NULL,
    provider_created_at bigint,
    received_at timestamp with time zone DEFAULT now() NOT NULL,
    attempt_count integer DEFAULT 0 NOT NULL,
    last_attempt_at timestamp with time zone,
    last_error text,
    CONSTRAINT webhook_attempt_count_check CHECK ((attempt_count >= 0)),
    CONSTRAINT webhook_processing_status_check CHECK ((processing_status = ANY (ARRAY['PENDING'::text, 'PROCESSED'::text, 'IGNORED'::text])))
);

ALTER TABLE ONLY payment.webhook_events FORCE ROW LEVEL SECURITY;


--
-- Name: inbox_events; Type: TABLE; Schema: payroll; Owner: -
--

CREATE TABLE payroll.inbox_events (
    event_id uuid NOT NULL,
    consumer text NOT NULL,
    processed_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: monthly_class_facts; Type: TABLE; Schema: payroll; Owner: -
--

CREATE TABLE payroll.monthly_class_facts (
    class_session_id uuid NOT NULL,
    studio_id uuid NOT NULL,
    teacher_id uuid NOT NULL,
    local_month date NOT NULL,
    approved_duration_minutes integer NOT NULL,
    effective_redemption_count integer DEFAULT 0 NOT NULL,
    completed boolean DEFAULT false NOT NULL,
    source_version bigint DEFAULT 0 NOT NULL CHECK (source_version >= 0),
    studio_timezone text DEFAULT 'America/Los_Angeles' NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT monthly_class_facts_approved_duration_minutes_check CHECK ((approved_duration_minutes > 0)),
    CONSTRAINT monthly_class_facts_effective_redemption_count_check CHECK ((effective_redemption_count >= 0))
);

ALTER TABLE ONLY payroll.monthly_class_facts FORCE ROW LEVEL SECURITY;


--
-- Name: inbox_events; Type: TABLE; Schema: recommendation; Owner: -
--

CREATE TABLE recommendation.inbox_events (
    event_id uuid NOT NULL,
    consumer text NOT NULL,
    processed_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: studio_metrics; Type: TABLE; Schema: recommendation; Owner: -
--

CREATE TABLE recommendation.studio_metrics (
    studio_id uuid NOT NULL,
    month date NOT NULL,
    metric_name text NOT NULL,
    metric_value double precision DEFAULT 0 NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE ONLY recommendation.studio_metrics FORCE ROW LEVEL SECURITY;


--
-- Name: attendance_audit; Type: TABLE; Schema: scheduling; Owner: -
--

CREATE TABLE scheduling.attendance_audit (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    studio_id uuid NOT NULL,
    class_session_id uuid NOT NULL,
    booking_id uuid,
    student_id uuid NOT NULL,
    actor_id uuid NOT NULL,
    action text NOT NULL,
    reason text NOT NULL,
    before_status text,
    after_status text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT attendance_audit_action_check CHECK ((action = ANY (ARRAY['ATTEND'::text, 'NO_SHOW'::text, 'ADD_WALK_IN'::text, 'REMOVE_REDEMPTION'::text])))
);

ALTER TABLE ONLY scheduling.attendance_audit FORCE ROW LEVEL SECURITY;


--
-- Name: bookings; Type: TABLE; Schema: scheduling; Owner: -
--

CREATE TABLE scheduling.bookings (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    studio_id uuid NOT NULL,
    class_session_id uuid NOT NULL,
    student_id uuid NOT NULL,
    status scheduling.booking_status DEFAULT 'PENDING_CREDIT'::scheduling.booking_status NOT NULL,
    credit_hold_id uuid,
    idempotency_key text NOT NULL,
    is_walk_in boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE ONLY scheduling.bookings FORCE ROW LEVEL SECURITY;


--
-- Name: class_sessions; Type: TABLE; Schema: scheduling; Owner: -
--

CREATE TABLE scheduling.class_sessions (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    studio_id uuid NOT NULL,
    room_id uuid NOT NULL,
    teacher_id uuid NOT NULL,
    studio_timezone text DEFAULT 'America/Los_Angeles' NOT NULL,
    payroll_fact_version bigint DEFAULT 0 NOT NULL CHECK (payroll_fact_version >= 0),
    title text NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    starts_at timestamp with time zone NOT NULL,
    ends_at timestamp with time zone NOT NULL,
    occupied_range tstzrange GENERATED ALWAYS AS (tstzrange(starts_at, ends_at, '[)'::text)) STORED,
    capacity integer NOT NULL,
    minimum_students integer DEFAULT 4 NOT NULL,
    credit_cost integer NOT NULL,
    confirmed_count integer DEFAULT 0 NOT NULL,
    status scheduling.class_status DEFAULT 'PENDING_APPROVAL'::scheduling.class_status NOT NULL,
    cancellation_cutoff_at timestamp with time zone NOT NULL,
    attendance_teacher_deadline timestamp with time zone NOT NULL,
    attendance_admin_deadline timestamp with time zone NOT NULL,
    cancelled_reason text,
    video_url text,
    completed_by uuid,
    completed_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT class_sessions_capacity_check CHECK ((capacity > 0)),
    CONSTRAINT class_sessions_check CHECK (((confirmed_count >= 0) AND (confirmed_count <= capacity))),
    CONSTRAINT class_sessions_check1 CHECK ((ends_at > starts_at)),
    CONSTRAINT class_sessions_check2 CHECK ((((EXTRACT(epoch FROM (ends_at - starts_at)))::bigint % (900)::bigint) = 0)),
    CONSTRAINT class_sessions_credit_cost_check CHECK ((credit_cost > 0)),
    CONSTRAINT class_sessions_minimum_students_check CHECK ((minimum_students >= 1))
);

ALTER TABLE ONLY scheduling.class_sessions FORCE ROW LEVEL SECURITY;


--
-- Name: inbox_events; Type: TABLE; Schema: scheduling; Owner: -
--

CREATE TABLE scheduling.inbox_events (
    event_id uuid NOT NULL,
    consumer text NOT NULL,
    processed_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: outbox_events; Type: TABLE; Schema: scheduling; Owner: -
--

CREATE TABLE scheduling.outbox_events (
    event_id uuid DEFAULT gen_random_uuid() NOT NULL,
    event_type text NOT NULL,
    aggregate_type text NOT NULL,
    aggregate_id uuid NOT NULL,
    tenant_id uuid,
    payload jsonb NOT NULL,
    occurred_at timestamp with time zone DEFAULT now() NOT NULL,
    published_at timestamp with time zone,
    traceparent text,
    tracestate text,
    baggage text
);


--
-- Name: room_reservations; Type: TABLE; Schema: scheduling; Owner: -
--

CREATE TABLE scheduling.room_reservations (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    studio_id uuid NOT NULL,
    room_id uuid NOT NULL,
    student_id uuid NOT NULL,
    starts_at timestamp with time zone NOT NULL,
    ends_at timestamp with time zone NOT NULL,
    occupied_range tstzrange GENERATED ALWAYS AS (tstzrange(starts_at, ends_at, '[)'::text)) STORED,
    amount_cents bigint NOT NULL,
    status scheduling.room_reservation_status DEFAULT 'HOLD'::scheduling.room_reservation_status NOT NULL,
    hold_expires_at timestamp with time zone NOT NULL,
    order_id uuid,
    idempotency_key text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT room_reservations_amount_cents_check CHECK ((amount_cents >= 0)),
    CONSTRAINT room_reservations_check CHECK ((ends_at > starts_at))
);

ALTER TABLE ONLY scheduling.room_reservations FORCE ROW LEVEL SECURITY;


--
-- Name: global_role_assignments global_role_assignments_pkey; Type: CONSTRAINT; Schema: account; Owner: -
--

ALTER TABLE ONLY account.global_role_assignments
    ADD CONSTRAINT global_role_assignments_pkey PRIMARY KEY (id);


--
-- Name: global_role_assignments global_role_assignments_user_id_role_key; Type: CONSTRAINT; Schema: account; Owner: -
--

ALTER TABLE ONLY account.global_role_assignments
    ADD CONSTRAINT global_role_assignments_user_id_role_key UNIQUE (user_id, role);


--
-- Name: global_role_audit global_role_audit_pkey; Type: CONSTRAINT; Schema: account; Owner: -
--

ALTER TABLE ONLY account.global_role_audit
    ADD CONSTRAINT global_role_audit_pkey PRIMARY KEY (id);


--
-- Name: inbox_events inbox_events_pkey; Type: CONSTRAINT; Schema: account; Owner: -
--

ALTER TABLE ONLY account.inbox_events
    ADD CONSTRAINT inbox_events_pkey PRIMARY KEY (event_id);


--
-- Name: invitations invitations_pkey; Type: CONSTRAINT; Schema: account; Owner: -
--

ALTER TABLE ONLY account.invitations
    ADD CONSTRAINT invitations_pkey PRIMARY KEY (id);


--
-- Name: invitations invitations_studio_id_idempotency_key_key; Type: CONSTRAINT; Schema: account; Owner: -
--

ALTER TABLE ONLY account.invitations
    ADD CONSTRAINT invitations_studio_id_idempotency_key_key UNIQUE (studio_id, idempotency_key);


--
-- Name: studio_memberships studio_memberships_pkey; Type: CONSTRAINT; Schema: account; Owner: -
--

ALTER TABLE ONLY account.studio_memberships
    ADD CONSTRAINT studio_memberships_pkey PRIMARY KEY (id);


--
-- Name: studio_memberships studio_memberships_studio_id_user_id_role_key; Type: CONSTRAINT; Schema: account; Owner: -
--

ALTER TABLE ONLY account.studio_memberships
    ADD CONSTRAINT studio_memberships_studio_id_user_id_role_key UNIQUE (studio_id, user_id, role);


--
-- Name: teacher_profiles teacher_profiles_pkey; Type: CONSTRAINT; Schema: account; Owner: -
--

ALTER TABLE ONLY account.teacher_profiles
    ADD CONSTRAINT teacher_profiles_pkey PRIMARY KEY (user_id);


--
-- Name: users users_email_key; Type: CONSTRAINT; Schema: account; Owner: -
--

ALTER TABLE ONLY account.users
    ADD CONSTRAINT users_email_key UNIQUE (email);


--
-- Name: users users_oidc_subject_key; Type: CONSTRAINT; Schema: account; Owner: -
--

ALTER TABLE ONLY account.users
    ADD CONSTRAINT users_oidc_subject_key UNIQUE (oidc_subject);


--
-- Name: users users_pkey; Type: CONSTRAINT; Schema: account; Owner: -
--

ALTER TABLE ONLY account.users
    ADD CONSTRAINT users_pkey PRIMARY KEY (id);


--
-- Name: campaigns campaigns_pkey; Type: CONSTRAINT; Schema: catalog; Owner: -
--

ALTER TABLE ONLY catalog.campaigns
    ADD CONSTRAINT campaigns_pkey PRIMARY KEY (id);


--
-- Name: credit_product_versions credit_product_versions_pkey; Type: CONSTRAINT; Schema: catalog; Owner: -
--

ALTER TABLE ONLY catalog.credit_product_versions
    ADD CONSTRAINT credit_product_versions_pkey PRIMARY KEY (id);


--
-- Name: credit_product_versions credit_product_versions_product_id_version_key; Type: CONSTRAINT; Schema: catalog; Owner: -
--

ALTER TABLE ONLY catalog.credit_product_versions
    ADD CONSTRAINT credit_product_versions_product_id_version_key UNIQUE (product_id, version);


--
-- Name: credit_products credit_products_pkey; Type: CONSTRAINT; Schema: catalog; Owner: -
--

ALTER TABLE ONLY catalog.credit_products
    ADD CONSTRAINT credit_products_pkey PRIMARY KEY (id);


--
-- Name: inbox_events inbox_events_pkey; Type: CONSTRAINT; Schema: catalog; Owner: -
--

ALTER TABLE ONLY catalog.inbox_events
    ADD CONSTRAINT inbox_events_pkey PRIMARY KEY (event_id);


--
-- Name: rooms rooms_pkey; Type: CONSTRAINT; Schema: catalog; Owner: -
--

ALTER TABLE ONLY catalog.rooms
    ADD CONSTRAINT rooms_pkey PRIMARY KEY (id);


--
-- Name: rooms rooms_studio_id_name_key; Type: CONSTRAINT; Schema: catalog; Owner: -
--

ALTER TABLE ONLY catalog.rooms
    ADD CONSTRAINT rooms_studio_id_name_key UNIQUE (studio_id, name);


--
-- Name: studios studios_pkey; Type: CONSTRAINT; Schema: catalog; Owner: -
--

ALTER TABLE ONLY catalog.studios
    ADD CONSTRAINT studios_pkey PRIMARY KEY (id);


--
-- Name: studios studios_slug_key; Type: CONSTRAINT; Schema: catalog; Owner: -
--

ALTER TABLE ONLY catalog.studios
    ADD CONSTRAINT studios_slug_key UNIQUE (slug);


--
-- Name: attachments attachments_object_key_key; Type: CONSTRAINT; Schema: chat; Owner: -
--

ALTER TABLE ONLY chat.attachments
    ADD CONSTRAINT attachments_object_key_key UNIQUE (object_key);


--
-- Name: attachments attachments_pkey; Type: CONSTRAINT; Schema: chat; Owner: -
--

ALTER TABLE ONLY chat.attachments
    ADD CONSTRAINT attachments_pkey PRIMARY KEY (id);


--
-- Name: conversation_participants conversation_participants_pkey; Type: CONSTRAINT; Schema: chat; Owner: -
--

ALTER TABLE ONLY chat.conversation_participants
    ADD CONSTRAINT conversation_participants_pkey PRIMARY KEY (conversation_id, user_id);


--
-- Name: conversations conversations_pkey; Type: CONSTRAINT; Schema: chat; Owner: -
--

ALTER TABLE ONLY chat.conversations
    ADD CONSTRAINT conversations_pkey PRIMARY KEY (id);


--
-- Name: group_bans group_bans_pkey; Type: CONSTRAINT; Schema: chat; Owner: -
--

ALTER TABLE ONLY chat.group_bans
    ADD CONSTRAINT group_bans_pkey PRIMARY KEY (conversation_id, user_id);


--
-- Name: inbox_events inbox_events_pkey; Type: CONSTRAINT; Schema: chat; Owner: -
--

ALTER TABLE ONLY chat.inbox_events
    ADD CONSTRAINT inbox_events_pkey PRIMARY KEY (event_id, consumer);


--
-- Name: media_deletion_jobs media_deletion_jobs_pkey; Type: CONSTRAINT; Schema: chat; Owner: -
--

ALTER TABLE ONLY chat.media_deletion_jobs
    ADD CONSTRAINT media_deletion_jobs_pkey PRIMARY KEY (object_key);


--
-- Name: message_reports message_reports_message_id_reporter_id_key; Type: CONSTRAINT; Schema: chat; Owner: -
--

ALTER TABLE ONLY chat.message_reports
    ADD CONSTRAINT message_reports_message_id_reporter_id_key UNIQUE (message_id, reporter_id);


--
-- Name: message_reports message_reports_pkey; Type: CONSTRAINT; Schema: chat; Owner: -
--

ALTER TABLE ONLY chat.message_reports
    ADD CONSTRAINT message_reports_pkey PRIMARY KEY (id);


--
-- Name: messages messages_conversation_id_sequence_key; Type: CONSTRAINT; Schema: chat; Owner: -
--

ALTER TABLE ONLY chat.messages
    ADD CONSTRAINT messages_conversation_id_sequence_key UNIQUE (conversation_id, sequence);


--
-- Name: messages messages_pkey; Type: CONSTRAINT; Schema: chat; Owner: -
--

ALTER TABLE ONLY chat.messages
    ADD CONSTRAINT messages_pkey PRIMARY KEY (id);


--
-- Name: messages messages_sender_id_client_message_id_key; Type: CONSTRAINT; Schema: chat; Owner: -
--

ALTER TABLE ONLY chat.messages
    ADD CONSTRAINT messages_sender_id_client_message_id_key UNIQUE (sender_id, client_message_id);


--
-- Name: moderation_audit moderation_audit_pkey; Type: CONSTRAINT; Schema: chat; Owner: -
--

ALTER TABLE ONLY chat.moderation_audit
    ADD CONSTRAINT moderation_audit_pkey PRIMARY KEY (id);


--
-- Name: realtime_outbox realtime_outbox_pkey; Type: CONSTRAINT; Schema: chat; Owner: -
--

ALTER TABLE ONLY chat.realtime_outbox
    ADD CONSTRAINT realtime_outbox_pkey PRIMARY KEY (event_id);


--
-- Name: user_blocks user_blocks_pkey; Type: CONSTRAINT; Schema: chat; Owner: -
--

ALTER TABLE ONLY chat.user_blocks
    ADD CONSTRAINT user_blocks_pkey PRIMARY KEY (blocker_user_id, blocked_user_id);


--
-- Name: grant_operations grant_operations_idempotency_key_key; Type: CONSTRAINT; Schema: credit; Owner: -
--

ALTER TABLE ONLY credit.grant_operations
    ADD CONSTRAINT grant_operations_idempotency_key_key UNIQUE (idempotency_key);


--
-- Name: grant_operations grant_operations_pkey; Type: CONSTRAINT; Schema: credit; Owner: -
--

ALTER TABLE ONLY credit.grant_operations
    ADD CONSTRAINT grant_operations_pkey PRIMARY KEY (id);


--
-- Name: grants grants_fulfillment_key_key; Type: CONSTRAINT; Schema: credit; Owner: -
--

ALTER TABLE ONLY credit.grants
    ADD CONSTRAINT grants_fulfillment_key_key UNIQUE (fulfillment_key);


--
-- Name: grants grants_pkey; Type: CONSTRAINT; Schema: credit; Owner: -
--

ALTER TABLE ONLY credit.grants
    ADD CONSTRAINT grants_pkey PRIMARY KEY (id);


--
-- Name: hold_allocations hold_allocations_pkey; Type: CONSTRAINT; Schema: credit; Owner: -
--

ALTER TABLE ONLY credit.hold_allocations
    ADD CONSTRAINT hold_allocations_pkey PRIMARY KEY (hold_id, grant_id);


--
-- Name: holds holds_booking_id_key; Type: CONSTRAINT; Schema: credit; Owner: -
--

ALTER TABLE ONLY credit.holds
    ADD CONSTRAINT holds_booking_id_key UNIQUE (booking_id);


--
-- Name: holds holds_pkey; Type: CONSTRAINT; Schema: credit; Owner: -
--

ALTER TABLE ONLY credit.holds
    ADD CONSTRAINT holds_pkey PRIMARY KEY (id);


--
-- Name: inbox_events inbox_events_pkey; Type: CONSTRAINT; Schema: credit; Owner: -
--

ALTER TABLE ONLY credit.inbox_events
    ADD CONSTRAINT inbox_events_pkey PRIMARY KEY (event_id);


--
-- Name: ledger_entries ledger_entries_idempotency_key_key; Type: CONSTRAINT; Schema: credit; Owner: -
--

ALTER TABLE ONLY credit.ledger_entries
    ADD CONSTRAINT ledger_entries_idempotency_key_key UNIQUE (idempotency_key);


--
-- Name: ledger_entries ledger_entries_pkey; Type: CONSTRAINT; Schema: credit; Owner: -
--

ALTER TABLE ONLY credit.ledger_entries
    ADD CONSTRAINT ledger_entries_pkey PRIMARY KEY (id);


--
-- Name: flashsale_inventory_ledger flashsale_inventory_ledger_campaign_id_request_id_reason_key; Type: CONSTRAINT; Schema: orders; Owner: -
--

ALTER TABLE ONLY orders.flashsale_inventory_ledger
    ADD CONSTRAINT flashsale_inventory_ledger_campaign_id_request_id_reason_key UNIQUE (campaign_id, request_id, reason);


--
-- Name: flashsale_inventory_ledger flashsale_inventory_ledger_pkey; Type: CONSTRAINT; Schema: orders; Owner: -
--

ALTER TABLE ONLY orders.flashsale_inventory_ledger
    ADD CONSTRAINT flashsale_inventory_ledger_pkey PRIMARY KEY (id);


--
-- Name: flashsale_requests flashsale_requests_campaign_id_user_id_idempotency_key_key; Type: CONSTRAINT; Schema: orders; Owner: -
--

ALTER TABLE ONLY orders.flashsale_requests
    ADD CONSTRAINT flashsale_requests_campaign_id_user_id_idempotency_key_key UNIQUE (campaign_id, user_id, idempotency_key);


--
-- Name: flashsale_requests flashsale_requests_pkey; Type: CONSTRAINT; Schema: orders; Owner: -
--

ALTER TABLE ONLY orders.flashsale_requests
    ADD CONSTRAINT flashsale_requests_pkey PRIMARY KEY (request_id);


--
-- Name: inbox_events inbox_events_pkey; Type: CONSTRAINT; Schema: orders; Owner: -
--

ALTER TABLE ONLY orders.inbox_events
    ADD CONSTRAINT inbox_events_pkey PRIMARY KEY (event_id);


--
-- Name: order_lines order_lines_pkey; Type: CONSTRAINT; Schema: orders; Owner: -
--

ALTER TABLE ONLY orders.order_lines
    ADD CONSTRAINT order_lines_pkey PRIMARY KEY (id);


--
-- Name: orders orders_pkey; Type: CONSTRAINT; Schema: orders; Owner: -
--

ALTER TABLE ONLY orders.orders
    ADD CONSTRAINT orders_pkey PRIMARY KEY (id);


--
-- Name: orders orders_user_id_idempotency_key_key; Type: CONSTRAINT; Schema: orders; Owner: -
--

ALTER TABLE ONLY orders.orders
    ADD CONSTRAINT orders_user_id_idempotency_key_key UNIQUE (user_id, idempotency_key);


--
-- Name: inbox_events inbox_events_pkey; Type: CONSTRAINT; Schema: payment; Owner: -
--

ALTER TABLE ONLY payment.inbox_events
    ADD CONSTRAINT inbox_events_pkey PRIMARY KEY (event_id);


--
-- Name: outbox_events outbox_events_pkey; Type: CONSTRAINT; Schema: payment; Owner: -
--

ALTER TABLE ONLY payment.outbox_events
    ADD CONSTRAINT outbox_events_pkey PRIMARY KEY (event_id);


--
-- Name: payments payments_order_id_key; Type: CONSTRAINT; Schema: payment; Owner: -
--

ALTER TABLE ONLY payment.payments
    ADD CONSTRAINT payments_order_id_key UNIQUE (order_id);


--
-- Name: payments payments_pkey; Type: CONSTRAINT; Schema: payment; Owner: -
--

ALTER TABLE ONLY payment.payments
    ADD CONSTRAINT payments_pkey PRIMARY KEY (id);


--
-- Name: payments payments_provider_payment_id_key; Type: CONSTRAINT; Schema: payment; Owner: -
--

ALTER TABLE ONLY payment.payments
    ADD CONSTRAINT payments_provider_payment_id_key UNIQUE (provider_payment_id);


--
-- Name: webhook_events webhook_events_pkey; Type: CONSTRAINT; Schema: payment; Owner: -
--

ALTER TABLE ONLY payment.webhook_events
    ADD CONSTRAINT webhook_events_pkey PRIMARY KEY (provider_event_id);


--
-- Name: inbox_events inbox_events_pkey; Type: CONSTRAINT; Schema: payroll; Owner: -
--

ALTER TABLE ONLY payroll.inbox_events
    ADD CONSTRAINT inbox_events_pkey PRIMARY KEY (event_id);


--
-- Name: monthly_class_facts monthly_class_facts_pkey; Type: CONSTRAINT; Schema: payroll; Owner: -
--

ALTER TABLE ONLY payroll.monthly_class_facts
    ADD CONSTRAINT monthly_class_facts_pkey PRIMARY KEY (class_session_id);


--
-- Name: inbox_events inbox_events_pkey; Type: CONSTRAINT; Schema: recommendation; Owner: -
--

ALTER TABLE ONLY recommendation.inbox_events
    ADD CONSTRAINT inbox_events_pkey PRIMARY KEY (event_id);


--
-- Name: studio_metrics studio_metrics_pkey; Type: CONSTRAINT; Schema: recommendation; Owner: -
--

ALTER TABLE ONLY recommendation.studio_metrics
    ADD CONSTRAINT studio_metrics_pkey PRIMARY KEY (studio_id, month, metric_name);


--
-- Name: attendance_audit attendance_audit_pkey; Type: CONSTRAINT; Schema: scheduling; Owner: -
--

ALTER TABLE ONLY scheduling.attendance_audit
    ADD CONSTRAINT attendance_audit_pkey PRIMARY KEY (id);


--
-- Name: bookings bookings_pkey; Type: CONSTRAINT; Schema: scheduling; Owner: -
--

ALTER TABLE ONLY scheduling.bookings
    ADD CONSTRAINT bookings_pkey PRIMARY KEY (id);


--
-- Name: bookings bookings_studio_id_idempotency_key_key; Type: CONSTRAINT; Schema: scheduling; Owner: -
--

ALTER TABLE ONLY scheduling.bookings
    ADD CONSTRAINT bookings_studio_id_idempotency_key_key UNIQUE (studio_id, idempotency_key);


--
-- Name: class_sessions class_sessions_pkey; Type: CONSTRAINT; Schema: scheduling; Owner: -
--

ALTER TABLE ONLY scheduling.class_sessions
    ADD CONSTRAINT class_sessions_pkey PRIMARY KEY (id);


--
-- Name: class_sessions class_sessions_studio_id_room_id_occupied_range_excl; Type: CONSTRAINT; Schema: scheduling; Owner: -
--

ALTER TABLE ONLY scheduling.class_sessions
    ADD CONSTRAINT class_sessions_studio_id_room_id_occupied_range_excl EXCLUDE USING gist (studio_id WITH =, room_id WITH =, occupied_range WITH &&) WHERE ((status = ANY (ARRAY['APPROVED'::scheduling.class_status, 'OPEN'::scheduling.class_status, 'MINIMUM_CONFIRMED'::scheduling.class_status, 'AWAITING_ADMIN_CONFIRMATION'::scheduling.class_status, 'COMPLETED'::scheduling.class_status])));


--
-- Name: class_sessions class_sessions_teacher_id_occupied_range_excl; Type: CONSTRAINT; Schema: scheduling; Owner: -
--

ALTER TABLE ONLY scheduling.class_sessions
    ADD CONSTRAINT class_sessions_teacher_id_occupied_range_excl EXCLUDE USING gist (teacher_id WITH =, occupied_range WITH &&) WHERE ((status = ANY (ARRAY['APPROVED'::scheduling.class_status, 'OPEN'::scheduling.class_status, 'MINIMUM_CONFIRMED'::scheduling.class_status, 'AWAITING_ADMIN_CONFIRMATION'::scheduling.class_status, 'COMPLETED'::scheduling.class_status])));


--
-- Name: inbox_events inbox_events_pkey; Type: CONSTRAINT; Schema: scheduling; Owner: -
--

ALTER TABLE ONLY scheduling.inbox_events
    ADD CONSTRAINT inbox_events_pkey PRIMARY KEY (event_id);


--
-- Name: outbox_events outbox_events_pkey; Type: CONSTRAINT; Schema: scheduling; Owner: -
--

ALTER TABLE ONLY scheduling.outbox_events
    ADD CONSTRAINT outbox_events_pkey PRIMARY KEY (event_id);


--
-- Name: room_reservations room_reservations_pkey; Type: CONSTRAINT; Schema: scheduling; Owner: -
--

ALTER TABLE ONLY scheduling.room_reservations
    ADD CONSTRAINT room_reservations_pkey PRIMARY KEY (id);


--
-- Name: room_reservations room_reservations_studio_id_idempotency_key_key; Type: CONSTRAINT; Schema: scheduling; Owner: -
--

ALTER TABLE ONLY scheduling.room_reservations
    ADD CONSTRAINT room_reservations_studio_id_idempotency_key_key UNIQUE (studio_id, idempotency_key);


--
-- Name: room_reservations room_reservations_studio_id_room_id_occupied_range_excl; Type: CONSTRAINT; Schema: scheduling; Owner: -
--

ALTER TABLE ONLY scheduling.room_reservations
    ADD CONSTRAINT room_reservations_studio_id_room_id_occupied_range_excl EXCLUDE USING gist (studio_id WITH =, room_id WITH =, occupied_range WITH &&) WHERE ((status = ANY (ARRAY['HOLD'::scheduling.room_reservation_status, 'PENDING_PAYMENT'::scheduling.room_reservation_status, 'CONFIRMED'::scheduling.room_reservation_status, 'REFUND_PENDING'::scheduling.room_reservation_status])));


--
-- Name: account_one_pending_invitation; Type: INDEX; Schema: account; Owner: -
--

CREATE UNIQUE INDEX account_one_pending_invitation ON account.invitations USING btree (studio_id, lower(email), role) WHERE (status = 'PENDING'::text);


--
-- Name: global_role_audit_actor_idempotency; Type: INDEX; Schema: account; Owner: -
--

CREATE UNIQUE INDEX global_role_audit_actor_idempotency ON account.global_role_audit USING btree (actor_id, idempotency_key) WHERE (actor_id IS NOT NULL);


--
-- Name: studio_memberships_tenant_idx; Type: INDEX; Schema: account; Owner: -
--

CREATE INDEX studio_memberships_tenant_idx ON account.studio_memberships USING btree (studio_id, role, active);


--
-- Name: chat_attachment_scan_idx; Type: INDEX; Schema: chat; Owner: -
--

CREATE INDEX chat_attachment_scan_idx ON chat.attachments USING btree (status, created_at);


--
-- Name: chat_direct_pair_unique; Type: INDEX; Schema: chat; Owner: -
--

CREATE UNIQUE INDEX chat_direct_pair_unique ON chat.conversations USING btree (direct_low_user_id, direct_high_user_id) WHERE ((kind = 'DIRECT_TEACHER'::chat.conversation_kind) AND active);


--
-- Name: chat_group_discovery_idx; Type: INDEX; Schema: chat; Owner: -
--

CREATE INDEX chat_group_discovery_idx ON chat.conversations USING btree (kind, studio_id, teacher_id, created_at) WHERE (active AND (kind = ANY (ARRAY['STUDIO_GROUP'::chat.conversation_kind, 'TEACHER_GROUP'::chat.conversation_kind])));


--
-- Name: chat_message_history_idx; Type: INDEX; Schema: chat; Owner: -
--

CREATE INDEX chat_message_history_idx ON chat.messages USING btree (conversation_id, sequence DESC);


--
-- Name: chat_participant_inbox_idx; Type: INDEX; Schema: chat; Owner: -
--

CREATE INDEX chat_participant_inbox_idx ON chat.conversation_participants USING btree (user_id, state, updated_at DESC);


--
-- Name: chat_studio_support_unique; Type: INDEX; Schema: chat; Owner: -
--

CREATE UNIQUE INDEX chat_studio_support_unique ON chat.conversations USING btree (studio_id, student_id) WHERE ((kind = 'STUDIO_SUPPORT'::chat.conversation_kind) AND active);


--
-- Name: credit_grants_order_line_idx; Type: INDEX; Schema: credit; Owner: -
--

CREATE INDEX credit_grants_order_line_idx ON credit.grants USING btree (source_order_line_id);


--
-- Name: credit_ledger_user_idx; Type: INDEX; Schema: credit; Owner: -
--

CREATE INDEX credit_ledger_user_idx ON credit.ledger_entries USING btree (user_id, created_at DESC);


--
-- Name: one_successful_transfer_per_grant; Type: INDEX; Schema: credit; Owner: -
--

CREATE UNIQUE INDEX one_successful_transfer_per_grant ON credit.grant_operations USING btree (grant_id) WHERE (operation = 'TRANSFER'::text);


--
-- Name: orders_refund_idempotency_idx; Type: INDEX; Schema: orders; Owner: -
--

CREATE UNIQUE INDEX orders_refund_idempotency_idx ON orders.orders USING btree (user_id, refund_idempotency_key) WHERE (refund_idempotency_key IS NOT NULL);


--
-- Name: webhook_pending_provider_idx; Type: INDEX; Schema: payment; Owner: -
--

CREATE INDEX webhook_pending_provider_idx ON payment.webhook_events USING btree (((payload #>> '{data,object,id}'::text[])), provider_created_at) WHERE (processing_status = 'PENDING'::text);


--
-- Name: payroll_month_idx; Type: INDEX; Schema: payroll; Owner: -
--

CREATE INDEX payroll_month_idx ON payroll.monthly_class_facts USING btree (studio_id, local_month, teacher_id);


--
-- Name: bookings_active_student_session_key; Type: INDEX; Schema: scheduling; Owner: -
--

-- A student holds at most one live booking per class. Cancelled and reversed rows stay as
-- audit history and must not block booking the same class again with a fresh credit hold.
CREATE UNIQUE INDEX bookings_active_student_session_key ON scheduling.bookings USING btree (studio_id, class_session_id, student_id) WHERE (status <> ALL (ARRAY['CANCELLED'::scheduling.booking_status, 'REVERSED'::scheduling.booking_status]));


--
-- Name: bookings_session_status_idx; Type: INDEX; Schema: scheduling; Owner: -
--

CREATE INDEX bookings_session_status_idx ON scheduling.bookings USING btree (studio_id, class_session_id, status);


--
-- Name: class_sessions_tenant_time_idx; Type: INDEX; Schema: scheduling; Owner: -
--

CREATE INDEX class_sessions_tenant_time_idx ON scheduling.class_sessions USING btree (studio_id, starts_at, status);


--
-- Name: global_role_audit global_role_audit_immutable; Type: TRIGGER; Schema: account; Owner: -
--

CREATE TRIGGER global_role_audit_immutable BEFORE DELETE OR UPDATE ON account.global_role_audit FOR EACH ROW EXECUTE FUNCTION account.reject_global_role_audit_mutation();


--
-- Name: global_role_assignments global_role_assignments_granted_by_fkey; Type: FK CONSTRAINT; Schema: account; Owner: -
--

ALTER TABLE ONLY account.global_role_assignments
    ADD CONSTRAINT global_role_assignments_granted_by_fkey FOREIGN KEY (granted_by) REFERENCES account.users(id);


--
-- Name: global_role_assignments global_role_assignments_revoked_by_fkey; Type: FK CONSTRAINT; Schema: account; Owner: -
--

ALTER TABLE ONLY account.global_role_assignments
    ADD CONSTRAINT global_role_assignments_revoked_by_fkey FOREIGN KEY (revoked_by) REFERENCES account.users(id);


--
-- Name: global_role_assignments global_role_assignments_user_id_fkey; Type: FK CONSTRAINT; Schema: account; Owner: -
--

ALTER TABLE ONLY account.global_role_assignments
    ADD CONSTRAINT global_role_assignments_user_id_fkey FOREIGN KEY (user_id) REFERENCES account.users(id);


--
-- Name: global_role_audit global_role_audit_actor_id_fkey; Type: FK CONSTRAINT; Schema: account; Owner: -
--

ALTER TABLE ONLY account.global_role_audit
    ADD CONSTRAINT global_role_audit_actor_id_fkey FOREIGN KEY (actor_id) REFERENCES account.users(id);


--
-- Name: global_role_audit global_role_audit_assignment_id_fkey; Type: FK CONSTRAINT; Schema: account; Owner: -
--

ALTER TABLE ONLY account.global_role_audit
    ADD CONSTRAINT global_role_audit_assignment_id_fkey FOREIGN KEY (assignment_id) REFERENCES account.global_role_assignments(id);


--
-- Name: global_role_audit global_role_audit_target_user_id_fkey; Type: FK CONSTRAINT; Schema: account; Owner: -
--

ALTER TABLE ONLY account.global_role_audit
    ADD CONSTRAINT global_role_audit_target_user_id_fkey FOREIGN KEY (target_user_id) REFERENCES account.users(id);


--
-- Name: studio_memberships studio_memberships_user_id_fkey; Type: FK CONSTRAINT; Schema: account; Owner: -
--

ALTER TABLE ONLY account.studio_memberships
    ADD CONSTRAINT studio_memberships_user_id_fkey FOREIGN KEY (user_id) REFERENCES account.users(id);


--
-- Name: teacher_profiles teacher_profiles_user_id_fkey; Type: FK CONSTRAINT; Schema: account; Owner: -
--

ALTER TABLE ONLY account.teacher_profiles
    ADD CONSTRAINT teacher_profiles_user_id_fkey FOREIGN KEY (user_id) REFERENCES account.users(id);


--
-- Name: campaigns campaigns_product_version_id_fkey; Type: FK CONSTRAINT; Schema: catalog; Owner: -
--

ALTER TABLE ONLY catalog.campaigns
    ADD CONSTRAINT campaigns_product_version_id_fkey FOREIGN KEY (product_version_id) REFERENCES catalog.credit_product_versions(id);


--
-- Name: credit_product_versions credit_product_versions_product_id_fkey; Type: FK CONSTRAINT; Schema: catalog; Owner: -
--

ALTER TABLE ONLY catalog.credit_product_versions
    ADD CONSTRAINT credit_product_versions_product_id_fkey FOREIGN KEY (product_id) REFERENCES catalog.credit_products(id);


--
-- Name: credit_products credit_products_studio_id_fkey; Type: FK CONSTRAINT; Schema: catalog; Owner: -
--

ALTER TABLE ONLY catalog.credit_products
    ADD CONSTRAINT credit_products_studio_id_fkey FOREIGN KEY (studio_id) REFERENCES catalog.studios(id);


--
-- Name: rooms rooms_studio_id_fkey; Type: FK CONSTRAINT; Schema: catalog; Owner: -
--

ALTER TABLE ONLY catalog.rooms
    ADD CONSTRAINT rooms_studio_id_fkey FOREIGN KEY (studio_id) REFERENCES catalog.studios(id);


--
-- Name: attachments attachments_conversation_id_fkey; Type: FK CONSTRAINT; Schema: chat; Owner: -
--

ALTER TABLE ONLY chat.attachments
    ADD CONSTRAINT attachments_conversation_id_fkey FOREIGN KEY (conversation_id) REFERENCES chat.conversations(id) ON DELETE CASCADE;


--
-- Name: attachments attachments_message_id_fkey; Type: FK CONSTRAINT; Schema: chat; Owner: -
--

ALTER TABLE ONLY chat.attachments
    ADD CONSTRAINT attachments_message_id_fkey FOREIGN KEY (message_id) REFERENCES chat.messages(id) ON DELETE CASCADE;


--
-- Name: conversation_participants conversation_participants_conversation_id_fkey; Type: FK CONSTRAINT; Schema: chat; Owner: -
--

ALTER TABLE ONLY chat.conversation_participants
    ADD CONSTRAINT conversation_participants_conversation_id_fkey FOREIGN KEY (conversation_id) REFERENCES chat.conversations(id) ON DELETE CASCADE;


--
-- Name: group_bans group_bans_conversation_id_fkey; Type: FK CONSTRAINT; Schema: chat; Owner: -
--

ALTER TABLE ONLY chat.group_bans
    ADD CONSTRAINT group_bans_conversation_id_fkey FOREIGN KEY (conversation_id) REFERENCES chat.conversations(id) ON DELETE CASCADE;


--
-- Name: message_reports message_reports_message_id_fkey; Type: FK CONSTRAINT; Schema: chat; Owner: -
--

ALTER TABLE ONLY chat.message_reports
    ADD CONSTRAINT message_reports_message_id_fkey FOREIGN KEY (message_id) REFERENCES chat.messages(id) ON DELETE CASCADE;


--
-- Name: messages messages_conversation_id_fkey; Type: FK CONSTRAINT; Schema: chat; Owner: -
--

ALTER TABLE ONLY chat.messages
    ADD CONSTRAINT messages_conversation_id_fkey FOREIGN KEY (conversation_id) REFERENCES chat.conversations(id) ON DELETE CASCADE;


--
-- Name: messages messages_reply_to_message_id_fkey; Type: FK CONSTRAINT; Schema: chat; Owner: -
--

ALTER TABLE ONLY chat.messages
    ADD CONSTRAINT messages_reply_to_message_id_fkey FOREIGN KEY (reply_to_message_id) REFERENCES chat.messages(id);


--
-- Name: realtime_outbox realtime_outbox_conversation_id_fkey; Type: FK CONSTRAINT; Schema: chat; Owner: -
--

ALTER TABLE ONLY chat.realtime_outbox
    ADD CONSTRAINT realtime_outbox_conversation_id_fkey FOREIGN KEY (conversation_id) REFERENCES chat.conversations(id) ON DELETE CASCADE;


--
-- Name: grant_operations grant_operations_grant_id_fkey; Type: FK CONSTRAINT; Schema: credit; Owner: -
--

ALTER TABLE ONLY credit.grant_operations
    ADD CONSTRAINT grant_operations_grant_id_fkey FOREIGN KEY (grant_id) REFERENCES credit.grants(id);


--
-- Name: grant_operations grant_operations_successor_grant_id_fkey; Type: FK CONSTRAINT; Schema: credit; Owner: -
--

ALTER TABLE ONLY credit.grant_operations
    ADD CONSTRAINT grant_operations_successor_grant_id_fkey FOREIGN KEY (successor_grant_id) REFERENCES credit.grants(id);


--
-- Name: grants grants_predecessor_grant_id_fkey; Type: FK CONSTRAINT; Schema: credit; Owner: -
--

ALTER TABLE ONLY credit.grants
    ADD CONSTRAINT grants_predecessor_grant_id_fkey FOREIGN KEY (predecessor_grant_id) REFERENCES credit.grants(id);


--
-- Name: hold_allocations hold_allocations_grant_id_fkey; Type: FK CONSTRAINT; Schema: credit; Owner: -
--

ALTER TABLE ONLY credit.hold_allocations
    ADD CONSTRAINT hold_allocations_grant_id_fkey FOREIGN KEY (grant_id) REFERENCES credit.grants(id);


--
-- Name: hold_allocations hold_allocations_hold_id_fkey; Type: FK CONSTRAINT; Schema: credit; Owner: -
--

ALTER TABLE ONLY credit.hold_allocations
    ADD CONSTRAINT hold_allocations_hold_id_fkey FOREIGN KEY (hold_id) REFERENCES credit.holds(id);


--
-- Name: ledger_entries ledger_entries_grant_id_fkey; Type: FK CONSTRAINT; Schema: credit; Owner: -
--

ALTER TABLE ONLY credit.ledger_entries
    ADD CONSTRAINT ledger_entries_grant_id_fkey FOREIGN KEY (grant_id) REFERENCES credit.grants(id);


--
-- Name: ledger_entries ledger_entries_hold_id_fkey; Type: FK CONSTRAINT; Schema: credit; Owner: -
--

ALTER TABLE ONLY credit.ledger_entries
    ADD CONSTRAINT ledger_entries_hold_id_fkey FOREIGN KEY (hold_id) REFERENCES credit.holds(id);


--
-- Name: flashsale_requests flashsale_requests_order_id_fkey; Type: FK CONSTRAINT; Schema: orders; Owner: -
--

ALTER TABLE ONLY orders.flashsale_requests
    ADD CONSTRAINT flashsale_requests_order_id_fkey FOREIGN KEY (order_id) REFERENCES orders.orders(id);


--
-- Name: order_lines order_lines_order_id_fkey; Type: FK CONSTRAINT; Schema: orders; Owner: -
--

ALTER TABLE ONLY orders.order_lines
    ADD CONSTRAINT order_lines_order_id_fkey FOREIGN KEY (order_id) REFERENCES orders.orders(id);


--
-- Name: attendance_audit attendance_audit_booking_id_fkey; Type: FK CONSTRAINT; Schema: scheduling; Owner: -
--

ALTER TABLE ONLY scheduling.attendance_audit
    ADD CONSTRAINT attendance_audit_booking_id_fkey FOREIGN KEY (booking_id) REFERENCES scheduling.bookings(id);


--
-- Name: attendance_audit attendance_audit_class_session_id_fkey; Type: FK CONSTRAINT; Schema: scheduling; Owner: -
--

ALTER TABLE ONLY scheduling.attendance_audit
    ADD CONSTRAINT attendance_audit_class_session_id_fkey FOREIGN KEY (class_session_id) REFERENCES scheduling.class_sessions(id);


--
-- Name: bookings bookings_class_session_id_fkey; Type: FK CONSTRAINT; Schema: scheduling; Owner: -
--

ALTER TABLE ONLY scheduling.bookings
    ADD CONSTRAINT bookings_class_session_id_fkey FOREIGN KEY (class_session_id) REFERENCES scheduling.class_sessions(id);


--
-- Name: global_role_assignments; Type: ROW SECURITY; Schema: account; Owner: -
--

ALTER TABLE account.global_role_assignments ENABLE ROW LEVEL SECURITY;

--
-- Name: global_role_assignments global_role_assignments_access; Type: POLICY; Schema: account; Owner: -
--

CREATE POLICY global_role_assignments_access ON account.global_role_assignments USING (((user_id = app_security.user_id()) OR app_security.platform_admin() OR app_security.service_is('accountservice'::text))) WITH CHECK ((app_security.platform_admin() OR app_security.service_is('accountservice'::text)));


--
-- Name: global_role_audit; Type: ROW SECURITY; Schema: account; Owner: -
--

ALTER TABLE account.global_role_audit ENABLE ROW LEVEL SECURITY;

--
-- Name: global_role_audit global_role_audit_insert; Type: POLICY; Schema: account; Owner: -
--

CREATE POLICY global_role_audit_insert ON account.global_role_audit FOR INSERT WITH CHECK ((app_security.platform_admin() OR app_security.service_is('accountservice'::text)));


--
-- Name: global_role_audit global_role_audit_read; Type: POLICY; Schema: account; Owner: -
--

CREATE POLICY global_role_audit_read ON account.global_role_audit FOR SELECT USING (((target_user_id = app_security.user_id()) OR app_security.platform_admin() OR app_security.service_is('accountservice'::text)));


--
-- Name: invitations; Type: ROW SECURITY; Schema: account; Owner: -
--

ALTER TABLE account.invitations ENABLE ROW LEVEL SECURITY;

--
-- Name: invitations invitations_isolation; Type: POLICY; Schema: account; Owner: -
--

CREATE POLICY invitations_isolation ON account.invitations USING (app_security.owns_studio(studio_id)) WITH CHECK (app_security.owns_studio(studio_id));


--
-- Name: studio_memberships memberships_isolation; Type: POLICY; Schema: account; Owner: -
--

CREATE POLICY memberships_isolation ON account.studio_memberships USING ((app_security.owns_user(user_id) OR app_security.owns_studio(studio_id))) WITH CHECK (app_security.owns_studio(studio_id));


--
-- Name: studio_memberships; Type: ROW SECURITY; Schema: account; Owner: -
--

ALTER TABLE account.studio_memberships ENABLE ROW LEVEL SECURITY;

--
-- Name: teacher_profiles; Type: ROW SECURITY; Schema: account; Owner: -
--

ALTER TABLE account.teacher_profiles ENABLE ROW LEVEL SECURITY;

--
-- Name: teacher_profiles teacher_profiles_read; Type: POLICY; Schema: account; Owner: -
--

CREATE POLICY teacher_profiles_read ON account.teacher_profiles FOR SELECT USING (true);


--
-- Name: teacher_profiles teacher_profiles_write; Type: POLICY; Schema: account; Owner: -
--

CREATE POLICY teacher_profiles_write ON account.teacher_profiles USING (app_security.owns_user(user_id)) WITH CHECK (app_security.owns_user(user_id));


--
-- Name: users; Type: ROW SECURITY; Schema: account; Owner: -
--

ALTER TABLE account.users ENABLE ROW LEVEL SECURITY;

--
-- Name: users users_oidc_subject_resolution; Type: POLICY; Schema: account; Owner: -
--

CREATE POLICY users_oidc_subject_resolution ON account.users FOR SELECT TO account_runtime USING (((current_setting('app.service'::text, true) = 'accountservice'::text) AND (oidc_subject = NULLIF(current_setting('app.oidc_subject'::text, true), ''::text))));


--
-- Name: users users_read; Type: POLICY; Schema: account; Owner: -
--

CREATE POLICY users_read ON account.users FOR SELECT USING (app_security.owns_user(id));


--
-- Name: users users_write; Type: POLICY; Schema: account; Owner: -
--

CREATE POLICY users_write ON account.users FOR UPDATE USING (app_security.owns_user(id)) WITH CHECK (app_security.owns_user(id));


--
-- Name: campaigns; Type: ROW SECURITY; Schema: catalog; Owner: -
--

ALTER TABLE catalog.campaigns ENABLE ROW LEVEL SECURITY;

--
-- Name: campaigns campaigns_admin_write; Type: POLICY; Schema: catalog; Owner: -
--

CREATE POLICY campaigns_admin_write ON catalog.campaigns USING ((((studio_id IS NOT NULL) AND app_security.owns_studio(studio_id)) OR app_security.platform_admin())) WITH CHECK ((((studio_id IS NOT NULL) AND app_security.owns_studio(studio_id)) OR app_security.platform_admin()));


--
-- Name: campaigns campaigns_public_read; Type: POLICY; Schema: catalog; Owner: -
--

CREATE POLICY campaigns_public_read ON catalog.campaigns FOR SELECT USING (((active AND (starts_at <= now()) AND (ends_at > now())) OR ((studio_id IS NOT NULL) AND app_security.owns_studio(studio_id)) OR app_security.platform_admin()));


--
-- Name: credit_product_versions; Type: ROW SECURITY; Schema: catalog; Owner: -
--

ALTER TABLE catalog.credit_product_versions ENABLE ROW LEVEL SECURITY;

--
-- Name: credit_product_versions credit_product_versions_read; Type: POLICY; Schema: catalog; Owner: -
--

CREATE POLICY credit_product_versions_read ON catalog.credit_product_versions FOR SELECT USING ((EXISTS ( SELECT 1
   FROM catalog.credit_products p
  WHERE ((p.id = credit_product_versions.product_id) AND (p.active OR ((p.studio_id IS NOT NULL) AND app_security.owns_studio(p.studio_id)) OR app_security.platform_admin())))));


--
-- Name: credit_product_versions credit_product_versions_write; Type: POLICY; Schema: catalog; Owner: -
--

CREATE POLICY credit_product_versions_write ON catalog.credit_product_versions USING ((EXISTS ( SELECT 1
   FROM catalog.credit_products p
  WHERE ((p.id = credit_product_versions.product_id) AND (((p.studio_id IS NOT NULL) AND app_security.owns_studio(p.studio_id)) OR app_security.platform_admin()))))) WITH CHECK ((EXISTS ( SELECT 1
   FROM catalog.credit_products p
  WHERE ((p.id = credit_product_versions.product_id) AND (((p.studio_id IS NOT NULL) AND app_security.owns_studio(p.studio_id)) OR app_security.platform_admin())))));


--
-- Name: credit_products; Type: ROW SECURITY; Schema: catalog; Owner: -
--

ALTER TABLE catalog.credit_products ENABLE ROW LEVEL SECURITY;

--
-- Name: credit_products credit_products_admin_write; Type: POLICY; Schema: catalog; Owner: -
--

CREATE POLICY credit_products_admin_write ON catalog.credit_products USING ((((studio_id IS NOT NULL) AND app_security.owns_studio(studio_id)) OR app_security.platform_admin())) WITH CHECK ((((studio_id IS NOT NULL) AND app_security.owns_studio(studio_id)) OR app_security.platform_admin()));


--
-- Name: credit_products credit_products_public_read; Type: POLICY; Schema: catalog; Owner: -
--

CREATE POLICY credit_products_public_read ON catalog.credit_products FOR SELECT USING ((active OR ((studio_id IS NOT NULL) AND app_security.owns_studio(studio_id)) OR app_security.platform_admin()));


--
-- Name: rooms; Type: ROW SECURITY; Schema: catalog; Owner: -
--

ALTER TABLE catalog.rooms ENABLE ROW LEVEL SECURITY;

--
-- Name: rooms rooms_admin_write; Type: POLICY; Schema: catalog; Owner: -
--

CREATE POLICY rooms_admin_write ON catalog.rooms USING ((app_security.owns_studio(studio_id) OR app_security.platform_admin())) WITH CHECK ((app_security.owns_studio(studio_id) OR app_security.platform_admin()));


--
-- Name: rooms rooms_public_read; Type: POLICY; Schema: catalog; Owner: -
--

CREATE POLICY rooms_public_read ON catalog.rooms FOR SELECT USING ((rentable OR app_security.owns_studio(studio_id) OR app_security.platform_admin()));


--
-- Name: studios; Type: ROW SECURITY; Schema: catalog; Owner: -
--

ALTER TABLE catalog.studios ENABLE ROW LEVEL SECURITY;

--
-- Name: studios studios_admin_write; Type: POLICY; Schema: catalog; Owner: -
--

CREATE POLICY studios_admin_write ON catalog.studios USING ((app_security.owns_studio(id) OR app_security.platform_admin())) WITH CHECK ((app_security.owns_studio(id) OR app_security.platform_admin()));


--
-- Name: studios studios_public_read; Type: POLICY; Schema: catalog; Owner: -
--

CREATE POLICY studios_public_read ON catalog.studios FOR SELECT USING ((active OR app_security.owns_studio(id) OR app_security.platform_admin()));


--
-- Name: attachments; Type: ROW SECURITY; Schema: chat; Owner: -
--

ALTER TABLE chat.attachments ENABLE ROW LEVEL SECURITY;

--
-- Name: attachments chat_attachments_runtime; Type: POLICY; Schema: chat; Owner: -
--

CREATE POLICY chat_attachments_runtime ON chat.attachments TO chat_runtime USING (((uploader_id = app_security.user_id()) OR ((studio_id = app_security.studio_id()) AND app_security.has_tenant_role('studio_admin'::text)) OR (EXISTS ( SELECT 1
   FROM chat.conversation_participants participant
  WHERE ((participant.conversation_id = attachments.conversation_id) AND (participant.user_id = app_security.user_id()) AND (participant.state = 'ACTIVE'::chat.member_state)))))) WITH CHECK (((uploader_id = app_security.user_id()) OR ((studio_id = app_security.studio_id()) AND app_security.has_tenant_role('studio_admin'::text))));


--
-- Name: attachments chat_attachments_worker; Type: POLICY; Schema: chat; Owner: -
--

CREATE POLICY chat_attachments_worker ON chat.attachments TO chat_media_worker USING ((app_security.setting('app.service'::text) = 'chat-media-worker'::text)) WITH CHECK ((app_security.setting('app.service'::text) = 'chat-media-worker'::text));


--
-- Name: moderation_audit chat_audit_access; Type: POLICY; Schema: chat; Owner: -
--

CREATE POLICY chat_audit_access ON chat.moderation_audit TO chat_runtime USING (((actor_id = app_security.user_id()) OR (target_user_id = app_security.user_id()) OR ((studio_id = app_security.studio_id()) AND app_security.has_tenant_role('studio_admin'::text)) OR app_security.platform_admin())) WITH CHECK (((actor_id = app_security.user_id()) OR ((studio_id = app_security.studio_id()) AND app_security.has_tenant_role('studio_admin'::text))));


--
-- Name: group_bans chat_bans_access; Type: POLICY; Schema: chat; Owner: -
--

CREATE POLICY chat_bans_access ON chat.group_bans TO chat_runtime USING (((user_id = app_security.user_id()) OR (owner_user_id = app_security.user_id()) OR ((studio_id = app_security.studio_id()) AND app_security.has_tenant_role('studio_admin'::text)))) WITH CHECK (((owner_user_id = app_security.user_id()) OR ((studio_id = app_security.studio_id()) AND app_security.has_tenant_role('studio_admin'::text))));


--
-- Name: user_blocks chat_blocks_delete; Type: POLICY; Schema: chat; Owner: -
--

CREATE POLICY chat_blocks_delete ON chat.user_blocks FOR DELETE TO chat_runtime USING ((blocker_user_id = app_security.user_id()));


--
-- Name: user_blocks chat_blocks_insert; Type: POLICY; Schema: chat; Owner: -
--

CREATE POLICY chat_blocks_insert ON chat.user_blocks FOR INSERT TO chat_runtime WITH CHECK ((blocker_user_id = app_security.user_id()));


--
-- Name: user_blocks chat_blocks_select; Type: POLICY; Schema: chat; Owner: -
--

CREATE POLICY chat_blocks_select ON chat.user_blocks FOR SELECT TO chat_runtime USING (((blocker_user_id = app_security.user_id()) OR (blocked_user_id = app_security.user_id())));


--
-- Name: conversations chat_conversations_delete; Type: POLICY; Schema: chat; Owner: -
--

CREATE POLICY chat_conversations_delete ON chat.conversations FOR DELETE TO chat_runtime USING (((kind = ANY (ARRAY['STUDIO_GROUP'::chat.conversation_kind, 'TEACHER_GROUP'::chat.conversation_kind])) AND ((owner_user_id = app_security.user_id()) OR ((studio_id = app_security.studio_id()) AND app_security.has_tenant_role('studio_admin'::text)))));


--
-- Name: conversations chat_conversations_insert; Type: POLICY; Schema: chat; Owner: -
--

CREATE POLICY chat_conversations_insert ON chat.conversations FOR INSERT TO chat_runtime WITH CHECK (((app_security.user_id() IS NOT NULL) AND (app_security.owns_user(student_id) OR app_security.owns_user(teacher_id) OR app_security.owns_user(owner_user_id) OR ((studio_id = app_security.studio_id()) AND app_security.has_tenant_role('studio_admin'::text)))));


--
-- Name: conversations chat_conversations_select; Type: POLICY; Schema: chat; Owner: -
--

CREATE POLICY chat_conversations_select ON chat.conversations FOR SELECT TO chat_runtime USING ((active AND ((kind = ANY (ARRAY['STUDIO_GROUP'::chat.conversation_kind, 'TEACHER_GROUP'::chat.conversation_kind])) OR app_security.owns_user(student_id) OR app_security.owns_user(teacher_id) OR app_security.owns_user(direct_low_user_id) OR app_security.owns_user(direct_high_user_id) OR app_security.owns_user(owner_user_id) OR ((studio_id = app_security.studio_id()) AND app_security.has_tenant_role('studio_admin'::text)) OR (EXISTS ( SELECT 1
   FROM chat.conversation_participants participant
  WHERE ((participant.conversation_id = conversations.id) AND (participant.user_id = app_security.user_id()) AND (participant.state = 'ACTIVE'::chat.member_state)))))));


--
-- Name: inbox_events chat_inbox_runtime; Type: POLICY; Schema: chat; Owner: -
--

CREATE POLICY chat_inbox_runtime ON chat.inbox_events TO chat_runtime USING ((app_security.user_id() IS NOT NULL)) WITH CHECK ((app_security.user_id() IS NOT NULL));


--
-- Name: media_deletion_jobs chat_media_deletion_runtime; Type: POLICY; Schema: chat; Owner: -
--

CREATE POLICY chat_media_deletion_runtime ON chat.media_deletion_jobs FOR INSERT TO chat_runtime WITH CHECK ((app_security.user_id() IS NOT NULL));


--
-- Name: media_deletion_jobs chat_media_deletion_worker; Type: POLICY; Schema: chat; Owner: -
--

CREATE POLICY chat_media_deletion_worker ON chat.media_deletion_jobs TO chat_media_worker USING ((app_security.setting('app.service'::text) = 'chat-media-worker'::text)) WITH CHECK ((app_security.setting('app.service'::text) = 'chat-media-worker'::text));


--
-- Name: messages chat_messages_insert; Type: POLICY; Schema: chat; Owner: -
--

CREATE POLICY chat_messages_insert ON chat.messages FOR INSERT TO chat_runtime WITH CHECK (((sender_id = app_security.user_id()) AND (((studio_id = app_security.studio_id()) AND app_security.has_tenant_role('studio_admin'::text)) OR (EXISTS ( SELECT 1
   FROM chat.conversation_participants participant
  WHERE ((participant.conversation_id = messages.conversation_id) AND (participant.user_id = app_security.user_id()) AND (participant.state = 'ACTIVE'::chat.member_state)))))));


--
-- Name: messages chat_messages_select; Type: POLICY; Schema: chat; Owner: -
--

CREATE POLICY chat_messages_select ON chat.messages FOR SELECT TO chat_runtime USING ((((studio_id = app_security.studio_id()) AND app_security.has_tenant_role('studio_admin'::text)) OR (EXISTS ( SELECT 1
   FROM chat.conversation_participants participant
  WHERE ((participant.conversation_id = messages.conversation_id) AND (participant.user_id = app_security.user_id()) AND (participant.state = 'ACTIVE'::chat.member_state) AND (messages.sequence >= participant.visible_from_sequence))))));


--
-- Name: messages chat_messages_update; Type: POLICY; Schema: chat; Owner: -
--

CREATE POLICY chat_messages_update ON chat.messages FOR UPDATE TO chat_runtime USING (((sender_id = app_security.user_id()) OR ((studio_id = app_security.studio_id()) AND app_security.has_tenant_role('studio_admin'::text)))) WITH CHECK (((sender_id = app_security.user_id()) OR ((studio_id = app_security.studio_id()) AND app_security.has_tenant_role('studio_admin'::text))));


--
-- Name: conversation_participants chat_participants_access; Type: POLICY; Schema: chat; Owner: -
--

CREATE POLICY chat_participants_access ON chat.conversation_participants TO chat_runtime USING ((app_security.owns_user(user_id) OR app_security.owns_user(owner_user_id) OR ((studio_id = app_security.studio_id()) AND app_security.has_tenant_role('studio_admin'::text)))) WITH CHECK ((app_security.owns_user(user_id) OR app_security.owns_user(owner_user_id) OR ((studio_id = app_security.studio_id()) AND app_security.has_tenant_role('studio_admin'::text))));


--
-- Name: realtime_outbox chat_realtime_media; Type: POLICY; Schema: chat; Owner: -
--

CREATE POLICY chat_realtime_media ON chat.realtime_outbox FOR INSERT TO chat_media_worker WITH CHECK ((app_security.setting('app.service'::text) = 'chat-media-worker'::text));


--
-- Name: realtime_outbox chat_realtime_relay; Type: POLICY; Schema: chat; Owner: -
--

CREATE POLICY chat_realtime_relay ON chat.realtime_outbox TO chat_relay USING ((app_security.setting('app.service'::text) = 'chat-relay'::text)) WITH CHECK ((app_security.setting('app.service'::text) = 'chat-relay'::text));


--
-- Name: realtime_outbox chat_realtime_runtime; Type: POLICY; Schema: chat; Owner: -
--

CREATE POLICY chat_realtime_runtime ON chat.realtime_outbox FOR INSERT TO chat_runtime WITH CHECK ((app_security.user_id() IS NOT NULL));


--
-- Name: message_reports chat_reports_access; Type: POLICY; Schema: chat; Owner: -
--

CREATE POLICY chat_reports_access ON chat.message_reports TO chat_runtime USING (((reporter_id = app_security.user_id()) OR app_security.platform_admin())) WITH CHECK ((reporter_id = app_security.user_id()));


--
-- Name: conversation_participants; Type: ROW SECURITY; Schema: chat; Owner: -
--

ALTER TABLE chat.conversation_participants ENABLE ROW LEVEL SECURITY;

--
-- Name: conversations; Type: ROW SECURITY; Schema: chat; Owner: -
--

ALTER TABLE chat.conversations ENABLE ROW LEVEL SECURITY;

--
-- Name: group_bans; Type: ROW SECURITY; Schema: chat; Owner: -
--

ALTER TABLE chat.group_bans ENABLE ROW LEVEL SECURITY;

--
-- Name: inbox_events; Type: ROW SECURITY; Schema: chat; Owner: -
--

ALTER TABLE chat.inbox_events ENABLE ROW LEVEL SECURITY;

--
-- Name: media_deletion_jobs; Type: ROW SECURITY; Schema: chat; Owner: -
--

ALTER TABLE chat.media_deletion_jobs ENABLE ROW LEVEL SECURITY;

--
-- Name: message_reports; Type: ROW SECURITY; Schema: chat; Owner: -
--

ALTER TABLE chat.message_reports ENABLE ROW LEVEL SECURITY;

--
-- Name: messages; Type: ROW SECURITY; Schema: chat; Owner: -
--

ALTER TABLE chat.messages ENABLE ROW LEVEL SECURITY;

--
-- Name: moderation_audit; Type: ROW SECURITY; Schema: chat; Owner: -
--

ALTER TABLE chat.moderation_audit ENABLE ROW LEVEL SECURITY;

--
-- Name: realtime_outbox; Type: ROW SECURITY; Schema: chat; Owner: -
--

ALTER TABLE chat.realtime_outbox ENABLE ROW LEVEL SECURITY;

--
-- Name: user_blocks; Type: ROW SECURITY; Schema: chat; Owner: -
--

ALTER TABLE chat.user_blocks ENABLE ROW LEVEL SECURITY;

--
-- Name: grant_operations; Type: ROW SECURITY; Schema: credit; Owner: -
--

ALTER TABLE credit.grant_operations ENABLE ROW LEVEL SECURITY;

--
-- Name: grant_operations grant_operations_isolation; Type: POLICY; Schema: credit; Owner: -
--

CREATE POLICY grant_operations_isolation ON credit.grant_operations USING (((source_user_id = app_security.user_id()) OR (target_user_id = app_security.user_id()) OR ((studio_id IS NOT NULL) AND app_security.owns_studio(studio_id)))) WITH CHECK (((studio_id IS NOT NULL) AND app_security.owns_studio(studio_id)));


--
-- Name: grants; Type: ROW SECURITY; Schema: credit; Owner: -
--

ALTER TABLE credit.grants ENABLE ROW LEVEL SECURITY;

--
-- Name: grants grants_isolation; Type: POLICY; Schema: credit; Owner: -
--

CREATE POLICY grants_isolation ON credit.grants USING ((app_security.owns_user(user_id) OR ((studio_id IS NOT NULL) AND app_security.owns_studio(studio_id)))) WITH CHECK ((app_security.owns_user(user_id) OR ((studio_id IS NOT NULL) AND app_security.owns_studio(studio_id))));


--
-- Name: hold_allocations; Type: ROW SECURITY; Schema: credit; Owner: -
--

ALTER TABLE credit.hold_allocations ENABLE ROW LEVEL SECURITY;

--
-- Name: hold_allocations hold_allocations_isolation; Type: POLICY; Schema: credit; Owner: -
--

CREATE POLICY hold_allocations_isolation ON credit.hold_allocations USING ((EXISTS ( SELECT 1
   FROM credit.holds h
  WHERE ((h.id = hold_allocations.hold_id) AND (app_security.owns_user(h.user_id) OR app_security.owns_studio(h.studio_id)))))) WITH CHECK ((EXISTS ( SELECT 1
   FROM credit.holds h
  WHERE ((h.id = hold_allocations.hold_id) AND (app_security.owns_user(h.user_id) OR app_security.owns_studio(h.studio_id))))));


--
-- Name: holds; Type: ROW SECURITY; Schema: credit; Owner: -
--

ALTER TABLE credit.holds ENABLE ROW LEVEL SECURITY;

--
-- Name: holds holds_isolation; Type: POLICY; Schema: credit; Owner: -
--

CREATE POLICY holds_isolation ON credit.holds USING ((app_security.owns_user(user_id) OR app_security.owns_studio(studio_id))) WITH CHECK ((app_security.owns_user(user_id) OR app_security.owns_studio(studio_id)));


--
-- Name: ledger_entries; Type: ROW SECURITY; Schema: credit; Owner: -
--

ALTER TABLE credit.ledger_entries ENABLE ROW LEVEL SECURITY;

--
-- Name: ledger_entries ledger_isolation; Type: POLICY; Schema: credit; Owner: -
--

CREATE POLICY ledger_isolation ON credit.ledger_entries USING ((app_security.owns_user(user_id) OR ((studio_id IS NOT NULL) AND app_security.owns_studio(studio_id)))) WITH CHECK ((app_security.owns_user(user_id) OR ((studio_id IS NOT NULL) AND app_security.owns_studio(studio_id))));


--
-- Name: flashsale_inventory_ledger flashsale_inventory_isolation; Type: POLICY; Schema: orders; Owner: -
--

CREATE POLICY flashsale_inventory_isolation ON orders.flashsale_inventory_ledger USING ((app_security.owns_user(user_id) OR app_security.service_principal_is('orderservice'::text))) WITH CHECK (app_security.service_principal_is('orderservice'::text));


--
-- Name: flashsale_inventory_ledger; Type: ROW SECURITY; Schema: orders; Owner: -
--

ALTER TABLE orders.flashsale_inventory_ledger ENABLE ROW LEVEL SECURITY;

--
-- Name: flashsale_requests; Type: ROW SECURITY; Schema: orders; Owner: -
--

ALTER TABLE orders.flashsale_requests ENABLE ROW LEVEL SECURITY;

--
-- Name: flashsale_requests flashsale_requests_isolation; Type: POLICY; Schema: orders; Owner: -
--

CREATE POLICY flashsale_requests_isolation ON orders.flashsale_requests USING ((app_security.owns_user(user_id) OR app_security.service_principal_is('orderservice'::text))) WITH CHECK ((app_security.owns_user(user_id) OR app_security.service_principal_is('orderservice'::text)));


--
-- Name: order_lines; Type: ROW SECURITY; Schema: orders; Owner: -
--

ALTER TABLE orders.order_lines ENABLE ROW LEVEL SECURITY;

--
-- Name: order_lines order_lines_isolation; Type: POLICY; Schema: orders; Owner: -
--

CREATE POLICY order_lines_isolation ON orders.order_lines USING ((EXISTS ( SELECT 1
   FROM orders.orders item_order
  WHERE (item_order.id = order_lines.order_id)))) WITH CHECK ((EXISTS ( SELECT 1
   FROM orders.orders item_order
  WHERE (item_order.id = order_lines.order_id))));


--
-- Name: orders; Type: ROW SECURITY; Schema: orders; Owner: -
--

ALTER TABLE orders.orders ENABLE ROW LEVEL SECURITY;

--
-- Name: orders orders_credit_service_read; Type: POLICY; Schema: orders; Owner: -
--

CREATE POLICY orders_credit_service_read ON orders.orders FOR SELECT USING (app_security.service_principal_is('creditservice'::text));


--
-- Name: orders orders_credit_service_update; Type: POLICY; Schema: orders; Owner: -
--

CREATE POLICY orders_credit_service_update ON orders.orders FOR UPDATE USING (app_security.service_principal_is('creditservice'::text)) WITH CHECK (app_security.service_principal_is('creditservice'::text));


--
-- Name: orders orders_human_access; Type: POLICY; Schema: orders; Owner: -
--

CREATE POLICY orders_human_access ON orders.orders USING ((app_security.owns_user(user_id) OR ((studio_id IS NOT NULL) AND app_security.owns_studio(studio_id)) OR app_security.platform_admin())) WITH CHECK ((app_security.owns_user(user_id) OR ((studio_id IS NOT NULL) AND app_security.owns_studio(studio_id)) OR app_security.platform_admin()));


--
-- Name: orders orders_payment_saga_read; Type: POLICY; Schema: orders; Owner: -
--

CREATE POLICY orders_payment_saga_read ON orders.orders FOR SELECT USING (app_security.service_principal_is('payment-order-saga'::text));


--
-- Name: orders orders_payment_saga_update; Type: POLICY; Schema: orders; Owner: -
--

CREATE POLICY orders_payment_saga_update ON orders.orders FOR UPDATE USING (app_security.service_principal_is('payment-order-saga'::text)) WITH CHECK (app_security.service_principal_is('payment-order-saga'::text));


--
-- Name: orders orders_worker_access; Type: POLICY; Schema: orders; Owner: -
--

CREATE POLICY orders_worker_access ON orders.orders USING (app_security.service_principal_is('orderservice'::text)) WITH CHECK (app_security.service_principal_is('orderservice'::text));


--
-- Name: payments; Type: ROW SECURITY; Schema: payment; Owner: -
--

ALTER TABLE payment.payments ENABLE ROW LEVEL SECURITY;

--
-- Name: payments payments_service_access; Type: POLICY; Schema: payment; Owner: -
--

CREATE POLICY payments_service_access ON payment.payments USING ((app_security.service_is('paymentservice'::text) OR app_security.platform_admin())) WITH CHECK ((app_security.service_is('paymentservice'::text) OR app_security.platform_admin()));


--
-- Name: webhook_events; Type: ROW SECURITY; Schema: payment; Owner: -
--

ALTER TABLE payment.webhook_events ENABLE ROW LEVEL SECURITY;

--
-- Name: webhook_events webhook_events_service_access; Type: POLICY; Schema: payment; Owner: -
--

CREATE POLICY webhook_events_service_access ON payment.webhook_events USING (app_security.service_is('paymentservice'::text)) WITH CHECK (app_security.service_is('paymentservice'::text));


--
-- Name: monthly_class_facts; Type: ROW SECURITY; Schema: payroll; Owner: -
--

ALTER TABLE payroll.monthly_class_facts ENABLE ROW LEVEL SECURITY;

--
-- Name: monthly_class_facts payroll_isolation; Type: POLICY; Schema: payroll; Owner: -
--

CREATE POLICY payroll_isolation ON payroll.monthly_class_facts FOR SELECT TO payroll_runtime
USING (app_security.platform_admin() OR
       (app_security.setting('app.actor_kind')='human' AND
        (app_security.owns_studio(studio_id) OR
         (teacher_id=app_security.user_id() AND studio_id=app_security.studio_id() AND app_security.has_tenant_role('teacher')))));


--
-- Name: studio_metrics recommendation_metrics_admin; Type: POLICY; Schema: recommendation; Owner: -
--

CREATE POLICY recommendation_metrics_admin ON recommendation.studio_metrics FOR SELECT USING ((app_security.owns_studio(studio_id) OR app_security.platform_admin()));


--
-- Name: studio_metrics recommendation_metrics_projection; Type: POLICY; Schema: recommendation; Owner: -
--

CREATE POLICY recommendation_metrics_projection ON recommendation.studio_metrics USING (app_security.service_is('recommendation-projection'::text)) WITH CHECK (app_security.service_is('recommendation-projection'::text));


--
-- Name: studio_metrics; Type: ROW SECURITY; Schema: recommendation; Owner: -
--

ALTER TABLE recommendation.studio_metrics ENABLE ROW LEVEL SECURITY;

--
-- Name: attendance_audit; Type: ROW SECURITY; Schema: scheduling; Owner: -
--

ALTER TABLE scheduling.attendance_audit ENABLE ROW LEVEL SECURITY;

--
-- Name: attendance_audit attendance_audit_isolation; Type: POLICY; Schema: scheduling; Owner: -
--

CREATE POLICY attendance_audit_isolation ON scheduling.attendance_audit USING ((app_security.owns_studio(studio_id) OR (student_id = app_security.user_id()))) WITH CHECK (app_security.owns_studio(studio_id));


--
-- Name: bookings; Type: ROW SECURITY; Schema: scheduling; Owner: -
--

ALTER TABLE scheduling.bookings ENABLE ROW LEVEL SECURITY;

--
-- Name: bookings bookings_isolation; Type: POLICY; Schema: scheduling; Owner: -
--

CREATE POLICY bookings_isolation ON scheduling.bookings USING ((app_security.owns_user(student_id) OR app_security.owns_studio(studio_id))) WITH CHECK ((app_security.owns_user(student_id) OR app_security.owns_studio(studio_id)));


--
-- Name: bookings bookings_worker_access; Type: POLICY; Schema: scheduling; Owner: -
--

CREATE POLICY bookings_worker_access ON scheduling.bookings TO scheduling_runtime USING (app_security.service_principal_is('scheduling-worker'::text)) WITH CHECK (app_security.service_principal_is('scheduling-worker'::text));

-- Teachers can read the students of their own classes across studios. Audited
-- corrections are persisted by the worker; this policy grants no direct write.
CREATE POLICY bookings_teacher_read ON scheduling.bookings FOR SELECT TO scheduling_runtime
USING (app_security.setting('app.actor_kind')='human' AND app_security.has_tenant_role('teacher') AND
       EXISTS (SELECT 1 FROM scheduling.class_sessions s
               WHERE s.id=class_session_id AND s.studio_id=scheduling.bookings.studio_id
                 AND s.teacher_id=app_security.user_id()));


--
-- Name: class_sessions; Type: ROW SECURITY; Schema: scheduling; Owner: -
--

ALTER TABLE scheduling.class_sessions ENABLE ROW LEVEL SECURITY;

--
-- Name: class_sessions class_sessions_isolation; Type: POLICY; Schema: scheduling; Owner: -
--

CREATE POLICY class_sessions_isolation ON scheduling.class_sessions USING ((app_security.platform_admin() OR (studio_id = app_security.studio_id()) OR (teacher_id = app_security.user_id()) OR ((app_security.user_id() IS NULL) AND (status = ANY (ARRAY['OPEN'::scheduling.class_status, 'MINIMUM_CONFIRMED'::scheduling.class_status]))))) WITH CHECK ((app_security.platform_admin() OR (studio_id = app_security.studio_id()) OR (teacher_id = app_security.user_id())));


--
-- Name: class_sessions class_sessions_worker_access; Type: POLICY; Schema: scheduling; Owner: -
--

CREATE POLICY class_sessions_worker_access ON scheduling.class_sessions TO scheduling_runtime USING (app_security.service_principal_is('scheduling-worker'::text)) WITH CHECK (app_security.service_principal_is('scheduling-worker'::text));


--
-- Name: room_reservations; Type: ROW SECURITY; Schema: scheduling; Owner: -
--

ALTER TABLE scheduling.room_reservations ENABLE ROW LEVEL SECURITY;

--
-- Name: room_reservations room_reservations_isolation; Type: POLICY; Schema: scheduling; Owner: -
--

CREATE POLICY room_reservations_isolation ON scheduling.room_reservations USING ((app_security.owns_user(student_id) OR app_security.owns_studio(studio_id))) WITH CHECK ((app_security.owns_user(student_id) OR app_security.owns_studio(studio_id)));


--
-- Name: room_reservations room_reservations_worker_access; Type: POLICY; Schema: scheduling; Owner: -
--

CREATE POLICY room_reservations_worker_access ON scheduling.room_reservations TO scheduling_runtime USING (app_security.service_principal_is('scheduling-worker'::text)) WITH CHECK (app_security.service_principal_is('scheduling-worker'::text));


--
-- Name: SCHEMA account; Type: ACL; Schema: -; Owner: -
--

GRANT USAGE ON SCHEMA account TO account_runtime;
GRANT USAGE ON SCHEMA account TO outbox_worker;
GRANT USAGE ON SCHEMA account TO account_chat_definer;


--
-- Name: SCHEMA app_security; Type: ACL; Schema: -; Owner: -
--

GRANT USAGE ON SCHEMA app_security TO PUBLIC;
GRANT USAGE ON SCHEMA app_security TO account_runtime;
GRANT USAGE ON SCHEMA app_security TO catalog_runtime;
GRANT USAGE ON SCHEMA app_security TO scheduling_runtime;
GRANT USAGE ON SCHEMA app_security TO credit_runtime;
GRANT USAGE ON SCHEMA app_security TO orders_runtime;
GRANT USAGE ON SCHEMA app_security TO payment_runtime;
GRANT USAGE ON SCHEMA app_security TO payroll_runtime;
GRANT USAGE ON SCHEMA app_security TO outbox_worker;
GRANT USAGE ON SCHEMA app_security TO projection_worker;
GRANT USAGE ON SCHEMA app_security TO recommendation_runtime;
GRANT USAGE ON SCHEMA app_security TO chat_runtime;
GRANT USAGE ON SCHEMA app_security TO chat_relay;
GRANT USAGE ON SCHEMA app_security TO chat_media_worker;
GRANT USAGE ON SCHEMA app_security TO chat_invariant_definer;


--
-- Name: SCHEMA catalog; Type: ACL; Schema: -; Owner: -
--

GRANT USAGE ON SCHEMA catalog TO catalog_runtime;
GRANT USAGE ON SCHEMA catalog TO outbox_worker;


--
-- Name: SCHEMA chat; Type: ACL; Schema: -; Owner: -
--

GRANT USAGE ON SCHEMA chat TO chat_runtime;
GRANT USAGE ON SCHEMA chat TO chat_relay;
GRANT USAGE ON SCHEMA chat TO chat_media_worker;
GRANT USAGE ON SCHEMA chat TO chat_invariant_definer;
GRANT USAGE ON SCHEMA chat TO outbox_worker;


--
-- Name: SCHEMA credit; Type: ACL; Schema: -; Owner: -
--

GRANT USAGE ON SCHEMA credit TO credit_runtime;
GRANT USAGE ON SCHEMA credit TO outbox_worker;


--
-- Name: SCHEMA orders; Type: ACL; Schema: -; Owner: -
--

GRANT USAGE ON SCHEMA orders TO orders_runtime;
GRANT USAGE ON SCHEMA orders TO outbox_worker;


--
-- Name: SCHEMA payment; Type: ACL; Schema: -; Owner: -
--

GRANT USAGE ON SCHEMA payment TO payment_runtime;
GRANT USAGE ON SCHEMA payment TO outbox_worker;


--
-- Name: SCHEMA payroll; Type: ACL; Schema: -; Owner: -
--

GRANT USAGE ON SCHEMA payroll TO payroll_runtime;
GRANT USAGE ON SCHEMA payroll TO outbox_worker;


--
-- Name: SCHEMA public; Type: ACL; Schema: -; Owner: -
--

GRANT ALL ON SCHEMA public TO dancehub_migrator;


--
-- Name: SCHEMA recommendation; Type: ACL; Schema: -; Owner: -
--

GRANT USAGE ON SCHEMA recommendation TO outbox_worker;
GRANT USAGE ON SCHEMA recommendation TO recommendation_runtime;


--
-- Name: SCHEMA scheduling; Type: ACL; Schema: -; Owner: -
--

GRANT USAGE ON SCHEMA scheduling TO scheduling_runtime;
GRANT USAGE ON SCHEMA scheduling TO outbox_worker;


--
-- Name: FUNCTION get_chat_memberships(requested_user uuid); Type: ACL; Schema: account; Owner: -
--

REVOKE ALL ON FUNCTION account.get_chat_memberships(requested_user uuid) FROM PUBLIC;
GRANT ALL ON FUNCTION account.get_chat_memberships(requested_user uuid) TO account_runtime;


--
-- Name: FUNCTION get_chat_principal(requested_user uuid); Type: ACL; Schema: account; Owner: -
--

REVOKE ALL ON FUNCTION account.get_chat_principal(requested_user uuid) FROM PUBLIC;
GRANT ALL ON FUNCTION account.get_chat_principal(requested_user uuid) TO account_runtime;


--
-- Name: FUNCTION resolve_oidc_subject(subject text); Type: ACL; Schema: account; Owner: -
--

REVOKE ALL ON FUNCTION account.resolve_oidc_subject(subject text) FROM PUBLIC;
GRANT ALL ON FUNCTION account.resolve_oidc_subject(subject text) TO account_runtime;


--
-- Name: FUNCTION search_public_teachers(selected_studio uuid, name_query text, max_rows integer); Type: ACL; Schema: account; Owner: -
--

REVOKE ALL ON FUNCTION account.search_public_teachers(selected_studio uuid, name_query text, max_rows integer) FROM PUBLIC;
GRANT ALL ON FUNCTION account.search_public_teachers(selected_studio uuid, name_query text, max_rows integer) TO account_runtime;


--
-- Name: FUNCTION has_global_role(role_name text); Type: ACL; Schema: app_security; Owner: -
--

REVOKE ALL ON FUNCTION app_security.has_global_role(role_name text) FROM PUBLIC;
GRANT ALL ON FUNCTION app_security.has_global_role(role_name text) TO account_runtime;
GRANT ALL ON FUNCTION app_security.has_global_role(role_name text) TO catalog_runtime;
GRANT ALL ON FUNCTION app_security.has_global_role(role_name text) TO scheduling_runtime;
GRANT ALL ON FUNCTION app_security.has_global_role(role_name text) TO credit_runtime;
GRANT ALL ON FUNCTION app_security.has_global_role(role_name text) TO orders_runtime;
GRANT ALL ON FUNCTION app_security.has_global_role(role_name text) TO payment_runtime;
GRANT ALL ON FUNCTION app_security.has_global_role(role_name text) TO payroll_runtime;
GRANT ALL ON FUNCTION app_security.has_global_role(role_name text) TO recommendation_runtime;
GRANT ALL ON FUNCTION app_security.has_global_role(role_name text) TO outbox_worker;
GRANT ALL ON FUNCTION app_security.has_global_role(role_name text) TO projection_worker;
GRANT ALL ON FUNCTION app_security.has_global_role(role_name text) TO chat_runtime;
GRANT ALL ON FUNCTION app_security.has_global_role(role_name text) TO chat_relay;
GRANT ALL ON FUNCTION app_security.has_global_role(role_name text) TO chat_media_worker;
-- chat.next_message_sequence / chat.refresh_member_count run as this SECURITY DEFINER owner
-- and evaluate the same tenant predicates, so it needs the functions PUBLIC may not call.
GRANT EXECUTE ON FUNCTION app_security.has_global_role(role_name text) TO chat_invariant_definer;


--
-- Name: FUNCTION has_tenant_role(role_name text); Type: ACL; Schema: app_security; Owner: -
--

REVOKE ALL ON FUNCTION app_security.has_tenant_role(role_name text) FROM PUBLIC;
GRANT ALL ON FUNCTION app_security.has_tenant_role(role_name text) TO account_runtime;
GRANT ALL ON FUNCTION app_security.has_tenant_role(role_name text) TO catalog_runtime;
GRANT ALL ON FUNCTION app_security.has_tenant_role(role_name text) TO scheduling_runtime;
GRANT ALL ON FUNCTION app_security.has_tenant_role(role_name text) TO credit_runtime;
GRANT ALL ON FUNCTION app_security.has_tenant_role(role_name text) TO orders_runtime;
GRANT ALL ON FUNCTION app_security.has_tenant_role(role_name text) TO payment_runtime;
GRANT ALL ON FUNCTION app_security.has_tenant_role(role_name text) TO payroll_runtime;
GRANT ALL ON FUNCTION app_security.has_tenant_role(role_name text) TO recommendation_runtime;
GRANT ALL ON FUNCTION app_security.has_tenant_role(role_name text) TO outbox_worker;
GRANT ALL ON FUNCTION app_security.has_tenant_role(role_name text) TO projection_worker;
GRANT ALL ON FUNCTION app_security.has_tenant_role(role_name text) TO chat_runtime;
GRANT ALL ON FUNCTION app_security.has_tenant_role(role_name text) TO chat_relay;
GRANT ALL ON FUNCTION app_security.has_tenant_role(role_name text) TO chat_media_worker;
-- chat.next_message_sequence / chat.refresh_member_count run as this SECURITY DEFINER owner
-- and evaluate the same tenant predicates, so it needs the functions PUBLIC may not call.
GRANT EXECUTE ON FUNCTION app_security.has_tenant_role(role_name text) TO chat_invariant_definer;


--
-- Name: FUNCTION owns_studio(row_studio_id uuid); Type: ACL; Schema: app_security; Owner: -
--

GRANT ALL ON FUNCTION app_security.owns_studio(row_studio_id uuid) TO account_runtime;
GRANT ALL ON FUNCTION app_security.owns_studio(row_studio_id uuid) TO catalog_runtime;
GRANT ALL ON FUNCTION app_security.owns_studio(row_studio_id uuid) TO scheduling_runtime;
GRANT ALL ON FUNCTION app_security.owns_studio(row_studio_id uuid) TO credit_runtime;
GRANT ALL ON FUNCTION app_security.owns_studio(row_studio_id uuid) TO orders_runtime;
GRANT ALL ON FUNCTION app_security.owns_studio(row_studio_id uuid) TO payment_runtime;
GRANT ALL ON FUNCTION app_security.owns_studio(row_studio_id uuid) TO payroll_runtime;
GRANT ALL ON FUNCTION app_security.owns_studio(row_studio_id uuid) TO outbox_worker;
GRANT ALL ON FUNCTION app_security.owns_studio(row_studio_id uuid) TO projection_worker;
GRANT ALL ON FUNCTION app_security.owns_studio(row_studio_id uuid) TO recommendation_runtime;
GRANT ALL ON FUNCTION app_security.owns_studio(row_studio_id uuid) TO chat_runtime;
GRANT ALL ON FUNCTION app_security.owns_studio(row_studio_id uuid) TO chat_relay;
GRANT ALL ON FUNCTION app_security.owns_studio(row_studio_id uuid) TO chat_media_worker;
GRANT ALL ON FUNCTION app_security.owns_studio(row_studio_id uuid) TO chat_invariant_definer;


--
-- Name: FUNCTION owns_user(row_user_id uuid); Type: ACL; Schema: app_security; Owner: -
--

GRANT ALL ON FUNCTION app_security.owns_user(row_user_id uuid) TO account_runtime;
GRANT ALL ON FUNCTION app_security.owns_user(row_user_id uuid) TO catalog_runtime;
GRANT ALL ON FUNCTION app_security.owns_user(row_user_id uuid) TO scheduling_runtime;
GRANT ALL ON FUNCTION app_security.owns_user(row_user_id uuid) TO credit_runtime;
GRANT ALL ON FUNCTION app_security.owns_user(row_user_id uuid) TO orders_runtime;
GRANT ALL ON FUNCTION app_security.owns_user(row_user_id uuid) TO payment_runtime;
GRANT ALL ON FUNCTION app_security.owns_user(row_user_id uuid) TO payroll_runtime;
GRANT ALL ON FUNCTION app_security.owns_user(row_user_id uuid) TO outbox_worker;
GRANT ALL ON FUNCTION app_security.owns_user(row_user_id uuid) TO projection_worker;
GRANT ALL ON FUNCTION app_security.owns_user(row_user_id uuid) TO recommendation_runtime;
GRANT ALL ON FUNCTION app_security.owns_user(row_user_id uuid) TO chat_runtime;
GRANT ALL ON FUNCTION app_security.owns_user(row_user_id uuid) TO chat_relay;
GRANT ALL ON FUNCTION app_security.owns_user(row_user_id uuid) TO chat_media_worker;
GRANT ALL ON FUNCTION app_security.owns_user(row_user_id uuid) TO chat_invariant_definer;


--
-- Name: FUNCTION platform_admin(); Type: ACL; Schema: app_security; Owner: -
--

GRANT ALL ON FUNCTION app_security.platform_admin() TO account_runtime;
GRANT ALL ON FUNCTION app_security.platform_admin() TO catalog_runtime;
GRANT ALL ON FUNCTION app_security.platform_admin() TO scheduling_runtime;
GRANT ALL ON FUNCTION app_security.platform_admin() TO credit_runtime;
GRANT ALL ON FUNCTION app_security.platform_admin() TO orders_runtime;
GRANT ALL ON FUNCTION app_security.platform_admin() TO payment_runtime;
GRANT ALL ON FUNCTION app_security.platform_admin() TO payroll_runtime;
GRANT ALL ON FUNCTION app_security.platform_admin() TO outbox_worker;
GRANT ALL ON FUNCTION app_security.platform_admin() TO projection_worker;
GRANT ALL ON FUNCTION app_security.platform_admin() TO recommendation_runtime;
GRANT ALL ON FUNCTION app_security.platform_admin() TO chat_runtime;
GRANT ALL ON FUNCTION app_security.platform_admin() TO chat_relay;
GRANT ALL ON FUNCTION app_security.platform_admin() TO chat_media_worker;
GRANT ALL ON FUNCTION app_security.platform_admin() TO chat_invariant_definer;


--
-- Name: FUNCTION service_is(service_name text); Type: ACL; Schema: app_security; Owner: -
--

GRANT ALL ON FUNCTION app_security.service_is(service_name text) TO account_runtime;
GRANT ALL ON FUNCTION app_security.service_is(service_name text) TO catalog_runtime;
GRANT ALL ON FUNCTION app_security.service_is(service_name text) TO scheduling_runtime;
GRANT ALL ON FUNCTION app_security.service_is(service_name text) TO credit_runtime;
GRANT ALL ON FUNCTION app_security.service_is(service_name text) TO orders_runtime;
GRANT ALL ON FUNCTION app_security.service_is(service_name text) TO payment_runtime;
GRANT ALL ON FUNCTION app_security.service_is(service_name text) TO payroll_runtime;
GRANT ALL ON FUNCTION app_security.service_is(service_name text) TO outbox_worker;
GRANT ALL ON FUNCTION app_security.service_is(service_name text) TO projection_worker;
GRANT ALL ON FUNCTION app_security.service_is(service_name text) TO recommendation_runtime;
GRANT ALL ON FUNCTION app_security.service_is(service_name text) TO chat_runtime;
GRANT ALL ON FUNCTION app_security.service_is(service_name text) TO chat_relay;
GRANT ALL ON FUNCTION app_security.service_is(service_name text) TO chat_media_worker;
GRANT ALL ON FUNCTION app_security.service_is(service_name text) TO chat_invariant_definer;


--
-- Name: FUNCTION service_principal_is(service_name text); Type: ACL; Schema: app_security; Owner: -
--

REVOKE ALL ON FUNCTION app_security.service_principal_is(service_name text) FROM PUBLIC;
GRANT ALL ON FUNCTION app_security.service_principal_is(service_name text) TO account_runtime;
GRANT ALL ON FUNCTION app_security.service_principal_is(service_name text) TO catalog_runtime;
GRANT ALL ON FUNCTION app_security.service_principal_is(service_name text) TO scheduling_runtime;
GRANT ALL ON FUNCTION app_security.service_principal_is(service_name text) TO credit_runtime;
GRANT ALL ON FUNCTION app_security.service_principal_is(service_name text) TO orders_runtime;
GRANT ALL ON FUNCTION app_security.service_principal_is(service_name text) TO payment_runtime;
GRANT ALL ON FUNCTION app_security.service_principal_is(service_name text) TO payroll_runtime;
GRANT ALL ON FUNCTION app_security.service_principal_is(service_name text) TO recommendation_runtime;
GRANT ALL ON FUNCTION app_security.service_principal_is(service_name text) TO outbox_worker;
GRANT ALL ON FUNCTION app_security.service_principal_is(service_name text) TO projection_worker;
GRANT ALL ON FUNCTION app_security.service_principal_is(service_name text) TO chat_runtime;
GRANT ALL ON FUNCTION app_security.service_principal_is(service_name text) TO chat_relay;
GRANT ALL ON FUNCTION app_security.service_principal_is(service_name text) TO chat_media_worker;
-- chat.next_message_sequence / chat.refresh_member_count run as this SECURITY DEFINER owner
-- and evaluate the same tenant predicates, so it needs the functions PUBLIC may not call.
GRANT EXECUTE ON FUNCTION app_security.service_principal_is(service_name text) TO chat_invariant_definer;


--
-- Name: FUNCTION setting(name text); Type: ACL; Schema: app_security; Owner: -
--

GRANT ALL ON FUNCTION app_security.setting(name text) TO account_runtime;
GRANT ALL ON FUNCTION app_security.setting(name text) TO catalog_runtime;
GRANT ALL ON FUNCTION app_security.setting(name text) TO scheduling_runtime;
GRANT ALL ON FUNCTION app_security.setting(name text) TO credit_runtime;
GRANT ALL ON FUNCTION app_security.setting(name text) TO orders_runtime;
GRANT ALL ON FUNCTION app_security.setting(name text) TO payment_runtime;
GRANT ALL ON FUNCTION app_security.setting(name text) TO payroll_runtime;
GRANT ALL ON FUNCTION app_security.setting(name text) TO outbox_worker;
GRANT ALL ON FUNCTION app_security.setting(name text) TO projection_worker;
GRANT ALL ON FUNCTION app_security.setting(name text) TO recommendation_runtime;
GRANT ALL ON FUNCTION app_security.setting(name text) TO chat_runtime;
GRANT ALL ON FUNCTION app_security.setting(name text) TO chat_relay;
GRANT ALL ON FUNCTION app_security.setting(name text) TO chat_media_worker;
GRANT ALL ON FUNCTION app_security.setting(name text) TO chat_invariant_definer;


--
-- Name: FUNCTION studio_id(); Type: ACL; Schema: app_security; Owner: -
--

GRANT ALL ON FUNCTION app_security.studio_id() TO account_runtime;
GRANT ALL ON FUNCTION app_security.studio_id() TO catalog_runtime;
GRANT ALL ON FUNCTION app_security.studio_id() TO scheduling_runtime;
GRANT ALL ON FUNCTION app_security.studio_id() TO credit_runtime;
GRANT ALL ON FUNCTION app_security.studio_id() TO orders_runtime;
GRANT ALL ON FUNCTION app_security.studio_id() TO payment_runtime;
GRANT ALL ON FUNCTION app_security.studio_id() TO payroll_runtime;
GRANT ALL ON FUNCTION app_security.studio_id() TO outbox_worker;
GRANT ALL ON FUNCTION app_security.studio_id() TO projection_worker;
GRANT ALL ON FUNCTION app_security.studio_id() TO recommendation_runtime;
GRANT ALL ON FUNCTION app_security.studio_id() TO chat_runtime;
GRANT ALL ON FUNCTION app_security.studio_id() TO chat_relay;
GRANT ALL ON FUNCTION app_security.studio_id() TO chat_media_worker;
GRANT ALL ON FUNCTION app_security.studio_id() TO chat_invariant_definer;


--
-- Name: FUNCTION user_id(); Type: ACL; Schema: app_security; Owner: -
--

GRANT ALL ON FUNCTION app_security.user_id() TO account_runtime;
GRANT ALL ON FUNCTION app_security.user_id() TO catalog_runtime;
GRANT ALL ON FUNCTION app_security.user_id() TO scheduling_runtime;
GRANT ALL ON FUNCTION app_security.user_id() TO credit_runtime;
GRANT ALL ON FUNCTION app_security.user_id() TO orders_runtime;
GRANT ALL ON FUNCTION app_security.user_id() TO payment_runtime;
GRANT ALL ON FUNCTION app_security.user_id() TO payroll_runtime;
GRANT ALL ON FUNCTION app_security.user_id() TO outbox_worker;
GRANT ALL ON FUNCTION app_security.user_id() TO projection_worker;
GRANT ALL ON FUNCTION app_security.user_id() TO recommendation_runtime;
GRANT ALL ON FUNCTION app_security.user_id() TO chat_runtime;
GRANT ALL ON FUNCTION app_security.user_id() TO chat_relay;
GRANT ALL ON FUNCTION app_security.user_id() TO chat_media_worker;
GRANT ALL ON FUNCTION app_security.user_id() TO chat_invariant_definer;


--
-- Name: FUNCTION next_message_sequence(requested_conversation uuid); Type: ACL; Schema: chat; Owner: -
--

REVOKE ALL ON FUNCTION chat.next_message_sequence(requested_conversation uuid) FROM PUBLIC;
GRANT ALL ON FUNCTION chat.next_message_sequence(requested_conversation uuid) TO chat_runtime;


--
-- Name: FUNCTION refresh_member_count(requested_conversation uuid); Type: ACL; Schema: chat; Owner: -
--

REVOKE ALL ON FUNCTION chat.refresh_member_count(requested_conversation uuid) FROM PUBLIC;
GRANT ALL ON FUNCTION chat.refresh_member_count(requested_conversation uuid) TO chat_runtime;


--
-- Name: TABLE global_role_assignments; Type: ACL; Schema: account; Owner: -
--

GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE account.global_role_assignments TO account_runtime;


--
-- Name: TABLE global_role_audit; Type: ACL; Schema: account; Owner: -
--

GRANT SELECT,INSERT ON TABLE account.global_role_audit TO account_runtime;


--
-- Name: TABLE inbox_events; Type: ACL; Schema: account; Owner: -
--

GRANT SELECT,INSERT,UPDATE ON TABLE account.inbox_events TO account_runtime;


--
-- Name: TABLE invitations; Type: ACL; Schema: account; Owner: -
--

GRANT SELECT,INSERT,UPDATE ON TABLE account.invitations TO account_runtime;


--
-- Name: TABLE studio_memberships; Type: ACL; Schema: account; Owner: -
--

GRANT SELECT,INSERT,UPDATE ON TABLE account.studio_memberships TO account_runtime;
GRANT SELECT ON TABLE account.studio_memberships TO account_chat_definer;


--
-- Name: TABLE teacher_profiles; Type: ACL; Schema: account; Owner: -
--

GRANT SELECT,INSERT,UPDATE ON TABLE account.teacher_profiles TO account_runtime;
GRANT SELECT ON TABLE account.teacher_profiles TO account_chat_definer;


--
-- Name: TABLE users; Type: ACL; Schema: account; Owner: -
--

GRANT SELECT,INSERT,UPDATE ON TABLE account.users TO account_runtime;
GRANT SELECT ON TABLE account.users TO account_chat_definer;


--
-- Name: TABLE campaigns; Type: ACL; Schema: catalog; Owner: -
--

GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE catalog.campaigns TO catalog_runtime;


--
-- Name: TABLE credit_product_versions; Type: ACL; Schema: catalog; Owner: -
--

GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE catalog.credit_product_versions TO catalog_runtime;


--
-- Name: TABLE credit_products; Type: ACL; Schema: catalog; Owner: -
--

GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE catalog.credit_products TO catalog_runtime;


--
-- Name: TABLE inbox_events; Type: ACL; Schema: catalog; Owner: -
--

GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE catalog.inbox_events TO catalog_runtime;


--
-- Name: TABLE rooms; Type: ACL; Schema: catalog; Owner: -
--

GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE catalog.rooms TO catalog_runtime;


--
-- Name: TABLE studios; Type: ACL; Schema: catalog; Owner: -
--

GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE catalog.studios TO catalog_runtime;


--
-- Name: TABLE attachments; Type: ACL; Schema: chat; Owner: -
--

GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE chat.attachments TO chat_runtime;
GRANT SELECT,UPDATE ON TABLE chat.attachments TO chat_media_worker;


--
-- Name: TABLE conversation_participants; Type: ACL; Schema: chat; Owner: -
--

GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE chat.conversation_participants TO chat_runtime;
GRANT SELECT ON TABLE chat.conversation_participants TO chat_invariant_definer;


--
-- Name: TABLE conversations; Type: ACL; Schema: chat; Owner: -
--

GRANT SELECT,INSERT,DELETE ON TABLE chat.conversations TO chat_runtime;
GRANT SELECT ON TABLE chat.conversations TO chat_invariant_definer;


--
-- Name: COLUMN conversations.member_count; Type: ACL; Schema: chat; Owner: -
--

GRANT UPDATE(member_count) ON TABLE chat.conversations TO chat_invariant_definer;


--
-- Name: COLUMN conversations.last_sequence; Type: ACL; Schema: chat; Owner: -
--

GRANT UPDATE(last_sequence) ON TABLE chat.conversations TO chat_invariant_definer;


--
-- Name: TABLE group_bans; Type: ACL; Schema: chat; Owner: -
--

GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE chat.group_bans TO chat_runtime;


--
-- Name: TABLE inbox_events; Type: ACL; Schema: chat; Owner: -
--

GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE chat.inbox_events TO chat_runtime;


--
-- Name: TABLE media_deletion_jobs; Type: ACL; Schema: chat; Owner: -
--

GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE chat.media_deletion_jobs TO chat_runtime;
GRANT SELECT,DELETE,UPDATE ON TABLE chat.media_deletion_jobs TO chat_media_worker;


--
-- Name: TABLE message_reports; Type: ACL; Schema: chat; Owner: -
--

GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE chat.message_reports TO chat_runtime;


--
-- Name: TABLE messages; Type: ACL; Schema: chat; Owner: -
--

GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE chat.messages TO chat_runtime;


--
-- Name: TABLE moderation_audit; Type: ACL; Schema: chat; Owner: -
--

GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE chat.moderation_audit TO chat_runtime;


--
-- Name: TABLE realtime_outbox; Type: ACL; Schema: chat; Owner: -
--

GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE chat.realtime_outbox TO chat_runtime;
GRANT SELECT,UPDATE ON TABLE chat.realtime_outbox TO chat_relay;
GRANT INSERT ON TABLE chat.realtime_outbox TO chat_media_worker;


--
-- Name: TABLE user_blocks; Type: ACL; Schema: chat; Owner: -
--

GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE chat.user_blocks TO chat_runtime;


--
-- Name: TABLE grants; Type: ACL; Schema: credit; Owner: -
--

GRANT SELECT,INSERT,UPDATE ON TABLE credit.grants TO credit_runtime;


--
-- Name: TABLE hold_allocations; Type: ACL; Schema: credit; Owner: -
--

GRANT SELECT,INSERT,UPDATE ON TABLE credit.hold_allocations TO credit_runtime;


--
-- Name: TABLE holds; Type: ACL; Schema: credit; Owner: -
--

GRANT SELECT,INSERT,UPDATE ON TABLE credit.holds TO credit_runtime;


--
-- Name: TABLE inbox_events; Type: ACL; Schema: credit; Owner: -
--

GRANT SELECT,INSERT,UPDATE ON TABLE credit.inbox_events TO credit_runtime;


--
-- Name: TABLE ledger_entries; Type: ACL; Schema: credit; Owner: -
--

GRANT SELECT,INSERT,UPDATE ON TABLE credit.ledger_entries TO credit_runtime;


--
-- Name: TABLE flashsale_inventory_ledger; Type: ACL; Schema: orders; Owner: -
--

GRANT SELECT,INSERT,UPDATE ON TABLE orders.flashsale_inventory_ledger TO orders_runtime;


--
-- Name: TABLE flashsale_requests; Type: ACL; Schema: orders; Owner: -
--

GRANT SELECT,INSERT,UPDATE ON TABLE orders.flashsale_requests TO orders_runtime;


--
-- Name: TABLE inbox_events; Type: ACL; Schema: orders; Owner: -
--

GRANT SELECT,INSERT,UPDATE ON TABLE orders.inbox_events TO orders_runtime;


--
-- Name: TABLE order_lines; Type: ACL; Schema: orders; Owner: -
--

GRANT SELECT,INSERT,UPDATE ON TABLE orders.order_lines TO orders_runtime;


--
-- Name: TABLE orders; Type: ACL; Schema: orders; Owner: -
--

GRANT SELECT,INSERT,UPDATE ON TABLE orders.orders TO orders_runtime;


--
-- Name: TABLE inbox_events; Type: ACL; Schema: payment; Owner: -
--

GRANT SELECT,INSERT,UPDATE ON TABLE payment.inbox_events TO payment_runtime;


--
-- Name: TABLE outbox_events; Type: ACL; Schema: payment; Owner: -
--

GRANT SELECT,INSERT,UPDATE ON TABLE payment.outbox_events TO payment_runtime;
GRANT SELECT,UPDATE ON TABLE payment.outbox_events TO outbox_worker;


--
-- Name: TABLE payments; Type: ACL; Schema: payment; Owner: -
--

GRANT SELECT,INSERT,UPDATE ON TABLE payment.payments TO payment_runtime;


--
-- Name: TABLE webhook_events; Type: ACL; Schema: payment; Owner: -
--

GRANT SELECT,INSERT,UPDATE ON TABLE payment.webhook_events TO payment_runtime;


--
-- Name: TABLE inbox_events; Type: ACL; Schema: payroll; Owner: -
--

GRANT SELECT,INSERT,UPDATE ON TABLE payroll.inbox_events TO payroll_runtime;


--
-- Name: TABLE monthly_class_facts; Type: ACL; Schema: payroll; Owner: -
--

GRANT SELECT,INSERT,UPDATE ON TABLE payroll.monthly_class_facts TO payroll_runtime;


--
-- Name: TABLE inbox_events; Type: ACL; Schema: recommendation; Owner: -
--

GRANT SELECT,INSERT,UPDATE ON TABLE recommendation.inbox_events TO recommendation_runtime;


--
-- Name: TABLE studio_metrics; Type: ACL; Schema: recommendation; Owner: -
--

GRANT SELECT,INSERT,UPDATE ON TABLE recommendation.studio_metrics TO recommendation_runtime;


--
-- Name: TABLE attendance_audit; Type: ACL; Schema: scheduling; Owner: -
--

GRANT SELECT,INSERT,UPDATE ON TABLE scheduling.attendance_audit TO scheduling_runtime;


--
-- Name: TABLE bookings; Type: ACL; Schema: scheduling; Owner: -
--

GRANT SELECT,INSERT,UPDATE ON TABLE scheduling.bookings TO scheduling_runtime;


--
-- Name: TABLE class_sessions; Type: ACL; Schema: scheduling; Owner: -
--

GRANT SELECT,INSERT,UPDATE ON TABLE scheduling.class_sessions TO scheduling_runtime;


--
-- Name: TABLE inbox_events; Type: ACL; Schema: scheduling; Owner: -
--

GRANT SELECT,INSERT,UPDATE ON TABLE scheduling.inbox_events TO scheduling_runtime;


--
-- Name: TABLE outbox_events; Type: ACL; Schema: scheduling; Owner: -
--

GRANT SELECT,INSERT,UPDATE ON TABLE scheduling.outbox_events TO scheduling_runtime;
GRANT SELECT,UPDATE ON TABLE scheduling.outbox_events TO outbox_worker;


--
-- Name: TABLE room_reservations; Type: ACL; Schema: scheduling; Owner: -
--

GRANT SELECT,INSERT,UPDATE ON TABLE scheduling.room_reservations TO scheduling_runtime;


--
-- Name: DEFAULT PRIVILEGES FOR TABLES; Type: DEFAULT ACL; Schema: account; Owner: -
--

ALTER DEFAULT PRIVILEGES FOR ROLE dancehub_migrator IN SCHEMA account GRANT SELECT,INSERT,DELETE,UPDATE ON TABLES TO account_runtime;


--
-- Name: DEFAULT PRIVILEGES FOR TABLES; Type: DEFAULT ACL; Schema: catalog; Owner: -
--

ALTER DEFAULT PRIVILEGES FOR ROLE dancehub_migrator IN SCHEMA catalog GRANT SELECT,INSERT,DELETE,UPDATE ON TABLES TO catalog_runtime;


--
-- Name: DEFAULT PRIVILEGES FOR TABLES; Type: DEFAULT ACL; Schema: chat; Owner: -
--

ALTER DEFAULT PRIVILEGES FOR ROLE dancehub_migrator IN SCHEMA chat GRANT SELECT,INSERT,DELETE,UPDATE ON TABLES TO chat_runtime;


--
-- Name: DEFAULT PRIVILEGES FOR TABLES; Type: DEFAULT ACL; Schema: credit; Owner: -
--

ALTER DEFAULT PRIVILEGES FOR ROLE dancehub_migrator IN SCHEMA credit GRANT SELECT,INSERT,UPDATE ON TABLES TO credit_runtime;


--
-- Name: DEFAULT PRIVILEGES FOR TABLES; Type: DEFAULT ACL; Schema: orders; Owner: -
--

ALTER DEFAULT PRIVILEGES FOR ROLE dancehub_migrator IN SCHEMA orders GRANT SELECT,INSERT,UPDATE ON TABLES TO orders_runtime;


--
-- Name: DEFAULT PRIVILEGES FOR TABLES; Type: DEFAULT ACL; Schema: payment; Owner: -
--

ALTER DEFAULT PRIVILEGES FOR ROLE dancehub_migrator IN SCHEMA payment GRANT SELECT,INSERT,UPDATE ON TABLES TO payment_runtime;


--
-- Name: DEFAULT PRIVILEGES FOR TABLES; Type: DEFAULT ACL; Schema: payroll; Owner: -
--

ALTER DEFAULT PRIVILEGES FOR ROLE dancehub_migrator IN SCHEMA payroll GRANT SELECT,INSERT,UPDATE ON TABLES TO payroll_runtime;


--
-- Name: DEFAULT PRIVILEGES FOR TABLES; Type: DEFAULT ACL; Schema: recommendation; Owner: -
--

ALTER DEFAULT PRIVILEGES FOR ROLE dancehub_migrator IN SCHEMA recommendation GRANT SELECT,INSERT,UPDATE ON TABLES TO recommendation_runtime;


--
-- Name: DEFAULT PRIVILEGES FOR TABLES; Type: DEFAULT ACL; Schema: scheduling; Owner: -
--

ALTER DEFAULT PRIVILEGES FOR ROLE dancehub_migrator IN SCHEMA scheduling GRANT SELECT,INSERT,DELETE,UPDATE ON TABLES TO scheduling_runtime;

GRANT USAGE, CREATE ON SCHEMA account TO account_chat_definer;
ALTER FUNCTION account.get_chat_memberships(uuid) OWNER TO account_chat_definer;
ALTER FUNCTION account.get_chat_principal(uuid) OWNER TO account_chat_definer;
ALTER FUNCTION account.search_public_teachers(uuid, text, integer) OWNER TO account_chat_definer;
REVOKE CREATE ON SCHEMA account FROM account_chat_definer;

GRANT USAGE, CREATE ON SCHEMA chat TO chat_invariant_definer;
ALTER FUNCTION chat.next_message_sequence(uuid) OWNER TO chat_invariant_definer;
ALTER FUNCTION chat.refresh_member_count(uuid) OWNER TO chat_invariant_definer;
REVOKE CREATE ON SCHEMA chat FROM chat_invariant_definer;


--
-- PostgreSQL database dump complete
--

\unrestrict QxdVuctbvq7onrDH3aqbEdyBiCgAxmKPH4IP3uF81EnX7XYqaUwnFYZ6H5s8oiz

-- Additions on top of the dumped schema: refund orchestration, payment ownership,
-- attachment publication, payroll facts and scheduling correction rules.
ALTER TABLE orders.orders ADD COLUMN paid_at timestamptz;
-- Durable refund orchestration; ordinary order statuses remain the public state machine.
CREATE TABLE orders.refund_attempts (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), order_id uuid NOT NULL REFERENCES orders.orders(id),
 request_key text NOT NULL, decision_key text NOT NULL DEFAULT '', requested_by uuid, approved_by uuid,
 decision text NOT NULL CHECK(decision IN('REQUEST','APPROVE','REJECT')),
 kind text NOT NULL DEFAULT 'CREDIT' CHECK(kind IN('CREDIT','ROOM','UNFULFILLED')),
 phase text NOT NULL CHECK(phase IN('RESERVING','AWAITING_APPROVAL','REFUNDING','REVOKING','RELEASING','COMPLETED','REJECTED','DENIED')),
 reason text NOT NULL, attempt_count integer NOT NULL DEFAULT 0, next_attempt_at timestamptz NOT NULL DEFAULT now(),
 lease_until timestamptz, lease_token uuid, last_error text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(order_id,request_key)
);
CREATE UNIQUE INDEX one_active_refund_per_order ON orders.refund_attempts(order_id) WHERE phase NOT IN('COMPLETED','REJECTED','DENIED');
ALTER TABLE orders.refund_attempts ENABLE ROW LEVEL SECURITY;
ALTER TABLE orders.refund_attempts FORCE ROW LEVEL SECURITY;
CREATE POLICY refund_attempts_access ON orders.refund_attempts USING (app_security.service_principal_is('orderservice') OR EXISTS(SELECT 1 FROM orders.orders o WHERE o.id=order_id)) WITH CHECK (app_security.service_principal_is('orderservice') OR EXISTS(SELECT 1 FROM orders.orders o WHERE o.id=order_id));
GRANT SELECT,INSERT,UPDATE ON orders.refund_attempts TO orders_runtime;

CREATE TABLE credit.refund_attempts(id uuid PRIMARY KEY,order_id uuid NOT NULL,user_id uuid NOT NULL,requirements jsonb NOT NULL,status text NOT NULL CHECK(status IN('RESERVED','RELEASED','REVOKED')));
ALTER TABLE credit.refund_attempts ENABLE ROW LEVEL SECURITY;
ALTER TABLE credit.refund_attempts FORCE ROW LEVEL SECURITY;
CREATE POLICY credit_refund_worker ON credit.refund_attempts USING(app_security.service_principal_is('orderservice')) WITH CHECK(app_security.service_principal_is('orderservice'));
GRANT SELECT,INSERT,UPDATE ON credit.refund_attempts TO credit_runtime;
ALTER TABLE credit.grants ADD COLUMN refund_attempt_id uuid REFERENCES credit.refund_attempts(id);

CREATE TABLE credit.booking_cancellations(booking_id uuid PRIMARY KEY,user_id uuid NOT NULL,studio_id uuid NOT NULL,created_at timestamptz NOT NULL DEFAULT now());
ALTER TABLE credit.booking_cancellations ENABLE ROW LEVEL SECURITY;
ALTER TABLE credit.booking_cancellations FORCE ROW LEVEL SECURITY;
CREATE POLICY booking_cancellations_read ON credit.booking_cancellations FOR SELECT USING(app_security.owns_user(user_id) OR app_security.service_principal_is('scheduling-worker'));
CREATE POLICY booking_cancellations_write ON credit.booking_cancellations FOR INSERT WITH CHECK(app_security.service_principal_is('scheduling-worker'));
GRANT SELECT,INSERT ON credit.booking_cancellations TO credit_runtime;
-- Internal callers provide explicit owner scope, without manufacturing human admin roles.
CREATE POLICY grants_internal_worker ON credit.grants TO credit_runtime USING(app_security.service_principal_is('orderservice') OR app_security.service_principal_is('scheduling-worker')) WITH CHECK(app_security.service_principal_is('orderservice') OR app_security.service_principal_is('scheduling-worker'));
CREATE POLICY holds_internal_worker ON credit.holds TO credit_runtime USING(app_security.service_principal_is('orderservice') OR app_security.service_principal_is('scheduling-worker')) WITH CHECK(app_security.service_principal_is('scheduling-worker'));
CREATE POLICY hold_allocations_internal_worker ON credit.hold_allocations TO credit_runtime USING(app_security.service_principal_is('orderservice') OR app_security.service_principal_is('scheduling-worker')) WITH CHECK(app_security.service_principal_is('scheduling-worker'));
CREATE POLICY ledger_internal_worker ON credit.ledger_entries TO credit_runtime USING(app_security.service_principal_is('orderservice') OR app_security.service_principal_is('scheduling-worker')) WITH CHECK(app_security.service_principal_is('orderservice') OR app_security.service_principal_is('scheduling-worker'));

ALTER TABLE scheduling.bookings ADD COLUMN credit_compensation_pending boolean NOT NULL DEFAULT false;
CREATE TABLE scheduling.credit_compensations(
 booking_id uuid PRIMARY KEY REFERENCES scheduling.bookings(id),user_id uuid NOT NULL,studio_id uuid NOT NULL,reason text NOT NULL,
 status text NOT NULL DEFAULT 'PENDING' CHECK(status IN('PENDING','DONE')),attempt_count integer NOT NULL DEFAULT 0,
 next_attempt_at timestamptz NOT NULL DEFAULT now(),lease_until timestamptz,lease_token uuid,last_error text NOT NULL DEFAULT '',created_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE scheduling.credit_compensations ENABLE ROW LEVEL SECURITY;
ALTER TABLE scheduling.credit_compensations FORCE ROW LEVEL SECURITY;
CREATE POLICY compensations_access ON scheduling.credit_compensations USING(app_security.owns_user(user_id) OR app_security.owns_studio(studio_id) OR app_security.service_principal_is('scheduling-worker')) WITH CHECK(app_security.owns_user(user_id) OR app_security.owns_studio(studio_id) OR app_security.service_principal_is('scheduling-worker'));
GRANT SELECT,INSERT,UPDATE ON scheduling.credit_compensations TO scheduling_runtime;

CREATE TABLE scheduling.room_occupancies(
 class_session_id uuid UNIQUE REFERENCES scheduling.class_sessions(id) ON DELETE CASCADE,
 room_reservation_id uuid UNIQUE REFERENCES scheduling.room_reservations(id) ON DELETE CASCADE,
 studio_id uuid NOT NULL,room_id uuid NOT NULL,starts_at timestamptz NOT NULL,ends_at timestamptz NOT NULL,
 occupied_range tstzrange GENERATED ALWAYS AS(tstzrange(starts_at,ends_at,'[)')) STORED,active boolean NOT NULL,
 CHECK(num_nonnulls(class_session_id,room_reservation_id)=1),CHECK(ends_at>starts_at),
 EXCLUDE USING gist(studio_id WITH =,room_id WITH =,occupied_range WITH &&) WHERE(active)
);
ALTER TABLE scheduling.room_occupancies ENABLE ROW LEVEL SECURITY;
ALTER TABLE scheduling.room_occupancies FORCE ROW LEVEL SECURITY;
CREATE POLICY occupancy_trigger_owner ON scheduling.room_occupancies TO dancehub_migrator USING(true) WITH CHECK(true);
REVOKE ALL ON scheduling.room_occupancies FROM scheduling_runtime;
CREATE FUNCTION scheduling.sync_room_occupancy() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog SET row_security=on AS $$
BEGIN
 IF TG_TABLE_NAME='class_sessions' THEN
  INSERT INTO scheduling.room_occupancies(class_session_id,studio_id,room_id,starts_at,ends_at,active)
  VALUES(NEW.id,NEW.studio_id,NEW.room_id,NEW.starts_at,NEW.ends_at,NEW.status::text IN('APPROVED','OPEN','MINIMUM_CONFIRMED','AWAITING_ADMIN_CONFIRMATION','COMPLETED'))
  ON CONFLICT(class_session_id) DO UPDATE SET studio_id=EXCLUDED.studio_id,room_id=EXCLUDED.room_id,starts_at=EXCLUDED.starts_at,ends_at=EXCLUDED.ends_at,active=EXCLUDED.active;
 ELSE
  INSERT INTO scheduling.room_occupancies(room_reservation_id,studio_id,room_id,starts_at,ends_at,active)
  VALUES(NEW.id,NEW.studio_id,NEW.room_id,NEW.starts_at,NEW.ends_at,NEW.status::text IN('HOLD','PENDING_PAYMENT','CONFIRMED','REFUND_PENDING'))
  ON CONFLICT(room_reservation_id) DO UPDATE SET studio_id=EXCLUDED.studio_id,room_id=EXCLUDED.room_id,starts_at=EXCLUDED.starts_at,ends_at=EXCLUDED.ends_at,active=EXCLUDED.active;
 END IF;
 RETURN NEW;
END $$;
REVOKE ALL ON FUNCTION scheduling.sync_room_occupancy() FROM PUBLIC;
CREATE TRIGGER class_room_occupancy AFTER INSERT OR UPDATE OF status,studio_id,room_id,starts_at,ends_at ON scheduling.class_sessions FOR EACH ROW EXECUTE FUNCTION scheduling.sync_room_occupancy();
CREATE TRIGGER rental_room_occupancy AFTER INSERT OR UPDATE OF status,studio_id,room_id,starts_at,ends_at ON scheduling.room_reservations FOR EACH ROW EXECUTE FUNCTION scheduling.sync_room_occupancy();
ALTER TABLE scheduling.class_sessions DROP CONSTRAINT class_sessions_studio_id_room_id_occupied_range_excl;
ALTER TABLE scheduling.room_reservations DROP CONSTRAINT room_reservations_studio_id_room_id_occupied_range_excl;

CREATE POLICY room_orders_worker ON scheduling.room_reservations TO scheduling_runtime USING(app_security.service_principal_is('orderservice')) WITH CHECK(app_security.service_principal_is('orderservice'));

CREATE UNIQUE INDEX one_order_per_room_reservation ON orders.order_lines(room_reservation_id) WHERE room_reservation_id IS NOT NULL;

-- Payments carry their owner and expiry so ownership checks do not depend on the order row.
ALTER TABLE payment.payments ADD COLUMN owner_user_id uuid NOT NULL;
ALTER TABLE payment.payments ADD COLUMN payment_expires_at timestamptz NOT NULL;
ALTER TABLE payment.payments ADD COLUMN succeeded_at timestamptz;
DROP POLICY payments_service_access ON payment.payments;
CREATE POLICY payments_service_access ON payment.payments
USING ((app_security.setting('app.actor_kind')='human' AND owner_user_id=app_security.user_id())
 OR app_security.service_principal_is('paymentservice') OR app_security.service_principal_is('orderservice'))
WITH CHECK ((app_security.setting('app.actor_kind')='human' AND owner_user_id=app_security.user_id())
 OR app_security.service_principal_is('paymentservice') OR app_security.service_principal_is('orderservice'));

-- Only a scanned candidate copy may be published; downloads never use the upload key.
ALTER TABLE chat.attachments ADD COLUMN clean_object_key text;
ALTER TABLE chat.attachments ADD CONSTRAINT attachments_clean_key_check
  CHECK (status <> 'CLEAN' OR clean_object_key IS NOT NULL);
CREATE UNIQUE INDEX chat_attachment_clean_key_unique ON chat.attachments(clean_object_key)
  WHERE clean_object_key IS NOT NULL;
ALTER TABLE chat.media_deletion_jobs ADD COLUMN not_before timestamptz NOT NULL DEFAULT now();
CREATE INDEX chat_media_deletion_due_idx ON chat.media_deletion_jobs(not_before);
GRANT INSERT ON chat.media_deletion_jobs TO chat_media_worker;

GRANT SELECT,INSERT ON credit.grant_operations TO credit_runtime;
DROP POLICY grant_operations_isolation ON credit.grant_operations;
CREATE POLICY grant_operations_isolation ON credit.grant_operations
USING (
  app_security.platform_admin() OR source_user_id=app_security.user_id()
  OR target_user_id=app_security.user_id()
  OR (studio_id IS NOT NULL AND app_security.owns_studio(studio_id))
)
WITH CHECK (
  app_security.platform_admin()
  OR (studio_id IS NOT NULL AND app_security.owns_studio(studio_id))
);

-- Payroll facts are written only by the local consumer identity. Human reads
-- continue to use payroll_isolation and never acquire projector privileges.
ALTER TABLE payroll.inbox_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE payroll.inbox_events FORCE ROW LEVEL SECURITY;
CREATE POLICY payroll_inbox_projector ON payroll.inbox_events TO payroll_runtime
USING (app_security.service_principal_is('payroll-projector'))
WITH CHECK (app_security.service_principal_is('payroll-projector'));
CREATE POLICY payroll_facts_projector ON payroll.monthly_class_facts TO payroll_runtime
USING (app_security.service_principal_is('payroll-projector'))
WITH CHECK (app_security.service_principal_is('payroll-projector'));

-- Scheduling corrections change the effective redemption count. They cannot
-- move a completed class to another teacher, tenant, month, timezone or duration.
-- Equal versions must describe the same fact; stale versions cannot overwrite it.
CREATE FUNCTION payroll.guard_fact_update() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF ROW(NEW.class_session_id,NEW.studio_id,NEW.teacher_id,NEW.local_month,
         NEW.approved_duration_minutes,NEW.studio_timezone)
     IS DISTINCT FROM
     ROW(OLD.class_session_id,OLD.studio_id,OLD.teacher_id,OLD.local_month,
         OLD.approved_duration_minutes,OLD.studio_timezone) THEN
    RAISE EXCEPTION 'completed payroll class identity is immutable' USING ERRCODE='23514';
  END IF;
  IF NEW.source_version<OLD.source_version OR
     (NEW.source_version=OLD.source_version AND
      ROW(NEW.effective_redemption_count,NEW.completed) IS DISTINCT FROM
      ROW(OLD.effective_redemption_count,OLD.completed)) THEN
    RAISE EXCEPTION 'payroll fact version conflicts with the stored fact' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER payroll_fact_update BEFORE UPDATE ON payroll.monthly_class_facts
FOR EACH ROW EXECUTE FUNCTION payroll.guard_fact_update();

CREATE TABLE scheduling.attendance_operations (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  studio_id uuid NOT NULL,
  class_session_id uuid NOT NULL REFERENCES scheduling.class_sessions(id),
  booking_id uuid NOT NULL REFERENCES scheduling.bookings(id),
  student_id uuid NOT NULL,
  actor_id uuid NOT NULL,
  action text NOT NULL CHECK (action IN ('ADD_WALK_IN','REMOVE_REDEMPTION','ATTEND','NO_SHOW')),
  idempotency_key text NOT NULL,
  reason text NOT NULL,
  phase text NOT NULL DEFAULT 'PENDING' CHECK (phase IN ('PENDING','DONE','FAILED')),
  attempt_count integer NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
  next_attempt_at timestamptz NOT NULL DEFAULT now(),
  lease_until timestamptz,
  lease_token uuid,
  last_error text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (studio_id,idempotency_key)
);
CREATE UNIQUE INDEX attendance_operation_pending_idx ON scheduling.attendance_operations(booking_id) WHERE phase='PENDING';
CREATE INDEX attendance_operation_retry_idx ON scheduling.attendance_operations(next_attempt_at,id) WHERE phase='PENDING';
ALTER TABLE scheduling.attendance_operations ENABLE ROW LEVEL SECURITY;
ALTER TABLE scheduling.attendance_operations FORCE ROW LEVEL SECURITY;
CREATE POLICY attendance_operations_access ON scheduling.attendance_operations TO scheduling_runtime
USING (actor_id=app_security.user_id() OR app_security.owns_studio(studio_id) OR app_security.service_principal_is('scheduling-worker'))
WITH CHECK (actor_id=app_security.user_id() OR app_security.owns_studio(studio_id) OR app_security.service_principal_is('scheduling-worker'));
GRANT SELECT,INSERT,UPDATE ON scheduling.attendance_operations TO scheduling_runtime;
CREATE POLICY attendance_audit_worker ON scheduling.attendance_audit TO scheduling_runtime
USING (app_security.service_principal_is('scheduling-worker'))
WITH CHECK (app_security.service_principal_is('scheduling-worker'));

CREATE INDEX class_sessions_studio_page_idx ON scheduling.class_sessions(studio_id,starts_at,id);
CREATE INDEX class_sessions_teacher_page_idx ON scheduling.class_sessions(teacher_id,starts_at,id);
CREATE INDEX bookings_student_page_idx ON scheduling.bookings(student_id,created_at DESC,id DESC);
CREATE INDEX bookings_session_page_idx ON scheduling.bookings(class_session_id,created_at,id);
