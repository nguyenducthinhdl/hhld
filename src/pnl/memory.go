package pnl

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/exchange"
)

// Ensure Memory implements Tracker.
var _ Tracker = (*Memory)(nil)

// Memory is an in-memory Tracker: fills are kept for audit; realized PnL uses inventory matching.
type Memory struct {
	mu sync.RWMutex

	fills    []exchange.Fill
	byHedge  map[string][]exchange.Fill
	pos      map[posKey]*Position
	realized map[string]float64 // "" = global; else hedge id
}

type posKey struct {
	hedge  string
	symbol exchange.Symbol
}

// NewMemory builds an empty in-memory PnL tracker.
func NewMemory() *Memory {
	return &Memory{
		byHedge:  make(map[string][]exchange.Fill),
		pos:      make(map[posKey]*Position),
		realized: make(map[string]float64),
	}
}

func (m *Memory) RecordFill(ctx context.Context, f exchange.Fill) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	px, err := strconv.ParseFloat(f.Price, 64)
	if err != nil {
		return fmt.Errorf("pnl: bad price %q: %w", f.Price, err)
	}
	sz, err := strconv.ParseFloat(f.Size, 64)
	if err != nil || sz <= 0 {
		return fmt.Errorf("pnl: bad size %q", f.Size)
	}
	fee := 0.0
	if f.Fee != "" {
		fee, err = strconv.ParseFloat(f.Fee, 64)
		if err != nil {
			return fmt.Errorf("pnl: bad fee %q: %w", f.Fee, err)
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.fills = append(m.fills, f)
	if f.HedgeID != "" {
		m.byHedge[f.HedgeID] = append(m.byHedge[f.HedgeID], f)
	}

	keys := []string{""}
	if f.HedgeID != "" {
		keys = append(keys, f.HedgeID)
	}
	for _, hedge := range keys {
		pk := posKey{hedge: hedge, symbol: f.Symbol}
		p := m.pos[pk]
		if p == nil {
			p = &Position{}
			m.pos[pk] = p
		}
		match, _ := ApplySide(p, f.Side, px, sz)
		m.realized[hedge] += match - fee
	}
	return nil
}

func (m *Memory) Snapshot(ctx context.Context) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return Snapshot{
		Realized:   FormatAmount(m.realized[""]),
		Unrealized: "0",
		AsOf:       time.Now().UTC(),
	}, nil
}

func (m *Memory) SnapshotByHedge(ctx context.Context, hedgeID string) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return Snapshot{
		Realized:   FormatAmount(m.realized[hedgeID]),
		Unrealized: "0",
		AsOf:       time.Now().UTC(),
	}, nil
}

// Fills returns a copy of all recorded fills (audit/reconstruct).
func (m *Memory) Fills() []exchange.Fill {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]exchange.Fill, len(m.fills))
	copy(out, m.fills)
	return out
}

