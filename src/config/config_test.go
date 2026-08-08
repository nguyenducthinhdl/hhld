package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/config"
	"github.com/nguyenducthinhdl/hhld/src/strategy"
)

func TestDefault_Validate(t *testing.T) {
	cfg := config.Default()
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Symbols) != 1 || cfg.Symbols[0] != "BTCUSD" {
		t.Fatalf("symbols: %+v", cfg.Symbols)
	}
}

func TestParseJSON_OverridesAndMultiSymbol(t *testing.T) {
	raw := []byte(`{
  "symbols": ["BTCUSD", "ETHUSD"],
  "trading": {
    "strategy": "cross-venue-arb",
    "size": "0.5",
    "min_gap": 0.5,
    "kind": "perp"
  },
  "venues": { "a": "hyperliquid", "b": "grvt" },
  "timeouts": { "book": "30ms", "order": "40ms" }
}`)
	cfg, err := config.ParseJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Symbols) != 2 || cfg.Trading.Size != "0.5" || cfg.Trading.MinGap != 0.5 {
		t.Fatalf("cfg: %+v", cfg)
	}
	if cfg.Timeouts.Book.Duration() != 30*time.Millisecond {
		t.Fatalf("book timeout: %v", cfg.Timeouts.Book.Duration())
	}
	if cfg.Timeouts.Order.Duration() != 40*time.Millisecond {
		t.Fatalf("order timeout: %v", cfg.Timeouts.Order.Duration())
	}

	arb := strategy.NewCrossVenueArb(strategy.ArbConfigFrom(cfg))
	if arb.Name() != "cross-venue-arb" {
		t.Fatal(arb.Name())
	}
	ac := strategy.ArbConfigFrom(cfg)
	if len(ac.Symbols) != 2 || ac.Size != "0.5" || ac.MinGap != 0.5 {
		t.Fatalf("arb cfg: %+v", ac)
	}
}

func TestLoadJSON_FromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hhld.json")
	body := `{
  "symbols": ["BTCUSD"],
  "trading": { "strategy": "cross-venue-arb", "size": "1", "min_gap": 0.3, "kind": "perp" },
  "venues": { "a": "hyperliquid", "b": "grvt" },
  "timeouts": { "book": "25ms", "order": "25ms" }
}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadJSON(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestValidate_RejectsBadVenues(t *testing.T) {
	cfg := config.Default()
	cfg.Venues.B = cfg.Venues.A
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for identical venues")
	}
}
