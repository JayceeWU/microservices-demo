# BayAreaDanceHub workflows

The repository has two supported workflows:

- [dancehub-quality.yaml](./dancehub-quality.yaml) is the continuous integration workflow. It runs for pull requests and pushes to every branch, including `dance-dev`, and supports manual runs. It defines checks for English repository content, formatting, linting, contracts and generated code, application tests, isolated Compose acceptance, browser tests, SQL/RLS, and Helm/Kustomize rendering with security-baseline checks plus schema validation of rendered resources (Kubernetes, Gateway API, KEDA, GKE and Istio).
- [gke-production-deploy.yaml](./gke-production-deploy.yaml) is the optional production deployment integration. It is manual, uses Google Workload Identity Federation, and targets the `gke-production` GitHub Environment; configure its required reviewers to enforce approval.

The quality workflow installs dependencies with both the root `npm ci` and `npm --prefix src/paymentservice ci` before running tests. The root [workspace list](../../package.json) includes only `apps/*` and `packages/*`; [src/paymentservice](../../src/paymentservice/package.json) has its own dependencies and lockfile. Check the workflow run for the selected commit to determine which checks passed; the configured checks alone are not acceptance evidence.

The root [global.json](../../global.json) selects the latest installed stable .NET 9.0 SDK, so a newer preinstalled major SDK on the runner does not override the acceptance environment.

The [repository language check](../../scripts/check-repository-language.mjs) rejects Chinese characters in repository filenames and text, including encoded text, to keep future changes consistent with the English content policy. Run it locally with `npm run language:check`.

The [development configuration checks](../../scripts/kubernetes-development-checks.mjs) run against the rendered local and dev manifests. They verify required network connections and isolation, local Secret/ConfigMap references, Redis persistence, Keycloak startup storage, and MinIO/OTLP endpoints. The workflow also runs their [Node regression tests](../../scripts/kubernetes-development-checks.test.mjs), the Kubernetes security-checker tests and the acceptance-runner tests. These static checks do not replace a cluster with an enforcing CNI or browser acceptance tests.

The shared local entry points are:

```sh
npm run verify:local -- static
npm run verify:local -- compose
npm run verify:local -- kubernetes --overlay all
```

CI invokes the Compose entry point for the complete fake-payment environment, SQL and business recovery checks, and browser tests. The Kubernetes entry point supports `--overlay local`, `dev` or `all` (default), creates a dedicated Kind/Calico cluster and runs network, Redis, telemetry and browser checks. It is a separate local command, not an automatic GKE deployment.

The runner supports Windows/Linux x64 and selects verified Node.js `22.16.0`, Go `1.25.0` and pinned deployment tools from `.cache/acceptance-tools`; host prerequisites and exact versions are listed in the [root verification guide](../../README.md#development-and-verification). Each run writes diagnostics and browser artifacts beneath `.cache/acceptance/<run>/`. Runtime modes remove their own Compose volumes or Kind cluster and stop their background processes, while retaining reports and reusable dependency/tool/image caches. Generation checks compare against pre-run file snapshots and do not reset or commit the working tree; the CI contract job additionally checks committed generated files against Git.

Configure required reviewers on the `gke-production` Environment before enabling deployment. Store endpoints and resource names as Environment variables. Store application credentials only in the externally managed Kubernetes Secret named `dancehub-secrets`; the workflow verifies required keys without printing their values.

Required Environment variables are `GCP_PROJECT_ID`, `GCP_REGION`, `GKE_CLUSTER`, `ARTIFACT_REGISTRY_REPOSITORY`, `K8S_NAMESPACE`, `GCP_WORKLOAD_IDENTITY_PROVIDER`, `GCP_DEPLOY_SERVICE_ACCOUNT`, `KAFKA_BOOTSTRAP_SERVERS`, `CART_REDIS_ADDRESS`, `FLASHSALE_REDIS_ADDRESS`, `CHAT_REDIS_ADDRESS`, `OIDC_ISSUER`, `OTEL_HTTP_ENDPOINT`, `OTEL_GRPC_ENDPOINT`, `OBJECT_STORAGE_ENDPOINT`, `OBJECT_STORAGE_PUBLIC_ENDPOINT`, `OBJECT_STORAGE_BUCKET`, `CLAMAV_ADDRESS`, `GATEWAY_STATIC_IP_NAME`, `CERTIFICATE_MAP_NAME`, `STUDENT_HOSTNAME`, `TEACHER_HOSTNAME`, `ADMIN_HOSTNAME`, and `API_HOSTNAME`. `KEDA_ENABLED` is optional and defaults to `true`; `OIDC_JWKS_URL` is optional and defaults to the Keycloak convention `<OIDC_ISSUER>/protocol/openid-connect/certs`. `SERVICE_MESH_ENABLED` (default `true`) renders the Istio / Cloud Service Mesh resources and turns on peer-identity enforcement in every gRPC service; when it is enabled the workflow requires the Istio CRDs and a sidecar injector in the cluster, `SERVICE_MESH_TRUST_DOMAIN` must match the mesh (`cluster.local` for self-managed Istio, `<PROJECT_ID>.svc.id.goog` for Cloud Service Mesh) and `SERVICE_MESH_REVISION` selects a revision-based injector such as `asm-managed`.

The production Environment supplies separate `OTEL_HTTP_ENDPOINT` and `OTEL_GRPC_ENDPOINT` values because the Go and Node.js SDKs use OTLP/HTTP while the .NET and Python SDKs use OTLP/gRPC. It also supplies separate server-side and browser-facing object-storage endpoints, the bucket, and the ClamAV address. `OBJECT_STORAGE_PUBLIC_ENDPOINT` maps to `dancehub.config.objectStoragePublicEndpoint`; `CLAMAV_ADDRESS` maps to `dancehub.config.clamavAddress`. Object-storage credentials remain in `dancehub-secrets`.

Pull-request code is never executed through a privileged target-context event, and pull requests do not create cloud namespaces or deployments.
