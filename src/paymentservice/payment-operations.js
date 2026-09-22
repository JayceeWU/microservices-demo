'use strict';

const crypto = require('node:crypto');
const grpc = require('@grpc/grpc-js');
const { applyStripeEvent, reconcilePaymentEvents } = require('./stripe-webhook');

const columns =
  'id,order_id,owner_user_id,provider_payment_id,amount_cents,status,payment_expires_at,succeeded_at';
const failure = (code, message) => Object.assign(new Error(message), { code, details: message });
const metadata = (call, key) => call.metadata.get(key)[0]?.toString() || '';
const isOrders = (call) =>
  metadata(call, 'x-actor-kind') === 'service' &&
  metadata(call, 'x-service-principal') === 'orderservice';
function human(call) {
  const id = metadata(call, 'x-user-id');
  if (!id || metadata(call, 'x-actor-kind') !== 'human')
    throw failure(grpc.status.UNAUTHENTICATED, 'human identity required');
  return id;
}
function timestamp(value) {
  if (!value) return null;
  const milliseconds = new Date(value).getTime();
  return {
    seconds: String(Math.floor(milliseconds / 1000)),
    nanos: (milliseconds % 1000) * 1000000,
  };
}
function dateFromTimestamp(value) {
  if (!value || !Number.isFinite(Number(value.seconds))) return null;
  const date = new Date(Number(value.seconds) * 1000 + Number(value.nanos || 0) / 1000000);
  return Number.isFinite(date.getTime()) ? date : null;
}

function createPaymentOperations({ withTenant, config, stripe, insertOutbox }) {
  const view = (row, clientSecret = '') => ({
    id: row.id,
    orderId: row.order_id,
    providerPaymentId: row.provider_payment_id || '',
    amountCents: String(row.amount_cents),
    status: row.status,
    clientSecret,
    simulationEnabled: config.simulationEnabled,
    succeededAt: timestamp(row.succeeded_at),
    paymentExpiresAt: timestamp(row.payment_expires_at),
  });
  const key = (input) => {
    if (!input.audit?.idempotencyKey?.trim())
      throw failure(grpc.status.INVALID_ARGUMENT, 'idempotency_key is required');
    return input.audit.idempotencyKey;
  };
  const owned = async (client, call, where, value, lock = false) => {
    const owner = isOrders(call) ? '' : human(call);
    const result = await client.query(
      `SELECT ${columns} FROM payment.payments WHERE ${where}=$1 AND ($2='' OR owner_user_id::text=$2)${lock ? ' FOR UPDATE' : ''}`,
      [value, owner],
    );
    if (result.rowCount !== 1) throw failure(grpc.status.NOT_FOUND, 'payment not found');
    return result.rows[0];
  };
  const operations = {
    async createPayment(call) {
      const owner = human(call),
        input = call.request;
      key(input);
      const expires = dateFromTimestamp(input.paymentExpiresAt);
      if (
        !input.orderId ||
        input.ownerUserId !== owner ||
        !expires ||
        !Number.isSafeInteger(Number(input.amountCents)) ||
        Number(input.amountCents) < 0
      ) {
        throw failure(
          grpc.status.INVALID_ARGUMENT,
          'valid order, owner, amount and payment deadline are required',
        );
      }
      return withTenant(call, async (client) => {
        await client.query('SELECT pg_advisory_xact_lock(hashtextextended($1,0))', [input.orderId]);
        const existing = await client.query(
          `SELECT ${columns} FROM payment.payments WHERE order_id=$1 FOR UPDATE`,
          [input.orderId],
        );
        if (existing.rowCount) {
          if (existing.rows[0].owner_user_id !== owner)
            throw failure(grpc.status.NOT_FOUND, 'payment not found');
          return view(existing.rows[0]);
        }
        const clock = await client.query('SELECT clock_timestamp() AS now');
        if (expires <= clock.rows[0].now)
          throw failure(grpc.status.FAILED_PRECONDITION, 'payment deadline has passed');
        let providerId,
          secret = '';
        if (stripe) {
          const intent = await stripe.paymentIntents.create(
            {
              amount: Number(input.amountCents),
              currency: 'usd',
              metadata: { order_id: input.orderId },
              automatic_payment_methods: { enabled: true },
            },
            { idempotencyKey: `order:${input.orderId}` },
          );
          providerId = intent.id;
          secret = intent.client_secret;
        } else {
          providerId = `pi_test_${crypto.randomUUID().replaceAll('-', '')}`;
        }
        await client.query(
          `INSERT INTO payment.payments(order_id,owner_user_id,provider_payment_id,amount_cents,status,payment_expires_at) VALUES($1,$2,$3,$4,'REQUIRES_PAYMENT_METHOD',$5)`,
          [input.orderId, owner, providerId, input.amountCents, expires],
        );
        // Reconciliation needs the paymentservice principal for the private inbox.
        await client.query(
          "SELECT set_config('app.actor_kind','service',true),set_config('app.service_principal','paymentservice',true)",
        );
        await reconcilePaymentEvents(client, providerId, insertOutbox);
        const result = await client.query(
          `SELECT ${columns} FROM payment.payments WHERE order_id=$1`,
          [input.orderId],
        );
        return view(result.rows[0], secret);
      });
    },
    async getPayment(call) {
      human(call);
      return withTenant(call, async (client) =>
        view(await owned(client, call, 'id', call.request.id)),
      );
    },
    async getPaymentByOrder(call) {
      return withTenant(call, async (client) =>
        view(await owned(client, call, 'order_id', call.request.orderId)),
      );
    },
    async simulatePayment(call) {
      if (!config.simulationEnabled)
        throw failure(grpc.status.NOT_FOUND, 'simulated payments are disabled');
      human(call);
      const input = call.request,
        operationKey = key(input);
      if (!['SUCCEEDED', 'FAILED'].includes(input.outcome))
        throw failure(grpc.status.INVALID_ARGUMENT, 'outcome must be SUCCEEDED or FAILED');
      return withTenant(call, async (client) => {
        const payment = await owned(client, call, 'id', input.id, true);
        const eventId = `evt_sim_${crypto.createHash('sha256').update(`${payment.id}:${operationKey}`).digest('hex')}`;
        // Only after ownership is checked may this transaction access the private event inbox.
        await client.query(
          "SELECT set_config('app.actor_kind','service',true),set_config('app.service_principal','paymentservice',true)",
        );
        const previous = await client.query(
          'SELECT event_type FROM payment.webhook_events WHERE provider_event_id=$1',
          [eventId],
        );
        const eventType =
          input.outcome === 'SUCCEEDED'
            ? 'payment_intent.succeeded'
            : 'payment_intent.payment_failed';
        if (previous.rowCount) {
          if (previous.rows[0].event_type !== eventType)
            throw failure(
              grpc.status.ALREADY_EXISTS,
              'idempotency key was used with another outcome',
            );
          return view(payment);
        }
        if (['SUCCEEDED', 'REFUNDED'].includes(payment.status))
          throw failure(grpc.status.FAILED_PRECONDITION, 'payment is already complete');
        const clock = await client.query('SELECT clock_timestamp() AS now');
        const now = clock.rows[0].now;
        if (now >= payment.payment_expires_at)
          throw failure(grpc.status.FAILED_PRECONDITION, 'payment deadline has passed');
        const event = {
          id: eventId,
          type: eventType,
          created: Math.floor(now.getTime() / 1000),
          simulated_paid_at: now.toISOString(),
          data: {
            object: {
              id: payment.provider_payment_id,
              last_payment_error: { code: 'simulated_decline' },
            },
          },
        };
        await applyStripeEvent(client, event, insertOutbox);
        const current = await client.query(`SELECT ${columns} FROM payment.payments WHERE id=$1`, [
          payment.id,
        ]);
        return view(current.rows[0]);
      });
    },
    async refundPayment(call) {
      if (!isOrders(call))
        throw failure(grpc.status.PERMISSION_DENIED, 'orderservice identity required');
      const input = call.request;
      key(input);
      return withTenant(call, async (client) => {
        const payment = await owned(client, call, 'id', input.id, true);
        if (Number(input.amountCents) !== Number(payment.amount_cents))
          throw failure(grpc.status.INVALID_ARGUMENT, 'only a full refund is supported');
        if (payment.status === 'REFUNDED') return view(payment);
        if (payment.status !== 'SUCCEEDED')
          throw failure(grpc.status.FAILED_PRECONDITION, 'payment is not refundable');
        const refundId = stripe
          ? (
              await stripe.refunds.create(
                { payment_intent: payment.provider_payment_id },
                { idempotencyKey: `refund:${payment.order_id}` },
              )
            ).id
          : `re_test_${crypto.createHash('sha256').update(payment.order_id).digest('hex').slice(0, 24)}`;
        await client.query(
          "UPDATE payment.payments SET status='REFUNDED',provider_refund_id=$2,refunded_at=now(),updated_at=now() WHERE id=$1",
          [payment.id, refundId],
        );
        await insertOutbox(client, 'PaymentRefunded', payment.id, {
          order_id: payment.order_id,
          provider_refund_id: refundId,
          amount_cents: Number(payment.amount_cents),
        });
        return view({ ...payment, status: 'REFUNDED' });
      });
    },
  };
  return operations;
}

module.exports = { createPaymentOperations };
