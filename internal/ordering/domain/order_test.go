package domain_test

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/raulalmeidatarazona/go-sdd/internal/ordering/domain"
)

const tenant = "8b5d3c1e-7d6f-4c2a-9a59-0d7f3c0b2a11"

var now = time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

func line(t *testing.T, sku string, qty int, price int64) domain.LineItem {
	t.Helper()
	money, err := domain.NewMoney(price, "EUR")
	if err != nil {
		t.Fatal(err)
	}
	l, err := domain.NewLineItem(domain.SKU(sku), qty, money)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func placeOrder(t *testing.T, lines ...domain.LineItem) *domain.Order {
	t.Helper()
	order, err := domain.Place(domain.NewOrderID(), tenant, "customer-1", "EUR", lines, now)
	if err != nil {
		t.Fatal(err)
	}
	return order
}

func TestPlaceComputesTotalAndRecordsEvent(t *testing.T) {
	order := placeOrder(t, line(t, "SKU-1", 2, 1050), line(t, "SKU-2", 1, 300))

	if got := order.Total().AmountMinor(); got != 2400 {
		t.Fatalf("total = %d, want 2400", got)
	}
	if order.Status() != domain.StatusPlaced {
		t.Fatalf("status = %s", order.Status())
	}
	events := order.Events()
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	placed, ok := events[0].(domain.OrderPlaced)
	if !ok || placed.Total.AmountMinor() != 2400 || len(placed.Lines) != 2 {
		t.Fatalf("unexpected event %#v", events[0])
	}
}

func TestPlaceRejectsInvalidOrders(t *testing.T) {
	usd, _ := domain.NewMoney(100, "USD")
	usdLine, _ := domain.NewLineItem("SKU-USD", 1, usd)
	maxPrice, _ := domain.NewMoney(math.MaxInt64/2+1, "EUR")
	huge, _ := domain.NewLineItem("SKU-BIG", 2, maxPrice)

	tests := map[string]struct {
		lines []domain.LineItem
		want  error
	}{
		"no lines":          {nil, domain.ErrNoLines},
		"duplicate sku":     {[]domain.LineItem{line(t, "A", 1, 1), line(t, "A", 2, 1)}, domain.ErrDuplicateSKU},
		"currency mismatch": {[]domain.LineItem{usdLine}, domain.ErrInvalidCurrency},
		"total overflow":    {[]domain.LineItem{huge}, domain.ErrInvalidAmount},
		"too many lines":    {manyLines(t, 101), domain.ErrTooManyLines},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := domain.Place(domain.NewOrderID(), tenant, "customer-1", "EUR", tc.lines, now)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func manyLines(t *testing.T, n int) []domain.LineItem {
	lines := make([]domain.LineItem, n)
	for i := range lines {
		lines[i] = line(t, "SKU-"+string(rune('A'+i%26))+string(rune('A'+i/26)), 1, 1)
	}
	return lines
}

func TestCancel(t *testing.T) {
	order := placeOrder(t, line(t, "SKU-1", 1, 100))
	order.MarkPersisted(1)

	if err := order.Cancel("customer request", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if order.Status() != domain.StatusCancelled || order.CancellationReason() != "customer request" {
		t.Fatalf("unexpected state %s %q", order.Status(), order.CancellationReason())
	}
	if _, ok := order.Events()[0].(domain.OrderCancelled); !ok {
		t.Fatalf("expected OrderCancelled, got %#v", order.Events())
	}
	if err := order.Cancel("again", now); !errors.Is(err, domain.ErrOrderAlreadyCancelled) {
		t.Fatalf("second cancel err = %v", err)
	}
}

func TestCancelRequiresReason(t *testing.T) {
	order := placeOrder(t, line(t, "SKU-1", 1, 100))
	if err := order.Cancel("", now); !errors.Is(err, domain.ErrInvalidCancelReason) {
		t.Fatalf("err = %v", err)
	}
}

func TestValueObjects(t *testing.T) {
	if _, err := domain.ParseCurrency("eur"); !errors.Is(err, domain.ErrInvalidCurrency) {
		t.Fatalf("lowercase currency accepted: %v", err)
	}
	if _, err := domain.ParseSKU("bad sku"); !errors.Is(err, domain.ErrInvalidSKU) {
		t.Fatalf("invalid sku accepted: %v", err)
	}
	if _, err := domain.NewLineItem("SKU", 0, domain.Money{}); !errors.Is(err, domain.ErrInvalidQuantity) {
		t.Fatalf("zero quantity accepted: %v", err)
	}
	if _, err := domain.NewMoney(-1, "EUR"); !errors.Is(err, domain.ErrInvalidAmount) {
		t.Fatalf("negative money accepted: %v", err)
	}
	if _, err := domain.ParseOrderID("not-a-uuid"); !errors.Is(err, domain.ErrInvalidOrderID) {
		t.Fatalf("invalid id accepted: %v", err)
	}
}
