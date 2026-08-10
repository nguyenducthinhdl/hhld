package strategy_test

import (
	"context"
	"testing"

	"github.com/nguyenducthinhdl/hhld/src/config"
	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/src/strategy"
)

func TestArbConfigFrom_AndName(t *testing.T) {
	cfg := config.Default()
	cfg.Symbols = []exchange.Symbol{"BTCUSD", "ETHUSD"}
	cfg.Trading.Size = "0.25"
	cfg.Trading.MinGap = 0.4
	ac := strategy.ArbConfigFrom(cfg)
	if len(ac.Symbols) != 2 || ac.Size != "0.25" || ac.MinGap != 0.4 {
		t.Fatalf("%+v", ac)
	}
	if ac.SizeBySymbol["BTCUSD"] != "0.25" {
		t.Fatalf("size by symbol: %+v", ac.SizeBySymbol)
	}
	arb := strategy.NewCrossVenueArb(ac)
	if arb.Name() != "cross-venue-arb" {
		t.Fatal(arb.Name())
	}
}

func TestCrossVenueArb_ClampsToMaxVolumeTrade(t *testing.T) {
	arb := strategy.NewCrossVenueArb(strategy.ArbConfig{
		Symbols: []exchange.Symbol{"BTCUSD"},
		Size:    "2",
		SizeBySymbol: map[exchange.Symbol]string{
			"BTCUSD": "1",
		},
		MinGap: 0.3,
	})
	books := []exchange.Book{
		{
			Venue: "hyperliquid", Symbol: "BTCUSD", Kind: exchange.KindPerp,
			Bids: []exchange.Level{{Price: "100.0", Size: "2"}},
			Asks: []exchange.Level{{Price: "100.1", Size: "2"}},
		},
		{
			Venue: "grvt", Symbol: "BTCUSD", Kind: exchange.KindPerp,
			Bids: []exchange.Level{{Price: "100.8", Size: "2"}},
			Asks: []exchange.Level{{Price: "100.9", Size: "2"}},
		},
	}
	ds, err := arb.OnBooks(context.Background(), books)
	if err != nil {
		t.Fatal(err)
	}
	if len(ds) != 1 {
		t.Fatalf("decisions: %+v", ds)
	}
	for _, leg := range ds[0].Legs {
		if leg.Size != "1" {
			t.Fatalf("want clamped size 1, got %+v", leg)
		}
	}
}

func TestCrossVenueArb_EmptyDefaultsAndNoGap(t *testing.T) {
	arb := strategy.NewCrossVenueArb(strategy.ArbConfig{MinGap: 0.5})
	books := []exchange.Book{
		{
			Venue: "hyperliquid", Symbol: "BTCUSD", Kind: exchange.KindPerp,
			Asks: []exchange.Level{{Price: "100.1", Size: "1"}},
			Bids: []exchange.Level{{Price: "100.0", Size: "1"}},
		},
		{
			Venue: "grvt", Symbol: "BTCUSD", Kind: exchange.KindPerp,
			Asks: []exchange.Level{{Price: "100.2", Size: "1"}},
			Bids: []exchange.Level{{Price: "100.15", Size: "1"}},
		},
	}
	ds, err := arb.OnBooks(context.Background(), books)
	if err != nil {
		t.Fatal(err)
	}
	if len(ds) != 0 {
		t.Fatalf("want no decision under min gap 0.5, got %+v", ds)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := arb.OnBooks(ctx, books); err == nil {
		t.Fatal("want ctx error")
	}
}
