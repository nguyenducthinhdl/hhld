// Package config parameterizes symbols, venues, and trading conditions for HHLD modules.
// Strategy/risk/execution read from Config instead of hard-coded values.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
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
	// SymbolMap maps HHLD symbols to venue-native ids plus per-symbol trade limits.
	SymbolMap map[string]SymbolEntry `json:"symbol_map"`
}

// SymbolEntry maps one HHLD symbol to venue-native instrument ids and trade limits.
// JSON may be the new object form or a legacy flat venue→native string map.
type SymbolEntry struct {
	Venues         map[string]string `json:"venues"`
	OrderInterval  *Duration         `json:"order_interval"`
	MaxVolumeTrade string            `json:"max_volume_trade"`
}

// UnmarshalJSON accepts either:
//
//	{"venues":{"hyperliquid":"BTC"},"order_interval":"1s","max_volume_trade":"1"}
//
// or legacy flat:
//
//	{"hyperliquid":"BTC","grvt":"BTC_USDT_Perp"}
func (e *SymbolEntry) UnmarshalJSON(b []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return fmt.Errorf("config: symbol_map entry: %w", err)
	}
	if v, ok := raw["venues"]; ok {
		var venues map[string]string
		if err := json.Unmarshal(v, &venues); err != nil {
			return fmt.Errorf("config: symbol_map.venues: %w", err)
		}
		e.Venues = venues
		if oi, ok := raw["order_interval"]; ok {
			var d Duration
			if err := json.Unmarshal(oi, &d); err != nil {
				return fmt.Errorf("config: symbol_map.order_interval: %w", err)
			}
			e.OrderInterval = &d
		}
		if mv, ok := raw["max_volume_trade"]; ok {
			if err := json.Unmarshal(mv, &e.MaxVolumeTrade); err != nil {
				return fmt.Errorf("config: symbol_map.max_volume_trade: %w", err)
			}
		}
		return nil
	}
	venues := make(map[string]string, len(raw))
	for k, v := range raw {
		var s string
		if err := json.Unmarshal(v, &s); err != nil {
			return fmt.Errorf("config: symbol_map legacy %s: %w", k, err)
		}
		venues[k] = s
	}
	e.Venues = venues
	return nil
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
	// Budgets is per (venue, symbol) notional caps keyed "venue/symbol" (decimal strings).
	// Missing or "0" = unlimited. Process-lifetime sum(price×size) of accepted legs.
	Budgets map[string]string `json:"budgets"`
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
			Budgets: map[string]string{
				"hyperliquid/BTCUSD": "10000",
				"grvt/BTCUSD":        "10000",
			},
		},
		SymbolMap: map[string]SymbolEntry{
			"BTCUSD": {
				Venues: map[string]string{
					"hyperliquid": "BTC",
					"grvt":        "BTC_USDT_Perp",
				},
				OrderInterval:  durationPtr(time.Second),
				MaxVolumeTrade: "1",
			},
		},
	}
}

func durationPtr(d time.Duration) *Duration {
	x := Duration(d)
	return &x
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
	for key, raw := range c.Risk.Budgets {
		if key == "" {
			return fmt.Errorf("config: risk.budgets has empty key")
		}
		if !strings.Contains(key, "/") {
			return fmt.Errorf("config: risk.budgets key %q must be venue/symbol", key)
		}
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil || v < 0 {
			return fmt.Errorf("config: risk.budgets[%s] must be a non-negative number", key)
		}
	}
	for sym, entry := range c.SymbolMap {
		if sym == "" {
			return fmt.Errorf("config: symbol_map has empty symbol key")
		}
		if len(entry.Venues) == 0 {
			return fmt.Errorf("config: symbol_map[%s] has no venues", sym)
		}
		for venue, native := range entry.Venues {
			if venue == "" || native == "" {
				return fmt.Errorf("config: symbol_map[%s] has empty venue or native id", sym)
			}
		}
		if entry.OrderInterval != nil && *entry.OrderInterval < 0 {
			return fmt.Errorf("config: symbol_map[%s].order_interval must be >= 0", sym)
		}
		if entry.MaxVolumeTrade != "" {
			mv, err := strconv.ParseFloat(entry.MaxVolumeTrade, 64)
			if err != nil || mv <= 0 {
				return fmt.Errorf("config: symbol_map[%s].max_volume_trade must be > 0", sym)
			}
		}
	}
	return nil
}

// NativeSymbol returns the venue-native instrument id for an HHLD symbol.
func (c Config) NativeSymbol(venue exchange.VenueID, symbol exchange.Symbol) (string, error) {
	entry, ok := c.SymbolMap[string(symbol)]
	if !ok || entry.Venues == nil {
		return "", fmt.Errorf("config: no symbol_map for %s", symbol)
	}
	native, ok := entry.Venues[string(venue)]
	if !ok || native == "" {
		return "", fmt.Errorf("config: no symbol_map[%s][%s]", symbol, venue)
	}
	return native, nil
}

// EffectiveSize returns min(trading.size, symbol max_volume_trade).
// Missing max_volume_trade defaults to trading.size.
func (c Config) EffectiveSize(symbol exchange.Symbol) string {
	size := c.Trading.Size
	max := size
	if entry, ok := c.SymbolMap[string(symbol)]; ok && entry.MaxVolumeTrade != "" {
		max = entry.MaxVolumeTrade
	}
	sf, err1 := strconv.ParseFloat(size, 64)
	mf, err2 := strconv.ParseFloat(max, 64)
	if err1 != nil || err2 != nil {
		return size
	}
	if mf < sf {
		return formatDecimal(max, mf)
	}
	return size
}

// OrderIntervalFor returns the min place interval for a symbol (0 if unset/disabled).
func (c Config) OrderIntervalFor(symbol exchange.Symbol) time.Duration {
	entry, ok := c.SymbolMap[string(symbol)]
	if !ok || entry.OrderInterval == nil {
		return 0
	}
	return entry.OrderInterval.Duration()
}

// MaxVolumeTradeFor returns max volume for a symbol (0 if unset).
func (c Config) MaxVolumeTradeFor(symbol exchange.Symbol) float64 {
	entry, ok := c.SymbolMap[string(symbol)]
	if !ok || entry.MaxVolumeTrade == "" {
		return 0
	}
	v, err := strconv.ParseFloat(entry.MaxVolumeTrade, 64)
	if err != nil {
		return 0
	}
	return v
}

func formatDecimal(raw string, v float64) string {
	if raw != "" {
		return raw
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// BudgetKey builds the risk.budgets map key for a venue+symbol.
func BudgetKey(venue exchange.VenueID, symbol exchange.Symbol) string {
	return string(venue) + "/" + string(symbol)
}

// LoadJSON reads Config from a JSON file path.
func LoadJSON(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("config: read %s: %w", path, err)
	}
	return ParseJSON(data)
}

// ParseJSON unmarshals Config from JSON bytes, applies symbol_map defaults, and validates.
func ParseJSON(data []byte) (Config, error) {
	cfg := Default()
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("config: parse json: %w", err)
	}
	cfg.applySymbolMapDefaults()
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// applySymbolMapDefaults fills order_interval / max_volume_trade when omitted (legacy maps).
func (c *Config) applySymbolMapDefaults() {
	for sym, entry := range c.SymbolMap {
		if entry.OrderInterval == nil {
			entry.OrderInterval = durationPtr(time.Second)
		}
		if entry.MaxVolumeTrade == "" {
			entry.MaxVolumeTrade = c.Trading.Size
		}
		c.SymbolMap[sym] = entry
	}
}
