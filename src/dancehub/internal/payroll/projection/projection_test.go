package projection

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

func sampleEvent() Event {
	return Event{
		ID: "90000000-0000-0000-0000-000000000001", Type: "ClassCompleted", SchemaVersion: 1,
		Producer: "scheduling", TenantID: "10000000-0000-0000-0000-000000000001",
		AggregateType: "class_session", AggregateID: "40000000-0000-0000-0000-000000000001",
		Fact: Fact{ClassSessionID: "40000000-0000-0000-0000-000000000001",
			StudioID: "10000000-0000-0000-0000-000000000001", TeacherID: "30000000-0000-0000-0000-000000000002",
			StartsAt: time.Date(2026, 9, 1, 0, 30, 0, 0, time.UTC), Duration: 75, Redemptions: 4,
			Version: 1, Timezone: "America/Los_Angeles", LocalMonth: "2026-08-01"},
	}
}

func eventBytes(t *testing.T, event Event) []byte {
	t.Helper()
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestDecodePreservesFrozenLocalMonthAcrossDST(t *testing.T) {
	for _, instant := range []string{"2026-03-01T07:30:00Z", "2026-04-01T06:30:00Z", "2026-11-01T06:30:00Z", "2026-12-01T07:30:00Z"} {
		t.Run(instant, func(t *testing.T) {
			event := sampleEvent()
			event.Fact.StartsAt, _ = time.Parse(time.RFC3339, instant)
			event.Fact.LocalMonth = event.Fact.StartsAt.AddDate(0, -1, 0).Format("2006-01") + "-01"
			got, relevant, err := Decode(eventBytes(t, event))
			if err != nil || !relevant || got.Fact.LocalMonth != event.Fact.LocalMonth {
				t.Fatalf("unexpected local month: %+v, relevant=%v, err=%v", got.Fact, relevant, err)
			}
		})
	}
}

func TestDecodeRejectsInvalidFacts(t *testing.T) {
	cases := map[string]func(*Event){
		"month uses UTC":     func(e *Event) { e.Fact.LocalMonth = "2026-09-01" },
		"invalid timezone":   func(e *Event) { e.Fact.Timezone = "not/a/timezone" },
		"missing timezone":   func(e *Event) { e.Fact.Timezone = "" },
		"tenant mismatch":    func(e *Event) { e.TenantID = "10000000-0000-0000-0000-000000000002" },
		"aggregate mismatch": func(e *Event) { e.AggregateID = "40000000-0000-0000-0000-000000000002" },
		"wrong producer":     func(e *Event) { e.Producer = "payment" },
		"wrong schema":       func(e *Event) { e.SchemaVersion = 2 },
		"invalid event id":   func(e *Event) { e.ID = "not-uuid" },
		"zero teacher id":    func(e *Event) { e.Fact.TeacherID = "00000000-0000-0000-0000-000000000000" },
		"negative count":     func(e *Event) { e.Fact.Redemptions = -1 },
		"no version":         func(e *Event) { e.Fact.Version = 0 },
		"invalid duration":   func(e *Event) { e.Fact.Duration = 16 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			event := sampleEvent()
			mutate(&event)
			if _, _, err := Decode(eventBytes(t, event)); err == nil {
				t.Fatal("invalid payroll event was accepted")
			}
		})
	}
}

type storeFunc func(context.Context, Event) error

func (f storeFunc) Apply(ctx context.Context, event Event) error { return f(ctx, event) }

func TestOffsetCommitFollowsDatabaseCommitAndAllowsReplay(t *testing.T) {
	var steps []string
	store := storeFunc(func(_ context.Context, event Event) error {
		steps = append(steps, "database:"+event.ID)
		return nil
	})
	record := &kgo.Record{Value: eventBytes(t, sampleEvent()), Offset: 12}
	commitFailure := errors.New("rebalance during commit")
	for attempt := 0; attempt < 2; attempt++ {
		err := processRecord(context.Background(), store, record, func(_ context.Context, value *kgo.Record) error {
			if value != record {
				t.Fatal("committed a record other than the applied record")
			}
			steps = append(steps, "offset")
			if attempt == 0 {
				return commitFailure
			}
			return nil
		})
		if attempt == 0 && !errors.Is(err, commitFailure) || attempt == 1 && err != nil {
			t.Fatalf("unexpected commit result: %v", err)
		}
	}
	if want := []string{"database:" + sampleEvent().ID, "offset", "database:" + sampleEvent().ID, "offset"}; !reflect.DeepEqual(steps, want) {
		t.Fatalf("wrong commit order: %v", steps)
	}
}

func TestFailedDatabaseOrInvalidFactNeverCommitsOffset(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		event := sampleEvent()
		if invalid {
			event.Fact.Version = 0
		}
		err := processRecord(context.Background(), storeFunc(func(context.Context, Event) error {
			return errors.New("database unavailable")
		}), &kgo.Record{Value: eventBytes(t, event)}, func(context.Context, *kgo.Record) error {
			t.Fatal("failed record offset was acknowledged")
			return nil
		})
		if err == nil {
			t.Fatal("processing failure was lost")
		}
	}
}

func TestUnrelatedSchedulingEventDoesNotWritePayroll(t *testing.T) {
	committed := false
	err := processRecord(context.Background(), storeFunc(func(context.Context, Event) error {
		t.Fatal("unrelated event reached payroll store")
		return nil
	}), &kgo.Record{Value: []byte(`{"event_type":"BookingConfirmed","data":{"other":"shape"}}`)}, func(context.Context, *kgo.Record) error {
		committed = true
		return nil
	})
	if err != nil || !committed {
		t.Fatalf("unrelated event was not consumed: %v", err)
	}
}
