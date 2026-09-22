import { WebTracerProvider } from '@opentelemetry/sdk-trace-web';
import { BatchSpanProcessor } from '@opentelemetry/sdk-trace-base';
import { OTLPTraceExporter } from '@opentelemetry/exporter-trace-otlp-http';
import { resourceFromAttributes } from '@opentelemetry/resources';
import {
  ATTR_SERVICE_NAME,
  ATTR_DEPLOYMENT_ENVIRONMENT_NAME,
} from '@opentelemetry/semantic-conventions';
import { registerInstrumentations } from '@opentelemetry/instrumentation';
import { DocumentLoadInstrumentation } from '@opentelemetry/instrumentation-document-load';
import { FetchInstrumentation } from '@opentelemetry/instrumentation-fetch';
import { UserInteractionInstrumentation } from '@opentelemetry/instrumentation-user-interaction';

let started = false;
export function initializeBrowserTelemetry(serviceName) {
  if (started || typeof window === 'undefined') return;
  const gateway = globalThis.DANCEHUB_API_URL;
  const environment = globalThis.DANCEHUB_ENVIRONMENT;
  if (!gateway || !environment) throw new Error('Browser telemetry configuration is incomplete');
  started = true;
  const exporter = new OTLPTraceExporter({ url: `${gateway}/v1/telemetry/v1/traces` });
  const provider = new WebTracerProvider({
    resource: resourceFromAttributes({
      [ATTR_SERVICE_NAME]: serviceName,
      [ATTR_DEPLOYMENT_ENVIRONMENT_NAME]: environment,
    }),
    spanProcessors: [new BatchSpanProcessor(exporter)],
  });
  // Angular owns Zone.js. Replacing its context manager before bootstrap can
  // move Angular dependency-injection work outside the active DI context.
  provider.register();
  registerInstrumentations({
    instrumentations: [
      new DocumentLoadInstrumentation(),
      new FetchInstrumentation({
        propagateTraceHeaderCorsUrls: [
          new RegExp(`^${gateway.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}/`),
          /^\/v1\//,
        ],
        clearTimingResources: true,
      }),
      new UserInteractionInstrumentation({ eventNames: ['click', 'submit'] }),
    ],
  });
  const observer = new PerformanceObserver((list) => {
    const tracer = provider.getTracer(serviceName);
    for (const entry of list.getEntries()) {
      const span = tracer.startSpan(`web_vital.${entry.name}`, { startTime: entry.startTime });
      span.setAttribute('web_vital.value', entry.duration || entry.startTime);
      span.end(entry.startTime + (entry.duration || 0));
    }
  });
  observer.observe({ type: 'largest-contentful-paint', buffered: true });
}
