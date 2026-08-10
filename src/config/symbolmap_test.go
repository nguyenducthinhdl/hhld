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

func TestParseJSON_LegacySymbolMap(t *testing.T) {
	raw := []byte(`{
  "symbols": ["BTCUSD"],
  "trading": { "strategy": "cross-venue-arb", "size": "2", "min_gap": 0.3, "kind": "perp" },
  "venues": { "a": "hyperliquid", "b": "grvt" },
  "symbol_map": {
    "BTCUSD": { "hyperliquid": "BTC", "grvt": "BTC_USDT_Perp" }
  }
}`)
	cfg, err := config.ParseJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	hl, err := cfg.NativeSymbol("hyperliquid", "BTCUSD")
	if err != nil || hl != "BTC" {
		t.Fatalf("hl: %q %v", hl, err)
	}
	entry := cfg.SymbolMap["BTCUSD"]
	if entry.OrderInterval == nil || entry.OrderInterval.Duration() != time.Second {
		t.Fatalf("default interval: %v", entry.OrderInterval)
	}
	if entry.MaxVolumeTrade != "2" {
		t.Fatalf("default max vol from trading.size: %q", entry.MaxVolumeTrade)
	}
}

func TestParseJSON_RichSymbolMapAndBudgets(t *testing.T) {
	raw := []byte(`{
  "symbols": ["BTCUSD"],
  "trading": { "strategy": "cross-venue-arb", "size": "2", "min_gap": 0.3, "kind": "perp" },
  "venues": { "a": "hyperliquid", "b": "grvt" },
  "risk": {
    "budgets": { "hyperliquid/BTCUSD": "5000", "grvt/BTCUSD": "5000" }
  },
  "symbol_map": {
    "BTCUSD": {
      "venues": { "hyperliquid": "BTC", "grvt": "BTC_USDT_Perp" },
      "order_interval": "2s",
      "max_volume_trade": "0.5"
    }
  }
}`)
	cfg, err := config.ParseJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Risk.Budgets["hyperliquid/BTCUSD"] != "5000" {
		t.Fatalf("budgets: %+v", cfg.Risk.Budgets)
	}
	if cfg.OrderIntervalFor("BTCUSD") != 2*time.Second {
		t.Fatalf("interval: %v", cfg.OrderIntervalFor("BTCUSD"))
	}
	if cfg.EffectiveSize("BTCUSD") != "0.5" {
		t.Fatalf("effective size want 0.5 got %q", cfg.EffectiveSize("BTCUSD"))
	}
	if cfg.MaxVolumeTradeFor("BTCUSD") != 0.5 {
		t.Fatalf("max vol: %v", cfg.MaxVolumeTradeFor("BTCUSD"))
	}
}

func TestEffectiveSize_NoClampWhenSizeSmaller(t *testing.T) {
	cfg := config.Default()
	cfg.Trading.Size = "0.25"
	cfg.SymbolMap["BTCUSD"] = config.SymbolEntry{
		Venues:         cfg.SymbolMap["BTCUSD"].Venues,
		OrderInterval:  cfg.SymbolMap["BTCUSD"].OrderInterval,
		MaxVolumeTrade: "1",
	}
	if got := cfg.EffectiveSize("BTCUSD"); got != "0.25" {
		t.Fatalf("got %q", got)
	}
}
