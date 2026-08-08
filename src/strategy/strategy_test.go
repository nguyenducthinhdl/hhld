package strategy_test

import (
	"context"
	"testing"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/src/strategy"
)

var _ strategy.Strategy = (*stubStrategy)(nil)

type stubStrategy struct {
	name string
}

func (s *stubStrategy) Name() string { return s.name }

func (s *stubStrategy) OnBooks(ctx context.Context, books []exchange.Book) ([]strategy.Decision, error) {
	if len(books) < 2 {
		return nil, nil
	}
	// Fake arb: buy cheap venue, sell expensive venue.
	buy, sell := books[0], books[1]
	if bestAsk(buy) > bestBid(sell) {
		buy, sell = sell, buy
	}
	return []strategy.Decision{{
		TraceID: "trace-arb-1",
		Legs: []strategy.Leg{
			{Venue: buy.Venue, Symbol: buy.Symbol, Kind: buy.Kind, Side: exchange.SideBuy, Price: bestAsk(buy), Size: "1"},
			{Venue: sell.Venue, Symbol: sell.Symbol, Kind: sell.Kind, Side: exchange.SideSell, Price: bestBid(sell), Size: "1"},
		},
		Reason: "stub-arb",
	}}, nil
}

func bestAsk(b exchange.Book) string {
	if len(b.Asks) == 0 {
		return "0"
	}
	return b.Asks[0].Price
}

func bestBid(b exchange.Book) string {
	if len(b.Bids) == 0 {
		return "0"
	}
	return b.Bids[0].Price
}

func TestStrategy_OnBooksEmitsTwoLegArb(t *testing.T) {
	s := &stubStrategy{name: "stub-arb"}
	if s.Name() != "stub-arb" {
		t.Fatalf("name: %s", s.Name())
	}

	books := []exchange.Book{
		{
			Venue: "hyperliquid", Symbol: "BTCUSD", Kind: exchange.KindPerp,
			Bids: []exchange.Level{{Price: "100", Size: "1"}},
			Asks: []exchange.Level{{Price: "100.1", Size: "1"}},
			Time: time.Unix(1, 0).UTC(),
		},
		{
			Venue: "grvt", Symbol: "BTCUSD", Kind: exchange.KindPerp,
			Bids: []exchange.Level{{Price: "101", Size: "1"}},
			Asks: []exchange.Level{{Price: "101.1", Size: "1"}},
			Time: time.Unix(1, 0).UTC(),
		},
	}

	decisions, err := s.OnBooks(context.Background(), books)
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions) != 1 {
		t.Fatalf("want 1 decision, got %d", len(decisions))
	}
	d := decisions[0]
	if d.TraceID == "" || d.HedgeID != "" {
		t.Fatalf("arb should have TraceID and empty HedgeID: %+v", d)
	}
	if len(d.Legs) != 2 {
		t.Fatalf("want 2 legs, got %+v", d.Legs)
	}
	if d.Legs[0].Side != exchange.SideBuy || d.Legs[1].Side != exchange.SideSell {
		t.Fatalf("unexpected legs: %+v", d.Legs)
	}
}

func TestStrategy_HedgeDecisionShape(t *testing.T) {
	d := strategy.Decision{
		TraceID: "trace-hedge-1",
		HedgeID: "hedge-1",
		Legs: []strategy.Leg{
			{Venue: "polymarket", Symbol: "BTC-UP", Kind: exchange.KindPrediction, Side: exchange.SideBuy, Price: "0.55", Size: "10"},
			{Venue: "hyperliquid", Symbol: "BTCUSD", Kind: exchange.KindPerp, Side: exchange.SideSell, Price: "100", Size: "1"},
		},
		Reason: "pred-crypto-hedge",
	}
	if d.HedgeID == "" || len(d.Legs) != 2 {
		t.Fatalf("invalid hedge decision: %+v", d)
	}
	if d.Legs[0].Kind != exchange.KindPrediction || d.Legs[1].Kind != exchange.KindPerp {
		t.Fatalf("hedge kinds: %+v", d.Legs)
	}
}
