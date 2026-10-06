//go:build integration

// Package e2e runs against the stack started with `make up`.
package e2e

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	orderv1 "github.com/raulalmeidatarazona/go-sdd/gen/go/oms/order/v1"
)

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

var (
	grpcAddr = env("E2E_GRPC_ADDR", "localhost:9090")
	httpURL  = env("E2E_HTTP_URL", "http://localhost:8080")
	dbURL    = env("E2E_DATABASE_URL", "postgres://oms_app:oms_app_local@localhost:5432/oms?sslmode=disable")
)

func client(t *testing.T) orderv1.OrderServiceClient {
	t.Helper()
	conn, err := grpc.NewClient(grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return orderv1.NewOrderServiceClient(conn)
}

func asTenant(tenant string) context.Context {
	return metadata.AppendToOutgoingContext(context.Background(), "x-tenant-id", tenant)
}

func placeOrder(t *testing.T, c orderv1.OrderServiceClient, tenant string) string {
	t.Helper()
	res, err := c.PlaceOrder(asTenant(tenant), &orderv1.PlaceOrderRequest{
		IdempotencyKey: uuid.NewString(),
		CustomerId:     "customer-e2e",
		CurrencyCode:   "EUR",
		Lines:          []*orderv1.LineItem{{Sku: "SKU-E2E", Quantity: 3, UnitPriceMinor: 1000}},
	})
	if err != nil {
		t.Fatalf("place order: %v", err)
	}
	return res.GetOrderId()
}

// eventually polls the read model, which lags commands by design.
func eventually(t *testing.T, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("condition not met within 10s")
}

func TestOrderLifecycleOverGRPC(t *testing.T) {
	c := client(t)
	tenant := uuid.NewString()
	id := placeOrder(t, c, tenant)

	var order *orderv1.Order
	eventually(t, func() bool {
		res, err := c.GetOrder(asTenant(tenant), &orderv1.GetOrderRequest{OrderId: id})
		order = res.GetOrder()
		return err == nil
	})
	if order.GetTotalMinor() != 3000 || order.GetStatus() != orderv1.OrderStatus_ORDER_STATUS_PLACED || order.GetVersion() != 1 {
		t.Fatalf("unexpected order %v", order)
	}

	if _, err := c.CancelOrder(asTenant(tenant), &orderv1.CancelOrderRequest{OrderId: id, Reason: "e2e"}); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		res, err := c.GetOrder(asTenant(tenant), &orderv1.GetOrderRequest{OrderId: id})
		return err == nil && res.GetOrder().GetStatus() == orderv1.OrderStatus_ORDER_STATUS_CANCELLED && res.GetOrder().GetVersion() == 2
	})

	_, err := c.CancelOrder(asTenant(tenant), &orderv1.CancelOrderRequest{OrderId: id, Reason: "again"})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("second cancel: %v", err)
	}
}

func TestTenantsAreIsolated(t *testing.T) {
	c := client(t)
	owner, other := uuid.NewString(), uuid.NewString()
	id := placeOrder(t, c, owner)
	eventually(t, func() bool {
		_, err := c.GetOrder(asTenant(owner), &orderv1.GetOrderRequest{OrderId: id})
		return err == nil
	})

	if _, err := c.GetOrder(asTenant(other), &orderv1.GetOrderRequest{OrderId: id}); status.Code(err) != codes.NotFound {
		t.Fatalf("other tenant read: %v", err)
	}
	if _, err := c.CancelOrder(asTenant(other), &orderv1.CancelOrderRequest{OrderId: id, Reason: "x"}); status.Code(err) != codes.NotFound {
		t.Fatalf("other tenant cancel: %v", err)
	}
	list, err := c.ListOrders(asTenant(other), &orderv1.ListOrdersRequest{})
	if err != nil || len(list.GetOrders()) != 0 {
		t.Fatalf("other tenant list: %v %v", list, err)
	}
}

// TestRowLevelSecurity checks isolation at the database, independent of Go code.
func TestRowLevelSecurity(t *testing.T) {
	c := client(t)
	tenant := uuid.NewString()
	placeOrder(t, c, tenant)

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx) }()

	var visible int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM ordering.orders`).Scan(&visible); err != nil {
		t.Fatal(err)
	}
	if visible != 0 {
		t.Fatalf("without app.tenant_id the app role sees %d orders, want 0", visible)
	}

	_, err = conn.Exec(ctx, `INSERT INTO ordering.orders (tenant_id, id, customer_id, status, currency, total_minor, idempotency_key, request_fingerprint, version, placed_at, updated_at)
		VALUES ($1, $2, 'c', 'PLACED', 'EUR', 0, 'k', 'f', 1, now(), now())`, tenant, uuid.NewString())
	if err == nil || !strings.Contains(err.Error(), "row-level security") {
		t.Fatalf("insert for a tenant other than the session's must fail, got %v", err)
	}
}

func TestRESTFacadeAndIdempotency(t *testing.T) {
	tenant := uuid.NewString()
	body := fmt.Sprintf(`{"idempotencyKey":"%s","customerId":"c-rest","currencyCode":"EUR","lines":[{"sku":"SKU-R","quantity":1,"unitPriceMinor":99}]}`, uuid.NewString())

	post := func() (int, string) {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, httpURL+"/v1/orders", strings.NewReader(body))
		req.Header.Set("X-Tenant-Id", tenant)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, string(raw)
	}
	code1, first := post()
	code2, second := post()
	if code1 != http.StatusOK || code2 != http.StatusOK || first != second {
		t.Fatalf("idempotent replay differs: %d %s / %d %s", code1, first, code2, second)
	}

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, httpURL+"/v1/orders", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("missing tenant: status %d, want 401", resp.StatusCode)
	}
}
