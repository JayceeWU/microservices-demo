package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"time"

	"github.com/JayceeWU/microservices-demo/dancehub/internal/scheduling/application"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/scheduling/domain"
	"github.com/google/uuid"
)

// A cursor is a position in one query, never an authorization credential. Every
// page still runs through the application authorization and transaction RLS.
type pageCursor struct {
	Version int       `json:"v"`
	Query   string    `json:"q"`
	At      time.Time `json:"at"`
	ID      string    `json:"id"`
}

func queryFingerprint(values ...string) string {
	encoded, _ := json.Marshal(values)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func readPage(page application.PageRequest, fingerprint string) (int32, *pageCursor, error) {
	if page.Size < 0 {
		return 0, nil, application.Invalid("page_size must not be negative")
	}
	size := page.Size
	if size == 0 {
		size = 24
	}
	if size > 100 {
		size = 100
	}
	if page.Token == "" {
		return size, nil, nil
	}
	if len(page.Token) > 2048 {
		return 0, nil, application.Invalid("invalid page_token")
	}
	data, err := base64.RawURLEncoding.DecodeString(page.Token)
	if err != nil {
		return 0, nil, application.Invalid("invalid page_token")
	}
	var cursor pageCursor
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&cursor) != nil || decoder.Decode(new(any)) != io.EOF || cursor.Version != 1 || cursor.Query != fingerprint || cursor.At.IsZero() {
		return 0, nil, application.Invalid("page_token does not match this query")
	}
	id, err := uuid.Parse(cursor.ID)
	if err != nil || id == uuid.Nil || id.String() != cursor.ID {
		return 0, nil, application.Invalid("invalid page_token position")
	}
	return size, &cursor, nil
}

func nextPage(fingerprint string, at time.Time, id string) string {
	data, _ := json.Marshal(pageCursor{Version: 1, Query: fingerprint, At: at.UTC(), ID: id})
	return base64.RawURLEncoding.EncodeToString(data)
}

func position(cursor *pageCursor) (any, any) {
	if cursor == nil {
		return nil, nil
	}
	return cursor.At, cursor.ID
}

func (r sessionRepository) Search(ctx context.Context, query application.SearchSessionsQuery) (application.PageResult[application.SessionView], error) {
	result := application.PageResult[application.SessionView]{Items: []application.SessionView{}}
	bookable := query.BookableOnly != nil && *query.BookableOnly
	filter := "all"
	if bookable {
		filter = "bookable"
	}
	fingerprint := queryFingerprint("sessions", query.StudioID, query.TeacherID, query.ViewerID, query.TenantID, filter)
	size, cursor, err := readPage(query.Page, fingerprint)
	if err != nil {
		return result, err
	}
	at, id := position(cursor)
	rows, err := r.tx.Query(ctx, `SELECT `+sessionColumns+` FROM scheduling.class_sessions
		WHERE ($1='' OR studio_id=NULLIF($1,'')::uuid) AND ($2='' OR teacher_id=NULLIF($2,'')::uuid)
		AND (NOT $3::boolean OR (status IN ('OPEN','MINIMUM_CONFIRMED') AND starts_at>now() AND confirmed_count<capacity
			AND NOT EXISTS (SELECT 1 FROM scheduling.bookings b WHERE b.class_session_id=scheduling.class_sessions.id
				AND b.student_id=NULLIF($4,'')::uuid AND b.status IN ('PENDING_CREDIT','CONFIRMED','CHARGED','ATTENDED','NO_SHOW'))))
		AND ($5::timestamptz IS NULL OR (starts_at,id)>($5::timestamptz,$6::uuid))
		ORDER BY starts_at,id LIMIT $7`, query.StudioID, query.TeacherID, bookable, query.ViewerID, at, id, size+1)
	if err != nil {
		return result, translate(err, "unable to load class sessions")
	}
	defer rows.Close()
	for rows.Next() {
		record, err := scanSession(rows)
		if err != nil {
			return result, err
		}
		result.Items = append(result.Items, record.View())
	}
	if err := rows.Err(); err != nil {
		return result, translate(err, "unable to load class sessions")
	}
	if len(result.Items) > int(size) {
		result.Items = result.Items[:size]
		last := result.Items[len(result.Items)-1]
		result.NextPageToken = nextPage(fingerprint, last.StartsAt, last.ID)
	}
	return result, nil
}

func (r bookingRepository) ListByStudent(ctx context.Context, user domain.UserID, page application.PageRequest) (application.PageResult[application.BookingView], error) {
	return r.bookingPage(ctx, "bookings", string(user), page, `student_id=$1 AND ($2::timestamptz IS NULL OR (created_at,id)<($2::timestamptz,$3::uuid)) ORDER BY created_at DESC,id DESC`)
}

func (r bookingRepository) ListBySession(ctx context.Context, session domain.ClassSessionID, page application.PageRequest) (application.PageResult[application.BookingView], error) {
	return r.bookingPage(ctx, "roster", string(session), page, `class_session_id=$1 AND ($2::timestamptz IS NULL OR (created_at,id)>($2::timestamptz,$3::uuid)) ORDER BY created_at,id`)
}

type bookingWithTime struct {
	scanner
	createdAt *time.Time
}

func (r bookingWithTime) Scan(values ...any) error {
	return r.scanner.Scan(append(values, r.createdAt)...)
}

func (r bookingRepository) bookingPage(ctx context.Context, kind, parent string, page application.PageRequest, condition string) (application.PageResult[application.BookingView], error) {
	result := application.PageResult[application.BookingView]{Items: []application.BookingView{}}
	fingerprint := queryFingerprint(kind, parent)
	size, cursor, err := readPage(page, fingerprint)
	if err != nil {
		return result, err
	}
	at, id := position(cursor)
	rows, err := r.tx.Query(ctx, `SELECT `+bookingColumns+`,created_at FROM scheduling.bookings WHERE `+condition+` LIMIT $4`, parent, at, id, size+1)
	if err != nil {
		return result, translate(err, "unable to load bookings")
	}
	defer rows.Close()
	var lastTime time.Time
	for rows.Next() {
		var createdAt time.Time
		record, err := scanBooking(bookingWithTime{scanner: rows, createdAt: &createdAt})
		if err != nil {
			return result, err
		}
		if len(result.Items) == int(size) {
			result.NextPageToken = nextPage(fingerprint, lastTime, result.Items[len(result.Items)-1].ID)
			break
		}
		result.Items = append(result.Items, record.View())
		lastTime = createdAt
	}
	return result, translate(rows.Err(), "unable to load bookings")
}
