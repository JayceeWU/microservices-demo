'use strict';

const SUPPORTED_EVENTS = new Map([
  ['payment_intent.succeeded', 'SUCCEEDED'],
  ['payment_intent.payment_failed', 'FAILED'],
]);

class InvalidWebhookError extends Error {
  constructor(message) {
    super(message);
    this.name = 'InvalidWebhookError';
  }
}

function paymentConfiguration(environment) {
  const mode = environment.PAYMENT_MODE;
  const secretKey = environment.STRIPE_SECRET_KEY || '';
  const webhookSecret = environment.STRIPE_WEBHOOK_SECRET || '';
  if (!['stripe', 'fake'].includes(mode)) {
    throw new Error('PAYMENT_MODE must be explicitly set to stripe or fake');
  }
  if (mode === 'stripe' && (!secretKey || !webhookSecret)) {
    throw new Error('stripe mode requires STRIPE_SECRET_KEY and STRIPE_WEBHOOK_SECRET');
  }
  if (mode === 'fake' && (secretKey || webhookSecret)) {
    throw new Error('fake mode must not receive Stripe credentials');
  }
  const simulationEnabled = environment.ENABLE_SIMULATED_PAYMENTS === 'true';
  if (
    simulationEnabled &&
    (mode !== 'fake' || !['local', 'test'].includes(environment.ENVIRONMENT))
  ) {
    throw new Error('simulated payments require fake mode and a local or test environment');
  }
  return { mode, secretKey, webhookSecret, simulationEnabled };
}

function parseStripeEvent({ mode, stripe, webhookSecret, rawBody, signature }) {
  if (mode === 'stripe') {
    if (!signature) throw new InvalidWebhookError('missing Stripe-Signature header');
    return stripe.webhooks.constructEvent(rawBody, signature, webhookSecret);
  }
  throw new InvalidWebhookError('webhooks are disabled in fake mode');
}

function validateEvent(event) {
  if (!event?.id || !event?.type || !Number.isSafeInteger(event.created) || event.created < 0) {
    throw new InvalidWebhookError('event id, type and created timestamp are required');
  }
  if (SUPPORTED_EVENTS.has(event.type) && !event.data?.object?.id) {
    throw new InvalidWebhookError('payment intent id is required');
  }
}

async function markProcessed(client, eventId, status = 'PROCESSED') {
  await client.query(
    `UPDATE payment.webhook_events
     SET processing_status=$2,processed_at=now(),last_error=NULL
     WHERE provider_event_id=$1`,
    [eventId, status],
  );
}

async function processPersistedStripeEvent(client, event, insertOutbox) {
  const targetStatus = SUPPORTED_EVENTS.get(event.type);
  if (!targetStatus) {
    await markProcessed(client, event.id, 'IGNORED');
    return { received: true, ignored: true };
  }

  const object = event.data.object;
  const result = await client.query(
    `SELECT id,order_id,status,provider_event_created_at,succeeded_at
     FROM payment.payments WHERE provider_payment_id=$1 FOR UPDATE`,
    [object.id],
  );
  if (result.rowCount === 0) return { received: true, pending: true };

  const payment = result.rows[0];
  // A successful PaymentIntent cannot become failed again. Event timestamps have
  // only second precision, so a success always outranks a retryable failure.
  const stale =
    targetStatus !== 'SUCCEEDED' &&
    Number(event.created) <= Number(payment.provider_event_created_at || 0);
  const terminal = payment.status === 'REFUNDED' || payment.status === 'SUCCEEDED';
  const paidAt =
    targetStatus === 'SUCCEEDED'
      ? new Date(event.simulated_paid_at || event.created * 1000).toISOString()
      : null;
  if (!stale && !terminal && payment.status !== targetStatus) {
    await client.query(
      `UPDATE payment.payments
       SET status=$2,provider_event_created_at=GREATEST(COALESCE(provider_event_created_at,0),$3),
           succeeded_at=COALESCE(succeeded_at,$4::timestamptz),updated_at=now() WHERE id=$1`,
      [payment.id, targetStatus, event.created, paidAt],
    );
    await insertOutbox(
      client,
      targetStatus === 'SUCCEEDED' ? 'PaymentSucceeded' : 'PaymentFailed',
      payment.id,
      targetStatus === 'SUCCEEDED'
        ? { order_id: payment.order_id, provider_payment_id: object.id, paid_at: paidAt }
        : {
            order_id: payment.order_id,
            reason: object.last_payment_error?.code || 'payment_failed',
          },
    );
  } else if (!stale && !terminal) {
    await client.query(
      `UPDATE payment.payments SET provider_event_created_at=$2,updated_at=now() WHERE id=$1`,
      [payment.id, event.created],
    );
  }
  await markProcessed(client, event.id);
  return { received: true, stale: stale || terminal };
}

async function applyStripeEvent(client, event, insertOutbox) {
  validateEvent(event);
  const supported = SUPPORTED_EVENTS.has(event.type);
  const inserted = await client.query(
    `INSERT INTO payment.webhook_events(
       provider_event_id,event_type,payload,processing_status,provider_created_at,processed_at
     ) VALUES($1,$2,$3,$4,$5,$6)
     ON CONFLICT DO NOTHING RETURNING provider_event_id`,
    [event.id, event.type, event, supported ? 'PENDING' : 'IGNORED', event.created, null],
  );
  if (inserted.rowCount === 0) {
    const existing = await client.query(
      `SELECT processing_status FROM payment.webhook_events
       WHERE provider_event_id=$1 FOR UPDATE`,
      [event.id],
    );
    if (['PROCESSED', 'IGNORED'].includes(existing.rows[0]?.processing_status)) {
      return { received: true, duplicate: true };
    }
  }
  await client.query(
    `UPDATE payment.webhook_events
     SET attempt_count=attempt_count+1,last_attempt_at=now()
     WHERE provider_event_id=$1`,
    [event.id],
  );
  return processPersistedStripeEvent(client, event, insertOutbox);
}

async function reconcilePaymentEvents(client, providerPaymentId, insertOutbox) {
  const result = await client.query(
    `SELECT payload FROM payment.webhook_events
     WHERE processing_status='PENDING'
       AND payload #>> '{data,object,id}'=$1
     ORDER BY provider_created_at,provider_event_id FOR UPDATE`,
    [providerPaymentId],
  );
  for (const row of result.rows) {
    await processPersistedStripeEvent(client, row.payload, insertOutbox);
  }
  return result.rowCount;
}

async function reconcilePendingEvents(client, insertOutbox, limit = 100) {
  const result = await client.query(
    `SELECT webhook.payload
     FROM payment.webhook_events AS webhook
     JOIN payment.payments AS payment_row
       ON webhook.payload #>> '{data,object,id}'=payment_row.provider_payment_id
     WHERE webhook.processing_status='PENDING'
     ORDER BY webhook.received_at
     LIMIT $1 FOR UPDATE OF webhook SKIP LOCKED`,
    [limit],
  );
  for (const row of result.rows) {
    await processPersistedStripeEvent(client, row.payload, insertOutbox);
  }
  return result.rowCount;
}

module.exports = {
  InvalidWebhookError,
  applyStripeEvent,
  parseStripeEvent,
  paymentConfiguration,
  processPersistedStripeEvent,
  reconcilePaymentEvents,
  reconcilePendingEvents,
};
