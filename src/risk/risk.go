// Package risk is Risk Management: hard miss-more gates plus pluggable estimation
// (Value at Risk, winning-rate prediction). Estimators may use detective formulas on
// historical data today and machine learning later without changing Strategy/Execution.
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
	Books     []exchange.Book
	Now       time.Time
	Unhealthy map[exchange.VenueID]bool // venues known down / reconnecting
}

// Estimate is a predictive risk view (not a hard gate by itself).
// WinRate is in [0,1]; VaR is a loss magnitude at Confidence (e.g. 0.95).
type Estimate struct {
	WinRate    float64 // predicted probability the Decision is net-profitable
	VaR        float64 // value-at-risk (positive number = adverse move / loss size in PnL units)
	Confidence float64 // e.g. 0.95 for VaR
	Method     string  // "formula" | "historical" | "ml" | …
	Reason     string
}

// Risk evaluates a whole Decision so both legs of a hedge are checked together (hard gate).
type Risk interface {
	Evaluate(ctx context.Context, d strategy.Decision, mkt MarketView) (Verdict, error)
}

// Estimator predicts winning rate and/or VaR for a Decision.
// Implementations: detective formulas on history, later ML models — same interface.
type Estimator interface {
	Estimate(ctx context.Context, d strategy.Decision, mkt MarketView) (Estimate, error)
}

// Manager is the Risk Management facade: hard gates + optional estimation.
// Paper path may use only Gate; later paths can require min WinRate / max VaR.
type Manager interface {
	Risk
	Estimator
}
