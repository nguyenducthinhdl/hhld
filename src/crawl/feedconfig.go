package crawl

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/config"
	"github.com/nguyenducthinhdl/hhld/src/exchange"
)

// Feed methods for live capture.
const (
	MethodSnapshotBook  = "snapshot_book"
	MethodSubscribeBook = "subscribe_book"
	MethodSubscribeTicks = "subscribe_ticks"
)

// FeedConfig is one exchange/symbol/method stream.
type FeedConfig struct {
	Exchange string `json:"exchange"`
	Symbol   string `json:"symbol"`
	Method   string `json:"method"`
	// Interval poll period for snapshot_book (default 2s).
	Interval string `json:"interval,omitempty"`
}

// LiveConfig drives multi-exchange NDJSON capture.
type LiveConfig struct {
	Output string `json:"output"`
	// Config optional path to HHLD config JSON (symbol_map + trading.kind).
	Config string `json:"config,omitempty"`
	// Duration run length (0 or empty = until SIGINT).
	Duration string `json:"duration,omitempty"`
	Kind     exchange.Kind `json:"kind,omitempty"`
	SymbolMap map[string]config.SymbolEntry `json:"symbol_map,omitempty"`
	Feeds    []FeedConfig `json:"feeds"`
}

// ParseLiveConfigJSON unmarshals and validates a crawl config file.
func ParseLiveConfigJSON(data []byte) (LiveConfig, error) {
	var cfg LiveConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return LiveConfig{}, fmt.Errorf("crawl: parse config: %w", err)
	}
	return cfg, cfg.Validate()
}

// LoadLiveConfig reads JSON from path and merges optional HHLD config for symbol_map.
func LoadLiveConfig(path string) (LiveConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return LiveConfig{}, fmt.Errorf("crawl: read %s: %w", path, err)
	}
	cfg, err := ParseLiveConfigJSON(data)
	if err != nil {
		return LiveConfig{}, err
	}
	if cfg.Config != "" {
		app, err := config.LoadJSON(cfg.Config)
		if err != nil {
			return LiveConfig{}, err
		}
		if cfg.Kind == "" {
			cfg.Kind = app.Trading.Kind
		}
		if len(cfg.SymbolMap) == 0 {
			cfg.SymbolMap = app.SymbolMap
		}
	}
	return cfg, cfg.Validate()
}

// Validate checks crawl config fields.
func (c LiveConfig) Validate() error {
	if strings.TrimSpace(c.Output) == "" {
		return fmt.Errorf("crawl: output path required")
	}
	if len(c.Feeds) == 0 {
		return fmt.Errorf("crawl: at least one feed required")
	}
	for i, f := range c.Feeds {
		if err := f.Validate(); err != nil {
			return fmt.Errorf("crawl: feeds[%d]: %w", i, err)
		}
	}
	if c.Duration != "" {
		d, err := time.ParseDuration(c.Duration)
		if err != nil {
			return fmt.Errorf("crawl: duration: %w", err)
		}
		if d < 0 {
			return fmt.Errorf("crawl: duration must be >= 0")
		}
	}
	return nil
}

// Validate checks one feed entry.
func (f FeedConfig) Validate() error {
	if strings.TrimSpace(f.Exchange) == "" {
		return fmt.Errorf("exchange required")
	}
	if strings.TrimSpace(f.Symbol) == "" {
		return fmt.Errorf("symbol required")
	}
	switch strings.ToLower(strings.TrimSpace(f.Method)) {
	case MethodSnapshotBook, MethodSubscribeBook, MethodSubscribeTicks:
	default:
		return fmt.Errorf("method must be %q, %q, or %q", MethodSnapshotBook, MethodSubscribeBook, MethodSubscribeTicks)
	}
	if f.Interval != "" {
		d, err := time.ParseDuration(f.Interval)
		if err != nil {
			return fmt.Errorf("interval: %w", err)
		}
		if d <= 0 {
			return fmt.Errorf("interval must be > 0")
		}
	}
	return nil
}

func (f FeedConfig) method() string {
	return strings.ToLower(strings.TrimSpace(f.Method))
}

func (f FeedConfig) interval(defaultDur time.Duration) time.Duration {
	if f.Interval == "" {
		return defaultDur
	}
	d, _ := time.ParseDuration(f.Interval)
	if d <= 0 {
		return defaultDur
	}
	return d
}

// NativeSymbol resolves venue-native instrument id from symbol_map.
func (c LiveConfig) NativeSymbol(venue exchange.VenueID, symbol exchange.Symbol) (string, error) {
	entry, ok := c.SymbolMap[string(symbol)]
	if !ok || entry.Venues == nil {
		return "", fmt.Errorf("crawl: no symbol_map for %s", symbol)
	}
	native, ok := entry.Venues[string(venue)]
	if !ok || native == "" {
		return "", fmt.Errorf("crawl: no symbol_map[%s][%s]", symbol, venue)
	}
	return native, nil
}

func (c LiveConfig) kind() exchange.Kind {
	if c.Kind != "" {
		return c.Kind
	}
	return exchange.KindPerp
}
