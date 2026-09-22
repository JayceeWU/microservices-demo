package postgres

import (
	"context"
	"errors"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/scheduling/application"
	"github.com/jackc/pgx/v5"
)

type compensationRepository struct{ tx pgx.Tx }

func (r repositories) Compensations() application.CompensationRepository {
	return compensationRepository{r.tx}
}
func (r compensationRepository) Enqueue(ctx context.Context, b *application.BookingRecord, reason string) error {
	v := b.Aggregate.Snapshot()
	_, err := r.tx.Exec(ctx, `INSERT INTO scheduling.credit_compensations(booking_id,user_id,studio_id,reason) VALUES($1,$2,$3,$4) ON CONFLICT(booking_id) DO NOTHING`, v.ID, v.StudentID, b.StudioID, reason)
	if err != nil {
		return err
	}
	_, err = r.tx.Exec(ctx, `UPDATE scheduling.bookings SET credit_compensation_pending=EXISTS(SELECT 1 FROM scheduling.credit_compensations c WHERE c.booking_id=$1 AND c.status='PENDING') WHERE id=$1`, v.ID)
	return err
}
func (r compensationRepository) Claim(ctx context.Context) (*application.CreditCompensation, error) {
	var j application.CreditCompensation
	err := r.tx.QueryRow(ctx, `WITH due AS(SELECT booking_id FROM scheduling.credit_compensations WHERE status='PENDING' AND next_attempt_at<=now() AND (lease_until IS NULL OR lease_until<now()) ORDER BY next_attempt_at FOR UPDATE SKIP LOCKED LIMIT 1) UPDATE scheduling.credit_compensations c SET lease_until=now()+interval '30 seconds',lease_token=gen_random_uuid(),attempt_count=attempt_count+1 FROM due WHERE c.booking_id=due.booking_id RETURNING c.booking_id::text,c.user_id::text,c.studio_id::text,c.reason,c.lease_token::text,c.attempt_count`).Scan(&j.BookingID, &j.UserID, &j.StudioID, &j.Reason, &j.LeaseToken, &j.Attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &j, err
}
func (r compensationRepository) Finish(ctx context.Context, j *application.CreditCompensation, message string) error {
	// Completion, cancellation and credit recovery all serialize class ->
	// booking -> operation, including clearing the final compensation flag.
	var sessionID string
	if err := r.tx.QueryRow(ctx, `SELECT class_session_id::text FROM scheduling.bookings WHERE id=$1`, j.BookingID).Scan(&sessionID); err != nil {
		return err
	}
	if _, err := r.tx.Exec(ctx, `SELECT id FROM scheduling.class_sessions WHERE id=$1 FOR UPDATE`, sessionID); err != nil {
		return err
	}
	if _, err := r.tx.Exec(ctx, `SELECT id FROM scheduling.bookings WHERE id=$1 FOR UPDATE`, j.BookingID); err != nil {
		return err
	}
	tag, err := r.tx.Exec(ctx, `UPDATE scheduling.credit_compensations SET status=CASE WHEN $3='' THEN 'DONE' ELSE 'PENDING' END,last_error=$3,lease_until=NULL,lease_token=NULL,next_attempt_at=now()+make_interval(secs=>LEAST(60,power(2,LEAST(attempt_count-1,6)))::int) WHERE booking_id=$1 AND lease_token=$2::uuid`, j.BookingID, j.LeaseToken, message)
	if err != nil || tag.RowsAffected() == 0 {
		return err
	}
	if message == "" {
		_, err = r.tx.Exec(ctx, `UPDATE scheduling.bookings SET credit_compensation_pending=false WHERE id=$1`, j.BookingID)
	}
	return err
}
