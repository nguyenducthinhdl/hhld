package sim

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/admin"
	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/src/exchange/fake"
	"github.com/nguyenducthinhdl/hhld/src/pnl"
	"github.com/nguyenducthinhdl/hhld/src/risk"
	"github.com/nguyenducthinhdl/hhld/src/strategy"
)

// Ensure Replay implements Simulator and Analyzer.
var _ Simulator = (*Replay)(nil)
var _ Analyzer = (*Replay)(nil)

type acquirer interface {
	TryAcquire(d strategy.Decision) (release func(), v risk.Verdict)
}

type feeParams interface {
	Params() risk.Params
}

// Replay backtests by replaying book snapshots through Strategy → Risk → paper place → PnL.
type Replay struct {
	clock exchange.Clock
}

// NewReplay builds a simulator/analyzer. clock may be nil (ManualClock at Unix 0).
func NewReplay(clock exchange.Clock) *Replay {
	if clock == nil {
		clock = exchange.NewManualClock(time.Unix(0, 0).UTC())
	}
	return &Replay{clock: clock}
}

// Run replays Input books (ticks reserved for later), applying strategy/risk and recording fills on t.
func (r *Replay) Run(ctx context.Context, in Input, s strategy.Strategy, riskMod risk.Risk, t pnl.Tracker) (pnl.Snapshot, error) {
	if s == nil || riskMod == nil || t == nil {
		return pnl.Snapshot{}, fmt.Errorf("sim: strategy, risk, and tracker are required")
	}
	venues := strategy.Venues{}
	aud := admin.NewMemory(t)
	if _, err := r.replay(ctx, in, s, riskMod, venues, aud, t, nil); err != nil {
		return pnl.Snapshot{}, err
	}
	return t.Snapshot(ctx)
}

// WinningRate returns empirical win rate over decisions that passed risk and were placed.
func (r *Replay) WinningRate(ctx context.Context, in Input, s strategy.Strategy, riskMod risk.Risk, f Filter) (float64, error) {
	dist, err := r.WinningDistribution(ctx, in, s, riskMod, f)
	if err != nil {
		return 0, err
	}
	return dist.WinRate, nil
}

// WinningDistribution returns per-decision samples with OutcomeDims after a full replay on a fresh tracker.
func (r *Replay) WinningDistribution(ctx context.Context, in Input, s strategy.Strategy, riskMod risk.Risk, f Filter) (Distribution, error) {
	if s == nil || riskMod == nil {
		return Distribution{}, fmt.Errorf("sim: strategy and risk are required")
	}
	tracker := pnl.NewMemory()
	aud := admin.NewMemory(tracker)
	venues := strategy.Venues{}
	samples, err := r.replay(ctx, in, s, riskMod, venues, aud, tracker, &f)
	if err != nil {
		return Distribution{}, err
	}
	return distributionFrom(samples), nil
}

func (r *Replay) replay(
	ctx context.Context,
	in Input,
	s strategy.Strategy,
	riskMod risk.Risk,
	venues strategy.Venues,
	aud admin.Auditor,
	tracker pnl.Tracker,
	filter *Filter,
) ([]WinSample, error) {
	_ = in.Ticks // reserved for tick-driven strategies later
	steps := bookSteps(in.Books)
	var samples []WinSample

	for _, books := range steps {
		if err := ctx.Err(); err != nil {
			return samples, err
		}
		decisions, err := s.OnBooks(ctx, books)
		if err != nil {
			return samples, err
		}
		now := stepTime(books)
		mkt := risk.MarketView{Books: books, Now: now}

		for _, d := range decisions {
			sample, ok, err := r.executeDecision(ctx, d, mkt, riskMod, venues, aud, tracker)
			if err != nil {
				return samples, err
			}
			if !ok {
				continue // risk miss or incomplete place — not a distribution sample
			}
			if filter != nil && !matchFilter(*filter, sample) {
				continue
			}
			samples = append(samples, sample)
		}
	}
	return samples, nil
}

func (r *Replay) executeDecision(
	ctx context.Context,
	d strategy.Decision,
	mkt risk.MarketView,
	riskMod risk.Risk,
	venues strategy.Venues,
	aud admin.Auditor,
	tracker pnl.Tracker,
) (WinSample, bool, error) {
	var release func()
	if aq, ok := riskMod.(acquirer); ok {
		var v risk.Verdict
		release, v = aq.TryAcquire(d)
		if !v.OK {
			return WinSample{}, false, nil
		}
		defer release()
	}

	v, err := riskMod.Evaluate(ctx, d, mkt)
	if err != nil {
		return WinSample{}, false, err
	}
	if !v.OK {
		return WinSample{}, false, nil
	}

	ensureVenues(venues, d, r.clock)

	before, err := tracker.Snapshot(ctx)
	if err != nil {
		return WinSample{}, false, err
	}
	beforeR, _ := strconv.ParseFloat(before.Realized, 64)

	results, placeErr := strategy.PlaceDecision(ctx, venues, d)
	fees := feeScheduleFrom(riskMod)
	if recErr := admin.RecordPaperDecision(ctx, aud, tracker, d, results, fees); recErr != nil {
		return WinSample{}, false, recErr
	}
	// Partial place: still sample if at least one fill recorded; treat as not won if placeErr != nil
	after, err := tracker.Snapshot(ctx)
	if err != nil {
		return WinSample{}, false, err
	}
	afterR, _ := strconv.ParseFloat(after.Realized, 64)
	delta := afterR - beforeR

	dims := dimsFromDecision(d, mkt.Now)
	sample := WinSample{
		Dims: dims,
		Won:  placeErr == nil && delta > 0,
		PnL:  strconv.FormatFloat(delta, 'f', 8, 64),
	}
	if placeErr != nil && allLegsFailed(results) {
		return WinSample{}, false, nil
	}
	return sample, true, nil
}

func feeScheduleFrom(riskMod risk.Risk) risk.FeeSchedule {
	if p, ok := riskMod.(feeParams); ok {
		return p.Params().FeeSchedule()
	}
	return risk.FeeSchedule{}
}

func ensureVenues(venues strategy.Venues, d strategy.Decision, clock exchange.Clock) {
	for _, leg := range d.Legs {
		if _, ok := venues[leg.Venue]; ok {
			continue
		}
		venues[leg.Venue] = fake.New(leg.Venue, clock)
	}
}

func allLegsFailed(results []strategy.LegResult) bool {
	if len(results) == 0 {
		return true
	}
	for _, r := range results {
		if r.Err == nil {
			return false
		}
	}
	return true
}

func dimsFromDecision(d strategy.Decision, now time.Time) OutcomeDims {
	dims := OutcomeDims{Time: now}
	var buy, sell *strategy.Leg
	for i := range d.Legs {
		leg := &d.Legs[i]
		if dims.Symbol == "" {
			dims.Symbol = leg.Symbol
		}
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
	if buy != nil {
		dims.Exchange1 = buy.Venue
		dims.Volume1 = buy.Size
	}
	if sell != nil {
		dims.Exchange2 = sell.Venue
		dims.Volume2 = sell.Size
	}
	if buy != nil && sell != nil {
		bp, _ := strconv.ParseFloat(buy.Price, 64)
		sp, _ := strconv.ParseFloat(sell.Price, 64)
		dims.Gap = sp - bp
	}
	if dims.Time.IsZero() {
		dims.Time = time.Now().UTC()
	}
	return dims
}

func bookSteps(books []exchange.Book) [][]exchange.Book {
	if len(books) == 0 {
		return nil
	}
	sorted := append([]exchange.Book(nil), books...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Time.Before(sorted[j].Time)
	})

	type key struct {
		v exchange.VenueID
		s exchange.Symbol
	}
	latest := map[key]exchange.Book{}
	var steps [][]exchange.Book
	var curTime time.Time
	flush := func() {
		if len(latest) == 0 {
			return
		}
		step := make([]exchange.Book, 0, len(latest))
		for _, b := range latest {
			step = append(step, b)
		}
		steps = append(steps, step)
	}

	for i, b := range sorted {
		if i == 0 {
			curTime = b.Time
		}
		if !b.Time.Equal(curTime) {
			flush()
			curTime = b.Time
		}
		latest[key{v: b.Venue, s: b.Symbol}] = b
	}
	flush()
	return steps
}

func stepTime(books []exchange.Book) time.Time {
	var max time.Time
	for _, b := range books {
		if b.Time.After(max) {
			max = b.Time
		}
	}
	return max
}

func matchFilter(f Filter, s WinSample) bool {
	if f.Symbol != "" && s.Dims.Symbol != f.Symbol {
		return false
	}
	if f.Exchange1 != "" && s.Dims.Exchange1 != f.Exchange1 {
		return false
	}
	if f.Exchange2 != "" && s.Dims.Exchange2 != f.Exchange2 {
		return false
	}
	if f.MinGap != 0 && s.Dims.Gap < f.MinGap {
		return false
	}
	if f.MaxGap != 0 && s.Dims.Gap > f.MaxGap {
		return false
	}
	if !f.From.IsZero() && s.Dims.Time.Before(f.From) {
		return false
	}
	if !f.To.IsZero() && s.Dims.Time.After(f.To) {
		return false
	}
	return true
}

func distributionFrom(samples []WinSample) Distribution {
	if len(samples) == 0 {
		return Distribution{Samples: nil, WinRate: 0}
	}
	won := 0
	for _, s := range samples {
		if s.Won {
			won++
		}
	}
	return Distribution{
		Samples: samples,
		WinRate: float64(won) / float64(len(samples)),
	}
}
