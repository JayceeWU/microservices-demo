import fs from 'node:fs';
import path from 'node:path';
import process from 'node:process';
import { execFileSync } from 'node:child_process';
import yaml from 'js-yaml';
import { validateRestrictedPod } from './kubernetes-security-checks.mjs';
import {
  validateDevelopmentConnectivity,
  validateDevelopmentConfiguration,
} from './kubernetes-development-checks.mjs';

const FIRST_PARTY = new Set([
  'dancehub-migrations',
  'dancehub-seed',
  'gatewayservice',
  'accountservice',
  'catalogservice',
  'creditservice',
  'schedulingservice',
  'orderservice',
  'payrollservice',
  'paymentservice',
  'recommendationservice',
  'outbox-relay',
  'payment-order-saga',
  'cartservice',
  'chatservice',
  'chat-media-worker',
  'student-web',
  'teacher-web',
  'admin-web',
]);

const HARDENED_DEVELOPMENT_DEPENDENCIES = new Set(['keycloak', 'otel-collector', 'jaeger']);

// These images initialize persistent state as root before dropping privileges.
// They are local/dev-only and must never appear in the production Helm render.
const DEVELOPMENT_ROOT_EXCEPTIONS = new Map([
  ['postgres', 'official entrypoint creates and fixes ownership of the database volume'],
  ['redis-cart', 'official entrypoint fixes ownership of the persistent Redis data directory'],
  ['redis-flashsale', 'official entrypoint fixes ownership of the persistent Redis data directory'],
  ['redis-chat', 'official entrypoint fixes ownership of the persistent Redis data directory'],
  ['rabbitmq', 'official entrypoint initializes the broker data directory and cookie'],
  ['kafka', 'official entrypoint initializes and fixes ownership of broker storage'],
  ['minio', 'official entrypoint initializes and fixes ownership of object storage'],
  ['clamav', 'image updates the writable virus-signature database during startup'],
]);

const [mode, ...files] = process.argv.slice(2);
if (!['production', 'development'].includes(mode) || files.length === 0) {
  throw new Error(
    'usage: node scripts/check-kubernetes-security.mjs <production|development> <rendered.yaml> [...]',
  );
}

const failures = [];
const workloads = [];
const skaffold =
  mode === 'development'
    ? yaml.load(fs.readFileSync(new URL('../skaffold.yaml', import.meta.url), 'utf8'))
    : undefined;

function podSpecFor(document) {
  if (['Deployment', 'StatefulSet', 'DaemonSet', 'ReplicaSet', 'Job'].includes(document.kind)) {
    return document.spec?.template?.spec;
  }
  if (document.kind === 'CronJob') {
    return document.spec?.jobTemplate?.spec?.template?.spec;
  }
  return undefined;
}

function fail(name, message) {
  failures.push(`${name}: ${message}`);
}

function validateProbes(name, podSpec) {
  if (name === 'dancehub-migrations' || name === 'dancehub-seed') return;
  for (const container of podSpec.containers ?? []) {
    const containerName = `${name}/${container.name}`;
    for (const probeName of ['startupProbe', 'readinessProbe', 'livenessProbe']) {
      const probe = container[probeName];
      if (!probe) {
        fail(
          containerName,
          `${probeName} is required for every long-running first-party container`,
        );
        continue;
      }
      if (!probe.httpGet && !probe.exec && !probe.tcpSocket && !probe.grpc) {
        fail(containerName, `${probeName} must define a probe action`);
      }
    }
  }
}

for (const file of files) {
  const source = fs.readFileSync(file === '-' ? 0 : file, 'utf8');
  const documents = yaml
    .loadAll(source)
    .filter((document) => document && typeof document === 'object');
  documents.forEach((document) => {
    const name = document.metadata?.name ?? `${document.kind ?? 'unknown'} in ${file}`;

    if (mode === 'production' && document.kind === 'Secret' && document.stringData) {
      fail(name, 'production manifests may not contain Secret.stringData');
    }

    const podSpec = podSpecFor(document);
    if (!podSpec) return;
    workloads.push(name);

    if (mode === 'production') {
      const forbiddenImage = [
        ...(podSpec.initContainers ?? []),
        ...(podSpec.containers ?? []),
      ].find((container) =>
        /(^|\/)(postgres|redis|rabbitmq|kafka|keycloak|minio|clamav)(:|\/|$)/i.test(
          container.image ?? '',
        ),
      );
      if (forbiddenImage)
        fail(name, `production render contains local infrastructure image ${forbiddenImage.image}`);
      failures.push(...validateRestrictedPod(name, podSpec));
      if (FIRST_PARTY.has(name)) validateProbes(name, podSpec);
      return;
    }

    if (FIRST_PARTY.has(name) || HARDENED_DEVELOPMENT_DEPENDENCIES.has(name)) {
      failures.push(...validateRestrictedPod(name, podSpec));
      if (FIRST_PARTY.has(name)) validateProbes(name, podSpec);
    } else if (!DEVELOPMENT_ROOT_EXCEPTIONS.has(name)) {
      fail(name, 'workload is neither hardened nor listed as an explained local/dev exception');
    }
  });

  if (mode === 'development') {
    for (const failure of [
      ...validateDevelopmentConnectivity(documents),
      ...validateDevelopmentConfiguration(documents, skaffold),
    ])
      fail(path.basename(file), failure);
  }

  if (mode === 'production') {
    for (const pattern of [
      /dancehub-dev/i,
      /start-dev/i,
      /ENVIRONMENT[\s\S]{0,80}value:\s*["']?local/i,
    ]) {
      if (pattern.test(source))
        fail(path.basename(file), `production render contains forbidden pattern ${pattern}`);
    }
  }
}

if (mode === 'production') {
  const obsoleteOverlay = ['kustomize', 'overlays', 'prod'].join('/');
  if (fs.existsSync(path.join(...obsoleteOverlay.split('/')))) {
    fail('repository', `${obsoleteOverlay} must not exist`);
  }
  try {
    const references = execFileSync('git', ['grep', '-n', obsoleteOverlay, '--', '.'], {
      encoding: 'utf8',
      stdio: ['ignore', 'pipe', 'ignore'],
    }).trim();
    if (references)
      fail('repository', `obsolete production overlay is still referenced:\n${references}`);
  } catch (error) {
    if (error.status !== 1) throw error;
  }
}
if (workloads.length === 0) fail('render', 'no Kubernetes workloads were found');

if (failures.length > 0) {
  console.error(`Kubernetes security validation failed (${failures.length}):`);
  for (const failure of failures) console.error(`- ${failure}`);
  process.exit(1);
}

console.log(`Validated ${workloads.length} ${mode} workload(s).`);
if (mode === 'development') {
  console.log(
    `Approved local/dev exceptions: ${[...DEVELOPMENT_ROOT_EXCEPTIONS.keys()].join(', ')}`,
  );
}
