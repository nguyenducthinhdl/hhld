// Package viz builds read-only market snapshots for the trading dashboard.
package viz

import (
	"strconv"
	"sync"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/config"
	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/src/market"
	"github.com/nguyenducthinhdl/hhld/src/strategy"
)

// Snapshot is the JSON/HTML payload for GET /trading/market.
type Snapshot struct {
	Symbol string      `json:"symbol"`
	AsOf   time.Time   `json:"as_of"`
	VenueA BookView    `json:"venue_a"`
	VenueB BookView    `json:"venue_b"`
	Gap    *GapView    `json:"gap,omitempty"`
	Signal *SignalView `json:"signal,omitempty"`
	Ticks  []TickView  `json:"ticks,omitempty"`
	Config ConfigView  `json:"config"`
}

// BookView is top-of-book depth for one venue.
type BookView struct {
	Venue         string      `json:"venue"`
	NativeSymbol  string      `json:"native_symbol,omitempty"`
	Ready         bool        `json:"ready"`
	BestBid       string      `json:"best_bid,omitempty"`
	BestBidSize   string      `json:"best_bid_size,omitempty"`
	BestAsk       string      `json:"best_ask,omitempty"`
	BestAskSize   string      `json:"best_ask_size,omitempty"`
	Mid           string      `json:"mid,omitempty"`
	Bids          []LevelView `json:"bids,omitempty"`
	Asks          []LevelView `json:"asks,omitempty"`
	Time          time.Time   `json:"time,omitempty"`
	LatencyMs     int64       `json:"latency_ms"`      // ms since last local book update
	ExchangeAgeMs int64       `json:"exchange_age_ms"` // ms since exchange book time (0 if unknown)
}

// LevelView is one book level for display.
type LevelView struct {
	Price string `json:"price"`
	Size  string `json:"size"`
}

// GapView is the cross-venue arb gap (display-only).
type GapView struct {
	Ready     bool    `json:"ready"`
	Value     float64 `json:"value"`
	BuyVenue  string  `json:"buy_venue,omitempty"`
	BuyAsk    string  `json:"buy_ask,omitempty"`
	SellVenue string  `json:"sell_venue,omitempty"`
	SellBid   string  `json:"sell_bid,omitempty"`
	AboveMin  bool    `json:"above_min_gap"`
}

// SignalView is the last Decision or miss.
type SignalView struct {
	Kind      string    `json:"kind"` // decision | miss
	Reason    string    `json:"reason"`
	TraceID   string    `json:"trace_id,omitempty"`
	GrossGap  float64   `json:"gross_gap,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
	Legs      []LegView `json:"legs,omitempty"`
	// GapTime is book age in milliseconds when Reason is stale_book.
	GapTime   int64   `json:"gap_time,omitempty"`
	Net       float64 `json:"net,omitempty"`
	Venue     string  `json:"venue,omitempty"`
	BuyVenue  string  `json:"buy_venue,omitempty"`
	SellVenue string  `json:"sell_venue,omitempty"`
}

// LegView is one decision leg for the UI.
type LegView struct {
	Venue string `json:"venue"`
	Side  string `json:"side"`
	Price string `json:"price"`
	Size  string `json:"size"`
}

// TickView is a recent trade tick.
type TickView struct {
	Venue string    `json:"venue"`
	Price string    `json:"price"`
	Size  string    `json:"size"`
	Side  string    `json:"side,omitempty"`
	Time  time.Time `json:"time"`
}

// SideFeeView is one buy or sell fee schedule for display.
type SideFeeView struct {
	RateBps         float64 `json:"rate_bps"`
	Fixed           float64 `json:"fixed"`
	CommissionBps   float64 `json:"commission_bps"`
	CommissionFixed float64 `json:"commission_fixed"`
}

// FeeView is one venue's buy/sell fee schedule for display.
type FeeView struct {
	Buy  SideFeeView `json:"buy"`
	Sell SideFeeView `json:"sell"`
}

// ConfigView is read-only trading/risk knobs that drive signals.
type ConfigView struct {
	VenueA            string             `json:"venue_a"`
	VenueB            string             `json:"venue_b"`
	MinSize           string             `json:"min_size"`
	MaxSize           string             `json:"max_size"`
	EffectiveSize     string             `json:"effective_size"`
	MinGap            float64            `json:"min_gap"`
	OrderInterval     string             `json:"order_interval"`
	FeeBpsPerLeg      float64            `json:"fee_bps_per_leg"`
	FeesByVenue       map[string]FeeView `json:"fees_by_venue"`
	LatencyPenalty    float64            `json:"latency_penalty"`
	PartialFillFactor float64            `json:"partial_fill_factor"`
	MaxBookAge        string             `json:"max_book_age"`
	MaxInFlight       int                `json:"max_in_flight"`
	Budgets           map[string]string  `json:"budgets"`
}

// Source gathers live state for Snapshot.
type Source struct {
	Cfg     config.Config
	Store   *market.BookStore
	Depth   int
	Signals *SignalLog
	Ticks   *TickRing
}

// BuildSnapshot builds a dashboard snapshot for symbol.
func (s Source) BuildSnapshot(symbol exchange.Symbol) Snapshot {
	if symbol == "" {
		if syms := s.Cfg.Symbols(); len(syms) > 0 {
			symbol = syms[0]
		}
	}
	depth := s.Depth
	if depth <= 0 {
		depth = 10
	}
	now := time.Now().UTC()
	va := s.Cfg.Venues.A
	vb := s.Cfg.Venues.B
	out := Snapshot{
		Symbol: string(symbol),
		AsOf:   now,
		VenueA: bookView(s.Store, s.Cfg, va, symbol, depth, now),
		VenueB: bookView(s.Store, s.Cfg, vb, symbol, depth, now),
		Config: ConfigFrom(s.Cfg, symbol),
	}
	out.Gap = ComputeGap(out.VenueA, out.VenueB, s.Cfg.MinGapFor(symbol))
	if s.Signals != nil {
		if sig, ok := s.Signals.Latest(symbol); ok {
			cp := sig
			out.Signal = &cp
		}
	}
	if s.Ticks != nil {
		out.Ticks = s.Ticks.List(symbol)
	}
	return out
}

func bookView(store *market.BookStore, cfg config.Config, venue exchange.VenueID, sym exchange.Symbol, depth int, now time.Time) BookView {
	v := BookView{Venue: string(venue)}
	if n, err := cfg.NativeSymbol(venue, sym); err == nil {
		v.NativeSymbol = n
	}
	if store == nil {
		return v
	}
	b, ok := store.Get(venue, sym)
	if !ok {
		return v
	}
	v.Ready = true
	v.Time = b.Time
	v.Bids = levels(b.Bids, depth)
	v.Asks = levels(b.Asks, depth)
	if len(v.Bids) > 0 {
		v.BestBid = v.Bids[0].Price
		v.BestBidSize = v.Bids[0].Size
	}
	if len(v.Asks) > 0 {
		v.BestAsk = v.Asks[0].Price
		v.BestAskSize = v.Asks[0].Size
	}
	if bid, err1 := strconv.ParseFloat(v.BestBid, 64); err1 == nil {
		if ask, err2 := strconv.ParseFloat(v.BestAsk, 64); err2 == nil {
			v.Mid = strconv.FormatFloat((bid+ask)/2, 'f', -1, 64)
		}
	}
	if updated, ok := store.LastUpdated(venue, sym); ok {
		v.LatencyMs = now.Sub(updated).Milliseconds()
		if v.LatencyMs < 0 {
			v.LatencyMs = 0
		}
	}
	if !b.Time.IsZero() {
		age := now.Sub(b.Time)
		if age >= 0 && age < 24*time.Hour {
			v.ExchangeAgeMs = age.Milliseconds()
		}
	}
	return v
}

func levels(in []exchange.Level, depth int) []LevelView {
	if len(in) > depth {
		in = in[:depth]
	}
	out := make([]LevelView, 0, len(in))
	for _, lv := range in {
		out = append(out, LevelView{Price: lv.Price, Size: lv.Size})
	}
	return out
}

// ComputeGap derives cross-venue gap from two book views.
func ComputeGap(a, b BookView, minGap float64) *GapView {
	g := &GapView{}
	if !a.Ready || !b.Ready || len(a.Asks) == 0 || len(a.Bids) == 0 || len(b.Asks) == 0 || len(b.Bids) == 0 {
		return g
	}
	askA, err1 := strconv.ParseFloat(a.Asks[0].Price, 64)
	bidA, err2 := strconv.ParseFloat(a.Bids[0].Price, 64)
	askB, err3 := strconv.ParseFloat(b.Asks[0].Price, 64)
	bidB, err4 := strconv.ParseFloat(b.Bids[0].Price, 64)
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
		return g
	}
	// Buy lowest ask, sell highest bid (same as CrossVenueArb).
	buyVenue, buyAsk, buyAskPx := a.Venue, a.Asks[0].Price, askA
	sellVenue, sellBid, sellBidPx := a.Venue, a.Bids[0].Price, bidA
	if askB < buyAskPx {
		buyVenue, buyAsk, buyAskPx = b.Venue, b.Asks[0].Price, askB
	}
	if bidB > sellBidPx {
		sellVenue, sellBid, sellBidPx = b.Venue, b.Bids[0].Price, bidB
	}
	if buyVenue == sellVenue {
		return g
	}
	gap := sellBidPx - buyAskPx
	g.Ready = true
	g.Value = gap
	g.BuyVenue = buyVenue
	g.BuyAsk = buyAsk
	g.SellVenue = sellVenue
	g.SellBid = sellBid
	g.AboveMin = gap >= minGap
	return g
}

// ConfigFrom projects config knobs for the dashboard (no secrets).
func ConfigFrom(cfg config.Config, symbol exchange.Symbol) ConfigView {
	entry, ok := cfg.Lookup(symbol)
	if !ok && len(cfg.SymbolMap) > 0 {
		entry = cfg.SymbolMap[0]
		ok = true
	}
	interval := cfg.OrderIntervalFor(entry.Symbol)
	intervalStr := ""
	if interval > 0 {
		intervalStr = interval.String()
	}
	fees := map[string]FeeView{}
	budgets := map[string]string{}
	if ok {
		for venue, spec := range entry.Venues {
			fees[venue] = FeeView{
				Buy:  sideFeeView(spec.Fees.Buy),
				Sell: sideFeeView(spec.Fees.Sell),
			}
			if spec.Budget != "" {
				budgets[config.BudgetKey(exchange.VenueID(venue), entry.Symbol)] = spec.Budget
			}
		}
	}
	minSize, maxSize, minGap, feeBps, lat, pff, maxAge := "", "", 0.0, 0.0, 0.0, 0.0, ""
	if ok {
		minSize = entry.Trading.MinSize
		maxSize = entry.Trading.MaxSize
		minGap = entry.Trading.MinGap
		feeBps = entry.Risk.FeeBpsPerLeg
		lat = entry.Risk.LatencyPenalty
		pff = entry.Risk.PartialFillFactor
		maxAge = entry.Risk.MaxBookAge.Duration().String()
	}
	return ConfigView{
		VenueA:            string(cfg.Venues.A),
		VenueB:            string(cfg.Venues.B),
		MinSize:           minSize,
		MaxSize:           maxSize,
		EffectiveSize:     cfg.EffectiveSize(entry.Symbol),
		MinGap:            minGap,
		OrderInterval:     intervalStr,
		FeeBpsPerLeg:      feeBps,
		FeesByVenue:       fees,
		LatencyPenalty:    lat,
		PartialFillFactor: pff,
		MaxBookAge:        maxAge,
		MaxInFlight:       cfg.Risk.MaxInFlight,
		Budgets:           budgets,
	}
}

func sideFeeView(f config.SideFee) SideFeeView {
	return SideFeeView{
		RateBps: f.RateBps, Fixed: f.Fixed,
		CommissionBps: f.CommissionBps, CommissionFixed: f.CommissionFixed,
	}
}

// SignalLog keeps the latest signal per symbol (implements market.SignalNotifier).
type SignalLog struct {
	mu    sync.RWMutex
	bySym map[exchange.Symbol]SignalView
}

// NewSignalLog builds an empty signal log.
func NewSignalLog() *SignalLog {
	return &SignalLog{bySym: make(map[exchange.Symbol]SignalView)}
}

// NotifyDecision records an accepted strategy decision.
func (l *SignalLog) NotifyDecision(sym exchange.Symbol, d strategy.Decision, grossGap float64) {
	legs := make([]LegView, 0, len(d.Legs))
	for _, leg := range d.Legs {
		legs = append(legs, LegView{
			Venue: string(leg.Venue), Side: string(leg.Side),
			Price: leg.Price, Size: leg.Size,
		})
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.bySym[sym] = SignalView{
		Kind: "decision", Reason: d.Reason, TraceID: d.TraceID,
		GrossGap: grossGap, CheckedAt: time.Now().UTC(), Legs: legs,
	}
}

// NotifyMiss records a risk/peer miss reason. info may include gap_time (ms) for stale_book.
func (l *SignalLog) NotifyMiss(sym exchange.Symbol, reason string, info map[string]any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.bySym[sym] = SignalView{
		Kind: "miss", Reason: reason, CheckedAt: time.Now().UTC(),
		GapTime:   gapTimeMS(info),
		Net:       floatInfo(info, "net"),
		Venue:     stringInfo(info, "venue"),
		BuyVenue:  stringInfo(info, "buy_venue"),
		SellVenue: stringInfo(info, "sell_venue"),
	}
}

func gapTimeMS(info map[string]any) int64 {
	return int64(floatInfo(info, "gap_time"))
}

func floatInfo(info map[string]any, key string) float64 {
	if info == nil {
		return 0
	}
	switch n := info[key].(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int64:
		return float64(n)
	case int:
		return float64(n)
	default:
		return 0
	}
}

func stringInfo(info map[string]any, key string) string {
	if info == nil {
		return ""
	}
	s, _ := info[key].(string)
	return s
}

// Latest returns the last signal for symbol.
func (l *SignalLog) Latest(sym exchange.Symbol) (SignalView, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	v, ok := l.bySym[sym]
	return v, ok
}

// TickRing keeps recent ticks per symbol.
type TickRing struct {
	mu    sync.Mutex
	max   int
	bySym map[exchange.Symbol][]TickView
}

// NewTickRing keeps up to max ticks per symbol.
func NewTickRing(max int) *TickRing {
	if max <= 0 {
		max = 32
	}
	return &TickRing{max: max, bySym: make(map[exchange.Symbol][]TickView)}
}

// Push appends a tick.
func (r *TickRing) Push(t exchange.Tick) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v := TickView{
		Venue: string(t.Venue), Price: t.Price, Size: t.Size,
		Side: string(t.Side), Time: t.Time,
	}
	list := append(r.bySym[t.Symbol], v)
	if len(list) > r.max {
		list = list[len(list)-r.max:]
	}
	r.bySym[t.Symbol] = list
}

// List returns ticks for symbol (oldest first).
func (r *TickRing) List(sym exchange.Symbol) []TickView {
	r.mu.Lock()
	defer r.mu.Unlock()
	src := r.bySym[sym]
	out := make([]TickView, len(src))
	copy(out, src)
	return out
}
