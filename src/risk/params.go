package risk

import (
	"time"

	"github.com/nguyenducthinhdl/hhld/src/config"
	"github.com/nguyenducthinhdl/hhld/src/exchange"
)

// Params configures miss-more gates (fees, latency, staleness, concurrency).
type Params struct {
	// Fees is the per-venue trading fee schedule (rate / fixed / commission).
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
}

// ParamsFromConfig maps config.Risk (and defaults) into Params.
func ParamsFromConfig(cfg config.Config) Params {
	r := cfg.Risk
	fees := FeeSchedule{
		DefaultRateBps: r.FeeBpsPerLeg,
		ByVenue:        make(map[exchange.VenueID]VenueFee, len(r.Fees)),
	}
	for venue, vf := range r.Fees {
		fees.ByVenue[exchange.VenueID(venue)] = VenueFee{
			RateBps:         vf.RateBps,
			Fixed:           vf.Fixed,
			CommissionBps:   vf.CommissionBps,
			CommissionFixed: vf.CommissionFixed,
		}
	}
	p := Params{
		Fees:              fees,
		FeeBpsPerLeg:      r.FeeBpsPerLeg,
		LatencyPenalty:    r.LatencyPenalty,
		PartialFillFactor: r.PartialFillFactor,
		MaxBookAge:        r.MaxBookAge.Duration(),
		MaxInFlight:       r.MaxInFlight,
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
	// Keep DefaultRateBps and FeeBpsPerLeg aligned for callers that set only one.
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
