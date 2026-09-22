package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/JayceeWU/microservices-demo/dancehub/internal/platform"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/scheduling/application"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/scheduling/domain"
	"github.com/jackc/pgx/v5"
)

func NewRepositories(tx pgx.Tx) application.Repositories {
	return repositories{tx: tx}
}

type repositories struct{ tx pgx.Tx }

func (r repositories) Sessions() application.ClassSessionRepository {
	return sessionRepository{tx: r.tx}
}
func (r repositories) Bookings() application.BookingRepository      { return bookingRepository{tx: r.tx} }
func (r repositories) Rooms() application.RoomReservationRepository { return roomRepository{tx: r.tx} }
func (r repositories) Outbox() application.OutboxPort               { return outboxRepository{tx: r.tx} }

type scanner interface{ Scan(...any) error }

const sessionColumns = `id::text,studio_id::text,room_id::text,teacher_id::text,title,description,starts_at,ends_at,capacity,minimum_students,credit_cost,confirmed_count,status::text,COALESCE(video_url,''),attendance_teacher_deadline,attendance_admin_deadline,COALESCE(cancelled_reason,''),COALESCE(completed_by::text,''),studio_timezone,payroll_fact_version`

func scanSession(row scanner) (*application.SessionRecord, error) {
	var id, studioID, roomID, teacherID, title, description, state, videoURL, cancelledReason, completedBy string
	var startsAt, endsAt, teacherDeadline, adminDeadline time.Time
	var capacity, minimum, creditCost, confirmed int32
	var timezone string
	var version int64
	if err := row.Scan(&id, &studioID, &roomID, &teacherID, &title, &description, &startsAt, &endsAt, &capacity, &minimum, &creditCost, &confirmed, &state, &videoURL, &teacherDeadline, &adminDeadline, &cancelledReason, &completedBy, &timezone, &version); err != nil {
		return nil, translate(err, "class session not found")
	}
	period, err := domain.RehydrateClassTimeRange(startsAt, endsAt)
	if err != nil {
		return nil, err
	}
	aggregate, err := domain.RehydrateClassSession(domain.ClassSessionSnapshot{
		ID: domain.ClassSessionID(id), StudioID: domain.StudioID(studioID), RoomID: roomID,
		TeacherID: domain.UserID(teacherID), Title: title, TimeRange: period,
		Capacity: domain.Capacity(capacity), Minimum: domain.MinimumStudents(minimum),
		CreditCost: domain.CreditCost(creditCost), Confirmed: confirmed, Status: domain.ClassStatus(state),
		CancelledReason: cancelledReason, CompletedBy: domain.UserID(completedBy),
	})
	if err != nil {
		return nil, err
	}
	return &application.SessionRecord{Aggregate: aggregate, Description: description, VideoURL: videoURL, TeacherDeadline: teacherDeadline, AdminDeadline: adminDeadline, StudioTimezone: timezone, PayrollFactVersion: version}, nil
}

type sessionRepository struct{ tx pgx.Tx }

func (r sessionRepository) Get(ctx context.Context, id domain.ClassSessionID, lock bool) (*application.SessionRecord, error) {
	query := `SELECT ` + sessionColumns + ` FROM scheduling.class_sessions WHERE id=$1`
	if lock {
		query += ` FOR UPDATE`
	}
	return scanSession(r.tx.QueryRow(ctx, query, id))
}

func (r sessionRepository) Add(ctx context.Context, aggregate *domain.ClassSession, description, timezone string) (*application.SessionRecord, error) {
	v := aggregate.Snapshot()
	query := `INSERT INTO scheduling.class_sessions(studio_id,room_id,teacher_id,title,description,starts_at,ends_at,capacity,minimum_students,credit_cost,status,cancellation_cutoff_at,attendance_teacher_deadline,attendance_admin_deadline,studio_timezone) VALUES($1,$2,$3,$4,$5,$6::timestamptz,$7::timestamptz,$8,$9,$10,$11::scheduling.class_status,$6::timestamptz-interval '4 hours',$7::timestamptz+interval '4 hours',$7::timestamptz+interval '24 hours',$12) RETURNING ` + sessionColumns
	return scanSession(r.tx.QueryRow(ctx, query, v.StudioID, v.RoomID, v.TeacherID, v.Title, description, v.TimeRange.StartsAt(), v.TimeRange.EndsAt(), v.Capacity, v.Minimum, v.CreditCost, v.Status, timezone))
}

func (r sessionRepository) Save(ctx context.Context, record *application.SessionRecord, expected domain.ClassStatus) (*application.SessionRecord, error) {
	v := record.Aggregate.Snapshot()
	query := `UPDATE scheduling.class_sessions SET minimum_students=$2,confirmed_count=$3,status=$4::scheduling.class_status,cancelled_reason=NULLIF($5,''),completed_by=NULLIF($6,'')::uuid,completed_at=CASE WHEN $4='COMPLETED' THEN COALESCE(completed_at,now()) ELSE completed_at END,payroll_fact_version=$8,updated_at=now() WHERE id=$1 AND status=$7::scheduling.class_status RETURNING ` + sessionColumns
	result, err := scanSession(r.tx.QueryRow(ctx, query, v.ID, v.Minimum, v.Confirmed, v.Status, v.CancelledReason, v.CompletedBy, expected, record.PayrollFactVersion))
	if errors.Is(err, application.ErrNotFound) {
		return nil, application.Conflict("class session changed concurrently")
	}
	return result, err
}

func (r sessionRepository) UpdateVideo(ctx context.Context, id domain.ClassSessionID, actorID, studioID string, admin bool, videoURL string) (*application.SessionRecord, error) {
	query := `UPDATE scheduling.class_sessions SET video_url=$5,updated_at=now() WHERE id=$1 AND (teacher_id=$2::uuid OR ($4 AND ($3='' OR studio_id::text=$3))) RETURNING ` + sessionColumns
	return scanSession(r.tx.QueryRow(ctx, query, id, actorID, studioID, admin, videoURL))
}

func (r sessionRepository) DueMinimumIDs(ctx context.Context, limit int) ([]domain.ClassSessionID, error) {
	rows, err := r.tx.Query(ctx, `SELECT id::text FROM scheduling.class_sessions WHERE status='OPEN' AND cancellation_cutoff_at<=now() ORDER BY starts_at LIMIT $1`, limit)
	return collectIDs[domain.ClassSessionID](rows, err)
}

func (r sessionRepository) DueEndingIDs(ctx context.Context, limit int) ([]domain.ClassSessionID, error) {
	rows, err := r.tx.Query(ctx, `SELECT id::text FROM scheduling.class_sessions WHERE status='MINIMUM_CONFIRMED' AND ends_at<=now() ORDER BY ends_at LIMIT $1`, limit)
	return collectIDs[domain.ClassSessionID](rows, err)
}

type bookingRepository struct{ tx pgx.Tx }

const bookingColumns = `id::text,studio_id::text,class_session_id::text,student_id::text,status::text,COALESCE(credit_hold_id::text,''),idempotency_key,is_walk_in,credit_compensation_pending`

func scanBooking(row scanner) (*application.BookingRecord, error) {
	var id, studioID, sessionID, studentID, state, holdID, key string
	var walkIn, pending bool
	if err := row.Scan(&id, &studioID, &sessionID, &studentID, &state, &holdID, &key, &walkIn, &pending); err != nil {
		return nil, translate(err, "booking not found")
	}
	return &application.BookingRecord{Aggregate: domain.RehydrateBooking(domain.BookingSnapshot{ID: domain.BookingID(id), SessionID: domain.ClassSessionID(sessionID), StudentID: domain.UserID(studentID), Status: domain.BookingStatus(state), HoldID: holdID, WalkIn: walkIn}), StudioID: studioID, IdempotencyKey: key, CreditCompensationPending: pending}, nil
}

func (r bookingRepository) Get(ctx context.Context, id domain.BookingID, lock bool) (*application.BookingRecord, error) {
	query := `SELECT ` + bookingColumns + ` FROM scheduling.bookings WHERE id=$1`
	if lock {
		query += ` FOR UPDATE`
	}
	return scanBooking(r.tx.QueryRow(ctx, query, id))
}

func (r bookingRepository) Add(ctx context.Context, studioID string, aggregate *domain.Booking, key string) (*application.BookingRecord, error) {
	v := aggregate.Snapshot()
	query := `WITH inserted AS (
		INSERT INTO scheduling.bookings(studio_id,class_session_id,student_id,status,idempotency_key,is_walk_in)
		VALUES($1,$2,$3,$4::scheduling.booking_status,$5,$6) ON CONFLICT(studio_id,idempotency_key) DO NOTHING
		RETURNING ` + bookingColumns + `,true AS created)
	SELECT * FROM inserted UNION ALL
	SELECT ` + bookingColumns + `,false AS created FROM scheduling.bookings WHERE studio_id=$1::uuid AND idempotency_key=$5 AND NOT EXISTS(SELECT 1 FROM inserted) LIMIT 1`
	var id, storedStudio, session, student, state, hold, storedKey string
	var walkIn, created, pending bool
	if err := r.tx.QueryRow(ctx, query, studioID, v.SessionID, v.StudentID, v.Status, key, v.WalkIn).Scan(&id, &storedStudio, &session, &student, &state, &hold, &storedKey, &walkIn, &pending, &created); err != nil {
		return nil, translate(err, "booking already exists")
	}
	if session != string(v.SessionID) || student != string(v.StudentID) || walkIn != v.WalkIn {
		return nil, application.Conflict("idempotency_key was already used for a different booking")
	}
	return &application.BookingRecord{Aggregate: domain.RehydrateBooking(domain.BookingSnapshot{ID: domain.BookingID(id), SessionID: domain.ClassSessionID(session), StudentID: domain.UserID(student), Status: domain.BookingStatus(state), HoldID: hold, WalkIn: walkIn}), StudioID: storedStudio, IdempotencyKey: storedKey, Created: created, CreditCompensationPending: pending}, nil
}

func (r bookingRepository) Save(ctx context.Context, record *application.BookingRecord, expected domain.BookingStatus) (*application.BookingRecord, error) {
	v := record.Aggregate.Snapshot()
	query := `UPDATE scheduling.bookings SET status=$2::scheduling.booking_status,credit_hold_id=NULLIF($3,'')::uuid,updated_at=now() WHERE id=$1 AND status=$4::scheduling.booking_status RETURNING ` + bookingColumns
	result, err := scanBooking(r.tx.QueryRow(ctx, query, v.ID, v.Status, v.HoldID, expected))
	if errors.Is(err, application.ErrNotFound) {
		return nil, application.Conflict("booking changed concurrently")
	}
	return result, err
}

func (r bookingRepository) Delete(ctx context.Context, id domain.BookingID) error {
	_, err := r.tx.Exec(ctx, `DELETE FROM scheduling.bookings WHERE id=$1 AND status='PENDING_CREDIT'`, id)
	return err
}

func (r bookingRepository) ListForSession(ctx context.Context, session domain.ClassSessionID, lock bool) ([]*application.BookingRecord, error) {
	query := `SELECT ` + bookingColumns + ` FROM scheduling.bookings WHERE class_session_id=$1 AND status IN('PENDING_CREDIT','CONFIRMED','CHARGED','ATTENDED','NO_SHOW') ORDER BY created_at`
	if lock {
		query += ` FOR UPDATE`
	}
	rows, err := r.tx.Query(ctx, query, session)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]*application.BookingRecord, 0)
	for rows.Next() {
		item, err := scanBooking(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r bookingRepository) CountPayrollEligible(ctx context.Context, session domain.ClassSessionID) (int, error) {
	var count int
	err := r.tx.QueryRow(ctx, `SELECT count(*) FROM scheduling.bookings WHERE class_session_id=$1 AND status IN('CHARGED','ATTENDED','NO_SHOW')`, session).Scan(&count)
	return count, err
}

func (r bookingRepository) RecordAttendance(ctx context.Context, audit application.AttendanceAudit) error {
	_, err := r.tx.Exec(ctx, `INSERT INTO scheduling.attendance_audit(studio_id,class_session_id,booking_id,student_id,actor_id,action,reason,before_status,after_status) VALUES($1,$2,$3,$4,$5,$6,$7,NULLIF($8,'')::scheduling.booking_status,$9::scheduling.booking_status)`, audit.StudioID, audit.SessionID, audit.BookingID, audit.StudentID, audit.ActorID, audit.Action, audit.Reason, audit.BeforeStatus, audit.AfterStatus)
	return err
}

func (r bookingRepository) ReconciliationIDs(ctx context.Context, limit int) ([]domain.BookingID, error) {
	rows, err := r.tx.Query(ctx, `SELECT b.id::text FROM scheduling.bookings b JOIN scheduling.class_sessions s ON s.id=b.class_session_id WHERE NOT b.is_walk_in AND b.updated_at<=now()-interval '30 seconds' AND (b.status='PENDING_CREDIT' OR (b.status='CONFIRMED' AND s.status IN('MINIMUM_CONFIRMED','AWAITING_ADMIN_CONFIRMATION','COMPLETED','CANCELLED')) OR (b.status IN('CHARGED','ATTENDED','NO_SHOW') AND s.status='CANCELLED')) ORDER BY b.updated_at LIMIT $1`, limit)
	return collectIDs[domain.BookingID](rows, err)
}

type roomRepository struct{ tx pgx.Tx }

const roomColumns = `id::text,studio_id::text,room_id::text,student_id::text,starts_at,ends_at,amount_cents,status::text,hold_expires_at,COALESCE(order_id::text,'')`

func scanRoom(row scanner) (*application.RoomReservationRecord, error) {
	var id, studioID, roomID, studentID, state, orderID string
	var startsAt, endsAt, expires time.Time
	var amount int64
	if err := row.Scan(&id, &studioID, &roomID, &studentID, &startsAt, &endsAt, &amount, &state, &expires, &orderID); err != nil {
		return nil, translate(err, "room reservation not found")
	}
	period, err := domain.RehydrateClassTimeRange(startsAt, endsAt)
	if err != nil {
		return nil, err
	}
	aggregate := domain.RehydrateRoomReservation(domain.RoomReservationSnapshot{ID: domain.RoomReservationID(id), TimeRange: period, Status: domain.RoomReservationStatus(state), HoldExpiresAt: expires, OrderID: orderID})
	return &application.RoomReservationRecord{Aggregate: aggregate, StudioID: studioID, RoomID: roomID, StudentID: studentID, AmountCents: amount}, nil
}

func (r roomRepository) Get(ctx context.Context, id domain.RoomReservationID, lock bool) (*application.RoomReservationRecord, error) {
	query := `SELECT ` + roomColumns + ` FROM scheduling.room_reservations WHERE id=$1`
	if lock {
		query += ` FOR UPDATE`
	}
	return scanRoom(r.tx.QueryRow(ctx, query, id))
}

func (r roomRepository) Add(ctx context.Context, record *application.RoomReservationRecord, key string) (*application.RoomReservationRecord, error) {
	v := record.Aggregate.Snapshot()
	query := `INSERT INTO scheduling.room_reservations(studio_id,room_id,student_id,starts_at,ends_at,amount_cents,status,hold_expires_at,idempotency_key) VALUES($1,$2,$3,$4,$5,$6,$7::scheduling.room_reservation_status,$8,$9) ON CONFLICT(studio_id,idempotency_key) DO UPDATE SET idempotency_key=EXCLUDED.idempotency_key RETURNING ` + roomColumns
	return scanRoom(r.tx.QueryRow(ctx, query, record.StudioID, record.RoomID, record.StudentID, v.TimeRange.StartsAt(), v.TimeRange.EndsAt(), record.AmountCents, v.Status, v.HoldExpiresAt, key))
}

func (r roomRepository) Save(ctx context.Context, record *application.RoomReservationRecord, expected domain.RoomReservationStatus) (*application.RoomReservationRecord, error) {
	v := record.Aggregate.Snapshot()
	query := `UPDATE scheduling.room_reservations SET status=$2::scheduling.room_reservation_status,order_id=NULLIF($3,'')::uuid WHERE id=$1 AND status=$4::scheduling.room_reservation_status RETURNING ` + roomColumns
	result, err := scanRoom(r.tx.QueryRow(ctx, query, v.ID, v.Status, v.OrderID, expected))
	if errors.Is(err, application.ErrNotFound) {
		return nil, application.Conflict("room reservation changed concurrently")
	}
	return result, err
}

func (r roomRepository) DueExpiredIDs(ctx context.Context, limit int) ([]domain.RoomReservationID, error) {
	rows, err := r.tx.Query(ctx, `SELECT id::text FROM scheduling.room_reservations WHERE status IN('HOLD','PENDING_PAYMENT') AND hold_expires_at<=now() ORDER BY hold_expires_at LIMIT $1`, limit)
	return collectIDs[domain.RoomReservationID](rows, err)
}

type outboxRepository struct{ tx pgx.Tx }

func (r outboxRepository) Append(ctx context.Context, event application.DomainEvent) error {
	return platform.AppendOutbox(ctx, r.tx, `INSERT INTO scheduling.outbox_events(event_type,aggregate_type,aggregate_id,tenant_id,payload,traceparent,tracestate,baggage) VALUES($1,$2,$3::uuid,NULLIF($4,'')::uuid,$5,NULLIF($6,''),NULLIF($7,''),NULLIF($8,''))`, event.Payload, event.Type, event.AggregateType, event.AggregateID, event.StudioID)
}

func translate(err error, message string) error {
	return platform.TranslateDatabaseError(err, message, application.NotFound, application.Conflict)
}

func collectIDs[T ~string](rows pgx.Rows, queryError error) ([]T, error) {
	if queryError != nil {
		return nil, queryError
	}
	defer rows.Close()
	result := make([]T, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		result = append(result, T(id))
	}
	return result, rows.Err()
}
