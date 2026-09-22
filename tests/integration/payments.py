"""Local Compose payment regression checks; creates only fake demo purchases.

Run from the repository root: python tests/integration/payments.py
Uses real Keycloak authorization-code + PKCE login without printing tokens.
Requires the seeded local realm, fake payment mode, and Docker Compose.
"""
import base64
import hashlib
import html
import http.cookiejar
import json
import os
import re
import secrets
import subprocess
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid


PROJECT = os.environ.get("COMPOSE_PROJECT_NAME", "bay-area-dance-hub")
API = "http://localhost:8080"
ISSUER = "http://localhost:8081/realms/bay-area-dance-hub/protocol/openid-connect"
STUDIO = "10000000-0000-0000-0000-000000000001"


def require_isolated_environment():
    """Validate the acceptance target before login, SQL or fault injection."""
    run_id = os.environ.get("DANCEHUB_ACCEPTANCE_RUN_ID", "")
    namespace = os.environ.get("DANCEHUB_ACCEPTANCE_NAMESPACE", "")
    if namespace:
        assert namespace in ("dancehub-local", "dancehub-dev"), "unexpected acceptance namespace"
        assert os.environ.get("DANCEHUB_ACCEPTANCE_CONTEXT") == "kind-dancehub-acceptance", "unexpected Kubernetes context"
        assert os.environ.get("KUBECONFIG") and run_id, "dedicated kubeconfig and run ID are required"
    else:
        assert run_id and PROJECT.startswith("dancehub-acceptance-"), "fault checks require an isolated acceptance project"
        assert os.environ.get("DANCEHUB_ACCEPTANCE_PROJECT") == PROJECT, "acceptance project marker does not match"


def service_exec(service, *arguments, input=None):
    namespace = os.environ.get("DANCEHUB_ACCEPTANCE_NAMESPACE", "")
    if namespace:
        require_isolated_environment()
        controller = "statefulset" if service in ("postgres", "kafka", "redis-flashsale") else "deployment"
        command = ["kubectl", "--kubeconfig", os.environ["KUBECONFIG"], "--context",
                   os.environ["DANCEHUB_ACCEPTANCE_CONTEXT"], "-n", namespace,
                   "exec", "-i", controller + "/" + service, "--", *arguments]
    else:
        command = ["docker", "compose", "-p", PROJECT, "exec", "-T", service, *arguments]
    return subprocess.run(command, input=input, check=True, capture_output=True, text=True).stdout.strip()


class CaptureCallback(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        if newurl.startswith("http://localhost:4200/auth/callback"):
            self.callback = newurl
            return None
        return super().redirect_request(req, fp, code, msg, headers, newurl)


class LocalhostCookiePolicy(http.cookiejar.DefaultCookiePolicy):
    def return_ok_secure(self, cookie, request):
        # Browsers allow Secure cookies on localhost; urllib needs the same
        # explicit exception for the local Keycloak development realm.
        if urllib.parse.urlparse(request.full_url).hostname == "localhost":
            return True
        return super().return_ok_secure(cookie, request)


def login(username):
    redirect = CaptureCallback()
    opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar(policy=LocalhostCookiePolicy())), redirect)
    verifier, state = secrets.token_urlsafe(48), secrets.token_urlsafe(24)
    challenge = base64.urlsafe_b64encode(hashlib.sha256(verifier.encode()).digest()).decode().rstrip("=")
    query = urllib.parse.urlencode(dict(client_id="teacher-web", redirect_uri="http://localhost:4200/auth/callback", response_type="code", scope="openid", state=state, code_challenge_method="S256", code_challenge=challenge))
    with opener.open(ISSUER + "/auth?" + query) as response:
        page = response.read().decode()
    action = html.unescape(re.search(r'<form[^>]+action="([^"]+)"', page).group(1))
    try:
        opener.open(action, urllib.parse.urlencode(dict(username=username, password="DanceHub123!", credentialId="")).encode())
    except urllib.error.HTTPError as error:
        if error.code not in (302, 303) or not getattr(redirect, "callback", None):
            raise
    callback = urllib.parse.parse_qs(urllib.parse.urlparse(redirect.callback).query)
    assert callback["state"] == [state]
    form = dict(grant_type="authorization_code", client_id="teacher-web", redirect_uri="http://localhost:4200/auth/callback", code=callback["code"][0], code_verifier=verifier)
    with opener.open(ISSUER + "/token", urllib.parse.urlencode(form).encode()) as response:
        return json.load(response)["access_token"]


def request(path, token=None, data=None, expected=200):
    headers = {"X-Studio-Id": STUDIO, "Content-Type": "application/json"}
    if token:
        headers["Authorization"] = "Bearer " + token
    req = urllib.request.Request(API + path, json.dumps(data).encode() if data is not None else None, headers)
    try:
        response = urllib.request.urlopen(req, timeout=20)
    except urllib.error.HTTPError as error:
        response = error
    body = response.read().decode()
    assert response.code == expected, (path, response.code, expected, body)
    return json.loads(body) if body.startswith("{") else body


def sql(statement):
    return service_exec("postgres", "psql", "-U", "dancehub", "-d", "dancehub", "-v", "ON_ERROR_STOP=1", "-Atc", statement)


def main():
    subprocess.run(["docker", "compose", "-p", PROJECT, "exec", "-T", "paymentservice", "node", "-e", "if(process.env.PAYMENT_MODE!=='fake'||process.env.ENABLE_SIMULATED_PAYMENTS!=='true'||!['local','test'].includes(process.env.ENVIRONMENT))process.exit(1)"], check=True, capture_output=True)
    student = login("student@bayareadancehub.local")
    other = login("platform@bayareadancehub.local")
    products = request("/v1/credit-products", student)["products"]
    product = next(item for item in products if item.get("issuerScope") == "PLATFORM")
    version = product.get("versionId") or product.get("productVersionId") or product.get("id")
    order = request("/v1/orders", student, {"items": [{"productVersionId": version, "quantity": 1}], "idempotencyKey": str(uuid.uuid4())}, 201)
    order_id = str(uuid.UUID(order["id"]))
    payment = request("/v1/payments", student, {"orderId": order_id, "idempotencyKey": str(uuid.uuid4())}, 201)
    payment_id = str(uuid.UUID(payment["id"]))
    assert payment["simulationEnabled"] is True
    assert request(f"/v1/orders/{order_id}/payment", student)["id"] == payment_id
    request(f"/v1/payments/{payment_id}", other, expected=404)
    request(f"/v1/orders/{order_id}/payment", other, expected=404)
    request(f"/v1/payments/{payment_id}/simulate", other, {"outcome": "SUCCEEDED", "idempotencyKey": str(uuid.uuid4())}, 404)
    request("/webhooks/stripe", data={"id": "unsigned-event"}, expected=404)
    # Start near a second boundary so the actual persisted failure and success
    # exercise Stripe's equal-second timestamps on the local stack.
    time.sleep(1.05 - time.time() % 1)
    failed_key, success_key = str(uuid.uuid4()), str(uuid.uuid4())
    failure = {"outcome": "FAILED", "idempotencyKey": failed_key}
    success = {"outcome": "SUCCEEDED", "idempotencyKey": success_key}
    assert request(f"/v1/payments/{payment_id}/simulate", student, failure)["status"] == "FAILED"
    succeeded = request(f"/v1/payments/{payment_id}/simulate", student, success)
    assert succeeded["status"] == "SUCCEEDED" and succeeded["succeededAt"]
    assert request(f"/v1/payments/{payment_id}/simulate", student, success)["status"] == "SUCCEEDED"
    request(f"/v1/payments/{payment_id}/simulate", student, {"outcome": "FAILED", "idempotencyKey": success_key}, 409)
    assert sql(f"SELECT count(*) FROM payment.outbox_events WHERE aggregate_id='{payment_id}' AND event_type='PaymentSucceeded'") == "1"
    assert sql(f"SELECT count(DISTINCT provider_created_at) FROM payment.webhook_events WHERE payload #>> '{{data,object,id}}'=(SELECT provider_payment_id FROM payment.payments WHERE id='{payment_id}')") == "1", "local calls crossed the second boundary; rerun this timing scenario"
    deadline = time.monotonic() + 45
    while time.monotonic() < deadline:
        current = request(f"/v1/orders/{order_id}", student)
        if current["status"] == "FULFILLED":
            break
        time.sleep(1)
    assert current["status"] == "FULFILLED", current
    assert sql(f"SELECT count(*) FROM credit.grants WHERE source_order_line_id IN (SELECT id FROM orders.order_lines WHERE order_id='{order_id}')") == "1"
    # Database role itself cannot read another user's payment, independently of
    # HTTP and gRPC owner checks.
    count = sql(f"BEGIN; SET LOCAL ROLE payment_runtime; SELECT set_config('app.actor_kind','human',true),set_config('app.user_id','30000000-0000-0000-0000-000000000004',true),set_config('app.global_roles','platform_admin',true),set_config('app.service','paymentservice',true); SELECT count(*) FROM payment.payments WHERE id='{payment_id}'; ROLLBACK;")
    assert count.splitlines()[-2] == "0", count
    print("PASS owner isolation, fake webhook closure, same-second failure/success, simulation idempotency, fulfillment, SQL RLS")


if __name__ == "__main__":
    main()
