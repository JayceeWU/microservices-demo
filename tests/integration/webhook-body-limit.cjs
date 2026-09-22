'use strict';

// Run only an isolated local container. Dummy Stripe credentials enable the
// signed-webhook branch; oversized requests are rejected before any Stripe API.
const { execFileSync } = require('node:child_process');
const project = process.env.COMPOSE_PROJECT_NAME || 'bay-area-dance-hub';
const check = String.raw`
const assert = require('node:assert/strict');
const http = require('node:http');
const { spawn } = require('node:child_process');
const child = spawn(process.execPath, ['http-server.js'], {
  env: { ...process.env, PAYMENT_MODE: 'stripe', ENABLE_SIMULATED_PAYMENTS: 'false',
    STRIPE_SECRET_KEY: 'sk_test_local_body_limit_only', STRIPE_WEBHOOK_SECRET: 'whsec_local_body_limit_only' },
  stdio: ['ignore', 'inherit', 'inherit'],
});
function request(method, path, body) {
  return new Promise((resolve, reject) => {
    const req = http.request({ hostname: '127.0.0.1', port: 8080, method, path }, (res) => {
      res.resume(); res.on('end', () => resolve(res.statusCode));
    });
    req.on('error', reject);
    req.setTimeout(5000, () => req.destroy(new Error(method + ' ' + path + ' request timeout')));
    req.end(body);
  });
}
(async () => {
  try {
    let ready = false;
    for (let attempt = 0; attempt < 50; attempt++) {
      if (child.exitCode !== null) throw new Error('payment process exited during startup');
      try { ready = await request('GET', '/healthz') === 200; } catch {}
      if (ready) break;
      await new Promise(resolve => setTimeout(resolve, 100));
    }
    assert.ok(ready, 'isolated payment service must start');
    assert.equal(await request('POST', '/webhooks/stripe', Buffer.alloc(2 * 1024 * 1024, 'x')), 413);
    assert.equal(await request('GET', '/healthz'), 200);
    assert.equal(child.exitCode, null);
    console.log('PASS oversized Stripe webhook returns 413 and payment process remains healthy');
  } finally {
    child.kill('SIGTERM');
  }
})().catch(error => { console.error(error.message); process.exitCode = 1; });
`;
try {
  execFileSync(
    'docker',
    [
      'compose',
      '-p',
      project,
      'run',
      '--rm',
      '--no-deps',
      '--entrypoint',
      'node',
      'paymentservice',
      '-e',
      check,
    ],
    { stdio: 'inherit' },
  );
} catch (error) {
  process.exitCode = error.status || 1;
}
