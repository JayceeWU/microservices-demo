import json
import logging
import os
import signal
import time

import grpc
from kafka import KafkaConsumer
from opentelemetry import propagate, trace
from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import OTLPSpanExporter
from opentelemetry.instrumentation.grpc import GrpcInstrumentorClient
from opentelemetry.instrumentation.kafka import KafkaInstrumentor
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor
from health_server import mark_not_ready, mark_ready, start_health_server

from common.v1 import common_pb2
from orders.v1 import orders_pb2, orders_pb2_grpc

logging.basicConfig(level=logging.INFO, format="%(message)s")
logger = logging.getLogger("payment-order-saga")
OTEL_EXPORTER_OTLP_ENDPOINT = os.environ["OTEL_EXPORTER_OTLP_ENDPOINT"]
OTEL_SERVICE_NAMESPACE = os.environ["OTEL_SERVICE_NAMESPACE"]
ENVIRONMENT = os.environ["ENVIRONMENT"]

# Order Service rejects these deterministically (unknown order, state machine says no,
# malformed event); replaying them would only block the partition. Everything else
# (UNAVAILABLE, DEADLINE_EXCEEDED, INTERNAL, ...) is treated as transient.
PERMANENT_FAILURES = {
    grpc.StatusCode.NOT_FOUND,
    grpc.StatusCode.INVALID_ARGUMENT,
    grpc.StatusCode.FAILED_PRECONDITION,
    grpc.StatusCode.PERMISSION_DENIED,
    grpc.StatusCode.UNAUTHENTICATED,
    grpc.StatusCode.ALREADY_EXISTS,
    grpc.StatusCode.OUT_OF_RANGE,
    grpc.StatusCode.UNIMPLEMENTED,
}
# Must stay well below the consumer's max_poll_interval_ms (300 s), otherwise the group
# coordinator evicts this member while it is still retrying. When the budget runs out the
# error propagates, the process exits and resumes from the last committed offset.
RETRY_BUDGET_SECONDS = 60
MAX_BACKOFF_SECONDS = 15


def configure_telemetry():
    provider = TracerProvider(resource=Resource.create({"service.name": "payment-order-saga", "service.namespace": OTEL_SERVICE_NAMESPACE, "deployment.environment.name": ENVIRONMENT}))
    provider.add_span_processor(BatchSpanProcessor(OTLPSpanExporter(endpoint=OTEL_EXPORTER_OTLP_ENDPOINT, insecure=True)))
    trace.set_tracer_provider(provider)
    GrpcInstrumentorClient().instrument()
    KafkaInstrumentor().instrument()
    return provider


def header_carrier(headers):
    return {key: value.decode("utf-8") for key, value in headers or [] if value and key in {"traceparent", "tracestate", "baggage"}}


def apply_with_retry(apply, event_id, order_id, is_running):
    """Run `apply` until it succeeds, is rejected permanently, or the retry budget ends.

    Returns True when the order service accepted the call and False when the event was
    discarded; both mean the offset may be committed.
    """
    deadline = time.monotonic() + RETRY_BUDGET_SECONDS
    delay = 1.0
    while True:
        try:
            apply()
            mark_ready()
            return True
        except grpc.RpcError as error:
            code = error.code()
            if code in PERMANENT_FAILURES:
                logger.error(json.dumps({
                    "message": "discarding payment event rejected by order service",
                    "event_id": event_id,
                    "order_id": order_id,
                    "code": code.name,
                    "details": error.details(),
                }))
                return False
            if time.monotonic() + delay > deadline or not is_running():
                raise
            mark_not_ready()
            logger.warning(json.dumps({
                "message": "order service call failed; retrying",
                "event_id": event_id,
                "order_id": order_id,
                "code": code.name,
                "retry_in_seconds": delay,
            }))
            time.sleep(delay)
            delay = min(delay * 2, MAX_BACKOFF_SECONDS)


def metadata(envelope):
    return (
        ("x-request-id", envelope["event_id"]),
        ("idempotency-key", envelope["event_id"]),
        ("x-actor-kind", "service"),
        ("x-service-principal", "payment-order-saga"),
    )


def success_request(envelope, audit):
    request = orders_pb2.MarkPaymentSucceededRequest(
        order_id=envelope['data']['order_id'],
        payment_id=envelope.get('aggregate_id', ''), audit=audit,
    )
    # Never substitute delivery time: a timely payment may wait in Kafka while
    # its room hold expires. Missing timestamps must not advance the offset.
    request.paid_at.FromJsonString(envelope['data']['paid_at'])
    return request


def main():
    provider = configure_telemetry()
    consumer = KafkaConsumer(
        "payment.events.v1",
        bootstrap_servers=os.environ["KAFKA_BOOTSTRAP_SERVERS"],
        group_id="orders-payment-saga-v1",
        enable_auto_commit=False,
        auto_offset_reset="earliest",
        value_deserializer=lambda value: json.loads(value.decode("utf-8")),
    )
    channel = grpc.insecure_channel(os.environ["ORDER_SERVICE_ADDR"])
    grpc.channel_ready_future(channel).result(timeout=10)
    orders = orders_pb2_grpc.OrderServiceStub(channel)
    mark_ready()
    running = True

    def stop(*_):
        nonlocal running
        running = False

    signal.signal(signal.SIGTERM, stop)
    signal.signal(signal.SIGINT, stop)
    tracer = trace.get_tracer("payment-order-saga")
    try:
        for record in consumer:
            if not running:
                break
            envelope = record.value
            event_type = envelope.get("event_type")
            if event_type not in {"PaymentSucceeded", "PaymentFailed"}:
                consumer.commit()
                continue
            event_id = envelope["event_id"]
            order_id = envelope["data"].get("order_id")
            if not order_id:
                logger.error(json.dumps({"message": "payment event missing order_id", "event_id": event_id}))
                consumer.commit()
                continue
            parent = propagate.extract(header_carrier(record.headers))

            audit = common_pb2.AuditContext(idempotency_key=event_id, reason="payment domain event")
            succeeded = success_request(envelope, audit) if event_type == "PaymentSucceeded" else None

            def apply():
                with tracer.start_as_current_span("payment event -> order saga", context=parent, kind=trace.SpanKind.CONSUMER):
                    if event_type == "PaymentSucceeded":
                        orders.MarkPaymentSucceeded(
                            succeeded,
                            timeout=10,
                            metadata=metadata(envelope),
                        )
                    else:
                        orders.MarkPaymentFailed(
                            orders_pb2.MarkPaymentFailedRequest(order_id=order_id, reason=envelope["data"].get("reason") or "payment_failed", audit=audit),
                            timeout=10,
                            metadata=metadata(envelope),
                        )

            # The calls are idempotent (event_id is the idempotency key), so retrying is safe.
            apply_with_retry(apply, event_id, order_id, lambda: running)
            consumer.commit()
    finally:
        mark_not_ready()
        channel.close()
        consumer.close()
        provider.shutdown()


if __name__ == "__main__":
    health_server = start_health_server("payment-order-saga")
    try:
        main()
    finally:
        health_server.shutdown()
