package risk

import (
	"strconv"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/config"
	"github.com/nguyenducthinhdl/hhld/src/exchange"
)

// Params configures miss-more gates (fees, latency, staleness, concurrency, budgets).
type Params struct {
	// Fees is the per-venue, per-side trading fee schedule (rate / fixed / commission).
	Fees FeeSchedule
	// FeeBpsPerLeg is legacy default rate when Fees.ByVenue has no entry for a venue.
	// Prefer Fees.DefaultRateBps; this field stays for older call sites and is mirrored.
	FeeBpsPerLeg float64
	// LatencyPenalty is subtracted from gross edge (price units) for latency/slippage.
	LatencyPenalty float64
	// PartialFillFactor models worst-case fill ratio in (0, 1]; e.g. 0.9 = assume 90% size.
	PartialFillFactor float64
	// MaxBookAge rejects books older than this relative to MarketView.Now.
	MaxBookAge time.Duration
	// MaxInFlight is the global concurrent Risk+exec pipeline cap (miss when full).
	MaxInFlight int
	// Budgets is notional caps keyed "venue/symbol". Missing or <=0 = unlimited.
	Budgets map[string]float64
	// OrderInterval is min time between accepted Evaluates per symbol. Missing/0 = no limit.
	OrderInterval map[exchange.Symbol]time.Duration
	// MaxVolumeTrade caps leg size per symbol (from trading.max_size). Missing/0 = no cap check.
	MaxVolumeTrade map[exchange.Symbol]float64
	// MinNotional / MaxNotional are USD notional bounds per symbol (trading.min_value / max_value).
	MinNotional map[exchange.Symbol]float64
	MaxNotional map[exchange.Symbol]float64
}

// ParamsFromConfig maps global risk + per-symbol trading/risk/venues into Params.
func ParamsFromConfig(cfg config.Config) Params {
	fees := FeeSchedule{ByVenue: map[exchange.VenueID]VenueFee{}}
	budgets := map[string]float64{}
	intervals := make(map[exchange.Symbol]time.Duration, len(cfg.SymbolMap))
	maxVol := make(map[exchange.Symbol]float64, len(cfg.SymbolMap))
	minNtl := make(map[exchange.Symbol]float64, len(cfg.SymbolMap))
	maxNtl := make(map[exchange.Symbol]float64, len(cfg.SymbolMap))
	var feeDefault, lat, pff float64
	var maxAge time.Duration
	for i, entry := range cfg.SymbolMap {
		if i == 0 {
			feeDefault = entry.Risk.FeeBpsPerLeg
			lat = entry.Risk.LatencyPenalty
			pff = entry.Risk.PartialFillFactor
			maxAge = entry.Risk.MaxBookAge.Duration()
		}
		if entry.Trading.OrderInterval != nil {
			intervals[entry.Symbol] = entry.Trading.OrderInterval.Duration()
		}
		if mv, err := strconv.ParseFloat(entry.Trading.MaxSize, 64); err == nil && mv > 0 {
			maxVol[entry.Symbol] = mv
		}
		if v, err := strconv.ParseFloat(entry.Trading.MinValue, 64); err == nil && v > 0 {
			minNtl[entry.Symbol] = v
		}
		if v, err := strconv.ParseFloat(entry.Trading.MaxValue, 64); err == nil && v > 0 {
			maxNtl[entry.Symbol] = v
		}
		for venue, spec := range entry.Venues {
			vid := exchange.VenueID(venue)
			if _, exists := fees.ByVenue[vid]; !exists {
				fees.ByVenue[vid] = VenueFee{
					Buy:  sideFeeFrom(spec.Fees.Buy),
					Sell: sideFeeFrom(spec.Fees.Sell),
				}
			}
			if spec.Budget == "" {
				continue
			}
			v, err := strconv.ParseFloat(spec.Budget, 64)
			if err != nil || v <= 0 {
				continue
			}
			budgets[config.BudgetKey(vid, entry.Symbol)] = v
		}
	}
	fees.DefaultRateBps = feeDefault
	p := Params{
		Fees:              fees,
		FeeBpsPerLeg:      feeDefault,
		LatencyPenalty:    lat,
		PartialFillFactor: pff,
		MaxBookAge:        maxAge,
		MaxInFlight:       cfg.Risk.MaxInFlight,
		Budgets:           budgets,
		OrderInterval:     intervals,
		MaxVolumeTrade:    maxVol,
		MinNotional:       minNtl,
		MaxNotional:       maxNtl,
	}
	if p.PartialFillFactor <= 0 || p.PartialFillFactor > 1 {
		p.PartialFillFactor = 1
	}
	if p.MaxInFlight <= 0 {
		p.MaxInFlight = 4
	}
	if p.MaxBookAge <= 0 {
		p.MaxBookAge = 2 * time.Second
	}
	if p.Fees.DefaultRateBps == 0 && p.FeeBpsPerLeg != 0 {
		p.Fees.DefaultRateBps = p.FeeBpsPerLeg
	}
	if p.FeeBpsPerLeg == 0 && p.Fees.DefaultRateBps != 0 {
		p.FeeBpsPerLeg = p.Fees.DefaultRateBps
	}
	return p
}

// DefaultParams returns conservative solo-dev miss-more defaults.
func DefaultParams() Params {
	return ParamsFromConfig(config.Default())
}

// FeeSchedule returns the schedule used by Evaluate and paper fills.
func (p Params) FeeSchedule() FeeSchedule {
	s := p.Fees
	if s.DefaultRateBps == 0 {
		s.DefaultRateBps = p.FeeBpsPerLeg
	}
	if s.ByVenue == nil {
		s.ByVenue = map[exchange.VenueID]VenueFee{}
	}
	return s
}

func sideFeeFrom(f config.SideFee) SideFee {
	return SideFee{
		RateBps:         f.RateBps,
		Fixed:           f.Fixed,
		CommissionBps:   f.CommissionBps,
		CommissionFixed: f.CommissionFixed,
	}
}
