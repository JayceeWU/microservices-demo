package postgres

import (
	"context"

	"github.com/JayceeWU/microservices-demo/dancehub/internal/scheduling/application"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/scheduling/domain"
)

func (r bookingRepository) FindByIdempotencyKey(ctx context.Context, studioID, key string) (*application.BookingRecord, error) {
	return scanBooking(r.tx.QueryRow(ctx, `SELECT `+bookingColumns+` FROM scheduling.bookings WHERE studio_id=$1 AND idempotency_key=$2`, studioID, key))
}

func (r bookingRepository) HasPendingCredits(ctx context.Context, sessionID domain.ClassSessionID) (bool, error) {
	var pending bool
	err := r.tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM scheduling.bookings WHERE class_session_id=$1 AND (status IN('PENDING_CREDIT','CONFIRMED') OR credit_compensation_pending)) OR EXISTS(SELECT 1 FROM scheduling.attendance_operations WHERE class_session_id=$1 AND phase='PENDING')`, sessionID).Scan(&pending)
	return pending, err
}
