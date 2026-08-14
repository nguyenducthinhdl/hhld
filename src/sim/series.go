package sim

import (
	"context"
	"strconv"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/admin"
	"github.com/nguyenducthinhdl/hhld/src/config"
	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/src/pnl"
	"github.com/nguyenducthinhdl/hhld/src/risk"
	"github.com/nguyenducthinhdl/hhld/src/strategy"
	"github.com/nguyenducthinhdl/hhld/src/viz"
)

// TOB is top-of-book for one venue at a replay step.
type TOB struct {
	Venue       string `json:"venue"`
	Ready       bool   `json:"ready"`
	BestBid     string `json:"best_bid,omitempty"`
	BestBidSize string `json:"best_bid_size,omitempty"`
	BestAsk     string `json:"best_ask,omitempty"`
	BestAskSize string `json:"best_ask_size,omitempty"`
}

// StepSignal is a decision or miss at one step.
type StepSignal struct {
	Kind      string        `json:"kind"`
	Reason    string        `json:"reason"`
	TraceID   string        `json:"trace_id,omitempty"`
	Legs      []viz.LegView `json:"legs,omitempty"`
	GapTime   int64         `json:"gap_time,omitempty"`
	Net       float64       `json:"net,omitempty"`
	Venue     string        `json:"venue,omitempty"`
	BuyVenue  string        `json:"buy_venue,omitempty"`
	SellVenue string        `json:"sell_venue,omitempty"`
}

// Step is one time bucket of dual-venue books + gap/signal/PnL.
type Step struct {
	Time          time.Time     `json:"time"`
	Gap           *viz.GapView  `json:"gap,omitempty"`
	VenueA        TOB           `json:"venue_a"`
	VenueB        TOB           `json:"venue_b"`
	Signal        *StepSignal   `json:"signal,omitempty"`
	Explain       *risk.Explain `json:"explain,omitempty"`
	CumulativePnL float64       `json:"cumulative_pnl"`
}

// Series is the payload for GET /sim and POST /sim/run.
type Series struct {
	Symbol       string         `json:"symbol"`
	VenueA       string         `json:"venue_a"`
	VenueB       string         `json:"venue_b"`
	VenuesInFile []string       `json:"venues_in_file"`
	Message      string         `json:"message,omitempty"`
	Config       viz.ConfigView `json:"config"`
	Steps        []Step         `json:"steps"`
	Realized     string         `json:"realized"`
}

// ResolvePair picks venue A/B: overlay, then config, then first two distinct venues in books.
func ResolvePair(cfg config.Config, books []exchange.Book, overlayA, overlayB exchange.VenueID) (exchange.VenueID, exchange.VenueID) {
	a, b := overlayA, overlayB
	if a == "" {
		a = cfg.Venues.A
	}
	if b == "" {
		b = cfg.Venues.B
	}
	found := map[string]struct{}{}
	for _, bk := range books {
		found[string(bk.Venue)] = struct{}{}
	}
	if a != "" && b != "" {
		return a, b
	}
	ids := DistinctVenues(books)
	if a == "" && len(ids) > 0 {
		a = exchange.VenueID(ids[0])
	}
	if b == "" {
		for _, id := range ids {
			if exchange.VenueID(id) != a {
				b = exchange.VenueID(id)
				break
			}
		}
	}
	_ = found
	return a, b
}

func filterPair(in Input, symbol exchange.Symbol, a, b exchange.VenueID) Input {
	out := Input{}
	for _, bk := range in.Books {
		if symbol != "" && bk.Symbol != symbol {
			continue
		}
		if bk.Venue != a && bk.Venue != b {
			continue
		}
		out.Books = append(out.Books, bk)
	}
	for _, tk := range in.Ticks {
		if symbol != "" && tk.Symbol != symbol {
			continue
		}
		if tk.Venue != a && tk.Venue != b {
			continue
		}
		out.Ticks = append(out.Ticks, tk)
	}
	return out
}

func pickSymbol(cfg config.Config, in Input, overlay exchange.Symbol) exchange.Symbol {
	if overlay != "" {
		return overlay
	}
	if syms := cfg.Symbols(); len(syms) > 0 {
		return syms[0]
	}
	if len(in.Books) > 0 {
		return in.Books[0].Symbol
	}
	return ""
}

func tobFrom(b exchange.Book, ready bool) TOB {
	v := TOB{Venue: string(b.Venue), Ready: ready}
	if !ready {
		return v
	}
	if len(b.Bids) > 0 {
		v.BestBid, v.BestBidSize = b.Bids[0].Price, b.Bids[0].Size
	}
	if len(b.Asks) > 0 {
		v.BestAsk, v.BestAskSize = b.Asks[0].Price, b.Asks[0].Size
	}
	return v
}

func bookViewFrom(b exchange.Book, ready bool) viz.BookView {
	v := viz.BookView{Venue: string(b.Venue), Ready: ready, Time: b.Time}
	if !ready {
		return v
	}
	for i, lv := range b.Bids {
		if i >= 10 {
			break
		}
		v.Bids = append(v.Bids, viz.LevelView{Price: lv.Price, Size: lv.Size})
	}
	for i, lv := range b.Asks {
		if i >= 10 {
			break
		}
		v.Asks = append(v.Asks, viz.LevelView{Price: lv.Price, Size: lv.Size})
	}
	return v
}

func legsFrom(d strategy.Decision) []viz.LegView {
	out := make([]viz.LegView, 0, len(d.Legs))
	for _, leg := range d.Legs {
		out = append(out, viz.LegView{
			Venue: string(leg.Venue), Side: string(leg.Side),
			Price: leg.Price, Size: leg.Size,
		})
	}
	return out
}

// Trace replays Input through Strategy/Risk/paper place and emits a per-step series.
func Trace(ctx context.Context, in Input, cfg config.Config) (Series, error) {
	return TracePair(ctx, in, cfg, "", "")
}

// TracePair is Trace with optional venue overlay (empty = ResolvePair).
func TracePair(ctx context.Context, in Input, cfg config.Config, overlayA, overlayB exchange.VenueID) (Series, error) {
	sym := pickSymbol(cfg, in, "")
	if len(cfg.Symbols()) == 0 && sym != "" {
		cfg.UpdateSymbol(sym, func(*config.SymbolEntry) {})
	}
	venuesAll := DistinctVenues(in.Books)
	a, b := ResolvePair(cfg, in.Books, overlayA, overlayB)
	cfg.Venues.A, cfg.Venues.B = a, b
	out := Series{
		Symbol:       string(sym),
		VenueA:       string(a),
		VenueB:       string(b),
		VenuesInFile: venuesAll,
		Config:       viz.ConfigFrom(cfg, sym),
		Steps:        []Step{},
		Realized:     "0",
	}
	if a == "" || b == "" || a == b {
		out.Message = "need two distinct venues in the file or config"
		return out, nil
	}

	filtered := filterPair(in, sym, a, b)
	steps := bookSteps(filtered.Books)
	if len(steps) == 0 {
		out.Message = "no books for the selected symbol and venues"
		return out, nil
	}

	arb := strategy.NewCrossVenueArb(strategy.ArbConfigFrom(cfg))
	gate := risk.NewGate(risk.ParamsFromConfig(cfg))
	params := gate.Params()
	size := cfg.EffectiveSize(sym)
	tracker := pnl.NewMemory()
	aud := admin.NewMemory(tracker)
	venues := strategy.Venues{}
	cum := 0.0

	for _, books := range steps {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		var bookA, bookB exchange.Book
		okA, okB := false, false
		for _, bk := range books {
			if bk.Venue == a {
				bookA, okA = bk, true
			}
			if bk.Venue == b {
				bookB, okB = bk, true
			}
		}
		st := Step{
			Time:   stepTime(books),
			VenueA: tobFrom(bookA, okA),
			VenueB: tobFrom(bookB, okB),
			Gap:    viz.ComputeGap(bookViewFrom(bookA, okA), bookViewFrom(bookB, okB), cfg.MinGapFor(sym)),
		}
		if !okA || !okB {
			st.Signal = &StepSignal{Kind: "miss", Reason: "peer_missing"}
			st.CumulativePnL = cum
			out.Steps = append(out.Steps, st)
			continue
		}
		pair := []exchange.Book{bookA, bookB}
		decisions, err := arb.OnBooks(ctx, pair)
		if err != nil {
			return out, err
		}
		if len(decisions) == 0 {
			st.Signal = &StepSignal{Kind: "miss", Reason: "no_edge"}
			attachExplain(&st, strategy.Decision{}, size, params)
			st.CumulativePnL = cum
			out.Steps = append(out.Steps, st)
			continue
		}
		mkt := risk.MarketView{Books: pair, Now: st.Time}
		sig := &StepSignal{Kind: "miss", Reason: "no_trade"}
		chosen := decisions[0]
		for _, d := range decisions {
			kind, v, placed, err := applyDecision(ctx, d, mkt, gate, venues, aud, tracker)
			if err != nil {
				return out, err
			}
			sig = missSignal(kind, v, d)
			chosen = d
			if placed {
				snap, err := tracker.Snapshot(ctx)
				if err != nil {
					return out, err
				}
				cum, _ = strconv.ParseFloat(snap.Realized, 64)
				sig.Kind = "decision"
				sig.Reason = d.Reason
			}
		}
		st.Signal = sig
		attachExplain(&st, chosen, size, params)
		st.CumulativePnL = cum
		out.Steps = append(out.Steps, st)
	}
	snap, err := tracker.Snapshot(ctx)
	if err != nil {
		return out, err
	}
	out.Realized = snap.Realized
	return out, nil
}

func applyDecision(
	ctx context.Context,
	d strategy.Decision,
	mkt risk.MarketView,
	riskMod risk.Risk,
	venues strategy.Venues,
	aud admin.Auditor,
	tracker pnl.Tracker,
) (kind string, v risk.Verdict, placed bool, err error) {
	var release func()
	if aq, ok := riskMod.(acquirer); ok {
		release, v = aq.TryAcquire(d)
		if !v.OK {
			return "miss", v, false, nil
		}
		defer release()
	}
	v, err = riskMod.Evaluate(ctx, d, mkt)
	if err != nil {
		return "miss", risk.Verdict{Reason: "evaluate_error"}, false, err
	}
	if !v.OK {
		return "miss", v, false, nil
	}
	ensureVenues(venues, d, exchange.NewManualClock(mkt.Now))
	results, _ := strategy.PlaceDecision(ctx, venues, d)
	fees := feeScheduleFrom(riskMod)
	if recErr := admin.RecordPaperDecision(ctx, aud, tracker, d, results, fees); recErr != nil {
		return "", risk.Verdict{}, false, recErr
	}
	return "decision", risk.Verdict{OK: true, Reason: d.Reason}, true, nil
}

func missSignal(kind string, v risk.Verdict, d strategy.Decision) *StepSignal {
	return &StepSignal{
		Kind:      kind,
		Reason:    v.Reason,
		TraceID:   d.TraceID,
		Legs:      legsFrom(d),
		GapTime:   v.GapTimeMS(),
		Net:       v.FloatInfo("net"),
		Venue:     v.StringInfo("venue"),
		BuyVenue:  v.StringInfo("buy_venue"),
		SellVenue: v.StringInfo("sell_venue"),
	}
}

func attachExplain(st *Step, d strategy.Decision, size string, p risk.Params) {
	if ex, ok := risk.ExplainDecision(d, p); ok {
		st.Explain = &ex
		return
	}
	g := st.Gap
	if g == nil || !g.Ready {
		return
	}
	if ex, ok := risk.ExplainQuote(g.BuyVenue, g.BuyAsk, g.SellVenue, g.SellBid, size, p); ok {
		st.Explain = &ex
	}
}
