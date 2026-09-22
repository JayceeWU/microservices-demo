"""Prove the dedicated Kind CNI enforces both NetworkPolicy directions."""

import json
import os
from pathlib import Path
import subprocess
import time

OUTPUT = Path(os.environ['DANCEHUB_ACCEPTANCE_OUTPUT_DIR'])
BASE = ['kubectl', '--kubeconfig', str(OUTPUT / 'kubeconfig'),
        '--context', 'kind-dancehub-acceptance']
NAMESPACE = 'dancehub-cni-proof'


def kubectl(*args, data=None, check=True):
    result = subprocess.run(BASE + list(args), input=data, text=True,
                            capture_output=True, timeout=150)
    if check and result.returncode:
        raise RuntimeError(result.stdout + result.stderr)
    return result


def apply(document):
    return kubectl('apply', '-f', '-', data=json.dumps(document))


def main():
    if kubectl('get', 'namespace', NAMESPACE, '--ignore-not-found', '-o', 'name').stdout.strip():
        raise RuntimeError('CNI proof namespace already exists; refusing to adopt it')
    kubectl('create', 'namespace', NAMESPACE)
    try:
        for name, command in [('server', ['redis-server', '--save', '']), ('client', ['sleep', '900'])]:
            apply({'apiVersion': 'v1', 'kind': 'Pod',
                   'metadata': {'name': name, 'namespace': NAMESPACE, 'labels': {'role': name}},
                   'spec': {'automountServiceAccountToken': False, 'restartPolicy': 'Never',
                            'containers': [{'name': name, 'image': 'redis:7-alpine',
                                            'imagePullPolicy': 'Never', 'command': command}]}})
        kubectl('-n', NAMESPACE, 'wait', '--for=condition=Ready', 'pod', '--all', '--timeout=120s')
        address = json.loads(kubectl('-n', NAMESPACE, 'get', 'pod', 'server', '-o', 'json').stdout)['status']['podIP']

        def probe():
            return kubectl('-n', NAMESPACE, 'exec', 'client', '--',
                           'timeout', '4', 'redis-cli', '-h', address, 'ping', check=False)

        baseline = probe()
        if baseline.returncode != 0 or 'PONG' not in baseline.stdout:
            raise RuntimeError('CNI baseline did not connect; no denial can be accepted')
        print('PASS CNI: unrestricted Pod-to-Pod Redis PING', flush=True)
        for direction, selector in [('Ingress', 'server'), ('Egress', 'client')]:
            apply({'apiVersion': 'networking.k8s.io/v1', 'kind': 'NetworkPolicy',
                   'metadata': {'name': 'proof-deny', 'namespace': NAMESPACE},
                   'spec': {'podSelector': {'matchLabels': {'role': selector}}, 'policyTypes': [direction]}})
            time.sleep(3)
            denied = probe()
            if denied.returncode not in (124, 143):
                raise RuntimeError(f'CNI {direction} denial did not time out: exit={denied.returncode}')
            print(f'PASS CNI: {direction} denied with a timed-out TCP connection', flush=True)
            kubectl('-n', NAMESPACE, 'delete', 'networkpolicy', 'proof-deny')
            time.sleep(3)
            restored = probe()
            if restored.returncode != 0 or 'PONG' not in restored.stdout:
                raise RuntimeError(f'CNI traffic did not recover after removing {direction} denial')
            print(f'PASS CNI: connection restored after {direction} policy removal', flush=True)
    finally:
        kubectl('-n', NAMESPACE, 'get', 'pods,events', '-o', 'wide', check=False)
        kubectl('delete', 'namespace', NAMESPACE, '--wait=true', '--timeout=120s')


if __name__ == '__main__':
    main()
