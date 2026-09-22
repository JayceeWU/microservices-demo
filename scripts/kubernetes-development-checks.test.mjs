import assert from 'node:assert/strict';
import fs from 'node:fs';
import test from 'node:test';
import yaml from 'js-yaml';
import {
  createNetworkModel,
  matchesSelector,
  validateDevelopmentConnectivity,
  validateDevelopmentConfiguration,
} from './kubernetes-development-checks.mjs';

const pod = (name, namespace = 'default') => ({
  apiVersion: 'apps/v1',
  kind: 'Deployment',
  metadata: { name, namespace },
  spec: {
    template: {
      metadata: { labels: { app: name } },
      spec: {
        containers: [{ name, ports: [{ name: 'sql', containerPort: 5432 }] }],
      },
    },
  },
});
const policy = (name, app, direction, rules, namespace = 'default') => ({
  kind: 'NetworkPolicy',
  metadata: { name, namespace },
  spec: {
    podSelector: app ? { matchLabels: { app } } : {},
    policyTypes: [direction],
    [direction.toLowerCase()]: rules,
  },
});
const peer = (app) => ({ podSelector: { matchLabels: { app } } });
function databaseFixture() {
  return [
    pod('seed'),
    pod('database'),
    policy('deny-in', null, 'Ingress', []),
    policy('deny-out', null, 'Egress', []),
    policy('seed-egress', 'seed', 'Egress', [{ to: [peer('database')], ports: [{ port: 5432 }] }]),
    policy('database-ingress', 'database', 'Ingress', [
      { from: [peer('seed')], ports: [{ port: 5432 }] },
    ]),
  ];
}

test('a connection needs both ingress and egress; deleting either allowance blocks it', () => {
  const documents = databaseFixture();
  assert.equal(createNetworkModel(documents).connection('seed', 'database', 5432).allowed, true);
  for (const removed of ['seed-egress', 'database-ingress']) {
    const model = createNetworkModel(
      documents.filter((document) => document.metadata.name !== removed),
    );
    assert.equal(model.connection('seed', 'database', 5432).allowed, false);
  }
});

test('allowances are additive, ordered independently, and scoped to protocol and port', () => {
  const documents = databaseFixture();
  const model = createNetworkModel(documents.reverse());
  assert.equal(model.connection('seed', 'database', 5432).allowed, true);
  assert.equal(model.connection('seed', 'database', 5433).allowed, false);
  assert.equal(model.connection('seed', 'database', 5432, 'UDP').allowed, false);
});

test('an absent policy permits traffic, an empty rule list denies, an empty rule allows', () => {
  assert.equal(createNetworkModel([pod('a'), pod('b')]).connection('a', 'b', 123).allowed, true);
  const docs = [pod('a'), pod('b'), policy('deny', 'b', 'Ingress', [])];
  assert.equal(createNetworkModel(docs).connection('a', 'b', 123).allowed, false);
  docs[2].spec.ingress.push({});
  assert.equal(createNetworkModel(docs).connection('a', 'b', 123).allowed, true);
});

test('same Pod communication remains allowed under default deny', () => {
  const model = createNetworkModel(databaseFixture());
  assert.equal(model.connection('seed', 'seed', 9999).allowed, true);
});

test('namespace and pod selectors in one peer are ANDed and namespace isolation is preserved', () => {
  const docs = [
    pod('client', 'tenant-a'),
    pod('database', 'tenant-b'),
    policy(
      'database-in',
      'database',
      'Ingress',
      [
        {
          from: [
            {
              ...peer('client'),
              namespaceSelector: { matchLabels: { 'kubernetes.io/metadata.name': 'tenant-a' } },
            },
          ],
          ports: [{ port: 5432 }],
        },
      ],
      'tenant-b',
    ),
  ];
  assert.equal(createNetworkModel(docs).connection('client', 'database', 5432).allowed, true);
  docs[0].metadata.namespace = 'tenant-c';
  assert.equal(createNetworkModel(docs).connection('client', 'database', 5432).allowed, false);
  delete docs[2].spec.ingress[0].from[0].namespaceSelector;
  assert.equal(createNetworkModel(docs).connection('client', 'database', 5432).allowed, false);
  docs[0].metadata.namespace = 'tenant-b';
  assert.equal(createNetworkModel(docs).connection('client', 'database', 5432).allowed, true);
});

test('selector expressions support label presence and absence and reject unknown operators', () => {
  assert.equal(
    matchesSelector(
      {
        matchExpressions: [
          { key: 'app', operator: 'In', values: ['a'] },
          { key: 'other', operator: 'NotIn', values: ['no'] },
          { key: 'app', operator: 'Exists' },
          { key: 'other', operator: 'DoesNotExist' },
        ],
      },
      { app: 'a' },
    ),
    true,
  );
  assert.equal(
    matchesSelector(
      { matchExpressions: [{ key: 'app', operator: 'NotIn', values: ['a'] }] },
      { app: 'a' },
    ),
    false,
  );
  assert.throws(
    () => matchesSelector({ matchExpressions: [{ key: 'app', operator: 'Unknown' }] }, {}),
    /Unsupported/,
  );
});

test('ipBlock egress observes CIDR exceptions, protocol and named destination ports', () => {
  const docs = [
    pod('clamav'),
    policy('https', 'clamav', 'Egress', [
      {
        to: [{ ipBlock: { cidr: '0.0.0.0/0', except: ['10.0.0.0/8'] } }],
        ports: [{ port: 443 }],
      },
    ]),
  ];
  const model = createNetworkModel(docs);
  assert.equal(
    model.connection('clamav', { external: true, ip: '203.0.113.7' }, 443).allowed,
    true,
  );
  assert.equal(model.connection('clamav', { external: true, ip: '10.0.0.7' }, 443).allowed, false);
  assert.equal(
    model.connection('clamav', { external: true, ip: '203.0.113.7' }, 80).allowed,
    false,
  );
  const database = databaseFixture();
  database.find((document) => document.metadata.name === 'database-ingress').spec.ingress[0].ports =
    [{ port: 'sql' }];
  assert.equal(createNetworkModel(database).connection('seed', 'database', 5432).allowed, true);
});

// Use the real policies and workload labels to catch a missing rule without invoking
// kubectl. CI separately validates the fully patched render of each overlay.
function repositoryNetworkFixture(namespace = 'default') {
  const root = new URL('../', import.meta.url);
  const kustomization = yaml.load(
    fs.readFileSync(new URL('kustomize/base/kustomization.yaml', root), 'utf8'),
  );
  const files = [
    ...kustomization.resources
      .filter((resource) => resource.endsWith('.yaml'))
      .map((resource) => `kustomize/base/${resource}`),
    'kustomize/components/network-policies/network-policy-deny-all.yaml',
    'kustomize/components/network-policies/network-policy-dancehub.yaml',
  ];
  return files
    .flatMap((file) => yaml.loadAll(fs.readFileSync(new URL(file, root), 'utf8')))
    .filter(Boolean)
    .map((document) => ({ ...document, metadata: { ...document.metadata, namespace } }));
}

test('real development policies support critical dependencies and preserve isolation in both namespaces', () => {
  for (const namespace of ['default', 'dancehub-dev']) {
    assert.deepEqual(validateDevelopmentConnectivity(repositoryNetworkFixture(namespace)), []);
  }
});

test('a trusted seed label from another namespace cannot access PostgreSQL', () => {
  for (const namespace of ['default', 'dancehub-dev']) {
    const documents = repositoryNetworkFixture(namespace);
    const seedIngress = documents.find(
      (document) => document.metadata.name === 'dancehub-seed-postgres-ingress',
    );
    seedIngress.spec.ingress[0].from[0].namespaceSelector = {};
    assert.ok(
      validateDevelopmentConnectivity(documents).some((failure) =>
        /foreign-dancehub-seed -> postgres TCP\/5432: expected denied/.test(failure),
      ),
    );
  }
});

test('named Service target ports require a matching container protocol, defaulting to TCP', () => {
  const documents = repositoryNetworkFixture();
  const service = documents.find(
    (document) => document.kind === 'Service' && document.metadata.name === 'postgres',
  );
  const workload = documents.find(
    (document) => document.kind === 'StatefulSet' && document.metadata.name === 'postgres',
  );
  service.spec.ports[0] = { port: 5432, targetPort: 'postgres-db', protocol: 'TCP' };
  const containerPort = { name: 'postgres-db', containerPort: 5432 };
  workload.spec.template.spec.containers[0].ports = [containerPort];
  assert.deepEqual(validateDevelopmentConnectivity(documents), []);
  containerPort.protocol = 'UDP';
  assert.ok(
    validateDevelopmentConnectivity(documents).some((failure) =>
      /postgres TCP\/5432: Service must route/.test(failure),
    ),
  );
  containerPort.protocol = 'TCP';
  assert.deepEqual(validateDevelopmentConnectivity(documents), []);
});

test('Kafka controller Service traffic requires explicit ingress and egress despite same-Pod allowance', () => {
  for (const direction of ['ingress', 'egress']) {
    const docs = repositoryNetworkFixture();
    const controllerPolicy = docs.find(
      (document) => document.metadata.name === 'dancehub-kafka-controller',
    );
    controllerPolicy.spec[direction] = [];
    // This check alone would conceal a missing Service hairpin allowance.
    assert.equal(createNetworkModel(docs).connection('kafka', 'kafka', 29093).allowed, true);
    assert.ok(
      validateDevelopmentConnectivity(docs).some((failure) =>
        /kafka-controller-client -> kafka TCP\/29093: expected allowed/.test(failure),
      ),
      direction,
    );
  }
  const docs = repositoryNetworkFixture();
  docs.find(
    (document) => document.metadata.name === 'dancehub-kafka-controller',
  ).spec.ingress[0].from[0].namespaceSelector = {};
  assert.ok(
    validateDevelopmentConnectivity(docs).some((failure) =>
      /foreign-kafka -> kafka TCP\/29093: expected denied/.test(failure),
    ),
  );
});

test('regressions to either bootstrap edge, Jaeger export or FreshClam HTTPS are reported', () => {
  for (const [name, expected] of [
    ['dancehub-bootstrap-postgres-egress', /dancehub-migrations -> postgres/],
    ['dancehub-seed-postgres-ingress', /dancehub-seed -> postgres/],
    ['dancehub-collector-jaeger-egress', /otel-collector -> jaeger/],
    ['dancehub-jaeger-ingress', /otel-collector -> jaeger/],
    ['dancehub-clamav-updates-egress', /clamav -> internet TCP\/443/],
  ]) {
    const docs = repositoryNetworkFixture().filter((document) => document.metadata.name !== name);
    assert.ok(
      validateDevelopmentConnectivity(docs).some((failure) => expected.test(failure)),
      name,
    );
  }
});

test('FreshClam HTTPS excludes private, loopback and link-local addresses', () => {
  const documents = repositoryNetworkFixture();
  const policy = documents.find(
    (document) => document.metadata.name === 'dancehub-clamav-updates-egress',
  );
  const original = [...policy.spec.egress[0].to[0].ipBlock.except];
  for (const cidr of original) {
    policy.spec.egress[0].to[0].ipBlock.except = original.filter((candidate) => candidate !== cidr);
    assert.ok(
      validateDevelopmentConnectivity(documents).some((failure) =>
        /clamav -> private-.*expected denied/.test(failure),
      ),
      cidr,
    );
  }
});

test('wrong ports, wrong namespaces, missing services and broad permissions fail the matrix', () => {
  let docs = repositoryNetworkFixture();
  docs.find(
    (document) => document.metadata.name === 'dancehub-bootstrap-postgres-egress',
  ).spec.egress[0].ports[0].port = 5433;
  assert.ok(
    validateDevelopmentConnectivity(docs).some((failure) =>
      /dancehub-seed -> postgres/.test(failure),
    ),
  );
  docs = repositoryNetworkFixture();
  docs.find(
    (document) => document.metadata.name === 'dancehub-seed-postgres-ingress',
  ).metadata.namespace = 'other';
  assert.ok(
    validateDevelopmentConnectivity(docs).some((failure) =>
      /dancehub-seed -> postgres/.test(failure),
    ),
  );
  docs = repositoryNetworkFixture().filter(
    (document) => document.kind !== 'Service' || document.metadata.name !== 'postgres',
  );
  assert.ok(
    validateDevelopmentConnectivity(docs).some((failure) => /Service must route/.test(failure)),
  );
  docs = repositoryNetworkFixture();
  docs.find(
    (document) => document.kind === 'Service' && document.metadata.name === 'postgres',
  ).spec.selector = {};
  assert.ok(
    validateDevelopmentConnectivity(docs).some((failure) => /Service must route/.test(failure)),
  );
  docs = repositoryNetworkFixture();
  docs.push(
    policy('allow-all-in', null, 'Ingress', [{}]),
    policy('allow-all-out', null, 'Egress', [{}]),
  );
  assert.ok(
    validateDevelopmentConnectivity(docs).some((failure) =>
      /unknown-pod -> postgres.*expected denied/.test(failure),
    ),
  );
  for (const [target, port] of [
    ['minio', 9000],
    ['kafka', 29092],
    ['clamav', 3310],
    ['otel-collector', 4317],
    ['otel-collector', 4318],
    ['jaeger', 4317],
  ]) {
    assert.ok(
      validateDevelopmentConnectivity(docs).some((failure) =>
        failure.includes(`unknown-pod -> ${target} TCP/${port}: expected denied`),
      ),
    );
  }
});

function configurationFixture() {
  const grpc = [
    'creditservice',
    'cartservice',
    'recommendationservice',
    'outbox-relay',
    'payment-order-saga',
  ];
  const http = [
    'gatewayservice',
    'accountservice',
    'catalogservice',
    'schedulingservice',
    'orderservice',
    'payrollservice',
    'paymentservice',
    'chatservice',
    'chat-media-worker',
  ];
  const docs = [...grpc, ...http].map((name) => {
    const document = pod(name);
    document.spec.template.spec.containers[0].env = [
      {
        name: 'OTEL_EXPORTER_OTLP_ENDPOINT',
        value: `http://otel-collector:${grpc.includes(name) ? 4317 : 4318}`,
      },
    ];
    return document;
  });
  docs[0].spec.template.spec.initContainers = [
    {
      name: 'wait-for-migrations',
      env: [
        {
          name: 'DATABASE_ADMIN_URL',
          valueFrom: {
            secretKeyRef: { name: 'dancehub-secrets', key: 'DATABASE_ADMIN_URL' },
          },
        },
      ],
    },
  ];
  docs[0].spec.template.spec.containers[0].env.push(
    {
      name: 'DATABASE_URL',
      valueFrom: { secretKeyRef: { name: 'dancehub-secrets', key: 'credit-database-url' } },
    },
    {
      name: 'KAFKA_BOOTSTRAP_SERVERS',
      valueFrom: { configMapKeyRef: { name: 'dancehub-config', key: 'kafka-bootstrap-servers' } },
    },
  );
  docs[0].spec.template.spec.containers[0].envFrom = [
    { secretRef: { name: 'dancehub-secrets' } },
    { configMapRef: { name: 'dancehub-config' } },
  ];
  docs
    .find((document) => document.metadata.name === 'chatservice')
    .spec.template.spec.containers[0].env.push(
      { name: 'OBJECT_STORAGE_ENDPOINT', value: 'minio:9000' },
      { name: 'OBJECT_STORAGE_PUBLIC_ENDPOINT', value: 'localhost:9000' },
      { name: 'OBJECT_STORAGE_SECURE', value: 'false' },
    );
  const redis = pod('redis-flashsale');
  redis.kind = 'StatefulSet';
  redis.spec.replicas = 1;
  redis.spec.serviceName = 'redis-flashsale-headless';
  redis.spec.volumeClaimTemplates = [
    {
      metadata: { name: 'data' },
      spec: { accessModes: ['ReadWriteOnce'], resources: { requests: { storage: '2Gi' } } },
    },
  ];
  Object.assign(redis.spec.template.spec.containers[0], {
    volumeMounts: [{ name: 'data', mountPath: '/data' }],
    args: [
      'redis-server',
      '--dir',
      '/data',
      '--appendonly',
      'yes',
      '--appendfsync',
      'everysec',
      '--maxmemory-policy',
      'noeviction',
    ],
  });
  const keycloak = pod('keycloak');
  keycloak.spec.template.spec.initContainers = [
    {
      name: 'prepare-quarkus',
      image: 'quay.io/keycloak/keycloak:26.3',
      command: ['/bin/bash', '-ec'],
      args: ['cp -R /opt/keycloak/lib/quarkus/. /quarkus/'],
      volumeMounts: [{ name: 'quarkus', mountPath: '/quarkus' }],
    },
  ];
  Object.assign(keycloak.spec.template.spec.containers[0], {
    image: 'quay.io/keycloak/keycloak:26.3',
    volumeMounts: [{ name: 'quarkus', mountPath: '/opt/keycloak/lib/quarkus' }],
    startupProbe: { tcpSocket: { port: 8080 }, periodSeconds: 5, failureThreshold: 60 },
  });
  keycloak.spec.template.spec.volumes = [{ name: 'quarkus', emptyDir: {} }];
  docs.push(
    redis,
    keycloak,
    {
      kind: 'Secret',
      metadata: { name: 'dancehub-secrets' },
      data: { 'credit-database-url': '' },
      stringData: { DATABASE_ADMIN_URL: 'fixture-secret-value-never-log' },
    },
    {
      kind: 'ConfigMap',
      metadata: { name: 'dancehub-config' },
      data: { 'kafka-bootstrap-servers': '' },
    },
    {
      kind: 'Service',
      metadata: { name: 'redis-flashsale-headless' },
      spec: { clusterIP: 'None', selector: { app: 'redis-flashsale' } },
    },
    {
      kind: 'ConfigMap',
      metadata: { name: 'otel-collector-config' },
      data: {
        'config.yaml': yaml.dump({
          receivers: {
            otlp: {
              protocols: { grpc: { endpoint: '0.0.0.0:4317' }, http: { endpoint: '0.0.0.0:4318' } },
            },
          },
          exporters: {
            'otlp/jaeger': { endpoint: 'jaeger:4317' },
            prometheus: { endpoint: '0.0.0.0:8889' },
          },
          service: {
            pipelines: {
              traces: { receivers: ['otlp'], exporters: ['otlp/jaeger'] },
              metrics: { receivers: ['otlp'], exporters: ['prometheus'] },
            },
          },
        }),
      },
    },
  );
  return {
    docs,
    skaffold: {
      portForward: [
        { resourceType: 'service', resourceName: 'minio', port: 9000, localPort: 9000 },
      ],
    },
  };
}

test('configuration checks accept persistent Redis and matching protocol/public endpoints', () => {
  const { docs, skaffold } = configurationFixture();
  assert.deepEqual(validateDevelopmentConfiguration(docs, skaffold), []);
});

test('a wrong init container Secret key fails without exposing Secret values', () => {
  const { docs, skaffold } = configurationFixture();
  docs[0].spec.template.spec.initContainers[0].env[0].valueFrom.secretKeyRef.key =
    'database-admin-url';
  const failures = validateDevelopmentConfiguration(docs, skaffold);
  assert.deepEqual(failures, [
    'creditservice: initContainers[wait-for-migrations] env DATABASE_ADMIN_URL: Secret/default/dancehub-secrets is missing key database-admin-url',
  ]);
  assert.ok(failures.every((failure) => !failure.includes('fixture-secret-value-never-log')));
});

test('Secret and ConfigMap env references require the resource and key in the Pod namespace', () => {
  for (const type of ['containers', 'initContainers']) {
    for (const [kind, selector, name, key] of [
      ['Secret', 'secretKeyRef', 'dancehub-secrets', 'DATABASE_ADMIN_URL'],
      ['ConfigMap', 'configMapKeyRef', 'dancehub-config', 'kafka-bootstrap-servers'],
    ]) {
      for (const mutation of ['key', 'name', 'namespace']) {
        const { docs, skaffold } = configurationFixture();
        const reference = { name, key, optional: false };
        docs[0].spec.template.spec[type][0].env.push({
          name: 'REFERENCE_TEST',
          valueFrom: { [selector]: reference },
        });
        if (mutation === 'namespace') {
          docs.find(
            (document) => document.kind === kind && document.metadata.name === name,
          ).metadata.namespace = 'other';
        } else reference[mutation] = 'missing';
        const failures = validateDevelopmentConfiguration(docs, skaffold);
        assert.ok(
          failures.some(
            (failure) => failure.includes(`${type}[`) && failure.includes('env REFERENCE_TEST:'),
          ),
          `${type} ${kind} ${mutation}`,
        );
      }
    }
  }
});

test('optional env references tolerate missing objects and keys in regular and init containers', () => {
  const { docs, skaffold } = configurationFixture();
  for (const type of ['containers', 'initContainers']) {
    for (const [selector, name] of [
      ['secretKeyRef', 'dancehub-secrets'],
      ['configMapKeyRef', 'dancehub-config'],
    ]) {
      for (const resourceName of [name, 'missing']) {
        docs[0].spec.template.spec[type][0].env.push({
          name: 'OPTIONAL_REFERENCE',
          valueFrom: { [selector]: { name: resourceName, key: 'missing', optional: true } },
        });
      }
    }
  }
  assert.deepEqual(validateDevelopmentConfiguration(docs, skaffold), []);
});

test('envFrom requires same-namespace resources unless optional in regular and init containers', () => {
  for (const type of ['containers', 'initContainers']) {
    for (const [kind, selector, name] of [
      ['Secret', 'secretRef', 'dancehub-secrets'],
      ['ConfigMap', 'configMapRef', 'dancehub-config'],
    ]) {
      for (const mutation of ['name', 'namespace']) {
        const { docs, skaffold } = configurationFixture();
        const reference = { name };
        const container = docs[0].spec.template.spec[type][0];
        container.envFrom = [{ [selector]: reference }];
        if (mutation === 'namespace') {
          // Retain existing env references and move just this envFrom target.
          docs.push({ kind, metadata: { name: 'foreign-resource', namespace: 'other' } });
          reference.name = 'foreign-resource';
        } else reference.name = 'missing';
        assert.ok(
          validateDevelopmentConfiguration(docs, skaffold).some(
            (failure) => failure.includes(`${type}[`) && failure.includes('envFrom[0]:'),
          ),
          `${type} ${kind} ${mutation}`,
        );
        reference.optional = true;
        assert.deepEqual(validateDevelopmentConfiguration(docs, skaffold), []);
      }
    }
  }
});

test('ConfigMap binaryData cannot satisfy an environment key reference', () => {
  const { docs, skaffold } = configurationFixture();
  const config = docs.find((document) => document.metadata.name === 'dancehub-config');
  config.binaryData = { 'kafka-bootstrap-servers': '' };
  delete config.data;
  assert.ok(
    validateDevelopmentConfiguration(docs, skaffold).some((failure) =>
      failure.includes('ConfigMap/default/dancehub-config is missing key kafka-bootstrap-servers'),
    ),
  );
});

test('Keycloak must prepare matching Quarkus artifacts in its writable volume and allow build time', () => {
  for (const change of [
    (spec) => {
      spec.initContainers = [];
    },
    (spec) => {
      spec.initContainers[0].image = 'different-image';
    },
    (spec) => {
      spec.initContainers[0].args = ['true'];
    },
    (spec) => {
      spec.initContainers[0].volumeMounts[0].name = 'different-volume';
    },
    (spec) => {
      spec.initContainers[0].volumeMounts[0].readOnly = true;
    },
    (spec) => {
      spec.volumes = [];
    },
    (spec) => {
      spec.volumes[0] = { name: 'quarkus', configMap: { name: 'wrong-source' } };
    },
    (spec) => {
      spec.containers[0].volumeMounts = [];
    },
    (spec) => {
      spec.containers[0].volumeMounts[0].readOnly = true;
    },
    (spec) => {
      spec.containers[0].volumeMounts[0].subPath = 'wrong-directory';
    },
    (spec) => {
      delete spec.containers[0].startupProbe;
    },
    (spec) => {
      spec.containers[0].startupProbe.tcpSocket.port = 9000;
    },
    (spec) => {
      spec.containers[0].startupProbe.failureThreshold = 3;
    },
  ]) {
    const { docs, skaffold } = configurationFixture();
    change(docs.find((document) => document.metadata.name === 'keycloak').spec.template.spec);
    assert.ok(
      validateDevelopmentConfiguration(docs, skaffold).some((failure) =>
        failure.startsWith('keycloak:'),
      ),
    );
  }
});

test('Redis ephemeral storage, extra replicas, deletion retention and AOF changes fail', () => {
  for (const change of [
    (redis) => {
      redis.kind = 'Deployment';
    },
    (redis) => {
      redis.spec.replicas = 2;
    },
    (redis) => {
      redis.spec.volumeClaimTemplates = [];
    },
    (redis) => {
      redis.spec.volumeClaimTemplates[0].metadata.name = 'renamed';
      redis.spec.template.spec.containers[0].volumeMounts[0].name = 'renamed';
    },
    (redis) => {
      redis.spec.volumeClaimTemplates[0].spec.resources.requests.storage = '1Gi';
    },
    (redis) => {
      redis.spec.volumeClaimTemplates[0].spec.storageClassName = '';
    },
    (redis) => {
      redis.spec.volumeClaimTemplates[0].spec.storageClassName = 'custom';
    },
    (redis) => {
      redis.spec.template.spec.volumes = [{ name: 'data', emptyDir: {} }];
    },
    (redis) => {
      redis.spec.template.spec.volumes = [
        { name: 'data', persistentVolumeClaim: { claimName: 'another-claim' } },
      ];
    },
    (redis) => {
      redis.spec.template.spec.containers[0].volumeMounts[0].readOnly = true;
    },
    (redis) => {
      redis.spec.persistentVolumeClaimRetentionPolicy = { whenDeleted: 'Delete' };
    },
    (redis) => {
      redis.spec.template.spec.containers[0].args = [];
    },
  ]) {
    const { docs, skaffold } = configurationFixture();
    change(docs.find((document) => document.metadata.name === 'redis-flashsale'));
    assert.ok(
      validateDevelopmentConfiguration(docs, skaffold).some((failure) =>
        /redis-flashsale/.test(failure),
      ),
    );
  }
});

test('Redis required flags reject duplicates, including later overrides of persistence settings', () => {
  for (const [flag, value, override] of [
    ['--dir', '/data', '/tmp'],
    ['--appendonly', 'yes', 'no'],
    ['--appendfsync', 'everysec', 'no'],
    ['--maxmemory-policy', 'noeviction', 'allkeys-lru'],
  ]) {
    for (const duplicateValue of [value, override]) {
      const { docs, skaffold } = configurationFixture();
      const redis = docs.find((document) => document.metadata.name === 'redis-flashsale');
      redis.spec.template.spec.containers[0].args.push(flag, duplicateValue);
      assert.ok(
        validateDevelopmentConfiguration(docs, skaffold).some((failure) =>
          failure.includes(`redis-flashsale: ${flag} must occur once`),
        ),
        `${flag} ${duplicateValue}`,
      );
    }
  }
});

test('Collector receiver and exporter definitions must be connected by signal pipelines', () => {
  for (const signal of ['traces', 'metrics']) {
    for (const change of [
      (config) => {
        delete config.service.pipelines[signal];
      },
      (config) => {
        config.service.pipelines[signal].receivers = [];
      },
      (config) => {
        config.service.pipelines[signal].receivers = ['different-receiver'];
      },
      (config) => {
        config.service.pipelines[signal].receivers = 'otlp';
      },
      (config) => {
        config.service.pipelines[signal].exporters = [];
      },
      (config) => {
        config.service.pipelines[signal].exporters = ['different-exporter'];
      },
    ]) {
      const { docs, skaffold } = configurationFixture();
      const collector = docs.find((document) => document.metadata.name === 'otel-collector-config');
      const config = yaml.load(collector.data['config.yaml']);
      change(config);
      collector.data['config.yaml'] = yaml.dump(config);
      assert.ok(
        validateDevelopmentConfiguration(docs, skaffold).some((failure) =>
          failure.includes(`otel-collector: ${signal} pipeline must connect`),
        ),
      );
    }
  }
  for (const exporter of [undefined, { endpoint: '0.0.0.0:9464' }]) {
    const { docs, skaffold } = configurationFixture();
    const collector = docs.find((document) => document.metadata.name === 'otel-collector-config');
    const config = yaml.load(collector.data['config.yaml']);
    config.exporters.prometheus = exporter;
    collector.data['config.yaml'] = yaml.dump(config);
    assert.ok(
      validateDevelopmentConfiguration(docs, skaffold).some((failure) =>
        failure.includes('otel-collector: Prometheus export must listen on 0.0.0.0:8889'),
      ),
    );
  }
});

test('wrong OTLP port, duplicate environment values and inaccessible public storage fail', () => {
  for (const change of [
    (docs) => {
      docs[0].spec.template.spec.containers[0].env[0].value = 'http://otel-collector:4318';
    },
    (docs) => {
      const env = docs[0].spec.template.spec.containers[0].env;
      env.push({ ...env[0] });
    },
    (docs) => {
      docs
        .find((document) => document.metadata.name === 'chatservice')
        .spec.template.spec.containers[0].env.find(
          (entry) => entry.name === 'OBJECT_STORAGE_PUBLIC_ENDPOINT',
        ).value = 'minio:9000';
    },
  ]) {
    const { docs, skaffold } = configurationFixture();
    change(docs);
    assert.ok(validateDevelopmentConfiguration(docs, skaffold).length > 0);
  }
  const { docs, skaffold } = configurationFixture();
  skaffold.portForward = [];
  assert.ok(
    validateDevelopmentConfiguration(docs, skaffold).some((failure) => /skaffold/.test(failure)),
  );
});
