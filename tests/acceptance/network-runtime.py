#!/usr/bin/env python3
"""Ephemeral TCP NetworkPolicy acceptance; never applies application manifests.

Usage: python tests/acceptance/network-runtime.py dancehub-local
       python tests/acceptance/network-runtime.py dancehub-dev --matrix-only

Requires DANCEHUB_ACCEPTANCE_OUTPUT_DIR to contain build.json, kubeconfig, runtime-<namespace>.yaml,
the repository's node_modules/js-yaml, and already-loaded recommendation image.
All kubectl calls use the dedicated kubeconfig/context. The manifest is read only.
"""

from __future__ import annotations

import argparse
import base64
from concurrent.futures import ThreadPoolExecutor, as_completed
from dataclasses import dataclass
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import time
from urllib.parse import urlsplit
import uuid


REPO = Path(__file__).resolve().parents[2]
CACHE = Path(os.environ.get('DANCEHUB_ACCEPTANCE_OUTPUT_DIR', REPO / '.cache' / 'acceptance'))
FOREIGN = "dancehub-network-foreign"
RUN_LABEL = "acceptance-netproof-run"
CONTEXT = "kind-dancehub-acceptance"
BASE = ["kubectl", "--kubeconfig", str(CACHE / "kubeconfig"), "--context", CONTEXT]
GRPC = (
    "accountservice", "catalogservice", "schedulingservice", "creditservice",
    "orderservice", "paymentservice", "payrollservice", "cartservice",
    "recommendationservice", "chatservice",
)
DATABASE_APPS = (
    "dancehub-migrations", "dancehub-seed", "keycloak", "accountservice",
    "catalogservice", "schedulingservice", "creditservice", "orderservice",
    "paymentservice", "payrollservice", "recommendationservice", "chatservice",
    "chat-media-worker", "outbox-relay",
)
OTLP_HTTP = (
    "gatewayservice", "accountservice", "catalogservice", "schedulingservice",
    "orderservice", "payrollservice", "paymentservice", "chatservice", "chat-media-worker",
)
OTLP_GRPC = ("creditservice", "cartservice", "recommendationservice", "outbox-relay", "payment-order-saga")


@dataclass(frozen=True)
class Case:
    source: str
    app: str
    foreign: bool
    target: str
    port: int
    allowed: bool
    reason: str


def read_manifest(path):
    # js-yaml is already a root dependency; no Python packages are installed.
    script = "const fs=require('fs'),y=require('js-yaml');process.stdout.write(JSON.stringify(y.loadAll(fs.readFileSync(process.argv[1],'utf8')).filter(Boolean)))"
    result = subprocess.run(["node", "-e", script, str(path)], cwd=REPO,
                            capture_output=True, text=True, encoding="utf-8", timeout=30)
    if result.returncode:
        raise RuntimeError("Cannot parse rendered manifest with the root js-yaml dependency")
    return json.loads(result.stdout)


def object_values(document):
    values = dict(document.get("data", {}))
    if document.get("kind") == "Secret":
        values = {key: base64.b64decode(value).decode("utf-8") for key, value in values.items()}
    values.update(document.get("stringData", {}))
    return values


def build_matrix(documents, namespace):
    services = {d["metadata"]["name"]: d for d in documents if d.get("kind") == "Service"}
    configurations = {(d["kind"], d["metadata"]["name"]): object_values(d)
                      for d in documents if d.get("kind") in ("Secret", "ConfigMap")}
    workloads = {}
    for document in documents:
        if document.get("kind") not in ("Deployment", "StatefulSet", "DaemonSet", "Job"):
            continue
        template = document["spec"]["template"]
        app = template.get("metadata", {}).get("labels", {}).get("app")
        if app:
            workloads[app] = template["spec"]
    cases = {}

    def add(app, target, port, reason, allowed=True, foreign=False, source=None):
        if allowed and app not in workloads:
            raise ValueError(f"Required source app is missing from rendered workloads: {app}")
        if target not in services:
            raise ValueError(f"Required target Service is missing: {target}")
        ports = {int(p["port"]) for p in services[target]["spec"]["ports"] if p.get("protocol", "TCP") == "TCP"}
        if port not in ports:
            raise ValueError(f"Required TCP Service port is missing: {target}:{port}")
        source = source or app
        key = (source, foreign, target, port, allowed)
        if key not in cases:
            cases[key] = Case(source, app, foreign, target, port, allowed, reason)

    # An explicit minimum matrix ensures missing env cannot silently remove coverage.
    for app in DATABASE_APPS:
        add(app, "postgres", 5432, "database / migration or seed initialization")
    for app in ("student-web", "gatewayservice"):
        add(app, "keycloak", 8080, "server-side OIDC / JWKS")
    for target in GRPC:
        add("gatewayservice", target, 9090, "gateway business RPC")
    add("gatewayservice", "paymentservice", 8080, "Stripe webhook proxy")
    for app, targets in {
        "schedulingservice": ("creditservice", "catalogservice"),
        "orderservice": ("creditservice", "schedulingservice", "catalogservice", "paymentservice"),
        "chatservice": ("accountservice", "catalogservice"),
        "payment-order-saga": ("orderservice",),
    }.items():
        for target in targets:
            add(app, target, 9090, "service / saga RPC")
    for app, target in (("cartservice", "redis-cart"), ("orderservice", "redis-flashsale"), ("chatservice", "redis-chat")):
        add(app, target, 6379, "Redis dependency")
    for app in ("outbox-relay", "recommendationservice", "payment-order-saga", "payrollservice"):
        add(app, "kafka", 29092, "event stream")
    add("kafka", "kafka", 29093, "broker-to-controller through Kafka Service")
    for app in ("orderservice", "chatservice", "chat-media-worker"):
        add(app, "rabbitmq", 5672, "work queue")
    for app in ("chatservice", "chat-media-worker"):
        add(app, "minio", 9000, "private object storage")
    add("chat-media-worker", "clamav", 3310, "attachment scan")
    for app in OTLP_HTTP:
        add(app, "otel-collector", 4318, "OTLP HTTP")
    for app in OTLP_GRPC:
        add(app, "otel-collector", 4317, "OTLP gRPC")
    add("otel-collector", "jaeger", 4317, "trace export")

    # Supplement the minimum with actual env dependencies, including initContainers.
    # Secret values remain in memory; only Service names and ports enter output.
    endpoint_name = re.compile(r"(?:_ADDR|_URL|_ENDPOINT|_BOOTSTRAP_SERVERS)$")
    defaults = {"postgres": 5432, "postgresql": 5432, "http": 80, "https": 443,
                "amqp": 5672, "amqps": 5671, "redis": 6379}
    for app, pod_spec in workloads.items():
        containers = list(pod_spec.get("initContainers", [])) + pod_spec.get("containers", [])
        for container in containers:
            env = {}
            for reference in container.get("envFrom", []):
                for field, kind in (("configMapRef", "ConfigMap"), ("secretRef", "Secret")):
                    if field in reference:
                        name = reference[field]["name"]
                        env.update({reference.get("prefix", "") + k: v
                                    for k, v in configurations.get((kind, name), {}).items()})
            for item in container.get("env", []):
                name = item["name"]
                if "value" in item:
                    env[name] = str(item["value"])
                    continue
                for field, kind in (("configMapKeyRef", "ConfigMap"), ("secretKeyRef", "Secret")):
                    reference = item.get("valueFrom", {}).get(field)
                    if reference:
                        value = configurations.get((kind, reference["name"]), {}).get(reference["key"])
                        if value is not None:
                            env[name] = value
            for name, value in env.items():
                if not endpoint_name.search(name):
                    continue
                for endpoint in value.split(","):
                    endpoint = endpoint.strip().removeprefix("jdbc:")
                    try:
                        address = urlsplit(endpoint if "://" in endpoint else "//" + endpoint)
                        host = address.hostname or ""
                        service = host.split(".")[0]
                        if service not in services:
                            continue  # Browser localhost URLs / external endpoints are not Pod traffic.
                        if "." in host and host not in (f"{service}.{namespace}", f"{service}.{namespace}.svc", f"{service}.{namespace}.svc.cluster.local"):
                            raise ValueError(f"Unexpected namespace in {app} env {name}")
                        port = address.port or defaults.get(address.scheme)
                        if port is None:
                            raise ValueError(f"Missing endpoint port in {app} env {name}")
                    except ValueError as error:
                        raise ValueError(f"Cannot resolve internal endpoint for {app} env {name}") from error
                    add(app, service, port, f"{container['name']} env {name}")

    protected = [(target, 9090) for target in GRPC] + [
        ("postgres", 5432), ("redis-cart", 6379), ("redis-flashsale", 6379),
        ("redis-chat", 6379), ("kafka", 29092), ("kafka", 29093), ("rabbitmq", 5672),
        ("minio", 9000), ("clamav", 3310), ("keycloak", 8080),
        ("otel-collector", 4317), ("otel-collector", 4318), ("jaeger", 4317),
        ("paymentservice", 8080),
    ]
    for target, port in protected:
        add("network-proof-unknown", target, port, "unknown app is denied", False, source="unknown")
        add("gatewayservice", target, port, "same app label in foreign namespace is denied",
            False, True, "foreign-gateway")
        if target != "postgres":
            add("dancehub-seed", target, port, "seed is restricted to PostgreSQL", False)
    add("dancehub-seed", "postgres", 5432, "foreign seed cannot access database", False, True, "foreign-seed")
    add("kafka", "kafka", 29093, "foreign Kafka cannot access controller", False, True, "foreign-kafka")
    for target in GRPC:
        add("dancehub-seed", target, 9090, "foreign seed cannot access RPC", False, True, "foreign-seed")
    return sorted(cases.values(), key=lambda c: (not c.allowed, c.foreign, c.source, c.target, c.port))


REMOTE_PROBE = r'''
import concurrent.futures, errno, json, socket, sys, time
cases=json.loads(sys.argv[1])
def probe(case):
    started=time.monotonic()
    try:
        with socket.create_connection((case['host'],case['port']),timeout=3):
            result={'status':'connected'}
    except socket.gaierror:
        result={'status':'dns_error'}
    except TimeoutError:
        result={'status':'timeout'}
    except OSError as error:
        result={'status':'socket_error','errno':error.errno}
    result.update(index=case['index'],elapsed_ms=round((time.monotonic()-started)*1000))
    return result
with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:
    for result in pool.map(probe,cases):
        print(json.dumps(result),flush=True)
'''


class Runtime:
    def __init__(self, namespace, image):
        self.namespace = namespace
        self.image = image
        self.run = uuid.uuid4().hex[:10]
        self.owner = "acceptance-netproof-" + self.run
        self.foreign_created = False
        self.owners = {}
        self.cleanup_namespaces = set()
        self.pods = {}
        self.failures = 0

    def kubectl(self, *args, document=None, check=True, timeout=45):
        result = subprocess.run(BASE + list(args), input=None if document is None else json.dumps(document),
                                text=True, encoding="utf-8", capture_output=True, timeout=timeout)
        if check and result.returncode:
            raise RuntimeError(f"kubectl {' '.join(args)}: {result.stderr.strip() or result.stdout.strip()}")
        return result

    def prepare(self, cases):
        self.kubectl("get", "namespace", self.namespace, "-o", "name")
        # A pre-existing foreign namespace is never modified or removed.
        existing = self.kubectl("get", "namespace", FOREIGN, "--ignore-not-found", "-o", "name")
        if existing.stdout.strip():
            raise RuntimeError(f"Dedicated probe namespace already exists: {FOREIGN}")
        # Track cleanup intent before mutation so a lost kubectl response does not
        # leak a successfully-created namespace; cleanup still verifies its label.
        self.foreign_created = True
        self.kubectl("create", "-f", "-", document={
            "apiVersion": "v1", "kind": "Namespace",
            "metadata": {"name": FOREIGN, "labels": {RUN_LABEL: self.run}},
        })
        for namespace in (self.namespace, FOREIGN):
            self.cleanup_namespaces.add(namespace)
            owner = self.kubectl("create", "-f", "-", "-o", "json", document={
                "apiVersion": "v1", "kind": "ConfigMap",
                "metadata": {"name": self.owner, "namespace": namespace, "labels": {RUN_LABEL: self.run}},
                "data": {"purpose": "Own ephemeral probes so application ReplicaSets cannot adopt them"},
            })
            self.owners[namespace] = json.loads(owner.stdout)["metadata"]["uid"]
        documents = []
        for case in cases:
            key = (case.source, case.foreign)
            if key in self.pods:
                continue
            namespace = FOREIGN if case.foreign else self.namespace
            name = f"netproof-{self.run}-{case.source}"[:63].rstrip("-")
            self.pods[key] = (namespace, name)
            documents.append({
                "apiVersion": "v1", "kind": "Pod",
                "metadata": {
                    "name": name, "namespace": namespace,
                    "labels": {"app": case.app, RUN_LABEL: self.run, "acceptance-netproof-source": case.source},
                    "ownerReferences": [{"apiVersion": "v1", "kind": "ConfigMap", "name": self.owner,
                                         "uid": self.owners[namespace], "controller": True}],
                },
                "spec": {
                    "automountServiceAccountToken": False, "restartPolicy": "Never",
                    "terminationGracePeriodSeconds": 1,
                    "securityContext": {"runAsNonRoot": True, "runAsUser": 65532, "runAsGroup": 65532,
                                        "seccompProfile": {"type": "RuntimeDefault"}},
                    # This unsatisfied readiness gate is never written by this helper.
                    # It protects Service endpoints even before the exec probe runs.
                    "readinessGates": [{"conditionType": "acceptance-netproof/ExcludedFromServices"}],
                    "containers": [{
                        "name": "probe", "image": self.image, "imagePullPolicy": "Never",
                        "command": ["python", "-c", "import time; time.sleep(1800)"],
                        "securityContext": {"allowPrivilegeEscalation": False,
                                            "capabilities": {"drop": ["ALL"]}, "readOnlyRootFilesystem": True},
                        "readinessProbe": {"exec": {"command": ["python", "-c", "raise SystemExit(1)"]},
                                           "periodSeconds": 30, "failureThreshold": 1},
                        "resources": {"requests": {"cpu": "5m", "memory": "16Mi"},
                                      "limits": {"cpu": "100m", "memory": "64Mi"}},
                    }],
                },
            })
        self.kubectl("create", "-f", "-", document={"apiVersion": "v1", "kind": "List", "items": documents}, timeout=90)
        deadline = time.monotonic() + 180
        while True:
            pending = []
            for namespace in (self.namespace, FOREIGN):
                result = self.kubectl("-n", namespace, "get", "pods", "-l", f"{RUN_LABEL}={self.run}", "-o", "json")
                items = json.loads(result.stdout)["items"]
                expected = sum(ns == namespace for ns, _ in self.pods.values())
                if len(items) != expected:
                    pending.append(f"{namespace}: {len(items)}/{expected} probes exist")
                for pod in items:
                    status = pod.get("status", {})
                    if any(c.get("type") == "Ready" and c.get("status") == "True" for c in status.get("conditions", [])):
                        raise RuntimeError(f"Probe unexpectedly Ready: {pod['metadata']['name']}")
                    if status.get("phase") != "Running" or not any("running" in c.get("state", {}) for c in status.get("containerStatuses", [])):
                        pending.append(pod["metadata"]["name"])
            if not pending:
                break
            if time.monotonic() >= deadline:
                raise RuntimeError("Probe startup timed out: " + ", ".join(pending))
            print(f"INFO waiting for {len(pending)} probe startup states", flush=True)
            time.sleep(3)
        # Source labels remain immutable for the entire run; allow initial CNI programming.
        time.sleep(3)

    def run_group(self, key, indexed):
        namespace, name = self.pods[key]
        payload = [{"index": index, "host": f"{case.target}.{self.namespace}.svc.cluster.local", "port": case.port}
                   for index, case in indexed]
        result = self.kubectl("-n", namespace, "exec", name, "-c", "probe", "--", "python", "-c", REMOTE_PROBE,
                              json.dumps(payload), timeout=max(45, 15 + len(payload) * 3))
        return [json.loads(line) for line in result.stdout.splitlines() if line.strip()]

    def execute(self, cases):
        controls = set()
        total_pass = 0
        for allowed in (True, False):
            groups = {}
            expected = {index for index, case in enumerate(cases) if case.allowed == allowed}
            received = set()
            for index in expected:
                case = cases[index]
                groups.setdefault((case.source, case.foreign), []).append((index, case))
            with ThreadPoolExecutor(max_workers=4) as pool:
                futures = {pool.submit(self.run_group, key, indexed): key for key, indexed in groups.items()}
                for future in as_completed(futures):
                    key = futures[future]
                    try:
                        rows = future.result()
                    except Exception as error:
                        print(f"FAIL source={key[0]} probe execution: {error}", flush=True)
                        self.failures += 1
                        continue
                    for row in rows:
                        index = row["index"]
                        if index not in expected or index in received:
                            raise RuntimeError("Probe returned an unexpected / duplicate result index")
                        received.add(index)
                        case = cases[index]
                        control = (case.target, case.port)
                        # DNS errors / refused connections do not prove policy enforcement.
                        passed = row["status"] == ("connected" if allowed else "timeout")
                        note = ""
                        if not allowed and control not in controls:
                            passed = False
                            note = " no successful allowed-source control for this target"
                        if passed:
                            total_pass += 1
                            if allowed:
                                controls.add(control)
                        else:
                            self.failures += 1
                        source_ns = FOREIGN if case.foreign else self.namespace
                        print(f"{'PASS' if passed else 'FAIL'} {'ALLOW' if allowed else 'DENY'} "
                              f"{source_ns}/{case.source}[app={case.app}] -> "
                              f"{case.target}:{case.port} {row['status']} {row['elapsed_ms']}ms "
                              f"({case.reason}){note}", flush=True)
            for index in expected - received:
                self.failures += 1
                print(f"FAIL missing result for {cases[index].source} -> {cases[index].target}:{cases[index].port}", flush=True)
        print(f"SUMMARY namespace={self.namespace} cases={len(cases)} passed={total_pass} failed={self.failures}", flush=True)

    def cleanup(self):
        # Delete only this run's label-selected probes and exact ConfigMap owners.
        for namespace in self.cleanup_namespaces:
            for args in (("delete", "pods", "-l", f"{RUN_LABEL}={self.run}", "--ignore-not-found", "--wait=true", "--timeout=30s"),
                         ("delete", "configmap", self.owner, "--ignore-not-found", "--wait=false")):
                try:
                    self.kubectl("-n", namespace, *args)
                except Exception as error:
                    self.failures += 1
                    print(f"FAIL cleanup {namespace}: {error}", flush=True)
        if self.foreign_created:
            try:
                output = self.kubectl("get", "namespace", FOREIGN, "--ignore-not-found", "-o", "json").stdout.strip()
                if not output:
                    return  # Creation failed or an earlier cleanup already completed.
                namespace = json.loads(output)
                if namespace["metadata"].get("labels", {}).get(RUN_LABEL) != self.run:
                    raise RuntimeError("Foreign namespace owner label changed; refusing namespace deletion")
                self.kubectl("delete", "namespace", FOREIGN, "--wait=true", "--timeout=30s")
            except Exception as error:
                self.failures += 1
                print(f"FAIL cleanup foreign namespace: {error}", flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("namespace", choices=("dancehub-local", "dancehub-dev"))
    parser.add_argument("--matrix-only", action="store_true", help="Read local files and print cases; never invoke kubectl")
    args = parser.parse_args()
    documents = read_manifest(CACHE / f"runtime-{args.namespace}.yaml")
    # A mismatched render must fail before any cluster mutation.
    for document in documents:
        namespace = document.get("metadata", {}).get("namespace")
        if namespace and namespace != args.namespace:
            raise ValueError(f"Rendered namespace mismatch on {document.get('kind')}/{document['metadata']['name']}")
    cases = build_matrix(documents, args.namespace)
    if args.matrix_only:
        for case in cases:
            print(f"{'ALLOW' if case.allowed else 'DENY'} {'foreign/' if case.foreign else ''}{case.source}[app={case.app}] "
                  f"-> {case.target}:{case.port} ({case.reason})")
        print(f"SUMMARY {len(cases)} cases; no kubectl invoked")
        return 0
    builds = json.loads((CACHE / "build.json").read_text(encoding="utf-8-sig"))["builds"]
    image = next(item["tag"] for item in builds if item["imageName"] == "recommendationservice")
    if not (CACHE / "kubeconfig").is_file():
        raise FileNotFoundError("Dedicated kubeconfig is missing")
    runtime = Runtime(args.namespace, image)
    print(f"INFO context={CONTEXT} namespace={args.namespace} run={runtime.run} cases={len(cases)}", flush=True)
    try:
        runtime.prepare(cases)
        runtime.execute(cases)
    except BaseException as error:
        runtime.failures += 1
        print(f"FAIL acceptance interrupted: {type(error).__name__}: {error}", flush=True)
    finally:
        runtime.cleanup()
    print(f"RESULT {'PASS' if runtime.failures == 0 else 'FAIL'} namespace={args.namespace}", flush=True)
    return 0 if runtime.failures == 0 else 1


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception as error:
        print(f"FAIL preflight: {error}", file=sys.stderr, flush=True)
        sys.exit(1)
