// Package config parameterizes symbols, venues, and trading conditions for HHLD modules.
// Strategy/risk/execution read from Config instead of hard-coded values.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/exchange"
)

// Config is the root application configuration.
type Config struct {
	// Symbols are HHLD instruments to trade (multi-symbol scaling; BTCUSD first).
	Symbols []exchange.Symbol `json:"symbols"`
	// Trading holds strategy and order conditions.
	Trading Trading `json:"trading"`
	// Venues names the primary pair used for same-kind arb (adapters map later).
	Venues Venues `json:"venues"`
	// Timeouts simulate / enforce latency budgets on book and order paths.
	Timeouts Timeouts `json:"timeouts"`
	// Risk holds miss-more gate parameters.
	Risk Risk `json:"risk"`
}

// Trading parameterizes strategy behavior.
type Trading struct {
	// Strategy selects the strategy module name (e.g. "cross-venue-arb").
	Strategy string `json:"strategy"`
	// Size is the default order size per leg (decimal string).
	Size string `json:"size"`
	// MinGap is the minimum sellBid - buyAsk to take an arb (price units).
	MinGap float64 `json:"min_gap"`
	// Kind is the default instrument kind for configured symbols.
	Kind exchange.Kind `json:"kind"`
}

// Venues identifies the dual venues for crypto arb (and later hedge legs).
type Venues struct {
	A exchange.VenueID `json:"a"`
	B exchange.VenueID `json:"b"`
}

// Timeouts are wall-clock budgets (JSON durations as strings, e.g. "25ms").
type Timeouts struct {
	Book  Duration `json:"book"`
	Order Duration `json:"order"`
}

// Risk parameterizes miss-more gates (P4).
type Risk struct {
	// FeeBpsPerLeg is the default rate (bps) when a venue is missing from Fees.
	FeeBpsPerLeg float64 `json:"fee_bps_per_leg"`
	// Fees is per-venue trading cost (rate / fixed / commission). Keys are venue ids.
	Fees map[string]VenueFee `json:"fees"`
	LatencyPenalty    float64  `json:"latency_penalty"`
	PartialFillFactor float64  `json:"partial_fill_factor"`
	MaxBookAge        Duration `json:"max_book_age"`
	MaxInFlight       int      `json:"max_in_flight"`
}

// VenueFee models one exchange's fee/commission schedule (additive components).
type VenueFee struct {
	RateBps         float64 `json:"rate_bps"`
	Fixed           float64 `json:"fixed"`
	CommissionBps   float64 `json:"commission_bps"`
	CommissionFixed float64 `json:"commission_fixed"`
}

// Duration wraps time.Duration for JSON string unmarshaling.
type Duration time.Duration

func (d Duration) Duration() time.Duration { return time.Duration(d) }

func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		// allow numeric nanoseconds as fallback
		var n int64
		if err2 := json.Unmarshal(b, &n); err2 != nil {
			return fmt.Errorf("config: duration: %w", err)
		}
		*d = Duration(n)
		return nil
	}
	if s == "" {
		*d = 0
		return nil
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("config: parse duration %q: %w", s, err)
	}
	*d = Duration(parsed)
	return nil
}

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

// Default returns a solo-dev starting config (BTCUSD, HL + GRVT, paper arb).
func Default() Config {
	return Config{
		Symbols: []exchange.Symbol{"BTCUSD"},
		Trading: Trading{
			Strategy: "cross-venue-arb",
			Size:     "1",
			MinGap:   0.3,
			Kind:     exchange.KindPerp,
		},
		Venues: Venues{
			A: "hyperliquid",
			B: "grvt",
		},
		Timeouts: Timeouts{
			Book:  Duration(25 * time.Millisecond),
			Order: Duration(25 * time.Millisecond),
		},
		Risk: Risk{
			FeeBpsPerLeg: 5,
			Fees: map[string]VenueFee{
				"hyperliquid": {RateBps: 3.5},
				"grvt":        {RateBps: 5, CommissionFixed: 0.01},
			},
			LatencyPenalty:    0.05,
			PartialFillFactor: 0.95,
			MaxBookAge:        Duration(2 * time.Second),
			MaxInFlight:       4,
		},
	}
}

// Validate checks required fields for trading modules.
func (c Config) Validate() error {
	if len(c.Symbols) == 0 {
		return fmt.Errorf("config: symbols must not be empty")
	}
	for i, s := range c.Symbols {
		if s == "" {
			return fmt.Errorf("config: symbols[%d] is empty", i)
		}
	}
	if c.Trading.Strategy == "" {
		return fmt.Errorf("config: trading.strategy is required")
	}
	if c.Trading.Size == "" {
		return fmt.Errorf("config: trading.size is required")
	}
	if c.Trading.MinGap < 0 {
		return fmt.Errorf("config: trading.min_gap must be >= 0")
	}
	if c.Venues.A == "" || c.Venues.B == "" {
		return fmt.Errorf("config: venues.a and venues.b are required")
	}
	if c.Venues.A == c.Venues.B {
		return fmt.Errorf("config: venues.a and venues.b must differ")
	}
	if c.Risk.PartialFillFactor < 0 || c.Risk.PartialFillFactor > 1 {
		return fmt.Errorf("config: risk.partial_fill_factor must be in [0,1]")
	}
	if c.Risk.MaxInFlight < 0 {
		return fmt.Errorf("config: risk.max_in_flight must be >= 0")
	}
	if c.Risk.FeeBpsPerLeg < 0 {
		return fmt.Errorf("config: risk.fee_bps_per_leg must be >= 0")
	}
	for venue, fee := range c.Risk.Fees {
		if venue == "" {
			return fmt.Errorf("config: risk.fees has empty venue key")
		}
		if fee.RateBps < 0 || fee.Fixed < 0 || fee.CommissionBps < 0 || fee.CommissionFixed < 0 {
			return fmt.Errorf("config: risk.fees[%s] amounts must be >= 0", venue)
		}
	}
	return nil
}

// LoadJSON reads Config from a JSON file path.
func LoadJSON(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("config: read %s: %w", path, err)
	}
	return ParseJSON(data)
}

// ParseJSON unmarshals Config from JSON bytes and validates.
func ParseJSON(data []byte) (Config, error) {
	cfg := Default()
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("config: parse json: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}
