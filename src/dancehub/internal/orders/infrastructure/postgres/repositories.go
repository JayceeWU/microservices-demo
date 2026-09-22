package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/JayceeWU/microservices-demo/dancehub/internal/orders/application"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/orders/domain"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/platform"
	"github.com/jackc/pgx/v5"
)

func NewRepositories(tx pgx.Tx) application.Repositories {
	return repositories{tx: tx}
}

type repositories struct{ tx pgx.Tx }

func (r repositories) Orders() application.OrderRepository { return orderRepository{tx: r.tx} }
func (r repositories) FlashSales() application.FlashSaleRepository {
	return flashSaleRepository{tx: r.tx}
}
func (r repositories) Inbox() application.InboxRepository { return inboxRepository{tx: r.tx} }

const orderColumns = `id::text,user_id::text,COALESCE(studio_id::text,''),status::text,total_amount_cents,COALESCE(payment_expires_at,'epoch'),issuer_scope::text,COALESCE(paid_at,'epoch'),COALESCE((SELECT a.phase FROM orders.refund_attempts a WHERE a.order_id=orders.orders.id ORDER BY a.created_at DESC LIMIT 1),''),COALESCE((SELECT CASE WHEN a.phase='DENIED' THEN 'This order is not eligible for a refund.' WHEN a.last_error<>'' THEN 'Refund processing will retry automatically.' ELSE '' END FROM orders.refund_attempts a WHERE a.order_id=orders.orders.id ORDER BY a.created_at DESC LIMIT 1),'')`

type orderRepository struct{ tx pgx.Tx }

func (r orderRepository) FindByIdempotencyKey(ctx context.Context, userID, key string) (*application.OrderRecord, error) {
	return r.load(ctx, `SELECT `+orderColumns+` FROM orders.orders WHERE user_id=$1 AND idempotency_key=$2`, userID, key)
}

func (r orderRepository) Get(ctx context.Context, id string, lock bool) (*application.OrderRecord, error) {
	query := `SELECT ` + orderColumns + ` FROM orders.orders WHERE id=$1`
	if lock {
		query += ` FOR UPDATE`
	}
	return r.load(ctx, query, id)
}

func (r orderRepository) load(ctx context.Context, query string, args ...any) (*application.OrderRecord, error) {
	var id, userID, studioID, state, scope string
	var total int64
	var expires, paidAt time.Time
	var refundPhase, refundError string
	if err := r.tx.QueryRow(ctx, query, args...).Scan(&id, &userID, &studioID, &state, &total, &expires, &scope, &paidAt, &refundPhase, &refundError); err != nil {
		return nil, translate(err, "order not found")
	}
	lines, domainLines, err := r.loadLines(ctx, id)
	if err != nil {
		return nil, err
	}
	money, err := domain.NewMoney(total)
	if err != nil {
		return nil, err
	}
	aggregate := domain.Rehydrate(domain.Snapshot{ID: id, UserID: userID, StudioID: studioID, Scope: domain.IssuerScope(scope), Status: domain.Status(state), Lines: domainLines, Total: money, PaymentExpiresAt: expires})
	return &application.OrderRecord{Aggregate: aggregate, Lines: lines, PaidAt: paidAt, RefundPhase: refundPhase, RefundFailureReason: refundError}, nil
}

func (r orderRepository) loadLines(ctx context.Context, orderID string) ([]application.OrderLineView, []domain.OrderLine, error) {
	rows, err := r.tx.Query(ctx, `SELECT id::text,line_type,COALESCE(product_version_id::text,''),COALESCE(room_reservation_id::text,''),description,quantity,unit_amount_cents,final_sale,snapshot FROM orders.order_lines WHERE order_id=$1 ORDER BY id`, orderID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	views := make([]application.OrderLineView, 0)
	domainLines := make([]domain.OrderLine, 0)
	for rows.Next() {
		var line application.OrderLineView
		var raw []byte
		if err = rows.Scan(&line.ID, &line.Type, &line.ProductVersionID, &line.RoomReservationID, &line.Description, &line.Quantity, &line.UnitAmountCents, &line.FinalSale, &raw); err != nil {
			return nil, nil, err
		}
		if err = json.Unmarshal(raw, &line.Snapshot); err != nil {
			return nil, nil, err
		}
		money, moneyErr := domain.NewMoney(line.UnitAmountCents)
		if moneyErr != nil {
			return nil, nil, moneyErr
		}
		if line.Type == "CREDIT_PRODUCT" {
			domainLines = append(domainLines, domain.CreditProductLine{ProductVersionID: line.ProductVersionID, Description: line.Description, UnitPrice: money, Count: line.Quantity, IsFinalSale: line.FinalSale, Scope: domain.IssuerScope(line.Snapshot.IssuerScope), StudioID: line.Snapshot.StudioID})
		} else if line.Type == "ROOM_RESERVATION" {
			domainLines = append(domainLines, domain.RoomReservationLine{ReservationID: line.RoomReservationID, Description: line.Description, UnitPrice: money})
		} else {
			return nil, nil, fmt.Errorf("unknown order line type %q", line.Type)
		}
		views = append(views, line)
	}
	return views, domainLines, rows.Err()
}

func (r orderRepository) Add(ctx context.Context, aggregate *domain.Order, key string, frozen []application.FrozenLine) (*application.OrderRecord, error) {
	v := aggregate.Snapshot()
	var id string
	err := r.tx.QueryRow(ctx, `INSERT INTO orders.orders(user_id,studio_id,issuer_scope,status,total_amount_cents,idempotency_key,payment_expires_at) VALUES($1,NULLIF($2,'')::uuid,$3,$4::orders.order_status,$5,$6,$7) ON CONFLICT(user_id,idempotency_key) DO NOTHING RETURNING id::text`, v.UserID, v.StudioID, v.Scope, v.Status, v.Total.Cents(), key, v.PaymentExpiresAt).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return r.FindByIdempotencyKey(ctx, v.UserID, key)
	}
	if err != nil {
		return nil, translate(err, "unable to create order")
	}
	if len(frozen) != len(v.Lines) {
		return nil, fmt.Errorf("frozen order line count mismatch")
	}
	for index, item := range v.Lines {
		raw, marshalErr := json.Marshal(frozen[index])
		if marshalErr != nil {
			return nil, marshalErr
		}
		switch line := item.(type) {
		case domain.CreditProductLine:
			_, err = r.tx.Exec(ctx, `INSERT INTO orders.order_lines(order_id,line_type,product_version_id,description,quantity,unit_amount_cents,final_sale,snapshot) VALUES($1,'CREDIT_PRODUCT',$2,$3,$4,$5,$6,$7)`, id, line.ProductVersionID, line.Description, line.Count, line.UnitPrice.Cents(), line.IsFinalSale, raw)
		case domain.RoomReservationLine:
			_, err = r.tx.Exec(ctx, `INSERT INTO orders.order_lines(order_id,line_type,room_reservation_id,description,quantity,unit_amount_cents,final_sale,snapshot) VALUES($1,'ROOM_RESERVATION',$2,$3,1,$4,false,$5)`, id, line.ReservationID, line.Description, line.UnitPrice.Cents(), raw)
		default:
			err = fmt.Errorf("unsupported order line type %T", item)
		}
		if err != nil {
			return nil, err
		}
	}
	return r.Get(ctx, id, false)
}

func (r orderRepository) Save(ctx context.Context, record *application.OrderRecord, expected domain.Status) (*application.OrderRecord, error) {
	v := record.Aggregate.Snapshot()
	tag, err := r.tx.Exec(ctx, `UPDATE orders.orders SET status=$2::orders.order_status,updated_at=now() WHERE id=$1 AND status=$3::orders.order_status`, v.ID, v.Status, expected)
	if err != nil {
		return nil, translate(err, "unable to update order")
	}
	if tag.RowsAffected() == 0 {
		return nil, application.Conflict("order changed concurrently")
	}
	return r.Get(ctx, v.ID, false)
}

func (r orderRepository) SaveRefundRequest(ctx context.Context, record *application.OrderRecord, expected domain.Status, audit application.AuditContext, actorID string) (*application.OrderRecord, error) {
	v := record.Aggregate.Snapshot()
	tag, err := r.tx.Exec(ctx, `UPDATE orders.orders SET status=$2::orders.order_status,refund_idempotency_key=$3,refund_reason=$4,refund_requested_by=$5,refund_requested_at=COALESCE(refund_requested_at,now()),updated_at=now() WHERE id=$1 AND status=$6::orders.order_status`, v.ID, v.Status, audit.IdempotencyKey, audit.Reason, actorID, expected)
	if err != nil {
		return nil, translate(err, "unable to request refund")
	}
	if tag.RowsAffected() == 0 {
		return nil, application.Conflict("order changed concurrently")
	}
	return r.Get(ctx, v.ID, false)
}

func (r orderRepository) RefundInfo(ctx context.Context, id string) (application.RefundInfo, error) {
	var result application.RefundInfo
	var state string
	err := r.tx.QueryRow(ctx, `SELECT o.id::text,o.user_id::text,COALESCE(o.studio_id::text,''),o.status::text,o.total_amount_cents,COALESCE(bool_or(l.final_sale),false),COALESCE(bool_or(l.line_type<>'CREDIT_PRODUCT'),false),array_agg(l.id::text ORDER BY l.id) FROM orders.orders o JOIN orders.order_lines l ON l.order_id=o.id WHERE o.id=$1 GROUP BY o.id`, id).Scan(&result.OrderID, &result.OwnerID, &result.StudioID, &state, &result.TotalCents, &result.FinalSale, &result.HasNonCreditLine, &result.LineIDs)
	result.Status = domain.Status(state)
	return result, translate(err, "order not found")
}

func (r orderRepository) FindRoomOrder(ctx context.Context, reservationID string) (string, error) {
	var id string
	err := r.tx.QueryRow(ctx, `SELECT o.id::text FROM orders.orders o JOIN orders.order_lines l ON l.order_id=o.id WHERE l.room_reservation_id=$1 AND o.status IN('FULFILLED','REFUND_PENDING','REFUNDED')`, reservationID).Scan(&id)
	return id, translate(err, "room order not found")
}

func (r orderRepository) ClaimFulfillment(ctx context.Context) (*application.OrderRecord, error) {
	var id string
	err := r.tx.QueryRow(ctx, `SELECT id::text FROM orders.orders WHERE status IN('PAID','PAID_NOT_FULFILLED') AND (fulfillment_attempted_at IS NULL OR fulfillment_attempted_at<now()-interval '30 seconds') ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1`).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	record, err := r.Get(ctx, id, false)
	if err != nil {
		return nil, err
	}
	before := record.Aggregate.Snapshot().Status
	if err = record.Aggregate.FulfillmentFailed(); err != nil {
		return nil, err
	}
	tag, err := r.tx.Exec(ctx, `UPDATE orders.orders SET status='PAID_NOT_FULFILLED',fulfillment_attempted_at=now(),fulfillment_attempt_count=fulfillment_attempt_count+1,updated_at=now() WHERE id=$1 AND status=$2::orders.order_status`, id, before)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, application.Conflict("order changed concurrently")
	}
	return r.Get(ctx, id, false)
}

type inboxRepository struct{ tx pgx.Tx }

func (r inboxRepository) TryAdd(ctx context.Context, eventID, consumer string) (bool, error) {
	tag, err := r.tx.Exec(ctx, `INSERT INTO orders.inbox_events(event_id,consumer) VALUES($1::uuid,$2) ON CONFLICT DO NOTHING`, eventID, consumer)
	return tag.RowsAffected() == 1, translate(err, "unable to record saga event")
}

type flashSaleRepository struct{ tx pgx.Tx }

func (r flashSaleRepository) GetRequest(ctx context.Context, id string) (application.FlashSaleRequestView, error) {
	var result application.FlashSaleRequestView
	var state string
	err := r.tx.QueryRow(ctx, `SELECT request_id,campaign_id,status,COALESCE(order_id::text,'') FROM orders.flashsale_requests WHERE request_id=$1`, id).Scan(&result.RequestID, &result.CampaignID, &state, &result.OrderID)
	result.Status = application.FlashSaleStatus(state)
	return result, translate(err, "flash sale request not found")
}

func (r flashSaleRepository) Allocate(ctx context.Context, message application.FlashSaleMessage, campaign application.CampaignSnapshot, aggregate *domain.Order, frozen application.FrozenLine) (*application.OrderRecord, error) {
	if _, err := r.tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "campaign:"+message.CampaignID); err != nil {
		return nil, err
	}
	existing, err := r.GetRequest(ctx, message.RequestID)
	if err == nil {
		if existing.OrderID == "" {
			return nil, nil
		}
		return (orderRepository{tx: r.tx}).Get(ctx, existing.OrderID, false)
	}
	if !errors.Is(err, application.ErrNotFound) {
		return nil, err
	}
	var allocated, userAllocated int
	if err = r.tx.QueryRow(ctx, `SELECT COALESCE(-sum(quantity),0)::int FROM orders.flashsale_inventory_ledger WHERE campaign_id=$1`, message.CampaignID).Scan(&allocated); err != nil {
		return nil, err
	}
	if err = r.tx.QueryRow(ctx, `SELECT COALESCE(-sum(quantity),0)::int FROM orders.flashsale_inventory_ledger WHERE campaign_id=$1 AND user_id=$2`, message.CampaignID, message.UserID).Scan(&userAllocated); err != nil {
		return nil, err
	}
	if allocated >= int(campaign.Inventory) || userAllocated >= int(campaign.PerUserLimit) {
		_, err = r.tx.Exec(ctx, `INSERT INTO orders.flashsale_requests(request_id,campaign_id,product_version_id,user_id,status,rejection_reason,idempotency_key) VALUES($1,$2,$3,$4,'REJECTED','postgres_limit_or_inventory',$5) ON CONFLICT DO NOTHING`, message.RequestID, message.CampaignID, campaign.ProductVersionID, message.UserID, message.IdempotencyKey)
		return nil, err
	}
	record, err := (orderRepository{tx: r.tx}).Add(ctx, aggregate, "flashsale:"+message.RequestID, []application.FrozenLine{frozen})
	if err != nil {
		return nil, err
	}
	orderID := record.View().ID
	if _, err = r.tx.Exec(ctx, `INSERT INTO orders.flashsale_requests(request_id,campaign_id,product_version_id,user_id,status,order_id,idempotency_key) VALUES($1,$2,$3,$4,'ORDER_CREATED',$5,$6) ON CONFLICT(request_id) DO UPDATE SET status='ORDER_CREATED',order_id=EXCLUDED.order_id,updated_at=now()`, message.RequestID, message.CampaignID, campaign.ProductVersionID, message.UserID, orderID, message.IdempotencyKey); err != nil {
		return nil, err
	}
	if _, err = r.tx.Exec(ctx, `INSERT INTO orders.flashsale_inventory_ledger(campaign_id,request_id,user_id,quantity,reason) VALUES($1,$2,$3,-1,'ALLOCATED') ON CONFLICT DO NOTHING`, message.CampaignID, message.RequestID, message.UserID); err != nil {
		return nil, err
	}
	return record, nil
}

func translate(err error, message string) error {
	return platform.TranslateDatabaseError(err, message, application.NotFound, application.Conflict)
}
