"""Redis AOF/PVC acceptance helper for the disposable local acceptance cluster.

Run from the repository with the dedicated kubeconfig already populated:
  python tests/acceptance/redis-runtime.py --namespace default prepare
  # The caller can now run Skaffold delete/deploy against the same namespace.
  python tests/acceptance/redis-runtime.py --namespace default verify
  python tests/acceptance/redis-runtime.py --namespace default cleanup

prepare recreates the Redis Pod and then its StatefulSet; verify only checks and
claims the saved test message. cleanup deletes exactly two saved acceptance keys.
No command in this helper deletes a PVC, PV, namespace, or business Stream.
The live StatefulSet must retain claims on deletion and scale-down before any
controller/Pod deletion. A sanitized recovery manifest and test state stay here.

This is a graceful restart/redeployment acceptance test, not a power-loss test.
Redis 7 does not universally provide WAITAOF (introduced in 7.2): we require the
configured everysec AOF, wait at least two seconds after writes, and inspect INFO
persistence for successful writes and no pending fsync before restarting.
"""

import argparse
import copy
import datetime
import json
import os
import re
import subprocess
import sys
import time
import uuid
from pathlib import Path


REPO = Path(__file__).resolve().parents[2]
ROOT = Path(os.environ.get('DANCEHUB_ACCEPTANCE_OUTPUT_DIR', REPO / '.cache' / 'acceptance'))
CONTEXT = "kind-dancehub-acceptance"
KUBECONFIG = ROOT / "kubeconfig"
STATEFULSET = "redis-flashsale"
POD = "redis-flashsale-0"
PVC = "data-redis-flashsale-0"
CONTAINER = "redis"


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def timestamp():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()


def save_json(path, document):
    temporary = path.with_suffix(path.suffix + ".tmp")
    temporary.write_text(json.dumps(document, indent=2) + "\n", encoding="utf-8")
    temporary.replace(path)


def fields(value):
    """Handle both RESP2 alternating key/value arrays and RESP3 maps."""
    if isinstance(value, dict):
        return value
    require(isinstance(value, list) and len(value) % 2 == 0,
            f"Expected a Redis field map, got {value!r}")
    return dict(zip(value[::2], value[1::2]))


def info_fields(value):
    require(isinstance(value, str), "INFO did not return a string")
    return dict(line.split(":", 1) for line in value.splitlines()
                if line and not line.startswith("#") and ":" in line)


def recovery_manifest(document):
    """Keep declarative metadata, dropping API status and runtime metadata."""
    result = copy.deepcopy(document)
    result.pop("status", None)

    def clean_metadata(resource):
        metadata = resource.get("metadata", {})
        resource["metadata"] = {key: metadata[key] for key in
                                ("name", "namespace", "labels", "annotations")
                                if key in metadata}
        resource["metadata"].get("annotations", {}).pop(
            "kubectl.kubernetes.io/last-applied-configuration", None)

    clean_metadata(result)
    clean_metadata(result["spec"]["template"])
    for template in result["spec"].get("volumeClaimTemplates", []):
        clean_metadata(template)
        template.pop("status", None)
    return result


class Acceptance:
    def __init__(self, namespace):
        self.namespace = namespace
        self.state_path = ROOT / f"redis-runtime-{namespace}.json"
        self.manifest_path = ROOT / f"redis-statefulset-{namespace}.json"
        self.base = ["kubectl", "--kubeconfig", str(KUBECONFIG),
                     "--context", CONTEXT, "--request-timeout=30s"]
        self.state = None

    def kubectl(self, *arguments, data=None, timeout=60):
        result = subprocess.run(self.base + list(arguments), input=data, text=True,
                                capture_output=True, timeout=timeout)
        require(result.returncode == 0,
                f"kubectl {' '.join(arguments)} failed:\n"
                f"{result.stdout}{result.stderr}")
        return result.stdout

    def get(self, kind, name, optional=False, namespaced=True):
        arguments = ["-n", self.namespace] if namespaced else []
        arguments += ["get", kind, name, "-o", "json"]
        if optional:
            arguments.append("--ignore-not-found=true")
        output = self.kubectl(*arguments)
        return json.loads(output) if output.strip() else None

    def redis(self, *arguments):
        # --json defaults to RESP3, where XREADGROUP returns a map. Pin RESP2
        # for every command so Stream replies consistently use nested arrays;
        # CONFIG GET and XINFO GROUPS then use alternating field/value arrays.
        output = self.kubectl("-n", self.namespace, "exec", POD, "-c", CONTAINER,
                              "--", "redis-cli", "-2", "--json", *map(str, arguments))
        # redis-cli prints INFO as raw text even in JSON output mode.
        if arguments[0] == "INFO" and output.startswith("# Persistence"):
            return output
        try:
            result = json.loads(output)
        except json.JSONDecodeError as error:
            raise RuntimeError(f"redis-cli {arguments[0]} returned {output!r}") from error
        # redis-cli --json represents a Redis error as an error object on some
        # versions, while others emit non-JSON text; neither should pass checks.
        require(not (isinstance(result, dict) and "error" in result),
                f"Redis {arguments[0]} failed: {result!r}")
        return result

    def save(self, phase=None):
        if phase is not None:
            self.state["phase"] = phase
        self.state["updated_at"] = timestamp()
        save_json(self.state_path, self.state)

    def load(self):
        require(self.state_path.is_file(), f"No saved state: {self.state_path}")
        self.state = json.loads(self.state_path.read_text(encoding="utf-8"))
        require(self.state.get("namespace") == self.namespace
                and self.state.get("context") == CONTEXT,
                "Saved state belongs to another namespace/context")
        expected_prefix = f"acceptance:kubernetes:{self.namespace}:"
        prefix = self.state.get("prefix", "")
        require(prefix.startswith(expected_prefix)
                and re.fullmatch(r"[0-9a-f]{32}", prefix[len(expected_prefix):]),
                "Saved state has an invalid acceptance prefix")
        require(self.state.get("key") == prefix + ":key"
                and self.state.get("stream") == prefix + ":stream"
                and self.state.get("group") == prefix + ":group",
                "Saved state key/group names do not match its unique prefix")

    def wait_ready(self, previous_uid=None, timeout=180):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            pod = self.get("pod", POD, optional=True)
            if pod and not pod["metadata"].get("deletionTimestamp"):
                ready = any(item.get("type") == "Ready" and item.get("status") == "True"
                            for item in pod.get("status", {}).get("conditions", []))
                if ready and pod["metadata"]["uid"] != previous_uid:
                    require(self.redis("PING") == "PONG", "Redis failed PING")
                    return pod
            time.sleep(1)
        raise RuntimeError(f"{POD} did not become Ready with a new UID within {timeout}s")

    def volume_identity(self):
        pod = self.get("pod", POD)
        mounted = [volume.get("persistentVolumeClaim", {}).get("claimName")
                   for volume in pod["spec"].get("volumes", [])
                   if volume.get("name") == "data"]
        require(mounted == [PVC], f"Redis /data does not use expected PVC: {mounted!r}")
        container = next((item for item in pod["spec"]["containers"]
                          if item["name"] == CONTAINER), None)
        require(container and any(mount.get("name") == "data"
                                  and mount.get("mountPath") == "/data"
                                  and not mount.get("readOnly", False)
                                  for mount in container.get("volumeMounts", [])),
                "Redis container has no writable /data claim mount")
        claim = self.get("pvc", PVC)
        require(claim.get("status", {}).get("phase") == "Bound", "Redis PVC is not Bound")
        require(not claim["metadata"].get("deletionTimestamp"), "Redis PVC is deleting")
        require(not claim["metadata"].get("ownerReferences"),
                "Redis PVC has ownerReferences; refuse a deletion that could garbage-collect it")
        pv_name = claim["spec"]["volumeName"]
        volume = self.get("pv", pv_name, namespaced=False)
        reference = volume["spec"].get("claimRef", {})
        require(reference.get("uid") == claim["metadata"]["uid"]
                and reference.get("name") == PVC
                and reference.get("namespace") == self.namespace,
                "PV claimRef does not match this PVC")
        return {"pvc_name": PVC, "pvc_uid": claim["metadata"]["uid"],
                "pv_name": pv_name, "pv_uid": volume["metadata"]["uid"]}

    def retained_statefulset(self):
        controller = self.get("statefulset", STATEFULSET)
        require(controller["spec"].get("replicas") == 1, "Expected one Redis replica")
        retention = controller["spec"].get("persistentVolumeClaimRetentionPolicy", {})
        require(retention.get("whenDeleted", "Retain") == "Retain"
                and retention.get("whenScaled", "Retain") == "Retain",
                "Refusing restart: StatefulSet must retain PVCs")
        return controller

    def flush_aof(self, checkpoint):
        configuration = fields(self.redis("CONFIG", "GET", "appendonly", "appendfsync", "dir"))
        require(configuration.get("appendonly") == "yes"
                and configuration.get("appendfsync") == "everysec"
                and configuration.get("dir") == "/data",
                f"Unexpected Redis persistence configuration: {configuration!r}")
        started = time.monotonic()
        time.sleep(2.1)
        deadline = started + 30
        while time.monotonic() < deadline:
            persistence = info_fields(self.redis("INFO", "persistence"))
            require(persistence.get("aof_enabled") == "1", "AOF is disabled")
            require(persistence.get("aof_last_write_status") == "ok", "AOF write failed")
            require(persistence.get("aof_last_bgrewrite_status") == "ok", "AOF rewrite failed")
            if (persistence.get("aof_pending_bio_fsync") == "0"
                    and persistence.get("aof_rewrite_in_progress") == "0"
                    and persistence.get("aof_buffer_length", "0") == "0"):
                observation = {key: value for key, value in persistence.items()
                               if key.startswith("aof_")}
                observation["waited_seconds"] = round(time.monotonic() - started, 3)
                self.state.setdefault("aof_checks", []).append(
                    {"checkpoint": checkpoint, "at": timestamp(), **observation})
                self.save()
                print(f"PASS {checkpoint}: everysec AOF writes OK, no pending fsync", flush=True)
                return
            time.sleep(1)
        raise RuntimeError("AOF did not reach a successful, idle persistence state within 30s")

    def verify_and_claim(self, checkpoint):
        pod = self.wait_ready()
        controller = self.retained_statefulset()
        identity = self.volume_identity()
        require(identity == self.state["volume"],
                f"PVC/PV identity changed: expected {self.state['volume']!r}, got {identity!r}")
        require(self.redis("GET", self.state["key"]) == self.state["value"],
                "Unique acceptance value did not survive")
        stream, group, entry_id = (self.state[name] for name in ("stream", "group", "entry_id"))
        groups = [fields(item) for item in self.redis("XINFO", "GROUPS", stream)]
        require(any(item.get("name") == group for item in groups),
                "Acceptance consumer group did not survive")
        pending = self.redis("XPENDING", stream, group)
        require(pending[0] == 1 and pending[1:3] == [entry_id, entry_id],
                f"Expected one retained pending message: {pending!r}")
        detail = self.redis("XPENDING", stream, group, "-", "+", 10)
        require(len(detail) == 1 and detail[0][0] == entry_id
                and detail[0][1] == self.state["pending_consumer"],
                f"Pending owner changed or was lost: {detail!r}")
        messages = self.redis("XRANGE", stream, entry_id, entry_id)
        require(len(messages) == 1 and fields(messages[0][1]) == {"acceptance": self.state["value"]},
                f"Acceptance Stream payload did not survive: {messages!r}")
        consumer = self.state["prefix"] + ":claim:" + uuid.uuid4().hex
        claimed = self.redis("XAUTOCLAIM", stream, group, consumer, 0, "0-0", "COUNT", 1)
        require(len(claimed) >= 2 and len(claimed[1]) == 1
                and claimed[1][0][0] == entry_id
                and fields(claimed[1][0][1]) == {"acceptance": self.state["value"]}
                and (len(claimed) < 3 or not claimed[2]),
                f"XAUTOCLAIM failed to recover the pending entry: {claimed!r}")
        # Persist ownership immediately so a later failure can be retried. Do not
        # acknowledge the entry: the next restart must recover the same pending ID.
        self.state["pending_consumer"] = consumer
        self.save()
        detail = self.redis("XPENDING", stream, group, "-", "+", 10)
        require(len(detail) == 1 and detail[0][0] == entry_id and detail[0][1] == consumer,
                f"XAUTOCLAIM did not preserve one pending entry: {detail!r}")
        self.state.setdefault("checks", []).append(
            {"checkpoint": checkpoint, "at": timestamp(), "volume": identity,
             "pod_uid": pod["metadata"]["uid"],
             "statefulset_uid": controller["metadata"]["uid"],
             "pending_entry_id": entry_id, "pending_consumer": consumer})
        self.save()
        self.flush_aof(checkpoint)
        print(f"PASS {checkpoint}: same PVC/PV, value, group, pending ID; XAUTOCLAIM succeeded", flush=True)

    def prepare(self):
        if self.state_path.exists():
            self.load()
            require(self.state.get("phase") == "cleaned",
                    "Saved acceptance state already exists; use verify or cleanup first")
        pod = self.wait_ready()
        controller = self.retained_statefulset()
        identity = self.volume_identity()
        prefix = f"acceptance:kubernetes:{self.namespace}:{uuid.uuid4().hex}"
        self.state = {"version": 1, "context": CONTEXT, "namespace": self.namespace,
                      "created_at": timestamp(), "prefix": prefix,
                      "key": prefix + ":key", "stream": prefix + ":stream",
                      "group": prefix + ":group", "value": uuid.uuid4().hex,
                      "pending_consumer": prefix + ":original", "volume": identity,
                      "initial_pod_uid": pod["metadata"]["uid"],
                      "initial_statefulset_uid": controller["metadata"]["uid"]}
        self.save("creating-test-data")
        require(self.redis("SET", self.state["key"], self.state["value"], "NX") == "OK",
                "Acceptance key already exists")
        require(self.redis("EXISTS", self.state["stream"]) == 0, "Acceptance Stream already exists")
        self.state["entry_id"] = self.redis("XADD", self.state["stream"], "*",
                                             "acceptance", self.state["value"])
        self.save()
        require(self.redis("XGROUP", "CREATE", self.state["stream"], self.state["group"], "0") == "OK",
                "Could not create acceptance consumer group")
        delivered = self.redis("XREADGROUP", "GROUP", self.state["group"],
                               self.state["pending_consumer"], "COUNT", 1,
                               "STREAMS", self.state["stream"], ">")
        require(delivered and delivered[0][0] == self.state["stream"]
                and delivered[0][1][0][0] == self.state["entry_id"],
                f"Could not create the initial pending delivery: {delivered!r}")
        self.flush_aof("initial-write")
        self.save("recreating-pod")
        self.kubectl("-n", self.namespace, "delete", "pod", POD,
                     "--wait=true", "--timeout=120s", timeout=150)
        self.wait_ready(previous_uid=pod["metadata"]["uid"])
        self.verify_and_claim("pod-recreation")

        controller = self.retained_statefulset()
        self.volume_identity()  # Recheck claim ownership immediately before deletion.
        previous_pod_uid = self.get("pod", POD)["metadata"]["uid"]
        manifest = recovery_manifest(controller)
        save_json(self.manifest_path, manifest)
        self.state["recovery_manifest"] = str(self.manifest_path)
        self.save("recreating-statefulset")
        self.kubectl("-n", self.namespace, "delete", "statefulset", STATEFULSET,
                     "--cascade=foreground", "--wait=true", "--timeout=120s", timeout=150)
        self.save("statefulset-deleted")
        self.kubectl("-n", self.namespace, "apply", "-f", "-", data=json.dumps(manifest))
        self.wait_ready(previous_uid=previous_pod_uid)
        replacement = self.retained_statefulset()
        require(replacement["metadata"]["uid"] != controller["metadata"]["uid"],
                "StatefulSet UID did not change")
        self.verify_and_claim("statefulset-recreation")
        self.save("prepared")
        print(f"PREPARED: pending message retained for later verify; state: {self.state_path}", flush=True)

    def verify(self):
        self.load()
        require(self.state.get("phase") != "cleaned", "Acceptance data was already cleaned")
        require("entry_id" in self.state, "Prepare did not create a Stream entry")
        attempt = self.state.get("verify_count", 0) + 1
        self.verify_and_claim(f"external-redeploy-{attempt}")
        self.state["verify_count"] = attempt
        self.save("verified")

    def cleanup(self):
        self.load()
        if self.state.get("phase") == "cleaned":
            print("CLEANED: acceptance keys were already removed; no action", flush=True)
            return
        self.wait_ready()
        require(self.volume_identity() == self.state["volume"],
                "PVC/PV changed; refusing to clean data on a different volume")
        removed = self.redis("DEL", self.state["key"], self.state["stream"])
        require(self.redis("EXISTS", self.state["key"], self.state["stream"]) == 0,
                "Acceptance keys still exist after cleanup")
        self.state["removed_keys"] = removed
        self.flush_aof("cleanup")
        self.save("cleaned")
        print(f"CLEANED: removed {removed} acceptance keys; PVC/PV and business data retained", flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--namespace", required=True, help="Existing validation namespace")
    parser.add_argument("step", choices=("prepare", "verify", "cleanup"))
    arguments = parser.parse_args()
    require(re.fullmatch(r"[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?", arguments.namespace),
            "Namespace must be a Kubernetes DNS label")
    require(KUBECONFIG.is_file(), f"Dedicated validation kubeconfig is absent: {KUBECONFIG}")
    acceptance = Acceptance(arguments.namespace)
    getattr(acceptance, arguments.step)()


if __name__ == "__main__":
    try:
        main()
    except (RuntimeError, OSError, subprocess.TimeoutExpired, ValueError) as error:
        print(f"FAIL: {error}", file=sys.stderr, flush=True)
        print("Saved state and any StatefulSet recovery manifest remain in the helper directory.",
              file=sys.stderr, flush=True)
        sys.exit(1)
