import assert from 'node:assert/strict';
import test from 'node:test';
import { validateRestrictedPod } from './kubernetes-security-checks.mjs';

function fixture() {
  const container = (name) => ({
    name,
    securityContext: {
      allowPrivilegeEscalation: false,
      readOnlyRootFilesystem: true,
      capabilities: { drop: ['ALL'] },
    },
    volumeMounts: [{ name: 'tmp', mountPath: '/tmp' }],
  });
  return {
    automountServiceAccountToken: false,
    securityContext: {
      runAsNonRoot: true,
      runAsUser: 65532,
      runAsGroup: 65532,
      seccompProfile: { type: 'RuntimeDefault' },
    },
    initContainers: [container('prepare')],
    containers: [container('application')],
    volumes: [{ name: 'tmp', emptyDir: {} }],
  };
}

test('restricted containers can inherit or explicitly select RuntimeDefault seccomp', () => {
  const spec = fixture();
  assert.deepEqual(validateRestrictedPod('service', spec), []);
  for (const container of [...spec.containers, ...spec.initContainers]) {
    container.securityContext.seccompProfile = { type: 'RuntimeDefault' };
    container.securityContext.capabilities.add = [];
    container.volumeMounts[0].readOnly = false;
  }
  assert.deepEqual(validateRestrictedPod('service', spec), []);
});

for (const collection of ['containers', 'initContainers']) {
  test(`${collection}: container overrides cannot bypass the security baseline`, () => {
    for (const type of ['Unconfined', 'Localhost']) {
      const spec = fixture();
      spec[collection][0].securityContext.seccompProfile = { type };
      assert.match(validateRestrictedPod('service', spec).join('\n'), /effective seccompProfile/);
    }
    const spec = fixture();
    spec[collection][0].securityContext.capabilities.add = ['NET_ADMIN'];
    assert.match(
      validateRestrictedPod('service', spec).join('\n'),
      /capabilities may not be added/,
    );
  });

  test(`${collection}: temporary storage must be the declared writable emptyDir`, () => {
    for (const change of [
      (spec) => {
        spec[collection][0].volumeMounts[0].readOnly = true;
      },
      (spec) => {
        spec[collection][0].volumeMounts[0].name = 'another-volume';
      },
      (spec) => {
        spec.volumes[0] = { name: 'tmp', hostPath: { path: '/tmp' } };
      },
    ]) {
      const spec = fixture();
      change(spec);
      assert.notDeepEqual(validateRestrictedPod('service', spec), []);
    }
  });
}

test('student cache must reference its writable emptyDir', () => {
  const spec = fixture();
  spec.containers[0].volumeMounts.push({
    name: 'next-cache',
    mountPath: '/app/apps/student-web/.next/cache',
  });
  spec.volumes.push({ name: 'next-cache', emptyDir: {} });
  assert.deepEqual(validateRestrictedPod('student-web', spec), []);
  for (const change of [
    (copy) => {
      copy.containers[0].volumeMounts[1].readOnly = true;
    },
    (copy) => {
      copy.containers[0].volumeMounts[1].name = 'tmp';
    },
    (copy) => {
      copy.volumes[1] = { name: 'next-cache', persistentVolumeClaim: { claimName: 'other' } };
    },
  ]) {
    const copy = structuredClone(spec);
    change(copy);
    assert.match(
      validateRestrictedPod('student-web', copy).join('\n'),
      /writable emptyDir for the Next.js cache/,
    );
  }
});
