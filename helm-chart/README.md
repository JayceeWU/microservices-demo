# BayAreaDanceHub Helm chart

This chart provides the optional production Kubernetes integration for BayAreaDanceHub. It assumes managed PostgreSQL, Kafka, RabbitMQ, three Redis pools (Cart, Flash-sale, and Chat), OIDC, OpenTelemetry, and S3-compatible object storage. It does not install those dependencies. The complete local demonstration uses fake payments through Compose or the local/dev Kustomize overlays; see the [root README](../README.md#scope-and-limits).

The following parameter example shows the shared image repository prefix and commit tag. Run commands from the repository root. Before installing, supply the actual service endpoints, public hostnames, Gateway resources, and mesh settings through Helm value overrides, and create the required `dancehub-secrets` Secret in the target namespace. [values-production.yaml](./values-production.yaml) enables production features but retains example endpoints and hostnames from [values.yaml](./values.yaml), so this example alone is not a complete deployment command:

```sh
helm upgrade --install bay-area-dance-hub ./helm-chart \
  --namespace dancehub \
  --create-namespace \
  --atomic \
  --wait \
  -f helm-chart/values-production.yaml \
  --set-string images.repository=us-central1-docker.pkg.dev/PROJECT/REPOSITORY \
  --set-string images.tag=GIT_SHA
```

Production deployment is performed by [gke-production-deploy.yaml](../.github/workflows/gke-production-deploy.yaml), which supplies managed service endpoints and the four public hostnames. Its [configuration guide](../.github/workflows/README.md) lists the required Environment variables; the workflow also lists and checks every required Secret key. The workflow is manually dispatched and depends on the target environment's infrastructure and approval configuration.

## Required infrastructure

- GKE Gateway API with `gke-l7-global-external-managed` available.
- A named global static IP and Certificate Manager certificate map.
- KEDA when `dancehub.keda.enabled=true`.
- Istio or Cloud Service Mesh with sidecar injection when `dancehub.serviceMesh.enabled=true` (the production values enable it).
- The `dancehub-secrets` Secret, created by an external secrets controller or a cluster administrator.
- Managed PostgreSQL, Kafka, RabbitMQ, the Redis instances, Keycloak (OIDC) and S3-compatible object storage reachable from the cluster. Payroll consumes `scheduling.events.v1` using group `payroll-projector-v1`; Kafka connectivity is required by its startup and health check as well as by the Relay, Saga and Recommendation workloads.
- A ClamAV daemon reachable at `dancehub.config.clamavAddress`; `chat-media-worker` refuses attachments it cannot scan.

The deployment Environment supplies `OBJECT_STORAGE_ENDPOINT` for server-side storage access, `OBJECT_STORAGE_PUBLIC_ENDPOINT` for browser upload/download URLs, `OBJECT_STORAGE_BUCKET`, and `CLAMAV_ADDRESS` for scanning. The workflow passes all four into Helm; the public endpoint must be reachable from the browser and the scanner address from `chat-media-worker`. Object-storage credentials remain in `dancehub-secrets`. Stripe mode additionally requires Stripe credentials, a signed webhook endpoint and a payment-confirmation client; the repository's browser checkout uses local fake payments.

Run `npm run verify:local -- static` from the repository root to check production rendering, security and schema validation together with application checks. `npm run verify:local -- compose` and `npm run verify:local -- kubernetes --overlay all` exercise the isolated local environments and clean their test resources. These commands do not provision managed GKE infrastructure or issue real Stripe transactions.

## Workload identity

Every Deployment runs under its own ServiceAccount (pods still never mount the API token). With the mesh enabled the chart renders:

- a namespace-wide `PeerAuthentication` in `STRICT` mode, with the public port of `gatewayservice` and the three web apps left `PERMISSIVE` because the Google Cloud load balancer terminates TLS and reaches those pods in plaintext;
- one `AuthorizationPolicy` per gRPC service that admits exactly the SPIFFE principals of its `allowedCallers` on the gRPC port (plus `httpCallers` on the health port, used for the Stripe webhook relay), and a deny-all policy for every worker;
- `MESH_PEER_ENFORCEMENT`, `MESH_TRUST_DOMAIN`, `MESH_NAMESPACE` and `MESH_ALLOWED_PEERS` on every gRPC service, so the Go, Node.js, Python and C# services additionally verify the peer certificate Envoy forwards in `x-forwarded-client-cert` and bind asserted service principals (`servicePrincipals`) to the caller's ServiceAccount.

`dancehub.serviceMesh.trustDomain` must match the mesh (`cluster.local` for self-managed Istio, `<PROJECT_ID>.svc.id.goog` for Cloud Service Mesh); `dancehub.serviceMesh.revision` sets the `istio.io/rev` injection label. The migration Job is never injected so it can complete.

`allowedCallers`, `servicePrincipals` and `httpCallers` are the single source for the NetworkPolicies, the AuthorizationPolicies and the per-service `MESH_ALLOWED_PEERS` value; unknown caller names fail the render.

The migration hook consumes administrator and runtime-role credentials from `dancehub-secrets`. Application Deployments receive only their own runtime DSN and service-specific secrets.

The migration Job runs as a `pre-install,pre-upgrade` hook, so it must not depend on ordinary chart resources: its `dancehub-migrations` ServiceAccount is itself a hook with a lower weight (`-5`) and is created first on a fresh namespace. Helm never deletes hook resources on `helm uninstall`; remove that ServiceAccount by hand if the namespace is reused for something else.

The Student, Teacher, Admin, and API hosts are routed by four `HTTPRoute` resources through one HTTPS `Gateway`. Static Web images generate `runtime-config.js` at container startup; Student Web injects the same public configuration from server runtime environment variables.
