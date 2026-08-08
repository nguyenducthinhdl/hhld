// Package risk evaluates orders and hedge sets under the miss-more doctrine
// (fees, latency, partial fills). Prefer rejecting trades over taking bad fills.
package risk

import (
	"context"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/src/strategy"
)

// Verdict is the miss-more gate result for a Decision (including multi-leg hedges).
type Verdict struct {
	OK     bool
	Reason string
}

// MarketView is pre-trade market state used by Risk (books, clock, venue health).
type MarketView struct {
	Books      []exchange.Book
	Now        time.Time
	Unhealthy  map[exchange.VenueID]bool // venues known down / reconnecting
}

// Risk evaluates a whole Decision so both legs of a hedge are checked together.
type Risk interface {
	Evaluate(ctx context.Context, d strategy.Decision, mkt MarketView) (Verdict, error)
}
