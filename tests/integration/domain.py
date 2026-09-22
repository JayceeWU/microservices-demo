"""Domain regressions against the seeded local fake-payment Compose stack.

    python tests/integration/domain.py
    python tests/integration/domain.py --faults-only
    python tests/integration/domain.py --boundaries-only

The ordinary run purchases demo credits, exercises their real HTTP commands,
and verifies durable records. Fault checks additionally pause/restart services
and inject the precise persisted state of a lost RPC response or delayed event.
They require a dedicated acceptance project and restore paused services in finally.
"""
import argparse
from contextlib import contextmanager
from datetime import datetime, timedelta, timezone
import json
import subprocess
import time
import uuid

from payments import PROJECT, STUDIO, login, request, sql, require_isolated_environment


TEACHER_ID = "30000000-0000-0000-0000-000000000002"
STUDENT_ID = "30000000-0000-0000-0000-000000000001"


def audit():
    return {"idempotencyKey": str(uuid.uuid4()), "reason": "local domain regression"}


def compose(*args):
    return subprocess.run(["docker", "compose", "-p", PROJECT, *args], check=True,
                          capture_output=True, text=True).stdout


@contextmanager
def paused(service):
    require_isolated_environment()
    compose("pause", service)
    try:
        yield
    finally:
        compose("unpause", service)


def eventually(read, predicate, seconds=100):
    deadline = time.monotonic() + seconds
    while True:
        value = read()
        if predicate(value):
            return value
        assert time.monotonic() < deadline, value
        time.sleep(1)


def order_state(token, order_id, status, phase=None):
    return eventually(lambda: request(f"/v1/orders/{order_id}", token),
                      lambda value: value["status"] == status and (phase is None or value.get("refundPhase") == phase))


def new_payment(token, version, quantity=1):
    order = request("/v1/orders", token, {"items": [{"productVersionId": version, "quantity": quantity}],
                                         "idempotencyKey": str(uuid.uuid4())}, 201)
    order_id = str(uuid.UUID(order["id"]))
    payment = request("/v1/payments", token, {"orderId": order_id, "idempotencyKey": str(uuid.uuid4())}, 201)
    assert payment["simulationEnabled"] is True
    return order_id, str(uuid.UUID(payment["id"]))


def pay(token, payment_id):
    return request(f"/v1/payments/{payment_id}/simulate", token,
                   {"outcome": "SUCCEEDED", "idempotencyKey": str(uuid.uuid4())})


def buy(token, version, quantity=1):
    order_id, payment_id = new_payment(token, version, quantity)
    pay(token, payment_id)
    order_state(token, order_id, "FULFILLED")
    return order_id, payment_id


def grant_ids(order_id):
    return sql(f"SELECT id FROM credit.grants WHERE source_order_line_id IN "
               f"(SELECT id FROM orders.order_lines WHERE order_id='{order_id}') ORDER BY id").splitlines()


def approve(admin, order_id):
    return request(f"/v1/orders/{order_id}/refund/approve", admin, audit())


def make_class(teacher, admin):
    room = request(f"/v1/studios/{STUDIO}/rooms", teacher)["rooms"][0]["id"]
    # Stay inside the seeded studio product's validity and outside E2E dates.
    start = datetime.now(timezone.utc).replace(hour=7, minute=0, second=0, microsecond=0) + timedelta(days=14)
    # A unique future day avoids overlap when the script is rerun.
    while sql(f"SELECT count(*) FROM scheduling.class_sessions WHERE teacher_id='{TEACHER_ID}' "
              f"AND starts_at='{start.isoformat()}'") != "0":
        start += timedelta(days=1)
    body = {"studioId": STUDIO, "roomId": room, "title": "Domain cancellation regression",
            "description": "Local integration fixture", "startsAt": start.isoformat(),
            "endsAt": (start + timedelta(hours=1)).isoformat(), "capacity": 6, **audit()}
    session = request("/v1/class-sessions", teacher, body, 201)
    request(f"/v1/class-sessions/{session['id']}/approve", admin,
            {"approve": True, "minimumStudents": 1, **audit()})
    return session["id"]


def book(teacher, admin):
    session_id = make_class(teacher, admin)
    booking = request(f"/v1/class-sessions/{session_id}/bookings", teacher,
                      {"idempotencyKey": str(uuid.uuid4())}, 201)
    return session_id, str(uuid.UUID(booking["id"]))


def assert_compensated(booking_id, session_id):
    eventually(lambda: sql(f"SELECT status FROM scheduling.credit_compensations WHERE booking_id='{booking_id}'"),
               lambda value: value == "DONE", 130)
    assert sql(f"SELECT status FROM credit.holds WHERE booking_id='{booking_id}'") in ("RELEASED", "REVERSED")
    assert sql(f"SELECT confirmed_count FROM scheduling.class_sessions WHERE id='{session_id}'") == "0"
    assert sql(f"SELECT credit_compensation_pending FROM scheduling.bookings WHERE id='{booking_id}'") == "f"
    assert sql(f"SELECT count(*) FROM credit.booking_cancellations WHERE booking_id='{booking_id}'") == "1"
    assert sql(f"SELECT count(*) FROM credit.ledger_entries WHERE booking_id='{booking_id}' "
               "AND kind IN ('RELEASE','REVERSAL')") == "1"


def assert_late_hold_rejected(booking_id):
    # Replay the real downstream RPC after the cancellation worker committed.
    script = """
const grpc=require('@grpc/grpc-js'),loader=require('@grpc/proto-loader');
const google=require('google-proto-files'),path=require('node:path');
const definition=loader.loadSync('/usr/protos/credits/v1/credits.proto',
  {includeDirs:['/usr/protos',path.dirname(google.getProtoPath())]});
const Service=grpc.loadPackageDefinition(definition).dancehub.credits.v1.CreditService;
const client=new Service('creditservice:9090',grpc.credentials.createInsecure());
const p=JSON.parse(process.argv[1]),md=new grpc.Metadata();
for(const [k,v] of Object.entries({'x-user-id':p.user,'x-studio-id':p.studio,'x-actor-kind':'human','x-request-id':'domain-late-hold'}))md.set(k,v);
client.PlaceHold({bookingId:p.booking,studioId:p.studio,amount:{units:4},
 classStartsAt:{seconds:Math.floor(Date.now()/1000)+86400},audit:{idempotencyKey:'late-hold:'+p.booking}},
 md,{deadline:Date.now()+10000},(error)=>{client.close();if(!error||error.code!==9){console.error(error?.message||'unexpected success');process.exitCode=1;}});
"""
    compose("exec", "-T", "paymentservice", "node", "-e", script,
            json.dumps({"user": TEACHER_ID, "studio": STUDIO, "booking": booking_id}))


def ordinary(teacher, admin, version):
    # Direct approval from FULFILLED must reserve and revoke all quantity units.
    order_id, payment_id = buy(teacher, version, 2)
    ids = grant_ids(order_id)
    assert len(ids) == 2, ids
    # Verify real pause/resume operations and paused-grant refund eligibility.
    grant = ids[0]
    assert request(f"/v1/credits/grants/{grant}/pause", admin, audit())["status"] == "PAUSED"
    assert request(f"/v1/credits/grants/{grant}/resume", admin, audit())["status"] == "ACTIVE"
    assert request(f"/v1/credits/grants/{grant}/pause", admin, audit())["status"] == "PAUSED"
    approve(admin, order_id)
    order_state(teacher, order_id, "REFUNDED", "COMPLETED")
    assert request(f"/v1/payments/{payment_id}", teacher)["status"] == "REFUNDED"
    assert sql(f"SELECT count(*) FROM credit.grants WHERE id IN ('{ids[0]}','{ids[1]}') "
               "AND status='REVOKED' AND remaining_credits=0 AND refund_pending_at IS NULL") == "2"
    # Repeated approval cannot refund money or revoke credits twice.
    approve(admin, order_id)
    assert sql(f"SELECT count(*) FROM credit.ledger_entries WHERE grant_id IN ('{ids[0]}','{ids[1]}') AND kind='REFUND'") == "2"
    print("PASS direct quantity-2 refund, pause/resume, paused eligibility, repeat approval", flush=True)

    held_order, held_payment = buy(teacher, version)
    session_id, booking_id = book(teacher, admin)
    allocated_order = sql(f"SELECT DISTINCT l.order_id FROM credit.hold_allocations a JOIN credit.holds h ON h.id=a.hold_id "
                          f"JOIN credit.grants g ON g.id=a.grant_id JOIN orders.order_lines l ON l.id=g.source_order_line_id "
                          f"WHERE h.booking_id='{booking_id}'")
    # A rerun may select an earlier still-unused grant; validate the real
    # allocation's source order instead of assuming creation order wins.
    held_order = str(uuid.UUID(allocated_order))
    held_payment = request(f"/v1/orders/{held_order}/payment", teacher)["id"]
    approve(admin, held_order)
    denied = order_state(teacher, held_order, "FULFILLED", "DENIED")
    assert denied["refundFailureReason"] == "This order is not eligible for a refund."
    assert request(f"/v1/payments/{held_payment}", teacher)["status"] == "SUCCEEDED"
    cancellation = audit()
    request(f"/v1/bookings/{booking_id}/cancel", teacher, cancellation)
    request(f"/v1/bookings/{booking_id}/cancel", teacher, cancellation)
    assert_compensated(booking_id, session_id)
    print("PASS active-use refund denial and idempotent cancellation compensation", flush=True)

    source_grant = grant_ids(held_order)[0]
    transferred = request(f"/v1/credits/grants/{source_grant}/transfer", admin,
                          {"targetUserId": STUDENT_ID, **audit()})
    assert transferred["status"] == "TRANSFERRED" and transferred["successorGrantId"]
    approve(admin, held_order)
    order_state(teacher, held_order, "FULFILLED", "DENIED")
    assert request(f"/v1/payments/{held_payment}", teacher)["status"] == "SUCCEEDED"
    assert sql(f"SELECT count(*) FROM credit.grant_operations WHERE grant_id='{source_grant}' AND operation='TRANSFER'") == "1"
    print("PASS transfer operation and transferred-order refund denial", flush=True)


def faults(teacher, admin, version):
    # Persisted state after Credit commits PlaceHold but its response is lost.
    buy(teacher, version)
    session_id, booking_id = book(teacher, admin)
    sql(f"UPDATE scheduling.bookings SET status='PENDING_CREDIT',credit_hold_id=NULL WHERE id='{booking_id}'")
    with paused("creditservice"):
        cancellation = audit()
        result = request(f"/v1/bookings/{booking_id}/cancel", teacher, cancellation)
        assert result["creditCompensationPending"] is True
        request(f"/v1/bookings/{booking_id}/cancel", teacher, cancellation)
        compose("restart", "schedulingservice")
        assert sql(f"SELECT status FROM scheduling.credit_compensations WHERE booking_id='{booking_id}'") == "PENDING"
        assert sql(f"SELECT confirmed_count FROM scheduling.class_sessions WHERE id='{session_id}'") == "0"
    assert_compensated(booking_id, session_id)
    assert_late_hold_rejected(booking_id)
    print("PASS lost PlaceHold response + unavailable Credit + Scheduling restart recovery", flush=True)

    order_id, payment_id = buy(teacher, version, 2)
    with paused("creditservice"):
        approve(admin, order_id)
        eventually(lambda: sql(f"SELECT attempt_count FROM orders.refund_attempts WHERE order_id='{order_id}'"),
                   lambda value: value and int(value) > 0, 30)
        assert request(f"/v1/payments/{payment_id}", teacher)["status"] == "SUCCEEDED"
        compose("restart", "orderservice")
    order_state(teacher, order_id, "REFUNDED", "COMPLETED")
    assert sql(f"SELECT count(*) FROM credit.grants WHERE source_order_line_id IN "
               f"(SELECT id FROM orders.order_lines WHERE order_id='{order_id}') AND status='REVOKED'") == "2"
    print("PASS refund RPC outage + Orders restart recovery without premature money refund", flush=True)

    # Fake confirms within its real deadline. Before the delayed event is read,
    # inject an Orders deadline just before paid_at, the late-payment boundary.
    with paused("payment-order-saga"):
        order_id, payment_id = new_payment(teacher, version)
        payment = pay(teacher, payment_id)
        assert payment["succeededAt"]
        sql(f"UPDATE orders.orders SET payment_expires_at=(SELECT succeeded_at-interval '1 second' "
            f"FROM payment.payments WHERE id='{payment_id}') WHERE id='{order_id}'")
    order_state(teacher, order_id, "REFUNDED", "COMPLETED")
    assert grant_ids(order_id) == []
    assert request(f"/v1/payments/{payment_id}", teacher)["status"] == "REFUNDED"
    assert sql(f"SELECT kind FROM orders.refund_attempts WHERE order_id='{order_id}'") == "UNFULFILLED"
    print("PASS late payment event compensates money without issuing credits", flush=True)
    payment_boundaries(teacher, version)


def payment_boundaries(teacher, version):
    # Both scenarios delay the real Kafka consumer. Only deadlines are changed;
    # the existing expiry workers must actually release the persisted states.
    with paused("payment-order-saga"):
        on_time_order, on_time_payment = new_payment(teacher, version)
        pay(teacher, on_time_payment)
        sql(f"UPDATE orders.orders SET payment_expires_at=(SELECT succeeded_at+interval '1 second' "
            f"FROM payment.payments WHERE id='{on_time_payment}') WHERE id='{on_time_order}'")

        room = request(f"/v1/studios/{STUDIO}/rooms", teacher)["rooms"][0]["id"]
        start = datetime.now(timezone.utc).replace(hour=6, minute=0, second=0, microsecond=0) + timedelta(days=42)
        while sql(f"SELECT count(*) FROM scheduling.room_reservations WHERE room_id='{room}' "
                  f"AND starts_at='{start.isoformat()}'") != "0":
            start += timedelta(days=1)
        reservation = request("/v1/room-reservations", teacher,
                              {"studioId": STUDIO, "roomId": room, "startsAt": start.isoformat(),
                               "endsAt": (start + timedelta(hours=1)).isoformat(), **audit()}, 201)
        reservation_id = str(uuid.UUID(reservation["id"]))
        room_order = request("/v1/orders/room", teacher, {"roomReservationId": reservation_id, **audit()}, 201)["id"]
        room_payment = request("/v1/payments", teacher, {"orderId": room_order, "idempotencyKey": str(uuid.uuid4())}, 201)["id"]
        pay(teacher, room_payment)
        sql(f"UPDATE scheduling.room_reservations SET hold_expires_at=now()-interval '1 second' WHERE id='{reservation_id}'")
        order_state(teacher, on_time_order, "EXPIRED")
        eventually(lambda: sql(f"SELECT status FROM scheduling.room_reservations WHERE id='{reservation_id}'"),
                   lambda value: value == "EXPIRED", 90)
        assert sql(f"SELECT active FROM scheduling.room_occupancies WHERE room_reservation_id='{reservation_id}'") == "f"

    order_state(teacher, on_time_order, "FULFILLED")
    assert len(grant_ids(on_time_order)) == 1
    assert request(f"/v1/payments/{on_time_payment}", teacher)["status"] == "SUCCEEDED"
    assert sql(f"SELECT count(*) FROM orders.refund_attempts WHERE order_id='{on_time_order}'") == "0"
    print("PASS on-time paid_at delayed beyond order EXPIRED still fulfills exactly once", flush=True)

    order_state(teacher, room_order, "REFUNDED", "COMPLETED")
    assert request(f"/v1/payments/{room_payment}", teacher)["status"] == "REFUNDED"
    assert sql(f"SELECT kind FROM orders.refund_attempts WHERE order_id='{room_order}'") == "ROOM"
    assert sql(f"SELECT status FROM scheduling.room_reservations WHERE id='{reservation_id}'") == "EXPIRED"
    assert sql(f"SELECT active FROM scheduling.room_occupancies WHERE room_reservation_id='{reservation_id}'") == "f"
    print("PASS paid room whose hold expired refunds automatically without reclaiming occupancy", flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--faults-only", action="store_true")
    parser.add_argument("--boundaries-only", action="store_true")
    args = parser.parse_args()
    if args.faults_only or args.boundaries_only:
        require_isolated_environment()
    compose("exec", "-T", "paymentservice", "node", "-e",
            "if(process.env.PAYMENT_MODE!=='fake'||process.env.ENABLE_SIMULATED_PAYMENTS!=='true'||!['local','test'].includes(process.env.ENVIRONMENT))process.exit(1)")
    teacher = login("teacher@bayareadancehub.local")
    admin = login("admin@bayareadancehub.local")
    products = request(f"/v1/credit-products?studio_id={STUDIO}", teacher)["products"]
    product = next(value for value in products if value.get("issuerScope") == "STUDIO" and
                   not value.get("finalSale") and value.get("kind") == "CREDITS")
    version = product.get("versionId") or product.get("productVersionId") or product["id"]
    if args.boundaries_only:
        payment_boundaries(teacher, version)
    else:
        (faults if args.faults_only else ordinary)(teacher, admin, version)


if __name__ == "__main__":
    main()
