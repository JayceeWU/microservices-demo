'use strict';

const assert = require('node:assert/strict');
const test = require('node:test');
const {
  InvalidWebhookError,
  applyStripeEvent,
  parseStripeEvent,
  paymentConfiguration,
  processPersistedStripeEvent,
  reconcilePaymentEvents,
  reconcilePendingEvents,
} = require('./stripe-webhook');

const event = (type = 'payment_intent.succeeded', created = 10) => ({
  id: `evt_${type}_${created}`,
  type,
  created,
  data: { object: { id: 'pi_1', last_payment_error: { code: 'declined' } } },
});

test('payment mode is explicit and Stripe credentials never degrade to unsigned mode', () => {
  assert.deepEqual(paymentConfiguration({ PAYMENT_MODE: 'fake' }), {
    mode: 'fake',
    secretKey: '',
    webhookSecret: '',
    simulationEnabled: false,
  });
  assert.deepEqual(
    paymentConfiguration({
      PAYMENT_MODE: 'stripe',
      STRIPE_SECRET_KEY: 'sk',
      STRIPE_WEBHOOK_SECRET: 'whsec',
    }),
    { mode: 'stripe', secretKey: 'sk', webhookSecret: 'whsec', simulationEnabled: false },
  );
  assert.throws(() => paymentConfiguration({}), /PAYMENT_MODE/);
  assert.throws(
    () => paymentConfiguration({ PAYMENT_MODE: 'stripe', STRIPE_SECRET_KEY: 'sk' }),
    /requires/,
  );
  assert.throws(
    () => paymentConfiguration({ PAYMENT_MODE: 'stripe', STRIPE_WEBHOOK_SECRET: 'whsec' }),
    /requires/,
  );
  assert.throws(
    () => paymentConfiguration({ PAYMENT_MODE: 'fake', STRIPE_SECRET_KEY: 'sk' }),
    /must not/,
  );
  assert.throws(
    () => paymentConfiguration({ PAYMENT_MODE: 'fake', STRIPE_WEBHOOK_SECRET: 'whsec' }),
    /must not/,
  );
});

test('parser requires a Stripe signature and refuses public webhooks in fake mode', () => {
  const stripe = {
    webhooks: { constructEvent: (body, signature, secret) => ({ body, signature, secret }) },
  };
  assert.throws(
    () => parseStripeEvent({ mode: 'stripe', stripe, rawBody: Buffer.from('{}') }),
    InvalidWebhookError,
  );
  const signed = parseStripeEvent({
    mode: 'stripe',
    stripe,
    webhookSecret: 'whsec',
    rawBody: Buffer.from('{}'),
    signature: 'signature',
  });
  assert.equal(signed.signature, 'signature');
  assert.throws(
    () => parseStripeEvent({ mode: 'fake', rawBody: Buffer.from('{"id":"evt"}') }),
    InvalidWebhookError,
  );
});

test('malformed events are rejected before persistence', async () => {
  const client = { query: async () => assert.fail('query must not run') };
  const supportedWithoutPaymentIntent = event();
  delete supportedWithoutPaymentIntent.data.object.id;
  for (const malformed of [
    null,
    {},
    { id: 'evt', created: 1 },
    { id: 'evt', type: 'type', created: 1.5 },
    { id: 'evt', type: 'type', created: -1 },
    supportedWithoutPaymentIntent,
  ]) {
    await assert.rejects(
      applyStripeEvent(client, malformed, async () => {}),
      InvalidWebhookError,
    );
  }
});

test('unknown events are durably ignored', async () => {
  const queries = [];
  const client = {
    async query(sql) {
      queries.push(sql);
      if (sql.includes('INSERT INTO')) return { rowCount: 1 };
      return { rowCount: 1, rows: [] };
    },
  };
  const result = await applyStripeEvent(client, event('customer.updated'), async () => {});
  assert.deepEqual(result, { received: true, ignored: true });
  assert.match(queries.at(-1), /processing_status=\$2/);
});

test('event received before payment remains pending', async () => {
  const client = {
    async query(sql) {
      if (sql.includes('INSERT INTO')) return { rowCount: 1 };
      if (sql.includes('SELECT id,order_id')) return { rowCount: 0, rows: [] };
      return { rowCount: 1, rows: [] };
    },
  };
  assert.deepEqual(await applyStripeEvent(client, event(), async () => {}), {
    received: true,
    pending: true,
  });
});

test('processed duplicates do not update payment or outbox', async () => {
  let calls = 0;
  const client = {
    async query(sql) {
      calls += 1;
      if (sql.includes('INSERT INTO')) return { rowCount: 0 };
      return { rowCount: 1, rows: [{ processing_status: 'PROCESSED' }] };
    },
  };
  assert.deepEqual(await applyStripeEvent(client, event(), async () => assert.fail()), {
    received: true,
    duplicate: true,
  });
  assert.equal(calls, 2);
});

test('pending duplicates are retried, including rows whose status cannot be read', async () => {
  for (const existingRows of [[{ processing_status: 'PENDING' }], []]) {
    const client = {
      async query(sql) {
        if (sql.includes('INSERT INTO')) return { rowCount: 0 };
        if (sql.includes('SELECT processing_status')) {
          return { rowCount: existingRows.length, rows: existingRows };
        }
        if (sql.includes('SELECT id,order_id')) return { rowCount: 0, rows: [] };
        return { rowCount: 1, rows: [] };
      },
    };
    assert.deepEqual(await applyStripeEvent(client, event(), async () => {}), {
      received: true,
      pending: true,
    });
  }
});

test('success and failure update payment and emit their domain event', async () => {
  for (const [type, expectedStatus, expectedOutbox] of [
    ['payment_intent.succeeded', 'SUCCEEDED', 'PaymentSucceeded'],
    ['payment_intent.payment_failed', 'FAILED', 'PaymentFailed'],
  ]) {
    const updates = [];
    const outbox = [];
    const client = {
      async query(sql, values) {
        if (sql.includes('SELECT id,order_id')) {
          return {
            rowCount: 1,
            rows: [
              {
                id: 'payment-1',
                order_id: 'order-1',
                status: 'REQUIRES_PAYMENT_METHOD',
                provider_event_created_at: null,
              },
            ],
          };
        }
        updates.push({ sql, values });
        return { rowCount: 1, rows: [] };
      },
    };
    await processPersistedStripeEvent(client, event(type), async (...args) => outbox.push(args));
    assert.equal(updates[0].values[1], expectedStatus);
    assert.equal(outbox[0][1], expectedOutbox);
  }

  const client = {
    async query(sql) {
      if (sql.includes('SELECT id,order_id')) {
        return {
          rowCount: 1,
          rows: [
            {
              id: 'payment-1',
              order_id: 'order-1',
              status: 'REQUIRES_PAYMENT_METHOD',
              provider_event_created_at: null,
            },
          ],
        };
      }
      return { rowCount: 1, rows: [] };
    },
  };
  const outbox = [];
  const failedWithoutProviderCode = event('payment_intent.payment_failed');
  delete failedWithoutProviderCode.data.object.last_payment_error;
  await processPersistedStripeEvent(client, failedWithoutProviderCode, async (...args) =>
    outbox.push(args),
  );
  assert.equal(outbox[0][3].reason, 'payment_failed');
});

test('stale, refunded and same-status events cannot regress payment state', async () => {
  for (const [status, timestamp, expectedStale] of [
    ['REFUNDED', 0, true],
    ['SUCCEEDED', 0, true],
  ]) {
    const updates = [];
    const client = {
      async query(sql, values) {
        if (sql.includes('SELECT id,order_id')) {
          return {
            rowCount: 1,
            rows: [
              {
                id: 'payment-1',
                order_id: 'order-1',
                status,
                provider_event_created_at: timestamp,
              },
            ],
          };
        }
        updates.push({ sql, values });
        return { rowCount: 1 };
      },
    };
    const result = await processPersistedStripeEvent(client, event(), async () => assert.fail());
    assert.equal(result.stale, expectedStale);
    assert.equal(
      updates.some(({ sql }) => sql.includes('SET status=$2')),
      false,
    );
  }
});

test('both reconcilers lock and process every returned pending event', async () => {
  for (const reconcile of [
    (client, outbox) => reconcilePaymentEvents(client, 'pi_1', outbox),
    (client, outbox) => reconcilePendingEvents(client, outbox, 5),
  ]) {
    let selected = false;
    const client = {
      async query(sql) {
        if (!selected) {
          selected = true;
          return { rowCount: 1, rows: [{ payload: event() }] };
        }
        if (sql.includes('SELECT id,order_id')) return { rowCount: 0, rows: [] };
        return { rowCount: 1, rows: [] };
      },
    };
    assert.equal(await reconcile(client, async () => {}), 1);
  }
});

test('simulation is explicitly restricted to local/test fake mode', () => {
  for (const ENVIRONMENT of ['local', 'test']) {
    assert.equal(
      paymentConfiguration({ PAYMENT_MODE: 'fake', ENVIRONMENT, ENABLE_SIMULATED_PAYMENTS: 'true' })
        .simulationEnabled,
      true,
    );
  }
  for (const ENVIRONMENT of [undefined, 'production', 'development']) {
    assert.throws(
      () =>
        paymentConfiguration({
          PAYMENT_MODE: 'fake',
          ENVIRONMENT,
          ENABLE_SIMULATED_PAYMENTS: 'true',
        }),
      /local or test/,
    );
  }
  assert.throws(
    () =>
      paymentConfiguration({
        PAYMENT_MODE: 'stripe',
        ENVIRONMENT: 'local',
        ENABLE_SIMULATED_PAYMENTS: 'true',
        STRIPE_SECRET_KEY: 'sk',
        STRIPE_WEBHOOK_SECRET: 'wh',
      }),
    /fake mode/,
  );
});

test('success supersedes a same-second or newer failure and carries actual paid_at', async () => {
  for (const previousTime of [10, 20]) {
    const updates = [],
      outbox = [];
    const client = {
      query: async (sql, values) => {
        if (sql.includes('SELECT id,order_id'))
          return {
            rowCount: 1,
            rows: [
              {
                id: 'payment-1',
                order_id: 'order-1',
                status: 'FAILED',
                provider_event_created_at: previousTime,
              },
            ],
          };
        updates.push({ sql, values });
        return { rowCount: 1 };
      },
    };
    await processPersistedStripeEvent(
      client,
      event('payment_intent.succeeded', 10),
      async (...args) => outbox.push(args),
    );
    assert.equal(updates[0].values[1], 'SUCCEEDED');
    assert.equal(outbox[0][3].paid_at, '1970-01-01T00:00:10.000Z');
  }
});

test('a later failure cannot regress a succeeded payment', async () => {
  const outbox = [],
    updates = [];
  const client = {
    query: async (sql, values) => {
      if (sql.includes('SELECT id,order_id'))
        return {
          rowCount: 1,
          rows: [
            {
              id: 'payment-1',
              order_id: 'order-1',
              status: 'SUCCEEDED',
              provider_event_created_at: 10,
            },
          ],
        };
      updates.push({ sql, values });
      return { rowCount: 1 };
    },
  };
  await processPersistedStripeEvent(
    client,
    event('payment_intent.payment_failed', 20),
    async (...args) => outbox.push(args),
  );
  assert.equal(outbox.length, 0);
  assert.equal(
    updates.some((x) => x.sql.includes('SET status=$2')),
    false,
  );
});
