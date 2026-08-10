package risk

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/config"
	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/src/strategy"
)

// Ensure Gate implements Risk.
var _ Risk = (*Gate)(nil)

// Gate is the miss-more Risk implementation: edge, staleness, health, serial keys, in-flight cap.
type Gate struct {
	params Params

	mu        sync.Mutex
	inFlight  int
	held      map[string]struct{} // lock keys currently in a Risk+exec pipeline
	spent     map[string]float64  // notional spent per venue/symbol
	lastPlace map[exchange.Symbol]time.Time
}

// NewGate builds a Gate from Params.
func NewGate(p Params) *Gate {
	if p.PartialFillFactor <= 0 || p.PartialFillFactor > 1 {
		p.PartialFillFactor = 1
	}
	if p.MaxInFlight <= 0 {
		p.MaxInFlight = 4
	}
	if p.Budgets == nil {
		p.Budgets = map[string]float64{}
	}
	if p.OrderInterval == nil {
		p.OrderInterval = map[exchange.Symbol]time.Duration{}
	}
	if p.MaxVolumeTrade == nil {
		p.MaxVolumeTrade = map[exchange.Symbol]float64{}
	}
	return &Gate{
		params:    p,
		held:      make(map[string]struct{}),
		spent:     make(map[string]float64),
		lastPlace: make(map[exchange.Symbol]time.Time),
	}
}

// TryAcquire reserves the decision lock key and an in-flight slot.
// On OK, caller must Release after Evaluate + place/reconcile (or after rejecting place).
// On miss (busy/overload), Verdict.OK is false and release is nil.
func (g *Gate) TryAcquire(d strategy.Decision) (release func(), v Verdict) {
	key := LockKey(d)
	g.mu.Lock()
	defer g.mu.Unlock()

	if _, busy := g.held[key]; busy {
		return nil, Verdict{OK: false, Reason: "lock_busy"}
	}
	if g.inFlight >= g.params.MaxInFlight {
		return nil, Verdict{OK: false, Reason: "overloaded"}
	}
	g.held[key] = struct{}{}
	g.inFlight++

	var once sync.Once
	return func() {
		once.Do(func() {
			g.mu.Lock()
			defer g.mu.Unlock()
			delete(g.held, key)
			g.inFlight--
		})
	}, Verdict{OK: true, Reason: "acquired"}
}

// Evaluate applies miss-more economic and market-data gates (does not acquire locks).
// Call TryAcquire before Evaluate+Place for concurrency rules.
func (g *Gate) Evaluate(ctx context.Context, d strategy.Decision, mkt MarketView) (Verdict, error) {
	if err := ctx.Err(); err != nil {
		return Verdict{}, err
	}
	if len(d.Legs) == 0 {
		return Verdict{OK: false, Reason: "no legs"}, nil
	}
	for _, leg := range d.Legs {
		if strings.TrimSpace(leg.Price) == "" || strings.TrimSpace(leg.Size) == "" {
			return Verdict{OK: false, Reason: "missing price or size"}, nil
		}
	}
	if d.HedgeID != "" && len(d.Legs) < 2 {
		return Verdict{OK: false, Reason: "hedge requires >= 2 legs"}, nil
	}

	for _, leg := range d.Legs {
		if mkt.Unhealthy[leg.Venue] {
			return Verdict{OK: false, Reason: "venue_unhealthy:" + string(leg.Venue)}, nil
		}
	}

	now := mkt.Now
	if now.IsZero() {
		now = timeNow()
	}
	if g.params.MaxBookAge > 0 && len(mkt.Books) > 0 {
		needed := map[exchange.VenueID]exchange.Symbol{}
		for _, leg := range d.Legs {
			needed[leg.Venue] = leg.Symbol
		}
		for venue, sym := range needed {
			book, ok := findBook(mkt.Books, venue, sym)
			if !ok {
				return Verdict{OK: false, Reason: "missing_book:" + string(venue)}, nil
			}
			if book.Time.IsZero() || now.Sub(book.Time) > g.params.MaxBookAge {
				return Verdict{OK: false, Reason: "stale_book:" + string(venue)}, nil
			}
		}
	}

	if ok, reason := g.edgeSurvives(d); !ok {
		return Verdict{OK: false, Reason: reason}, nil
	}
	if ok, reason := g.checkLimits(d, now); !ok {
		return Verdict{OK: false, Reason: reason}, nil
	}
	return Verdict{OK: true, Reason: "ok"}, nil
}

// checkLimits enforces max volume, per-symbol rate limit, and notional budgets.
// On accept, charges spent notional and updates lastPlace (process lifetime).
func (g *Gate) checkLimits(d strategy.Decision, now time.Time) (bool, string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	for _, leg := range d.Legs {
		sz, err := strconv.ParseFloat(leg.Size, 64)
		if err != nil || sz <= 0 {
			return false, "bad size"
		}
		if max, ok := g.params.MaxVolumeTrade[leg.Symbol]; ok && max > 0 && sz > max+1e-12 {
			return false, "max_volume_exceeded"
		}
	}

	syms := map[exchange.Symbol]struct{}{}
	for _, leg := range d.Legs {
		syms[leg.Symbol] = struct{}{}
	}
	for sym := range syms {
		interval, ok := g.params.OrderInterval[sym]
		if !ok || interval <= 0 {
			continue
		}
		if last, seen := g.lastPlace[sym]; seen && now.Sub(last) < interval {
			return false, "rate_limited"
		}
	}

	type add struct {
		key string
		n   float64
	}
	var adds []add
	for _, leg := range d.Legs {
		px, err := strconv.ParseFloat(leg.Price, 64)
		if err != nil {
			return false, "bad price"
		}
		sz, err := strconv.ParseFloat(leg.Size, 64)
		if err != nil {
			return false, "bad size"
		}
		key := config.BudgetKey(leg.Venue, leg.Symbol)
		notional := px * sz
		if budget, ok := g.params.Budgets[key]; ok && budget > 0 {
			if g.spent[key]+notional > budget+1e-9 {
				return false, "budget_exceeded:" + key
			}
		}
		adds = append(adds, add{key: key, n: notional})
	}

	for _, a := range adds {
		g.spent[a.key] += a.n
	}
	for sym := range syms {
		g.lastPlace[sym] = now
	}
	return true, "ok"
}

func (g *Gate) edgeSurvives(d strategy.Decision) (bool, string) {
	if len(d.Legs) < 2 {
		// Single-leg: only fee/size sanity; no cross-edge model yet.
		return true, "ok"
	}
	var buy, sell *strategy.Leg
	for i := range d.Legs {
		leg := &d.Legs[i]
		switch leg.Side {
		case exchange.SideBuy:
			if buy == nil {
				buy = leg
			}
		case exchange.SideSell:
			if sell == nil {
				sell = leg
			}
		}
	}
	if buy == nil || sell == nil {
		return false, "need buy and sell legs"
	}
	buyPx, err := strconv.ParseFloat(buy.Price, 64)
	if err != nil {
		return false, "bad buy price"
	}
	sellPx, err := strconv.ParseFloat(sell.Price, 64)
	if err != nil {
		return false, "bad sell price"
	}
	size, err := strconv.ParseFloat(buy.Size, 64)
	if err != nil || size <= 0 {
		return false, "bad size"
	}
	sellSize, err := strconv.ParseFloat(sell.Size, 64)
	if err != nil || sellSize <= 0 {
		return false, "bad size"
	}
	if sellSize < size {
		size = sellSize
	}
	size *= g.params.PartialFillFactor

	gross := (sellPx - buyPx) * size
	fees := g.params.FeeSchedule()
	fee := fees.Cost(buy.Venue, buyPx, size) + fees.Cost(sell.Venue, sellPx, size)
	net := gross - fee - g.params.LatencyPenalty*size
	if net <= 0 {
		return false, fmt.Sprintf("negative_edge net=%.6f", net)
	}
	return true, "ok"
}

// Params returns a copy of the gate parameters (including fee schedule for paper fills).
func (g *Gate) Params() Params {
	return g.params
}

// LockKey returns the concurrency key: HedgeID if set, else arb key from symbol+venues.
func LockKey(d strategy.Decision) string {
	if d.HedgeID != "" {
		return "hedge:" + d.HedgeID
	}
	if len(d.Legs) == 0 {
		return "trace:" + d.TraceID
	}
	sym := d.Legs[0].Symbol
	venues := make([]string, 0, len(d.Legs))
	seen := map[exchange.VenueID]struct{}{}
	for _, leg := range d.Legs {
		if _, ok := seen[leg.Venue]; ok {
			continue
		}
		seen[leg.Venue] = struct{}{}
		venues = append(venues, string(leg.Venue))
	}
	sort.Strings(venues)
	return fmt.Sprintf("arb:%s:%s", sym, strings.Join(venues, "|"))
}

func findBook(books []exchange.Book, venue exchange.VenueID, sym exchange.Symbol) (exchange.Book, bool) {
	for _, b := range books {
		if b.Venue == venue && b.Symbol == sym {
			return b, true
		}
	}
	return exchange.Book{}, false
}

// timeNow is replaceable in tests.
var timeNow = func() time.Time { return time.Now().UTC() }
