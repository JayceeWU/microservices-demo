package application

import (
	"context"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/scheduling/domain"
	"testing"
	"time"
)

type cancellationRepos struct {
	fakeRepositories
	compensation *cancellationJobs
}

func (r cancellationRepos) Compensations() CompensationRepository { return r.compensation }

type cancellationJobs struct {
	CompensationRepository
	jobs map[string]bool
}

func (r *cancellationJobs) Enqueue(_ context.Context, b *BookingRecord, _ string) error {
	r.jobs[string(b.Aggregate.Snapshot().ID)] = true
	b.CreditCompensationPending = true
	return nil
}

func TestCancellationQueuesMissingHoldAndDoesNotReleaseSeatTwice(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	period, _ := domain.RehydrateClassTimeRange(now.Add(8*time.Hour), now.Add(9*time.Hour))
	session, _ := domain.RehydrateClassSession(domain.ClassSessionSnapshot{ID: testClass, StudioID: testStudio, TimeRange: period, Capacity: 20, Minimum: 4, Confirmed: 1, Status: domain.ClassOpen})
	booking := &BookingRecord{StudioID: testStudio, Aggregate: domain.RehydrateBooking(domain.BookingSnapshot{ID: testBooking, SessionID: testClass, StudentID: testUser, Status: domain.BookingPendingCredit})}
	jobs := &cancellationJobs{jobs: map[string]bool{}}
	work := &fakeWork{repositories: cancellationRepos{fakeRepositories: fakeRepositories{sessions: &fakeSessions{record: &SessionRecord{Aggregate: session}}, bookings: &fakeBookings{record: booking}}, compensation: jobs}}
	s := NewService(work, nil, nil, fixedClock{now: now})
	command := CancelBookingCommand{Actor: ActorContext{UserID: testUser, StudioID: testStudio, ActorKind: "human"}, BookingID: testBooking}
	for i := 0; i < 2; i++ {
		result, err := s.CancelBooking(context.Background(), command)
		if err != nil || result.Status != domain.BookingCancelled || !result.CreditCompensationPending {
			t.Fatalf("cancel %d: result=%+v err=%v", i, result, err)
		}
	}
	if session.Snapshot().Confirmed != 0 || len(jobs.jobs) != 1 {
		t.Fatal("duplicate cancellation duplicated its seat or compensation effects")
	}
	if booking.Aggregate.Snapshot().HoldID != "" {
		t.Fatal("test requires lost PlaceHold response without persisted hold id")
	}
}
