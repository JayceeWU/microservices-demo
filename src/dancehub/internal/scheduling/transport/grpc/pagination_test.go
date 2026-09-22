package grpctransport

import (
	"context"
	"testing"

	commonv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/common/v1"
	schedulingv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/scheduling/v1"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/scheduling/application"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/testsupport"
	"google.golang.org/grpc"
)

type pagedApplicationStub struct {
	schedulingApplication
	query     application.SearchSessionsQuery
	page      application.PageRequest
	sessionID string
}

func (s *pagedApplicationStub) SearchSessions(_ context.Context, _ application.ActorContext, q application.SearchSessionsQuery) (application.PageResult[application.SessionView], error) {
	s.query = q
	return application.PageResult[application.SessionView]{NextPageToken: "sessions-next"}, nil
}
func (s *pagedApplicationStub) ListMyBookings(_ context.Context, _ application.ActorContext, p application.PageRequest) (application.PageResult[application.BookingView], error) {
	s.page = p
	return application.PageResult[application.BookingView]{NextPageToken: "bookings-next"}, nil
}
func (s *pagedApplicationStub) GetRoster(_ context.Context, _ application.ActorContext, id string, p application.PageRequest) (application.PageResult[application.BookingView], error) {
	s.page, s.sessionID = p, id
	return application.PageResult[application.BookingView]{NextPageToken: "roster-next"}, nil
}

func TestPaginationAndOptionalBookableFilterSurviveGRPC(t *testing.T) {
	stub := &pagedApplicationStub{}
	connection := testsupport.GRPCConnection(t, nil, func(server *grpc.Server) { schedulingv1.RegisterSchedulingServiceServer(server, NewServer(stub)) })
	client := schedulingv1.NewSchedulingServiceClient(connection)
	ctx := context.Background()
	page := &commonv1.PageRequest{PageSize: 17, PageToken: "opaque-cursor"}
	for _, value := range []*bool{nil, boolPointer(false), boolPointer(true)} {
		response, err := client.SearchClassSessions(ctx, &schedulingv1.SearchClassSessionsRequest{StudioId: "studio", TeacherId: "teacher", Page: page, BookableOnly: value})
		if err != nil {
			t.Fatal(err)
		}
		if stub.query.Page.Token != page.PageToken || stub.query.Page.Size != 17 || response.GetPage().GetNextPageToken() != "sessions-next" {
			t.Fatalf("lost session pagination: %+v %v", stub.query, response)
		}
		if (value == nil) != (stub.query.BookableOnly == nil) || value != nil && *value != *stub.query.BookableOnly {
			t.Fatal("lost optional boolean presence/value")
		}
	}
	bookings, err := client.ListMyBookings(ctx, &schedulingv1.ListMyBookingsRequest{Page: page})
	if err != nil || stub.page.Token != page.PageToken || bookings.GetPage().GetNextPageToken() != "bookings-next" {
		t.Fatalf("lost bookings page: %v %v", bookings, err)
	}
	roster, err := client.GetRoster(ctx, &schedulingv1.GetRosterRequest{ClassSessionId: "session", Page: page})
	if err != nil || stub.sessionID != "session" || stub.page.Token != page.PageToken || roster.GetPage().GetNextPageToken() != "roster-next" {
		t.Fatalf("lost roster page: %v %v", roster, err)
	}
}

func boolPointer(value bool) *bool { return &value }
