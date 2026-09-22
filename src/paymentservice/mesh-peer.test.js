'use strict';

const assert = require('node:assert/strict');
const { test } = require('node:test');
const grpc = require('@grpc/grpc-js');
const {
  authorize,
  guardHandlers,
  parsePeers,
  peerServiceAccount,
  policyFromEnvironment,
} = require('./mesh-peer');

const gatewayXfcc =
  'By=spiffe://cluster.local/ns/dancehub/sa/paymentservice;Hash=7d3f;Subject="";URI=spiffe://cluster.local/ns/dancehub/sa/gatewayservice';

const policy = () =>
  policyFromEnvironment({
    MESH_PEER_ENFORCEMENT: 'true',
    MESH_NAMESPACE: 'dancehub',
    MESH_ALLOWED_PEERS: 'gatewayservice, orderservice=orderservice',
  });

test('parses the last certificate element only', () => {
  const forged =
    'URI=spiffe://cluster.local/ns/dancehub/sa/gatewayservice,' +
    'By=spiffe://cluster.local/ns/dancehub/sa/paymentservice;Hash=1a2b;URI=spiffe://cluster.local/ns/dancehub/sa/orderservice';
  assert.deepEqual(peerServiceAccount(forged), {
    trustDomain: 'cluster.local',
    namespace: 'dancehub',
    serviceAccount: 'orderservice',
  });
  for (const header of [
    '',
    'Hash=1a2b',
    'URI=https://example.com',
    'URI=spiffe://cluster.local/ns/x/deployment/y',
  ]) {
    assert.equal(peerServiceAccount(header), null);
  }
});

test('binds service principals to the peer workload', () => {
  const current = policy();
  const cases = [
    [{ xfcc: gatewayXfcc, actorKind: 'human' }, null],
    [{ xfcc: gatewayXfcc, actorKind: '' }, null],
    [
      {
        xfcc: 'URI=spiffe://cluster.local/ns/dancehub/sa/orderservice',
        actorKind: 'service',
        servicePrincipal: 'orderservice',
      },
      null,
    ],
    [{ xfcc: '', actorKind: 'human' }, grpc.status.UNAUTHENTICATED],
    [
      { xfcc: gatewayXfcc, actorKind: 'service', servicePrincipal: 'orderservice' },
      grpc.status.UNAUTHENTICATED,
    ],
    [
      { xfcc: 'URI=spiffe://cluster.local/ns/dancehub/sa/chatservice', actorKind: 'human' },
      grpc.status.PERMISSION_DENIED,
    ],
    [
      { xfcc: 'URI=spiffe://cluster.local/ns/staging/sa/gatewayservice', actorKind: 'human' },
      grpc.status.PERMISSION_DENIED,
    ],
  ];
  for (const [input, expected] of cases) {
    const error = authorize(current, input);
    assert.equal(error?.code ?? null, expected, JSON.stringify(input));
  }
});

test('is inactive unless enforcement is enabled', () => {
  assert.equal(authorize(policyFromEnvironment({}), { xfcc: '', actorKind: 'service' }), null);
  assert.throws(() =>
    policyFromEnvironment({ MESH_PEER_ENFORCEMENT: 'true', MESH_NAMESPACE: 'dancehub' }),
  );
  assert.throws(() => parsePeers('gatewayservice,=worker'));
});

test('guarded handlers reject before business code runs', () => {
  const calls = [];
  const handlers = guardHandlers(policy(), {
    getPayment: (call, callback) => {
      calls.push('handled');
      callback(null, { ok: true });
    },
  });
  const metadata = new grpc.Metadata();
  metadata.set('x-actor-kind', 'service');
  metadata.set('x-service-principal', 'orderservice');
  metadata.set('x-forwarded-client-cert', gatewayXfcc);
  handlers.getPayment({ metadata }, (error, value) => {
    assert.equal(error.code, grpc.status.UNAUTHENTICATED);
    assert.equal(value, undefined);
  });
  metadata.set('x-forwarded-client-cert', 'URI=spiffe://cluster.local/ns/dancehub/sa/orderservice');
  handlers.getPayment({ metadata }, (error, value) => {
    assert.equal(error, null);
    assert.deepEqual(value, { ok: true });
  });
  assert.deepEqual(calls, ['handled']);
});
