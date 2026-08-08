// Package exchange defines the venue-agnostic Exchange boundary.
// Adapters (Hyperliquid, GRVT, Polymarket, …) live behind this package in later phases.
package exchange

import (
	"context"
	"time"
)

// VenueID identifies a trading venue (e.g. "hyperliquid", "grvt", "polymarket").
type VenueID string

// Symbol is an HHLD-level instrument id (e.g. "BTCUSD", or a prediction outcome key).
// Adapters map Symbol to venue-native names; strategy code never uses vendor symbols.
type Symbol string

// Side is the trade direction.
type Side string

const (
	SideBuy  Side = "buy"
	SideSell Side = "sell"
)

// Kind classifies an instrument so crypto and prediction markets share one model.
type Kind string

const (
	KindPerp       Kind = "perp"
	KindSpot       Kind = "spot"
	KindPrediction Kind = "prediction" // up/down, Yes/No, condition tokens
)

// Level is one price level on a book.
type Level struct {
	Price string
	Size  string
}

// Book is a normalized order book snapshot for one symbol on one venue.
type Book struct {
	Venue  VenueID
	Symbol Symbol
	Kind   Kind
	Bids   []Level // best first
	Asks   []Level // best first
	Time   time.Time
}

// Tick is a normalized trade or last-price update.
type Tick struct {
	Venue  VenueID
	Symbol Symbol
	Kind   Kind
	Price  string
	Size   string
	Side   Side // empty if unknown
	Time   time.Time
}

// OrderRequest is a paper (later live) order to place on this venue.
// HedgeID links legs of a multi-venue hedge; empty for standalone / same-kind arb legs
// that only share TraceID at the strategy layer.
type OrderRequest struct {
	ClientOrderID string
	TraceID       string
	HedgeID       string
	Symbol        Symbol
	Kind          Kind
	Side          Side
	Price         string
	Size          string
}

// OrderAck is the venue acknowledgement for a placed order.
type OrderAck struct {
	OrderID       string
	ClientOrderID string
	TraceID       string
	HedgeID       string
	Symbol        Symbol
	Status        string
	Time          time.Time
}

// Fill is a (partial or full) execution report.
type Fill struct {
	OrderID       string
	ClientOrderID string
	TraceID       string
	HedgeID       string
	Venue         VenueID
	Symbol        Symbol
	Kind          Kind
	Side          Side
	Price         string
	Size          string
	Fee           string
	Time          time.Time
}

// BookHandler receives book updates from SubscribeBook.
type BookHandler func(Book)

// TickHandler receives tick updates from SubscribeTicks.
type TickHandler func(Tick)

// Exchange is any tradable venue: crypto books or prediction markets.
// Implementations must not leak vendor SDKs into strategy/risk/pnl.
type Exchange interface {
	ID() VenueID

	SnapshotBook(ctx context.Context, symbol Symbol) (Book, error)
	SubscribeBook(ctx context.Context, symbol Symbol, h BookHandler) error
	SubscribeTicks(ctx context.Context, symbol Symbol, h TickHandler) error

	PlaceOrder(ctx context.Context, req OrderRequest) (OrderAck, error)
	CancelOrder(ctx context.Context, orderID string) error
}
