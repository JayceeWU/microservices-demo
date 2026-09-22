import assert from 'node:assert/strict';
import test from 'node:test';
import fs from 'node:fs/promises';
import path from 'node:path';
import os from 'node:os';
import { createRequire } from 'node:module';
import {
  parseArgs,
  withCleanup,
  snapshot,
  changedFiles,
  composeDefaults,
  retryNpmCi,
  retryBufGenerate,
  workloadImages,
} from './verify-local.mjs';
import { validateComposeKeycloak } from './compose-startup-checks.mjs';
import {
  artifact,
  toolchainArtifact,
  checksumFor,
  digest,
  fetchBytes,
  installConcurrently,
} from './acceptance-tools.mjs';

const require = createRequire(import.meta.url);
const { metricEvidence } = require('../tests/acceptance/telemetry-runtime.cjs');

test('Kind image preload includes CNI init containers and excludes locally built applications', () => {
  const spec = {
    initContainers: [{ image: 'calico/cni:v3' }],
    containers: [{ image: 'calico/node:v3' }, { image: 'local-app' }],
  };
  const documents = [
    null,
    { kind: 'ConfigMap' },
    { kind: 'DaemonSet', spec: { template: { spec } } },
    { kind: 'Pod', spec: { containers: [{ image: 'calico/node:v3' }] } },
    {
      kind: 'CronJob',
      spec: {
        jobTemplate: { spec: { template: { spec: { containers: [{ image: 'backup:v1' }] } } } },
      },
    },
  ];
  assert.deepEqual(workloadImages(documents, ['local-app']), [
    'calico/cni:v3',
    'calico/node:v3',
    'backup:v1',
  ]);
});

test('Compose Keycloak waits through cold import and requires ready HTTP status', async () => {
  const service = require('js-yaml').load(await fs.readFile('compose.yaml', 'utf8')).services
    .keycloak;
  assert.deepEqual(validateComposeKeycloak(service), []);
  for (const mutate of [
    (value) => {
      value.healthcheck.start_period = '10s';
    },
    (value) => {
      value.healthcheck.retries = 1;
    },
    (value) => {
      value.healthcheck.disable = true;
    },
    (value) => {
      value.environment.KC_HEALTH_ENABLED = 'false';
    },
    (value) => {
      value.healthcheck.test = ['CMD-SHELL', "bash -ec 'exec 3<>/dev/tcp/127.0.0.1/8080'"];
    },
    (value) => {
      value.healthcheck.test[3] = value.healthcheck.test[3].replace(' 200 ', ' 503 ');
    },
  ]) {
    const changed = structuredClone(service);
    mutate(changed);
    assert.ok(validateComposeKeycloak(changed).length);
  }
});

test('npm ci retries only fatal EBUSY/EPERM, caps attempts and preserves the final error', async () => {
  const pauses = [],
    messages = [];
  let calls = 0;
  const error = new Error('npm ci failed');
  const options = {
    pause: async (delay) => pauses.push(delay),
    log: (message) => messages.push(message),
  };
  error.stderr = 'npm error code EBUSY\nnpm error syscall rmdir';
  assert.equal(
    await retryNpmCi(async () => {
      if (++calls < 3) throw error;
      return 'installed';
    }, options),
    'installed',
  );
  assert.deepEqual(pauses, [5000, 10000]);
  assert.equal(messages.length, 2);
  calls = 0;
  error.stderr = 'npm ERR! code EPERM';
  await assert.rejects(
    retryNpmCi(async () => {
      calls++;
      throw error;
    }, options),
    (failure) => failure === error,
  );
  assert.equal(calls, 3);
  calls = 0;
  error.stderr = 'npm warn cleanup EPERM\nnpm error code ECONNRESET';
  await assert.rejects(
    retryNpmCi(async () => {
      calls++;
      throw error;
    }, options),
    (failure) => failure === error,
  );
  assert.equal(calls, 1);
});

test('Buf generation retries explicit rate limits with bounded backoff and visible progress', async () => {
  const pauses = [],
    messages = [];
  let calls = 0;
  const error = Object.assign(new Error('buf exited 1'), {
    stderr:
      'Failure: resource_exhausted: too many requests\nPlease see https://buf.build/docs/bsr/rate-limits/',
  });
  const result = await retryBufGenerate(
    async () => {
      if (++calls < 5) throw error;
      return 'generated';
    },
    {
      pause: async (delay) => pauses.push(delay),
      log: (message) => messages.push(message),
    },
  );
  assert.equal(result, 'generated');
  assert.equal(calls, 5);
  assert.equal(
    pauses.reduce((sum, delay) => sum + delay, 0),
    450_000,
  );
  assert.ok(pauses.every((delay) => delay <= 30_000));
  assert.deepEqual(
    messages
      .filter((message) => message.includes('was rate limited'))
      .map((message) => message.match(/in (\d+)s/)[1]),
    ['30', '60', '120', '240'],
  );
  assert.ok(messages.some((message) => /backoff: 180s/.test(message)));
});

test('Buf generation preserves final errors and does not retry other failures', async () => {
  for (const [stderr, expectedCalls] of [
    ['Failure: resource_exhausted: too many requests', 5],
    ['Failure: resource_exhausted: plugin response exceeds size limit', 1],
    ['Failure: unavailable: remote plugin failed', 1],
    ['protos/service.proto:1:1: unexpected identifier', 1],
    ['Failure: unauthenticated: invalid token', 1],
  ]) {
    const error = Object.assign(new Error('buf exited 1'), { stderr });
    let calls = 0;
    await assert.rejects(
      retryBufGenerate(
        async () => {
          calls++;
          throw error;
        },
        { pause: async () => {}, log: () => {} },
      ),
      (failure) => failure === error,
    );
    assert.equal(calls, expectedCalls, stderr);
  }
});

test('Buf backoff stops after an interruption without another remote request', async () => {
  let interrupted = false,
    calls = 0;
  await assert.rejects(
    retryBufGenerate(
      async () => {
        calls++;
        throw Object.assign(new Error('buf exited 1'), {
          stderr: 'Failure: resource_exhausted: too many requests',
        });
      },
      {
        pause: async () => {
          interrupted = true;
        },
        log: () => {},
        interrupted: () => interrupted,
      },
    ),
    /Acceptance interrupted/,
  );
  assert.equal(calls, 1);
});

test('tool installation limits parallelism and waits for active workers before returning failure', async () => {
  let active = 0,
    maximum = 0,
    completed = 0;
  await assert.rejects(
    installConcurrently([1, 2, 3, 4, 5], 3, async (value) => {
      active++;
      maximum = Math.max(maximum, active);
      await new Promise((resolve) => setTimeout(resolve, 5));
      active--;
      completed++;
      if (value === 2) throw new Error('download failed');
    }),
    AggregateError,
  );
  assert.equal(maximum, 3);
  assert.equal(active, 0);
  assert.equal(completed, 5);
});

test('downloads stream exact bytes and report retries instead of silently restarting', async () => {
  const messages = [];
  const bytes = Buffer.from([0, 255, 1, 42]);
  let calls = 0;
  const result = await fetchBytes('https://example.invalid/tool.zip', {
    attempts: 2,
    platform: 'linux',
    log: (message) => messages.push(message),
    fetchImpl: async () =>
      ++calls === 1 ? new Response('', { status: 503 }) : new Response(bytes),
  });
  assert.deepEqual(result, bytes);
  assert.equal(calls, 2);
  assert.ok(messages.some((message) => /attempt 1 failed.*503/.test(message)));
  assert.ok(messages.some((message) => /MiB/.test(message)));
});

test('an idle download aborts with a visible cause and releases its timers', async () => {
  const messages = [];
  await assert.rejects(
    fetchBytes('https://example.invalid/stalled.zip', {
      attempts: 1,
      platform: 'linux',
      idleTimeout: 10,
      log: (message) => messages.push(message),
      fetchImpl: async (_url, { signal }) =>
        new Promise((_resolve, reject) =>
          signal.addEventListener('abort', () => reject(signal.reason), { once: true }),
        ),
    }),
    /received no data/,
  );
  assert.ok(messages.some((message) => /attempt 1 failed.*received no data/.test(message)));
});

test('disposable Compose fixture takes declared defaults and ignores escaped container variables', () => {
  assert.deepEqual(
    composeDefaults(
      'A: ${AUTH_MODE:-oidc}\nB: ${STRIPE_SECRET_KEY:-}\nC: $${INSIDE_CONTAINER:-keep}',
    ),
    { AUTH_MODE: 'oidc', STRIPE_SECRET_KEY: '' },
  );
  assert.throws(() => composeDefaults('${AUTH_MODE:-oidc} ${AUTH_MODE:-dev}'), /Conflicting/);
});

test('entry point limits modes and overlays without starting external commands', () => {
  assert.deepEqual(parseArgs(['kubernetes']), { mode: 'kubernetes', overlays: ['local', 'dev'] });
  assert.deepEqual(parseArgs(['kubernetes', '--overlay', 'dev']), {
    mode: 'kubernetes',
    overlays: ['dev'],
  });
  assert.equal(parseArgs(['static']).mode, 'static');
  for (const args of [
    [],
    ['production'],
    ['compose', '--overlay', 'local'],
    ['kubernetes', '--overlay', 'prod'],
    ['static', '--context', 'production'],
  ]) {
    assert.throws(() => parseArgs(args));
  }
});

test('cleanup always follows success or failure and retains both failure causes', async () => {
  const events = [];
  assert.equal(
    await withCleanup(
      async () => {
        events.push('run');
        return 7;
      },
      async () => {
        events.push('cleanup');
      },
    ),
    7,
  );
  assert.deepEqual(events, ['run', 'cleanup']);
  events.length = 0;
  await assert.rejects(
    withCleanup(
      async () => {
        events.push('run');
        throw new Error('business failed');
      },
      async () => {
        events.push('cleanup');
      },
    ),
    /business failed/,
  );
  assert.deepEqual(events, ['run', 'cleanup']);
  await assert.rejects(
    withCleanup(
      async () => {
        throw new Error('run');
      },
      async () => {
        throw new Error('cleanup');
      },
    ),
    (error) => error instanceof AggregateError && error.errors.length === 2,
  );
});

test('generated-file checks use pre-run contents, including untracked additions and deletions', async () => {
  const directory = await fs.mkdtemp(path.join(os.tmpdir(), 'dancehub-generation-'));
  try {
    const existing = path.join(directory, 'already-edited.txt');
    await fs.writeFile(existing, 'uncommitted user content');
    const before = await snapshot([directory]);
    assert.deepEqual(changedFiles(before, await snapshot([directory])), []);
    await fs.writeFile(existing, 'generator changed this');
    const added = path.join(directory, 'new-generated.txt');
    await fs.writeFile(added, 'new');
    assert.deepEqual(
      changedFiles(before, await snapshot([directory])).sort(),
      [existing, added].sort(),
    );
    await fs.unlink(existing);
    assert.ok(changedFiles(before, await snapshot([directory])).includes(existing));
  } finally {
    assert.equal(path.dirname(path.resolve(directory)), path.resolve(os.tmpdir()));
    assert.ok(path.basename(directory).startsWith('dancehub-generation-'));
    await fs.rm(directory, { recursive: true, force: true });
  }
});

test('Windows and Linux installers select pinned official artifacts and exact checksum entries', () => {
  for (const platform of ['win32', 'linux']) {
    for (const name of ['node', 'go']) {
      const item = toolchainArtifact(name, platform, 'x64');
      assert.match(item.sha256, /^[a-f0-9]{64}$/);
      assert.ok(item.url.includes(item.version));
      assert.ok(item.executable.endsWith(platform === 'win32' ? '.exe' : name));
    }
    for (const name of [
      'kind',
      'kubectl',
      'skaffold',
      'helm',
      'buf',
      'kubeconform',
      'actionlint',
    ]) {
      const item = artifact(name, platform, 'x64');
      assert.ok(item.url.startsWith('https://'));
      assert.ok(item.url.includes(item.version));
      assert.ok(item.checksum.startsWith('https://'));
    }
  }
  assert.throws(() => artifact('kind', 'darwin', 'arm64'), /Windows\/Linux x64/);
  const hash = digest(Buffer.from('verified release bytes'));
  assert.equal(checksumFor(`${'0'.repeat(64)} other\n${hash} *tool.zip\n`, 'tool.zip'), hash);
  assert.equal(checksumFor(hash, 'binary'), hash);
  assert.throws(() => checksumFor(`${hash} one\n${hash} two`, 'missing'));
});

test('telemetry evidence accepts new Go RPC names and requires actual non-health business samples', () => {
  const lines = [
    'rpc_server_call_duration_seconds_count{job="dancehub/orderservice",rpc_method="dancehub.orders.v1.OrderService/GetOrder",rpc_response_status_code="OK"} 1',
    'http_server_request_duration_seconds_count{job="dancehub/creditservice",http_route="/dancehub.credits.v1.CreditService/GetBalances"} 1',
    'http_server_request_duration_seconds_count{job="dancehub/cartservice",http_route="/dancehub.cart.v1.CartService/GetCart"} 1',
  ];
  assert.equal(metricEvidence(lines.join('\n')).evidence.length, 3);
  assert.equal(metricEvidence(lines.join('\n').replace(/} 1/g, '} 0')).evidence.length, 0);
  assert.equal(
    metricEvidence(lines.join('\n').replace(/dancehub\.[^"}]+/g, '/healthz')).evidence.length,
    0,
  );
  assert.equal(metricEvidence(lines[0].replace('"OK"', '"UNAVAILABLE"')).evidence.length, 0);
});
