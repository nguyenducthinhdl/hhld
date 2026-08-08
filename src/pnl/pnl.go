// Package pnl tracks profit and loss for paper and (later) live trading.
package pnl

import (
	"context"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/exchange"
)

// Snapshot is a point-in-time PnL view. Amounts are decimal strings.
type Snapshot struct {
	Realized   string
	Unrealized string
	AsOf       time.Time
}

// Tracker records fills and exposes PnL, including by hedge id for linked legs.
type Tracker interface {
	RecordFill(ctx context.Context, f exchange.Fill) error
	Snapshot(ctx context.Context) (Snapshot, error)
	SnapshotByHedge(ctx context.Context, hedgeID string) (Snapshot, error)
}
