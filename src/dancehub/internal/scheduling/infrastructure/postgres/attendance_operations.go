package postgres

import (
	"context"
	"errors"

	"github.com/JayceeWU/microservices-demo/dancehub/internal/scheduling/application"
	"github.com/jackc/pgx/v5"
)

type attendanceOperationRepository struct{ tx pgx.Tx }

func (r repositories) AttendanceOperations() application.AttendanceOperationRepository {
	return attendanceOperationRepository{r.tx}
}

const attendanceOperationColumns = `id::text,studio_id::text,class_session_id::text,booking_id::text,student_id::text,actor_id::text,action,idempotency_key,reason,phase,COALESCE(lease_token::text,''),last_error,attempt_count`

func scanAttendanceOperation(row scanner) (*application.AttendanceOperation, error) {
	var value application.AttendanceOperation
	err := row.Scan(&value.ID, &value.StudioID, &value.ClassSessionID, &value.BookingID, &value.StudentID, &value.ActorID, &value.Action, &value.IdempotencyKey, &value.Reason, &value.Phase, &value.LeaseToken, &value.LastError, &value.Attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &value, translate(err, "unable to load attendance operation")
}

func (r attendanceOperationRepository) FindByKey(ctx context.Context, studioID, key string) (*application.AttendanceOperation, error) {
	return scanAttendanceOperation(r.tx.QueryRow(ctx, `SELECT `+attendanceOperationColumns+` FROM scheduling.attendance_operations WHERE studio_id=$1 AND idempotency_key=$2`, studioID, key))
}

func (r attendanceOperationRepository) Add(ctx context.Context, v application.AttendanceOperation) (*application.AttendanceOperation, error) {
	// Class and booking locks serialize requests for one booking. The unique
	// keys additionally reject conflicting commands arriving through another class.
	return scanAttendanceOperation(r.tx.QueryRow(ctx, `INSERT INTO scheduling.attendance_operations(studio_id,class_session_id,booking_id,student_id,actor_id,action,idempotency_key,reason) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING `+attendanceOperationColumns, v.StudioID, v.ClassSessionID, v.BookingID, v.StudentID, v.ActorID, v.Action, v.IdempotencyKey, v.Reason))
}

func (r attendanceOperationRepository) Claim(ctx context.Context, id string) (*application.AttendanceOperation, error) {
	return scanAttendanceOperation(r.tx.QueryRow(ctx, `UPDATE scheduling.attendance_operations SET lease_until=now()+interval '30 seconds',lease_token=gen_random_uuid(),attempt_count=attempt_count+1,updated_at=now() WHERE id=(SELECT id FROM scheduling.attendance_operations WHERE phase='PENDING' AND ($1='' OR id::text=$1) AND next_attempt_at<=now() AND (lease_until IS NULL OR lease_until<now()) ORDER BY next_attempt_at,id FOR UPDATE SKIP LOCKED LIMIT 1) RETURNING `+attendanceOperationColumns, id))
}

func (r attendanceOperationRepository) OwnsClaim(ctx context.Context, operation *application.AttendanceOperation) (bool, error) {
	var value string
	err := r.tx.QueryRow(ctx, `SELECT COALESCE(lease_token::text,'') FROM scheduling.attendance_operations WHERE id=$1 AND phase='PENDING' FOR UPDATE`, operation.ID).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil && value == operation.LeaseToken, err
}

func (r attendanceOperationRepository) Finish(ctx context.Context, operation *application.AttendanceOperation, phase, message string) error {
	tag, err := r.tx.Exec(ctx, `UPDATE scheduling.attendance_operations SET phase=$3,last_error=$4,lease_until=NULL,lease_token=NULL,next_attempt_at=now()+make_interval(secs=>LEAST(60,power(2,LEAST(attempt_count-1,6)))::int),updated_at=now() WHERE id=$1 AND lease_token=$2::uuid AND phase='PENDING'`, operation.ID, operation.LeaseToken, phase, message)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return application.Unavailable("attendance operation lease changed", nil)
	}
	return nil
}
