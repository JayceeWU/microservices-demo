export const KEYCLOAK_READINESS_COMMAND = String.raw`exec 3<>/dev/tcp/127.0.0.1/9000;
printf 'GET /health/ready HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n' >&3;
IFS= read -r status <&3;
[[ "$status" == *' 200 '* ]]`;

function seconds(value) {
  const units = { h: 3600, m: 60, s: 1, ms: 0.001, us: 0.000001, ns: 0.000000001 };
  const source = String(value ?? '');
  const parts = [...source.matchAll(/(\d+(?:\.\d+)?)(h|ms|us|ns|m|s)/g)];
  if (!parts.length || parts.map((part) => part[0]).join('') !== source) return NaN;
  return parts.reduce((total, part) => total + Number(part[1]) * units[part[2]], 0);
}

export function validateComposeKeycloak(service) {
  const failures = [];
  const health = service?.healthcheck ?? {};
  if (health.disable === true) failures.push('keycloak: healthcheck must remain enabled');
  const normalize = (command) => String(command).replaceAll('$$', '$').replace(/\s+/g, ' ').trim();
  if (
    JSON.stringify(health.test?.slice(0, 3)) !== JSON.stringify(['CMD', 'bash', '-ec']) ||
    normalize(health.test?.[3]) !== normalize(KEYCLOAK_READINESS_COMMAND)
  )
    failures.push(
      'keycloak: healthcheck must require HTTP 200 from the management /health/ready endpoint',
    );
  if (String(service?.environment?.KC_HEALTH_ENABLED) !== 'true')
    failures.push('keycloak: KC_HEALTH_ENABLED must be true');
  const grace = seconds(health.start_period);
  const window = grace + seconds(health.interval) * health.retries;
  if (!(grace >= 180 && window >= 300))
    failures.push(
      'keycloak: reserve at least 180 seconds for import/startup and 300 seconds before unhealthy',
    );
  return failures;
}
