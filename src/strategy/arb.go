package strategy

import (
	"context"
	"fmt"
	"strconv"
	"sync"

	"github.com/nguyenducthinhdl/hhld/src/exchange"
)

// ArbConfig configures cross-venue paper arbitrage for one or more symbols.
type ArbConfig struct {
	// Symbols to evaluate (BTCUSD first; multi-symbol via config).
	Symbols []exchange.Symbol
	// Size is the order size string applied to each leg.
	Size string
	// MinGap is the minimum (sellBid - buyAsk) required to emit a Decision.
	MinGap float64
}

// CrossVenueArb detects gaps between two venue books and emits two-leg Decisions.
type CrossVenueArb struct {
	cfg ArbConfig

	mu  sync.Mutex
	seq uint64
}

// NewCrossVenueArb builds an arb strategy. Defaults Size to "1" if empty.
func NewCrossVenueArb(cfg ArbConfig) *CrossVenueArb {
	if cfg.Size == "" {
		cfg.Size = "1"
	}
	return &CrossVenueArb{cfg: cfg}
}

func (a *CrossVenueArb) Name() string { return "cross-venue-arb" }

// OnBooks looks for same-symbol books on different venues and emits buy-low / sell-high legs.
func (a *CrossVenueArb) OnBooks(ctx context.Context, books []exchange.Book) ([]Decision, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	want := make(map[exchange.Symbol]struct{}, len(a.cfg.Symbols))
	for _, s := range a.cfg.Symbols {
		want[s] = struct{}{}
	}

	bySymbol := map[exchange.Symbol][]exchange.Book{}
	for _, b := range books {
		if len(want) > 0 {
			if _, ok := want[b.Symbol]; !ok {
				continue
			}
		}
		bySymbol[b.Symbol] = append(bySymbol[b.Symbol], b)
	}

	var out []Decision
	for sym, group := range bySymbol {
		if len(group) < 2 {
			continue
		}
		d, ok, err := a.decisionForSymbol(sym, group)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, d)
		}
	}
	return out, nil
}

func (a *CrossVenueArb) decisionForSymbol(sym exchange.Symbol, books []exchange.Book) (Decision, bool, error) {
	type quote struct {
		book exchange.Book
		ask  float64
		bid  float64
	}
	quotes := make([]quote, 0, len(books))
	for _, b := range books {
		if len(b.Asks) == 0 || len(b.Bids) == 0 {
			continue
		}
		ask, err := strconv.ParseFloat(b.Asks[0].Price, 64)
		if err != nil {
			return Decision{}, false, fmt.Errorf("arb: parse ask %q on %s: %w", b.Asks[0].Price, b.Venue, err)
		}
		bid, err := strconv.ParseFloat(b.Bids[0].Price, 64)
		if err != nil {
			return Decision{}, false, fmt.Errorf("arb: parse bid %q on %s: %w", b.Bids[0].Price, b.Venue, err)
		}
		quotes = append(quotes, quote{book: b, ask: ask, bid: bid})
	}
	if len(quotes) < 2 {
		return Decision{}, false, nil
	}

	buy := quotes[0]
	sell := quotes[0]
	for _, q := range quotes[1:] {
		if q.ask < buy.ask {
			buy = q
		}
		if q.bid > sell.bid {
			sell = q
		}
	}
	if buy.book.Venue == sell.book.Venue {
		return Decision{}, false, nil
	}
	gap := sell.bid - buy.ask
	if gap < a.cfg.MinGap {
		return Decision{}, false, nil
	}

	a.mu.Lock()
	a.seq++
	trace := fmt.Sprintf("arb-%s-%d", sym, a.seq)
	a.mu.Unlock()
	return Decision{
		TraceID: trace,
		Legs: []Leg{
			{
				Venue:  buy.book.Venue,
				Symbol: sym,
				Kind:   buy.book.Kind,
				Side:   exchange.SideBuy,
				Price:  buy.book.Asks[0].Price,
				Size:   a.cfg.Size,
			},
			{
				Venue:  sell.book.Venue,
				Symbol: sym,
				Kind:   sell.book.Kind,
				Side:   exchange.SideSell,
				Price:  sell.book.Bids[0].Price,
				Size:   a.cfg.Size,
			},
		},
		Reason: fmt.Sprintf("gap=%.4f >= min=%.4f", gap, a.cfg.MinGap),
	}, true, nil
}

// Ensure CrossVenueArb implements Strategy.
var _ Strategy = (*CrossVenueArb)(nil)
