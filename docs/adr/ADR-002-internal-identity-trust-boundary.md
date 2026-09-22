# ADR-002: Internal identity propagation and the trust boundary

Status: Accepted

## Context

Every request that reaches a domain service carries the caller's identity as gRPC metadata:
`x-user-id`, `x-studio-id`, `x-tenant-roles`, `x-global-roles`, `x-actor-kind`,
`x-service-principal` and `x-request-id`. The receiving side copies those values into the
`app.*` transaction variables that PostgreSQL row-level security evaluates, so whoever can
set the metadata effectively chooses the RLS identity.

The metadata is **asserted** by the caller:

- `gatewayservice` verifies the OIDC token, resolves memberships through Account Service,
  derives tenant/global roles for the selected studio, and only then emits the metadata
  (`src/dancehub/cmd/gatewayservice/auth.go`). It deletes any `X-Tenant-Roles`,
  `X-Global-Roles`, `X-Actor-Kind` or `X-Service-Principal` header a browser sends.
- Domain services forward the gateway-validated **human** identity on service-to-service hops
  (order fulfillment → Credit, booking → Credit, chat → Account/Catalog), so "human identity
  only ever comes from the gateway" is _not_ an invariant of this system.
- Workers assert `x-actor-kind: service` plus their own principal: `scheduling-worker`
  (inside `schedulingservice`), `orderservice` (order worker) and `payment-order-saga`.
- `platform.grpcIdentityInterceptor` (Go) validates the _shape_ of the metadata (a service
  identity may not also carry user/tenant fields, roles require an actor kind); the
  Node/Python/C# parsers only require a request id and copy the rest.

Without a way to prove _which workload_ sent the metadata, a process that can open a TCP
connection to a service's port 9090 can impersonate any user or worker.

## Decision

Bind asserted identity to the **workload identity proven by the service mesh**, and keep
network isolation as the second layer.

### Layer 1: mesh workload identity (Helm, `dancehub.serviceMesh.enabled`)

- Every Deployment runs under its own ServiceAccount (`templates/service-accounts.yaml`), so
  Istio / Cloud Service Mesh issues it a SPIFFE identity
  `spiffe://<trust-domain>/ns/<namespace>/sa/<workload>`. Pods still never mount the API token.
- A namespace-wide `PeerAuthentication` enforces `STRICT` mTLS. Only the public port of
  `gatewayservice` and of the three web apps is `PERMISSIVE`, because the Google Cloud load
  balancer terminates TLS and reaches those pods in plaintext.
- `allowedCallers` is translated into one `AuthorizationPolicy` per gRPC service that admits
  exactly those principals on the gRPC port (`templates/service-mesh.yaml`); `httpCallers`
  covers the Stripe webhook relay on the payment service's HTTP port; workers get a deny-all
  policy because nothing legitimately calls them.
- The same list becomes `MESH_ALLOWED_PEERS` on each gRPC service, together with
  `MESH_PEER_ENFORCEMENT`, `MESH_TRUST_DOMAIN` and `MESH_NAMESPACE`. Unknown caller names
  fail the render.

### Layer 2: application-level binding (all four gRPC languages)

Envoy records the peer certificate in `x-forwarded-client-cert`; under STRICT mTLS that
certificate can only have been issued by the mesh CA, so its SPIFFE URI names the caller's
ServiceAccount. Each service parses the **last** element of the header (the one appended by
its own sidecar, which an upstream hop cannot forge) and enforces:

1. the peer's trust domain and namespace match the deployment;
2. the peer's ServiceAccount is one of the service's allowed callers;
3. a `service` actor may only claim a principal bound to that ServiceAccount
   (`servicePrincipals` in `values.yaml`: `schedulingservice → scheduling-worker`,
   `orderservice → orderservice`, `payment-order-saga → payment-order-saga`);
4. human and anonymous identities are accepted from any allowed caller, because forwarding
   the gateway-validated user downstream is legitimate.

The gRPC health service is exempt so probes and the gateway's dependency check do not depend
on caller identity. Implementations: `internal/platform/mesh.go` (Go, unary and stream
interceptors), `src/paymentservice/mesh-peer.js`, `src/recommendationservice/mesh_peer.py`,
`MeshPeerPolicy.cs` in the credit and cart services; each has unit tests. With
`MESH_PEER_ENFORCEMENT` unset (Compose, Kustomize `local`/`dev`) the check is inactive and the
plain metadata contract applies, exactly as before.

### Layer 3: network isolation and RLS (unchanged)

- gRPC ports are never reachable from outside the cluster or the Compose network; Helm
  exposes only `gatewayservice:8080` and the web apps through the GKE Gateway.
- The production values also enable the NetworkPolicies generated from `allowedCallers`;
  the Kustomize overlays apply the equivalent `network-policies` component.
- RLS remains the final guard: a forged identity can only see what that identity may see.

## Alternative considered: signed actor context

A shared-secret HMAC over the identity fields (`x-actor-signature`, `x-actor-issued-at`)
would work without a mesh, including in Compose, and needs the same four verification points.
It was not chosen because it introduces a secret to rotate and a canonical string that every
language must reproduce byte-for-byte, whereas the mesh binds identity to the connection with
no application-side crypto. It remains the right option for a deployment without a mesh.

## Consequences

- Production needs Istio or Cloud Service Mesh with sidecar injection; the deploy workflow
  checks the CRDs and the injector before rolling out, and `SERVICE_MESH_ENABLED=false` falls
  back to network isolation only.
- Adding a gRPC service means declaring `allowedCallers` (and `servicePrincipals` if it asserts
  a service identity); the NetworkPolicy, the AuthorizationPolicy and `MESH_ALLOWED_PEERS` are
  derived from that one declaration, and CI validates the rendered resources against the
  Istio, Gateway API, KEDA and GKE schemas.
- The Compose and Kustomize development environments cannot exercise the mesh; the peer
  binding is covered by unit tests per language, and the mesh policies by rendering and schema
  validation. The first production rollout should be verified with `istioctl x authz check`
  and a deliberately unauthorized call from a debug pod.
- Application and domain code stay untouched: they already consume `ActorContext` rather
  than raw metadata.
