import fs from 'node:fs/promises';
import { createWriteStream, existsSync, realpathSync } from 'node:fs';
import path from 'node:path';
import net from 'node:net';
import { spawn } from 'node:child_process';
import { randomUUID } from 'node:crypto';
import { fileURLToPath, pathToFileURL } from 'node:url';
import {
  ensureTools,
  ensureToolchains,
  ensureCalico,
  versions,
  nodeImage,
  digest,
} from './acceptance-tools.mjs';

const REPO = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const CLUSTER = 'dancehub-acceptance';
const CONTEXT = `kind-${CLUSTER}`;
const MINUTE = 60_000;
const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

export function parseArgs(args) {
  const mode = args.shift();
  if (['--help', '-h'].includes(mode)) return { help: true };
  if (!['static', 'compose', 'kubernetes'].includes(mode))
    throw new Error('Usage: verify-local.mjs static|compose|kubernetes [--overlay local|dev|all]');
  let overlay = 'all';
  while (args.length) {
    const flag = args.shift();
    if (
      flag !== '--overlay' ||
      mode !== 'kubernetes' ||
      !['local', 'dev', 'all'].includes(args[0])
    ) {
      throw new Error(`Unsupported argument: ${flag}`);
    }
    overlay = args.shift();
  }
  return { mode, overlays: overlay === 'all' ? ['local', 'dev'] : [overlay] };
}

export async function withCleanup(body, cleanup) {
  let result, failure;
  try {
    result = await body();
  } catch (error) {
    failure = error;
  }
  try {
    await cleanup();
  } catch (error) {
    failure = failure
      ? new AggregateError([failure, error], 'Acceptance and cleanup failed')
      : error;
  }
  if (failure) throw failure;
  return result;
}

export async function snapshot(paths) {
  const result = new Map();
  async function visit(file) {
    let stat;
    try {
      stat = await fs.stat(file);
    } catch (error) {
      if (error.code === 'ENOENT') return;
      throw error;
    }
    if (stat.isDirectory()) {
      for (const name of (await fs.readdir(file)).sort()) await visit(path.join(file, name));
    } else if (stat.isFile()) result.set(file, digest(await fs.readFile(file)));
  }
  for (const file of paths) await visit(file);
  return result;
}

export function changedFiles(before, after) {
  return [...new Set([...before.keys(), ...after.keys()])].filter(
    (file) => before.get(file) !== after.get(file),
  );
}

export function composeDefaults(source) {
  const result = {};
  for (const match of source.matchAll(/(?<!\$)\$\{([A-Z_][A-Z0-9_]*):-([^}]*)\}/g)) {
    if (Object.hasOwn(result, match[1]) && result[match[1]] !== match[2])
      throw new Error(`Conflicting Compose defaults for ${match[1]}`);
    result[match[1]] = match[2];
  }
  return result;
}

export function workloadImages(documents, excluded = []) {
  const ignored = new Set(excluded);
  const images = new Set();
  for (const document of documents) {
    const spec =
      document?.kind === 'Pod'
        ? document.spec
        : document?.kind === 'CronJob'
          ? document.spec?.jobTemplate?.spec?.template?.spec
          : document?.spec?.template?.spec;
    for (const container of [...(spec?.initContainers || []), ...(spec?.containers || [])]) {
      if (typeof container.image === 'string' && !ignored.has(container.image))
        images.add(container.image);
    }
  }
  return [...images];
}

export async function retryNpmCi(operation, { log = console.log, pause = sleep } = {}) {
  for (let attempt = 1; attempt <= 3; attempt++) {
    try {
      return await operation();
    } catch (error) {
      const output = `${error.stdout || ''}\n${error.stderr || ''}`;
      const lock = output.match(/^npm (?:error|ERR!) code (EBUSY|EPERM)\b/m)?.[1];
      if (!lock || attempt === 3) throw error;
      const delay = attempt * 5000;
      log(
        `npm ci failed with ${lock}; retry ${attempt + 1}/3 in ${delay / 1000}s. See the failed attempt log for details.`,
      );
      await pause(delay);
    }
  }
}

export async function retryBufGenerate(
  operation,
  { log = console.log, pause = sleep, interrupted = () => false } = {},
) {
  // Buf 1.57.2 surfaces the Connect error without the HTTP Retry-After header.
  // Follow the documented fallback with a bounded exponential backoff:
  // https://buf.build/docs/bsr/rate-limits/#handling-429-responses
  const delays = [30_000, 60_000, 120_000, 240_000];
  for (let attempt = 0; ; attempt++) {
    if (interrupted()) throw new Error('Acceptance interrupted');
    try {
      return await operation();
    } catch (error) {
      const output = `${error.stdout || ''}\n${error.stderr || ''}`;
      if (
        !/^(?:Failure: )?resource_exhausted:\s*too many requests\b/im.test(output) ||
        attempt === delays.length
      )
        throw error;
      let remaining = delays[attempt];
      log(
        `Buf remote generation was rate limited; retry ${attempt + 2}/5 in ${remaining / 1000}s. Generated-file verification remains required.`,
      );
      while (remaining > 0) {
        if (interrupted()) throw new Error('Acceptance interrupted');
        const interval = Math.min(30_000, remaining);
        await pause(interval);
        remaining -= interval;
        if (remaining > 0 && remaining % 60_000 === 0)
          log(`Buf rate-limit backoff: ${remaining / 1000}s until retry ${attempt + 2}/5.`);
      }
    }
  }
}

function npmCli() {
  const candidates = [
    process.env.npm_execpath,
    path.join(path.dirname(process.execPath), 'node_modules/npm/bin/npm-cli.js'),
  ];
  for (const entry of (process.env.PATH || '').split(path.delimiter)) {
    const command = path.join(entry, process.platform === 'win32' ? 'npm.cmd' : 'npm');
    if (existsSync(command))
      candidates.push(
        process.platform === 'win32'
          ? path.join(entry, 'node_modules/npm/bin/npm-cli.js')
          : realpathSync(command),
      );
  }
  const cli = candidates.find(
    (candidate) =>
      candidate &&
      /(?:npm-cli\.js|\/npm)$/.test(candidate.replaceAll('\\', '/')) &&
      existsSync(candidate),
  );
  if (!cli) throw new Error('Cannot locate npm CLI; install Node.js 22 with npm.');
  return cli;
}

class Run {
  constructor(options, id, output) {
    this.options = options;
    this.id = id;
    this.output = output;
    this.project = `dancehub-acceptance-${id}`;
    this.children = new Set();
    this.counter = 0;
    this.report = {
      id,
      mode: options.mode,
      overlays: options.overlays,
      startedAt: new Date().toISOString(),
      versions,
      steps: [],
      passed: false,
    };
    this.env = {
      ...process.env,
      CI: 'true',
      PYTHONUTF8: '1',
      AUTH_MODE: 'oidc',
      PAYMENT_MODE: 'fake',
      ENABLE_SIMULATED_PAYMENTS: 'true',
      COMPOSE_FILE: path.join(REPO, 'compose.yaml'),
      DANCEHUB_ACCEPTANCE_NAMESPACE: '',
      DANCEHUB_ACCEPTANCE_CONTEXT: '',
      DANCEHUB_ACCEPTANCE_RUN_ID: id,
      DANCEHUB_ACCEPTANCE_PROJECT: this.project,
      COMPOSE_PROJECT_NAME: this.project,
      DANCEHUB_ACCEPTANCE_OUTPUT_DIR: output,
      PLAYWRIGHT_AUTH_DIR: path.join(output, 'auth'),
      PLAYWRIGHT_HTML_OUTPUT_DIR: path.join(output, 'playwright-report'),
    };
    this.python = process.env.PYTHON || (process.platform === 'win32' ? 'python' : 'python3');
    this.nodeExecutable = process.execPath;
    this.npmExecutable = npmCli();
  }

  stop(child) {
    if (!child.pid || child.exitCode !== null || child.signalCode !== null) return;
    if (process.platform === 'win32') {
      const killer = spawn('taskkill', ['/PID', String(child.pid), '/T', '/F'], {
        windowsHide: true,
        stdio: 'ignore',
      });
      killer.on('error', () => child.kill());
    } else {
      try {
        process.kill(-child.pid, 'SIGTERM');
      } catch {
        child.kill();
      }
      const force = setTimeout(() => {
        try {
          process.kill(-child.pid, 'SIGKILL');
        } catch {
          /* Already exited. */
        }
      }, 10_000);
      force.unref();
      child.once('close', () => clearTimeout(force));
    }
  }

  start(
    command,
    args,
    {
      cwd = REPO,
      input,
      env = {},
      label = path.basename(command),
      timeout = 30 * MINUTE,
      quiet = false,
    } = {},
  ) {
    const logFile = path.join(
      this.output,
      `${String(++this.counter).padStart(3, '0')}-${label.replace(/[^\w.-]/g, '_')}.log`,
    );
    const log = createWriteStream(logFile);
    const started = Date.now();
    const child = spawn(command, args, {
      cwd,
      env: { ...this.env, ...env },
      windowsHide: true,
      detached: process.platform !== 'win32',
      stdio: ['pipe', 'pipe', 'pipe'],
    });
    this.children.add(child);
    let stdout = '',
      stderr = '',
      timedOut = false;
    const timer = setTimeout(() => {
      timedOut = true;
      this.stop(child);
    }, timeout);
    child.stdin.on('error', () => {});
    child.stdin.end(input);
    for (const [stream, target] of [
      [child.stdout, 'stdout'],
      [child.stderr, 'stderr'],
    ]) {
      stream.on('data', (data) => {
        log.write(data);
        if (target === 'stdout') stdout += data;
        else stderr += data;
        if (!quiet) process[target].write(data);
      });
    }
    const completion = new Promise((resolve, reject) => {
      let settled = false;
      function finish(error, code) {
        if (settled) return;
        settled = true;
        clearTimeout(timer);
        log.end();
        if (error) {
          error.stdout = stdout;
          error.stderr = stderr;
          reject(error);
        } else resolve({ stdout, stderr, code });
      }
      child.on('error', (error) => finish(error));
      child.on('close', (code, signal) => {
        this.children.delete(child);
        this.report.steps.push({
          label,
          log: path.basename(logFile),
          elapsedMs: Date.now() - started,
          exitCode: code,
          signal,
        });
        finish(
          code === 0
            ? null
            : new Error(
                `${label} ${timedOut ? 'timed out' : `exited ${code ?? signal}`}; see ${logFile}`,
              ),
          code,
        );
      });
    });
    // Background forward failures are checked explicitly by stopBackground/waitPort.
    completion.catch(() => {});
    return { child, completion };
  }

  async command(command, args, options) {
    if (this.interrupted && !this.cleaning) throw new Error('Acceptance interrupted');
    return await this.start(command, args, options).completion;
  }
  npm(args, options) {
    return this.command(this.nodeExecutable, [this.npmExecutable, ...args], {
      label: `npm-${args.join('-')}`,
      ...options,
    });
  }
  node(file, args = [], options) {
    return this.command(this.nodeExecutable, [path.join(REPO, file), ...args], {
      label: path.basename(file),
      ...options,
    });
  }
  py(file, args = [], options) {
    return this.command(this.python, [path.join(REPO, file), ...args], {
      label: path.basename(file),
      ...options,
    });
  }
  async bestEffort(label, operation) {
    try {
      await operation();
    } catch (error) {
      this.report.cleanupErrors ??= [];
      this.report.cleanupErrors.push(`${label}: ${error.message}`);
      console.error(`Cleanup ${label}: ${error.message}`);
    }
  }
  async stopBackground(items) {
    for (const item of items) this.stop(item.child);
    await Promise.allSettled(items.map((item) => item.completion));
  }
}

async function assertPortsAvailable(ports) {
  for (const port of new Set(ports)) {
    await new Promise((resolve, reject) => {
      const server = net.createServer();
      server.once('error', () =>
        reject(
          new Error(
            `Local port ${port} is occupied; stop the conflicting stack before acceptance.`,
          ),
        ),
      );
      server.listen(port, '127.0.0.1', () => server.close(resolve));
    });
  }
}

async function waitPort(port, forward, timeout = MINUTE) {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) {
    if (forward.child.exitCode !== null) {
      await forward.completion;
      throw new Error(`Forward ${port} exited`);
    }
    const connected = await new Promise((resolve) => {
      const socket = net.connect({ host: '127.0.0.1', port });
      socket.setTimeout(500);
      socket.on('connect', () => {
        socket.destroy();
        resolve(true);
      });
      socket.on('error', () => {
        socket.destroy();
        resolve(false);
      });
      socket.on('timeout', () => {
        socket.destroy();
        resolve(false);
      });
    });
    if (connected) return;
    await sleep(250);
  }
  throw new Error(`Port-forward ${port} did not become available`);
}

async function dependencies(ctx) {
  await retryNpmCi(() => ctx.npm(['ci']));
  await retryNpmCi(() => ctx.npm(['--prefix', 'src/paymentservice', 'ci']));
}

async function browser(ctx, directory) {
  await fs.mkdir(directory, { recursive: true });
  await ctx.command(
    ctx.nodeExecutable,
    [
      path.join(REPO, 'node_modules/@playwright/test/cli.js'),
      'test',
      '--workers=2',
      '--output',
      path.join(directory, 'playwright-results'),
    ],
    {
      label: 'playwright',
      env: {
        DANCEHUB_ACCEPTANCE_OUTPUT_DIR: directory,
        PLAYWRIGHT_OUTPUT_DIR: path.join(directory, 'playwright-results'),
      },
      timeout: 20 * MINUTE,
    },
  );
}

async function installBrowser(ctx) {
  await ctx.command(
    ctx.nodeExecutable,
    [
      path.join(REPO, 'node_modules/@playwright/test/cli.js'),
      'install',
      ...(process.platform === 'linux' ? ['--with-deps'] : []),
      'chromium',
    ],
    { label: 'playwright-install' },
  );
}

async function staticChecks(ctx, tools, yaml, toolCache) {
  await ctx.npm(['run', 'quality:static']);
  await ctx.node('scripts/check-markdown-links.mjs');
  const goFormatting = await ctx.command('gofmt', ['-l', 'src/dancehub'], { label: 'gofmt-check' });
  if (goFormatting.stdout.trim()) throw new Error(`Go files need gofmt:\n${goFormatting.stdout}`);
  const generator = yaml.load(await fs.readFile(path.join(REPO, 'buf.gen.yaml'), 'utf8'));
  const generated = [...new Set(generator.plugins.map((plugin) => path.join(REPO, plugin.out)))];
  generated.push(path.join(REPO, 'packages/api-client/openapi.d.ts'));
  const before = await snapshot(generated);
  await ctx.command(tools.buf, ['lint']);
  await ctx.command(tools.buf, ['build']);
  await retryBufGenerate(() => ctx.command(tools.buf, ['generate']), {
    interrupted: () => ctx.interrupted,
  });
  await ctx.npm(['run', 'generate:api-types']);
  const differences = changedFiles(before, await snapshot(generated));
  if (differences.length)
    throw new Error(
      `Generated files changed relative to their pre-run contents:\n${differences.map((file) => path.relative(REPO, file)).join('\n')}`,
    );
  await ctx.npm(['run', 'build:web']);
  await ctx.npm(['test']);
  await ctx.command('go', ['test', './...'], {
    cwd: path.join(REPO, 'src/dancehub'),
    label: 'go-test',
  });
  await ctx.command('go', ['vet', './...'], {
    cwd: path.join(REPO, 'src/dancehub'),
    label: 'go-vet',
  });
  for (const project of [
    'src/creditservice/creditservice.csproj',
    'src/cartservice/src/cartservice.csproj',
  ])
    await ctx.command('dotnet', ['build', project], { label: 'dotnet-build' });
  for (const project of [
    'tests/creditservice-domain/creditservice-domain.tests.csproj',
    'tests/cartservice/cartservice.tests.csproj',
  ])
    await ctx.command('dotnet', ['test', project], { label: 'dotnet-test' });
  await ctx.command(
    ctx.python,
    [
      '-m',
      'py_compile',
      'src/eventrelay/relay.py',
      'src/eventrelay/saga.py',
      'src/recommendationservice/recommendation_server.py',
      'src/recommendationservice/projection.py',
      'src/recommendationservice/mesh_peer.py',
      'tests/acceptance/network-runtime.py',
      'tests/acceptance/redis-runtime.py',
      'tests/acceptance/cni-runtime.py',
    ],
    { label: 'python-compile' },
  );
  await ctx.command(
    ctx.python,
    ['-m', 'unittest', 'discover', '-s', 'src/recommendationservice', '-p', 'test_*.py'],
    { label: 'python-tests' },
  );
  await ctx.command(
    ctx.nodeExecutable,
    [
      '--test',
      'scripts/kubernetes-development-checks.test.mjs',
      'scripts/kubernetes-security-checks.test.mjs',
      'scripts/verify-local.test.mjs',
      'scripts/check-markdown-links.test.mjs',
      'scripts/check-repository-language.test.mjs',
    ],
    { label: 'checker-tests' },
  );
  await ctx.command(tools.actionlint, ['-shellcheck='], { label: 'actionlint' });
  await ctx.command(tools.helm, [
    'lint',
    '--strict',
    'helm-chart',
    '-f',
    'helm-chart/values-production.yaml',
  ]);
  const manifests = [];
  for (const overlay of ['production', 'local', 'dev']) {
    const args =
      overlay === 'production'
        ? [
            'template',
            'dancehub',
            'helm-chart',
            '--namespace',
            'dancehub',
            '-f',
            'helm-chart/values-production.yaml',
            '--set-string',
            'dancehub.config.objectStoragePublicEndpoint=objects.acceptance.example',
            '--set-string',
            'dancehub.config.clamavAddress=scanner.acceptance.svc.cluster.local:3310',
          ]
        : ['kustomize', `kustomize/overlays/${overlay}`];
    const rendered = await ctx.command(
      overlay === 'production' ? tools.helm : tools.kubectl,
      args,
      { label: `render-${overlay}`, quiet: true },
    );
    const file = path.join(ctx.output, `render-${overlay}.yaml`);
    await fs.writeFile(file, rendered.stdout);
    manifests.push(file);
    await ctx.node('scripts/check-kubernetes-security.mjs', [
      overlay === 'production' ? 'production' : 'development',
      file,
    ]);
    if (overlay === 'production') {
      const docs = yaml.loadAll(rendered.stdout);
      for (const [name, variable, expected] of [
        ['chatservice', 'OBJECT_STORAGE_PUBLIC_ENDPOINT', 'objects.acceptance.example'],
        ['chat-media-worker', 'CLAMAV_ADDR', 'scanner.acceptance.svc.cluster.local:3310'],
      ]) {
        const value = docs
          .find((doc) => doc?.kind === 'Deployment' && doc.metadata.name === name)
          ?.spec.template.spec.containers[0].env.find((env) => env.name === variable)?.value;
        if (value !== expected)
          throw new Error(`Production Helm override did not reach ${name}/${variable}`);
      }
    }
  }
  const schemaCache = path.join(toolCache, 'kubeconform-schemas');
  await fs.mkdir(schemaCache, { recursive: true });
  await ctx.command(
    tools.kubeconform,
    [
      '-strict',
      '-summary',
      '-cache',
      schemaCache,
      '-kubernetes-version',
      versions.kubernetes,
      '-schema-location',
      'default',
      '-schema-location',
      'https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json',
      ...manifests,
    ],
    { label: 'kubeconform' },
  );
  await ctx.command('docker', ['compose', 'config', '--quiet']);
  await ctx.node('scripts/check-compose-security.mjs');
}

async function composeChecks(ctx) {
  const compose = (args, options) =>
    ctx.command('docker', ['compose', '-p', ctx.project, '--profile', 'full', ...args], {
      label: `compose-${args[0]}`,
      ...options,
    });
  const rendered = JSON.parse(
    (await compose(['config', '--format', 'json'], { quiet: true })).stdout,
  );
  await assertPortsAvailable(
    Object.values(rendered.services).flatMap((service) =>
      (service.ports || []).filter((port) => port.published).map((port) => Number(port.published)),
    ),
  );
  await withCleanup(
    async () => {
      await compose(['up', '-d', '--build', '--wait', '--wait-timeout', '600'], {
        timeout: 60 * MINUTE,
      });
      await ctx.node('scripts/check-image-users.mjs');
      for (const check of ['rls', 'security_controls', 'domain_reliability', 'payroll']) {
        await compose(['cp', `database/tests/${check}.sql`, `postgres:/tmp/${check}.sql`]);
        await compose([
          'exec',
          '-T',
          'postgres',
          'psql',
          '-v',
          'ON_ERROR_STOP=1',
          '-U',
          'dancehub',
          '-d',
          'dancehub',
          '-f',
          `/tmp/${check}.sql`,
        ]);
      }
      for (const file of ['payments.py', 'domain.py', 'cart.py', 'closeout.py'])
        await ctx.py(`tests/integration/${file}`);
      await ctx.node('tests/integration/webhook-body-limit.cjs');
      await ctx.command(
        'go',
        [
          'test',
          './internal/orders/infrastructure/flashsale',
          '-run',
          'TestRedisPendingIsRecoveredByANewConsumer',
          '-count=1',
        ],
        {
          cwd: path.join(REPO, 'src/dancehub'),
          env: { FLASHSALE_TEST_REDIS_ADDR: 'localhost:6380' },
          label: 'redis-pending',
        },
      );
      await ctx.py('tests/integration/domain.py', ['--faults-only']);
      await ctx.py('tests/integration/domain.py', ['--boundaries-only']);
      await browser(ctx, ctx.output);
      await compose(['up', '-d', '--wait', '--force-recreate', '--no-deps', 'gatewayservice'], {
        env: { AUTH_MODE: 'dev' },
      });
    },
    async () => {
      ctx.cleaning = true;
      await ctx.bestEffort('Compose logs', () => compose(['logs', '--no-color'], { quiet: true }));
      // Fault scripts normally unpause in finally; this also covers interruption.
      await ctx.bestEffort('Compose unpause', async () => {
        const paused = await ctx.command(
          'docker',
          [
            'ps',
            '-aq',
            '--filter',
            `label=com.docker.compose.project=${ctx.project}`,
            '--filter',
            'status=paused',
          ],
          { quiet: true },
        );
        const containers = paused.stdout.trim().split(/\s+/).filter(Boolean);
        if (containers.length) await ctx.command('docker', ['unpause', ...containers]);
      });
      await compose(['down', '--volumes', '--remove-orphans'], { timeout: 5 * MINUTE });
      ctx.cleaning = false;
    },
  );
}

async function kubernetesChecks(ctx, tools, yaml, toolCache) {
  await assertPortsAvailable([3000, 4200, 5173, 8080, 8081, 9000, 16686, 18889]);
  const existing = await ctx.command(tools.kind, ['get', 'clusters'], { quiet: true });
  if (existing.stdout.split(/\s+/).includes(CLUSTER))
    throw new Error(
      `Dedicated Kind cluster ${CLUSTER} already exists; inspect it before rerunning. It was not modified.`,
    );
  const kubeconfig = path.join(ctx.output, 'kubeconfig');
  ctx.env.KUBECONFIG = kubeconfig;
  const kube = (args, options) =>
    ctx.command(tools.kubectl, ['--kubeconfig', kubeconfig, '--context', CONTEXT, ...args], {
      label: `kubectl-${args[0]}`,
      ...options,
    });
  const kindConfig = path.join(ctx.output, 'kind.yaml');
  await fs.writeFile(
    kindConfig,
    yaml.dump({
      kind: 'Cluster',
      apiVersion: 'kind.x-k8s.io/v1alpha4',
      networking: {
        disableDefaultCNI: true,
        podSubnet: '192.168.0.0/16',
        apiServerAddress: '127.0.0.1',
      },
      nodes: [{ role: 'control-plane', image: nodeImage }],
    }),
  );
  const loadImages = async (images, label) => {
    const archive = path.join(ctx.output, `images-${label}.tar`);
    try {
      await ctx.command(
        'docker',
        ['image', 'save', '--platform', 'linux/amd64', '--output', archive, ...images],
        { label: `save-${label}`, timeout: 20 * MINUTE },
      );
      await ctx.command(tools.kind, ['load', 'image-archive', archive, '--name', CLUSTER], {
        label: `load-${label}`,
        timeout: 15 * MINUTE,
      });
    } finally {
      await fs.rm(archive, { force: true });
    }
  };
  const cacheImages = async (images) => {
    for (const image of new Set(images)) {
      let available = false;
      try {
        const inspected = await ctx.command(
          'docker',
          ['image', 'inspect', '--format', '{{.Os}}/{{.Architecture}}', image],
          { quiet: true },
        );
        available = inspected.stdout.trim() === 'linux/amd64';
      } catch {
        /* Missing images are pulled for the explicitly selected platform. */
      }
      if (!available)
        await ctx.command('docker', ['pull', '--platform', 'linux/amd64', image], {
          timeout: 30 * MINUTE,
        });
    }
  };
  // Fetch and verify the custom CNI before creating any runtime resources.
  const calicoManifest = await ensureCalico(toolCache, ctx.command.bind(ctx));
  const calicoImages = workloadImages(yaml.loadAll(await fs.readFile(calicoManifest, 'utf8')));
  if (!calicoImages.length)
    throw new Error('The verified Calico manifest contains no workload images');
  await cacheImages([nodeImage, ...calicoImages, 'redis:7-alpine']);
  await withCleanup(
    async () => {
      await ctx.command(
        tools.kind,
        [
          'create',
          'cluster',
          '--name',
          CLUSTER,
          '--config',
          kindConfig,
          '--kubeconfig',
          kubeconfig,
          '--wait',
          '0s',
        ],
        { timeout: 10 * MINUTE },
      );
      // Nodes cannot become Ready before the disabled default CNI is replaced.
      // Docker's cache is separate from the Kind node's containerd image store.
      await loadImages(calicoImages, 'calico');
      await kube(['apply', '--server-side', '-f', calicoManifest]);
      await kube(
        ['rollout', 'status', 'daemonset/calico-node', '-n', 'kube-system', '--timeout=300s'],
        { timeout: 6 * MINUTE },
      );
      await kube(
        [
          'rollout',
          'status',
          'deployment/calico-kube-controllers',
          '-n',
          'kube-system',
          '--timeout=300s',
        ],
        { timeout: 6 * MINUTE },
      );
      await kube(['wait', '--for=condition=Ready', 'nodes', '--all', '--timeout=180s'], {
        timeout: 4 * MINUTE,
      });
      await loadImages(['redis:7-alpine'], 'cni');
      await ctx.py('tests/acceptance/cni-runtime.py', [], { timeout: 5 * MINUTE });
      const config = yaml.load(await fs.readFile(path.join(REPO, 'skaffold.yaml'), 'utf8'));
      for (const item of config.build.artifacts) item.context = path.resolve(REPO, item.context);
      const globalConfig = path.join(ctx.output, 'skaffold-global.yaml');
      await fs.writeFile(
        globalConfig,
        yaml.dump({ global: { 'collect-metrics': false }, kubeContexts: [] }),
      );
      const buildConfig = path.join(ctx.output, 'skaffold-build.yaml');
      config.manifests.kustomize.paths = [path.join(REPO, 'kustomize/overlays/local')];
      await fs.writeFile(buildConfig, yaml.dump(config));
      const buildFile = path.join(ctx.output, 'build.json');
      const skaffold = (args, options) =>
        ctx.command(
          tools.skaffold,
          [
            ...args,
            // Skaffold 2.24 render has no kubeconfig/context/global-config flags.
            // Rendering stays offline; deploy/delete apply the namespace below.
            ...(args[0] === 'render'
              ? ['--offline=true', '--digest-source=tag']
              : ['--kubeconfig', kubeconfig, '--kube-context', CONTEXT, '--config', globalConfig]),
          ],
          { label: `skaffold-${args[0]}`, ...options },
        );
      await skaffold(
        [
          'build',
          '-f',
          buildConfig,
          '--push=false',
          '--file-output',
          buildFile,
          '--cache-file',
          path.join(toolCache, 'skaffold-build-cache.json'),
        ],
        { timeout: 90 * MINUTE },
      );
      const builds = JSON.parse(await fs.readFile(buildFile, 'utf8')).builds;
      await loadImages(
        builds.map((item) => item.tag),
        'applications',
      );
      // Reuse Docker's existing infrastructure image cache; only pull absent images.
      const baseRender = await kube(['kustomize', path.join(REPO, 'kustomize/overlays/local')], {
        quiet: true,
      });
      const infrastructure = new Set(
        workloadImages(
          yaml.loadAll(baseRender.stdout),
          config.build.artifacts.map((item) => item.image),
        ),
      );
      await cacheImages(infrastructure);
      infrastructure.delete('redis:7-alpine');
      if (infrastructure.size) await loadImages([...infrastructure], 'infrastructure');
      for (const overlay of ctx.options.overlays) {
        const namespace = `dancehub-${overlay}`;
        const output = path.join(ctx.output, overlay);
        await fs.mkdir(output, { recursive: true });
        await fs.copyFile(kubeconfig, path.join(output, 'kubeconfig'));
        await fs.copyFile(buildFile, path.join(output, 'build.json'));
        const environment = {
          DANCEHUB_ACCEPTANCE_OUTPUT_DIR: output,
          PLAYWRIGHT_AUTH_DIR: path.join(output, 'auth'),
          DANCEHUB_ACCEPTANCE_NAMESPACE: namespace,
          DANCEHUB_ACCEPTANCE_CONTEXT: CONTEXT,
          KUBECONFIG: kubeconfig,
        };
        ctx.env.PLAYWRIGHT_AUTH_DIR = environment.PLAYWRIGHT_AUTH_DIR;
        const forwarders = [];
        const overlayConfig = path.join(output, 'skaffold.yaml');
        config.manifests.kustomize.paths = [path.join(REPO, 'kustomize/overlays', overlay)];
        await fs.writeFile(overlayConfig, yaml.dump(config));
        const args = ['-f', overlayConfig, '--namespace', namespace];
        const deploy = () =>
          skaffold(
            [
              'deploy',
              ...args,
              '--build-artifacts',
              buildFile,
              '--status-check=true',
              '--tolerate-failures-until-deadline=true',
            ],
            { timeout: 20 * MINUTE },
          );
        await withCleanup(
          async () => {
            await kube(['create', 'namespace', namespace]);
            await skaffold([
              'render',
              ...args,
              '--build-artifacts',
              buildFile,
              '--output',
              path.join(output, `runtime-${namespace}.yaml`),
            ]);
            await deploy();
            for (const [service, localPort, preferredName] of [
              ['student-web', 3000, 'http'],
              ['teacher-web', 4200, 'http'],
              ['admin-web', 5173, 'http'],
              ['gatewayservice', 8080, 'http'],
              ['keycloak', 8081, 'http'],
              ['minio', 9000, 'api'],
              ['jaeger', 16686, 'ui'],
              ['otel-collector', 18889, 'metrics'],
            ]) {
              const document = JSON.parse(
                (
                  await kube(['get', 'service', service, '-n', namespace, '-o', 'json'], {
                    quiet: true,
                  })
                ).stdout,
              );
              const ports = document.spec.ports.filter(
                (port) => (port.protocol || 'TCP') === 'TCP',
              );
              const selected =
                ports.find((port) => port.name === preferredName) ||
                (ports.length === 1 ? ports[0] : undefined);
              if (!selected)
                throw new Error(`Cannot select ${service} ${preferredName} Service port`);
              const remotePort = selected.port;
              const forward = ctx.start(
                tools.kubectl,
                [
                  '--kubeconfig',
                  kubeconfig,
                  '--context',
                  CONTEXT,
                  '-n',
                  namespace,
                  'port-forward',
                  `service/${service}`,
                  `${localPort}:${remotePort}`,
                  '--address',
                  '127.0.0.1',
                ],
                { label: `forward-${overlay}-${service}`, timeout: 90 * MINUTE, quiet: true },
              );
              forwarders.push(forward);
              await waitPort(localPort, forward);
            }
            await ctx.py('tests/acceptance/network-runtime.py', [namespace], {
              env: environment,
              timeout: 15 * MINUTE,
            });
            const started = path.join(output, `${overlay}-e2e-start-ms.txt`);
            await fs.writeFile(started, String(Date.now()));
            await ctx.py('tests/integration/closeout.py', [], { env: environment });
            await browser(ctx, output);
            await ctx.node('tests/acceptance/telemetry-runtime.cjs', [started], {
              env: environment,
              timeout: 10 * MINUTE,
            });
            await ctx.py(
              'tests/acceptance/redis-runtime.py',
              ['--namespace', namespace, 'prepare'],
              { env: environment, timeout: 10 * MINUTE },
            );
            await ctx.stopBackground(forwarders);
            await skaffold(['delete', ...args], { timeout: 5 * MINUTE });
            await deploy();
            await ctx.py(
              'tests/acceptance/redis-runtime.py',
              ['--namespace', namespace, 'verify'],
              { env: environment, timeout: 5 * MINUTE },
            );
            await ctx.py(
              'tests/acceptance/redis-runtime.py',
              ['--namespace', namespace, 'cleanup'],
              { env: environment, timeout: 5 * MINUTE },
            );
          },
          async () => {
            ctx.cleaning = true;
            await ctx.bestEffort('Kubernetes diagnostics', () =>
              kube(['get', 'pods,events', '-n', namespace, '-o', 'wide'], { quiet: true }),
            );
            await ctx.bestEffort('Kubernetes application logs', () =>
              kube(
                [
                  'logs',
                  '-n',
                  namespace,
                  '--all-containers=true',
                  '--max-log-requests=30',
                  '-l',
                  'app',
                  '--tail=200',
                  '--prefix=true',
                ],
                { quiet: true },
              ),
            );
            await ctx.stopBackground(forwarders);
            await ctx.bestEffort('Skaffold delete', () =>
              skaffold(['delete', ...args], { timeout: 5 * MINUTE }),
            );
            await kube(
              [
                'delete',
                'namespace',
                namespace,
                '--ignore-not-found=true',
                '--wait=true',
                '--timeout=180s',
              ],
              { timeout: 4 * MINUTE },
            );
            ctx.cleaning = false;
          },
        );
      }
    },
    async () => {
      ctx.cleaning = true;
      await ctx.bestEffort('Kind diagnostics', () =>
        ctx.command(
          tools.kind,
          ['export', 'logs', path.join(ctx.output, 'kind-logs'), '--name', CLUSTER],
          { timeout: 5 * MINUTE },
        ),
      );
      await ctx.command(tools.kind, ['delete', 'cluster', '--name', CLUSTER], {
        timeout: 5 * MINUTE,
      });
      ctx.cleaning = false;
    },
  );
}

export async function main(args = process.argv.slice(2)) {
  const options = parseArgs([...args]);
  if (options.help) {
    console.log(
      'Usage: npm run verify:local -- static|compose|kubernetes [--overlay local|dev|all]\nRequires a bootstrap Node.js (22+), Python 3.13.x, .NET SDK 9.0.x, Docker/Compose and tar on Windows/Linux x64.\nVerified Node.js 22.16.0, Go 1.25.0 and pinned Kubernetes tools install into .cache/acceptance-tools. Reports go to .cache/acceptance/<run>.\nRuntime stages use isolated resources and remove their volumes/cluster after collecting diagnostics.',
    );
    return;
  }
  const id = `${Date.now()}-${randomUUID().slice(0, 8)}`;
  const output = path.join(REPO, '.cache', 'acceptance', id);
  const toolCache = path.resolve(
    process.env.DANCEHUB_ACCEPTANCE_TOOLS_DIR || path.join(REPO, '.cache', 'acceptance-tools'),
  );
  await fs.mkdir(output, { recursive: true });
  const ctx = new Run(options, id, output);
  console.log(`Acceptance ${options.mode}; reports: ${output}`);
  const interrupt = () => {
    ctx.interrupted = true;
    for (const child of ctx.children) ctx.stop(child);
  };
  process.once('SIGINT', interrupt);
  process.once('SIGTERM', interrupt);
  try {
    // The disposable fixture always uses the checked-in Compose defaults,
    // including fake payments, regardless of a developer's shell or .env.
    Object.assign(
      ctx.env,
      composeDefaults(await fs.readFile(path.join(REPO, 'compose.yaml'), 'utf8')),
    );
    const composeEnvironment = path.join(output, 'compose.env');
    await fs.writeFile(composeEnvironment, '');
    ctx.env.COMPOSE_ENV_FILES = composeEnvironment;
    ctx.env.COMPOSE_DISABLE_ENV_FILE = 'true';
    const toolchains = await ensureToolchains(toolCache, ctx.command.bind(ctx));
    ctx.nodeExecutable = toolchains.node;
    ctx.npmExecutable = toolchains.npm;
    ctx.env.PATH = [path.dirname(toolchains.node), path.dirname(toolchains.go), ctx.env.PATH].join(
      path.delimiter,
    );
    ctx.env.GOROOT = toolchains.goroot;
    ctx.env.GOTOOLCHAIN = 'local';
    ctx.env.npm_execpath = toolchains.npm;
    ctx.env.npm_node_execpath = toolchains.node;
    await ctx.command(toolchains.node, ['--version'], { label: 'verified-node-version' });
    await ctx.command(toolchains.go, ['version'], { label: 'verified-go-version' });
    const pythonVersion = await ctx.command(ctx.python, ['--version'], { label: 'python-version' });
    if (!/^Python 3\.13\./m.test(pythonVersion.stdout + pythonVersion.stderr))
      throw new Error('Acceptance requires Python 3.13.x, matching the quality workflow.');
    const dotnetVersion = await ctx.command('dotnet', ['--version'], { label: 'dotnet-version' });
    if (!/^9\.0\./m.test(dotnetVersion.stdout))
      throw new Error('Acceptance requires .NET SDK 9.0.x, matching the quality workflow.');
    await dependencies(ctx);
    const yaml = (await import('js-yaml')).default;
    const names =
      options.mode === 'static'
        ? ['buf', 'helm', 'kubectl', 'kubeconform', 'actionlint']
        : options.mode === 'kubernetes'
          ? ['kind', 'skaffold', 'kubectl']
          : [];
    const tools = await ensureTools(names, toolCache, ctx.command.bind(ctx));
    ctx.env.PATH = [
      ...new Set(Object.values(tools).map((file) => path.dirname(file))),
      ctx.env.PATH,
    ].join(path.delimiter);
    if (options.mode === 'static') await staticChecks(ctx, tools, yaml, toolCache);
    else {
      await installBrowser(ctx);
      if (options.mode === 'compose') await composeChecks(ctx);
      else await kubernetesChecks(ctx, tools, yaml, toolCache);
    }
    if (ctx.report.cleanupErrors?.length)
      throw new Error('Cleanup or diagnostics reported errors; inspect report.json');
    ctx.report.passed = true;
  } catch (error) {
    ctx.report.error =
      error instanceof AggregateError
        ? error.errors.map((item) => item.message).join('\n')
        : error.message;
    throw error;
  } finally {
    await ctx.stopBackground(
      [...ctx.children].map((child) => ({
        child,
        completion: new Promise((resolve) => child.once('close', resolve)),
      })),
    );
    ctx.report.finishedAt = new Date().toISOString();
    await fs.writeFile(path.join(output, 'report.json'), JSON.stringify(ctx.report, null, 2));
    process.removeListener('SIGINT', interrupt);
    process.removeListener('SIGTERM', interrupt);
    console.log(
      `RESULT ${ctx.report.passed ? 'PASS' : 'FAIL'}: ${path.join(output, 'report.json')}`,
    );
  }
}

if (process.argv[1] && pathToFileURL(path.resolve(process.argv[1])).href === import.meta.url) {
  main().catch((error) => {
    console.error(error.message);
    process.exitCode = 1;
  });
}
