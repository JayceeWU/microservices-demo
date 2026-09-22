// Package projection maintains the payroll read model from Scheduling facts.
package projection

import (
	"encoding/json"
	"fmt"
	"time"
	_ "time/tzdata"

	"github.com/google/uuid"
)

const Topic = "scheduling.events.v1"
const Group = "payroll-projector-v1"

type Fact struct {
	ClassSessionID string    `json:"class_session_id"`
	StudioID       string    `json:"studio_id"`
	TeacherID      string    `json:"teacher_id"`
	StartsAt       time.Time `json:"starts_at"`
	Duration       int       `json:"approved_duration_minutes"`
	Redemptions    int       `json:"redemption_count"`
	Version        int64     `json:"fact_version"`
	Timezone       string    `json:"studio_timezone"`
	LocalMonth     string    `json:"local_month"`
}

type Event struct {
	ID            string `json:"event_id"`
	Type          string `json:"event_type"`
	SchemaVersion int    `json:"schema_version"`
	Producer      string `json:"producer"`
	TenantID      string `json:"tenant_id"`
	AggregateType string `json:"aggregate_type"`
	AggregateID   string `json:"aggregate_id"`
	Traceparent   string `json:"traceparent"`
	Fact          Fact   `json:"data"`
}

// Decode ignores other valid event types on the shared topic. An invalid payroll
// fact is an error, so its offset cannot be acknowledged and silently lost.
func Decode(data []byte) (Event, bool, error) {
	var envelope struct {
		Type string `json:"event_type"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil || envelope.Type == "" {
		return Event{}, false, fmt.Errorf("invalid scheduling event envelope")
	}
	if envelope.Type != "ClassCompleted" {
		return Event{}, false, nil
	}
	var event Event
	if err := json.Unmarshal(data, &event); err != nil {
		return Event{}, false, fmt.Errorf("invalid payroll fact: %w", err)
	}
	f := event.Fact
	if event.SchemaVersion != 1 || event.Producer != "scheduling" || event.AggregateType != "class_session" || event.AggregateID != f.ClassSessionID || event.TenantID != f.StudioID {
		return Event{}, false, fmt.Errorf("payroll event identity or schema mismatch")
	}
	for _, id := range []string{event.ID, f.ClassSessionID, f.StudioID, f.TeacherID} {
		if parsed, err := uuid.Parse(id); err != nil || parsed == uuid.Nil {
			return Event{}, false, fmt.Errorf("payroll event contains an invalid identifier")
		}
	}
	if f.Version < 1 || f.Duration <= 0 || f.Duration%15 != 0 || f.Redemptions < 0 || f.StartsAt.IsZero() || f.Timezone == "" {
		return Event{}, false, fmt.Errorf("payroll event contains invalid amounts or version")
	}
	zone, err := time.LoadLocation(f.Timezone)
	if err != nil {
		return Event{}, false, fmt.Errorf("invalid payroll studio timezone: %w", err)
	}
	if expected := f.StartsAt.In(zone).Format("2006-01") + "-01"; f.LocalMonth != expected {
		return Event{}, false, fmt.Errorf("payroll month does not match the frozen studio timezone")
	}
	return event, true, nil
}
