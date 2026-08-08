package risk

import (
	"time"

	"github.com/nguyenducthinhdl/hhld/src/config"
)

// Params configures miss-more gates (fees, latency, staleness, concurrency).
type Params struct {
	// FeeBpsPerLeg is taker-style fee in basis points applied per leg notional.
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
	p := Params{
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
	return p
}

// DefaultParams returns conservative solo-dev miss-more defaults.
func DefaultParams() Params {
	return ParamsFromConfig(config.Default())
}
