package command

import (
	"context"
	"time"

	"github.com/raulalmeidatarazona/go-sdd/internal/ordering/domain"
	"github.com/raulalmeidatarazona/go-sdd/internal/platform/tenancy"
)

type CancelOrder struct {
	OrderID string
	Reason  string
}

type CancelOrderResult struct{}

type CancelOrderHandler struct {
	uow    UnitOfWork
	orders domain.Repository
	now    func() time.Time
}

func NewCancelOrderHandler(uow UnitOfWork, orders domain.Repository, now func() time.Time) *CancelOrderHandler {
	return &CancelOrderHandler{uow: uow, orders: orders, now: now}
}

func (h *CancelOrderHandler) Handle(ctx context.Context, cmd CancelOrder) (CancelOrderResult, error) {
	if _, err := tenancy.FromContext(ctx); err != nil {
		return CancelOrderResult{}, err
	}
	id, err := domain.ParseOrderID(cmd.OrderID)
	if err != nil {
		return CancelOrderResult{}, err
	}

	err = h.uow(ctx, func(ctx context.Context) error {
		order, err := h.orders.Get(ctx, id)
		if err != nil {
			return err
		}
		if err := order.Cancel(cmd.Reason, h.now()); err != nil {
			return err
		}
		return h.orders.Update(ctx, order)
	})
	return CancelOrderResult{}, err
}
