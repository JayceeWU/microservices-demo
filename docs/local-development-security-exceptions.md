# Local development container security exceptions

Kustomize supports `local` and `dev` only. Production is deployed exclusively with
`helm-chart/values-production.yaml`, and the production render is required to contain only
first-party workloads plus references to managed dependencies and existing secrets.

In Kustomize, all first-party workloads, along with Keycloak, the OpenTelemetry Collector, and
Jaeger, use the repository's Restricted-style pod and container security baseline. This includes
numeric non-root users, read-only root filesystems, disabled service account token automount,
`RuntimeDefault` seccomp, and writable temporary/cache volumes. It does not apply to every
third-party workload. The following
third-party workloads are allowed to use their upstream root-initialization entrypoints only
in local/dev:

| Workload                                      | Reason                                                                         | Compensating controls                                                                   |
| --------------------------------------------- | ------------------------------------------------------------------------------ | --------------------------------------------------------------------------------------- |
| `postgres`                                    | Creates and fixes ownership of the database volume before dropping privileges. | Local-only PVC, isolated namespace/network policy, never rendered by Helm.              |
| `redis-cart`, `redis-flashsale`, `redis-chat` | Fix ownership of persistent Redis data before dropping privileges.             | Local-only data, isolated network policy, never rendered by Helm.                       |
| `rabbitmq`                                    | Initializes broker storage and its Erlang cookie before dropping privileges.   | Development credentials only, isolated network policy, never rendered by Helm.          |
| `kafka`                                       | Initializes and fixes ownership of broker storage.                             | Single-node development broker, isolated network policy, never rendered by Helm.        |
| `minio`                                       | Initializes and fixes ownership of object storage.                             | Development credentials and data only, isolated network policy, never rendered by Helm. |
| `clamav`                                      | Updates its writable virus-signature database during startup.                  | No host storage or public ingress, isolated network policy, never rendered by Helm.     |

`scripts/check-kubernetes-security.mjs` owns this fixed allowlist. Any new workload that is not
hardened or explicitly documented causes CI to fail.

Redis, RabbitMQ, Kafka, MinIO, and ClamAV have ingress NetworkPolicies limiting callers and
ports. These controls require a CNI that enforces NetworkPolicy. The flash-sale Redis uses a
StatefulSet with a `2Gi` persistent volume claim, AOF, and `noeviction` so accepted requests in
its Stream survive Pod replacement; it remains a single-instance development dependency.
