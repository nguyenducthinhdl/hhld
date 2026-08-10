package market

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/admin"
	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/src/pnl"
	"github.com/nguyenducthinhdl/hhld/src/risk"
	"github.com/nguyenducthinhdl/hhld/src/strategy"
)

type acquirer interface {
	TryAcquire(d strategy.Decision) (release func(), v risk.Verdict)
}

type feeParams interface {
	Params() risk.Params
}

// RunnerConfig wires dual venues and the trading pipeline.
type RunnerConfig struct {
	VenueA  exchange.VenueID
	VenueB  exchange.VenueID
	Symbols []exchange.Symbol
	Store   *BookStore
	Strategy strategy.Strategy
	Risk    risk.Risk
	Venues  strategy.Venues // paper place map
	Auditor admin.Auditor
	Tracker pnl.Tracker
}

// Runner applies bus events to the BookStore and evaluates Strategy on every update
// once both venues have a book for the symbol.
type Runner struct {
	cfg RunnerConfig

	mu       sync.Mutex
	onBooksN int // test counter: successful OnBooks calls
	missPeer int
}

// NewRunner builds a runner. Store must be non-nil.
func NewRunner(cfg RunnerConfig) (*Runner, error) {
	if cfg.Store == nil {
		return nil, fmt.Errorf("market: store required")
	}
	if cfg.VenueA == "" || cfg.VenueB == "" {
		return nil, fmt.Errorf("market: venue A and B required")
	}
	if cfg.Strategy == nil || cfg.Risk == nil {
		return nil, fmt.Errorf("market: strategy and risk required")
	}
	if cfg.Tracker == nil {
		cfg.Tracker = pnl.NewMemory()
	}
	if cfg.Auditor == nil {
		cfg.Auditor = admin.NewMemory(cfg.Tracker)
	}
	if cfg.Venues == nil {
		cfg.Venues = strategy.Venues{}
	}
	return &Runner{cfg: cfg}, nil
}

// AttachBus subscribes the runner to bus events.
func (r *Runner) AttachBus(bus *Bus) {
	bus.Subscribe(r.OnEvent)
}

// OnEvent applies the event then maybe evaluates.
func (r *Runner) OnEvent(ev BookEvent) {
	_, err := r.cfg.Store.Apply(ev)
	if err != nil {
		return // miss: bad delta / seq — do not evaluate
	}
	_, sym := ev.VenueSymbol()
	if sym == "" {
		return
	}
	if !r.watches(sym) {
		return
	}
	r.evaluate(sym)
}

func (r *Runner) watches(sym exchange.Symbol) bool {
	if len(r.cfg.Symbols) == 0 {
		return true
	}
	for _, s := range r.cfg.Symbols {
		if s == sym {
			return true
		}
	}
	return false
}

func (r *Runner) evaluate(sym exchange.Symbol) {
	bookA, okA := r.cfg.Store.Get(r.cfg.VenueA, sym)
	bookB, okB := r.cfg.Store.Get(r.cfg.VenueB, sym)
	if !okA || !okB {
		r.mu.Lock()
		r.missPeer++
		r.mu.Unlock()
		return
	}
	books := []exchange.Book{bookA, bookB}
	ctx := context.Background()
	decisions, err := r.cfg.Strategy.OnBooks(ctx, books)
	if err != nil {
		return
	}
	r.mu.Lock()
	r.onBooksN++
	r.mu.Unlock()

	mkt := risk.MarketView{Books: books, Now: maxBookTime(books)}
	for _, d := range decisions {
		r.execute(ctx, d, mkt)
	}
}

func (r *Runner) execute(ctx context.Context, d strategy.Decision, mkt risk.MarketView) {
	var release func()
	if aq, ok := r.cfg.Risk.(acquirer); ok {
		var v risk.Verdict
		release, v = aq.TryAcquire(d)
		if !v.OK {
			return
		}
		defer release()
	}
	v, err := r.cfg.Risk.Evaluate(ctx, d, mkt)
	if err != nil || !v.OK {
		return
	}
	results, _ := strategy.PlaceDecision(ctx, r.cfg.Venues, d)
	fees := risk.FeeSchedule{}
	if p, ok := r.cfg.Risk.(feeParams); ok {
		fees = p.Params().FeeSchedule()
	}
	_ = admin.RecordPaperDecision(ctx, r.cfg.Auditor, r.cfg.Tracker, d, results, fees)
}

// OnBooksCalls returns how many times OnBooks was invoked (tests).
func (r *Runner) OnBooksCalls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.onBooksN
}

// PeerMisses returns how many evaluates were skipped for missing peer book.
func (r *Runner) PeerMisses() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.missPeer
}

func maxBookTime(books []exchange.Book) time.Time {
	var maxT time.Time
	for _, b := range books {
		if b.Time.After(maxT) {
			maxT = b.Time
		}
	}
	return maxT
}
