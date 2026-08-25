package config_test

import (
	"os"
	"path/filepath"
	"strings"
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
        "hyperliquid": { "symbol_name": "BTC", "fees": { "buy": { "rate_bps": 1 }, "sell": { "rate_bps": 1 } }, "budget": "10000" },
        "grvt": { "symbol_name": "BTC_USDT_Perp", "fees": { "buy": { "rate_bps": 2 }, "sell": { "rate_bps": 2 } }, "budget": "10000" }
      }
    },
    {
      "symbol": "ETHUSD",
      "trading": { "strategy": "cross-venue-arb", "kind": "perp", "min_size": "0.01", "max_size": "0.5", "min_gap": 0.5, "order_interval": "1s" },
      "risk": { "fee_bps_per_leg": 5, "latency_penalty": 0.05, "partial_fill_factor": 0.95, "max_book_age": "2s" },
      "venues": {
        "hyperliquid": { "symbol_name": "ETH", "fees": { "buy": { "rate_bps": 1 }, "sell": { "rate_bps": 1 } }, "budget": "5000" },
        "grvt": { "symbol_name": "ETH_USDT_Perp", "fees": { "buy": { "rate_bps": 2 }, "sell": { "rate_bps": 2 } }, "budget": "5000" }
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
      "hyperliquid": { "symbol_name": "BTC", "fees": { "buy": { "rate_bps": 1 }, "sell": { "rate_bps": 1 } }, "budget": "10000" },
      "grvt": { "symbol_name": "BTC_USDT_Perp", "fees": { "buy": { "rate_bps": 2 }, "sell": { "rate_bps": 2 } }, "budget": "10000" }
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
	if cfg.EffectiveSize("BTCUSD") != "0.0003" {
		t.Fatalf("effective size: %q", cfg.EffectiveSize("BTCUSD"))
	}
	if cfg.Env != "local" {
		t.Fatalf("env: %q", cfg.Env)
	}
	spot, err := cfg.NativeInstrument("hyperliquid", "BTCUSD", "spot")
	if err != nil || spot != "BTC" {
		t.Fatalf("spot native: %q %v", spot, err)
	}
	hl, err := cfg.NativeSymbol("hyperliquid", "BTCUSD")
	if err != nil || hl != "BTC" {
		t.Fatalf("native: %q %v", hl, err)
	}
	if _, err := cfg.NativeSymbol("hyperliquid", "ETHUSD"); err == nil {
		t.Fatal("ETHUSD must live in configs/craw-ethusd.json, not default.json")
	}
	if _, err := cfg.NativeSymbol("hyperliquid", "SOLUSD"); err == nil {
		t.Fatal("SOLUSD must live in configs/craw-solusd.json, not default.json")
	}
	if _, err := cfg.NativeSymbol("hyperliquid", "TRUMPUSD"); err == nil {
		t.Fatal("TRUMPUSD must live in configs/craw-trumpusd.json, not default.json")
	}
	entry, ok := cfg.Lookup("BTCUSD")
	if !ok {
		t.Fatal("missing BTCUSD")
	}
	hlFee := entry.Venues["hyperliquid"].Fees
	if hlFee.Buy.RateBps != 4.5 || hlFee.Sell.RateBps != 4.5 {
		t.Fatalf("hl fees: %+v", hlFee)
	}
	grFee := entry.Venues["grvt"].Fees
	if grFee.Buy.RateBps != 4.5 || grFee.Sell.RateBps != 4.5 {
		t.Fatalf("grvt fees: %+v", grFee)
	}
}

func TestLoadJSON_CrawETHUSD(t *testing.T) {
	cfg, err := config.LoadJSON("../../configs/craw-ethusd.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	syms := cfg.Symbols()
	if len(syms) != 1 || syms[0] != "ETHUSD" {
		t.Fatalf("symbols: %+v", syms)
	}
	if cfg.EffectiveSize("ETHUSD") != "0.001" {
		t.Fatalf("eth effective size: %q", cfg.EffectiveSize("ETHUSD"))
	}
	ethHL, err := cfg.NativeSymbol("hyperliquid", "ETHUSD")
	if err != nil || ethHL != "ETH" {
		t.Fatalf("eth hl native: %q %v", ethHL, err)
	}
	ethGR, err := cfg.NativeSymbol("grvt", "ETHUSD")
	if err != nil || ethGR != "ETH_USDT_Perp" {
		t.Fatalf("eth grvt native: %q %v", ethGR, err)
	}
	entry, ok := cfg.Lookup("ETHUSD")
	if !ok {
		t.Fatal("missing ETHUSD")
	}
	if entry.Trading.MinGap != 1.7 {
		t.Fatalf("min_gap: %v", entry.Trading.MinGap)
	}
}

func TestLoadJSON_CrawSOLUSD(t *testing.T) {
	cfg, err := config.LoadJSON("../../configs/craw-solusd.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	syms := cfg.Symbols()
	if len(syms) != 1 || syms[0] != "SOLUSD" {
		t.Fatalf("symbols: %+v", syms)
	}
	if cfg.EffectiveSize("SOLUSD") != "0.07" {
		t.Fatalf("sol effective size: %q", cfg.EffectiveSize("SOLUSD"))
	}
	solHL, err := cfg.NativeSymbol("hyperliquid", "SOLUSD")
	if err != nil || solHL != "SOL" {
		t.Fatalf("sol hl native: %q %v", solHL, err)
	}
	solGR, err := cfg.NativeSymbol("grvt", "SOLUSD")
	if err != nil || solGR != "SOL_USDT_Perp" {
		t.Fatalf("sol grvt native: %q %v", solGR, err)
	}
	entry, ok := cfg.Lookup("SOLUSD")
	if !ok {
		t.Fatal("missing SOLUSD")
	}
	if entry.Trading.MinGap != 0.07 {
		t.Fatalf("min_gap: %v", entry.Trading.MinGap)
	}
}

func TestLoadJSON_CrawTRUMPUSD(t *testing.T) {
	cfg, err := config.LoadJSON("../../configs/craw-trumpusd.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	syms := cfg.Symbols()
	if len(syms) != 1 || syms[0] != "TRUMPUSD" {
		t.Fatalf("symbols: %+v", syms)
	}
	if cfg.EffectiveSize("TRUMPUSD") != "0.02" {
		t.Fatalf("trump effective size: %q", cfg.EffectiveSize("TRUMPUSD"))
	}
	hl, err := cfg.NativeSymbol("hyperliquid", "TRUMPUSD")
	if err != nil || hl != "TRUMP" {
		t.Fatalf("trump hl native: %q %v", hl, err)
	}
	gr, err := cfg.NativeSymbol("grvt", "TRUMPUSD")
	if err != nil || gr != "TRUMP_USDT_Perp" {
		t.Fatalf("trump grvt native: %q %v", gr, err)
	}
	entry, ok := cfg.Lookup("TRUMPUSD")
	if !ok {
		t.Fatal("missing TRUMPUSD")
	}
	if entry.Trading.MinGap != 0.000001 {
		t.Fatalf("min_gap: %v", entry.Trading.MinGap)
	}
}

func TestValidate_RejectsBadVenues(t *testing.T) {
	cfg := config.Default()
	cfg.Venues.B = cfg.Venues.A
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for identical venues")
	}
}

func TestPinEnv(t *testing.T) {
	cfg := config.Default()
	if err := cfg.PinEnv("local"); err != nil {
		t.Fatal(err)
	}
	cfg.Env = "staging"
	if err := cfg.PinEnv("local"); err == nil {
		t.Fatal("want mismatch")
	}
	if err := cfg.PinEnv("staging"); err != nil {
		t.Fatal(err)
	}
	if err := cfg.PinEnv("testnet"); err != nil {
		t.Fatal(err)
	}
	cfg.Env = "prod"
	if err := cfg.PinEnv("mainnet"); err != nil {
		t.Fatal(err)
	}
}

func TestNativeInstrument_SpotAndPerp(t *testing.T) {
	cfg := config.Default()
	perp, err := cfg.NativeInstrument("grvt", "BTCUSD", "perp")
	if err != nil || perp != "BTC_USDT_Perp" {
		t.Fatalf("perp: %q %v", perp, err)
	}
	spot, err := cfg.NativeInstrument("grvt", "BTCUSD", "spot")
	if err != nil || spot != "BTC_USDT" {
		t.Fatalf("spot: %q %v", spot, err)
	}
	spec := cfg.SymbolMap[0].Venues["grvt"]
	spec.SpotSymbolName = ""
	cfg.SymbolMap[0].Venues["grvt"] = spec
	if _, err := cfg.NativeInstrument("grvt", "BTCUSD", "spot"); err == nil {
		t.Fatal("want no perp fallback")
	}
	if _, err := cfg.NativeInstrument("hyperliquid", "NOPE", "perp"); err == nil {
		t.Fatal("want missing symbol")
	}
	if cfg.KindFor("BTCUSD") != "perp" || cfg.MinGapFor("BTCUSD") != 0.3 {
		t.Fatalf("kind/gap")
	}
	if cfg.KindFor("NOPE") != "perp" || cfg.MinGapFor("NOPE") != 0 {
		t.Fatal("unknown symbol defaults")
	}
}

func TestLoadJSON_EnvKnobFiles(t *testing.T) {
	stg, err := config.LoadJSON("../../configs/default-staging.json")
	if err != nil {
		t.Fatal(err)
	}
	if stg.Env != "staging" || stg.Timeouts.Book.Duration() != 8*time.Second || stg.Risk.MaxInFlight != 2 {
		t.Fatalf("staging: %+v", stg)
	}
	if stg.EffectiveSize("BTCUSD") != "0.0003" {
		t.Fatalf("staging size %q", stg.EffectiveSize("BTCUSD"))
	}
	prod, err := config.LoadJSON("../../configs/default-production.json")
	if err != nil {
		t.Fatal(err)
	}
	if prod.Env != "prod" || prod.Risk.MaxInFlight != 1 {
		t.Fatalf("prod: %+v", prod)
	}
	if prod.EffectiveSize("BTCUSD") != "0.00015" {
		t.Fatalf("prod size must equal max_size, got %q", prod.EffectiveSize("BTCUSD"))
	}
	entry, _ := prod.Lookup("BTCUSD")
	if entry.Venues["hyperliquid"].Budget != "50" {
		t.Fatalf("prod budget %+v", entry.Venues)
	}
	if entry.Trading.MinValue != "10" || entry.Trading.MaxValue != "50" {
		t.Fatalf("prod notional bounds %+v", entry.Trading)
	}
}

func TestEnvJSON_NoHostsOrSecrets(t *testing.T) {
	for _, name := range []string{"default.json", "default-staging.json", "default-production.json"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "configs", name))
		if err != nil {
			t.Fatal(err)
		}
		s := string(raw)
		for _, bad := range []string{"https://", "wss://", "PRIVATE_KEY", "API_KEY"} {
			if strings.Contains(s, bad) {
				t.Fatalf("%s contains %q", name, bad)
			}
		}
	}
}
