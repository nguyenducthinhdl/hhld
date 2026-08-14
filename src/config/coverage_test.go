package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/config"
)

func TestDuration_MarshalUnmarshal(t *testing.T) {
	var d config.Duration
	if err := json.Unmarshal([]byte(`"150ms"`), &d); err != nil {
		t.Fatal(err)
	}
	if d.Duration() != 150*time.Millisecond {
		t.Fatalf("got %v", d.Duration())
	}
	b, err := d.MarshalJSON()
	if err != nil || string(b) != `"150ms"` {
		t.Fatalf("marshal: %s err=%v", b, err)
	}

	var n config.Duration
	if err := json.Unmarshal([]byte(`25000000`), &n); err != nil {
		t.Fatal(err)
	}
	if n.Duration() != 25*time.Millisecond {
		t.Fatalf("nano: %v", n.Duration())
	}

	var empty config.Duration
	if err := json.Unmarshal([]byte(`""`), &empty); err != nil || empty.Duration() != 0 {
		t.Fatalf("empty: %v err=%v", empty, err)
	}
	if err := json.Unmarshal([]byte(`"notadur"`), &empty); err == nil {
		t.Fatal("want parse error")
	}
	if err := json.Unmarshal([]byte(`true`), &empty); err == nil {
		t.Fatal("want type error")
	}
}

func TestValidate_MoreRejects(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*config.Config)
	}{
		{"no symbols", func(c *config.Config) { c.SymbolMap = nil }},
		{"empty symbol", func(c *config.Config) { c.SymbolMap[0].Symbol = "" }},
		{"no strategy", func(c *config.Config) { c.SymbolMap[0].Trading.Strategy = "" }},
		{"no min_size", func(c *config.Config) { c.SymbolMap[0].Trading.MinSize = "" }},
		{"no max_size", func(c *config.Config) { c.SymbolMap[0].Trading.MaxSize = "" }},
		{"min gt max", func(c *config.Config) { c.SymbolMap[0].Trading.MinSize, c.SymbolMap[0].Trading.MaxSize = "2", "1" }},
		{"neg gap", func(c *config.Config) { c.SymbolMap[0].Trading.MinGap = -1 }},
		{"empty venue a", func(c *config.Config) { c.Venues.A = "" }},
		{"bad fill factor", func(c *config.Config) { c.SymbolMap[0].Risk.PartialFillFactor = 1.5 }},
		{"neg in flight", func(c *config.Config) { c.Risk.MaxInFlight = -1 }},
		{"neg fee bps", func(c *config.Config) { c.SymbolMap[0].Risk.FeeBpsPerLeg = -1 }},
		{"empty fee venue", func(c *config.Config) { c.SymbolMap[0].Venues[""] = config.VenueSpec{SymbolName: "x"} }},
		{"neg fee amount", func(c *config.Config) {
			spec := c.SymbolMap[0].Venues["hyperliquid"]
			spec.Fees.Fixed = -1
			c.SymbolMap[0].Venues["hyperliquid"] = spec
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Default()
			tc.mut(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("want error")
			}
		})
	}
}

func TestParseJSON_InvalidAndLoadMissing(t *testing.T) {
	if _, err := config.ParseJSON([]byte(`{`)); err == nil {
		t.Fatal("want parse error")
	}
	if _, err := config.ParseJSON([]byte(`{"symbol_map":[]}`)); err == nil {
		t.Fatal("want validate error")
	}
	if _, err := config.LoadJSON(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("want read error")
	}

	path := filepath.Join(t.TempDir(), "ok.json")
	raw, _ := json.Marshal(config.Default())
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadJSON(path)
	if err != nil || len(cfg.Symbols()) == 0 {
		t.Fatalf("load: %+v err=%v", cfg, err)
	}
}
