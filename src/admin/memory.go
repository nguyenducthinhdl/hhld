package admin

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/src/pnl"
	"github.com/nguyenducthinhdl/hhld/src/strategy"
)

// Ensure Memory implements Auditor.
var _ Auditor = (*Memory)(nil)

// Memory is an in-memory order store wired to a PnL tracker for audit.
type Memory struct {
	mu      sync.RWMutex
	orders  []OrderRecord
	tracker pnl.Tracker
}

// NewMemory builds an auditor. If tracker is nil, a new pnl.Memory is used.
func NewMemory(tracker pnl.Tracker) *Memory {
	if tracker == nil {
		tracker = pnl.NewMemory()
	}
	return &Memory{tracker: tracker}
}

func (m *Memory) RecordOrder(ctx context.Context, rec OrderRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if rec.OrderID == "" && rec.ClientOrderID == "" {
		return fmt.Errorf("admin: order id or client order id required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.orders = append(m.orders, rec)
	return nil
}

func (m *Memory) ListOrders(ctx context.Context, f Filter) ([]OrderRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []OrderRecord
	for _, o := range m.orders {
		if f.TraceID != "" && o.TraceID != f.TraceID {
			continue
		}
		if f.HedgeID != "" && o.HedgeID != f.HedgeID {
			continue
		}
		if f.Venue != "" && o.Venue != f.Venue {
			continue
		}
		if f.Symbol != "" && o.Symbol != f.Symbol {
			continue
		}
		if !f.From.IsZero() && o.Time.Before(f.From) {
			continue
		}
		if !f.To.IsZero() && o.Time.After(f.To) {
			continue
		}
		out = append(out, o)
	}
	return out, nil
}

func (m *Memory) PnL(ctx context.Context) (pnl.Snapshot, error) {
	return m.tracker.Snapshot(ctx)
}

func (m *Memory) PnLByHedge(ctx context.Context, hedgeID string) (pnl.Snapshot, error) {
	return m.tracker.SnapshotByHedge(ctx, hedgeID)
}

// Tracker exposes the underlying PnL tracker (for RecordFill).
func (m *Memory) Tracker() pnl.Tracker { return m.tracker }

// RecordPaperDecision persists accepted paper legs as orders + fills so trades are reconstructable.
// Failed legs are stored with status "error" and no fill.
func RecordPaperDecision(ctx context.Context, a Auditor, tracker pnl.Tracker, d strategy.Decision, results []strategy.LegResult) error {
	if tracker == nil {
		return fmt.Errorf("admin: tracker required")
	}
	for _, r := range results {
		status := "error"
		orderID := ""
		clientID := fmt.Sprintf("%s-%d", d.TraceID, r.Index)
		ts := time.Now().UTC()
		if r.Err == nil {
			status = r.Ack.Status
			if status == "" {
				status = "accepted"
			}
			orderID = r.Ack.OrderID
			clientID = r.Ack.ClientOrderID
			if !r.Ack.Time.IsZero() {
				ts = r.Ack.Time
			}
		}
		rec := OrderRecord{
			OrderID:       orderID,
			ClientOrderID: clientID,
			TraceID:       d.TraceID,
			HedgeID:       d.HedgeID,
			Venue:         r.Leg.Venue,
			Symbol:        r.Leg.Symbol,
			Kind:          r.Leg.Kind,
			Side:          r.Leg.Side,
			Price:         r.Leg.Price,
			Size:          r.Leg.Size,
			Status:        status,
			Time:          ts,
		}
		if err := a.RecordOrder(ctx, rec); err != nil {
			return err
		}
		if r.Err != nil {
			continue
		}
		fill := exchange.Fill{
			OrderID:       orderID,
			ClientOrderID: clientID,
			TraceID:       d.TraceID,
			HedgeID:       d.HedgeID,
			Venue:         r.Leg.Venue,
			Symbol:        r.Leg.Symbol,
			Kind:          r.Leg.Kind,
			Side:          r.Leg.Side,
			Price:         r.Leg.Price,
			Size:          r.Leg.Size,
			Fee:           "0",
			Time:          ts,
		}
		if err := tracker.RecordFill(ctx, fill); err != nil {
			return err
		}
	}
	return nil
}
