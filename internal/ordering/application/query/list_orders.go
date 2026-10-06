package query

import (
	"context"

	"github.com/raulalmeidatarazona/go-sdd/internal/ordering/domain"
	"github.com/raulalmeidatarazona/go-sdd/internal/platform/apperr"
	"github.com/raulalmeidatarazona/go-sdd/internal/platform/tenancy"
)

const (
	defaultPageSize = 50
	maxPageSize     = 200
)

var ErrInvalidStatusFilter = apperr.Invalid("invalid_status_filter", "status filter is not a known order status")

type ListOrders struct {
	PageSize   int
	PageToken  string
	Status     string // empty means any
	CustomerID string // empty means any
}

type ListOrdersResult struct {
	Orders        []OrderView
	NextPageToken string
}

type ListOrdersHandler struct {
	orders ReadModel
}

func NewListOrdersHandler(orders ReadModel) *ListOrdersHandler {
	return &ListOrdersHandler{orders: orders}
}

func (h *ListOrdersHandler) Handle(ctx context.Context, q ListOrders) (ListOrdersResult, error) {
	if _, err := tenancy.FromContext(ctx); err != nil {
		return ListOrdersResult{}, err
	}
	size := q.PageSize
	switch {
	case size <= 0:
		size = defaultPageSize
	case size > maxPageSize:
		size = maxPageSize
	}
	switch domain.Status(q.Status) {
	case "", domain.StatusPlaced, domain.StatusCancelled:
	default:
		return ListOrdersResult{}, ErrInvalidStatusFilter
	}
	after, err := decodeCursor(q.PageToken)
	if err != nil {
		return ListOrdersResult{}, err
	}

	// Fetch one extra row to know whether another page exists.
	views, err := h.orders.List(ctx, ListFilter{Status: q.Status, CustomerID: q.CustomerID, After: after, Limit: size + 1})
	if err != nil {
		return ListOrdersResult{}, err
	}
	result := ListOrdersResult{Orders: views}
	if len(views) > size {
		result.Orders = views[:size]
		last := result.Orders[size-1]
		result.NextPageToken = encodeCursor(Cursor{PlacedAt: last.PlacedAt, ID: last.ID})
	}
	return result, nil
}
