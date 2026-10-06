package domain

import (
	"math"
	"regexp"

	"github.com/google/uuid"
)

// OrderID is a UUIDv7, so ids sort by creation time.
type OrderID string

func NewOrderID() OrderID { return OrderID(uuid.Must(uuid.NewV7()).String()) }

func ParseOrderID(raw string) (OrderID, error) {
	id, err := uuid.Parse(raw)
	if err != nil {
		return "", ErrInvalidOrderID
	}
	return OrderID(id.String()), nil
}

// CustomerID references a customer owned by another bounded context.
type CustomerID string

func ParseCustomerID(raw string) (CustomerID, error) {
	if raw == "" || len(raw) > 64 {
		return "", ErrInvalidCustomerID
	}
	return CustomerID(raw), nil
}

var currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)

// Currency is an ISO 4217 alphabetic code.
type Currency string

func ParseCurrency(raw string) (Currency, error) {
	if !currencyPattern.MatchString(raw) {
		return "", ErrInvalidCurrency
	}
	return Currency(raw), nil
}

// Money is an amount in the currency's minor unit. It never uses floats.
type Money struct {
	amountMinor int64
	currency    Currency
}

func NewMoney(amountMinor int64, currency Currency) (Money, error) {
	if amountMinor < 0 {
		return Money{}, ErrInvalidAmount
	}
	return Money{amountMinor: amountMinor, currency: currency}, nil
}

func (m Money) AmountMinor() int64 { return m.amountMinor }
func (m Money) Currency() Currency { return m.currency }

// Times multiplies by a quantity, failing on overflow.
func (m Money) Times(quantity int) (Money, error) {
	if quantity != 0 && m.amountMinor > math.MaxInt64/int64(quantity) {
		return Money{}, ErrInvalidAmount
	}
	return Money{amountMinor: m.amountMinor * int64(quantity), currency: m.currency}, nil
}

// Plus adds two amounts of the same currency, failing on overflow.
func (m Money) Plus(other Money) (Money, error) {
	if m.currency != other.currency || m.amountMinor > math.MaxInt64-other.amountMinor {
		return Money{}, ErrInvalidAmount
	}
	return Money{amountMinor: m.amountMinor + other.amountMinor, currency: m.currency}, nil
}

var skuPattern = regexp.MustCompile(`^[A-Z0-9_-]{1,64}$`)

// SKU identifies a sellable item.
type SKU string

func ParseSKU(raw string) (SKU, error) {
	if !skuPattern.MatchString(raw) {
		return "", ErrInvalidSKU
	}
	return SKU(raw), nil
}

// LineItem is an immutable order line.
type LineItem struct {
	sku       SKU
	quantity  int
	unitPrice Money
}

const maxQuantity = 10_000

func NewLineItem(sku SKU, quantity int, unitPrice Money) (LineItem, error) {
	if quantity < 1 || quantity > maxQuantity {
		return LineItem{}, ErrInvalidQuantity
	}
	return LineItem{sku: sku, quantity: quantity, unitPrice: unitPrice}, nil
}

func (l LineItem) SKU() SKU                 { return l.sku }
func (l LineItem) Quantity() int            { return l.quantity }
func (l LineItem) UnitPrice() Money         { return l.unitPrice }
func (l LineItem) Subtotal() (Money, error) { return l.unitPrice.Times(l.quantity) }

// IdempotencyKey deduplicates retried commands per tenant.
type IdempotencyKey string

func ParseIdempotencyKey(raw string) (IdempotencyKey, error) {
	if raw == "" || len(raw) > 128 {
		return "", ErrInvalidIdempotencyKey
	}
	return IdempotencyKey(raw), nil
}
