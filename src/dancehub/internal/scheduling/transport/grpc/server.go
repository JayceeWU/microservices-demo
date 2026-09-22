package grpctransport

import (
	"context"
	"errors"
	"strings"

	commonv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/common/v1"
	schedulingv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/scheduling/v1"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/platform"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/scheduling/application"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/scheduling/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Server struct {
	schedulingv1.UnimplementedSchedulingServiceServer
	service schedulingApplication
}

type schedulingApplication interface {
	SearchSessions(context.Context, application.ActorContext, application.SearchSessionsQuery) (application.PageResult[application.SessionView], error)
	GetSession(context.Context, application.ActorContext, string) (application.SessionView, error)
	ListMyBookings(context.Context, application.ActorContext, application.PageRequest) (application.PageResult[application.BookingView], error)
	GetRoster(context.Context, application.ActorContext, string, application.PageRequest) (application.PageResult[application.BookingView], error)
	SubmitSchedule(context.Context, application.SubmitScheduleCommand) (application.SessionView, error)
	ReviewSchedule(context.Context, application.ReviewScheduleCommand) (application.SessionView, error)
	BookClass(context.Context, application.BookClassCommand) (application.BookingView, error)
	CancelBooking(context.Context, application.CancelBookingCommand) (application.BookingView, error)
	CorrectAttendance(context.Context, application.CorrectAttendanceCommand) (application.BookingView, error)
	AddWalkIn(context.Context, application.AddWalkInCommand) (application.BookingView, error)
	CompleteClass(context.Context, application.CompleteClassCommand) (application.SessionView, error)
	CancelClass(context.Context, application.CancelClassCommand) (application.SessionView, error)
	UpdateVideo(context.Context, application.UpdateVideoCommand) (application.SessionView, error)
	CreateRoomHold(context.Context, application.CreateRoomHoldCommand) (application.RoomReservationView, error)
	GetRoomReservation(context.Context, application.ActorContext, string) (application.RoomReservationView, error)
	ChangeRoom(context.Context, application.ChangeRoomCommand) (application.RoomReservationView, error)
}

func NewServer(service schedulingApplication) *Server { return &Server{service: service} }

func (s *Server) SearchClassSessions(ctx context.Context, request *schedulingv1.SearchClassSessionsRequest) (*schedulingv1.SearchClassSessionsResponse, error) {
	items, err := s.service.SearchSessions(ctx, actor(ctx), application.SearchSessionsQuery{StudioID: request.StudioId, TeacherID: request.TeacherId, Page: page(request.Page), BookableOnly: request.BookableOnly})
	if err != nil {
		return nil, grpcError(err)
	}
	response := &schedulingv1.SearchClassSessionsResponse{Page: &commonv1.PageResponse{NextPageToken: items.NextPageToken}}
	for _, item := range items.Items {
		response.Sessions = append(response.Sessions, session(item))
	}
	return response, nil
}

func (s *Server) GetClassSession(ctx context.Context, request *schedulingv1.GetClassSessionRequest) (*schedulingv1.ClassSession, error) {
	item, err := s.service.GetSession(ctx, actor(ctx), request.Id)
	if err != nil {
		return nil, grpcError(err)
	}
	return session(item), nil
}

func (s *Server) ListMyBookings(ctx context.Context, request *schedulingv1.ListMyBookingsRequest) (*schedulingv1.ListMyBookingsResponse, error) {
	items, err := s.service.ListMyBookings(ctx, actor(ctx), page(request.Page))
	if err != nil {
		return nil, grpcError(err)
	}
	response := &schedulingv1.ListMyBookingsResponse{Page: &commonv1.PageResponse{NextPageToken: items.NextPageToken}}
	for _, item := range items.Items {
		response.Bookings = append(response.Bookings, booking(item))
	}
	return response, nil
}

func (s *Server) GetRoster(ctx context.Context, request *schedulingv1.GetRosterRequest) (*schedulingv1.GetRosterResponse, error) {
	items, err := s.service.GetRoster(ctx, actor(ctx), request.ClassSessionId, page(request.Page))
	if err != nil {
		return nil, grpcError(err)
	}
	response := &schedulingv1.GetRosterResponse{Page: &commonv1.PageResponse{NextPageToken: items.NextPageToken}}
	for _, item := range items.Items {
		response.Bookings = append(response.Bookings, booking(item))
	}
	return response, nil
}

func (s *Server) SubmitScheduleRequest(ctx context.Context, request *schedulingv1.SubmitScheduleRequestRequest) (*schedulingv1.ClassSession, error) {
	if request.Proposed == nil || request.Proposed.TimeRange == nil || request.Proposed.TimeRange.StartsAt == nil || request.Proposed.TimeRange.EndsAt == nil {
		return nil, status.Error(codes.InvalidArgument, "proposed class and time range are required")
	}
	p := request.Proposed
	item, err := s.service.SubmitSchedule(ctx, application.SubmitScheduleCommand{Actor: actor(ctx), StudioID: p.StudioId, RoomID: p.RoomId, Title: p.Title, Description: p.Description, StartsAt: p.TimeRange.StartsAt.AsTime(), EndsAt: p.TimeRange.EndsAt.AsTime(), Capacity: p.Capacity, Audit: audit(request.Audit)})
	if err != nil {
		return nil, grpcError(err)
	}
	return session(item), nil
}

func (s *Server) ReviewScheduleRequest(ctx context.Context, request *schedulingv1.ReviewScheduleRequestRequest) (*schedulingv1.ClassSession, error) {
	item, err := s.service.ReviewSchedule(ctx, application.ReviewScheduleCommand{Actor: actor(ctx), SessionID: request.ClassSessionId, Approve: request.Approve, MinimumStudents: request.MinimumStudents, Audit: audit(request.Audit)})
	if err != nil {
		return nil, grpcError(err)
	}
	return session(item), nil
}

func (s *Server) BookClass(ctx context.Context, request *schedulingv1.BookClassRequest) (*schedulingv1.Booking, error) {
	item, err := s.service.BookClass(ctx, application.BookClassCommand{Actor: actor(ctx), SessionID: request.ClassSessionId, Audit: audit(request.Audit)})
	if err != nil {
		return nil, grpcError(err)
	}
	return booking(item), nil
}

func (s *Server) CancelBooking(ctx context.Context, request *schedulingv1.CancelBookingRequest) (*schedulingv1.Booking, error) {
	item, err := s.service.CancelBooking(ctx, application.CancelBookingCommand{Actor: actor(ctx), BookingID: request.BookingId, Audit: audit(request.Audit)})
	if err != nil {
		return nil, grpcError(err)
	}
	return booking(item), nil
}

func (s *Server) CorrectAttendance(ctx context.Context, request *schedulingv1.CorrectAttendanceRequest) (*schedulingv1.Booking, error) {
	return s.correctAttendance(ctx, request.BookingId, request.Attended, false, request.Audit)
}

func (s *Server) ReverseClassRedemption(ctx context.Context, request *schedulingv1.ReverseClassRedemptionRequest) (*schedulingv1.Booking, error) {
	return s.correctAttendance(ctx, request.BookingId, false, true, request.Audit)
}

func (s *Server) correctAttendance(ctx context.Context, id string, attended, reverse bool, value *commonv1.AuditContext) (*schedulingv1.Booking, error) {
	item, err := s.service.CorrectAttendance(ctx, application.CorrectAttendanceCommand{Actor: actor(ctx), BookingID: id, Attended: attended, Reverse: reverse, Audit: audit(value)})
	if err != nil {
		return nil, grpcError(err)
	}
	return booking(item), nil
}

func (s *Server) AddWalkInRedemption(ctx context.Context, request *schedulingv1.AddWalkInRedemptionRequest) (*schedulingv1.Booking, error) {
	item, err := s.service.AddWalkIn(ctx, application.AddWalkInCommand{Actor: actor(ctx), SessionID: request.ClassSessionId, StudentID: request.StudentId, Audit: audit(request.Audit)})
	if err != nil {
		return nil, grpcError(err)
	}
	return booking(item), nil
}

func (s *Server) ConfirmClassCompleted(ctx context.Context, request *schedulingv1.ConfirmClassCompletedRequest) (*schedulingv1.ClassSession, error) {
	item, err := s.service.CompleteClass(ctx, application.CompleteClassCommand{Actor: actor(ctx), SessionID: request.ClassSessionId, Audit: audit(request.Audit)})
	if err != nil {
		return nil, grpcError(err)
	}
	return session(item), nil
}

func (s *Server) CancelClassByStudio(ctx context.Context, request *schedulingv1.CancelClassByStudioRequest) (*schedulingv1.ClassSession, error) {
	item, err := s.service.CancelClass(ctx, application.CancelClassCommand{Actor: actor(ctx), SessionID: request.ClassSessionId, Audit: audit(request.Audit)})
	if err != nil {
		return nil, grpcError(err)
	}
	return session(item), nil
}

func (s *Server) UpdateClassVideo(ctx context.Context, request *schedulingv1.UpdateClassVideoRequest) (*schedulingv1.ClassSession, error) {
	item, err := s.service.UpdateVideo(ctx, application.UpdateVideoCommand{Actor: actor(ctx), SessionID: request.ClassSessionId, VideoURL: request.VideoUrl, Audit: audit(request.Audit)})
	if err != nil {
		return nil, grpcError(err)
	}
	return session(item), nil
}

func (s *Server) CreateRoomHold(ctx context.Context, request *schedulingv1.CreateRoomHoldRequest) (*schedulingv1.RoomReservation, error) {
	if request.TimeRange == nil || request.TimeRange.StartsAt == nil || request.TimeRange.EndsAt == nil {
		return nil, status.Error(codes.InvalidArgument, "time range and audit are required")
	}
	item, err := s.service.CreateRoomHold(ctx, application.CreateRoomHoldCommand{Actor: actor(ctx), StudioID: request.StudioId, RoomID: request.RoomId, StartsAt: request.TimeRange.StartsAt.AsTime(), EndsAt: request.TimeRange.EndsAt.AsTime(), Audit: audit(request.Audit)})
	if err != nil {
		return nil, grpcError(err)
	}
	return room(item), nil
}

func (s *Server) GetRoomReservation(ctx context.Context, request *schedulingv1.GetRoomReservationRequest) (*schedulingv1.RoomReservation, error) {
	item, err := s.service.GetRoomReservation(ctx, actor(ctx), request.RoomReservationId)
	if err != nil {
		return nil, grpcError(err)
	}
	return room(item), nil
}

func (s *Server) ConfirmRoomReservation(ctx context.Context, request *schedulingv1.ConfirmRoomReservationRequest) (*schedulingv1.RoomReservation, error) {
	return s.changeRoom(ctx, request.RoomReservationId, request.OrderId, true, request.Audit)
}

func (s *Server) ReleaseRoomReservation(ctx context.Context, request *schedulingv1.ReleaseRoomReservationRequest) (*schedulingv1.RoomReservation, error) {
	return s.changeRoom(ctx, request.RoomReservationId, "", false, request.Audit)
}

func (s *Server) changeRoom(ctx context.Context, id, orderID string, confirm bool, value *commonv1.AuditContext) (*schedulingv1.RoomReservation, error) {
	item, err := s.service.ChangeRoom(ctx, application.ChangeRoomCommand{Actor: actor(ctx), ReservationID: id, OrderID: orderID, Confirm: confirm, Audit: audit(value)})
	if err != nil {
		return nil, grpcError(err)
	}
	return room(item), nil
}

func actor(ctx context.Context) application.ActorContext {
	return platform.Actor(ctx)
}

func audit(value *commonv1.AuditContext) application.AuditContext {
	if value == nil {
		return application.AuditContext{}
	}
	return application.NewAuditContext(value.IdempotencyKey, value.Reason)
}

func page(value *commonv1.PageRequest) application.PageRequest {
	if value == nil {
		return application.PageRequest{}
	}
	return application.PageRequest{Size: value.PageSize, Token: value.PageToken}
}

func session(value application.SessionView) *schedulingv1.ClassSession {
	return &schedulingv1.ClassSession{Id: value.ID, StudioId: value.StudioID, RoomId: value.RoomID, TeacherId: value.TeacherID, Title: value.Title, Description: value.Description, TimeRange: &commonv1.TimeRange{StartsAt: timestamppb.New(value.StartsAt), EndsAt: timestamppb.New(value.EndsAt)}, Capacity: value.Capacity, MinimumStudents: value.MinimumStudents, CreditCost: &commonv1.CreditAmount{Units: value.CreditCost}, ConfirmedCount: value.ConfirmedCount, Status: classStatus(value.Status), VideoUrl: value.VideoURL}
}

func booking(value application.BookingView) *schedulingv1.Booking {
	return &schedulingv1.Booking{Id: value.ID, StudioId: value.StudioID, ClassSessionId: value.ClassSessionID, StudentId: value.StudentID, Status: bookingStatus(value.Status), IsWalkIn: value.WalkIn, CreditCompensationPending: value.CreditCompensationPending}
}

func room(value application.RoomReservationView) *schedulingv1.RoomReservation {
	return &schedulingv1.RoomReservation{Id: value.ID, StudioId: value.StudioID, RoomId: value.RoomID, StudentId: value.StudentID, TimeRange: &commonv1.TimeRange{StartsAt: timestamppb.New(value.StartsAt), EndsAt: timestamppb.New(value.EndsAt)}, Amount: &commonv1.Money{AmountCents: value.AmountCents}, Status: roomStatus(value.Status), HoldExpiresAt: timestamppb.New(value.HoldExpiresAt)}
}

func classStatus(value domain.ClassStatus) schedulingv1.ClassStatus {
	return map[domain.ClassStatus]schedulingv1.ClassStatus{domain.ClassPendingApproval: 1, domain.ClassApproved: 2, domain.ClassOpen: 3, domain.ClassMinimumConfirmed: 4, domain.ClassAwaitingAdminConfirmation: 5, domain.ClassCompleted: 6, domain.ClassCancelled: 7}[value]
}
func bookingStatus(value domain.BookingStatus) schedulingv1.BookingStatus {
	return map[domain.BookingStatus]schedulingv1.BookingStatus{domain.BookingPendingCredit: 1, domain.BookingConfirmed: 2, domain.BookingCharged: 3, domain.BookingAttended: 4, domain.BookingNoShow: 5, domain.BookingCancelled: 6, domain.BookingReversed: 7}[value]
}
func roomStatus(value domain.RoomReservationStatus) schedulingv1.RoomReservationStatus {
	return map[domain.RoomReservationStatus]schedulingv1.RoomReservationStatus{domain.RoomHold: 1, domain.RoomPendingPayment: 2, domain.RoomConfirmed: 3, domain.RoomExpired: 4, domain.RoomRefundPending: 5, domain.RoomRefunded: 6, domain.RoomCancelled: 7}[value]
}

func grpcError(err error) error {
	switch {
	case errors.Is(err, application.ErrUnauthenticated):
		return status.Error(codes.Unauthenticated, clean(err, application.ErrUnauthenticated))
	case errors.Is(err, application.ErrPermissionDenied), errors.Is(err, domain.ErrPermissionDenied):
		return status.Error(codes.PermissionDenied, clean(err, application.ErrPermissionDenied))
	case errors.Is(err, application.ErrInvalidArgument), errors.Is(err, domain.ErrInvalidArgument):
		return status.Error(codes.InvalidArgument, clean(err, application.ErrInvalidArgument))
	case errors.Is(err, application.ErrNotFound):
		return status.Error(codes.NotFound, clean(err, application.ErrNotFound))
	case errors.Is(err, application.ErrCapacity), errors.Is(err, domain.ErrCapacityExceeded):
		return status.Error(codes.ResourceExhausted, clean(err, application.ErrCapacity))
	case errors.Is(err, application.ErrConflict), errors.Is(err, domain.ErrInvalidTransition), errors.Is(err, domain.ErrWindowClosed):
		return status.Error(codes.FailedPrecondition, clean(err, application.ErrConflict))
	case errors.Is(err, application.ErrUnavailable):
		return status.Error(codes.Unavailable, clean(err, application.ErrUnavailable))
	default:
		return status.Error(codes.Internal, "scheduling operation failed")
	}
}

func clean(err error, marker error) string {
	return strings.TrimSpace(strings.TrimPrefix(err.Error(), marker.Error()+":"))
}
