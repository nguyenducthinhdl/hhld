// Package config parameterizes symbols, venues, and trading conditions for HHLD modules.
// Strategy/risk/execution read from Config instead of hard-coded values.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/exchange"
)

// Config is the root application configuration.
type Config struct {
	// Venues names the primary pair used for same-kind arb (adapters map later).
	Venues Venues `json:"venues"`
	// Timeouts simulate / enforce latency budgets on book and order paths.
	Timeouts Timeouts `json:"timeouts"`
	// Risk holds process-wide miss-more caps (in-flight).
	Risk Risk `json:"risk"`
	// SymbolMap is the traded set: each row is one HHLD symbol with trading, risk, and venues.
	SymbolMap []SymbolEntry `json:"symbol_map"`
}

// SymbolEntry is one HHLD symbol: strategy knobs, risk knobs, and per-venue native id/fees/budget.
type SymbolEntry struct {
	Symbol  exchange.Symbol      `json:"symbol"`
	Trading Trading              `json:"trading"`
	Risk    SymbolRisk           `json:"risk"`
	Venues  map[string]VenueSpec `json:"venues"`
}

// VenueSpec is one venue leg for a symbol.
type VenueSpec struct {
	SymbolName string   `json:"symbol_name"`
	Fees       VenueFee `json:"fees"`
	Budget     string   `json:"budget"`
}

// Trading parameterizes strategy behavior for one symbol.
type Trading struct {
	Strategy      string        `json:"strategy"`
	Kind          exchange.Kind `json:"kind"`
	MinSize       string        `json:"min_size"`
	MaxSize       string        `json:"max_size"`
	MinGap        float64       `json:"min_gap"`
	OrderInterval *Duration     `json:"order_interval"`
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

// Risk is process-wide (not per symbol).
type Risk struct {
	MaxInFlight int `json:"max_in_flight"`
}

// SymbolRisk is miss-more knobs for one symbol (fees live on VenueSpec).
type SymbolRisk struct {
	FeeBpsPerLeg      float64  `json:"fee_bps_per_leg"`
	LatencyPenalty    float64  `json:"latency_penalty"`
	PartialFillFactor float64  `json:"partial_fill_factor"`
	MaxBookAge        Duration `json:"max_book_age"`
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
		Venues: Venues{A: "hyperliquid", B: "grvt"},
		Timeouts: Timeouts{
			Book:  Duration(25 * time.Millisecond),
			Order: Duration(25 * time.Millisecond),
		},
		Risk: Risk{MaxInFlight: 4},
		SymbolMap: []SymbolEntry{defaultBTCUSD()},
	}
}

func defaultBTCUSD() SymbolEntry {
	return SymbolEntry{
		Symbol: "BTCUSD",
		Trading: Trading{
			Strategy:      "cross-venue-arb",
			Kind:          exchange.KindPerp,
			MinSize:       "0.000015",
			MaxSize:       "0.00003",
			MinGap:        0.3,
			OrderInterval: durationPtr(time.Second),
		},
		Risk: SymbolRisk{
			FeeBpsPerLeg:      5,
			LatencyPenalty:    0.05,
			PartialFillFactor: 0.95,
			MaxBookAge:        Duration(2 * time.Second),
		},
		Venues: map[string]VenueSpec{
			"hyperliquid": {SymbolName: "BTC", Fees: VenueFee{RateBps: 1}, Budget: "10000"},
			"grvt":        {SymbolName: "BTC_USDT_Perp", Fees: VenueFee{RateBps: 2, CommissionFixed: 0.01}, Budget: "10000"},
		},
	}
}

func durationPtr(d time.Duration) *Duration {
	x := Duration(d)
	return &x
}

// Symbols returns HHLD symbols in symbol_map order.
func (c Config) Symbols() []exchange.Symbol {
	out := make([]exchange.Symbol, 0, len(c.SymbolMap))
	for _, e := range c.SymbolMap {
		if e.Symbol != "" {
			out = append(out, e.Symbol)
		}
	}
	return out
}

// Lookup returns the symbol_map row for sym.
func (c Config) Lookup(sym exchange.Symbol) (SymbolEntry, bool) {
	for _, e := range c.SymbolMap {
		if e.Symbol == sym {
			return e, true
		}
	}
	return SymbolEntry{}, false
}

// Clone deep-copies SymbolMap (including venue maps) so overlays do not mutate the base.
func (c Config) Clone() Config {
	out := c
	out.SymbolMap = make([]SymbolEntry, len(c.SymbolMap))
	for i, e := range c.SymbolMap {
		out.SymbolMap[i] = cloneEntry(e)
	}
	return out
}

// UpdateSymbol applies fn to the row for sym. Empty sym patches the first row.
// Missing symbols are appended (cloned from the first row when present).
func (c *Config) UpdateSymbol(sym exchange.Symbol, fn func(*SymbolEntry)) {
	if c == nil || fn == nil {
		return
	}
	idx := -1
	for i := range c.SymbolMap {
		if (sym == "" && i == 0) || c.SymbolMap[i].Symbol == sym {
			idx = i
			break
		}
	}
	if idx < 0 {
		e := SymbolEntry{Symbol: sym, Venues: map[string]VenueSpec{}}
		if len(c.SymbolMap) > 0 {
			e = cloneEntry(c.SymbolMap[0])
			if sym != "" {
				e.Symbol = sym
			}
		}
		c.SymbolMap = append(c.SymbolMap, e)
		idx = len(c.SymbolMap) - 1
	}
	fn(&c.SymbolMap[idx])
}

func cloneEntry(e SymbolEntry) SymbolEntry {
	out := e
	if e.Venues != nil {
		out.Venues = make(map[string]VenueSpec, len(e.Venues))
		for k, v := range e.Venues {
			out.Venues[k] = v
		}
	}
	return out
}

// Validate checks required fields for trading modules.
func (c Config) Validate() error {
	if c.Venues.A == "" || c.Venues.B == "" {
		return fmt.Errorf("config: venues.a and venues.b are required")
	}
	if c.Venues.A == c.Venues.B {
		return fmt.Errorf("config: venues.a and venues.b must differ")
	}
	if c.Risk.MaxInFlight < 0 {
		return fmt.Errorf("config: risk.max_in_flight must be >= 0")
	}
	if len(c.SymbolMap) == 0 {
		return fmt.Errorf("config: symbol_map must not be empty")
	}
	seen := map[exchange.Symbol]struct{}{}
	for i, e := range c.SymbolMap {
		if err := e.validate(i); err != nil {
			return err
		}
		if _, dup := seen[e.Symbol]; dup {
			return fmt.Errorf("config: symbol_map duplicate symbol %s", e.Symbol)
		}
		seen[e.Symbol] = struct{}{}
	}
	return nil
}

func (e SymbolEntry) validate(i int) error {
	prefix := fmt.Sprintf("config: symbol_map[%d]", i)
	if e.Symbol == "" {
		return fmt.Errorf("%s: symbol is required", prefix)
	}
	prefix = fmt.Sprintf("config: symbol_map[%s]", e.Symbol)
	if e.Trading.Strategy == "" {
		return fmt.Errorf("%s: trading.strategy is required", prefix)
	}
	if e.Trading.MinSize == "" {
		return fmt.Errorf("%s: trading.min_size is required", prefix)
	}
	if e.Trading.MaxSize == "" {
		return fmt.Errorf("%s: trading.max_size is required", prefix)
	}
	minSz, err := strconv.ParseFloat(e.Trading.MinSize, 64)
	if err != nil || minSz <= 0 {
		return fmt.Errorf("%s: trading.min_size must be a positive number", prefix)
	}
	maxSz, err := strconv.ParseFloat(e.Trading.MaxSize, 64)
	if err != nil || maxSz <= 0 {
		return fmt.Errorf("%s: trading.max_size must be a positive number", prefix)
	}
	if minSz > maxSz {
		return fmt.Errorf("%s: trading.min_size must be <= trading.max_size", prefix)
	}
	if e.Trading.MinGap < 0 {
		return fmt.Errorf("%s: trading.min_gap must be >= 0", prefix)
	}
	if e.Trading.OrderInterval != nil && *e.Trading.OrderInterval < 0 {
		return fmt.Errorf("%s: trading.order_interval must be >= 0", prefix)
	}
	if e.Risk.PartialFillFactor < 0 || e.Risk.PartialFillFactor > 1 {
		return fmt.Errorf("%s: risk.partial_fill_factor must be in [0,1]", prefix)
	}
	if e.Risk.FeeBpsPerLeg < 0 {
		return fmt.Errorf("%s: risk.fee_bps_per_leg must be >= 0", prefix)
	}
	if len(e.Venues) == 0 {
		return fmt.Errorf("%s: venues must not be empty", prefix)
	}
	for venue, spec := range e.Venues {
		if venue == "" || spec.SymbolName == "" {
			return fmt.Errorf("%s: venue key and symbol_name are required", prefix)
		}
		f := spec.Fees
		if f.RateBps < 0 || f.Fixed < 0 || f.CommissionBps < 0 || f.CommissionFixed < 0 {
			return fmt.Errorf("%s: venues[%s].fees amounts must be >= 0", prefix, venue)
		}
		if spec.Budget != "" {
			v, err := strconv.ParseFloat(spec.Budget, 64)
			if err != nil || v < 0 {
				return fmt.Errorf("%s: venues[%s].budget must be a non-negative number", prefix, venue)
			}
		}
	}
	return nil
}

// NativeSymbol returns the venue instrument id (symbol_name) for an HHLD symbol.
func (c Config) NativeSymbol(venue exchange.VenueID, symbol exchange.Symbol) (string, error) {
	entry, ok := c.Lookup(symbol)
	if !ok {
		return "", fmt.Errorf("config: no symbol_map for %s", symbol)
	}
	spec, ok := entry.Venues[string(venue)]
	if !ok || spec.SymbolName == "" {
		return "", fmt.Errorf("config: no symbol_map[%s].venues[%s]", symbol, venue)
	}
	return spec.SymbolName, nil
}

// EffectiveSize is the leg size Strategy emits (trading.max_size, at least min_size).
func (c Config) EffectiveSize(symbol exchange.Symbol) string {
	entry, ok := c.Lookup(symbol)
	if !ok {
		return ""
	}
	minS, err1 := strconv.ParseFloat(entry.Trading.MinSize, 64)
	maxS, err2 := strconv.ParseFloat(entry.Trading.MaxSize, 64)
	if err1 != nil || err2 != nil || minS <= 0 || maxS <= 0 {
		return entry.Trading.MaxSize
	}
	if maxS < minS {
		return entry.Trading.MinSize
	}
	return entry.Trading.MaxSize
}

// MinGapFor returns trading.min_gap for symbol (0 if unknown).
func (c Config) MinGapFor(symbol exchange.Symbol) float64 {
	entry, ok := c.Lookup(symbol)
	if !ok {
		return 0
	}
	return entry.Trading.MinGap
}

// OrderIntervalFor returns the min place interval for a symbol (0 if unset/disabled).
func (c Config) OrderIntervalFor(symbol exchange.Symbol) time.Duration {
	entry, ok := c.Lookup(symbol)
	if !ok || entry.Trading.OrderInterval == nil {
		return 0
	}
	return entry.Trading.OrderInterval.Duration()
}

// KindFor returns the instrument kind for a symbol.
func (c Config) KindFor(symbol exchange.Symbol) exchange.Kind {
	entry, ok := c.Lookup(symbol)
	if !ok || entry.Trading.Kind == "" {
		return exchange.KindPerp
	}
	return entry.Trading.Kind
}

// BudgetKey builds the notional-cap key for a venue+symbol.
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

// ParseJSON unmarshals Config from JSON bytes, applies defaults, and validates.
func ParseJSON(data []byte) (Config, error) {
	cfg := Default()
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("config: parse json: %w", err)
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c *Config) applyDefaults() {
	for i := range c.SymbolMap {
		e := &c.SymbolMap[i]
		if e.Trading.OrderInterval == nil {
			e.Trading.OrderInterval = durationPtr(time.Second)
		}
		if e.Trading.Strategy == "" {
			e.Trading.Strategy = "cross-venue-arb"
		}
		if e.Trading.Kind == "" {
			e.Trading.Kind = exchange.KindPerp
		}
	}
}
