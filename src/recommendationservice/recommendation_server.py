import json
import logging
import os
import threading
import time
from concurrent import futures
from datetime import date
from http.server import BaseHTTPRequestHandler, HTTPServer

import grpc
import psycopg
from kafka import KafkaConsumer
from grpc_health.v1 import health, health_pb2, health_pb2_grpc
from opentelemetry import trace
from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import OTLPSpanExporter
from opentelemetry.instrumentation.grpc import GrpcInstrumentorServer
from opentelemetry.instrumentation.kafka import KafkaInstrumentor
from opentelemetry.instrumentation.psycopg import PsycopgInstrumentor
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor

from mesh_peer import MeshPolicy
from projection import consume_projection, decode_event, parse_scheduling_event
from recommendations.v1 import recommendations_pb2, recommendations_pb2_grpc

logging.basicConfig(level=logging.INFO, format="%(message)s")
logger = logging.getLogger("recommendationservice")
DATABASE_URL = os.environ["DATABASE_URL"]
KAFKA_BOOTSTRAP_SERVERS = os.environ["KAFKA_BOOTSTRAP_SERVERS"]
OTEL_EXPORTER_OTLP_ENDPOINT = os.environ["OTEL_EXPORTER_OTLP_ENDPOINT"]
OTEL_SERVICE_NAMESPACE = os.environ["OTEL_SERVICE_NAMESPACE"]
ENVIRONMENT = os.environ["ENVIRONMENT"]
PROJECTION_READY = threading.Event()
MESH_POLICY = MeshPolicy.from_environment(os.environ)


def configure_telemetry():
    provider = TracerProvider(resource=Resource.create({"service.name": "recommendationservice", "service.namespace": OTEL_SERVICE_NAMESPACE, "deployment.environment.name": ENVIRONMENT}))
    provider.add_span_processor(BatchSpanProcessor(OTLPSpanExporter(endpoint=OTEL_EXPORTER_OTLP_ENDPOINT, insecure=True)))
    trace.set_tracer_provider(provider)
    GrpcInstrumentorServer().instrument()
    KafkaInstrumentor().instrument()
    PsycopgInstrumentor().instrument()
    return provider


def metadata(context, key):
    return dict(context.invocation_metadata()).get(key, "")


def require_mesh_peer(context):
    """Abort the call unless the mesh-proven peer may assert the supplied identity."""
    denial = MESH_POLICY.authorize(metadata(context, "x-forwarded-client-cert"), metadata(context, "x-actor-kind"), metadata(context, "x-service-principal"))
    if denial is not None:
        context.abort(getattr(grpc.StatusCode, denial[0]), denial[1])


def transaction(context, service="recommendationservice", studio_id=""):
    connection = psycopg.connect(DATABASE_URL)
    connection.execute(
        "SELECT set_config('app.user_id',%s,true),set_config('app.studio_id',%s,true),set_config('app.tenant_roles',%s,true),set_config('app.global_roles',%s,true),set_config('app.actor_kind',%s,true),set_config('app.service_principal',%s,true),set_config('app.service',%s,true),set_config('app.request_id',%s,true)",
        (metadata(context, "x-user-id"), studio_id or metadata(context, "x-studio-id"), metadata(context, "x-tenant-roles"), metadata(context, "x-global-roles"), metadata(context, "x-actor-kind"), metadata(context, "x-service-principal"), service, metadata(context, "x-request-id")),
    )
    return connection


class RecommendationService(recommendations_pb2_grpc.RecommendationServiceServicer):
    def GetStudioAnalytics(self, request, context):
        require_mesh_peer(context)
        month = request.month or date.today().strftime("%Y-%m")
        try:
            with transaction(context) as db:
                rows = db.execute(
                    "SELECT metric_name,metric_value FROM recommendation.studio_metrics WHERE studio_id=%s AND month=%s::date ORDER BY metric_name",
                    (request.studio_id, month + "-01"),
                ).fetchall()
            return recommendations_pb2.GetStudioAnalyticsResponse(metrics=[recommendations_pb2.Metric(name=row[0], value=row[1]) for row in rows])
        except Exception:
            logger.exception("analytics query failed")
            context.abort(grpc.StatusCode.INTERNAL, "unable to load analytics")


def project_event(event, connect=lambda: psycopg.connect(DATABASE_URL)):
    """Apply one scheduling event to the metrics projection in a single transaction."""
    event_id, event_type, studio_id = parse_scheduling_event(event)
    with connect() as db:
        db.execute("SELECT set_config('app.user_id','',true),set_config('app.studio_id',%s,true),set_config('app.tenant_roles','',true),set_config('app.global_roles','',true),set_config('app.actor_kind','service',true),set_config('app.service_principal','recommendation-projection',true),set_config('app.service','recommendation-projection',true),set_config('app.request_id',%s,true)", (studio_id, event_id))
        inserted = db.execute("INSERT INTO recommendation.inbox_events(event_id,consumer) VALUES(%s,'recommendation-projection') ON CONFLICT DO NOTHING RETURNING event_id", (event_id,)).fetchone()
        if inserted and event_type in {"BookingCreated", "WalkInCreditCaptured"}:
            # Attribute the metric to the month the event happened, not the month it
            # was projected, so replays and consumer lag never shift the numbers.
            db.execute("INSERT INTO recommendation.studio_metrics(studio_id,month,metric_name,metric_value) VALUES(%s,date_trunc('month',COALESCE(%s::timestamptz,now()))::date,'bookings',1) ON CONFLICT(studio_id,month,metric_name) DO UPDATE SET metric_value=recommendation.studio_metrics.metric_value+1,updated_at=now()", (studio_id, event.get("occurred_at")))


def projection_worker():
    while True:
        consumer = None
        try:
            consumer = KafkaConsumer("scheduling.events.v1", bootstrap_servers=KAFKA_BOOTSTRAP_SERVERS, group_id="recommendation-projection-v1", enable_auto_commit=False, auto_offset_reset="earliest", value_deserializer=decode_event)
            PROJECTION_READY.set()
            consume_projection(consumer, project_event)
        except Exception:
            logger.exception("recommendation projection disconnected")
        finally:
            PROJECTION_READY.clear()
            if consumer is not None:
                consumer.close()
        time.sleep(2)


class HealthHandler(BaseHTTPRequestHandler):
    def do_GET(self):
        status = 404
        if self.path == "/healthz":
            try:
                if not PROJECTION_READY.is_set():
                    raise RuntimeError("projection worker is not ready")
                with psycopg.connect(DATABASE_URL, connect_timeout=2) as db:
                    db.execute("SELECT 1")
                status = 200
            except Exception:
                status = 503
        self.send_response(status)
        self.end_headers()
    def log_message(self, *_):
        return


def main():
    provider = configure_telemetry()
    logger.info(json.dumps({"message": "mesh peer enforcement", "enabled": MESH_POLICY.enforce, "trust_domain": MESH_POLICY.trust_domain}))
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=10))
    recommendations_pb2_grpc.add_RecommendationServiceServicer_to_server(RecommendationService(), server)
    health_service = health.HealthServicer()
    health_pb2_grpc.add_HealthServicer_to_server(health_service, server)
    health_service.set("", health_pb2.HealthCheckResponse.SERVING)
    server.add_insecure_port("[::]:9090")
    threading.Thread(target=lambda: HTTPServer(("0.0.0.0", 8080), HealthHandler).serve_forever(), daemon=True).start()
    threading.Thread(target=projection_worker, daemon=True).start()
    server.start()
    try:
        server.wait_for_termination()
    finally:
        provider.shutdown()


if __name__ == "__main__":
    main()
