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
	syms := cfg.Symbols()
	if len(syms) != 1 || syms[0] != "BTCUSD" {
		t.Fatalf("symbols: %+v", syms)
	}
}

func TestParseJSON_OverridesAndMultiSymbol(t *testing.T) {
	raw := []byte(`{
  "venues": { "a": "hyperliquid", "b": "grvt" },
  "timeouts": { "book": "30ms", "order": "40ms" },
  "risk": { "max_in_flight": 4 },
  "symbol_map": [
    {
      "symbol": "BTCUSD",
      "trading": { "strategy": "cross-venue-arb", "kind": "perp", "min_size": "0.01", "max_size": "0.5", "min_gap": 0.5, "order_interval": "1s" },
      "risk": { "fee_bps_per_leg": 5, "latency_penalty": 0.05, "partial_fill_factor": 0.95, "max_book_age": "2s" },
      "venues": {
        "hyperliquid": { "symbol_name": "BTC", "fees": { "rate_bps": 1 }, "budget": "10000" },
        "grvt": { "symbol_name": "BTC_USDT_Perp", "fees": { "rate_bps": 2 }, "budget": "10000" }
      }
    },
    {
      "symbol": "ETHUSD",
      "trading": { "strategy": "cross-venue-arb", "kind": "perp", "min_size": "0.01", "max_size": "0.5", "min_gap": 0.5, "order_interval": "1s" },
      "risk": { "fee_bps_per_leg": 5, "latency_penalty": 0.05, "partial_fill_factor": 0.95, "max_book_age": "2s" },
      "venues": {
        "hyperliquid": { "symbol_name": "ETH", "fees": { "rate_bps": 1 }, "budget": "5000" },
        "grvt": { "symbol_name": "ETH_USDT_Perp", "fees": { "rate_bps": 2 }, "budget": "5000" }
      }
    }
  ]
}`)
	cfg, err := config.ParseJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	syms := cfg.Symbols()
	entry, ok := cfg.Lookup("BTCUSD")
	if !ok || len(syms) != 2 || entry.Trading.MaxSize != "0.5" || entry.Trading.MinGap != 0.5 {
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
  "venues": { "a": "hyperliquid", "b": "grvt" },
  "timeouts": { "book": "25ms", "order": "25ms" },
  "risk": { "max_in_flight": 4 },
  "symbol_map": [{
    "symbol": "BTCUSD",
    "trading": { "strategy": "cross-venue-arb", "min_size": "0.01", "max_size": "1", "min_gap": 0.3, "kind": "perp" },
    "risk": { "fee_bps_per_leg": 5, "partial_fill_factor": 0.95, "max_book_age": "2s" },
    "venues": {
      "hyperliquid": { "symbol_name": "BTC", "fees": { "rate_bps": 1 }, "budget": "10000" },
      "grvt": { "symbol_name": "BTC_USDT_Perp", "fees": { "rate_bps": 2 }, "budget": "10000" }
    }
  }]
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

func TestLoadJSON_RepoDefault(t *testing.T) {
	cfg, err := config.LoadJSON("../../configs/default.json")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.EffectiveSize("BTCUSD") != "0.00003" {
		t.Fatalf("effective size: %q", cfg.EffectiveSize("BTCUSD"))
	}
	hl, err := cfg.NativeSymbol("hyperliquid", "BTCUSD")
	if err != nil || hl != "BTC" {
		t.Fatalf("native: %q %v", hl, err)
	}
}

func TestValidate_RejectsBadVenues(t *testing.T) {
	cfg := config.Default()
	cfg.Venues.B = cfg.Venues.A
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for identical venues")
	}
}
