"""Fresh-environment regressions for payroll, attendance recovery and pagination.

Run through scripts/verify-local.mjs; this script deliberately creates fixtures
and injects database failures only in the marked disposable acceptance target.
"""
import concurrent.futures
from datetime import datetime, timedelta, timezone
import json
import os
from pathlib import Path
import subprocess
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid

from payments import (API, PROJECT, STUDIO, login, require_isolated_environment,
                      service_exec, sql)

STUDENT = "30000000-0000-0000-0000-000000000001"
TEACHER = "30000000-0000-0000-0000-000000000002"
ADMIN = "30000000-0000-0000-0000-000000000003"


def uid():
    return str(uuid.uuid4())


def audit(key=None):
    return {"idempotencyKey": key or uid(), "reason": "Disposable acceptance regression"}


def call(path, token, data=None, method=None, expected=(200, 201), studio=STUDIO):
    headers = {"Authorization": "Bearer " + token, "Content-Type": "application/json", "X-Studio-Id": studio}
    request = urllib.request.Request(API + path, None if data is None else json.dumps(data).encode(), headers,
                                     method=method or ("GET" if data is None else "POST"))
    try:
        response = urllib.request.urlopen(request, timeout=25)
    except urllib.error.HTTPError as error:
        response = error
    value = json.loads(response.read())
    assert response.status in expected, (path, response.status, value)
    return value


def eventually(read, accept, seconds=120):
    deadline = time.monotonic() + seconds
    while True:
        value = read()
        if accept(value):
            return value
        if time.monotonic() >= deadline:
            raise AssertionError(("Timed out waiting for recovery", value))
        time.sleep(0.7)


def query_json(statement):
    value = sql(statement)
    return json.loads(value.splitlines()[-1]) if value else None


def buy_credits(token):
    products = call("/v1/credit-products", token)["products"]
    product = next(p for p in products if p["issuerScope"] == "PLATFORM" and p.get("creditAmount") == 200)
    order = call("/v1/orders", token, {"items": [{"productVersionId": product["id"], "quantity": 1}], **audit()})
    payment = call("/v1/payments", token, {"orderId": order["id"], "idempotencyKey": uid()})
    assert payment["simulationEnabled"]
    call(f"/v1/payments/{payment['id']}/simulate", token, {"outcome": "SUCCEEDED", "idempotencyKey": uid()})
    eventually(lambda: call(f"/v1/orders/{order['id']}", token), lambda v: v["status"] == "FULFILLED")


def new_class(teacher, admin, number):
    rooms = call(f"/v1/studios/{STUDIO}/rooms", teacher)["rooms"]
    start = datetime.now(timezone.utc).replace(hour=9, minute=0, second=0, microsecond=0) + timedelta(days=180 + number)
    session = call("/v1/class-sessions", teacher, {
        "studioId": STUDIO, "roomId": rooms[0]["id"], "title": f"Closeout payroll {number}",
        "description": "Disposable acceptance fixture", "startsAt": start.isoformat(),
        "endsAt": (start + timedelta(hours=1)).isoformat(), "capacity": 20, **audit()})
    call(f"/v1/class-sessions/{session['id']}/approve", admin, {"approve": True, "minimumStudents": 1, **audit()})
    assert sql(f"SELECT studio_timezone FROM scheduling.class_sessions WHERE id='{session['id']}'") == "America/Los_Angeles"
    return session["id"]


def end_class(session_id, number, status="AWAITING_ADMIN_CONFIRMATION"):
    # Advance the persisted fixture clock, keeping actual credit service calls.
    end = datetime.now(timezone.utc).replace(minute=0, second=0, microsecond=0) - timedelta(hours=2 + number * 2)
    start = end - timedelta(hours=1)
    sql(f"UPDATE scheduling.class_sessions SET starts_at='{start.isoformat()}',ends_at='{end.isoformat()}',"
        f"cancellation_cutoff_at='{(start-timedelta(hours=4)).isoformat()}',"
        f"attendance_teacher_deadline=now()+interval '2 hours',attendance_admin_deadline=now()+interval '22 hours',"
        f"status='{status}' WHERE id='{session_id}'")
    return start


def fact(session_id):
    return query_json(f"SELECT row_to_json(f) FROM payroll.monthly_class_facts f WHERE class_session_id='{session_id}'")


def await_fact(session_id, count):
    return eventually(lambda: fact(session_id), lambda f: f is not None and f["effective_redemption_count"] == count)


def booking_status(booking_id):
    return sql(f"SELECT status FROM scheduling.bookings WHERE id='{booking_id}'")


def retry_command(path, token, body, expected_status, booking_id=None, method=None):
    value = call(path, token, body, method=method, expected=(200, 201, 503))
    if booking_id is None and "id" in value:
        booking_id = value["id"]
    if booking_id is None:
        booking_id = sql(f"SELECT booking_id FROM scheduling.attendance_operations WHERE studio_id='{STUDIO}' AND idempotency_key='{body['idempotencyKey']}'")
    eventually(lambda: booking_status(booking_id), lambda state: state == expected_status)
    result = call(path, token, body, method=method)
    assert result["id"] == booking_id and result["status"] == expected_status
    return booking_id


def emit_events(events):
    # Use the existing Kafka client already installed in the relay image.
    code = """import json,sys,os
from kafka import KafkaProducer
p=KafkaProducer(bootstrap_servers=os.environ['KAFKA_BOOTSTRAP_SERVERS'],acks='all',value_serializer=lambda v:json.dumps(v).encode())
for event in json.load(sys.stdin):
 p.send('scheduling.events.v1',key=event['aggregate_id'].encode(),value=event).get(timeout=20)
p.flush();p.close()
"""
    service_exec("outbox-relay", "python", "-c", code, input=json.dumps(events))


def source_event(session_id):
    payload = query_json(f"SELECT json_build_object('event_id',event_id,'event_type',event_type,'schema_version',1,"
                         f"'producer','scheduling','tenant_id',tenant_id,'aggregate_type',aggregate_type,"
                         f"'aggregate_id',aggregate_id,'data',payload) FROM scheduling.outbox_events "
                         f"WHERE aggregate_id='{session_id}' AND event_type='ClassCompleted' "
                         "ORDER BY (payload->>'fact_version')::bigint DESC LIMIT 1")
    assert payload
    return payload


def restart_payroll():
    namespace = os.environ.get("DANCEHUB_ACCEPTANCE_NAMESPACE")
    if namespace:
        prefix = ["kubectl", "--kubeconfig", os.environ["KUBECONFIG"], "--context", os.environ["DANCEHUB_ACCEPTANCE_CONTEXT"], "-n", namespace]
        subprocess.run(prefix + ["rollout", "restart", "deployment/payrollservice"], check=True, capture_output=True)
        subprocess.run(prefix + ["rollout", "status", "deployment/payrollservice", "--timeout=180s"], check=True, capture_output=True)
    else:
        subprocess.run(["docker", "compose", "-p", PROJECT, "restart", "payrollservice"], check=True, capture_output=True)


def scale_payroll(replicas):
    namespace = os.environ.get("DANCEHUB_ACCEPTANCE_NAMESPACE")
    if namespace:
        prefix = ["kubectl", "--kubeconfig", os.environ["KUBECONFIG"], "--context", os.environ["DANCEHUB_ACCEPTANCE_CONTEXT"], "-n", namespace]
        subprocess.run(prefix + ["scale", "deployment/payrollservice", f"--replicas={replicas}"], check=True, capture_output=True)
        subprocess.run(prefix + ["rollout", "status", "deployment/payrollservice", "--timeout=180s"], check=True, capture_output=True)
    else:
        subprocess.run(["docker", "compose", "-p", PROJECT, "up", "-d", "--no-deps", "--scale", f"payrollservice={replicas}", "payrollservice"], check=True, capture_output=True)


def projection_recovery():
    # Use the relay's existing Kafka access. The broker Pod deliberately cannot
    # reach its own broker Service through NetworkPolicy; Java CLI failures can
    # also return exit 0 and must not be mistaken for an empty consumer group.
    describe_group = """import json,os
from kafka.admin import KafkaAdminClient
from kafka.protocol.admin import DescribeGroupsRequest
# kafka-python-ng 2.2.3's v3 response schema omits authorized_operations,
# while its admin converter expects it. v2 correctly supplies a placeholder.
class AcceptanceAdmin(KafkaAdminClient):
    def _matching_api_version(self, operation):
        version=super()._matching_api_version(operation)
        return min(version,2) if operation is DescribeGroupsRequest else version
client=AcceptanceAdmin(bootstrap_servers=os.environ['KAFKA_BOOTSTRAP_SERVERS'].split(','),
                       request_timeout_ms=10000,api_version_auto_timeout_ms=10000)
try:
    groups=client.describe_consumer_groups(['payroll-projector-v1'])
    assert len(groups)==1, 'Expected one payroll consumer group'
    group=groups[0]
    assert group.error_code==0, 'Kafka group description returned an error'
    print(json.dumps(dict(group=group.group,state=group.state,members=len(group.members))))
finally:
    client.close()
"""
    def members():
        value = json.loads(service_exec("outbox-relay", "python", "-c", describe_group))
        assert value["group"] == "payroll-projector-v1", value
        return value["members"] if value["state"] == "Stable" else 0

    cases = [("2026-03-01T07:30:00Z", "2026-02-01"),
             ("2026-03-08T09:30:00Z", "2026-03-01"),
             ("2026-03-08T10:30:00Z", "2026-03-01"),
             ("2026-11-01T09:30:00Z", "2026-11-01")]
    events = []
    for starts, month in cases:
        session = uid()
        events.append({"event_id": uid(), "event_type": "ClassCompleted", "schema_version": 1,
                       "producer": "scheduling", "tenant_id": STUDIO, "aggregate_type": "class_session", "aggregate_id": session,
                       "data": {"class_session_id": session, "studio_id": STUDIO, "teacher_id": TEACHER,
                                "starts_at": starts, "local_month": month, "studio_timezone": "America/Los_Angeles",
                                "approved_duration_minutes": 60, "redemption_count": 2, "fact_version": 2}})
    try:
        scale_payroll(2)
        eventually(members, lambda count: count == 2)
        records = []
        for event in events:
            older = json.loads(json.dumps(event))
            older["event_id"] = uid()
            older["data"].update(fact_version=1, redemption_count=1)
            records.extend([event, event, older])
        emit_events(records)
        for event in events:
            row = await_fact(event["aggregate_id"], 2)
            assert row["source_version"] == 2 and row["local_month"] == event["data"]["local_month"]
    finally:
        scale_payroll(1)
    eventually(members, lambda count: count == 1)

    # Reject actual writes temporarily: inbox and consumer offset must stay
    # uncommitted, including records queued behind the rejected one.
    event = json.loads(json.dumps(events[0]))
    event["event_id"] = uid()
    event["data"].update(fact_version=3, redemption_count=3)
    following = json.loads(json.dumps(event))
    following["event_id"] = uid()
    following["data"].update(fact_version=4, redemption_count=4)
    sql("CREATE SEQUENCE payroll.acceptance_projection_attempt; GRANT USAGE ON SEQUENCE payroll.acceptance_projection_attempt TO payroll_runtime; "
        "CREATE FUNCTION payroll.acceptance_reject_fact() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN "
        f"IF NEW.class_session_id='{event['aggregate_id']}' THEN PERFORM nextval('payroll.acceptance_projection_attempt'); RAISE EXCEPTION 'acceptance projection failure'; END IF; "
        "RETURN NEW; END $$; CREATE TRIGGER acceptance_reject_fact BEFORE UPDATE ON payroll.monthly_class_facts "
        "FOR EACH ROW EXECUTE FUNCTION payroll.acceptance_reject_fact()")
    try:
        emit_events([event, following])
        eventually(lambda: sql("SELECT is_called FROM payroll.acceptance_projection_attempt"), lambda value: value == "t")
        assert sql(f"SELECT count(*) FROM payroll.inbox_events WHERE event_id='{event['event_id']}'") == "0"
        assert sql(f"SELECT count(*) FROM payroll.inbox_events WHERE event_id='{following['event_id']}'") == "0"
        assert fact(event["aggregate_id"])["source_version"] == 2
    finally:
        sql("DROP TRIGGER IF EXISTS acceptance_reject_fact ON payroll.monthly_class_facts; DROP FUNCTION IF EXISTS payroll.acceptance_reject_fact(); DROP SEQUENCE IF EXISTS payroll.acceptance_projection_attempt")
    assert await_fact(event["aggregate_id"], 4)["source_version"] == 4
    assert sql(f"SELECT count(*) FROM payroll.inbox_events WHERE event_id='{event['event_id']}'") == "1"
    print("PASS real Kafka rebalance, projection failure replay, Los Angeles month boundary and DST", flush=True)


def payroll_flow(student, teacher, admin):
    session = new_class(teacher, admin, 1)
    booked = call(f"/v1/class-sessions/{session}/bookings", student, {"idempotencyKey": uid()})
    booking = booked["id"]
    end_class(session, 1)
    # The end-of-class transition must not prevent a previously frozen hold
    # from being captured. Completion cannot race past the unfinished charge.
    sql(f"UPDATE scheduling.bookings SET updated_at=now() WHERE id='{booking}'")
    call(f"/v1/class-sessions/{session}/complete", admin, audit(), expected=(409,))
    sql(f"UPDATE scheduling.bookings SET updated_at=now()-interval '1 minute' WHERE id='{booking}'")
    eventually(lambda: booking_status(booking), lambda status: status == "CHARGED")
    call(f"/v1/class-sessions/{session}/complete", admin, audit())
    initial = await_fact(session, 1)
    first_event = source_event(session)

    no_show = audit()
    retry_command(f"/v1/bookings/{booking}/attendance", teacher, {"attended": False, **no_show}, "NO_SHOW", booking, "PATCH")
    assert sql(f"SELECT actor_id FROM scheduling.attendance_audit WHERE booking_id='{booking}' AND action='NO_SHOW'") == TEACHER
    assert sql(f"SELECT status FROM credit.holds WHERE booking_id='{booking}'") == "CAPTURED"
    assert await_fact(session, 1)["effective_redemption_count"] == 1

    # Credits were genuinely purchased, but move only their test validity start
    # to match the fixture clock used to exercise already-ended classes.
    sql(f"UPDATE credit.grants SET valid_from=now()-interval '3 days' WHERE user_id IN('{STUDENT}','{TEACHER}') AND status='ACTIVE'")
    walk_body = {"studentId": TEACHER, **audit()}
    walk_booking = retry_command(f"/v1/class-sessions/{session}/walk-ins", admin, walk_body, "ATTENDED")
    latest = await_fact(session, 2)
    assert latest["source_version"] > initial["source_version"]
    reverse_body = audit()
    retry_command(f"/v1/bookings/{walk_booking}/reverse", admin, reverse_body, "REVERSED", walk_booking)
    current = await_fact(session, 1)
    assert current["source_version"] > latest["source_version"]
    assert sql(f"SELECT count(*) FROM scheduling.attendance_audit WHERE booking_id='{walk_booking}' AND action='ADD_WALK_IN'") == "1"
    assert sql(f"SELECT count(*) FROM credit.ledger_entries WHERE booking_id='{walk_booking}' AND kind='CAPTURE'") == "1"
    assert sql(f"SELECT status FROM credit.holds WHERE booking_id='{walk_booking}'") == "REVERSED"
    call(f"/v1/class-sessions/{session}/walk-ins", admin, {**walk_body, "studentId": STUDENT}, expected=(409,))

    # Re-delivery and an old fact with a new event ID cannot roll back the sum.
    newest = source_event(session)
    old = json.loads(json.dumps(first_event))
    old["event_id"] = uid()
    emit_events([newest, newest, old])
    eventually(lambda: sql(f"SELECT count(*) FROM payroll.inbox_events WHERE event_id='{old['event_id']}'"), lambda v: v == "1")
    assert fact(session)["source_version"] == current["source_version"]
    restart_payroll()
    old["event_id"] = uid()
    emit_events([old, newest])
    eventually(lambda: sql(f"SELECT count(*) FROM payroll.inbox_events WHERE event_id='{old['event_id']}'"), lambda v: v == "1")
    assert fact(session)["effective_redemption_count"] == 1

    month = current["local_month"][:7]
    for token in (admin, teacher):
        result = call(f"/v1/payroll/monthly?studio_id={STUDIO}&month={month}", token)
        line = next(line for line in result["lines"] if line["classSessionId"] == session)
        assert int(line["totalAmountCents"]) == 3400, line
    call(f"/v1/payroll/monthly?studio_id={STUDIO}&month={month}", student, expected=(403,))
    call(f"/v1/payroll/monthly?studio_id={STUDIO}&month={month}&teacher_id={STUDENT}", teacher, expected=(403,))
    other_studio = "10000000-0000-0000-0000-000000000002"
    call(f"/v1/payroll/monthly?studio_id={other_studio}&month={month}", admin, expected=(403,))
    print("PASS completed payroll, no-show, walk-in, reversal, replay/restart, tenant and teacher isolation", flush=True)
    return {"payrollSessionId": session, "payrollMonth": month, "payrollAmountCents": 3400}


def pagination_flow(student, teacher, admin):
    room, session_ids, own_ids = uid(), [uid() for _ in range(30)], [uid() for _ in range(30)]
    sql(f"INSERT INTO catalog.rooms(id,studio_id,name,capacity,rental_rate_cents_per_hour) "
        f"VALUES('{room}','{STUDIO}','Closeout pagination room',100,6000)")
    start = datetime.now(timezone.utc).replace(hour=10, minute=0, second=0, microsecond=0) + timedelta(days=500)
    statements = []
    for index, session in enumerate(session_ids):
        begins = start + timedelta(days=index)
        ends = begins + timedelta(hours=1)
        statements.append(f"INSERT INTO scheduling.class_sessions(id,studio_id,room_id,teacher_id,title,starts_at,ends_at,"
                          f"capacity,minimum_students,credit_cost,status,cancellation_cutoff_at,attendance_teacher_deadline,attendance_admin_deadline,studio_timezone) "
                          f"VALUES('{session}','{STUDIO}','{room}','{TEACHER}','Closeout pagination {index+1:02d}',"
                          f"'{begins.isoformat()}','{ends.isoformat()}',100,1,4,'OPEN','{(begins-timedelta(hours=4)).isoformat()}',"
                          f"'{(ends+timedelta(hours=4)).isoformat()}','{(ends+timedelta(hours=24)).isoformat()}','America/Los_Angeles')")
        # Identical created_at deliberately exercises the ID tie-breaker.
        statements.append(f"INSERT INTO scheduling.bookings(id,studio_id,class_session_id,student_id,status,idempotency_key,created_at) "
                          f"VALUES('{own_ids[index]}','{STUDIO}','{session}','{STUDENT}','CANCELLED','{uid()}','2026-01-01T00:00:00Z')")
    roster_ids = [own_ids[0]] + [uid() for _ in range(29)]
    for booking in roster_ids[1:]:
        statements.append(f"INSERT INTO scheduling.bookings(id,studio_id,class_session_id,student_id,status,idempotency_key,created_at) "
                          f"VALUES('{booking}','{STUDIO}','{session_ids[0]}','{uid()}','CANCELLED','{uid()}','2026-01-01T00:00:00Z')")
    service_exec("postgres", "psql", "-U", "dancehub", "-d", "dancehub", "-v", "ON_ERROR_STOP=1", "-q", input="BEGIN;\n" + ";\n".join(statements) + ";\nCOMMIT;")

    def all_pages(path, token, field):
        values, seen = [], set()
        response = call(path, token)
        assert len(response[field]) == 24, (path, len(response[field]))
        first_cursor = response.get("nextPageToken")
        assert first_cursor, path
        while True:
            values.extend(response[field])
            cursor = response.get("nextPageToken")
            if not cursor:
                break
            assert cursor not in seen, "pagination did not advance"
            seen.add(cursor)
            response = call(path + ("&" if "?" in path else "?") + urllib.parse.urlencode({"page_token": cursor}), token)
        ids = [item["id"] for item in values]
        assert len(ids) == len(set(ids)), "duplicate records across pages"
        return values, first_cursor

    sessions, cursor = all_pages(f"/v1/class-sessions?studio_id={STUDIO}&teacher_id={TEACHER}&bookable_only=true", student, "sessions")
    assert set(session_ids) <= {s["id"] for s in sessions}
    roster, roster_cursor = all_pages(f"/v1/class-sessions/{session_ids[0]}/roster", teacher, "bookings")
    assert [row["id"] for row in roster] == sorted(roster_ids)
    bookings, _ = all_pages("/v1/bookings", student, "bookings")
    own = [row["id"] for row in bookings if row["id"] in own_ids]
    assert own == sorted(own_ids, reverse=True)
    call(f"/v1/class-sessions?studio_id={STUDIO}&page_token=not-a-cursor", student, expected=(400,))
    call(f"/v1/class-sessions?studio_id={STUDIO}&teacher_id={TEACHER}&page_token=" + urllib.parse.quote(cursor), student, expected=(400,))
    call(f"/v1/class-sessions/{session_ids[1]}/roster?page_token=" + urllib.parse.quote(roster_cursor), teacher, expected=(400,))
    empty = call(f"/v1/class-sessions?teacher_id={uid()}", admin)
    assert empty["sessions"] == [] and not empty.get("nextPageToken")
    call(f"/v1/class-sessions/{session_ids[0]}/roster", student, expected=(403,))
    print("PASS courses, bookings and 30-person roster paginate with stable ties and bound cursors", flush=True)
    last_booking = min(own_ids)
    return {"paginationSessionId": session_ids[0], "paginationBookingId": last_booking,
            "paginationBookingSessionId": session_ids[own_ids.index(last_booking)]}


def final_commit_failure(student, teacher, admin):
    session = new_class(teacher, admin, 2)
    end_class(session, 2)
    # The remote capture succeeds, then a real database trigger rejects the
    # scheduling commit. Removing that failure must recover the same booking.
    function = "acceptance_reject_capture"
    sql(f"CREATE FUNCTION scheduling.{function}() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN "
        f"IF NEW.class_session_id='{session}' AND NEW.status IN('CHARGED','ATTENDED') THEN "
        "RAISE EXCEPTION 'acceptance injected final commit failure'; END IF; RETURN NEW; END $$; "
        f"CREATE TRIGGER {function} BEFORE UPDATE ON scheduling.bookings FOR EACH ROW EXECUTE FUNCTION scheduling.{function}()")
    body = {"studentId": STUDENT, **audit()}
    booking = None
    try:
        call(f"/v1/class-sessions/{session}/walk-ins", admin, body, expected=(503,))
        booking = sql(f"SELECT booking_id FROM scheduling.attendance_operations WHERE idempotency_key='{body['idempotencyKey']}'")
        assert booking and booking_status(booking) == "PENDING_CREDIT"
        assert sql(f"SELECT status FROM credit.holds WHERE booking_id='{booking}'") == "CAPTURED"
        assert sql(f"SELECT count(*) FROM scheduling.attendance_audit WHERE booking_id='{booking}'") == "0"
        call(f"/v1/class-sessions/{session}/complete", admin, audit(), expected=(409,))
    finally:
        sql(f"DROP TRIGGER IF EXISTS {function} ON scheduling.bookings; DROP FUNCTION IF EXISTS scheduling.{function}()")
    retry_command(f"/v1/class-sessions/{session}/walk-ins", admin, body, "ATTENDED", booking)
    assert sql(f"SELECT count(*) FROM credit.holds WHERE booking_id='{booking}'") == "1"
    assert sql(f"SELECT count(*) FROM credit.ledger_entries WHERE booking_id='{booking}' AND kind='CAPTURE'") == "1"
    assert sql(f"SELECT count(*) FROM scheduling.attendance_audit WHERE booking_id='{booking}' AND action='ADD_WALK_IN'") == "1"
    call(f"/v1/class-sessions/{session}/complete", admin, audit())
    await_fact(session, 1)
    print("PASS remote capture survives failed local commit without duplicate hold, capture or audit", flush=True)


def concurrent_walk_in(teacher, admin):
    session = new_class(teacher, admin, 3)
    end_class(session, 3)
    body = {"studentId": TEACHER, **audit()}
    def submit(_):
        return call(f"/v1/class-sessions/{session}/walk-ins", admin, body, expected=(200, 201, 503))
    with concurrent.futures.ThreadPoolExecutor(max_workers=5) as pool:
        list(pool.map(submit, range(5)))
    booking = sql(f"SELECT booking_id FROM scheduling.attendance_operations WHERE idempotency_key='{body['idempotencyKey']}'")
    retry_command(f"/v1/class-sessions/{session}/walk-ins", admin, body, "ATTENDED", booking)
    assert sql(f"SELECT count(*) FROM scheduling.bookings WHERE class_session_id='{session}'") == "1"
    assert sql(f"SELECT count(*) FROM credit.ledger_entries WHERE booking_id='{booking}' AND kind='CAPTURE'") == "1"
    assert sql(f"SELECT count(*) FROM scheduling.attendance_audit WHERE booking_id='{booking}'") == "1"
    # Both payload and operation-kind collisions must be rejected, even after completion.
    call(f"/v1/class-sessions/{session}/walk-ins", admin, {**body, "studentId": STUDENT}, expected=(409,))
    call(f"/v1/bookings/{booking}/reverse", admin, audit(body["idempotencyKey"]), expected=(409,))
    print("PASS concurrent walk-in retries preserve one booking, one capture and one audit", flush=True)


def capture_failure_after_hold(student, teacher, admin):
    session = new_class(teacher, admin, 4)
    end_class(session, 4, "OPEN")
    call(f"/v1/class-sessions/{session}/bookings", student, {"idempotencyKey": uid()}, expected=(409,))
    sql(f"UPDATE scheduling.class_sessions SET status='AWAITING_ADMIN_CONFIRMATION' WHERE id='{session}'")
    sql("CREATE FUNCTION credit.acceptance_reject_capture() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN "
        "IF NEW.kind='CAPTURE' THEN RAISE EXCEPTION 'acceptance capture failure'; END IF; RETURN NEW; END $$; "
        "CREATE TRIGGER acceptance_reject_capture BEFORE INSERT ON credit.ledger_entries "
        "FOR EACH ROW EXECUTE FUNCTION credit.acceptance_reject_capture()")
    body = {"studentId": STUDENT, **audit()}
    try:
        call(f"/v1/class-sessions/{session}/walk-ins", admin, body, expected=(503,))
        booking = sql(f"SELECT booking_id FROM scheduling.attendance_operations WHERE idempotency_key='{body['idempotencyKey']}'")
        assert booking
        assert sql(f"SELECT status FROM credit.holds WHERE booking_id='{booking}'") == "ACTIVE"
        # An accepted operation must finish even after the audit deadline and
        # when recovering a class already marked completed by an older run.
        sql(f"UPDATE scheduling.class_sessions SET status='COMPLETED',attendance_admin_deadline=now()-interval '1 minute' WHERE id='{session}'")
    finally:
        sql("DROP TRIGGER IF EXISTS acceptance_reject_capture ON credit.ledger_entries; DROP FUNCTION IF EXISTS credit.acceptance_reject_capture()")
    retry_command(f"/v1/class-sessions/{session}/walk-ins", admin, body, "ATTENDED", booking)
    await_fact(session, 1)
    assert sql(f"SELECT count(*) FROM credit.holds WHERE booking_id='{booking}' AND status='ACTIVE'") == "0"
    assert sql(f"SELECT count(*) FROM credit.ledger_entries WHERE booking_id='{booking}' AND kind='CAPTURE'") == "1"
    assert sql(f"SELECT count(*) FROM scheduling.attendance_audit WHERE booking_id='{booking}'") == "1"
    print("PASS ended class rejects bookings; accepted hold/capture recovers after completion and audit deadline", flush=True)


def attendance_browser_fixture(teacher, admin):
    session = new_class(teacher, admin, 5)
    end_class(session, 5)
    booking = retry_command(f"/v1/class-sessions/{session}/walk-ins", admin,
                            {"studentId": TEACHER, **audit()}, "ATTENDED")
    call(f"/v1/class-sessions/{session}/complete", admin, audit())
    projected = await_fact(session, 1)
    return {"attendanceSessionId": session, "attendanceBookingId": booking,
            "attendanceMonth": projected["local_month"][:7]}


def main():
    require_isolated_environment()
    def actors():
        # Fault recovery can span multiple worker ticks. Start each scenario
        # with real fresh OIDC tokens instead of extending the realm lifetime.
        return tuple(login(name + "@bayareadancehub.local") for name in ("student", "teacher", "admin"))

    student, teacher, admin = actors()
    buy_credits(student)
    buy_credits(teacher)
    fixtures = payroll_flow(student, teacher, admin)
    final_commit_failure(*actors())
    concurrent_walk_in(*actors()[1:])
    capture_failure_after_hold(*actors())
    student, teacher, admin = actors()
    from pagination import run as run_pagination
    run_pagination(student, teacher, admin)
    fixtures.update(pagination_flow(student, teacher, admin))
    projection_recovery()
    fixtures.update(attendance_browser_fixture(*actors()[1:]))
    output = Path(os.environ.get("DANCEHUB_ACCEPTANCE_OUTPUT_DIR", "test-results/acceptance"))
    output.mkdir(parents=True, exist_ok=True)
    (output / "browser-fixtures.json").write_text(json.dumps(fixtures), encoding="utf-8")


if __name__ == "__main__":
    main()
