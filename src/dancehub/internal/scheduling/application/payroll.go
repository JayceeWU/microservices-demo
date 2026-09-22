package application

import (
	"context"
	"fmt"
	"time"
	_ "time/tzdata"

	"github.com/JayceeWU/microservices-demo/dancehub/internal/scheduling/domain"
)

// The caller holds the class row until its booking changes and this full
// snapshot have committed. Versions also protect against delayed outbox sends.
func payrollFact(ctx context.Context, r Repositories, session *SessionRecord) (*DomainEvent, error) {
	if session.Aggregate.Snapshot().Status != domain.ClassCompleted {
		return nil, nil
	}
	if session.StudioTimezone == "" {
		return nil, fmt.Errorf("class has no studio timezone snapshot")
	}
	zone, err := time.LoadLocation(session.StudioTimezone)
	if err != nil {
		return nil, fmt.Errorf("invalid studio timezone snapshot: %w", err)
	}
	count, err := r.Bookings().CountPayrollEligible(ctx, session.Aggregate.Snapshot().ID)
	if err != nil {
		return nil, err
	}
	session.PayrollFactVersion++
	v := session.View()
	return &DomainEvent{Type: "ClassCompleted", AggregateType: "class_session", AggregateID: v.ID, StudioID: v.StudioID, Payload: map[string]any{
		"class_session_id": v.ID, "studio_id": v.StudioID, "teacher_id": v.TeacherID,
		"starts_at": v.StartsAt, "approved_duration_minutes": int(v.EndsAt.Sub(v.StartsAt) / time.Minute), "redemption_count": count,
		"fact_version": session.PayrollFactVersion, "studio_timezone": session.StudioTimezone,
		"local_month": v.StartsAt.In(zone).Format("2006-01") + "-01",
	}}, nil
}

func publishPayrollCorrection(ctx context.Context, r Repositories, session *SessionRecord) error {
	event, err := payrollFact(ctx, r, session)
	if err != nil || event == nil {
		return err
	}
	if _, err = r.Sessions().Save(ctx, session, domain.ClassCompleted); err != nil {
		return err
	}
	return r.Outbox().Append(ctx, *event)
}

// A nonlocking lookup locates the immutable class ID. Every mutation then locks
// the class before its booking, including completion, capture and cancellation.
func lockClassBooking(ctx context.Context, r Repositories, id domain.BookingID) (*SessionRecord, *BookingRecord, error) {
	first, err := r.Bookings().Get(ctx, id, false)
	if err != nil {
		return nil, nil, err
	}
	session, err := r.Sessions().Get(ctx, first.Aggregate.Snapshot().SessionID, true)
	if err != nil {
		return nil, nil, err
	}
	booking, err := r.Bookings().Get(ctx, id, true)
	return session, booking, err
}

func payrollEligible(status domain.BookingStatus) bool {
	return status == domain.BookingCharged || status == domain.BookingAttended || status == domain.BookingNoShow
}
