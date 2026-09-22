import json
import logging
import os
import time
from datetime import datetime, timezone

import psycopg
from kafka import KafkaProducer
from opentelemetry import propagate, trace
from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import OTLPSpanExporter
from opentelemetry.instrumentation.psycopg import PsycopgInstrumentor
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor
from health_server import mark_not_ready, mark_ready, start_health_server

logging.basicConfig(level=logging.INFO, format='%(message)s')
logger = logging.getLogger('outbox-relay')

DATABASE_URL = os.environ['DATABASE_URL']
KAFKA_BOOTSTRAP = os.environ['KAFKA_BOOTSTRAP_SERVERS']
OTEL_EXPORTER_OTLP_ENDPOINT = os.environ['OTEL_EXPORTER_OTLP_ENDPOINT']
OTEL_SERVICE_NAMESPACE = os.environ['OTEL_SERVICE_NAMESPACE']
ENVIRONMENT = os.environ['ENVIRONMENT']
SCHEMAS = ('scheduling', 'payment')
POLL_INTERVAL_SECONDS = 0.5
TOPICS = {
    'scheduling': 'scheduling.events.v1',
    'payment': 'payment.events.v1',
}

provider = TracerProvider(resource=Resource.create({'service.name': 'outbox-relay', 'service.namespace': OTEL_SERVICE_NAMESPACE, 'deployment.environment.name': ENVIRONMENT}))
provider.add_span_processor(BatchSpanProcessor(OTLPSpanExporter(endpoint=OTEL_EXPORTER_OTLP_ENDPOINT, insecure=True)))
trace.set_tracer_provider(provider)
PsycopgInstrumentor().instrument()
tracer = trace.get_tracer('outbox-relay')


def relay_batch(db, producer, schema):
    with db.transaction():
        db.execute("SELECT set_config('app.user_id','',true),set_config('app.studio_id','',true),set_config('app.tenant_roles','',true),set_config('app.global_roles','',true),set_config('app.actor_kind','service',true),set_config('app.service_principal','outbox-relay',true),set_config('app.service','outbox-relay',true),set_config('app.request_id',%s,true)", (f'relay-{schema}-{time.time_ns()}',))
        rows = db.execute(f"SELECT event_id,event_type,aggregate_type,aggregate_id,tenant_id,payload,occurred_at,traceparent,tracestate,baggage FROM {schema}.outbox_events WHERE published_at IS NULL ORDER BY occurred_at LIMIT 100 FOR UPDATE SKIP LOCKED").fetchall()
        if not rows:
            return 0
        for event_id, event_type, aggregate_type, aggregate_id, tenant_id, payload, occurred_at, traceparent, tracestate, baggage in rows:
            envelope = {'event_id': str(event_id), 'event_type': event_type, 'schema_version': 1, 'occurred_at': occurred_at.astimezone(timezone.utc).isoformat(), 'producer': schema, 'tenant_id': str(tenant_id) if tenant_id else '', 'aggregate_type': aggregate_type, 'aggregate_id': str(aggregate_id), 'traceparent': traceparent or '', 'data': payload}
            saved_context = {name: value for name, value in [('traceparent', traceparent), ('tracestate', tracestate), ('baggage', baggage)] if value}
            parent = propagate.extract(saved_context)
            with tracer.start_as_current_span('outbox.publish', context=parent, attributes={'messaging.destination.name': TOPICS[schema], 'messaging.operation.name': 'publish', 'event.type': event_type}):
                outgoing = {}
                propagate.inject(outgoing)
                headers = [(name, value.encode()) for name, value in outgoing.items()]
                producer.send(TOPICS[schema], key=str(aggregate_id).encode(), value=envelope, headers=headers).get(timeout=15)
        ids = [row[0] for row in rows]
        db.execute(f"UPDATE {schema}.outbox_events SET published_at=now() WHERE event_id=ANY(%s)", (ids,))
        return len(rows)


def main():
    # kafka-python-ng does not expose the Java client's enable.idempotence
    # setting. The outbox therefore uses at-least-once delivery: wait for all
    # replicas, retry transient send failures in order, and rely on event_id
    # inbox/message uniqueness at consumers to discard crash-window duplicates.
    producer = KafkaProducer(
        bootstrap_servers=KAFKA_BOOTSTRAP,
        acks='all',
        retries=5,
        max_in_flight_requests_per_connection=1,
        value_serializer=lambda value: json.dumps(value, separators=(',', ':')).encode(),
    )
    # The connection is kept for the lifetime of the process and only re-opened after a
    # failure; each poll would otherwise pay a fresh TCP + TLS + auth handshake twice a
    # second per replica. autocommit=True keeps the idle connection out of a transaction;
    # relay_batch() opens an explicit one around each batch.
    db = None
    while True:
        try:
            if db is None:
                db = psycopg.connect(DATABASE_URL, autocommit=True)
                db.execute('SELECT 1')
                mark_ready()
            count = 0
            for schema in SCHEMAS:
                count += relay_batch(db, producer, schema)
            if count:
                logger.info(json.dumps({'message': 'outbox batch published', 'count': count, 'at': datetime.now(timezone.utc).isoformat()}))
            else:
                time.sleep(POLL_INTERVAL_SECONDS)
        except Exception:
            mark_not_ready()
            logger.exception('outbox relay failed')
            if db is not None:
                try:
                    db.close()
                except Exception:
                    pass
                db = None
            time.sleep(2)


if __name__ == '__main__':
    health_server = start_health_server('outbox-relay')
    try:
        main()
    finally:
        health_server.shutdown()
        provider.shutdown()
