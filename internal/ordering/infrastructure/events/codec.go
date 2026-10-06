// Package events translates domain events to and from the protobuf integration
// events in api/proto/oms/order/v1/order_events.proto.
package events

import (
	"fmt"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	orderv1 "github.com/raulalmeidatarazona/go-sdd/gen/go/oms/order/v1"
	"github.com/raulalmeidatarazona/go-sdd/internal/ordering/domain"
	"github.com/raulalmeidatarazona/go-sdd/internal/platform/ddd"
)

// Encode serialises a domain event to protobuf wire format.
func Encode(event ddd.Event) ([]byte, error) {
	var msg proto.Message
	switch e := event.(type) {
	case domain.OrderPlaced:
		msg = &orderv1.OrderPlaced{
			OrderId:      string(e.OrderID),
			CustomerId:   string(e.CustomerID),
			CurrencyCode: string(e.Currency),
			Lines:        linesToProto(e.Lines),
			TotalMinor:   e.Total.AmountMinor(),
			PlacedAt:     timestamppb.New(e.PlacedAt),
		}
	case domain.OrderCancelled:
		msg = &orderv1.OrderCancelled{
			OrderId:     string(e.OrderID),
			Reason:      e.Reason,
			CancelledAt: timestamppb.New(e.CancelledAt),
		}
	default:
		return nil, fmt.Errorf("encode: unsupported event %T", event)
	}
	return proto.Marshal(msg)
}

// Decode rebuilds a domain event from its type name and payload. Payloads
// come from our own outbox, but are still validated through the value objects.
func Decode(eventType string, payload []byte) (ddd.Event, error) {
	switch eventType {
	case domain.EventOrderPlaced:
		var msg orderv1.OrderPlaced
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return nil, err
		}
		currency, err := domain.ParseCurrency(msg.GetCurrencyCode())
		if err != nil {
			return nil, err
		}
		lines, err := linesFromProto(msg.GetLines(), currency)
		if err != nil {
			return nil, err
		}
		total, err := domain.NewMoney(msg.GetTotalMinor(), currency)
		if err != nil {
			return nil, err
		}
		return domain.OrderPlaced{
			OrderID:    domain.OrderID(msg.GetOrderId()),
			CustomerID: domain.CustomerID(msg.GetCustomerId()),
			Currency:   currency,
			Lines:      lines,
			Total:      total,
			PlacedAt:   msg.GetPlacedAt().AsTime(),
		}, nil
	case domain.EventOrderCancelled:
		var msg orderv1.OrderCancelled
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return nil, err
		}
		return domain.OrderCancelled{
			OrderID:     domain.OrderID(msg.GetOrderId()),
			Reason:      msg.GetReason(),
			CancelledAt: msg.GetCancelledAt().AsTime(),
		}, nil
	default:
		return nil, fmt.Errorf("decode: unknown event type %q", eventType)
	}
}

func linesToProto(lines []domain.LineItem) []*orderv1.LineItem {
	out := make([]*orderv1.LineItem, 0, len(lines))
	for _, l := range lines {
		out = append(out, &orderv1.LineItem{
			Sku:            string(l.SKU()),
			Quantity:       int32(l.Quantity()), //nolint:gosec // bounded to 10000 by domain.NewLineItem
			UnitPriceMinor: l.UnitPrice().AmountMinor(),
		})
	}
	return out
}

func linesFromProto(lines []*orderv1.LineItem, currency domain.Currency) ([]domain.LineItem, error) {
	out := make([]domain.LineItem, 0, len(lines))
	for _, l := range lines {
		sku, err := domain.ParseSKU(l.GetSku())
		if err != nil {
			return nil, err
		}
		price, err := domain.NewMoney(l.GetUnitPriceMinor(), currency)
		if err != nil {
			return nil, err
		}
		line, err := domain.NewLineItem(sku, int(l.GetQuantity()), price)
		if err != nil {
			return nil, err
		}
		out = append(out, line)
	}
	return out, nil
}
