'use strict';

require('./telemetry');
const crypto = require('crypto');
const http = require('http');
const path = require('path');
const grpc = require('@grpc/grpc-js');
const protoLoader = require('@grpc/proto-loader');
const googleProtoFiles = require('google-proto-files');
const { Pool } = require('pg');
const Stripe = require('stripe');
const { context: otelContext, propagation } = require('@opentelemetry/api');
const {
  InvalidWebhookError,
  applyStripeEvent,
  parseStripeEvent,
  paymentConfiguration,
  reconcilePendingEvents,
} = require('./stripe-webhook');
const meshPeer = require('./mesh-peer');
const { createPaymentOperations } = require('./payment-operations');

const httpPort = 8080;
const grpcPort = 9090;
const databaseUrl = process.env.DATABASE_URL;
if (!databaseUrl) throw new Error('DATABASE_URL is required');
const pool = new Pool({
  connectionString: databaseUrl,
  max: 10,
});
const paymentConfig = paymentConfiguration(process.env);
const meshPolicy = meshPeer.policyFromEnvironment(process.env);
const stripe = paymentConfig.mode === 'stripe' ? new Stripe(paymentConfig.secretKey) : null;
const protoRoot = path.resolve(__dirname, '../../protos');
const googleProtoRoot = path.dirname(googleProtoFiles.getProtoPath());
const definition = protoLoader.loadSync(path.join(protoRoot, 'payments/v1/payments.proto'), {
  includeDirs: [protoRoot, googleProtoRoot],
  defaults: true,
  enums: String,
  longs: String,
  oneofs: true,
});
const payments = grpc.loadPackageDefinition(definition).dancehub.payments.v1;
const healthDefinition = protoLoader.loadSync(path.join(protoRoot, 'grpc/health/v1/health.proto'), {
  includeDirs: [protoRoot],
  defaults: true,
  enums: String,
});
const health = grpc.loadPackageDefinition(healthDefinition).grpc.health.v1;

const metadataValue = (call, key) => call.metadata.get(key)[0]?.toString() || '';

async function withTenant(call, work) {
  const identity = {
    userId: metadataValue(call, 'x-user-id'),
    studioId: metadataValue(call, 'x-studio-id'),
    tenantRoles: metadataValue(call, 'x-tenant-roles'),
    globalRoles: metadataValue(call, 'x-global-roles'),
    actorKind: metadataValue(call, 'x-actor-kind'),
    servicePrincipal: metadataValue(call, 'x-service-principal'),
    requestId: metadataValue(call, 'x-request-id'),
  };
  if (!identity.requestId)
    throw grpcError(grpc.status.UNAUTHENTICATED, 'request identity is incomplete');
  const client = await pool.connect();
  try {
    await client.query('BEGIN');
    await client.query(
      `SELECT set_config('app.user_id',$1,true),set_config('app.studio_id',$2,true),set_config('app.tenant_roles',$3,true),set_config('app.global_roles',$4,true),set_config('app.actor_kind',$5,true),set_config('app.service_principal',$6,true),set_config('app.service','paymentservice',true),set_config('app.request_id',$7,true)`,
      [
        identity.userId,
        identity.studioId,
        identity.tenantRoles,
        identity.globalRoles,
        identity.actorKind,
        identity.servicePrincipal,
        identity.requestId,
      ],
    );
    const result = await work(client);
    await client.query('COMMIT');
    return result;
  } catch (error) {
    await client.query('ROLLBACK').catch(() => {});
    throw error;
  } finally {
    client.release();
  }
}

const grpcError = (code, details) => Object.assign(new Error(details), { code, details });
const toGrpcError = (error, fallbackDetails) =>
  Number.isInteger(error?.code) ? error : grpcError(grpc.status.INTERNAL, fallbackDetails);
const operations = createPaymentOperations({
  withTenant,
  config: paymentConfig,
  stripe,
  insertOutbox,
});
const handlers = Object.fromEntries(
  Object.entries(operations).map(([name, operation]) => [
    name,
    async (call, callback) => {
      try {
        callback(null, await operation(call));
      } catch (error) {
        callback(toGrpcError(error, 'payment operation failed'));
      }
    },
  ]),
);

async function insertOutbox(client, eventType, aggregateId, payload) {
  const carrier = {};
  propagation.inject(otelContext.active(), carrier);
  await client.query(
    `INSERT INTO payment.outbox_events(event_type,aggregate_type,aggregate_id,payload,traceparent,tracestate,baggage) VALUES($1,'payment',$2,$3,NULLIF($4,''),NULLIF($5,''),NULLIF($6,''))`,
    [
      eventType,
      aggregateId,
      payload,
      carrier.traceparent || '',
      carrier.tracestate || '',
      carrier.baggage || '',
    ],
  );
}

async function readBody(request, raw = false) {
  const chunks = [];
  let size = 0;
  for await (const chunk of request) {
    size += chunk.length;
    if (size > 1024 * 1024) throw new Error('request_too_large');
    chunks.push(chunk);
  }
  const value = Buffer.concat(chunks);
  return raw ? value : JSON.parse(value.toString('utf8') || '{}');
}
function json(response, status, value) {
  response.writeHead(status, { 'content-type': 'application/json; charset=utf-8' });
  response.end(JSON.stringify(value));
}

async function stripeWebhook(request, response) {
  if (paymentConfig.mode !== 'stripe') return json(response, 404, { error: 'not_found' });
  const raw = await readBody(request, true);
  let event;
  try {
    event = parseStripeEvent({
      mode: paymentConfig.mode,
      stripe,
      webhookSecret: paymentConfig.webhookSecret,
      rawBody: raw,
      signature: request.headers['stripe-signature'],
    });
  } catch {
    return json(response, 400, { error: 'invalid_webhook_signature' });
  }
  const serviceCall = serviceIdentity();
  try {
    const result = await withTenant(serviceCall, async (client) => {
      return applyStripeEvent(client, event, insertOutbox);
    });
    return json(response, result.pending ? 202 : 200, result);
  } catch (error) {
    console.error(JSON.stringify({ message: 'webhook processing failed', error: error.message }));
    if (error instanceof InvalidWebhookError)
      return json(response, 400, { error: 'invalid_webhook_event' });
    return json(response, 500, { error: 'webhook_processing_failed' });
  }
}

const grpcServer = new grpc.Server();
// Business RPCs run behind the mesh peer check; the health service stays open so probes
// and the gateway's dependency check are not tied to caller identity.
grpcServer.addService(
  payments.PaymentService.service,
  meshPeer.guardHandlers(meshPolicy, handlers),
);

function serviceIdentity() {
  const metadata = new grpc.Metadata();
  metadata.set('x-actor-kind', 'service');
  metadata.set('x-service-principal', 'paymentservice');
  metadata.set('x-request-id', crypto.randomUUID());
  return { metadata };
}
grpcServer.addService(health.Health.service, {
  check: (_, callback) => callback(null, { status: 'SERVING' }),
  watch: (call) => {
    call.write({ status: 'SERVING' });
    call.end();
  },
});
grpcServer.bindAsync(`0.0.0.0:${grpcPort}`, grpc.ServerCredentials.createInsecure(), (error) => {
  if (error) throw error;
  console.log(JSON.stringify({ message: 'paymentservice gRPC listening', port: grpcPort }));
});

const httpServer = http.createServer(async (request, response) => {
  try {
    const url = new URL(request.url, 'http://localhost');
    if (request.method === 'GET' && url.pathname === '/healthz') {
      await pool.query('SELECT 1');
      return json(response, 200, { status: 'ok', service: 'paymentservice' });
    }
    if (request.method === 'POST' && url.pathname === '/webhooks/stripe')
      return await stripeWebhook(request, response);
    return json(response, 404, { error: 'not_found' });
  } catch (error) {
    console.error(JSON.stringify({ message: 'http request failed', error: error.message }));
    // The remaining oversized body is unread. Close this connection so the
    // client cannot reuse it for a request behind those unread bytes.
    if (error.message === 'request_too_large') response.setHeader('Connection', 'close');
    return json(response, error.message === 'request_too_large' ? 413 : 500, {
      error: 'internal_error',
    });
  }
});
httpServer.listen(httpPort, () =>
  console.log(
    JSON.stringify({
      message: 'paymentservice HTTP listening',
      port: httpPort,
      stripeMode: stripe ? 'stripe' : 'local-fake',
      meshPeerEnforcement: meshPolicy.enforce,
    }),
  ),
);

const reconcileTimer = setInterval(() => {
  withTenant(serviceIdentity(), (client) => reconcilePendingEvents(client, insertOutbox)).catch(
    (error) => {
      console.error(
        JSON.stringify({ message: 'webhook reconciliation failed', error: error.message }),
      );
    },
  );
}, 5000);
reconcileTimer.unref();
