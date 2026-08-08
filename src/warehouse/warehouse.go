// Package warehouse stores crawled market data for backtest replay.
package warehouse

import (
	"context"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/exchange"
)

// Store persists and queries normalized market data for backtesting.
type Store interface {
	AppendBook(ctx context.Context, b exchange.Book) error
	AppendTick(ctx context.Context, t exchange.Tick) error
	QueryBooks(ctx context.Context, symbol exchange.Symbol, from, to time.Time) ([]exchange.Book, error)
	QueryTicks(ctx context.Context, symbol exchange.Symbol, from, to time.Time) ([]exchange.Tick, error)
}
