'use strict';

// Binds asserted identity metadata to the workload identity the service mesh proved during
// the mTLS handshake. Mirrors src/dancehub/internal/platform/mesh.go: the peer must be an
// allowed caller and a `service` actor may only claim a principal bound to the peer's
// ServiceAccount. Enforcement is off unless MESH_PEER_ENFORCEMENT=true, so Compose and the
// Kustomize development overlays keep working without a mesh.

const grpc = require('@grpc/grpc-js');

const SPIFFE_PATTERN = /^spiffe:\/\/([^/]+)\/ns\/([^/]+)\/sa\/([^/]+)$/;

function parsePeers(value) {
  const peers = new Map();
  for (const rawEntry of String(value || '').split(',')) {
    const entry = rawEntry.trim();
    if (!entry) continue;
    const [account, principalList = ''] = entry.split('=', 2);
    if (!account.trim())
      throw new Error(`MESH_ALLOWED_PEERS entry "${entry}" has no service account`);
    peers.set(
      account.trim(),
      new Set(
        principalList
          .split('|')
          .map((principal) => principal.trim())
          .filter(Boolean),
      ),
    );
  }
  return peers;
}

// Envoy appends the current client certificate as the last x-forwarded-client-cert
// element, so only the last element is trustworthy.
function peerServiceAccount(header) {
  const value = String(header || '').trim();
  if (!value) return null;
  const elements = value.split(',');
  const last = elements[elements.length - 1];
  for (const pair of last.split(';')) {
    const separator = pair.indexOf('=');
    if (separator === -1) continue;
    if (pair.slice(0, separator).trim().toLowerCase() !== 'uri') continue;
    const uri = pair
      .slice(separator + 1)
      .trim()
      .replace(/^"|"$/g, '');
    const match = SPIFFE_PATTERN.exec(uri);
    if (!match) return null;
    return { trustDomain: match[1], namespace: match[2], serviceAccount: match[3] };
  }
  return null;
}

function policyFromEnvironment(environment = process.env) {
  const enforce = String(environment.MESH_PEER_ENFORCEMENT || '').toLowerCase() === 'true';
  if (!enforce) return { enforce: false };
  const namespace = (environment.MESH_NAMESPACE || '').trim();
  if (!namespace) throw new Error('MESH_NAMESPACE is required when MESH_PEER_ENFORCEMENT is true');
  const peers = parsePeers(environment.MESH_ALLOWED_PEERS);
  if (peers.size === 0) {
    throw new Error(
      'MESH_ALLOWED_PEERS must list at least one caller when MESH_PEER_ENFORCEMENT is true',
    );
  }
  return {
    enforce: true,
    trustDomain: (environment.MESH_TRUST_DOMAIN || '').trim() || 'cluster.local',
    namespace,
    peers,
  };
}

const denied = (code, details) => Object.assign(new Error(details), { code, details });

// Returns null when the call is allowed, otherwise a gRPC-shaped error.
function authorize(policy, { xfcc, actorKind, servicePrincipal }) {
  if (!policy.enforce) return null;
  const peer = peerServiceAccount(xfcc);
  if (!peer) return denied(grpc.status.UNAUTHENTICATED, 'mesh peer identity is required');
  if (peer.trustDomain !== policy.trustDomain || peer.namespace !== policy.namespace) {
    return denied(
      grpc.status.PERMISSION_DENIED,
      'peer belongs to another trust domain or namespace',
    );
  }
  const principals = policy.peers.get(peer.serviceAccount);
  if (!principals) {
    return denied(grpc.status.PERMISSION_DENIED, 'peer workload is not an allowed caller');
  }
  if (actorKind === 'service' && !principals.has(servicePrincipal)) {
    return denied(
      grpc.status.UNAUTHENTICATED,
      'service principal is not bound to the peer workload',
    );
  }
  return null;
}

// Wraps unary gRPC handlers so the peer check runs before any business code.
function guardHandlers(policy, handlers) {
  const first = (call, key) => call.metadata.get(key)[0]?.toString() || '';
  const guarded = {};
  for (const [name, handler] of Object.entries(handlers)) {
    guarded[name] = (call, callback) => {
      const error = authorize(policy, {
        xfcc: first(call, 'x-forwarded-client-cert'),
        actorKind: first(call, 'x-actor-kind'),
        servicePrincipal: first(call, 'x-service-principal'),
      });
      if (error) return callback(error);
      return handler(call, callback);
    };
  }
  return guarded;
}

module.exports = {
  authorize,
  guardHandlers,
  parsePeers,
  peerServiceAccount,
  policyFromEnvironment,
};
