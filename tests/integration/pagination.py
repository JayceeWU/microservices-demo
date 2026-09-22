"""Disposable database fixtures for course pagination and authorization.

Imported at runtime by closeout.main. Every fixture ID belongs to this invocation;
the finally block removes only those rows, leaving browser acceptance fixtures intact.
"""
import base64
from datetime import datetime, timedelta, timezone
import json
import urllib.parse
import uuid

from payments import STUDIO, request, require_isolated_environment, service_exec, sql


STUDENT = "30000000-0000-0000-0000-000000000001"
TEACHER = "30000000-0000-0000-0000-000000000002"
OTHER_STUDIO = "10000000-0000-0000-0000-000000000002"


def uid():
    return str(uuid.uuid4())


def uuid_list(values):
    return ",".join("'" + str(uuid.UUID(value)) + "'" for value in values)


def path(**query):
    return "/v1/class-sessions?" + urllib.parse.urlencode(query)


def build_fixtures(now):
    prefix = "Pagination boundary " + uuid.uuid4().hex[:12]
    same_start = now.replace(hour=12, minute=0, second=0, microsecond=0) + timedelta(days=730)
    fixtures = {"prefix": prefix, "sessions": [], "bookings": [], "tied": []}

    def course(name, status="OPEN", starts=None, studio=STUDIO, teacher=None, confirmed=0):
        value = {"id": uid(), "room": uid(), "teacher": teacher or uid(), "studio": studio,
                 "title": prefix + " " + name, "status": status, "starts": starts or same_start,
                 "confirmed": confirmed}
        fixtures["sessions"].append(value)
        return value

    def booking(session, state, student=STUDENT):
        value = {"id": uid(), "session": session["id"], "studio": session["studio"],
                 "student": student, "state": state, "key": uid()}
        fixtures["bookings"].append(value)
        return value

    # Every tied row has a different room and teacher UUID. The exclusion
    # constraints therefore remain enabled and exercise the real ORDER BY tie.
    for index in range(130):
        fixtures["tied"].append(course(f"tie {index:03d}"))
    fixtures["minimum"] = course("minimum", status="MINIMUM_CONFIRMED")
    fixtures["full"] = course("full", confirmed=20)
    fixtures["pending"] = course("pending approval", status="PENDING_APPROVAL")
    fixtures["cancelled"] = course("cancelled", status="CANCELLED")
    fixtures["completed"] = course("completed", status="COMPLETED", starts=now-timedelta(days=4))
    fixtures["awaiting"] = course("awaiting", status="AWAITING_ADMIN_CONFIRMATION", starts=now-timedelta(days=3))
    # The normal worker may advance this already-ended class to awaiting
    # confirmation. Both forms must stay out of the bookable query.
    fixtures["past"] = course("past", status="MINIMUM_CONFIRMED", starts=now-timedelta(days=2))
    fixtures["booked"] = course("already booked", confirmed=1)
    booking(fixtures["booked"], "CHARGED")
    fixtures["cancelled_booking"] = course("cancelled booking history")
    booking(fixtures["cancelled_booking"], "CANCELLED")
    fixtures["reversed_booking"] = course("reversed booking history")
    booking(fixtures["reversed_booking"], "REVERSED")
    fixtures["foreign"] = course("foreign studio", studio=OTHER_STUDIO)
    tail_teacher = uid()
    fixtures["tail_first"] = course("empty continuation first", teacher=tail_teacher, starts=same_start+timedelta(days=1))
    fixtures["tail_last"] = course("empty continuation last", teacher=tail_teacher, starts=same_start+timedelta(days=2))
    fixtures["roster"] = course("assigned teacher roster", teacher=TEACHER, starts=same_start+timedelta(days=3))
    fixtures["roster_bookings"] = [booking(fixtures["roster"], "CANCELLED", uid()) for _ in range(3)]
    fixtures["empty_roster"] = course("assigned teacher empty roster", teacher=TEACHER, starts=same_start+timedelta(days=4))
    return fixtures


def insert_fixtures(fixtures):
    statements = []
    for value in fixtures["sessions"]:
        start, end = value["starts"], value["starts"] + timedelta(hours=1)
        statements.append("INSERT INTO catalog.rooms(id,studio_id,name,capacity,rental_rate_cents_per_hour) "
                          f"VALUES('{value['room']}','{value['studio']}','{value['title']}',20,6000)")
        statements.append("INSERT INTO scheduling.class_sessions(id,studio_id,room_id,teacher_id,title,starts_at,ends_at,"
                          "capacity,minimum_students,credit_cost,confirmed_count,status,cancellation_cutoff_at,"
                          "attendance_teacher_deadline,attendance_admin_deadline,studio_timezone) "
                          f"VALUES('{value['id']}','{value['studio']}','{value['room']}','{value['teacher']}',"
                          f"'{value['title']}','{start.isoformat()}','{end.isoformat()}',20,1,4,{value['confirmed']},"
                          f"'{value['status']}','{(start-timedelta(hours=4)).isoformat()}',"
                          f"'{(end+timedelta(hours=4)).isoformat()}','{(end+timedelta(hours=24)).isoformat()}',"
                          "'America/Los_Angeles')")
    for value in fixtures["bookings"]:
        statements.append("INSERT INTO scheduling.bookings(id,studio_id,class_session_id,student_id,status,idempotency_key,created_at) "
                          f"VALUES('{value['id']}','{value['studio']}','{value['session']}','{value['student']}',"
                          f"'{value['state']}','{value['key']}','2026-01-01T00:00:00Z')")
    service_exec("postgres", "psql", "-U", "dancehub", "-d", "dancehub", "-v", "ON_ERROR_STOP=1", "-q",
                 input="BEGIN;\n" + ";\n".join(statements) + ";\nCOMMIT;")


def remove_fixtures(fixtures):
    require_isolated_environment()
    sessions = uuid_list(value["id"] for value in fixtures["sessions"])
    rooms = uuid_list(value["room"] for value in fixtures["sessions"])
    bookings = uuid_list(value["id"] for value in fixtures["bookings"])
    # Exact generated IDs, never broad date/title filters. Occupancy rows cascade
    # from their class. No real credit calls are made for these query fixtures.
    cleanup = ("BEGIN; "
               f"DELETE FROM scheduling.attendance_operations WHERE class_session_id IN ({sessions}); "
               f"DELETE FROM scheduling.credit_compensations WHERE booking_id IN ({bookings}); "
               f"DELETE FROM scheduling.attendance_audit WHERE class_session_id IN ({sessions}); "
               f"DELETE FROM scheduling.bookings WHERE class_session_id IN ({sessions}); "
               f"DELETE FROM scheduling.outbox_events WHERE aggregate_id IN ({sessions},{bookings}); "
               f"DELETE FROM scheduling.class_sessions WHERE id IN ({sessions}); "
               f"DELETE FROM catalog.rooms WHERE id IN ({rooms}); COMMIT;")
    # Passing the large ID lists through stdin also works within Windows'
    # process-command-line limit.
    service_exec("postgres", "psql", "-U", "dancehub", "-d", "dancehub", "-v", "ON_ERROR_STOP=1", "-q", input=cleanup)
    assert sql(f"SELECT count(*) FROM scheduling.class_sessions WHERE id IN ({sessions})") == "0"
    assert sql(f"SELECT count(*) FROM catalog.rooms WHERE id IN ({rooms})") == "0"


def all_pages(call, token, query, size=17):
    records, seen_cursors, first_cursor = [], set(), None
    cursor = ""
    for _ in range(100):
        response = call(path(**query, page_size=size, page_token=cursor), token)
        rows = response["sessions"]
        assert isinstance(rows, list) and len(rows) <= min(size or 24, 100)
        records.extend(rows)
        cursor = response.get("nextPageToken")
        if not cursor:
            ids = [row["id"] for row in records]
            assert len(ids) == len(set(ids)), "duplicate course across pages"
            return records, first_cursor
        assert rows and cursor not in seen_cursors, "course pagination failed to advance"
        seen_cursors.add(cursor)
        first_cursor = first_cursor or cursor
    raise AssertionError("course pagination exceeded 100 pages")


def run(student, teacher, admin):
    require_isolated_environment()
    # closeout imports this module only from main, so importing its HTTP helper
    # here creates no import-time SQL, authentication or recursive execution.
    from closeout import call

    fixtures = build_fixtures(datetime.now(timezone.utc).replace(microsecond=0))
    try:
        insert_fixtures(fixtures)
        query = {"studio_id": STUDIO, "bookable_only": "true"}
        anonymous = request(path(**query))
        assert anonymous["sessions"], "anonymous course browsing requires authentication"
        request(path(**query), token="invalid-token", expected=401)
        values, cursor = all_pages(call, student, query)
        assert cursor
        ids = [value["id"] for value in values]
        tied = {value["id"] for value in fixtures["tied"]}
        assert [value for value in ids if value in tied] == sorted(tied), "same-time UUID ordering skipped or reordered courses"
        assert len(tied) == 130
        for name in ("minimum", "cancelled_booking", "reversed_booking"):
            assert fixtures[name]["id"] in ids, ("bookable course missing", name)
        for name in ("full", "pending", "cancelled", "completed", "awaiting", "past", "booked", "foreign"):
            assert fixtures[name]["id"] not in ids, ("unbookable or foreign course exposed", name)

        all_values, _ = all_pages(call, admin, {"studio_id": STUDIO, "bookable_only": "false"}, size=100)
        expected_all = {value["id"] for value in fixtures["sessions"] if value["studio"] == STUDIO}
        assert expected_all <= {value["id"] for value in all_values}, "all-courses view lost a status category"
        assert fixtures["foreign"]["id"] not in {value["id"] for value in all_values}
        viewer_values, _ = all_pages(call, teacher, query, size=100)
        assert fixtures["booked"]["id"] in {value["id"] for value in viewer_values}, "another student's booking hid a course"

        first = call(path(**query), student)
        zero = call(path(**query, page_size=0), student)
        maximum = call(path(**query, page_size=999), student)
        assert len(first["sessions"]) == 24 and zero["sessions"] == first["sessions"]
        assert len(maximum["sessions"]) == 100 and maximum.get("nextPageToken")
        assert call(path(studio_id=STUDIO, teacher_id=uid()), student)["sessions"] == []

        for bad in ("-1", "1.5", "2147483648", "", "invalid"):
            call(path(**query, page_size=bad), student, expected=(400,))
        for bad in ("maybe", ""):
            call(path(studio_id=STUDIO, bookable_only=bad), student, expected=(400,))
        call(path(studio_id="not-a-studio"), student, expected=(400,))
        call(path(teacher_id="not-a-teacher"), student, expected=(400,))
        for bad in ("not-a-cursor", "a" * 2050,
                    base64.urlsafe_b64encode(json.dumps({"v": 99}).encode()).decode().rstrip("=")):
            call(path(**query, page_token=bad), student, expected=(400,))

        # The cursor belongs to its actor, filters and tenant selection.
        call(path(**query, page_token=cursor), teacher, expected=(400,))
        call(path(studio_id=STUDIO, bookable_only="false", page_token=cursor), student, expected=(400,))
        call(path(**query, teacher_id=fixtures["tied"][0]["teacher"], page_token=cursor), student, expected=(400,))
        call(path(studio_id=OTHER_STUDIO, bookable_only="true", page_token=cursor), student, expected=(400,))
        call(path(**query, page_token=cursor), student, studio=OTHER_STUDIO, expected=(403,))
        for token in (student, teacher, admin):
            response = call(path(studio_id=OTHER_STUDIO), token)
            assert response["sessions"] == [] and not response.get("nextPageToken"), "query filter bypassed selected tenant"

        # A legitimate continuation may become empty when its remaining row is
        # deleted; it must terminate with an empty array and no repeated cursor.
        tail_query = {**query, "teacher_id": fixtures["tail_first"]["teacher"], "page_size": 1}
        first_tail = call(path(**tail_query), student)
        assert [row["id"] for row in first_tail["sessions"]] == [fixtures["tail_first"]["id"]]
        assert first_tail.get("nextPageToken")
        sql(f"DELETE FROM scheduling.class_sessions WHERE id='{fixtures['tail_last']['id']}'")
        empty_tail = call(path(**tail_query, page_token=first_tail["nextPageToken"]), student)
        assert empty_tail["sessions"] == [] and not empty_tail.get("nextPageToken")

        roster = call(f"/v1/class-sessions/{fixtures['roster']['id']}/roster", teacher)
        assert [row["id"] for row in roster["bookings"]] == sorted(value["id"] for value in fixtures["roster_bookings"])
        call(f"/v1/class-sessions/{fixtures['tied'][0]['id']}/roster", teacher, expected=(403,))
        empty_roster = call(f"/v1/class-sessions/{fixtures['empty_roster']['id']}/roster", teacher)
        assert empty_roster["bookings"] == [] and not empty_roster.get("nextPageToken")
        print("PASS 130 same-time courses, pagination limits, empty continuation, filters, tenant isolation and teacher roster", flush=True)
    finally:
        remove_fixtures(fixtures)


if __name__ == "__main__":
    raise SystemExit("Run pagination.run(student, teacher, admin) through the isolated closeout acceptance entry point.")
