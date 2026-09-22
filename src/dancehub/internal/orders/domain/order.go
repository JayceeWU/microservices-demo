package domain

import (
	"fmt"
	"strings"
	"time"
)

type Status string

const (
	Draft            Status = "DRAFT"
	PendingPayment   Status = "PENDING_PAYMENT"
	Paid             Status = "PAID"
	PaidNotFulfilled Status = "PAID_NOT_FULFILLED"
	Fulfilled        Status = "FULFILLED"
	PaymentFailed    Status = "PAYMENT_FAILED"
	Expired          Status = "EXPIRED"
	RefundPending    Status = "REFUND_PENDING"
	Refunded         Status = "REFUNDED"
)

type IssuerScope string

const (
	StudioScope   IssuerScope = "STUDIO"
	PlatformScope IssuerScope = "PLATFORM"
)

type OrderLine interface {
	Price() Money
	Quantity() int32
}
type CreditProductLine struct {
	ProductVersionID string
	Description      string
	UnitPrice        Money
	Count            int32
	IsFinalSale      bool
	Scope            IssuerScope
	StudioID         string
}

func (l CreditProductLine) Price() Money    { return l.UnitPrice }
func (l CreditProductLine) Quantity() int32 { return l.Count }

type RoomReservationLine struct {
	ReservationID string
	Description   string
	UnitPrice     Money
}

func (l RoomReservationLine) Price() Money  { return l.UnitPrice }
func (RoomReservationLine) Quantity() int32 { return 1 }

type Order struct {
	id, userID, studioID string
	scope                IssuerScope
	status               Status
	lines                []OrderLine
	total                Money
	paymentExpiresAt     time.Time
}
type Snapshot struct {
	ID, UserID, StudioID string
	Scope                IssuerScope
	Status               Status
	Lines                []OrderLine
	Total                Money
	PaymentExpiresAt     time.Time
}

func (o *Order) Snapshot() Snapshot {
	return Snapshot{ID: o.id, UserID: o.userID, StudioID: o.studioID, Scope: o.scope, Status: o.status, Lines: append([]OrderLine(nil), o.lines...), Total: o.total, PaymentExpiresAt: o.paymentExpiresAt}
}

type OrderFactory struct{}

func (OrderFactory) Membership(id, user string, lines []CreditProductLine, expires time.Time) (*Order, error) {
	if len(lines) == 0 {
		return nil, fmt.Errorf("%w: lines required", ErrInvalidOrder)
	}
	scope, studio := lines[0].Scope, lines[0].StudioID
	items := make([]OrderLine, 0, len(lines))
	for _, line := range lines {
		if line.Count < 1 || line.Scope != scope || line.StudioID != studio {
			return nil, fmt.Errorf("%w: membership lines must share issuer", ErrInvalidOrder)
		}
		items = append(items, line)
	}
	return newOrder(id, user, studio, scope, items, expires)
}
func (OrderFactory) Room(id, user, studio string, line RoomReservationLine, expires time.Time) (*Order, error) {
	if strings.TrimSpace(line.ReservationID) == "" {
		return nil, fmt.Errorf("%w: reservation required", ErrInvalidOrder)
	}
	return newOrder(id, user, studio, StudioScope, []OrderLine{line}, expires)
}
func newOrder(id, user, studio string, scope IssuerScope, lines []OrderLine, expires time.Time) (*Order, error) {
	total, _ := NewMoney(0)
	for _, line := range lines {
		extended, err := line.Price().Multiply(line.Quantity())
		if err != nil {
			return nil, err
		}
		total, err = total.Add(extended)
		if err != nil {
			return nil, err
		}
	}
	return &Order{id: id, userID: user, studioID: studio, scope: scope, status: PendingPayment, lines: lines, total: total, paymentExpiresAt: expires}, nil
}
func Rehydrate(v Snapshot) *Order {
	return &Order{id: v.ID, userID: v.UserID, studioID: v.StudioID, scope: v.Scope, status: v.Status, lines: append([]OrderLine(nil), v.Lines...), total: v.Total, paymentExpiresAt: v.PaymentExpiresAt}
}
func (o *Order) PaymentSucceeded() error {
	return o.transition([]Status{PendingPayment, PaymentFailed, Expired}, Paid)
}
func (o *Order) PaymentFailed() error { return o.transition([]Status{PendingPayment}, PaymentFailed) }
func (o *Order) FulfillmentFailed() error {
	return o.transition([]Status{Paid, PaidNotFulfilled}, PaidNotFulfilled)
}
func (o *Order) Fulfill() error       { return o.transition([]Status{Paid, PaidNotFulfilled}, Fulfilled) }
func (o *Order) RequestRefund() error { return o.transition([]Status{Fulfilled}, RefundPending) }
func (o *Order) RejectRefund() error  { return o.transition([]Status{RefundPending}, Fulfilled) }
func (o *Order) MarkRefunded() error  { return o.transition([]Status{RefundPending}, Refunded) }
func (o *Order) MarkExternallyRefunded() error {
	return o.transition([]Status{Fulfilled, RefundPending}, Refunded)
}
func (o *Order) transition(from []Status, to Status) error {
	if o.status == to {
		return nil
	}
	for _, candidate := range from {
		if o.status == candidate {
			o.status = to
			return nil
		}
	}
	return fmt.Errorf("%w: %s to %s", ErrInvalidTransition, o.status, to)
}

func (o *Order) BeginCompensation() error {
	return o.transition([]Status{PendingPayment, PaymentFailed, Expired, Paid, PaidNotFulfilled, Fulfilled}, RefundPending)
}
func (o *Order) DenyRefund() error { return o.transition([]Status{RefundPending}, Fulfilled) }
