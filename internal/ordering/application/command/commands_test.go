package command_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/raulalmeidatarazona/go-sdd/internal/ordering/application/command"
	"github.com/raulalmeidatarazona/go-sdd/internal/ordering/domain"
	"github.com/raulalmeidatarazona/go-sdd/internal/platform/tenancy"
)

// memoryRepository is an in-memory domain.Repository for application tests.
type memoryRepository struct {
	mu      sync.Mutex
	orders  map[domain.OrderID]domain.Snapshot
	keys    map[domain.IdempotencyKey][2]string
	events  []string
	addErrs []error
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{orders: map[domain.OrderID]domain.Snapshot{}, keys: map[domain.IdempotencyKey][2]string{}}
}

func (r *memoryRepository) Get(_ context.Context, id domain.OrderID) (*domain.Order, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.orders[id]
	if !ok {
		return nil, domain.ErrOrderNotFound
	}
	return domain.Rehydrate(s), nil
}

func (r *memoryRepository) FindByIdempotencyKey(_ context.Context, key domain.IdempotencyKey) (domain.OrderID, string, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.keys[key]
	return domain.OrderID(v[0]), v[1], ok, nil
}

func (r *memoryRepository) Add(_ context.Context, o *domain.Order, key domain.IdempotencyKey, fingerprint string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.addErrs) > 0 {
		err := r.addErrs[0]
		r.addErrs = r.addErrs[1:]
		return err
	}
	r.keys[key] = [2]string{string(o.ID()), fingerprint}
	r.save(o, 1)
	return nil
}

func (r *memoryRepository) Update(_ context.Context, o *domain.Order) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.orders[o.ID()].Version != o.Version() {
		return domain.ErrConcurrentModification
	}
	r.save(o, o.Version()+1)
	return nil
}

func (r *memoryRepository) save(o *domain.Order, version int64) {
	for _, e := range o.Events() {
		r.events = append(r.events, e.EventName())
	}
	r.orders[o.ID()] = domain.Snapshot{
		ID: o.ID(), TenantID: o.TenantID(), CustomerID: o.CustomerID(), Status: o.Status(), Lines: o.Lines(),
		Total: o.Total(), CancellationReason: o.CancellationReason(), PlacedAt: o.PlacedAt(), UpdatedAt: o.UpdatedAt(), Version: version,
	}
	o.MarkPersisted(version)
}

func directUnitOfWork(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }

var (
	tenantCtx = tenancy.WithTenant(context.Background(), "8b5d3c1e-7d6f-4c2a-9a59-0d7f3c0b2a11")
	fixedNow  = func() time.Time { return time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC) }
)

func validPlaceOrder() command.PlaceOrder {
	return command.PlaceOrder{
		IdempotencyKey: "key-1",
		CustomerID:     "customer-1",
		Currency:       "EUR",
		Lines:          []command.PlaceOrderLine{{SKU: "SKU-1", Quantity: 2, UnitPriceMinor: 500}},
	}
}

func TestPlaceOrderStoresOrderAndEvent(t *testing.T) {
	repo := newMemoryRepository()
	h := command.NewPlaceOrderHandler(directUnitOfWork, repo, fixedNow)

	res, err := h.Handle(tenantCtx, validPlaceOrder())
	if err != nil {
		t.Fatal(err)
	}
	stored, err := repo.Get(tenantCtx, domain.OrderID(res.OrderID))
	if err != nil {
		t.Fatal(err)
	}
	if stored.Total().AmountMinor() != 1000 {
		t.Fatalf("total = %d", stored.Total().AmountMinor())
	}
	if len(repo.events) != 1 || repo.events[0] != domain.EventOrderPlaced {
		t.Fatalf("events = %v", repo.events)
	}
}

func TestPlaceOrderIsIdempotent(t *testing.T) {
	repo := newMemoryRepository()
	h := command.NewPlaceOrderHandler(directUnitOfWork, repo, fixedNow)

	first, err := h.Handle(tenantCtx, validPlaceOrder())
	if err != nil {
		t.Fatal(err)
	}
	second, err := h.Handle(tenantCtx, validPlaceOrder())
	if err != nil {
		t.Fatal(err)
	}
	if first.OrderID != second.OrderID || len(repo.orders) != 1 {
		t.Fatalf("retry created a second order: %s vs %s", first.OrderID, second.OrderID)
	}

	changed := validPlaceOrder()
	changed.Lines[0].Quantity = 3
	if _, err := h.Handle(tenantCtx, changed); !errors.Is(err, domain.ErrIdempotencyKeyReused) {
		t.Fatalf("reused key with different payload: err = %v", err)
	}
}

func TestPlaceOrderRetriesAfterConcurrentInsert(t *testing.T) {
	repo := newMemoryRepository()
	repo.addErrs = []error{domain.ErrDuplicateIdempotencyKey}
	h := command.NewPlaceOrderHandler(directUnitOfWork, repo, fixedNow)

	if _, err := h.Handle(tenantCtx, validPlaceOrder()); err != nil {
		t.Fatalf("expected retry to succeed, got %v", err)
	}
}

func TestPlaceOrderRequiresTenant(t *testing.T) {
	h := command.NewPlaceOrderHandler(directUnitOfWork, newMemoryRepository(), fixedNow)
	if _, err := h.Handle(context.Background(), validPlaceOrder()); !errors.Is(err, tenancy.ErrMissing) {
		t.Fatalf("err = %v", err)
	}
}

func TestPlaceOrderValidatesInput(t *testing.T) {
	h := command.NewPlaceOrderHandler(directUnitOfWork, newMemoryRepository(), fixedNow)
	cmd := validPlaceOrder()
	cmd.Currency = "euro"
	if _, err := h.Handle(tenantCtx, cmd); !errors.Is(err, domain.ErrInvalidCurrency) {
		t.Fatalf("err = %v", err)
	}
}

func TestCancelOrder(t *testing.T) {
	repo := newMemoryRepository()
	placed, err := command.NewPlaceOrderHandler(directUnitOfWork, repo, fixedNow).Handle(tenantCtx, validPlaceOrder())
	if err != nil {
		t.Fatal(err)
	}
	cancel := command.NewCancelOrderHandler(directUnitOfWork, repo, fixedNow)

	if _, err := cancel.Handle(tenantCtx, command.CancelOrder{OrderID: placed.OrderID, Reason: "out of stock"}); err != nil {
		t.Fatal(err)
	}
	stored, _ := repo.Get(tenantCtx, domain.OrderID(placed.OrderID))
	if stored.Status() != domain.StatusCancelled || stored.Version() != 2 {
		t.Fatalf("status=%s version=%d", stored.Status(), stored.Version())
	}
	if _, err := cancel.Handle(tenantCtx, command.CancelOrder{OrderID: placed.OrderID, Reason: "again"}); !errors.Is(err, domain.ErrOrderAlreadyCancelled) {
		t.Fatalf("err = %v", err)
	}
	if _, err := cancel.Handle(tenantCtx, command.CancelOrder{OrderID: string(domain.NewOrderID()), Reason: "x"}); !errors.Is(err, domain.ErrOrderNotFound) {
		t.Fatalf("err = %v", err)
	}
}
