package main

import (
	"net/http"
	"strconv"
	"time"

	commonv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/common/v1"
	schedulingv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/scheduling/v1"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/platform"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (g *gateway) scheduling(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := grpcRequestContext(r, 10*time.Second)
	defer cancel()
	page := &commonv1.PageRequest{PageSize: 24, PageToken: r.URL.Query().Get("page_token")}
	if r.URL.Query().Has("page_size") {
		size, err := strconv.ParseInt(r.URL.Query().Get("page_size"), 10, 32)
		if err != nil || size < 0 {
			platform.Problem(w, http.StatusBadRequest, "invalid_page_size", "page_size must be a non-negative integer")
			return
		}
		if size > 100 {
			size = 100
		}
		if size > 0 {
			page.PageSize = int32(size)
		}
	}
	switch r.Pattern {
	case "GET /v1/class-sessions":
		var bookableOnly *bool
		if r.URL.Query().Has("bookable_only") {
			value, err := strconv.ParseBool(r.URL.Query().Get("bookable_only"))
			if err != nil {
				platform.Problem(w, http.StatusBadRequest, "invalid_bookable_filter", "bookable_only must be true or false")
				return
			}
			bookableOnly = &value
		}
		response, err := g.scheduleClient.SearchClassSessions(ctx, &schedulingv1.SearchClassSessionsRequest{StudioId: r.URL.Query().Get("studio_id"), TeacherId: r.URL.Query().Get("teacher_id"), Page: page, BookableOnly: bookableOnly})
		if err != nil {
			grpcProblem(w, err)
			return
		}
		items := make([]map[string]any, 0, len(response.Sessions))
		for _, item := range response.Sessions {
			items = append(items, sessionJSON(item))
		}
		platform.JSON(w, http.StatusOK, map[string]any{"sessions": items, "nextPageToken": response.GetPage().GetNextPageToken()})
	case "GET /v1/class-sessions/{id}":
		item, err := g.scheduleClient.GetClassSession(ctx, &schedulingv1.GetClassSessionRequest{Id: r.PathValue("id")})
		if err != nil {
			grpcProblem(w, err)
			return
		}
		platform.JSON(w, http.StatusOK, sessionJSON(item))
	case "GET /v1/class-sessions/{classSessionId}/roster":
		response, err := g.scheduleClient.GetRoster(ctx, &schedulingv1.GetRosterRequest{ClassSessionId: r.PathValue("classSessionId"), Page: page})
		if err != nil {
			grpcProblem(w, err)
			return
		}
		platform.JSON(w, http.StatusOK, bookingCollectionJSON(response.Bookings, response.GetPage()))
	case "GET /v1/bookings":
		response, err := g.scheduleClient.ListMyBookings(ctx, &schedulingv1.ListMyBookingsRequest{Page: page})
		if err != nil {
			grpcProblem(w, err)
			return
		}
		platform.JSON(w, http.StatusOK, bookingCollectionJSON(response.Bookings, response.GetPage()))
	case "POST /v1/class-sessions":
		var input struct {
			StudioID       string    `json:"studioId"`
			RoomID         string    `json:"roomId"`
			Title          string    `json:"title"`
			Description    string    `json:"description"`
			StartsAt       time.Time `json:"startsAt"`
			EndsAt         time.Time `json:"endsAt"`
			Capacity       int32     `json:"capacity"`
			Reason         string    `json:"reason"`
			IdempotencyKey string    `json:"idempotencyKey"`
		}
		if platform.DecodeJSON(r, &input) != nil {
			platform.Problem(w, 400, "invalid_schedule", "Invalid schedule request")
			return
		}
		item, err := g.scheduleClient.SubmitScheduleRequest(ctx, &schedulingv1.SubmitScheduleRequestRequest{Proposed: &schedulingv1.ClassSession{StudioId: input.StudioID, RoomId: input.RoomID, Title: input.Title, Description: input.Description, TimeRange: &commonv1.TimeRange{StartsAt: timestamppb.New(input.StartsAt), EndsAt: timestamppb.New(input.EndsAt)}, Capacity: input.Capacity}, Audit: audit(input.IdempotencyKey, input.Reason)})
		if err != nil {
			grpcProblem(w, err)
			return
		}
		platform.JSON(w, http.StatusCreated, sessionJSON(item))
	case "POST /v1/class-sessions/{classSessionId}/approve":
		var input struct {
			Approve         *bool  `json:"approve"`
			MinimumStudents int32  `json:"minimumStudents"`
			Reason          string `json:"reason"`
			IdempotencyKey  string `json:"idempotencyKey"`
		}
		if platform.DecodeJSON(r, &input) != nil {
			platform.Problem(w, 400, "invalid_review", "Invalid review request")
			return
		}
		approve := true
		if input.Approve != nil {
			approve = *input.Approve
		}
		item, err := g.scheduleClient.ReviewScheduleRequest(ctx, &schedulingv1.ReviewScheduleRequestRequest{ClassSessionId: r.PathValue("classSessionId"), Approve: approve, MinimumStudents: input.MinimumStudents, Audit: audit(input.IdempotencyKey, input.Reason)})
		if err != nil {
			grpcProblem(w, err)
			return
		}
		platform.JSON(w, http.StatusOK, sessionJSON(item))
	case "POST /v1/class-sessions/{classSessionId}/bookings":
		var input struct {
			IdempotencyKey string `json:"idempotencyKey"`
		}
		if platform.DecodeJSON(r, &input) != nil {
			platform.Problem(w, 400, "invalid_booking", "Invalid booking request")
			return
		}
		item, err := g.scheduleClient.BookClass(ctx, &schedulingv1.BookClassRequest{ClassSessionId: r.PathValue("classSessionId"), Audit: audit(input.IdempotencyKey, "")})
		if err != nil {
			grpcProblem(w, err)
			return
		}
		platform.JSON(w, http.StatusCreated, bookingJSON(item))
	case "POST /v1/class-sessions/{classSessionId}/walk-ins":
		var input struct {
			StudentID      string `json:"studentId"`
			Reason         string `json:"reason"`
			IdempotencyKey string `json:"idempotencyKey"`
		}
		if platform.DecodeJSON(r, &input) != nil {
			platform.Problem(w, 400, "invalid_walk_in", "Invalid walk-in request")
			return
		}
		item, err := g.scheduleClient.AddWalkInRedemption(ctx, &schedulingv1.AddWalkInRedemptionRequest{ClassSessionId: r.PathValue("classSessionId"), StudentId: input.StudentID, Audit: audit(input.IdempotencyKey, input.Reason)})
		if err != nil {
			grpcProblem(w, err)
			return
		}
		platform.JSON(w, http.StatusCreated, bookingJSON(item))
	case "POST /v1/class-sessions/{classSessionId}/complete", "POST /v1/class-sessions/{classSessionId}/cancel":
		var input struct {
			Reason         string `json:"reason"`
			IdempotencyKey string `json:"idempotencyKey"`
		}
		if platform.DecodeJSON(r, &input) != nil {
			platform.Problem(w, 400, "invalid_class_command", "Invalid class command")
			return
		}
		var item *schedulingv1.ClassSession
		var err error
		if r.Pattern == "POST /v1/class-sessions/{classSessionId}/complete" {
			item, err = g.scheduleClient.ConfirmClassCompleted(ctx, &schedulingv1.ConfirmClassCompletedRequest{ClassSessionId: r.PathValue("classSessionId"), Audit: audit(input.IdempotencyKey, input.Reason)})
		} else {
			item, err = g.scheduleClient.CancelClassByStudio(ctx, &schedulingv1.CancelClassByStudioRequest{ClassSessionId: r.PathValue("classSessionId"), Audit: audit(input.IdempotencyKey, input.Reason)})
		}
		if err != nil {
			grpcProblem(w, err)
			return
		}
		platform.JSON(w, http.StatusOK, sessionJSON(item))
	case "PATCH /v1/class-sessions/{classSessionId}/video":
		var input struct {
			VideoURL       string `json:"videoUrl"`
			Reason         string `json:"reason"`
			IdempotencyKey string `json:"idempotencyKey"`
		}
		if platform.DecodeJSON(r, &input) != nil {
			platform.Problem(w, 400, "invalid_video_link", "Invalid class video link")
			return
		}
		item, err := g.scheduleClient.UpdateClassVideo(ctx, &schedulingv1.UpdateClassVideoRequest{ClassSessionId: r.PathValue("classSessionId"), VideoUrl: input.VideoURL, Audit: audit(input.IdempotencyKey, input.Reason)})
		if err != nil {
			grpcProblem(w, err)
			return
		}
		platform.JSON(w, http.StatusOK, sessionJSON(item))
	case "POST /v1/bookings/{bookingId}/cancel":
		var input struct {
			Reason         string `json:"reason"`
			IdempotencyKey string `json:"idempotencyKey"`
		}
		if platform.DecodeJSON(r, &input) != nil {
			platform.Problem(w, 400, "invalid_cancel", "Invalid cancellation")
			return
		}
		item, err := g.scheduleClient.CancelBooking(ctx, &schedulingv1.CancelBookingRequest{BookingId: r.PathValue("bookingId"), Audit: audit(input.IdempotencyKey, input.Reason)})
		if err != nil {
			grpcProblem(w, err)
			return
		}
		platform.JSON(w, http.StatusOK, bookingJSON(item))
	case "PATCH /v1/bookings/{bookingId}/attendance":
		var input struct {
			Attended       bool   `json:"attended"`
			Reason         string `json:"reason"`
			IdempotencyKey string `json:"idempotencyKey"`
		}
		if platform.DecodeJSON(r, &input) != nil {
			platform.Problem(w, 400, "invalid_attendance", "Invalid attendance correction")
			return
		}
		item, err := g.scheduleClient.CorrectAttendance(ctx, &schedulingv1.CorrectAttendanceRequest{BookingId: r.PathValue("bookingId"), Attended: input.Attended, Audit: audit(input.IdempotencyKey, input.Reason)})
		if err != nil {
			grpcProblem(w, err)
			return
		}
		platform.JSON(w, http.StatusOK, bookingJSON(item))
	case "POST /v1/bookings/{bookingId}/reverse":
		var input struct {
			Reason         string `json:"reason"`
			IdempotencyKey string `json:"idempotencyKey"`
		}
		if platform.DecodeJSON(r, &input) != nil {
			platform.Problem(w, 400, "invalid_reversal", "Invalid reversal")
			return
		}
		item, err := g.scheduleClient.ReverseClassRedemption(ctx, &schedulingv1.ReverseClassRedemptionRequest{BookingId: r.PathValue("bookingId"), Audit: audit(input.IdempotencyKey, input.Reason)})
		if err != nil {
			grpcProblem(w, err)
			return
		}
		platform.JSON(w, http.StatusOK, bookingJSON(item))
	case "POST /v1/room-reservations":
		var input struct {
			StudioID       string    `json:"studioId"`
			RoomID         string    `json:"roomId"`
			StartsAt       time.Time `json:"startsAt"`
			EndsAt         time.Time `json:"endsAt"`
			Reason         string    `json:"reason"`
			IdempotencyKey string    `json:"idempotencyKey"`
		}
		if platform.DecodeJSON(r, &input) != nil {
			platform.Problem(w, 400, "invalid_room_hold", "Invalid room hold")
			return
		}
		item, err := g.scheduleClient.CreateRoomHold(ctx, &schedulingv1.CreateRoomHoldRequest{StudioId: input.StudioID, RoomId: input.RoomID, TimeRange: &commonv1.TimeRange{StartsAt: timestamppb.New(input.StartsAt), EndsAt: timestamppb.New(input.EndsAt)}, Audit: audit(input.IdempotencyKey, input.Reason)})
		if err != nil {
			grpcProblem(w, err)
			return
		}
		platform.JSON(w, http.StatusCreated, roomReservationJSON(item))
	}
}
