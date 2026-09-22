\set ON_ERROR_STOP on

-- Run against the seeded demo DB as its test administrator. Every fixture rolls back.
BEGIN;
DO $$ BEGIN
  IF NOT has_table_privilege('credit_runtime','credit.grant_operations','SELECT')
     OR NOT has_table_privilege('credit_runtime','credit.grant_operations','INSERT') THEN
    RAISE EXCEPTION 'credit runtime lacks operation audit permissions';
  END IF;
  IF has_table_privilege('credit_runtime','credit.grant_operations','UPDATE')
     OR has_table_privilege('credit_runtime','credit.grant_operations','DELETE') THEN
    RAISE EXCEPTION 'operation audit must be append-only for credit runtime';
  END IF;
END $$;

INSERT INTO credit.grants(id,user_id,studio_id,source_order_line_id,fulfillment_key,product_version_id,kind,granted_credits,remaining_credits,valid_from)
VALUES
 ('97000000-0000-0000-0000-000000000001','30000000-0000-0000-0000-000000000001',NULL,gen_random_uuid(),'security:platform-grant',gen_random_uuid(),'CREDITS',10,10,now()),
 ('97000000-0000-0000-0000-000000000002','30000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000001',gen_random_uuid(),'security:studio-grant',gen_random_uuid(),'CREDITS',10,10,now());

SET LOCAL ROLE credit_runtime;
DO $$
DECLARE operation_name text;
BEGIN
  PERFORM set_config('app.actor_kind','human',true), set_config('app.user_id','30000000-0000-0000-0000-000000000004',true),
    set_config('app.global_roles','platform_admin',true),set_config('app.tenant_roles','',true),set_config('app.studio_id','',true);
  FOREACH operation_name IN ARRAY ARRAY['PAUSE','RESUME','TRANSFER'] LOOP
    INSERT INTO credit.grant_operations(grant_id,operation,source_user_id,studio_id,actor_id,reason,before_state,after_state,idempotency_key,request_id)
    VALUES('97000000-0000-0000-0000-000000000001',operation_name,'30000000-0000-0000-0000-000000000001',NULL,
      app_security.user_id(),'security regression','{}','{}','security:platform:'||operation_name,'security-test');
  END LOOP;
  IF (SELECT count(*) FROM credit.grant_operations WHERE request_id='security-test') <> 3 THEN
    RAISE EXCEPTION 'platform administrator cannot read NULL-studio operations';
  END IF;

  PERFORM set_config('app.user_id','30000000-0000-0000-0000-000000000003',true),set_config('app.global_roles','',true),
    set_config('app.tenant_roles','studio_admin',true),set_config('app.studio_id','10000000-0000-0000-0000-000000000001',true);
  FOREACH operation_name IN ARRAY ARRAY['PAUSE','RESUME','TRANSFER'] LOOP
    INSERT INTO credit.grant_operations(grant_id,operation,source_user_id,studio_id,actor_id,reason,before_state,after_state,idempotency_key,request_id)
    VALUES('97000000-0000-0000-0000-000000000002',operation_name,'30000000-0000-0000-0000-000000000001',app_security.studio_id(),
      app_security.user_id(),'security regression','{}','{}','security:studio:'||operation_name,'security-test');
  END LOOP;
  IF (SELECT count(*) FROM credit.grant_operations WHERE request_id='security-test') <> 3 THEN
    RAISE EXCEPTION 'studio administrator can see platform operations or cannot read own operations';
  END IF;

  PERFORM set_config('app.studio_id','10000000-0000-0000-0000-000000000002',true);
  BEGIN
    INSERT INTO credit.grant_operations(grant_id,operation,source_user_id,studio_id,actor_id,reason,before_state,after_state,idempotency_key,request_id)
    VALUES('97000000-0000-0000-0000-000000000002','PAUSE','30000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000001',
      app_security.user_id(),'denied cross studio','{}','{}','security:denied-studio','security-test');
    RAISE EXCEPTION 'cross-studio audit insert was accepted';
  EXCEPTION WHEN insufficient_privilege THEN NULL;
  END;

  PERFORM set_config('app.user_id','30000000-0000-0000-0000-000000000001',true),set_config('app.tenant_roles','student',true);
  BEGIN
    INSERT INTO credit.grant_operations(grant_id,operation,source_user_id,studio_id,actor_id,reason,before_state,after_state,idempotency_key,request_id)
    VALUES('97000000-0000-0000-0000-000000000001','PAUSE',app_security.user_id(),NULL,
      app_security.user_id(),'denied student','{}','{}','security:denied-student','security-test');
    RAISE EXCEPTION 'student audit insert was accepted';
  EXCEPTION WHEN insufficient_privilege THEN NULL;
  END;
END $$;
ROLLBACK;

BEGIN;
INSERT INTO chat.conversations(id,kind,studio_id,owner_user_id,title,member_count)
VALUES('97000000-0000-0000-0000-000000000010','STUDIO_GROUP','10000000-0000-0000-0000-000000000001','30000000-0000-0000-0000-000000000003','security fixture',1);
INSERT INTO chat.attachments(id,conversation_id,studio_id,uploader_id,kind,object_key,mime_type,size_bytes,sha256,status)
VALUES('97000000-0000-0000-0000-000000000011','97000000-0000-0000-0000-000000000010','10000000-0000-0000-0000-000000000001',
  '30000000-0000-0000-0000-000000000001','IMAGE','quarantine/security-test','image/png',3,repeat('0',64),'PENDING_SCAN');
SET LOCAL ROLE chat_media_worker;
DO $$
DECLARE changed integer;
BEGIN
  PERFORM set_config('app.actor_kind','service',true),set_config('app.service','chat-media-worker',true),set_config('app.service_principal','chat-media-worker',true),
    set_config('app.user_id','',true),set_config('app.studio_id','',true),set_config('app.tenant_roles','',true),set_config('app.global_roles','',true);
  INSERT INTO chat.media_deletion_jobs(object_key,not_before) VALUES('clean/security-winner',now()+interval '15 minutes'),('clean/security-loser',now());
  UPDATE chat.attachments SET status='CLEAN',clean_object_key='clean/security-winner'
  WHERE id='97000000-0000-0000-0000-000000000011' AND status='PENDING_SCAN' AND clean_object_key IS NULL;
  GET DIAGNOSTICS changed=ROW_COUNT;
  IF changed<>1 THEN RAISE EXCEPTION 'first scan could not publish'; END IF;
  UPDATE chat.attachments SET status='CLEAN',clean_object_key='clean/security-loser'
  WHERE id='97000000-0000-0000-0000-000000000011' AND status='PENDING_SCAN' AND clean_object_key IS NULL;
  GET DIAGNOSTICS changed=ROW_COUNT;
  IF changed<>0 THEN RAISE EXCEPTION 'duplicate scan replaced published bytes'; END IF;
  -- Even a leftover cleanup job must not select a key referenced by a CLEAN row.
  UPDATE chat.media_deletion_jobs SET not_before=now() WHERE object_key='clean/security-winner';
  IF EXISTS(SELECT 1 FROM chat.media_deletion_jobs job WHERE job.object_key='clean/security-winner' AND job.not_before<=now()
      AND NOT EXISTS(SELECT 1 FROM chat.attachments a WHERE a.status='CLEAN' AND a.clean_object_key=job.object_key)) THEN
    RAISE EXCEPTION 'cleanup can select a published snapshot';
  END IF;
  BEGIN
    UPDATE chat.attachments SET clean_object_key=NULL WHERE id='97000000-0000-0000-0000-000000000011';
    RAISE EXCEPTION 'CLEAN attachment accepted without a snapshot key';
  EXCEPTION WHEN check_violation THEN NULL;
  END;
END $$;
ROLLBACK;
