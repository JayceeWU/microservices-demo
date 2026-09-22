package application

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/JayceeWU/microservices-demo/dancehub/internal/scheduling/domain"
)

func (s *Service) UpdateVideo(ctx context.Context, command UpdateVideoCommand) (SessionView, error) {
	user, err := requireUser(command.Actor)
	if err != nil || (!command.Actor.HasTenantRole("teacher", "studio_admin") && !command.Actor.IsPlatformAdmin()) {
		return SessionView{}, Denied("teacher or administrator role required")
	}
	if strings.TrimSpace(command.Audit.Reason) == "" || strings.TrimSpace(command.Audit.IdempotencyKey) == "" {
		return SessionView{}, Invalid("reason and idempotency_key are required")
	}
	videoURL := strings.TrimSpace(command.VideoURL)
	parsed, err := url.ParseRequestURI(videoURL)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
		return SessionView{}, Invalid("video_url must be an absolute HTTP(S) URL")
	}
	id, err := domain.NewClassSessionID(command.SessionID)
	if err != nil {
		return SessionView{}, Invalid(err.Error())
	}
	var result SessionView
	err = s.Work.Do(ctx, command.Actor, "schedulingservice", func(ctx context.Context, repositories Repositories) error {
		record, err := repositories.Sessions().UpdateVideo(ctx, id, string(user), command.Actor.StudioID, command.Actor.HasTenantRole("studio_admin") || command.Actor.IsPlatformAdmin(), videoURL)
		if err != nil {
			return err
		}
		result = record.View()
		return repositories.Outbox().Append(ctx, DomainEvent{Type: "ClassVideoUpdated", AggregateType: "class_session", AggregateID: result.ID, StudioID: result.StudioID, Payload: map[string]any{"class_session_id": result.ID, "video_url": videoURL}})
	})
	return result, err
}

func (s *Service) CreateRoomHold(ctx context.Context, command CreateRoomHoldCommand) (RoomReservationView, error) {
	user, err := requireUser(command.Actor)
	if err != nil {
		return RoomReservationView{}, err
	}
	if command.Audit.IdempotencyKey == "" {
		return RoomReservationView{}, Invalid("time range and audit are required")
	}
	period, err := domain.NewClassTimeRange(command.StartsAt, command.EndsAt, s.Clock.Now())
	if err != nil {
		return RoomReservationView{}, Invalid("future 15-minute time range is required")
	}
	room, err := s.Catalog.GetRoom(ctx, command.Actor, command.RoomID, command.StudioID)
	if err != nil {
		return RoomReservationView{}, NotFound("rentable room not found")
	}
	amount := room.RentalRateCentsPerHour * int64(period.Duration()/time.Minute) / 60
	record := &RoomReservationRecord{Aggregate: domain.NewRoomReservation("", period, s.Clock.Now()), StudioID: command.StudioID, RoomID: command.RoomID, StudentID: string(user), AmountCents: amount}
	var result RoomReservationView
	err = s.Work.Do(ctx, command.Actor, "schedulingservice", func(ctx context.Context, repositories Repositories) error {
		created, err := repositories.Rooms().Add(ctx, record, command.Audit.IdempotencyKey)
		if err == nil {
			result = created.View()
		}
		return err
	})
	return result, err
}

func (s *Service) GetRoomReservation(ctx context.Context, actor ActorContext, id string) (RoomReservationView, error) {
	reservationID, err := domain.NewRoomReservationID(id)
	if err != nil {
		return RoomReservationView{}, Invalid(err.Error())
	}
	var result RoomReservationView
	err = s.Work.Do(ctx, actor, "schedulingservice", func(ctx context.Context, repositories Repositories) error {
		record, err := repositories.Rooms().Get(ctx, reservationID, false)
		if err == nil {
			result = record.View()
		}
		return err
	})
	return result, err
}

func (s *Service) ChangeRoom(ctx context.Context, command ChangeRoomCommand) (RoomReservationView, error) {
	id, err := domain.NewRoomReservationID(command.ReservationID)
	if err != nil {
		return RoomReservationView{}, Invalid(err.Error())
	}
	var result RoomReservationView
	err = s.Work.Do(ctx, command.Actor, "schedulingservice", func(ctx context.Context, repositories Repositories) error {
		record, err := repositories.Rooms().Get(ctx, id, true)
		if err != nil {
			return err
		}
		before := record.Aggregate.Snapshot().Status
		if command.Confirm {
			err = record.Aggregate.ConfirmFromPayment(command.OrderID, s.Clock.Now())
		} else {
			err = record.Aggregate.Cancel()
		}
		if err != nil {
			return Conflict(err.Error())
		}
		record, err = repositories.Rooms().Save(ctx, record, before)
		if err == nil {
			result = record.View()
		}
		return err
	})
	return result, err
}

func requireUser(actor ActorContext) (domain.UserID, error) {
	if strings.TrimSpace(actor.UserID) == "" {
		return "", fmt.Errorf("%w: user identity is required", ErrUnauthenticated)
	}
	user, err := domain.NewUserID(actor.UserID)
	if err != nil {
		return "", fmt.Errorf("%w: user identity is invalid", ErrUnauthenticated)
	}
	return user, nil
}

func requireAdminSession(actor ActorContext, rawID string) (domain.UserID, domain.ClassSessionID, error) {
	user, err := requireUser(actor)
	if err != nil || (!actor.HasTenantRole("studio_admin") && !actor.IsPlatformAdmin()) {
		return "", "", Denied("studio administrator role required")
	}
	id, err := domain.NewClassSessionID(rawID)
	if err != nil {
		return "", "", Invalid(err.Error())
	}
	return user, id, nil
}

type sessionMutation func(context.Context, Repositories, *SessionRecord) (*DomainEvent, error)

func (s *Service) mutateSession(ctx context.Context, actor ActorContext, id domain.ClassSessionID, mutate sessionMutation) (SessionView, error) {
	var result SessionView
	err := s.Work.Do(ctx, actor, "schedulingservice", func(ctx context.Context, repositories Repositories) error {
		record, err := repositories.Sessions().Get(ctx, id, true)
		if err != nil {
			return err
		}
		before := record.Aggregate.Snapshot().Status
		event, err := mutate(ctx, repositories, record)
		if err != nil {
			return err
		}
		record, err = repositories.Sessions().Save(ctx, record, before)
		if err != nil {
			return err
		}
		result = record.View()
		if event == nil {
			return nil
		}
		event.AggregateID, event.StudioID = result.ID, result.StudioID
		if event.Payload == nil {
			event.Payload = make(map[string]any)
		}
		event.Payload["class_session_id"] = result.ID
		return repositories.Outbox().Append(ctx, *event)
	})
	return result, err
}
