package postgres

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/JayceeWU/microservices-demo/dancehub/internal/scheduling/application"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/scheduling/domain"
)

func TestPageSizeAndCursorPosition(t *testing.T) {
	for _, tc := range []struct{ input, want int32 }{{0, 24}, {1, 1}, {24, 24}, {100, 100}, {101, 100}, {10000, 100}} {
		size, cursor, err := readPage(application.PageRequest{Size: tc.input}, "query")
		if err != nil || size != tc.want || cursor != nil {
			t.Fatalf("size %d: got %d, %v, %v", tc.input, size, cursor, err)
		}
	}
	at := time.Date(2030, 1, 2, 3, 4, 5, 123456000, time.FixedZone("offset", -8*3600))
	id := "10000000-0000-0000-0000-000000000001"
	token := nextPage("query", at, id)
	_, cursor, err := readPage(application.PageRequest{Token: token}, "query")
	if err != nil || !cursor.At.Equal(at) || cursor.ID != id {
		t.Fatalf("cursor lost the timestamp/UUID tie breaker: %+v %v", cursor, err)
	}
	if _, _, err := readPage(application.PageRequest{Token: token}, "other query"); !errors.Is(err, application.ErrInvalidArgument) {
		t.Fatalf("accepted another query cursor: %v", err)
	}
}

func TestInvalidPageTokensFailBeforeDatabaseAccess(t *testing.T) {
	valid := pageCursor{Version: 1, Query: queryFingerprint("bookings", "user"), At: time.Now(), ID: "10000000-0000-0000-0000-000000000001"}
	encode := func(cursor pageCursor) string {
		data, _ := json.Marshal(cursor)
		return base64.RawURLEncoding.EncodeToString(data)
	}
	tokens := []string{"not/base64", strings.Repeat("a", 2049), base64.RawURLEncoding.EncodeToString([]byte("{}"))}
	for _, mutate := range []func(*pageCursor){
		func(c *pageCursor) { c.Version = 2 }, func(c *pageCursor) { c.Query = "wrong" },
		func(c *pageCursor) { c.At = time.Time{} }, func(c *pageCursor) { c.ID = "not-a-uuid" },
		func(c *pageCursor) { c.ID = "00000000-0000-0000-0000-000000000000" },
	} {
		cursor := valid
		mutate(&cursor)
		tokens = append(tokens, encode(cursor))
	}
	data, _ := json.Marshal(valid)
	tokens = append(tokens, base64.RawURLEncoding.EncodeToString(append(data, []byte(` {}`)...)))
	tokens = append(tokens, base64.RawURLEncoding.EncodeToString([]byte(strings.TrimSuffix(string(data), "}")+`,"extra":true}`)))
	for _, token := range tokens {
		// A nil transaction is deliberate: validation must return before any SQL.
		_, err := (bookingRepository{}).ListByStudent(context.Background(), domain.UserID("user"), application.PageRequest{Token: token})
		if !errors.Is(err, application.ErrInvalidArgument) {
			t.Fatalf("accepted invalid token: %v", err)
		}
	}
	if _, _, err := readPage(application.PageRequest{Size: -1}, "query"); !errors.Is(err, application.ErrInvalidArgument) {
		t.Fatalf("accepted negative size: %v", err)
	}
}

func TestCursorIsBoundToUserClassAndSessionFilters(t *testing.T) {
	ctx := context.Background()
	at := time.Now()
	id := "10000000-0000-0000-0000-000000000001"
	token := nextPage(queryFingerprint("bookings", "user-a"), at, id)
	if _, err := (bookingRepository{}).ListByStudent(ctx, "user-b", application.PageRequest{Token: token}); !errors.Is(err, application.ErrInvalidArgument) {
		t.Fatal("accepted another user's cursor")
	}
	if _, err := (bookingRepository{}).ListBySession(ctx, "user-a", application.PageRequest{Token: token}); !errors.Is(err, application.ErrInvalidArgument) {
		t.Fatal("accepted a booking cursor as a roster cursor")
	}
	token = nextPage(queryFingerprint("roster", "class-a"), at, id)
	if _, err := (bookingRepository{}).ListBySession(ctx, "class-b", application.PageRequest{Token: token}); !errors.Is(err, application.ErrInvalidArgument) {
		t.Fatal("accepted another class cursor")
	}
	bookable := true
	query := application.SearchSessionsQuery{StudioID: "studio", TeacherID: "teacher", ViewerID: "viewer", TenantID: "tenant", BookableOnly: &bookable}
	query.Page.Token = nextPage(queryFingerprint("sessions", "studio", "teacher", "viewer", "tenant", "bookable"), at, id)
	for _, mutate := range []func(*application.SearchSessionsQuery){
		func(q *application.SearchSessionsQuery) { q.StudioID = "other" },
		func(q *application.SearchSessionsQuery) { q.TeacherID = "other" },
		func(q *application.SearchSessionsQuery) { q.ViewerID = "other" },
		func(q *application.SearchSessionsQuery) { q.TenantID = "other" },
		func(q *application.SearchSessionsQuery) { q.BookableOnly = nil },
	} {
		changed := query
		mutate(&changed)
		if _, err := (sessionRepository{}).Search(ctx, changed); !errors.Is(err, application.ErrInvalidArgument) {
			t.Fatalf("accepted changed filter: %v", err)
		}
	}
}
