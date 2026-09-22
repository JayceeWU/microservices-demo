package application

import (
	"context"
	"testing"
	"time"

	"github.com/JayceeWU/microservices-demo/dancehub/internal/orders/domain"
)

type memoryWork struct {
	repositories  Repositories
	inTransaction bool
}

func (w *memoryWork) Do(ctx context.Context, _ ActorContext, _ string, execute func(context.Context, Repositories) error) error {
	w.inTransaction = true
	defer func() { w.inTransaction = false }()
	return execute(ctx, w.repositories)
}

type memoryRepositories struct {
	orders *memoryOrders
	inbox  *memoryInbox
}

func (r memoryRepositories) Orders() OrderRepository       { return r.orders }
func (memoryRepositories) FlashSales() FlashSaleRepository { return unusedFlashSales{} }
func (r memoryRepositories) Inbox() InboxRepository        { return r.inbox }

type memoryOrders struct {
	record *OrderRecord
	key    string
	adds   int
	saves  int
}

func (r *memoryOrders) FindByIdempotencyKey(_ context.Context, _ string, key string) (*OrderRecord, error) {
	if r.record != nil && r.key == key {
		return r.record, nil
	}
	return nil, NotFound("order not found")
}
func (r *memoryOrders) Get(context.Context, string, bool) (*OrderRecord, error) {
	if r.record == nil {
		return nil, NotFound("order not found")
	}
	return r.record, nil
}
func (r *memoryOrders) Add(_ context.Context, aggregate *domain.Order, key string, frozen []FrozenLine) (*OrderRecord, error) {
	snapshot := aggregate.Snapshot()
	snapshot.ID = "order-1"
	lines := make([]OrderLineView, 0, len(snapshot.Lines))
	for index, line := range snapshot.Lines {
		credit := line.(domain.CreditProductLine)
		lines = append(lines, OrderLineView{ID: "line-1", Type: "CREDIT_PRODUCT", ProductVersionID: credit.ProductVersionID, Description: credit.Description, Quantity: credit.Count, UnitAmountCents: credit.UnitPrice.Cents(), FinalSale: credit.IsFinalSale, Snapshot: frozen[index]})
	}
	r.record = &OrderRecord{Aggregate: domain.Rehydrate(snapshot), Lines: lines}
	r.key, r.adds = key, r.adds+1
	return r.record, nil
}
func (r *memoryOrders) Save(_ context.Context, record *OrderRecord, _ domain.Status) (*OrderRecord, error) {
	r.record, r.saves = record, r.saves+1
	return record, nil
}
func (*memoryOrders) SaveRefundRequest(context.Context, *OrderRecord, domain.Status, AuditContext, string) (*OrderRecord, error) {
	panic("unused")
}
func (*memoryOrders) RefundInfo(context.Context, string) (RefundInfo, error) { panic("unused") }
func (*memoryOrders) FindRoomOrder(context.Context, string) (string, error)  { panic("unused") }
func (*memoryOrders) ClaimFulfillment(context.Context) (*OrderRecord, error) { panic("unused") }

type memoryInbox struct{ seen map[string]bool }

func (r *memoryInbox) TryAdd(_ context.Context, eventID, consumer string) (bool, error) {
	key := consumer + ":" + eventID
	if r.seen[key] {
		return false, nil
	}
	r.seen[key] = true
	return true, nil
}

type checkingCatalog struct {
	t     *testing.T
	work  *memoryWork
	calls int
}

func (c *checkingCatalog) Product(context.Context, ActorContext, string) (ProductSnapshot, error) {
	if c.work.inTransaction {
		c.t.Fatal("catalog lookup was called inside the database transaction")
	}
	c.calls++
	return ProductSnapshot{ProductVersionID: "product-1", StudioID: "studio-1", IssuerScope: "STUDIO", Kind: "CREDITS", Name: "Ten credits", AmountCents: 12000, CreditAmount: 10}, nil
}
func (*checkingCatalog) Campaign(context.Context, ActorContext, string) (CampaignSnapshot, error) {
	panic("unused")
}

type unusedFlashSales struct{}

func (unusedFlashSales) GetRequest(context.Context, string) (FlashSaleRequestView, error) {
	panic("unused")
}
func (unusedFlashSales) Allocate(context.Context, FlashSaleMessage, CampaignSnapshot, *domain.Order, FrozenLine) (*OrderRecord, error) {
	panic("unused")
}

func newMemoryService(t *testing.T) (*Service, *memoryWork, *memoryOrders, *checkingCatalog) {
	orders := &memoryOrders{}
	repositories := memoryRepositories{orders: orders, inbox: &memoryInbox{seen: make(map[string]bool)}}
	work := &memoryWork{repositories: repositories}
	catalog := &checkingCatalog{t: t, work: work}
	return NewService(work, catalog, nil, nil, nil, nil, fixedOrderClock{}), work, orders, catalog
}

type fixedOrderClock struct{}

func (fixedOrderClock) Now() time.Time {
	return time.Date(2026, time.September, 2, 12, 0, 0, 0, time.UTC)
}

func TestCreateOrderKeepsCatalogOutsideTransactionAndIsIdempotent(t *testing.T) {
	service, _, orders, catalog := newMemoryService(t)
	command := CreateOrderCommand{
		Actor: ActorContext{UserID: "user-1", StudioID: "studio-1", TenantRoles: []string{"student"}, ActorKind: "human"},
		Items: []OrderItemInput{{ProductVersionID: "product-1", Quantity: 1}},
		Audit: AuditContext{IdempotencyKey: "create-1"},
	}
	first, err := service.CreateOrder(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.CreateOrder(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || orders.adds != 1 {
		t.Fatalf("idempotency failed: first=%s second=%s adds=%d", first.ID, second.ID, orders.adds)
	}
	if catalog.calls != 2 {
		t.Fatalf("expected product snapshot validation on both attempts, got %d", catalog.calls)
	}
}

func TestSagaDeduplicatesInbox(t *testing.T) {
	service, _, orders, _ := newMemoryService(t)
	money, _ := domain.NewMoney(12000)
	aggregate, err := (domain.OrderFactory{}).Membership("order-1", "user-1", []domain.CreditProductLine{{ProductVersionID: "product-1", UnitPrice: money, Count: 1, Scope: domain.StudioScope, StudioID: "studio-1"}}, fixedOrderClock{}.Now().Add(15*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	orders.record = &OrderRecord{Aggregate: aggregate}
	command := SagaCommand{Actor: ActorContext{GlobalRoles: []string{"platform_admin"}, ActorKind: "human"}, OrderID: "order-1", EventID: "event-1", Event: domain.PaymentSucceededEvent}
	for attempt := 0; attempt < 2; attempt++ {
		if _, err = service.ApplySaga(context.Background(), command); err != nil {
			t.Fatal(err)
		}
	}
	if orders.record.Aggregate.Snapshot().Status != domain.Paid || orders.saves != 1 {
		t.Fatalf("duplicate saga had side effects: status=%s saves=%d", orders.record.Aggregate.Snapshot().Status, orders.saves)
	}
}
