// Package sim provides backtest simulation: tick replay through Strategy/Risk → PnL.
package sim

import (
	"context"

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
