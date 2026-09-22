import { execFileSync } from 'node:child_process';
import { validateComposeKeycloak } from './compose-startup-checks.mjs';

const FIRST_PARTY = [
  'migrations',
  'seed',
  'accountservice',
  'catalogservice',
  'schedulingservice',
  'orderservice',
  'payrollservice',
  'gatewayservice',
  'chatservice',
  'chat-media-worker',
  'paymentservice',
  'recommendationservice',
  'outbox-relay',
  'payment-order-saga',
  'creditservice',
  'cartservice',
  'student-web',
  'teacher-web',
  'admin-web',
];

const rendered = execFileSync(
  'docker',
  ['compose', '--profile', 'full', 'config', '--format', 'json'],
  { encoding: 'utf8' },
);
const compose = JSON.parse(rendered);
const failures = [];
failures.push(...validateComposeKeycloak(compose.services?.keycloak));

for (const name of FIRST_PARTY) {
  const service = compose.services?.[name];
  if (!service) {
    failures.push(`${name}: missing from the full Compose profile`);
    continue;
  }
  if (service.read_only !== true) failures.push(`${name}: read_only must be true`);
  if (!(service.cap_drop ?? []).includes('ALL'))
    failures.push(`${name}: cap_drop must contain ALL`);
  if (!(service.security_opt ?? []).includes('no-new-privileges:true')) {
    failures.push(`${name}: no-new-privileges must be enabled`);
  }
  if (!(service.tmpfs ?? []).some((mount) => mount.startsWith('/tmp:'))) {
    failures.push(`${name}: must mount a writable /tmp tmpfs`);
  }
  if (!['migrations', 'seed'].includes(name) && !(service.healthcheck?.test ?? []).length) {
    failures.push(`${name}: long-running service must define a healthcheck`);
  }
}

for (const name of ['jaeger', 'otel-collector', 'prometheus']) {
  if (compose.services?.[name]?.restart !== 'unless-stopped') {
    failures.push(`${name}: restart must be unless-stopped`);
  }
}

if (
  !(compose.services['student-web']?.tmpfs ?? []).some((mount) =>
    mount.startsWith('/app/apps/student-web/.next/cache:'),
  )
) {
  failures.push('student-web: must mount a writable Next.js cache tmpfs');
}

if (
  !(compose.services['student-web']?.healthcheck?.test ?? []).join(' ').includes('127.0.0.1:3000/')
) {
  failures.push('student-web: healthcheck must target its port 3000 HTTP endpoint');
}

if (failures.length > 0) {
  console.error(`Compose security validation failed (${failures.length}):`);
  for (const failure of failures) console.error(`- ${failure}`);
  process.exit(1);
}

console.log(`Validated ${FIRST_PARTY.length} first-party Compose services.`);
