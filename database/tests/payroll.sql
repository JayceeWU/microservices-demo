-- Read-model writes require the projector; browser identities retain read scope.
BEGIN;
SET LOCAL ROLE payroll_runtime;
SELECT set_config('app.actor_kind','service',true),
       set_config('app.service_principal','payroll-projector',true),
       set_config('app.user_id','',true),set_config('app.studio_id','',true),
       set_config('app.tenant_roles','',true),set_config('app.global_roles','',true);

INSERT INTO payroll.inbox_events(event_id,consumer)
VALUES('99000000-0000-0000-0000-000000000001','payroll-projector-v1');
INSERT INTO payroll.monthly_class_facts(class_session_id,studio_id,teacher_id,local_month,
    approved_duration_minutes,effective_redemption_count,completed,source_version,studio_timezone)
VALUES('99000000-0000-0000-0000-000000000002','10000000-0000-0000-0000-000000000001',
    '30000000-0000-0000-0000-000000000002','2026-09-01',60,2,true,1,'America/Los_Angeles');

-- A higher revision can change the count, but cannot change the frozen class
-- identity. Equal revisions are accepted only when they carry the same facts.
UPDATE payroll.monthly_class_facts SET effective_redemption_count=3,source_version=2
WHERE class_session_id='99000000-0000-0000-0000-000000000002';
UPDATE payroll.monthly_class_facts SET effective_redemption_count=3,source_version=2
WHERE class_session_id='99000000-0000-0000-0000-000000000002';
DO $$
DECLARE field_name text; changed_value text;
BEGIN
  BEGIN
    UPDATE payroll.monthly_class_facts SET effective_redemption_count=2,source_version=1
    WHERE class_session_id='99000000-0000-0000-0000-000000000002';
    RAISE EXCEPTION 'old version overwrote newer payroll';
  EXCEPTION WHEN check_violation THEN NULL;
  END;
  BEGIN
    UPDATE payroll.monthly_class_facts SET effective_redemption_count=4,source_version=2
    WHERE class_session_id='99000000-0000-0000-0000-000000000002';
    RAISE EXCEPTION 'same version changed payroll facts';
  EXCEPTION WHEN check_violation THEN NULL;
  END;
  FOR field_name,changed_value IN SELECT * FROM (VALUES
    ('studio_id','10000000-0000-0000-0000-000000000002'),
    ('teacher_id','30000000-0000-0000-0000-000000000003'),
    ('local_month','2026-10-01'),
    ('studio_timezone','America/New_York'),
    ('approved_duration_minutes','75')
  ) AS changes(field_name,changed_value) LOOP
    BEGIN
      EXECUTE format('UPDATE payroll.monthly_class_facts SET %I=%L,source_version=3 WHERE class_session_id=%L',
        field_name,changed_value,'99000000-0000-0000-0000-000000000002');
      RAISE EXCEPTION 'new version changed frozen payroll field %',field_name;
    EXCEPTION WHEN check_violation THEN NULL;
    END;
  END LOOP;
  IF NOT EXISTS(SELECT 1 FROM payroll.monthly_class_facts
    WHERE class_session_id='99000000-0000-0000-0000-000000000002' AND source_version=2 AND effective_redemption_count=3) THEN
    RAISE EXCEPTION 'rejected writes changed the current payroll fact';
  END IF;
END $$;

INSERT INTO payroll.monthly_class_facts(class_session_id,studio_id,teacher_id,local_month,
    approved_duration_minutes,effective_redemption_count,completed,source_version,studio_timezone)
VALUES
 ('99000000-0000-0000-0000-000000000004','10000000-0000-0000-0000-000000000001',
  '30000000-0000-0000-0000-000000000004','2026-09-01',60,1,true,1,'America/Los_Angeles'),
 ('99000000-0000-0000-0000-000000000005','10000000-0000-0000-0000-000000000002',
  '30000000-0000-0000-0000-000000000002','2026-09-01',60,1,true,1,'America/Los_Angeles');

SELECT set_config('app.actor_kind','human',true),set_config('app.service_principal','',true),
       set_config('app.user_id','30000000-0000-0000-0000-000000000002',true),
       set_config('app.studio_id','10000000-0000-0000-0000-000000000001',true),
       set_config('app.tenant_roles','teacher',true);
DO $$
BEGIN
  IF (SELECT count(*) FROM payroll.monthly_class_facts WHERE class_session_id='99000000-0000-0000-0000-000000000002')<>1 THEN
    RAISE EXCEPTION 'teacher cannot read own payroll';
  END IF;
  IF (SELECT count(*) FROM payroll.inbox_events)<>0 THEN
    RAISE EXCEPTION 'human identity can read consumer inbox';
  END IF;
  IF EXISTS(SELECT 1 FROM payroll.monthly_class_facts WHERE class_session_id IN
    ('99000000-0000-0000-0000-000000000004','99000000-0000-0000-0000-000000000005')) THEN
    RAISE EXCEPTION 'teacher read another teacher or an unselected studio';
  END IF;
  UPDATE payroll.monthly_class_facts SET effective_redemption_count=99
  WHERE class_session_id='99000000-0000-0000-0000-000000000002';
  IF FOUND THEN RAISE EXCEPTION 'teacher modified projected payroll'; END IF;
END $$;

SELECT set_config('app.user_id','30000000-0000-0000-0000-000000000003',true),
       set_config('app.tenant_roles','studio_admin',true);
DO $$
BEGIN
  IF (SELECT count(*) FROM payroll.monthly_class_facts WHERE class_session_id='99000000-0000-0000-0000-000000000002')<>1 THEN
    RAISE EXCEPTION 'administrator cannot read studio payroll';
  END IF;
  IF (SELECT count(*) FROM payroll.monthly_class_facts WHERE class_session_id='99000000-0000-0000-0000-000000000004')<>1 THEN
    RAISE EXCEPTION 'administrator cannot read another teacher in the selected studio';
  END IF;
  IF EXISTS(SELECT 1 FROM payroll.monthly_class_facts WHERE class_session_id='99000000-0000-0000-0000-000000000005') THEN
    RAISE EXCEPTION 'administrator read an unselected studio';
  END IF;
  UPDATE payroll.monthly_class_facts SET effective_redemption_count=99
  WHERE class_session_id='99000000-0000-0000-0000-000000000002';
  IF FOUND THEN RAISE EXCEPTION 'administrator bypassed payroll projection'; END IF;
END $$;

-- A former teacher's user ID alone must not grant payroll access.
SELECT set_config('app.user_id','30000000-0000-0000-0000-000000000002',true),
       set_config('app.tenant_roles','student',true);
DO $$
BEGIN
  IF EXISTS(SELECT 1 FROM payroll.monthly_class_facts WHERE class_session_id='99000000-0000-0000-0000-000000000002') THEN
    RAISE EXCEPTION 'student can read payroll';
  END IF;
END $$;

SELECT set_config('app.global_roles','platform_admin',true),set_config('app.studio_id','',true);
DO $$
BEGIN
  IF (SELECT count(*) FROM payroll.monthly_class_facts WHERE class_session_id IN
    ('99000000-0000-0000-0000-000000000002','99000000-0000-0000-0000-000000000004','99000000-0000-0000-0000-000000000005'))<>3 THEN
    RAISE EXCEPTION 'platform administrator cannot read all studios';
  END IF;
  UPDATE payroll.monthly_class_facts SET source_version=9,effective_redemption_count=99
  WHERE class_session_id='99000000-0000-0000-0000-000000000002';
  IF FOUND THEN RAISE EXCEPTION 'platform administrator bypassed payroll projection'; END IF;
END $$;

SELECT set_config('app.actor_kind','service',true),set_config('app.service_principal','outbox-relay',true),
       set_config('app.user_id','30000000-0000-0000-0000-000000000002',true),
       set_config('app.studio_id','10000000-0000-0000-0000-000000000001',true),
       set_config('app.tenant_roles','studio_admin',true),
       set_config('app.global_roles','',true);
DO $$
BEGIN
  IF EXISTS(SELECT 1 FROM payroll.monthly_class_facts WHERE class_session_id='99000000-0000-0000-0000-000000000002') THEN
    RAISE EXCEPTION 'unrelated service acquired human payroll scope';
  END IF;
  BEGIN
    INSERT INTO payroll.inbox_events(event_id,consumer)
    VALUES('99000000-0000-0000-0000-000000000003','forbidden');
    RAISE EXCEPTION 'unrelated service wrote payroll inbox';
  EXCEPTION WHEN insufficient_privilege THEN NULL;
  END;
END $$;

-- Roster reads must include another student's booking only for the assigned
-- teacher. No human teacher receives direct booking mutation privileges.
SET LOCAL ROLE scheduling_runtime;
SELECT set_config('app.actor_kind','service',true),set_config('app.service_principal','scheduling-worker',true),
       set_config('app.user_id','',true),set_config('app.studio_id','',true),
       set_config('app.tenant_roles','',true),set_config('app.global_roles','',true);
INSERT INTO scheduling.class_sessions(id,studio_id,room_id,teacher_id,title,starts_at,ends_at,
    capacity,minimum_students,credit_cost,status,cancellation_cutoff_at,attendance_teacher_deadline,attendance_admin_deadline,studio_timezone)
VALUES
 ('99000000-0000-0000-0000-000000000006','10000000-0000-0000-0000-000000000001',
  '99000000-0000-0000-0000-000000000016','30000000-0000-0000-0000-000000000002','Teacher roster RLS',
  '2026-10-01T18:00:00Z','2026-10-01T19:00:00Z',20,1,4,'PENDING_APPROVAL',
  '2026-10-01T14:00:00Z','2026-10-01T23:00:00Z','2026-10-02T19:00:00Z','America/Los_Angeles'),
 ('99000000-0000-0000-0000-000000000007','10000000-0000-0000-0000-000000000001',
  '99000000-0000-0000-0000-000000000017','30000000-0000-0000-0000-000000000004','Another teacher roster RLS',
  '2026-10-01T18:00:00Z','2026-10-01T19:00:00Z',20,1,4,'PENDING_APPROVAL',
  '2026-10-01T14:00:00Z','2026-10-01T23:00:00Z','2026-10-02T19:00:00Z','America/Los_Angeles');
INSERT INTO scheduling.bookings(id,studio_id,class_session_id,student_id,status,idempotency_key)
VALUES
 ('99000000-0000-0000-0000-000000000008','10000000-0000-0000-0000-000000000001',
  '99000000-0000-0000-0000-000000000006','30000000-0000-0000-0000-000000000001','CHARGED','rls-own-roster'),
 ('99000000-0000-0000-0000-000000000009','10000000-0000-0000-0000-000000000001',
  '99000000-0000-0000-0000-000000000007','30000000-0000-0000-0000-000000000001','CHARGED','rls-other-roster');
SELECT set_config('app.actor_kind','human',true),set_config('app.service_principal','',true),
       set_config('app.user_id','30000000-0000-0000-0000-000000000002',true),
       set_config('app.studio_id','10000000-0000-0000-0000-000000000001',true),
       set_config('app.tenant_roles','teacher',true);
DO $$
BEGIN
  IF (SELECT count(*) FROM scheduling.bookings WHERE id='99000000-0000-0000-0000-000000000008')<>1 THEN
    RAISE EXCEPTION 'teacher cannot read another student in own class';
  END IF;
  IF EXISTS(SELECT 1 FROM scheduling.bookings WHERE id='99000000-0000-0000-0000-000000000009') THEN
    RAISE EXCEPTION 'teacher read another teacher roster';
  END IF;
  UPDATE scheduling.bookings SET status='NO_SHOW' WHERE id='99000000-0000-0000-0000-000000000008';
  IF FOUND THEN RAISE EXCEPTION 'teacher bypassed audited attendance commands'; END IF;
END $$;
SELECT set_config('app.tenant_roles','student',true);
DO $$
BEGIN
  IF EXISTS(SELECT 1 FROM scheduling.bookings WHERE id='99000000-0000-0000-0000-000000000008') THEN
    RAISE EXCEPTION 'former teacher retained roster access without teacher role';
  END IF;
END $$;
ROLLBACK;
