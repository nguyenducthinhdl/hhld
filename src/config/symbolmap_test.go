package config_test

import (
	"testing"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/config"
)

func TestNativeSymbol_FromDefaultMap(t *testing.T) {
	cfg := config.Default()
	hl, err := cfg.NativeSymbol("hyperliquid", "BTCUSD")
	if err != nil || hl != "BTC" {
		t.Fatalf("hl: %q err=%v", hl, err)
	}
	gr, err := cfg.NativeSymbol("grvt", "BTCUSD")
	if err != nil || gr != "BTC_USDT_Perp" {
		t.Fatalf("grvt: %q err=%v", gr, err)
	}
	if _, err := cfg.NativeSymbol("hyperliquid", "NOPE"); err == nil {
		t.Fatal("want missing symbol")
	}
}

func TestParseJSON_SymbolMapArray(t *testing.T) {
	raw := []byte(`{
  "venues": { "a": "hyperliquid", "b": "grvt" },
  "symbol_map": [{
    "symbol": "BTCUSD",
    "trading": { "strategy": "cross-venue-arb", "min_size": "0.01", "max_size": "2", "min_gap": 0.3, "kind": "perp" },
    "risk": { "fee_bps_per_leg": 5, "partial_fill_factor": 0.95, "max_book_age": "2s" },
    "venues": {
      "hyperliquid": { "symbol_name": "BTC", "fees": { "buy": { "rate_bps": 1 }, "sell": { "rate_bps": 1 } }, "budget": "10000" },
      "grvt": { "symbol_name": "BTC_USDT_Perp", "fees": { "buy": { "rate_bps": 2 }, "sell": { "rate_bps": 2 } }, "budget": "10000" }
    }
  }]
}`)
	cfg, err := config.ParseJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	hl, err := cfg.NativeSymbol("hyperliquid", "BTCUSD")
	if err != nil || hl != "BTC" {
		t.Fatalf("hl: %q %v", hl, err)
	}
	entry, ok := cfg.Lookup("BTCUSD")
	if !ok || entry.Trading.OrderInterval == nil || entry.Trading.OrderInterval.Duration() != time.Second {
		t.Fatalf("default interval: %+v", entry.Trading.OrderInterval)
	}
	if entry.Trading.MaxSize != "2" {
		t.Fatalf("max_size: %q", entry.Trading.MaxSize)
	}
}

func TestParseJSON_PerVenueBudgetsAndInterval(t *testing.T) {
	raw := []byte(`{
  "venues": { "a": "hyperliquid", "b": "grvt" },
  "symbol_map": [{
    "symbol": "BTCUSD",
    "trading": { "strategy": "cross-venue-arb", "min_size": "0.01", "max_size": "0.5", "min_gap": 0.3, "kind": "perp", "order_interval": "2s" },
    "risk": { "fee_bps_per_leg": 5, "partial_fill_factor": 0.95, "max_book_age": "2s" },
    "venues": {
      "hyperliquid": { "symbol_name": "BTC", "fees": { "buy": { "rate_bps": 1 }, "sell": { "rate_bps": 1 } }, "budget": "5000" },
      "grvt": { "symbol_name": "BTC_USDT_Perp", "fees": { "buy": { "rate_bps": 2 }, "sell": { "rate_bps": 2 } }, "budget": "5000" }
    }
  }]
}`)
	cfg, err := config.ParseJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	entry, _ := cfg.Lookup("BTCUSD")
	if entry.Venues["hyperliquid"].Budget != "5000" {
		t.Fatalf("budget: %+v", entry.Venues)
	}
	if cfg.OrderIntervalFor("BTCUSD") != 2*time.Second {
		t.Fatalf("interval: %v", cfg.OrderIntervalFor("BTCUSD"))
	}
	if cfg.EffectiveSize("BTCUSD") != "0.5" {
		t.Fatalf("effective size want 0.5 got %q", cfg.EffectiveSize("BTCUSD"))
	}
}

func TestEffectiveSize_UsesMaxSize(t *testing.T) {
	cfg := config.Default()
	cfg.UpdateSymbol("BTCUSD", func(e *config.SymbolEntry) {
		e.Trading.MinSize = "0.01"
		e.Trading.MaxSize = "0.25"
	})
	if got := cfg.EffectiveSize("BTCUSD"); got != "0.25" {
		t.Fatalf("got %q", got)
	}
}

func TestEffectiveSize_FloorsToMinSize(t *testing.T) {
	cfg := config.Default()
	cfg.UpdateSymbol("BTCUSD", func(e *config.SymbolEntry) {
		e.Trading.MinSize = "0.5"
		e.Trading.MaxSize = "2"
	})
	if got := cfg.EffectiveSize("BTCUSD"); got != "2" {
		t.Fatalf("want max_size 2, got %q", got)
	}
	cfg.UpdateSymbol("BTCUSD", func(e *config.SymbolEntry) {
		e.Trading.MaxSize = "0.25"
	})
	if err := cfg.Validate(); err == nil {
		t.Fatal("want min_size > max_size rejected")
	}
}
