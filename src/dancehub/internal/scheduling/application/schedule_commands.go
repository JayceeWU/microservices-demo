package application

import (
	"context"
	"time"

	"github.com/JayceeWU/microservices-demo/dancehub/internal/scheduling/domain"
)

func (s *Service) SubmitSchedule(ctx context.Context, command SubmitScheduleCommand) (SessionView, error) {
	if !command.Actor.HasTenantRole("teacher", "studio_admin") && !command.Actor.IsPlatformAdmin() {
		return SessionView{}, Denied("teacher role required")
	}
	user, err := requireUser(command.Actor)
	if err != nil {
		return SessionView{}, err
	}
	period, err := domain.NewClassTimeRange(command.StartsAt, command.EndsAt, s.Clock.Now())
	if err != nil {
		return SessionView{}, Invalid(err.Error())
	}
	studio, err := domain.NewStudioID(command.StudioID)
	if err != nil {
		return SessionView{}, Invalid(err.Error())
	}
	capacity, err := domain.NewCapacity(command.Capacity)
	if err != nil {
		return SessionView{}, Invalid(err.Error())
	}
	aggregate, err := domain.NewClassSession("", studio, command.RoomID, user, command.Title, period, capacity)
	if err != nil {
		return SessionView{}, Invalid(err.Error())
	}
	timezone, err := s.Catalog.GetStudioTimezone(ctx, command.Actor, command.StudioID)
	if err != nil {
		return SessionView{}, Unavailable("unable to load studio timezone", err)
	}
	if timezone == "" {
		return SessionView{}, Invalid("studio timezone is required")
	}
	if _, err = time.LoadLocation(timezone); err != nil {
		return SessionView{}, Invalid("studio timezone is invalid")
	}
	var result SessionView
	err = s.Work.Do(ctx, command.Actor, "schedulingservice", func(ctx context.Context, repositories Repositories) error {
		record, err := repositories.Sessions().Add(ctx, aggregate, command.Description, timezone)
		if err == nil {
			result = record.View()
		}
		return err
	})
	return result, err
}

func (s *Service) ReviewSchedule(ctx context.Context, command ReviewScheduleCommand) (SessionView, error) {
	if command.Actor.StudioID == "" && !command.Actor.IsPlatformAdmin() {
		return SessionView{}, Invalid("selected studio is required")
	}
	_, id, err := requireAdminSession(command.Actor, command.SessionID)
	if err != nil {
		return SessionView{}, err
	}
	return s.mutateSession(ctx, command.Actor, id, func(ctx context.Context, repositories Repositories, record *SessionRecord) (*DomainEvent, error) {
		minimum := command.MinimumStudents
		if minimum < 1 {
			minimum = 4
		}
		value, err := domain.NewMinimumStudents(minimum, record.Aggregate.Snapshot().Capacity)
		if err != nil {
			return nil, Invalid(err.Error())
		}
		if command.Approve {
			err = record.Aggregate.Approve(value)
		} else {
			err = record.Aggregate.Reject(command.Audit.Reason)
		}
		if err != nil {
			return nil, Conflict(err.Error())
		}
		if command.Approve {
			return &DomainEvent{Type: "ClassApproved", AggregateType: "class_session", Payload: map[string]any{"minimum_students": minimum}}, nil
		}
		return nil, nil
	})
}
