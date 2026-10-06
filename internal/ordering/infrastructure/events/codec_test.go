package events_test

import (
	"testing"
	"time"

	"github.com/raulalmeidatarazona/go-sdd/internal/ordering/domain"
	"github.com/raulalmeidatarazona/go-sdd/internal/ordering/infrastructure/events"
)

func TestOrderPlacedRoundTrip(t *testing.T) {
	price, _ := domain.NewMoney(250, "EUR")
	line, _ := domain.NewLineItem("SKU-1", 4, price)
	order, err := domain.Place(domain.NewOrderID(), "8b5d3c1e-7d6f-4c2a-9a59-0d7f3c0b2a11", "customer-1", "EUR",
		[]domain.LineItem{line}, time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	original := order.Events()[0].(domain.OrderPlaced)

	payload, err := events.Encode(original)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := events.Decode(original.EventName(), payload)
	if err != nil {
		t.Fatal(err)
	}
	got := decoded.(domain.OrderPlaced)
	if got.OrderID != original.OrderID || got.Total != original.Total || !got.PlacedAt.Equal(original.PlacedAt) || len(got.Lines) != 1 || got.Lines[0] != line {
		t.Fatalf("round trip mismatch:\n got %#v\nwant %#v", got, original)
	}
}

func TestDecodeUnknownType(t *testing.T) {
	if _, err := events.Decode("oms.order.v1.Unknown", nil); err == nil {
		t.Fatal("expected error")
	}
}
