function isPositiveInteger(value) {
  return Number.isInteger(value) && value > 0;
}

export function validateRestrictedPod(name, podSpec) {
  const failures = [];
  const fail = (target, message) => failures.push(`${target}: ${message}`);
  const podSecurity = podSpec.securityContext ?? {};
  if (podSpec.automountServiceAccountToken !== false) {
    fail(name, 'automountServiceAccountToken must be false');
  }
  if (podSecurity.runAsNonRoot !== true) fail(name, 'pod runAsNonRoot must be true');
  if (!isPositiveInteger(podSecurity.runAsUser))
    fail(name, 'pod runAsUser must be a non-zero number');
  if (!isPositiveInteger(podSecurity.runAsGroup))
    fail(name, 'pod runAsGroup must be a non-zero number');
  if (podSecurity.seccompProfile?.type !== 'RuntimeDefault') {
    fail(name, 'pod seccompProfile.type must be RuntimeDefault');
  }

  for (const container of [...(podSpec.initContainers ?? []), ...(podSpec.containers ?? [])]) {
    const containerName = `${name}/${container.name}`;
    const security = container.securityContext ?? {};
    const runAsUser = security.runAsUser ?? podSecurity.runAsUser;
    const runAsGroup = security.runAsGroup ?? podSecurity.runAsGroup;
    const runAsNonRoot = security.runAsNonRoot ?? podSecurity.runAsNonRoot;
    const seccomp = security.seccompProfile ?? podSecurity.seccompProfile;
    if (seccomp?.type !== 'RuntimeDefault') {
      fail(containerName, 'effective seccompProfile.type must be RuntimeDefault');
    }
    if (runAsNonRoot !== true) fail(containerName, 'effective runAsNonRoot must be true');
    if (!isPositiveInteger(runAsUser))
      fail(containerName, 'effective runAsUser must be a non-zero number');
    if (!isPositiveInteger(runAsGroup))
      fail(containerName, 'effective runAsGroup must be a non-zero number');
    if (security.allowPrivilegeEscalation !== false) {
      fail(containerName, 'allowPrivilegeEscalation must be false');
    }
    if (security.readOnlyRootFilesystem !== true) {
      fail(containerName, 'readOnlyRootFilesystem must be true');
    }
    if (!(security.capabilities?.drop ?? []).includes('ALL')) {
      fail(containerName, 'all Linux capabilities must be dropped');
    }
    if ((security.capabilities?.add ?? []).length > 0) {
      fail(containerName, 'capabilities may not be added after dropping ALL');
    }
    if (
      !(container.volumeMounts ?? []).some(
        (mount) => mount.mountPath === '/tmp' && mount.name === 'tmp' && mount.readOnly !== true,
      )
    ) {
      fail(containerName, 'must mount a writable /tmp volume');
    }
  }

  const tmpVolume = (podSpec.volumes ?? []).find((volume) => volume.name === 'tmp');
  if (!tmpVolume || tmpVolume.emptyDir === undefined) fail(name, 'must declare tmp as an emptyDir');
  if (name === 'student-web') {
    const cacheMount = podSpec.containers?.[0]?.volumeMounts?.find(
      (mount) => mount.mountPath === '/app/apps/student-web/.next/cache',
    );
    const cacheVolume = (podSpec.volumes ?? []).find((volume) => volume.name === 'next-cache');
    if (
      !cacheMount ||
      cacheMount.name !== 'next-cache' ||
      cacheMount.readOnly === true ||
      !cacheVolume ||
      cacheVolume.emptyDir === undefined
    ) {
      fail(name, 'must provide a writable emptyDir for the Next.js cache');
    }
  }
  return failures;
}
