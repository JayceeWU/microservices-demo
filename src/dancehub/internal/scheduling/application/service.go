package application

import (
	"context"
	"time"

	"github.com/JayceeWU/microservices-demo/dancehub/internal/scheduling/domain"
)

type Service struct {
	Work    UnitOfWork
	Credits CreditPort
	Catalog CatalogPort
	Clock   Clock
}

func NewService(work UnitOfWork, credits CreditPort, catalog CatalogPort, clock Clock) *Service {
	return &Service{Work: work, Credits: credits, Catalog: catalog, Clock: clock}
}

type SubmitScheduleCommand struct {
	Actor                                ActorContext
	StudioID, RoomID, Title, Description string
	StartsAt, EndsAt                     time.Time
	Capacity                             int32
	Audit                                AuditContext
}
type ReviewScheduleCommand struct {
	Actor           ActorContext
	SessionID       string
	Approve         bool
	MinimumStudents int32
	Audit           AuditContext
}
type BookClassCommand struct {
	Actor     ActorContext
	SessionID string
	Audit     AuditContext
}
type CancelBookingCommand struct {
	Actor     ActorContext
	BookingID string
	Audit     AuditContext
}
type CorrectAttendanceCommand struct {
	Actor             ActorContext
	BookingID         string
	Attended, Reverse bool
	Audit             AuditContext
}
type AddWalkInCommand struct {
	Actor                ActorContext
	SessionID, StudentID string
	Audit                AuditContext
}
type CompleteClassCommand struct {
	Actor     ActorContext
	SessionID string
	Audit     AuditContext
}
type CancelClassCommand struct {
	Actor     ActorContext
	SessionID string
	Audit     AuditContext
}
type UpdateVideoCommand struct {
	Actor               ActorContext
	SessionID, VideoURL string
	Audit               AuditContext
}
type CreateRoomHoldCommand struct {
	Actor            ActorContext
	StudioID, RoomID string
	StartsAt, EndsAt time.Time
	Audit            AuditContext
}
type ChangeRoomCommand struct {
	Actor                  ActorContext
	ReservationID, OrderID string
	Confirm                bool
	Audit                  AuditContext
}

func (s *Service) SearchSessions(ctx context.Context, actor ActorContext, query SearchSessionsQuery) (PageResult[SessionView], error) {
	if query.StudioID != "" {
		if _, err := domain.NewStudioID(query.StudioID); err != nil {
			return PageResult[SessionView]{}, Invalid(err.Error())
		}
	}
	if query.TeacherID != "" {
		if _, err := domain.NewUserID(query.TeacherID); err != nil {
			return PageResult[SessionView]{}, Invalid(err.Error())
		}
	}
	query.ViewerID = actor.UserID
	query.TenantID = actor.StudioID
	var result PageResult[SessionView]
	err := s.Work.Do(ctx, actor, "schedulingservice", func(ctx context.Context, repositories Repositories) error {
		var err error
		result, err = repositories.Sessions().Search(ctx, query)
		return err
	})
	return result, err
}

func (s *Service) GetSession(ctx context.Context, actor ActorContext, id string) (SessionView, error) {
	sessionID, err := domain.NewClassSessionID(id)
	if err != nil {
		return SessionView{}, Invalid(err.Error())
	}
	var result SessionView
	err = s.Work.Do(ctx, actor, "schedulingservice", func(ctx context.Context, repositories Repositories) error {
		record, err := repositories.Sessions().Get(ctx, sessionID, false)
		if err == nil {
			result = record.View()
		}
		return err
	})
	return result, err
}

func (s *Service) ListMyBookings(ctx context.Context, actor ActorContext, page PageRequest) (PageResult[BookingView], error) {
	user, err := requireUser(actor)
	if err != nil {
		return PageResult[BookingView]{}, err
	}
	var result PageResult[BookingView]
	err = s.Work.Do(ctx, actor, "schedulingservice", func(ctx context.Context, repositories Repositories) error {
		var err error
		result, err = repositories.Bookings().ListByStudent(ctx, user, page)
		return err
	})
	return result, err
}

func (s *Service) GetRoster(ctx context.Context, actor ActorContext, sessionID string, page PageRequest) (PageResult[BookingView], error) {
	id, err := domain.NewClassSessionID(sessionID)
	if err != nil {
		return PageResult[BookingView]{}, Invalid(err.Error())
	}
	var result PageResult[BookingView]
	err = s.Work.Do(ctx, actor, "schedulingservice", func(ctx context.Context, repositories Repositories) error {
		session, err := repositories.Sessions().Get(ctx, id, false)
		if err != nil {
			return err
		}
		teacher := string(session.Aggregate.Snapshot().TeacherID)
		if actor.UserID != teacher && !actor.HasTenantRole("studio_admin") && !actor.IsPlatformAdmin() {
			return Denied("roster is limited to the class teacher or studio administrator")
		}
		result, err = repositories.Bookings().ListBySession(ctx, id, page)
		return err
	})
	return result, err
}

func actorForStudent(parent ActorContext, userID, studioID string) ActorContext {
	return ActorContext{UserID: userID, StudioID: studioID, TenantRoles: []string{"student"}, ActorKind: "human", RequestID: parent.RequestID}
}
