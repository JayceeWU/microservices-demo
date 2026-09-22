'use strict';

const { NodeSDK } = require('@opentelemetry/sdk-node');
const { OTLPTraceExporter } = require('@opentelemetry/exporter-trace-otlp-http');
const { GrpcInstrumentation } = require('@opentelemetry/instrumentation-grpc');
const { HttpInstrumentation } = require('@opentelemetry/instrumentation-http');
const { PgInstrumentation } = require('@opentelemetry/instrumentation-pg');
const { resourceFromAttributes } = require('@opentelemetry/resources');
const {
  ATTR_SERVICE_NAME,
  ATTR_SERVICE_NAMESPACE,
  ATTR_DEPLOYMENT_ENVIRONMENT_NAME,
} = require('@opentelemetry/semantic-conventions');

const required = (name) => {
  const value = process.env[name];
  if (!value) throw new Error(`${name} is required`);
  return value;
};
const endpoint = required('OTEL_EXPORTER_OTLP_ENDPOINT').replace(/\/$/, '');
const sdk = new NodeSDK({
  resource: resourceFromAttributes({
    [ATTR_SERVICE_NAME]: 'paymentservice',
    [ATTR_SERVICE_NAMESPACE]: required('OTEL_SERVICE_NAMESPACE'),
    [ATTR_DEPLOYMENT_ENVIRONMENT_NAME]: required('ENVIRONMENT'),
  }),
  traceExporter: new OTLPTraceExporter({ url: `${endpoint}/v1/traces` }),
  instrumentations: [new HttpInstrumentation(), new GrpcInstrumentation(), new PgInstrumentation()],
});
sdk.start();
for (const signal of ['SIGTERM', 'SIGINT'])
  process.once(signal, () => sdk.shutdown().finally(() => process.exit(0)));
