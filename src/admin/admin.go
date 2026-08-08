// Package admin provides auditable views of orders and PnL.
package admin

import (
	"context"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/src/pnl"
)

// OrderRecord is a persisted, auditable order (paper or live).
type OrderRecord struct {
	OrderID       string
	ClientOrderID string
	TraceID       string
	HedgeID       string
	Venue         exchange.VenueID
	Symbol        exchange.Symbol
	Kind          exchange.Kind
	Side          exchange.Side
	Price         string
	Size          string
	Status        string
	Time          time.Time
}

// Filter narrows ListOrders queries. Empty fields are ignored.
type Filter struct {
	TraceID string
	HedgeID string
	Venue   exchange.VenueID
	Symbol  exchange.Symbol
	From    time.Time
	To      time.Time
}

// Auditor stores and retrieves orders and PnL for audit / mismatch tracing.
type Auditor interface {
	RecordOrder(ctx context.Context, rec OrderRecord) error
	ListOrders(ctx context.Context, f Filter) ([]OrderRecord, error)
	PnL(ctx context.Context) (pnl.Snapshot, error)
	PnLByHedge(ctx context.Context, hedgeID string) (pnl.Snapshot, error)
}
