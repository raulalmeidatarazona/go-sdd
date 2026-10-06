// Package grpcapi adapts orderv1.OrderService to the application handlers.
// It only maps protobuf <-> application types; no business rules live here.
package grpcapi

import (
	"context"

	"google.golang.org/protobuf/types/known/timestamppb"

	orderv1 "github.com/raulalmeidatarazona/go-sdd/gen/go/oms/order/v1"
	"github.com/raulalmeidatarazona/go-sdd/internal/ordering/application/command"
	"github.com/raulalmeidatarazona/go-sdd/internal/ordering/application/query"
	"github.com/raulalmeidatarazona/go-sdd/internal/ordering/domain"
	"github.com/raulalmeidatarazona/go-sdd/internal/platform/cqrs"
)

// Handlers groups the use cases the service exposes.
type Handlers struct {
	PlaceOrder  cqrs.Handler[command.PlaceOrder, command.PlaceOrderResult]
	CancelOrder cqrs.Handler[command.CancelOrder, command.CancelOrderResult]
	GetOrder    cqrs.Handler[query.GetOrder, query.OrderView]
	ListOrders  cqrs.Handler[query.ListOrders, query.ListOrdersResult]
}

type OrderServer struct {
	orderv1.UnimplementedOrderServiceServer
	h Handlers
}

func NewOrderServer(h Handlers) *OrderServer {
	return &OrderServer{h: h}
}

func (s *OrderServer) PlaceOrder(ctx context.Context, req *orderv1.PlaceOrderRequest) (*orderv1.PlaceOrderResponse, error) {
	lines := make([]command.PlaceOrderLine, 0, len(req.GetLines()))
	for _, l := range req.GetLines() {
		lines = append(lines, command.PlaceOrderLine{SKU: l.GetSku(), Quantity: int(l.GetQuantity()), UnitPriceMinor: l.GetUnitPriceMinor()})
	}
	res, err := s.h.PlaceOrder.Handle(ctx, command.PlaceOrder{
		IdempotencyKey: req.GetIdempotencyKey(),
		CustomerID:     req.GetCustomerId(),
		Currency:       req.GetCurrencyCode(),
		Lines:          lines,
	})
	if err != nil {
		return nil, err
	}
	return &orderv1.PlaceOrderResponse{OrderId: res.OrderID}, nil
}

func (s *OrderServer) CancelOrder(ctx context.Context, req *orderv1.CancelOrderRequest) (*orderv1.CancelOrderResponse, error) {
	if _, err := s.h.CancelOrder.Handle(ctx, command.CancelOrder{OrderID: req.GetOrderId(), Reason: req.GetReason()}); err != nil {
		return nil, err
	}
	return &orderv1.CancelOrderResponse{}, nil
}

func (s *OrderServer) GetOrder(ctx context.Context, req *orderv1.GetOrderRequest) (*orderv1.GetOrderResponse, error) {
	view, err := s.h.GetOrder.Handle(ctx, query.GetOrder{OrderID: req.GetOrderId()})
	if err != nil {
		return nil, err
	}
	return &orderv1.GetOrderResponse{Order: toProto(view)}, nil
}

func (s *OrderServer) ListOrders(ctx context.Context, req *orderv1.ListOrdersRequest) (*orderv1.ListOrdersResponse, error) {
	res, err := s.h.ListOrders.Handle(ctx, query.ListOrders{
		PageSize:   int(req.GetPageSize()),
		PageToken:  req.GetPageToken(),
		Status:     statusFromProto(req.GetStatus()),
		CustomerID: req.GetCustomerId(),
	})
	if err != nil {
		return nil, err
	}
	orders := make([]*orderv1.Order, 0, len(res.Orders))
	for _, v := range res.Orders {
		orders = append(orders, toProto(v))
	}
	return &orderv1.ListOrdersResponse{Orders: orders, NextPageToken: res.NextPageToken}, nil
}

func toProto(v query.OrderView) *orderv1.Order {
	lines := make([]*orderv1.LineItem, 0, len(v.Lines))
	for _, l := range v.Lines {
		lines = append(lines, &orderv1.LineItem{Sku: l.SKU, Quantity: int32(l.Quantity), UnitPriceMinor: l.UnitPriceMinor}) //nolint:gosec // bounded by the domain
	}
	return &orderv1.Order{
		OrderId:            v.ID,
		CustomerId:         v.CustomerID,
		Status:             statusToProto(v.Status),
		CurrencyCode:       v.Currency,
		Lines:              lines,
		TotalMinor:         v.TotalMinor,
		CancellationReason: v.CancellationReason,
		PlacedAt:           timestamppb.New(v.PlacedAt),
		UpdateTime:         timestamppb.New(v.UpdatedAt),
		Version:            v.Version,
	}
}

func statusToProto(s string) orderv1.OrderStatus {
	switch domain.Status(s) {
	case domain.StatusPlaced:
		return orderv1.OrderStatus_ORDER_STATUS_PLACED
	case domain.StatusCancelled:
		return orderv1.OrderStatus_ORDER_STATUS_CANCELLED
	default:
		return orderv1.OrderStatus_ORDER_STATUS_UNSPECIFIED
	}
}

func statusFromProto(s orderv1.OrderStatus) string {
	switch s {
	case orderv1.OrderStatus_ORDER_STATUS_PLACED:
		return string(domain.StatusPlaced)
	case orderv1.OrderStatus_ORDER_STATUS_CANCELLED:
		return string(domain.StatusCancelled)
	default:
		return ""
	}
}
