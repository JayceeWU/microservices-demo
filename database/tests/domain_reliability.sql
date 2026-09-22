\set ON_ERROR_STOP on
-- Run as the local database administrator after migrations/seed; fixtures roll back.
BEGIN;
DO $$ BEGIN
 IF has_table_privilege('scheduling_runtime','scheduling.room_occupancies','INSERT') OR
    has_table_privilege('scheduling_runtime','scheduling.room_occupancies','SELECT') THEN
  RAISE EXCEPTION 'runtime must not read or write the internal occupancy table directly';
 END IF;
 IF NOT EXISTS(SELECT 1 FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace
  WHERE n.nspname='scheduling' AND p.proname='sync_room_occupancy' AND p.prosecdef
  AND 'search_path=pg_catalog'=ANY(p.proconfig) AND 'row_security=on'=ANY(p.proconfig)) THEN
  RAISE EXCEPTION 'occupancy trigger must use fixed search_path and enabled row security';
 END IF;
END $$;

SET LOCAL ROLE scheduling_runtime;
SELECT set_config('app.user_id','30000000-0000-0000-0000-000000000003',true),
 set_config('app.studio_id','10000000-0000-0000-0000-000000000001',true),
 set_config('app.tenant_roles','studio_admin',true),set_config('app.global_roles','',true),
 set_config('app.actor_kind','human',true),set_config('app.service_principal','',true),
 set_config('app.service','schedulingservice',true);
DO $$
DECLARE
 room uuid := '90000000-0000-0000-0000-000000000001';
 studio uuid := '10000000-0000-0000-0000-000000000001';
 student uuid := '30000000-0000-0000-0000-000000000001';
 teacher uuid := '30000000-0000-0000-0000-000000000002';
 starts timestamptz := date_trunc('day',now())+interval '300 days 12 hours';
 class1 uuid := '90000000-0000-0000-0000-000000000010';
 class2 uuid := '90000000-0000-0000-0000-000000000011';
 rental uuid := '90000000-0000-0000-0000-000000000020';
BEGIN
 INSERT INTO scheduling.class_sessions(id,studio_id,room_id,teacher_id,title,starts_at,ends_at,capacity,minimum_students,credit_cost,status,cancellation_cutoff_at,attendance_teacher_deadline,attendance_admin_deadline)
 VALUES(class1,studio,room,teacher,'occupancy test',starts,starts+interval '1 hour',20,4,4,'OPEN',starts-interval '4 hours',starts+interval '5 hours',starts+interval '25 hours');
 BEGIN
  INSERT INTO scheduling.room_reservations(studio_id,room_id,student_id,starts_at,ends_at,amount_cents,status,hold_expires_at,idempotency_key)
  VALUES(studio,room,student,starts,starts+interval '1 hour',6000,'HOLD',now()+interval '15 minutes','domain-overlap');
  RAISE EXCEPTION 'rental was allowed over an open class';
 EXCEPTION WHEN exclusion_violation THEN NULL; END;
 INSERT INTO scheduling.room_reservations(studio_id,room_id,student_id,starts_at,ends_at,amount_cents,status,hold_expires_at,idempotency_key)
 VALUES(studio,room,student,starts+interval '1 hour',starts+interval '2 hours',6000,'HOLD',now()+interval '15 minutes','domain-adjacent');
 UPDATE scheduling.class_sessions SET status='CANCELLED' WHERE id=class1;
 INSERT INTO scheduling.room_reservations(id,studio_id,room_id,student_id,starts_at,ends_at,amount_cents,status,hold_expires_at,idempotency_key)
 VALUES(rental,studio,room,student,starts,starts+interval '1 hour',6000,'HOLD',now()+interval '15 minutes','domain-released-class');
 INSERT INTO scheduling.class_sessions(id,studio_id,room_id,teacher_id,title,starts_at,ends_at,capacity,minimum_students,credit_cost,status,cancellation_cutoff_at,attendance_teacher_deadline,attendance_admin_deadline)
 VALUES(class2,studio,room,teacher,'pending approval does not occupy',starts,starts+interval '1 hour',20,4,4,'PENDING_APPROVAL',starts-interval '4 hours',starts+interval '5 hours',starts+interval '25 hours');
 BEGIN
  UPDATE scheduling.class_sessions SET status='OPEN' WHERE id=class2;
  RAISE EXCEPTION 'class approval was allowed over a room hold';
 EXCEPTION WHEN exclusion_violation THEN NULL; END;
 UPDATE scheduling.room_reservations SET status='EXPIRED' WHERE id=rental;
 UPDATE scheduling.class_sessions SET status='OPEN' WHERE id=class2;
 -- One live booking per student and class; a cancelled booking stays as history and does
 -- not block booking the same class again.
 INSERT INTO scheduling.bookings(studio_id,class_session_id,student_id,status,idempotency_key)
 VALUES(studio,class2,student,'CONFIRMED','domain-booking-1');
 BEGIN
  INSERT INTO scheduling.bookings(studio_id,class_session_id,student_id,status,idempotency_key)
  VALUES(studio,class2,student,'PENDING_CREDIT','domain-booking-2');
  RAISE EXCEPTION 'a second live booking was allowed for the same student and class';
 EXCEPTION WHEN unique_violation THEN NULL; END;
 UPDATE scheduling.bookings SET status='CANCELLED' WHERE idempotency_key='domain-booking-1';
 INSERT INTO scheduling.bookings(studio_id,class_session_id,student_id,status,idempotency_key)
 VALUES(studio,class2,student,'PENDING_CREDIT','domain-booking-3');
 IF (SELECT count(*) FROM scheduling.bookings WHERE class_session_id=class2 AND student_id=student)<>2 THEN
  RAISE EXCEPTION 'rebooking after cancellation must keep the cancelled row as history';
 END IF;
END $$;

RESET ROLE;
SET LOCAL ROLE credit_runtime;
SELECT set_config('app.actor_kind','human',true),set_config('app.tenant_roles','student',true),
 set_config('app.user_id','30000000-0000-0000-0000-000000000001',true);
DO $$ BEGIN
 BEGIN
  INSERT INTO credit.booking_cancellations(booking_id,user_id,studio_id)
  VALUES('90000000-0000-0000-0000-000000000030','30000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000001');
  RAISE EXCEPTION 'human actor must not manufacture a cancellation tombstone';
 EXCEPTION WHEN insufficient_privilege THEN NULL; END;
END $$;
SELECT set_config('app.actor_kind','service',true),set_config('app.service_principal','scheduling-worker',true);
INSERT INTO credit.booking_cancellations(booking_id,user_id,studio_id)
VALUES('90000000-0000-0000-0000-000000000030','30000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000001');
SELECT set_config('app.actor_kind','human',true),set_config('app.service_principal','',true),
 set_config('app.user_id','30000000-0000-0000-0000-000000000002',true);
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM credit.booking_cancellations WHERE booking_id='90000000-0000-0000-0000-000000000030') THEN
  RAISE EXCEPTION 'another user can read cancellation tombstones';
 END IF;
END $$;

RESET ROLE;
SET LOCAL ROLE orders_runtime;
SELECT set_config('app.actor_kind','human',true),set_config('app.service_principal','',true),
 set_config('app.user_id','30000000-0000-0000-0000-000000000001',true),set_config('app.service','orderservice',true);
INSERT INTO orders.orders(id,user_id,issuer_scope,status,total_amount_cents,idempotency_key)
VALUES('90000000-0000-0000-0000-000000000040','30000000-0000-0000-0000-000000000001','PLATFORM','REFUND_PENDING',2000,'domain-refund-order');
INSERT INTO orders.refund_attempts(id,order_id,request_key,decision,phase,reason)
VALUES('90000000-0000-0000-0000-000000000041','90000000-0000-0000-0000-000000000040','domain-refund','APPROVE','RESERVING','test');
DO $$ BEGIN
 BEGIN
  INSERT INTO orders.refund_attempts(order_id,request_key,decision,phase,reason)
  VALUES('90000000-0000-0000-0000-000000000040','another-key','APPROVE','RESERVING','test');
  RAISE EXCEPTION 'two active refunds were allowed for one order';
 EXCEPTION WHEN unique_violation THEN NULL; END;
END $$;
SELECT set_config('app.user_id','30000000-0000-0000-0000-000000000002',true);
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM orders.refund_attempts WHERE id='90000000-0000-0000-0000-000000000041') THEN
  RAISE EXCEPTION 'refund task leaked to another student';
 END IF;
END $$;
SELECT set_config('app.actor_kind','service',true),set_config('app.service_principal','orderservice',true),set_config('app.user_id','',true);
DO $$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM orders.refund_attempts WHERE id='90000000-0000-0000-0000-000000000041') THEN
  RAISE EXCEPTION 'refund worker cannot recover tasks';
 END IF;
END $$;
ROLLBACK;
