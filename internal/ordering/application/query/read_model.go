// Package query holds the read use cases. Queries read the projection
// (ordering_read schema) maintained from events, never the write model.
package query

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/raulalmeidatarazona/go-sdd/internal/platform/apperr"
)

// OrderView is the denormalised read model of an order.
type OrderView struct {
	ID                 string
	CustomerID         string
	Status             string
	Currency           string
	Lines              []OrderViewLine
	TotalMinor         int64
	CancellationReason string
	PlacedAt           time.Time
	UpdatedAt          time.Time
	Version            int64
}

type OrderViewLine struct {
	SKU            string `json:"sku"`
	Quantity       int    `json:"quantity"`
	UnitPriceMinor int64  `json:"unit_price_minor"`
}

// ListFilter selects a page of orders, newest first.
type ListFilter struct {
	Status     string
	CustomerID string
	After      *Cursor
	Limit      int
}

// Cursor is a keyset position (placed_at, id). It is exposed as an opaque token.
type Cursor struct {
	PlacedAt time.Time `json:"p"`
	ID       string    `json:"i"`
}

// ReadModel is the query-side persistence port.
type ReadModel interface {
	Get(ctx context.Context, id string) (OrderView, error)
	List(ctx context.Context, filter ListFilter) ([]OrderView, error)
}

var ErrInvalidPageToken = apperr.Invalid("invalid_page_token", "page token is malformed")

func encodeCursor(c Cursor) string {
	raw, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeCursor(token string) (*Cursor, error) {
	if token == "" {
		return nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return nil, ErrInvalidPageToken
	}
	var c Cursor
	if err := json.Unmarshal(raw, &c); err != nil || c.ID == "" {
		return nil, ErrInvalidPageToken
	}
	return &c, nil
}
