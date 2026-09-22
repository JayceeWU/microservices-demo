package postgres

import (
	"context"
	"errors"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/orders/application"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/orders/domain"
	"github.com/jackc/pgx/v5"
	"time"
)

type refundRepository struct{ tx pgx.Tx }

func (r repositories) Refunds() application.RefundRepository { return refundRepository{r.tx} }

func (r refundRepository) Begin(ctx context.Context, record *application.OrderRecord, c application.RefundCommand, decision, kind string, system bool) error {
	var id, key, phase, previous string
	err := r.tx.QueryRow(ctx, `SELECT id::text,request_key,phase,decision FROM orders.refund_attempts WHERE order_id=$1 AND (request_key=$2 OR phase NOT IN('COMPLETED','REJECTED','DENIED')) ORDER BY created_at DESC LIMIT 1 FOR UPDATE`, c.OrderID, c.Audit.IdempotencyKey).Scan(&id, &key, &phase, &previous)
	if err == nil {
		if key == c.Audit.IdempotencyKey || decision == "REQUEST" || previous == decision {
			return nil
		}
		if decision == "APPROVE" && previous == "REQUEST" && (phase == "RESERVING" || phase == "AWAITING_APPROVAL") {
			_, err = r.tx.Exec(ctx, `UPDATE orders.refund_attempts SET decision='APPROVE',decision_key=$2,approved_by=NULLIF($3,'')::uuid,phase=CASE WHEN phase='AWAITING_APPROVAL' THEN 'REFUNDING' ELSE phase END,next_attempt_at=now() WHERE id=$1`, id, c.Audit.IdempotencyKey, c.Actor.UserID)
			return err
		}
		if decision == "REJECT" && previous == "REQUEST" && phase == "AWAITING_APPROVAL" {
			_, err = r.tx.Exec(ctx, `UPDATE orders.refund_attempts SET decision='REJECT',decision_key=$2,phase='RELEASING',next_attempt_at=now() WHERE id=$1`, id, c.Audit.IdempotencyKey)
			return err
		}
		return application.Conflict("refund decision is already in progress")
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if record.View().Status == domain.Refunded && decision == "APPROVE" {
		return nil
	}
	if decision == "REJECT" {
		return application.Conflict("no refund is awaiting approval")
	}
	before := record.Aggregate.Snapshot().Status
	if system {
		err = record.Aggregate.BeginCompensation()
	} else {
		err = record.Aggregate.RequestRefund()
	}
	if err != nil {
		return application.Conflict(err.Error())
	}
	if _, err = (orderRepository{r.tx}).Save(ctx, record, before); err != nil {
		return err
	}
	phase = "RESERVING"
	if kind != "CREDIT" {
		phase = "REFUNDING"
	}
	_, err = r.tx.Exec(ctx, `INSERT INTO orders.refund_attempts(order_id,request_key,requested_by,approved_by,decision,kind,phase,reason) VALUES($1,$2,NULLIF($3,'')::uuid,CASE WHEN $4='APPROVE' THEN NULLIF($3,'')::uuid ELSE NULL END,$4,$5,$6,$7)`, c.OrderID, c.Audit.IdempotencyKey, c.Actor.UserID, decision, kind, phase, c.Audit.Reason)
	return err
}

func (r refundRepository) Claim(ctx context.Context) (*application.RefundAttempt, error) {
	var j application.RefundAttempt
	err := r.tx.QueryRow(ctx, `WITH due AS(SELECT id FROM orders.refund_attempts WHERE phase IN('RESERVING','REFUNDING','REVOKING','RELEASING') AND next_attempt_at<=now() AND (lease_until IS NULL OR lease_until<now()) ORDER BY next_attempt_at FOR UPDATE SKIP LOCKED LIMIT 1) UPDATE orders.refund_attempts a SET lease_until=now()+interval '30 seconds',lease_token=gen_random_uuid(),attempt_count=attempt_count+1 FROM due WHERE a.id=due.id RETURNING a.id::text,a.order_id::text,a.kind,a.phase,a.decision,a.reason,a.lease_token::text,a.attempt_count`).Scan(&j.ID, &j.OrderID, &j.Kind, &j.Phase, &j.Decision, &j.Reason, &j.LeaseToken, &j.Attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &j, err
}

func (r refundRepository) Advance(ctx context.Context, j *application.RefundAttempt, phase, message string) error {
	if _, err := (orderRepository{r.tx}).Get(ctx, j.OrderID, true); err != nil {
		return err
	}
	tag, err := r.tx.Exec(ctx, `UPDATE orders.refund_attempts SET phase=CASE WHEN $3='AWAITING_APPROVAL' AND decision='APPROVE' THEN 'REFUNDING' ELSE $3 END,last_error=$4,lease_until=NULL,lease_token=NULL,next_attempt_at=CASE WHEN $4='' THEN now() ELSE now()+make_interval(secs=>LEAST(60,power(2,LEAST(attempt_count-1,6)))::int) END,updated_at=now() WHERE id=$1 AND lease_token=$2::uuid`, j.ID, j.LeaseToken, phase, message)
	if err != nil || tag.RowsAffected() == 0 {
		return err
	}
	target := ""
	switch phase {
	case "COMPLETED":
		target = "REFUNDED"
	case "REJECTED", "DENIED":
		target = "FULFILLED"
	}
	if target != "" {
		_, err = r.tx.Exec(ctx, `UPDATE orders.orders SET status=$2::orders.order_status,updated_at=now() WHERE id=$1 AND status='REFUND_PENDING'`, j.OrderID, target)
	}
	return err
}
func (r refundRepository) ExpirePending(ctx context.Context) error {
	_, err := r.tx.Exec(ctx, `UPDATE orders.orders SET status='EXPIRED',updated_at=now() WHERE status IN('PENDING_PAYMENT','PAYMENT_FAILED') AND payment_expires_at<=now()`)
	return err
}
func (r refundRepository) SetPaidAt(ctx context.Context, id string, at time.Time) error {
	_, err := r.tx.Exec(ctx, `UPDATE orders.orders SET paid_at=COALESCE(paid_at,$2) WHERE id=$1`, id, at)
	return err
}
