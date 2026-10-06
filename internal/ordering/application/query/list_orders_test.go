package query_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/raulalmeidatarazona/go-sdd/internal/ordering/application/query"
	"github.com/raulalmeidatarazona/go-sdd/internal/platform/tenancy"
)

type fakeReadModel struct {
	views      []query.OrderView
	lastFilter query.ListFilter
}

func (f *fakeReadModel) Get(context.Context, string) (query.OrderView, error) { return f.views[0], nil }

func (f *fakeReadModel) List(_ context.Context, filter query.ListFilter) ([]query.OrderView, error) {
	f.lastFilter = filter
	if filter.Limit < len(f.views) {
		return f.views[:filter.Limit], nil
	}
	return f.views, nil
}

var tenantCtx = tenancy.WithTenant(context.Background(), "8b5d3c1e-7d6f-4c2a-9a59-0d7f3c0b2a11")

func views(n int) []query.OrderView {
	out := make([]query.OrderView, n)
	base := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	for i := range out {
		out[i] = query.OrderView{ID: "0199b6f0-0000-7000-8000-00000000000" + string(rune('0'+i)), PlacedAt: base.Add(-time.Duration(i) * time.Minute)}
	}
	return out
}

func TestListOrdersPaginates(t *testing.T) {
	rm := &fakeReadModel{views: views(3)}
	h := query.NewListOrdersHandler(rm)

	page, err := h.Handle(tenantCtx, query.ListOrders{PageSize: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Orders) != 2 || page.NextPageToken == "" {
		t.Fatalf("got %d orders, token %q", len(page.Orders), page.NextPageToken)
	}
	if rm.lastFilter.Limit != 3 {
		t.Fatalf("limit = %d, want page size + 1", rm.lastFilter.Limit)
	}

	if _, err := h.Handle(tenantCtx, query.ListOrders{PageSize: 2, PageToken: page.NextPageToken}); err != nil {
		t.Fatal(err)
	}
	if rm.lastFilter.After == nil || rm.lastFilter.After.ID != page.Orders[1].ID {
		t.Fatalf("cursor not decoded: %#v", rm.lastFilter.After)
	}
}

func TestListOrdersLastPageHasNoToken(t *testing.T) {
	page, err := query.NewListOrdersHandler(&fakeReadModel{views: views(2)}).Handle(tenantCtx, query.ListOrders{PageSize: 5})
	if err != nil {
		t.Fatal(err)
	}
	if page.NextPageToken != "" {
		t.Fatalf("unexpected token %q", page.NextPageToken)
	}
}

func TestListOrdersRejectsBadInput(t *testing.T) {
	h := query.NewListOrdersHandler(&fakeReadModel{})
	if _, err := h.Handle(tenantCtx, query.ListOrders{PageToken: "%%%"}); !errors.Is(err, query.ErrInvalidPageToken) {
		t.Fatalf("err = %v", err)
	}
	if _, err := h.Handle(tenantCtx, query.ListOrders{Status: "SHIPPED"}); !errors.Is(err, query.ErrInvalidStatusFilter) {
		t.Fatalf("err = %v", err)
	}
	if _, err := h.Handle(context.Background(), query.ListOrders{}); !errors.Is(err, tenancy.ErrMissing) {
		t.Fatalf("err = %v", err)
	}
}

func TestListOrdersClampsPageSize(t *testing.T) {
	rm := &fakeReadModel{}
	if _, err := query.NewListOrdersHandler(rm).Handle(tenantCtx, query.ListOrders{PageSize: 10_000}); err != nil {
		t.Fatal(err)
	}
	if rm.lastFilter.Limit != 201 {
		t.Fatalf("limit = %d, want 201", rm.lastFilter.Limit)
	}
}
