'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const grpc = require('@grpc/grpc-js');
const { createPaymentOperations } = require('./payment-operations');

function fixture() {
  const row = {
    id: 'payment-1',
    order_id: 'order-1',
    owner_user_id: 'alice',
    provider_payment_id: 'pi_fake',
    amount_cents: 1200,
    status: 'REQUIRES_PAYMENT_METHOD',
    payment_expires_at: new Date('2026-01-01T00:15:00Z'),
    succeeded_at: null,
  };
  const events = new Map(),
    outbox = [];
  const state = { now: new Date('2026-01-01T00:00:00.123Z'), row, events, outbox };
  const client = {
    async query(sql, args = []) {
      if (sql.startsWith('SELECT clock_timestamp'))
        return { rows: [{ now: state.now }], rowCount: 1 };
      if (sql.startsWith('SELECT event_type')) {
        const event = events.get(args[0]);
        return { rows: event ? [{ event_type: event.type }] : [], rowCount: event ? 1 : 0 };
      }
      if (sql.startsWith('SELECT id,order_id')) {
        const denied =
          sql.includes('owner_user_id::text=$2') && args[1] && args[1] !== row.owner_user_id;
        return { rows: denied ? [] : [{ ...row }], rowCount: denied ? 0 : 1 };
      }
      if (sql.includes('INSERT INTO payment.webhook_events')) {
        if (events.has(args[0])) return { rowCount: 0 };
        events.set(args[0], args[2]);
      }
      if (sql.includes('SET status=$2')) {
        row.status = args[1];
        row.provider_event_created_at = args[2];
        row.succeeded_at = args[3];
      }
      if (sql.includes("SET status='REFUNDED'")) row.status = 'REFUNDED';
      return { rows: [], rowCount: 1 };
    },
  };
  state.operations = createPaymentOperations({
    withTenant: async (_, work) => work(client),
    config: { mode: 'fake', simulationEnabled: true },
    stripe: null,
    insertOutbox: async (_, type, id, payload) => outbox.push({ type, id, payload }),
  });
  return state;
}
const call = (request, user = 'alice', service = '') => ({
  request,
  metadata: {
    get: (key) =>
      [
        {
          'x-user-id': user,
          'x-actor-kind': service ? 'service' : 'human',
          'x-service-principal': service,
        }[key],
      ].filter(Boolean),
  },
});
const simulate = (key, outcome = 'SUCCEEDED', user = 'alice') =>
  call({ id: 'payment-1', outcome, audit: { idempotencyKey: key } }, user);

test('other users and human administrators cannot read or simulate a payment', async () => {
  const f = fixture();
  await assert.rejects(f.operations.getPayment(call({ id: 'payment-1' }, 'bob')), {
    code: grpc.status.NOT_FOUND,
  });
  await assert.rejects(f.operations.getPaymentByOrder(call({ orderId: 'order-1' }, 'bob')), {
    code: grpc.status.NOT_FOUND,
  });
  await assert.rejects(f.operations.simulatePayment(simulate('key', 'SUCCEEDED', 'bob')), {
    code: grpc.status.NOT_FOUND,
  });
  assert.equal(f.events.size, 0);
});

test('simulation retries once, preserves exact success time and recovers a lost response after expiry', async () => {
  const f = fixture();
  await f.operations.simulatePayment(simulate('failed', 'FAILED'));
  const paid = await f.operations.simulatePayment(simulate('paid'));
  assert.equal(paid.status, 'SUCCEEDED');
  assert.equal(paid.succeededAt.nanos, 123000000);
  f.now = new Date('2026-01-01T01:00:00Z');
  assert.equal((await f.operations.simulatePayment(simulate('paid'))).status, 'SUCCEEDED');
  assert.deepEqual(
    f.outbox.map((v) => v.type),
    ['PaymentFailed', 'PaymentSucceeded'],
  );
  assert.equal(f.outbox[1].payload.paid_at, '2026-01-01T00:00:00.123Z');
  await assert.rejects(f.operations.simulatePayment(simulate('paid', 'FAILED')), {
    code: grpc.status.ALREADY_EXISTS,
  });
});

test('expired or completed payments reject new attempts and no simulated event is emitted', async () => {
  const f = fixture();
  f.now = f.row.payment_expires_at;
  await assert.rejects(f.operations.simulatePayment(simulate('late')), {
    code: grpc.status.FAILED_PRECONDITION,
  });
  f.row.status = 'REFUNDED';
  await assert.rejects(f.operations.simulatePayment(simulate('refunded')), {
    code: grpc.status.FAILED_PRECONDITION,
  });
  assert.equal(f.events.size, 0);
});

test('only orderservice can issue a full refund and retries do not duplicate its event', async () => {
  const f = fixture();
  const input = { id: 'payment-1', amountCents: 1200, audit: { idempotencyKey: 'refund:order-1' } };
  await assert.rejects(f.operations.refundPayment(call(input)), {
    code: grpc.status.PERMISSION_DENIED,
  });
  await f.operations.simulatePayment(simulate('paid'));
  await assert.rejects(
    f.operations.refundPayment(call({ ...input, amountCents: 1 }, '', 'orderservice')),
    { code: grpc.status.INVALID_ARGUMENT },
  );
  await f.operations.refundPayment(call(input, '', 'orderservice'));
  await f.operations.refundPayment(call(input, '', 'orderservice'));
  assert.equal(f.row.status, 'REFUNDED');
  assert.equal(f.outbox.filter((v) => v.type === 'PaymentRefunded').length, 1);
});

test('disabled simulation never touches storage', async () => {
  const operations = createPaymentOperations({
    config: { simulationEnabled: false },
    withTenant: () => assert.fail('storage must not be used'),
  });
  await assert.rejects(operations.simulatePayment(simulate('disabled')), {
    code: grpc.status.NOT_FOUND,
  });
});

test('creation never trusts a mismatching owner or an unsafe amount', async () => {
  const operations = createPaymentOperations({
    config: { simulationEnabled: true },
    withTenant: () => assert.fail('invalid creation must not access storage'),
  });
  const request = {
    orderId: 'order-1',
    ownerUserId: 'bob',
    amountCents: '1200',
    paymentExpiresAt: { seconds: '2000000000', nanos: 0 },
    audit: { idempotencyKey: 'create-order-1' },
  };
  await assert.rejects(operations.createPayment(call(request)), {
    code: grpc.status.INVALID_ARGUMENT,
  });
  await assert.rejects(
    operations.createPayment(
      call({ ...request, ownerUserId: 'alice', amountCents: '9007199254740992' }),
    ),
    { code: grpc.status.INVALID_ARGUMENT },
  );
});
