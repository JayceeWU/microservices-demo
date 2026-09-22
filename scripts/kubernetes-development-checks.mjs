import yaml from 'js-yaml';

const GRPC_SERVICES = [
  'accountservice',
  'catalogservice',
  'creditservice',
  'schedulingservice',
  'orderservice',
  'payrollservice',
  'paymentservice',
  'recommendationservice',
  'cartservice',
  'chatservice',
];
const GRPC_OTEL = new Set([
  'creditservice',
  'cartservice',
  'recommendationservice',
  'outbox-relay',
  'payment-order-saga',
]);
const BACKENDS = [
  'gatewayservice',
  ...GRPC_SERVICES,
  'outbox-relay',
  'payment-order-saga',
  'chat-media-worker',
];

function namespaceOf(document) {
  return document.metadata?.namespace ?? 'default';
}

function podTemplate(document) {
  if (document.kind === 'CronJob') return document.spec?.jobTemplate?.spec?.template;
  if (['Deployment', 'StatefulSet', 'DaemonSet', 'ReplicaSet', 'Job'].includes(document.kind)) {
    return document.spec?.template;
  }
  return undefined;
}

export function matchesSelector(selector = {}, labels = {}) {
  if (!Object.entries(selector.matchLabels ?? {}).every(([key, value]) => labels[key] === value)) {
    return false;
  }
  return (selector.matchExpressions ?? []).every(({ key, operator, values = [] }) => {
    switch (operator) {
      case 'In':
        return values.includes(labels[key]);
      case 'NotIn':
        return !values.includes(labels[key]);
      case 'Exists':
        return Object.hasOwn(labels, key);
      case 'DoesNotExist':
        return !Object.hasOwn(labels, key);
      default:
        throw new Error(`Unsupported NetworkPolicy selector operator: ${operator}`);
    }
  });
}

function ipv4(address) {
  const parts = address?.split('.') ?? [];
  if (parts.length !== 4 || parts.some((part) => !/^\d+$/.test(part) || Number(part) > 255)) {
    throw new Error(`Connectivity checks require IPv4 addresses, got ${address}`);
  }
  return parts.reduce((value, part) => ((value << 8) | Number(part)) >>> 0, 0);
}

function inCidr(address, cidr) {
  const [network, prefix] = cidr.split('/');
  const bits = Number(prefix);
  if (!/^\d+$/.test(prefix ?? '') || bits < 0 || bits > 32) {
    throw new Error(`Invalid IPv4 CIDR in NetworkPolicy: ${cidr}`);
  }
  const mask = bits === 0 ? 0 : (0xffffffff << (32 - bits)) >>> 0;
  return (ipv4(address) & mask) === (ipv4(network) & mask);
}

function peerMatches(peer, endpoint, policyNamespace) {
  if (peer.ipBlock) {
    return (
      Boolean(endpoint.ip) &&
      inCidr(endpoint.ip, peer.ipBlock.cidr) &&
      !(peer.ipBlock.except ?? []).some((cidr) => inCidr(endpoint.ip, cidr))
    );
  }
  if (peer.namespaceSelector || peer.podSelector) {
    if (endpoint.external) return false;
    if (peer.namespaceSelector) {
      if (!matchesSelector(peer.namespaceSelector, endpoint.namespaceLabels)) return false;
    } else if (endpoint.namespace !== policyNamespace) return false;
    return !peer.podSelector || matchesSelector(peer.podSelector, endpoint.labels);
  }
  return true;
}

function portMatches(entry, destination, port, protocol) {
  if ((entry.protocol ?? 'TCP') !== protocol) return false;
  if (entry.port === undefined) return true;
  if (typeof entry.port === 'string') {
    return (
      destination.ports?.some(
        (candidate) =>
          candidate.name === entry.port &&
          candidate.containerPort === port &&
          (candidate.protocol ?? 'TCP') === protocol,
      ) ?? false
    );
  }
  return port >= entry.port && port <= (entry.endPort ?? entry.port);
}

// This checks declarative Pod policy semantics, not CNI enforcement, DNS, readiness,
// Service NAT or kubelet/port-forward traffic. Reply packets need no reverse rule.
export function createNetworkModel(documents) {
  const policies = documents.filter((document) => document?.kind === 'NetworkPolicy');
  const namespaces = new Map(
    documents
      .filter((document) => document?.kind === 'Namespace')
      .map((document) => [document.metadata.name, document.metadata.labels ?? {}]),
  );
  const endpoints = new Map();
  for (const document of documents) {
    const template = podTemplate(document ?? {});
    if (!template) continue;
    const name = document.metadata.name;
    if (endpoints.has(name)) throw new Error(`Duplicate development workload: ${name}`);
    const namespace = namespaceOf(document);
    endpoints.set(name, {
      name,
      namespace,
      id: `${namespace}/${name}`,
      labels: template.metadata?.labels ?? {},
      namespaceLabels: { ...namespaces.get(namespace), 'kubernetes.io/metadata.name': namespace },
      // Stable test addresses let ipBlock rules participate in the policy union.
      ip: `10.244.0.${endpoints.size + 1}`,
      ports: (template.spec?.containers ?? []).flatMap((container) => container.ports ?? []),
      document,
      podSpec: template.spec,
    });
  }

  function directionAllowed(endpoint, other, direction, port, protocol) {
    if (endpoint.external) return true;
    const selected = policies.filter((policy) => {
      const types = policy.spec.policyTypes ?? [
        'Ingress',
        ...((policy.spec.egress?.length ?? 0) > 0 ? ['Egress'] : []),
      ];
      return (
        namespaceOf(policy) === endpoint.namespace &&
        types.includes(direction) &&
        matchesSelector(policy.spec.podSelector, endpoint.labels)
      );
    });
    if (selected.length === 0) return true;
    const destination = direction === 'Egress' ? other : endpoint;
    return selected.some((policy) =>
      (policy.spec[direction.toLowerCase()] ?? []).some((rule) => {
        const peers = rule[direction === 'Egress' ? 'to' : 'from'];
        return (
          (!peers?.length || peers.some((peer) => peerMatches(peer, other, namespaceOf(policy)))) &&
          (!rule.ports?.length ||
            rule.ports.some((entry) => portMatches(entry, destination, port, protocol)))
        );
      }),
    );
  }

  function endpoint(value) {
    if (typeof value !== 'string') return value;
    const result = endpoints.get(value);
    if (!result) throw new Error(`Missing development workload: ${value}`);
    return result;
  }

  function connection(sourceValue, destinationValue, port, protocol = 'TCP') {
    const source = endpoint(sourceValue);
    const destination = endpoint(destinationValue);
    if (source.id && source.id === destination.id)
      return { allowed: true, egress: true, ingress: true };
    const egress = directionAllowed(source, destination, 'Egress', port, protocol);
    const ingress = directionAllowed(destination, source, 'Ingress', port, protocol);
    return { allowed: egress && ingress, egress, ingress };
  }
  return { endpoints, connection, directionAllowed };
}

export function validateDevelopmentConnectivity(documents) {
  const failures = [];
  const model = createNetworkModel(documents);
  const services = documents.filter((document) => document?.kind === 'Service');
  function check(source, destination, port, allowed = true, protocol = 'TCP') {
    const label = `${typeof source === 'string' ? source : source.name} -> ${typeof destination === 'string' ? destination : destination.name} ${protocol}/${port}`;
    try {
      const result = model.connection(source, destination, port, protocol);
      if (result.allowed !== allowed)
        failures.push(
          `${label}: expected ${allowed ? 'allowed' : 'denied'} (egress=${result.egress}, ingress=${result.ingress})`,
        );
      if (allowed && typeof destination === 'string') {
        const pod = model.endpoints.get(destination);
        const service = services.find(
          (item) => item.metadata.name === destination && namespaceOf(item) === pod.namespace,
        );
        if (
          !service ||
          Object.keys(service.spec.selector ?? {}).length === 0 ||
          !matchesSelector({ matchLabels: service.spec.selector }, pod.labels) ||
          !service.spec.ports?.some(
            (item) =>
              item.port === port &&
              (item.protocol ?? 'TCP') === protocol &&
              (item.targetPort === undefined ||
                item.targetPort === port ||
                pod.ports.some(
                  (entry) =>
                    entry.name === item.targetPort &&
                    entry.containerPort === port &&
                    (entry.protocol ?? 'TCP') === protocol,
                )),
          )
        ) {
          failures.push(`${label}: Service must route this port to the destination workload`);
        }
      }
    } catch (error) {
      failures.push(`${label}: ${error.message}`);
    }
  }

  for (const source of [
    'dancehub-migrations',
    'dancehub-seed',
    'keycloak',
    ...GRPC_SERVICES.filter((name) => name !== 'cartservice'),
    'outbox-relay',
    'chat-media-worker',
  ]) {
    check(source, 'postgres', 5432);
  }
  for (const [name, pod] of model.endpoints) {
    if (pod.podSpec.initContainers?.some((container) => container.name === 'wait-for-migrations')) {
      check(name, 'postgres', 5432);
    }
  }
  for (const name of GRPC_SERVICES) check('gatewayservice', name, 9090);
  for (const source of ['student-web', 'gatewayservice']) check(source, 'keycloak', 8080);
  check('gatewayservice', 'paymentservice', 8080);
  for (const [source, targets] of [
    ['schedulingservice', ['creditservice', 'catalogservice']],
    ['orderservice', ['creditservice', 'schedulingservice', 'catalogservice', 'paymentservice']],
    ['chatservice', ['accountservice', 'catalogservice']],
    ['payment-order-saga', ['orderservice']],
  ])
    for (const target of targets) check(source, target, 9090);
  for (const [source, target, port] of [
    ['cartservice', 'redis-cart', 6379],
    ['orderservice', 'redis-flashsale', 6379],
    ['chatservice', 'redis-chat', 6379],
    ['chat-media-worker', 'clamav', 3310],
    ['otel-collector', 'jaeger', 4317],
  ])
    check(source, target, port);
  for (const source of ['orderservice', 'chatservice', 'chat-media-worker'])
    check(source, 'rabbitmq', 5672);
  for (const source of ['chatservice', 'chat-media-worker']) check(source, 'minio', 9000);
  for (const source of [
    'outbox-relay',
    'payment-order-saga',
    'recommendationservice',
    'payrollservice',
  ])
    check(source, 'kafka', 29092);
  const kafka = model.endpoints.get('kafka');
  if (kafka) {
    // Service hairpin traffic must have explicit allowances. Use a distinct
    // identity with Kafka's labels so the same-Pod exception cannot hide a gap.
    check(
      {
        ...kafka,
        name: 'kafka-controller-client',
        id: `${kafka.id}-controller-client`,
        ip: '10.244.3.1',
      },
      'kafka',
      29093,
    );
  }
  for (const source of BACKENDS)
    check(source, 'otel-collector', GRPC_OTEL.has(source) ? 4317 : 4318);

  const dns = {
    name: 'cluster-dns',
    namespace: 'kube-system',
    labels: { 'k8s-app': 'kube-dns' },
    namespaceLabels: { 'kubernetes.io/metadata.name': 'kube-system' },
    ip: '10.96.0.10',
  };
  for (const [name, pod] of model.endpoints) {
    for (const protocol of ['UDP', 'TCP']) {
      if (!model.directionAllowed(pod, dns, 'Egress', 53, protocol))
        failures.push(`${name}: DNS ${protocol}/53 egress is denied`);
    }
  }
  const internet = { name: 'internet', ip: '203.0.113.10', external: true };
  check('clamav', internet, 443);
  check('clamav', internet, 22, false);
  check('clamav', internet, 80, false);
  for (const ip of ['10.0.0.10', '172.16.0.10', '192.168.0.10', '127.0.0.1', '169.254.169.254']) {
    check('clamav', { name: `private-${ip}`, ip, external: true }, 443, false);
  }

  const namespace = model.endpoints.get('postgres')?.namespace ?? 'default';
  const unknown = {
    name: 'unknown-pod',
    namespace,
    labels: { app: 'unknown' },
    namespaceLabels: { 'kubernetes.io/metadata.name': namespace },
    ip: '10.244.1.1',
  };
  // Test every trusted label set: limiting this to a foreign gateway would miss,
  // for example, accidentally allowing a seed Job from any namespace.
  const foreignNamespace = `${namespace}-untrusted`;
  const foreignPods = [...model.endpoints.values()].map((pod, index) => ({
    name: `foreign-${pod.name}`,
    id: `${foreignNamespace}/${pod.name}`,
    namespace: foreignNamespace,
    labels: { ...pod.labels },
    namespaceLabels: { 'kubernetes.io/metadata.name': foreignNamespace },
    ip: `10.244.2.${index + 1}`,
  }));
  const protectedTargets = [
    ['postgres', 5432],
    ['redis-cart', 6379],
    ['redis-flashsale', 6379],
    ['redis-chat', 6379],
    ['rabbitmq', 5672],
    ['minio', 9000],
    ['kafka', 29092],
    ['kafka', 29093],
    ['clamav', 3310],
    ['otel-collector', 4317],
    ['otel-collector', 4318],
    ['jaeger', 4317],
    ...GRPC_SERVICES.map((name) => [name, 9090]),
  ];
  for (const source of [unknown, ...foreignPods])
    for (const [target, port] of protectedTargets) check(source, target, port, false);
  for (const source of ['dancehub-migrations', 'dancehub-seed']) {
    for (const target of GRPC_SERVICES) check(source, target, 9090, false);
    for (const target of ['redis-cart', 'redis-flashsale', 'redis-chat'])
      check(source, target, 6379, false);
    check(source, 'minio', 9000, false);
  }
  for (const source of ['keycloak', 'otel-collector', 'clamav']) {
    check(source, 'accountservice', 9090, false);
  }
  check('keycloak', 'kafka', 29092, false);
  check('otel-collector', 'postgres', 5432, false);
  check('clamav', 'postgres', 5432, false);
  check('payment-order-saga', 'accountservice', 9090, false);
  return failures;
}

export function validateDevelopmentConfiguration(documents, skaffold) {
  const failures = [];
  const { endpoints } = createNetworkModel(documents);
  // Development renders are self-contained. Index names and keys only; never
  // include Secret values in diagnostics or decode their data.
  const localResources = new Map(
    documents
      .filter((document) => ['Secret', 'ConfigMap'].includes(document?.kind))
      .map((document) => [
        `${document.kind}/${namespaceOf(document)}/${document.metadata.name}`,
        new Set([
          ...Object.keys(document.data ?? {}),
          ...(document.kind === 'Secret' ? Object.keys(document.stringData ?? {}) : []),
        ]),
      ]),
  );
  for (const { name, namespace, podSpec } of endpoints.values()) {
    for (const type of ['containers', 'initContainers']) {
      for (const container of podSpec[type] ?? []) {
        const context = `${name}: ${type}[${container.name}]`;
        function checkReference(kind, reference, location, keyed) {
          if (!reference) return;
          const resource = `${kind}/${namespace}/${reference.name}`;
          const keys = localResources.get(resource);
          if (!keys) {
            if (reference.optional !== true)
              failures.push(`${context} ${location}: missing local ${resource}`);
          } else if (keyed && !keys.has(reference.key) && reference.optional !== true) {
            failures.push(`${context} ${location}: ${resource} is missing key ${reference.key}`);
          }
        }
        for (const entry of container.env ?? []) {
          checkReference('Secret', entry.valueFrom?.secretKeyRef, `env ${entry.name}`, true);
          checkReference('ConfigMap', entry.valueFrom?.configMapKeyRef, `env ${entry.name}`, true);
        }
        for (const [index, entry] of (container.envFrom ?? []).entries()) {
          checkReference('Secret', entry.secretRef, `envFrom[${index}]`, false);
          checkReference('ConfigMap', entry.configMapRef, `envFrom[${index}]`, false);
        }
      }
    }
  }
  function requireEnvironment(name, key, expected) {
    const values = (endpoints.get(name)?.podSpec.containers ?? [])
      .flatMap((container) => container.env ?? [])
      .filter((entry) => entry.name === key);
    if (values.length !== 1 || values[0].value !== expected)
      failures.push(`${name}: ${key} must occur once with value ${expected}`);
  }
  for (const name of BACKENDS)
    requireEnvironment(
      name,
      'OTEL_EXPORTER_OTLP_ENDPOINT',
      `http://otel-collector:${GRPC_OTEL.has(name) ? 4317 : 4318}`,
    );
  requireEnvironment('chatservice', 'OBJECT_STORAGE_ENDPOINT', 'minio:9000');
  requireEnvironment('chatservice', 'OBJECT_STORAGE_PUBLIC_ENDPOINT', 'localhost:9000');
  requireEnvironment('chatservice', 'OBJECT_STORAGE_SECURE', 'false');
  if (
    !skaffold.portForward?.some(
      (entry) =>
        entry.resourceType === 'service' &&
        entry.resourceName === 'minio' &&
        entry.port === 9000 &&
        entry.localPort === 9000,
    )
  ) {
    failures.push('skaffold: MinIO service port 9000 must forward to localhost:9000');
  }
  const keycloak = endpoints.get('keycloak')?.podSpec;
  const keycloakContainer = keycloak?.containers?.find(
    (container) => container.name === 'keycloak',
  );
  const prepareQuarkus = keycloak?.initContainers?.find(
    (container) => container.name === 'prepare-quarkus',
  );
  const quarkusMount = keycloakContainer?.volumeMounts?.find(
    (mount) => mount.mountPath === '/opt/keycloak/lib/quarkus',
  );
  const quarkusVolume = keycloak?.volumes?.find((volume) => volume.name === quarkusMount?.name);
  const prepareMount = prepareQuarkus?.volumeMounts?.find(
    (mount) => mount.name === quarkusMount?.name && mount.mountPath === '/quarkus',
  );
  if (
    !quarkusMount ||
    quarkusMount.readOnly ||
    quarkusMount.subPath ||
    quarkusMount.subPathExpr ||
    !quarkusVolume ||
    !Object.hasOwn(quarkusVolume, 'emptyDir') ||
    !prepareMount ||
    prepareMount.readOnly ||
    prepareMount.subPath ||
    prepareMount.subPathExpr ||
    !keycloakContainer?.image ||
    prepareQuarkus?.image !== keycloakContainer.image ||
    JSON.stringify(prepareQuarkus?.command) !== JSON.stringify(['/bin/bash', '-ec']) ||
    JSON.stringify(prepareQuarkus?.args) !==
      JSON.stringify(['cp -R /opt/keycloak/lib/quarkus/. /quarkus/'])
  ) {
    failures.push(
      'keycloak: prepare-quarkus must copy the matching image artifacts into a shared writable emptyDir mounted at /opt/keycloak/lib/quarkus',
    );
  }
  const startup = keycloakContainer?.startupProbe;
  if (
    startup?.tcpSocket?.port !== 8080 ||
    (startup.failureThreshold ?? 3) * (startup.periodSeconds ?? 10) < 300
  ) {
    failures.push(
      'keycloak: TCP/8080 startupProbe must allow at least 300 seconds for initial Quarkus build',
    );
  }
  const redis = endpoints.get('redis-flashsale');
  const document = redis?.document;
  if (document?.kind !== 'StatefulSet' || document.spec.replicas !== 1)
    failures.push('redis-flashsale: one StatefulSet replica is required');
  const dataMount = redis?.podSpec.containers?.[0]?.volumeMounts?.find(
    (mount) => mount.mountPath === '/data' && mount.name === 'data' && !mount.readOnly,
  );
  const claim = document?.spec.volumeClaimTemplates?.find(
    (item) => item.metadata.name === dataMount?.name,
  );
  if (
    !dataMount ||
    !claim ||
    claim.metadata.name !== 'data' ||
    claim.spec.accessModes?.length !== 1 ||
    claim.spec.accessModes[0] !== 'ReadWriteOnce' ||
    claim.spec.resources?.requests?.storage !== '2Gi' ||
    Object.hasOwn(claim.spec, 'storageClassName') ||
    redis?.podSpec.volumes?.some((volume) => volume.name === 'data')
  )
    failures.push(
      'redis-flashsale: /data must mount the data 2Gi ReadWriteOnce claim template using the default StorageClass, without a Pod volume override',
    );
  const retention = document?.spec.persistentVolumeClaimRetentionPolicy;
  if (
    (retention?.whenDeleted ?? 'Retain') !== 'Retain' ||
    (retention?.whenScaled ?? 'Retain') !== 'Retain'
  )
    failures.push('redis-flashsale: PVC retention must be Retain');
  const args = redis?.podSpec.containers?.[0]?.args ?? [];
  for (const [flag, value] of [
    ['--dir', '/data'],
    ['--appendonly', 'yes'],
    ['--appendfsync', 'everysec'],
    ['--maxmemory-policy', 'noeviction'],
  ]) {
    if (
      args.filter((argument) => argument === flag).length !== 1 ||
      args[args.indexOf(flag) + 1] !== value
    )
      failures.push(`redis-flashsale: ${flag} must occur once with value ${value}`);
  }
  const headless = documents.find(
    (item) =>
      item?.kind === 'Service' &&
      item.metadata.name === document?.spec.serviceName &&
      namespaceOf(item) === redis?.namespace,
  );
  if (
    !headless ||
    headless.spec.clusterIP !== 'None' ||
    Object.keys(headless.spec.selector ?? {}).length === 0 ||
    !matchesSelector({ matchLabels: headless.spec.selector }, redis?.labels)
  )
    failures.push('redis-flashsale: StatefulSet must use a matching headless Service');

  const collectorConfig = documents.find(
    (item) => item?.kind === 'ConfigMap' && item.metadata.name === 'otel-collector-config',
  );
  try {
    const config = yaml.load(collectorConfig?.data?.['config.yaml'] ?? '');
    if (
      config?.receivers?.otlp?.protocols?.grpc?.endpoint !== '0.0.0.0:4317' ||
      config?.receivers?.otlp?.protocols?.http?.endpoint !== '0.0.0.0:4318'
    )
      failures.push('otel-collector: HTTP/gRPC receivers must match backend endpoint ports');
    if (config?.exporters?.['otlp/jaeger']?.endpoint !== 'jaeger:4317')
      failures.push('otel-collector: trace export must target jaeger:4317');
    if (config?.exporters?.prometheus?.endpoint !== '0.0.0.0:8889')
      failures.push('otel-collector: Prometheus export must listen on 0.0.0.0:8889');
    for (const [signal, exporter] of [
      ['traces', 'otlp/jaeger'],
      ['metrics', 'prometheus'],
    ]) {
      const pipeline = config?.service?.pipelines?.[signal];
      if (
        !Array.isArray(pipeline?.receivers) ||
        !pipeline.receivers.includes('otlp') ||
        !Array.isArray(pipeline?.exporters) ||
        !pipeline.exporters.includes(exporter)
      )
        failures.push(
          `otel-collector: ${signal} pipeline must connect the otlp receiver to ${exporter}`,
        );
    }
  } catch (error) {
    failures.push(`otel-collector: invalid configuration: ${error.message}`);
  }
  return failures;
}
