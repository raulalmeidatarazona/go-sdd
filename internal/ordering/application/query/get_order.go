package query

import (
	"context"

	"github.com/raulalmeidatarazona/go-sdd/internal/ordering/domain"
	"github.com/raulalmeidatarazona/go-sdd/internal/platform/tenancy"
)

type GetOrder struct {
	OrderID string
}

type GetOrderHandler struct {
	orders ReadModel
}

func NewGetOrderHandler(orders ReadModel) *GetOrderHandler {
	return &GetOrderHandler{orders: orders}
}

func (h *GetOrderHandler) Handle(ctx context.Context, q GetOrder) (OrderView, error) {
	if _, err := tenancy.FromContext(ctx); err != nil {
		return OrderView{}, err
	}
	id, err := domain.ParseOrderID(q.OrderID)
	if err != nil {
		return OrderView{}, err
	}
	return h.orders.Get(ctx, string(id))
}
