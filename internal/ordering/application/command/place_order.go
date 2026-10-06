// Package command holds the write use cases of the ordering context. Every
// command follows the same steps: resolve tenant, parse input into value
// objects, run the aggregate behaviour inside a unit of work, return ids only.
package command

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/raulalmeidatarazona/go-sdd/internal/ordering/domain"
	"github.com/raulalmeidatarazona/go-sdd/internal/platform/tenancy"
)

// UnitOfWork runs fn atomically for the tenant in ctx.
type UnitOfWork func(ctx context.Context, fn func(ctx context.Context) error) error

type PlaceOrder struct {
	IdempotencyKey string
	CustomerID     string
	Currency       string
	Lines          []PlaceOrderLine
}

type PlaceOrderLine struct {
	SKU            string
	Quantity       int
	UnitPriceMinor int64
}

type PlaceOrderResult struct {
	OrderID string
}

type PlaceOrderHandler struct {
	uow    UnitOfWork
	orders domain.Repository
	now    func() time.Time
}

func NewPlaceOrderHandler(uow UnitOfWork, orders domain.Repository, now func() time.Time) *PlaceOrderHandler {
	return &PlaceOrderHandler{uow: uow, orders: orders, now: now}
}

func (h *PlaceOrderHandler) Handle(ctx context.Context, cmd PlaceOrder) (PlaceOrderResult, error) {
	tenant, err := tenancy.FromContext(ctx)
	if err != nil {
		return PlaceOrderResult{}, err
	}
	key, err := domain.ParseIdempotencyKey(cmd.IdempotencyKey)
	if err != nil {
		return PlaceOrderResult{}, err
	}
	customer, err := domain.ParseCustomerID(cmd.CustomerID)
	if err != nil {
		return PlaceOrderResult{}, err
	}
	currency, err := domain.ParseCurrency(cmd.Currency)
	if err != nil {
		return PlaceOrderResult{}, err
	}
	lines, err := parseLines(cmd.Lines, currency)
	if err != nil {
		return PlaceOrderResult{}, err
	}
	fingerprint, err := cmd.fingerprint()
	if err != nil {
		return PlaceOrderResult{}, err
	}

	place := func() (domain.OrderID, error) {
		var id domain.OrderID
		err := h.uow(ctx, func(ctx context.Context) error {
			existingID, existingFingerprint, found, err := h.orders.FindByIdempotencyKey(ctx, key)
			if err != nil {
				return err
			}
			if found {
				if existingFingerprint != fingerprint {
					return domain.ErrIdempotencyKeyReused
				}
				id = existingID
				return nil
			}
			order, err := domain.Place(domain.NewOrderID(), tenant, customer, currency, lines, h.now())
			if err != nil {
				return err
			}
			if err := h.orders.Add(ctx, order, key, fingerprint); err != nil {
				return err
			}
			id = order.ID()
			return nil
		})
		return id, err
	}

	id, err := place()
	if errors.Is(err, domain.ErrDuplicateIdempotencyKey) {
		// A concurrent request with the same key won the insert; the retry
		// reads its result (or detects a payload mismatch).
		id, err = place()
	}
	if err != nil {
		return PlaceOrderResult{}, err
	}
	return PlaceOrderResult{OrderID: string(id)}, nil
}

func parseLines(raw []PlaceOrderLine, currency domain.Currency) ([]domain.LineItem, error) {
	lines := make([]domain.LineItem, 0, len(raw))
	for _, l := range raw {
		sku, err := domain.ParseSKU(l.SKU)
		if err != nil {
			return nil, err
		}
		price, err := domain.NewMoney(l.UnitPriceMinor, currency)
		if err != nil {
			return nil, err
		}
		line, err := domain.NewLineItem(sku, l.Quantity, price)
		if err != nil {
			return nil, err
		}
		lines = append(lines, line)
	}
	return lines, nil
}

// fingerprint identifies the request payload so a reused idempotency key with
// a different body is rejected instead of silently returning another order.
func (cmd PlaceOrder) fingerprint() (string, error) {
	payload := cmd
	payload.IdempotencyKey = ""
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
