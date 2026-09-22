package application

import (
	"context"
	"time"

	"github.com/JayceeWU/microservices-demo/dancehub/internal/appcore"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/orders/domain"
)

var (
	ErrUnauthenticated  = appcore.ErrUnauthenticated
	ErrPermissionDenied = appcore.ErrPermissionDenied
	ErrInvalidArgument  = appcore.ErrInvalidArgument
	ErrNotFound         = appcore.ErrNotFound
	ErrConflict         = appcore.ErrConflict
	ErrUnavailable      = appcore.ErrUnavailable
	Invalid             = appcore.Invalid
	Denied              = appcore.Denied
	NotFound            = appcore.NotFound
	Conflict            = appcore.Conflict
	Unavailable         = appcore.Unavailable
	NewAuditContext     = appcore.NewAuditContext
)

type ActorContext = appcore.ActorContext
type AuditContext = appcore.AuditContext

type OrderItemInput struct {
	ProductVersionID string
	Quantity         int32
}

type ProductSnapshot struct {
	ProductVersionID, StudioID, IssuerScope, Kind, Name string
	AmountCents                                         int64
	CreditAmount, ValidityDays                          int32
	FinalSale                                           bool
}

type FrozenLine struct {
	ProductVersionID  string `json:"productVersionId"`
	StudioID          string `json:"studioId"`
	IssuerScope       string `json:"issuerScope"`
	Kind              string `json:"kind"`
	Name              string `json:"name"`
	AmountCents       int64  `json:"amountCents"`
	CreditAmount      int32  `json:"creditAmount"`
	ValidityDays      int32  `json:"validityDays"`
	FinalSale         bool   `json:"finalSale"`
	RoomReservationID string `json:"roomReservationId,omitempty"`
}

type OrderLineView struct {
	ID, Type, ProductVersionID, RoomReservationID, Description string
	Quantity                                                   int32
	UnitAmountCents                                            int64
	FinalSale                                                  bool
	Snapshot                                                   FrozenLine
}

type RefundLineRequirement struct {
	OrderLineID string
	Quantity    int32
}

type OrderView struct {
	RefundPhase, RefundFailureReason string
	ID, UserID, StudioID             string
	Status                           domain.Status
	TotalCents                       int64
	PaymentExpiresAt                 time.Time
	Lines                            []OrderLineView
}

type OrderRecord struct {
	RefundPhase, RefundFailureReason string
	PaidAt                           time.Time
	Aggregate                        *domain.Order
	Lines                            []OrderLineView
}

func (r OrderRecord) View() OrderView {
	v := r.Aggregate.Snapshot()
	return OrderView{RefundPhase: r.RefundPhase, RefundFailureReason: r.RefundFailureReason, ID: v.ID, UserID: v.UserID, StudioID: v.StudioID, Status: v.Status, TotalCents: v.Total.Cents(), PaymentExpiresAt: v.PaymentExpiresAt, Lines: append([]OrderLineView(nil), r.Lines...)}
}

type RefundInfo struct {
	OrderID, OwnerID, StudioID  string
	Status                      domain.Status
	TotalCents                  int64
	LineIDs                     []string
	FinalSale, HasNonCreditLine bool
}

type RoomReservationSnapshot struct {
	ID, StudioID, StudentID         string
	StartsAt, EndsAt, HoldExpiresAt time.Time
	AmountCents                     int64
	Status                          string
}

type PaymentSnapshot struct {
	Status      string
	SucceededAt time.Time
	ID          string
	AmountCents int64
}

type CampaignSnapshot struct {
	ID, StudioID, ProductVersionID, Name       string
	Inventory, PerUserLimit, PaymentTTLMinutes int32
	StartsAt, EndsAt                           time.Time
	Active                                     bool
}

type FlashSaleStatus string

const (
	FlashSaleQueued       FlashSaleStatus = "QUEUED"
	FlashSaleOrderCreated FlashSaleStatus = "ORDER_CREATED"
	FlashSaleRejected     FlashSaleStatus = "REJECTED"
	FlashSaleExpired      FlashSaleStatus = "EXPIRED"
)

type FlashSaleRequestView struct {
	RequestID, CampaignID, OrderID string
	Status                         FlashSaleStatus
}
type FlashSaleMessage struct {
	RequestID, CampaignID, UserID, IdempotencyKey string
	Traceparent, Tracestate, Baggage              string
}

type Clock interface{ Now() time.Time }
type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now().UTC() }

type UnitOfWork interface {
	Do(context.Context, ActorContext, string, func(context.Context, Repositories) error) error
}
type Repositories interface {
	Orders() OrderRepository
	FlashSales() FlashSaleRepository
	Inbox() InboxRepository
}
type OrderRepository interface {
	FindByIdempotencyKey(context.Context, string, string) (*OrderRecord, error)
	Get(context.Context, string, bool) (*OrderRecord, error)
	Add(context.Context, *domain.Order, string, []FrozenLine) (*OrderRecord, error)
	Save(context.Context, *OrderRecord, domain.Status) (*OrderRecord, error)
	SaveRefundRequest(context.Context, *OrderRecord, domain.Status, AuditContext, string) (*OrderRecord, error)
	RefundInfo(context.Context, string) (RefundInfo, error)
	FindRoomOrder(context.Context, string) (string, error)
	ClaimFulfillment(context.Context) (*OrderRecord, error)
}
type FlashSaleRepository interface {
	GetRequest(context.Context, string) (FlashSaleRequestView, error)
	Allocate(context.Context, FlashSaleMessage, CampaignSnapshot, *domain.Order, FrozenLine) (*OrderRecord, error)
}
type InboxRepository interface {
	TryAdd(context.Context, string, string) (bool, error)
}
type CatalogPort interface {
	Product(context.Context, ActorContext, string) (ProductSnapshot, error)
	Campaign(context.Context, ActorContext, string) (CampaignSnapshot, error)
}
type SchedulingPort interface {
	Room(context.Context, ActorContext, string) (RoomReservationSnapshot, error)
	ConfirmRoom(context.Context, ActorContext, string, string, AuditContext) error
	ReleaseRoom(context.Context, ActorContext, string, AuditContext) error
}
type CreditPort interface {
	RefundEligibility(context.Context, ActorContext, string, string, []string, AuditContext) (bool, string, error)
	ReserveRefund(context.Context, ActorContext, string, string, []string, AuditContext) error
	ReleaseRefund(context.Context, ActorContext, string, string, []string, AuditContext) error
	RevokeRefunded(context.Context, ActorContext, string, string, []string, AuditContext) error
	Grant(context.Context, ActorContext, string, string, FrozenLine, AuditContext) (string, error)
}
type PaymentPort interface {
	GetByOrder(context.Context, ActorContext, string) (PaymentSnapshot, error)
	Refund(context.Context, ActorContext, string, int64, AuditContext) error
}
type FlashSaleAdmission interface {
	Reserve(context.Context, ActorContext, CampaignSnapshot, AuditContext) (string, error)
}
