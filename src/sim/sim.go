// Package sim provides backtest simulation: tick/book replay through Strategy/Risk → PnL,
// plus abstracted winning-rate and winning-distribution analysis for Risk calibration.
package sim

import (
	"context"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/src/pnl"
	"github.com/nguyenducthinhdl/hhld/src/risk"
	"github.com/nguyenducthinhdl/hhld/src/strategy"
)

// Input is historical market data for a simulation run.
type Input struct {
	Books []exchange.Book
	Ticks []exchange.Tick
}

// Simulator replays market data through the same Strategy/Risk contracts as paper trading.
type Simulator interface {
	Run(ctx context.Context, in Input, s strategy.Strategy, r risk.Risk, t pnl.Tracker) (pnl.Snapshot, error)
}

// OutcomeDims identifies one simulated (or historical) arb outcome cell for distribution analysis.
type OutcomeDims struct {
	Symbol    exchange.Symbol
	Gap       float64 // sellBid - buyAsk (price units) at decision time
	Volume1   string  // size on exchange1 / buy leg
	Volume2   string  // size on exchange2 / sell leg
	Exchange1 exchange.VenueID
	Exchange2 exchange.VenueID
	Time      time.Time
}

// WinSample is one realized outcome under OutcomeDims.
type WinSample struct {
	Dims OutcomeDims
	Won  bool   // net profitable after modeled costs / fills
	PnL  string // realized PnL string for this sample
}

// Distribution is the empirical winning distribution over samples (optionally filtered).
type Distribution struct {
	Samples []WinSample
	WinRate float64 // len(won) / len(Samples); 0 if empty
}

// Filter narrows distribution queries. Zero/empty fields are ignored.
type Filter struct {
	Symbol    exchange.Symbol
	Exchange1 exchange.VenueID
	Exchange2 exchange.VenueID
	MinGap    float64
	MaxGap    float64
	From      time.Time
	To        time.Time
}

// Analyzer estimates winning rate and distributions from simulation/history.
// Implementations stay behind this interface so formula stats and later ML share one contract.
type Analyzer interface {
	// WinningRate returns an overall (or filtered) empirical win probability in [0,1].
	WinningRate(ctx context.Context, in Input, s strategy.Strategy, r risk.Risk, f Filter) (float64, error)
	// WinningDistribution returns samples keyed by OutcomeDims (symbol, gap, volumes, exchanges, time).
	WinningDistribution(ctx context.Context, in Input, s strategy.Strategy, r risk.Risk, f Filter) (Distribution, error)
}
