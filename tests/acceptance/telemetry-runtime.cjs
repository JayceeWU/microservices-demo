#!/usr/bin/env node
'use strict';

// Local acceptance helper. No cluster changes, login secrets, or raw span dumps.
// Usage: node tests/acceptance/telemetry-runtime.cjs local-e2e-start-ms.txt
// Add --skip-browser only when debugging collection after the browser step ran.
const fs = require('node:fs');
const path = require('node:path');

const REPO = path.resolve(__dirname, '../..');
const CACHE = process.env.DANCEHUB_ACCEPTANCE_OUTPUT_DIR || path.join(REPO, '.cache', 'acceptance');
const JAEGER = process.env.DANCEHUB_ACCEPTANCE_JAEGER_URL || 'http://localhost:16686';
const METRICS = process.env.DANCEHUB_ACCEPTANCE_METRICS_URL || 'http://localhost:18889/metrics';
const GATEWAY = 'http://localhost:8080';
const STUDIO = '10000000-0000-0000-0000-000000000001';
const POLL_TIMEOUT_MS = 90_000;
const pause = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
const escapeRegex = (value) => value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
const safeError = (error) =>
  String(error?.message || error)
    .split('\n')[0]
    .replace(/https?:\/\/\S+/g, '[URL]')
    .replace(/Bearer\s+\S+/gi, 'Bearer [REDACTED]');

function rpc(service, language, proto, methods) {
  const known = methods.flatMap((method) => {
    const operation = `${proto}/${method}`;
    return [operation, `/${operation}`, `POST /${operation}`, `grpc.${operation}`];
  });
  if (language === '.NET') known.unshift('POST'); // ASP.NET may name gRPC spans as HTTP.
  return {
    service,
    language,
    known,
    rpc: true,
    pattern: new RegExp(`${escapeRegex(proto)}/(?:${methods.join('|')})$`),
  };
}

const TARGETS = [
  rpc('orderservice', 'Go', 'dancehub.orders.v1.OrderService', [
    'CreateOrderFromCart',
    'CreateRoomOrder',
    'GetOrder',
    'MarkPaymentSucceeded',
  ]),
  {
    service: 'chat-media-worker',
    language: 'Go',
    known: ['chat.attachment.scan'],
    pattern: /^chat\.attachment\.scan$/,
  },
  {
    service: 'payrollservice',
    language: 'Go',
    known: ['payroll.project'],
    pattern: /^payroll\.project$/,
  },
  rpc('paymentservice', 'Node', 'dancehub.payments.v1.PaymentService', [
    'CreatePayment',
    'SimulatePayment',
    'GetPayment',
    'GetPaymentByOrder',
  ]),
  rpc('creditservice', '.NET', 'dancehub.credits.v1.CreditService', [
    'GetBalances',
    'GrantFromOrder',
    'ListGrants',
  ]),
  rpc('cartservice', '.NET', 'dancehub.cart.v1.CartService', ['GetCart', 'AddItem', 'EmptyCart']),
  rpc('recommendationservice', 'Python', 'dancehub.recommendations.v1.RecommendationService', [
    'GetStudioAnalytics',
  ]),
  {
    service: 'outbox-relay',
    language: 'Python',
    known: ['outbox.publish'],
    pattern: /^outbox\.publish$/,
  },
  {
    service: 'payment-order-saga',
    language: 'Python',
    known: ['payment event -> order saga'],
    pattern: /^payment event -> order saga$/,
  },
  ...['student-web', 'teacher-web', 'admin-web'].map((service) => ({
    service,
    language: 'browser',
    known: ['documentLoad', 'HTTP GET', 'web_vital.largest-contentful-paint'],
    pattern:
      /^(?:documentLoad|documentFetch|resourceFetch|HTTP (?:GET|POST)|click|submit|web_vital\.[\w.-]+)$/,
  })),
];

async function get(url, json = true) {
  const response = await fetch(url, { signal: AbortSignal.timeout(10_000) });
  if (!response.ok) throw new Error(`HTTP ${response.status} from telemetry endpoint`);
  const body = json ? await response.json() : await response.text();
  if (json && body.errors?.length) throw new Error('Jaeger returned query errors');
  return body;
}

async function browserTraffic(report) {
  const { chromium } = require('@playwright/test');
  const browser = await chromium.launch({ headless: true });
  const roles = [
    {
      role: 'admin',
      origin: 'http://localhost:5173',
      paths: [`/v1/analytics?studio_id=${STUDIO}&month=${new Date().toISOString().slice(0, 7)}`],
    },
    {
      role: 'student',
      origin: 'http://localhost:3000',
      paths: ['/v1/credits/balances', '/v1/cart'],
    },
    { role: 'teacher', origin: 'http://localhost:4200', paths: ['/v1/me'] },
  ];
  try {
    // Keep three independent role contexts; tokens stay inside page.evaluate.
    const results = await Promise.allSettled(
      roles.map(async ({ role, origin, paths }) => {
        const context = await browser.newContext({
          storageState: path.join(
            process.env.PLAYWRIGHT_AUTH_DIR || path.join(REPO, 'tests/e2e/.auth'),
            `${role}.json`,
          ),
        });
        const proof = { role, reads: [], traceExportStatuses: [] };
        try {
          const page = await context.newPage();
          page.on('response', (response) => {
            if (response.url() === `${GATEWAY}/v1/telemetry/v1/traces`)
              proof.traceExportStatuses.push(response.status());
          });
          await page.goto(origin, { waitUntil: 'domcontentloaded', timeout: 45_000 });
          await page.waitForFunction(
            () => {
              const auth = JSON.parse(sessionStorage.getItem('dancehub.auth') || '{}');
              return Boolean(auth.accessToken);
            },
            null,
            { timeout: 45_000 },
          );
          proof.reads = await page.evaluate(
            async ({ gateway, studio, paths }) => {
              const auth = JSON.parse(sessionStorage.getItem('dancehub.auth') || '{}');
              const reads = [];
              for (const endpoint of paths) {
                const response = await fetch(`${gateway}${endpoint}`, {
                  headers: { Authorization: `Bearer ${auth.accessToken}`, 'X-Studio-Id': studio },
                  signal: AbortSignal.timeout(15_000),
                });
                await response.arrayBuffer();
                reads.push({ path: endpoint.split('?')[0], status: response.status });
              }
              return reads;
            },
            { gateway: GATEWAY, studio: STUDIO, paths },
          );
          if (proof.reads.some((read) => read.status !== 200))
            throw new Error(`${role} telemetry-generating read failed`);
          await page.waitForTimeout(6_000); // Browser BatchSpanProcessor's default delay is 5 seconds.
          console.log(
            `PASS browser ${role} ${proof.reads.map((r) => `${r.path}=${r.status}`).join(' ')}`,
          );
          return proof;
        } finally {
          report.browser.push(proof);
          await context.close();
        }
      }),
    );
    const failures = results.filter((result) => result.status === 'rejected');
    if (failures.length)
      throw new Error(failures.map((result) => safeError(result.reason)).join('; '));
  } finally {
    await browser.close();
  }
}

const tagsOf = (span) => Object.fromEntries((span.tags || []).map((tag) => [tag.key, tag.value]));
function successful(span) {
  const tags = tagsOf(span);
  return (
    tags.error !== true &&
    tags.error !== 'true' &&
    tags['otel.status_code'] !== 'ERROR' &&
    !(Number(tags['http.status_code'] || tags['http.response.status_code']) >= 400) &&
    !(
      tags['rpc.response.status_code'] !== undefined &&
      !['OK', '0'].includes(String(tags['rpc.response.status_code']))
    ) &&
    !(tags['rpc.grpc.status_code'] !== undefined && Number(tags['rpc.grpc.status_code']) !== 0)
  );
}

function evidence(trace, span, language) {
  return {
    service: trace.processes?.[span.processID]?.serviceName,
    language,
    operation: span.operationName,
    traceId: trace.traceID,
    spanId: span.spanID,
    startTimeUs: span.startTime,
    durationUs: span.duration,
  };
}

function gatewayEvidence(trace, business, target, startUs) {
  const spans = new Map((trace.spans || []).map((span) => [span.spanID, span]));
  const sameTrace = (id) =>
    !id || String(id).padStart(32, '0') === String(trace.traceID).padStart(32, '0');
  const parents = (span) =>
    (span.references || [])
      .filter((ref) => ref.refType === 'CHILD_OF' && sameTrace(ref.traceID))
      .map((ref) => ref.spanID);
  const queue = parents(business).map((id) => ({
    id,
    chain: [business.spanID],
    rpcSpanId: undefined,
  }));
  const visited = new Set();
  while (queue.length) {
    const current = queue.shift();
    if (visited.has(current.id)) continue;
    visited.add(current.id);
    const span = spans.get(current.id);
    if (!span) continue;
    const tags = tagsOf(span);
    const kind = String(tags['span.kind'] || '').toLowerCase();
    const service = trace.processes?.[span.processID]?.serviceName;
    const chain = [...current.chain, span.spanID];
    let rpcSpanId = current.rpcSpanId;
    if (
      service === 'gatewayservice' &&
      kind === 'client' &&
      (target.pattern.test(span.operationName) ||
        target.pattern.test(String(tags['rpc.method'] || '')))
    )
      rpcSpanId = span.spanID;
    const method = String(
      tags['http.request.method'] || tags['http.method'] || span.operationName.split(' ')[0],
    ).toUpperCase();
    if (
      service === 'gatewayservice' &&
      kind === 'server' &&
      rpcSpanId &&
      ['GET', 'POST', 'PUT', 'PATCH', 'DELETE'].includes(method) &&
      span.startTime >= startUs &&
      successful(span)
    ) {
      return {
        ...evidence(trace, span, 'Go'),
        httpMethod: method,
        linkedBusinessService: target.service,
        linkedBusinessSpanId: business.spanID,
        gatewayRpcSpanId: rpcSpanId,
        parentChainSpanIds: chain,
      };
    }
    for (const id of parents(span)) queue.push({ id, chain, rpcSpanId });
  }
  return undefined;
}

async function discoverOperations(target) {
  // Both Jaeger 1.x query API shapes are supported; exact known names remain fallbacks.
  const urls = [
    `${JAEGER}/api/operations?service=${encodeURIComponent(target.service)}`,
    `${JAEGER}/api/services/${encodeURIComponent(target.service)}/operations`,
  ];
  for (const url of urls) {
    try {
      const body = await get(url);
      const names = (Array.isArray(body.data) ? body.data : body.data?.operations || [])
        .map((item) => (typeof item === 'string' ? item : item.name || item.operationName))
        .filter((name) => typeof name === 'string' && target.pattern.test(name));
      if (names.length) return [...new Set(names)];
    } catch {
      /* Fall back without printing response bodies or unfiltered operations. */
    }
  }
  return [];
}

async function collectTraces(report, startUs) {
  const found = new Map();
  const operations = new Map();
  const diagnostic = {};
  const deadline = Date.now() + POLL_TIMEOUT_MS;
  const save = (proof) => {
    if (!found.has(proof.service)) {
      found.set(proof.service, proof);
      report.traces.push(proof);
      console.log(
        `PASS trace service=${proof.service} operation=${proof.operation} trace_id=${proof.traceId}`,
      );
    }
  };
  const inspect = (target, trace) => {
    const matches = (trace.spans || []).filter(
      (span) =>
        trace.processes?.[span.processID]?.serviceName === target.service &&
        (target.pattern.test(span.operationName) ||
          target.pattern.test(tagsOf(span)['http.route'] || tagsOf(span)['url.path'] || '')) &&
        span.startTime >= startUs &&
        span.startTime <= Date.now() * 1000 &&
        successful(span),
    );
    if (!matches.length) return;
    save(evidence(trace, matches[0], target.language));
    // The current Go HTTP SDK may emit only GET/POST with no route attribute.
    // Prove business traffic through CHILD_OF references, not an absent route:
    // business RPC server -> Gateway RPC client -> Gateway HTTP server.
    if (!target.rpc) return;
    for (const business of matches) {
      const gateway = gatewayEvidence(trace, business, target, startUs);
      if (gateway) {
        save(gateway);
        break;
      }
    }
  };
  while (Date.now() < deadline) {
    const pending = TARGETS.filter((target) => !found.has(target.service));
    // A lost gateway span may arrive in a later batch; re-read an RPC trace if needed.
    if (
      !found.has('gatewayservice') &&
      !pending.some((target) => target.service === 'recommendationservice')
    )
      pending.push(TARGETS.find((target) => target.service === 'recommendationservice'));
    for (let offset = 0; offset < pending.length; offset += 3) {
      await Promise.all(
        pending.slice(offset, offset + 3).map(async (target) => {
          if (!operations.has(target.service))
            operations.set(target.service, await discoverOperations(target));
          const candidates = [
            ...new Set([...operations.get(target.service), ...target.known]),
          ].slice(0, 8);
          diagnostic[target.service] = { queriedOperations: candidates };
          for (const operation of candidates) {
            try {
              const query = new URLSearchParams({
                service: target.service,
                operation,
                start: String(startUs),
                end: String(Date.now() * 1000),
                limit: '50',
              });
              const result = await get(`${JAEGER}/api/traces?${query}`);
              for (const trace of result.data || []) inspect(target, trace);
              if (
                found.has(target.service) &&
                (found.has('gatewayservice') || target.service !== 'recommendationservice')
              )
                break;
            } catch (error) {
              diagnostic[target.service].error = safeError(error);
            }
          }
        }),
      );
    }
    const missing = [...TARGETS.map((target) => target.service), 'gatewayservice'].filter(
      (service) => !found.has(service),
    );
    report.missingTraces = missing;
    if (!missing.length) return;
    console.log(`INFO waiting for business traces: ${missing.join(', ')}`);
    // Refresh discovery for missing services that may just have received their first export.
    for (const service of missing) operations.delete(service);
    if (Date.now() < deadline) await pause(5_000);
  }
  report.traceDiagnostics = Object.fromEntries(
    report.missingTraces.map((service) => [service, diagnostic[service] || {}]),
  );
  throw new Error(`Missing business trace evidence: ${report.missingTraces.join(', ')}`);
}

function parseMetrics(text) {
  const samples = [];
  for (const line of text.split('\n')) {
    if (!line || line.startsWith('#')) continue;
    const match = line.match(/^([a-zA-Z_:][\w:]*)(?:\{(.*)\})?\s+([^\s]+)/);
    if (!match || !Number.isFinite(Number(match[3]))) continue;
    const labels = {};
    for (const label of (match[2] || '').matchAll(/([a-zA-Z_]\w*)="((?:\\.|[^"\\])*)"/g)) {
      try {
        labels[label[1]] = JSON.parse(`"${label[2]}"`);
      } catch {
        labels[label[1]] = label[2];
      }
    }
    samples.push({ name: match[1], labels, value: Number(match[3]) });
  }
  return samples;
}

function metricEvidence(text) {
  const samples = parseMetrics(text);
  const resources = new Map(
    samples
      .filter((sample) => sample.name === 'target_info')
      .map((sample) => [
        `${sample.labels.job || ''}|${sample.labels.instance || ''}`,
        sample.labels,
      ]),
  );
  const targets = [
    { key: 'Go', services: ['orderservice', 'gatewayservice'], language: 'Go' },
    { key: 'creditservice', services: ['creditservice'], language: '.NET' },
    { key: 'cartservice', services: ['cartservice'], language: '.NET' },
  ];
  const evidence = [];
  for (const target of targets) {
    const candidate = samples
      .filter((sample) => sample.value > 0 && /^(?:http_server_|rpc_server_)/.test(sample.name))
      .sort((a, b) => Number(b.name.endsWith('_count')) - Number(a.name.endsWith('_count')))
      .find((sample) => {
        const labels = {
          ...resources.get(`${sample.labels.job || ''}|${sample.labels.instance || ''}`),
          ...sample.labels,
        };
        const service =
          labels.service_name ||
          String(labels.job || '')
            .split('/')
            .at(-1);
        const route = [labels.rpc_service, labels.rpc_method, labels.http_route, labels.url_path]
          .filter(Boolean)
          .join(' ');
        const success =
          !labels.rpc_response_status_code || ['OK', '0'].includes(labels.rpc_response_status_code);
        return (
          success &&
          target.services.includes(service) &&
          /dancehub\.|\/v1\//.test(route) &&
          !/health/i.test(route)
        );
      });
    if (!candidate) continue;
    const labels = {
      ...resources.get(`${candidate.labels.job || ''}|${candidate.labels.instance || ''}`),
      ...candidate.labels,
    };
    evidence.push({
      key: target.key,
      language: target.language,
      service:
        labels.service_name ||
        String(labels.job || '')
          .split('/')
          .at(-1),
      name: candidate.name,
      value: candidate.value,
      labels: Object.fromEntries(
        [
          'job',
          'instance',
          'rpc_service',
          'rpc_method',
          'http_route',
          'http_request_method',
          'http_response_status_code',
          'telemetry_sdk_language',
        ]
          .filter((key) => labels[key] !== undefined)
          .map((key) => [key, labels[key]]),
      ),
    });
  }
  return {
    evidence,
    families: [...new Set(samples.map((sample) => sample.name))]
      .filter((name) => /^(?:http_server_|rpc_server_|dotnet_|process_runtime_)/.test(name))
      .slice(0, 30),
  };
}

async function collectMetrics(report) {
  const deadline = Date.now() + POLL_TIMEOUT_MS;
  let lastError;
  while (Date.now() < deadline) {
    try {
      const result = metricEvidence(await get(METRICS, false));
      report.metrics = result.evidence;
      report.metricFamilies = result.families;
      if (result.evidence.length === 3) {
        for (const proof of result.evidence)
          console.log(
            `PASS metric language=${proof.language} service=${proof.service} name=${proof.name} value=${proof.value}`,
          );
        return;
      }
      lastError = `Only ${result.evidence.length}/3 business metric sources visible`;
    } catch (error) {
      lastError = safeError(error);
    }
    console.log(`INFO waiting for Collector metrics: ${lastError}`);
    if (Date.now() < deadline) await pause(5_000);
  }
  throw new Error(`Collector metrics incomplete: ${lastError}`);
}

async function main(args) {
  if (!args[0] || args.some((arg, index) => index > 0 && arg !== '--skip-browser'))
    throw new Error(
      'Usage: telemetry-runtime.cjs <local-e2e-start-ms.txt|dev-e2e-start-ms.txt> [--skip-browser]',
    );
  const input = args[0];
  const inputPath = path.isAbsolute(input)
    ? input
    : /[\\/]/.test(input)
      ? path.resolve(input)
      : path.join(CACHE, input);
  const startMs = Number(
    fs
      .readFileSync(inputPath, 'utf8')
      .replace(/^\uFEFF/, '')
      .trim(),
  );
  if (
    !Number.isSafeInteger(startMs) ||
    startMs < 1_700_000_000_000 ||
    startMs > Date.now() + 60_000
  )
    throw new Error('Invalid acceptance start-ms file');
  const label = path
    .basename(inputPath)
    .replace(/-e2e-start-ms\.txt$/, '')
    .replace(/[^\w-]/g, '_');
  const output = path.join(CACHE, `${label}-telemetry.json`);
  const report = {
    startMs,
    startedAt: new Date().toISOString(),
    browser: [],
    traces: [],
    metrics: [],
    errors: [],
  };
  try {
    if (args.includes('--skip-browser')) report.browserSkipped = true;
    else {
      try {
        await browserTraffic(report);
      } catch (error) {
        report.errors.push(`Browser: ${safeError(error)}`);
        console.log(`FAIL ${report.errors.at(-1)}`);
      }
    }
    const results = await Promise.allSettled([
      collectTraces(report, startMs * 1000),
      collectMetrics(report),
    ]);
    for (const result of results)
      if (result.status === 'rejected') report.errors.push(safeError(result.reason));
    report.passed = report.errors.length === 0;
  } finally {
    report.finishedAt = new Date().toISOString();
    fs.writeFileSync(output, JSON.stringify(report, null, 2) + '\n');
    for (const error of report.errors) console.log(`FAIL ${error}`);
    console.log(
      `RESULT ${report.passed ? 'PASS' : 'FAIL'} traces=${report.traces.length}/${TARGETS.length + 1} metrics=${report.metrics.length}/3 report=${output}`,
    );
  }
  return report.passed ? 0 : 1;
}

// Pure helpers can be loaded by offline checks without browser/network activity.
module.exports = {
  TARGETS,
  parseMetrics,
  metricEvidence,
  successful,
  gatewayEvidence,
  safeError,
  main,
};
if (require.main === module)
  main(process.argv.slice(2))
    .then((code) => {
      process.exitCode = code;
    })
    .catch((error) => {
      console.error(`FAIL ${safeError(error)}`);
      process.exitCode = 1;
    });
