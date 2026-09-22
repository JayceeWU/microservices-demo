import unittest
from collections import namedtuple

from projection import MalformedEvent, consume_projection, decode_event, parse_scheduling_event

Record = namedtuple("Record", ["topic", "partition", "offset", "value"])


def event(number):
    return {"event_id": f"event-{number}", "event_type": "BookingCreated", "tenant_id": "studio-1", "data": {"class_session_id": "class-1"}}


class FakeConsumer:
    """Single-partition consumer that replays records from a seekable position."""

    def __init__(self, values, stop_after=None):
        self.records = [Record("scheduling.events.v1", 0, offset, value) for offset, value in enumerate(values)]
        self.position = 0
        self.commits = []
        self.seeks = []
        self.remaining_deliveries = stop_after

    def __iter__(self):
        while self.position < len(self.records):
            if self.remaining_deliveries is not None:
                if self.remaining_deliveries == 0:
                    return
                self.remaining_deliveries -= 1
            record = self.records[self.position]
            self.position += 1
            yield record

    def commit(self):
        self.commits.append(self.position)

    def seek(self, partition, offset):
        self.seeks.append((partition, offset))
        self.position = offset


def fake_seek(consumer, record):
    consumer.seek((record.topic, record.partition), record.offset)


class ConsumeProjectionTests(unittest.TestCase):
    def test_commits_each_record_after_it_is_projected(self):
        consumer = FakeConsumer([event(1), event(2)])
        projected = []
        consume_projection(consumer, projected.append, seek=fake_seek, sleep=lambda _: None)
        self.assertEqual([e["event_id"] for e in projected], ["event-1", "event-2"])
        self.assertEqual(consumer.commits, [1, 2])

    def test_transient_failure_retries_the_same_offset_and_never_commits_past_it(self):
        consumer = FakeConsumer([event(1), event(2)])
        attempts = []
        sleeps = []

        def project(value):
            attempts.append(value["event_id"])
            if value["event_id"] == "event-1" and attempts.count("event-1") < 3:
                raise RuntimeError("database unavailable")

        consume_projection(consumer, project, seek=fake_seek, sleep=sleeps.append, retry_seconds=0.5)
        self.assertEqual(attempts, ["event-1", "event-1", "event-1", "event-2"])
        self.assertEqual(consumer.seeks, [(("scheduling.events.v1", 0), 0)] * 2)
        self.assertEqual(consumer.commits, [1, 2])
        self.assertEqual(sleeps, [0.5, 0.5])

    def test_failure_is_not_skipped_by_a_later_success(self):
        # Regression: the previous loop kept reading after a failure, so a later commit moved
        # the group offset past the failed event and it was lost for good.
        consumer = FakeConsumer([event(1), event(2)], stop_after=3)
        seen = []

        def project(value):
            seen.append(value["event_id"])
            if value["event_id"] == "event-1":
                raise RuntimeError("database unavailable")

        consume_projection(consumer, project, seek=fake_seek, sleep=lambda _: None)
        self.assertEqual(seen, ["event-1"] * 3)
        self.assertEqual(consumer.commits, [])

    def test_malformed_events_are_skipped_and_committed(self):
        consumer = FakeConsumer([None, {"event_id": "x"}, event(3)])
        projected = []

        def project(value):
            parse_scheduling_event(value)
            projected.append(value["event_id"])

        with self.assertLogs("recommendationservice", level="ERROR") as logs:
            consume_projection(consumer, project, seek=fake_seek, sleep=lambda _: None)
        self.assertEqual(projected, ["event-3"])
        self.assertEqual(consumer.commits, [1, 2, 3])
        self.assertEqual(consumer.seeks, [])
        self.assertEqual(len(logs.output), 2)


class ParsingTests(unittest.TestCase):
    def test_decode_event_returns_none_for_undecodable_bodies(self):
        self.assertIsNone(decode_event(b"\xff"))
        self.assertIsNone(decode_event(b"not json"))
        self.assertEqual(decode_event(b'{"a":1}'), {"a": 1})

    def test_parse_rejects_missing_fields(self):
        for value in [None, {}, {"event_id": "1", "event_type": "t", "tenant_id": "s"}, {"event_id": "1", "event_type": "t", "tenant_id": "s", "data": {}}]:
            with self.assertRaises(MalformedEvent):
                parse_scheduling_event(value)
        self.assertEqual(parse_scheduling_event(event(1)), ("event-1", "BookingCreated", "studio-1"))


if __name__ == "__main__":
    unittest.main()
