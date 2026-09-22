package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	commonv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/common/v1"
	schedulingv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/scheduling/v1"
	"google.golang.org/grpc"
)

type schedulingPaginationClient struct {
	schedulingv1.SchedulingServiceClient
	query     *schedulingv1.SearchClassSessionsRequest
	page      *commonv1.PageRequest
	sessionID string
}

func (s *schedulingPaginationClient) SearchClassSessions(_ context.Context, query *schedulingv1.SearchClassSessionsRequest, _ ...grpc.CallOption) (*schedulingv1.SearchClassSessionsResponse, error) {
	s.query, s.page = query, query.Page
	return &schedulingv1.SearchClassSessionsResponse{Page: &commonv1.PageResponse{NextPageToken: "next"}}, nil
}
func (s *schedulingPaginationClient) ListMyBookings(_ context.Context, query *schedulingv1.ListMyBookingsRequest, _ ...grpc.CallOption) (*schedulingv1.ListMyBookingsResponse, error) {
	s.page = query.Page
	return &schedulingv1.ListMyBookingsResponse{Page: &commonv1.PageResponse{NextPageToken: "next"}}, nil
}
func (s *schedulingPaginationClient) GetRoster(_ context.Context, query *schedulingv1.GetRosterRequest, _ ...grpc.CallOption) (*schedulingv1.GetRosterResponse, error) {
	s.page, s.sessionID = query.Page, query.ClassSessionId
	return &schedulingv1.GetRosterResponse{Page: &commonv1.PageResponse{NextPageToken: "next"}}, nil
}

func TestSchedulingHTTPPagination(t *testing.T) {
	for _, tc := range []struct {
		path     string
		size     int32
		bookable string
	}{
		{"/v1/class-sessions", 24, "absent"},
		{"/v1/class-sessions?bookable_only=false&page_size=0", 24, "false"},
		{"/v1/class-sessions?bookable_only=true&page_size=999", 100, "true"},
		{"/v1/bookings?page_size=3", 3, ""},
		{"/v1/class-sessions/session/roster?page_size=5", 5, ""},
	} {
		t.Run(tc.path, func(t *testing.T) {
			stub := &schedulingPaginationClient{}
			g := &gateway{scheduleClient: stub}
			mux := http.NewServeMux()
			for _, path := range []string{"GET /v1/class-sessions", "GET /v1/bookings", "GET /v1/class-sessions/{classSessionId}/roster"} {
				mux.HandleFunc(path, g.scheduling)
			}
			r := httptest.NewRequest("GET", tc.path, nil)
			query := r.URL.Query()
			query.Set("page_token", "opaque+/=")
			r.URL.RawQuery = query.Encode()
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			if w.Code != 200 || stub.page.PageSize != tc.size || stub.page.PageToken != "opaque+/=" {
				t.Fatalf("lost request page: %d %v", w.Code, stub.page)
			}
			var body map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body["nextPageToken"] != "next" {
				t.Fatalf("lost response cursor: %s", w.Body.String())
			}
			if tc.bookable == "absent" && stub.query.BookableOnly != nil {
				t.Fatal("absent filter became false")
			}
			if tc.bookable == "true" || tc.bookable == "false" {
				if stub.query.BookableOnly == nil || *stub.query.BookableOnly != (tc.bookable == "true") {
					t.Fatal("incorrect filter mapping")
				}
			}
		})
	}
}

func TestSchedulingRejectsInvalidPaginationBeforeRPC(t *testing.T) {
	for _, query := range []string{"page_size=-1", "page_size=1.5", "page_size=2147483648", "page_size=", "bookable_only=maybe", "bookable_only="} {
		g := &gateway{}
		r := httptest.NewRequest("GET", "/v1/class-sessions?"+query, nil)
		r.Pattern = "GET /v1/class-sessions"
		w := httptest.NewRecorder()
		g.scheduling(w, r)
		if w.Code != 400 {
			t.Fatalf("%s: got %d", query, w.Code)
		}
	}
}
