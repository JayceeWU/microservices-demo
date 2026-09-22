"""Kafka consumption loop for the studio metrics projection.

Kept free of client-library imports at module level so the retry semantics can be unit
tested with a fake consumer; `recommendation_server.py` supplies the real KafkaConsumer and
the PostgreSQL projection function.
"""

import json
import logging
import time

logger = logging.getLogger("recommendationservice")

# Delay before re-reading a failed offset; kept well under Kafka's max_poll_interval_ms.
PROJECTION_RETRY_SECONDS = 2.0


class MalformedEvent(ValueError):
    """The event can never be projected; it is logged and skipped rather than retried."""


def decode_event(value):
    """Deserialize a record body; undecodable bodies become None and are skipped as malformed."""
    try:
        return json.loads(value.decode())
    except (UnicodeDecodeError, ValueError):
        return None


def parse_scheduling_event(event):
    try:
        event_id = event["event_id"]
        event_type = event["event_type"]
        studio_id = event["tenant_id"]
        event["data"]["class_session_id"]
    except (KeyError, TypeError) as error:
        raise MalformedEvent("malformed scheduling event") from error
    return event_id, event_type, studio_id


def seek_to(consumer, record):
    """Rewind the record's partition so the failed record is delivered again."""
    from kafka import TopicPartition  # deferred: unit tests run without the Kafka client installed

    consumer.seek(TopicPartition(record.topic, record.partition), record.offset)


def consume_projection(consumer, project, seek=seek_to, sleep=time.sleep, retry_seconds=PROJECTION_RETRY_SECONDS):
    """Project records in order and commit each one only after it is durably applied.

    A record that fails for a transient reason (database unavailable, timeout) is retried
    from its own offset: the consumer seeks back to it and never commits past it, so a later
    success cannot commit an offset that silently skips the failure. Only events that can
    never be projected (malformed payloads) are logged and skipped.
    """
    for record in consumer:
        try:
            project(record.value)
        except MalformedEvent:
            logger.error(json.dumps({"message": "discarding malformed scheduling event", "topic": record.topic, "partition": record.partition, "offset": record.offset}))
        except Exception:
            logger.exception(json.dumps({"message": "recommendation projection failed; retrying from the failed offset", "topic": record.topic, "partition": record.partition, "offset": record.offset}))
            seek(consumer, record)
            sleep(retry_seconds)
            continue
        consumer.commit()
