// Package strategy defines trading decision logic (arb, hedge, prediction).
// Implementations consume Exchange books/ticks and emit orders or multi-leg hedge sets.
package strategy

import (
	"context"

	"github.com/nguyenducthinhdl/hhld/src/exchange"
)

// Leg is one order intended for a specific venue (crypto or prediction).
type Leg struct {
	Venue  exchange.VenueID
	Symbol exchange.Symbol
	Kind   exchange.Kind
	Side   exchange.Side
	Price  string
	Size   string
}

// Decision is one strategy output: one or more legs under a shared TraceID.
// For cross-venue hedges (prediction up/down + HL/GRVT), set HedgeID so risk/pnl
// treat legs as one linked set. Same-kind arb may leave HedgeID empty and still
// emit two legs with one TraceID.
type Decision struct {
	TraceID string
	HedgeID string
	Legs    []Leg
	Reason  string
}

// Strategy turns market views into paper (later live) decisions.
// It must not import vendor exchange SDKs.
type Strategy interface {
	Name() string
	// OnBooks evaluates the latest books (typically two+ venues for arb/hedge).
	OnBooks(ctx context.Context, books []exchange.Book) ([]Decision, error)
}
