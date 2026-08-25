package strategy_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/src/exchange/fake"
	"github.com/nguyenducthinhdl/hhld/src/strategy"
)

func TestCrossVenueArb_EmitsPaperDecisionOnGap(t *testing.T) {
	arb := strategy.NewCrossVenueArb(strategy.ArbConfig{
		Symbols: []exchange.Symbol{"BTCUSD"},
		Size:    "1",
		MinGap:  0.3,
	})
	books := []exchange.Book{
		{
			Venue: "hyperliquid", Symbol: "BTCUSD", Kind: exchange.KindPerp,
			Bids: []exchange.Level{{Price: "100.0", Size: "2"}},
			Asks: []exchange.Level{{Price: "100.1", Size: "2"}},
			Time: time.Unix(1, 0).UTC(),
		},
		{
			Venue: "grvt", Symbol: "BTCUSD", Kind: exchange.KindPerp,
			Bids: []exchange.Level{{Price: "100.8", Size: "2"}},
			Asks: []exchange.Level{{Price: "100.9", Size: "2"}},
			Time: time.Unix(1, 0).UTC(),
		},
	}
	decisions, err := arb.OnBooks(context.Background(), books)
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions) != 1 {
		t.Fatalf("want 1 decision, got %d", len(decisions))
	}
	d := decisions[0]
	if len(d.Legs) != 2 || d.TraceID == "" || d.HedgeID != "" {
		t.Fatalf("unexpected decision: %+v", d)
	}
	if d.Legs[0].Side != exchange.SideBuy || d.Legs[0].Venue != "hyperliquid" {
		t.Fatalf("buy leg: %+v", d.Legs[0])
	}
	if d.Legs[1].Side != exchange.SideSell || d.Legs[1].Venue != "grvt" {
		t.Fatalf("sell leg: %+v", d.Legs[1])
	}

	dual := fake.NewDual("hyperliquid", "grvt", time.Unix(1, 0).UTC())
	venues := strategy.Venues{"hyperliquid": dual.A, "grvt": dual.B}
	results, err := strategy.PlaceDecision(context.Background(), venues, d)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].Err != nil || results[1].Err != nil {
		t.Fatalf("place results: %+v", results)
	}
	if results[0].Ack.Status != "filled" || results[1].Ack.Status != "filled" {
		t.Fatalf("acks: %+v", results)
	}
}

func TestPlaceDecision_OneLegOrderTimeout(t *testing.T) {
	dual := fake.NewDual("hyperliquid", "grvt", time.Unix(1, 0).UTC())
	dual.A.SetOrderDelay(80 * time.Millisecond)
	dual.B.SetOrderDelay(0)

	d := strategy.Decision{
		TraceID: "arb-one-leg-timeout",
		Legs: []strategy.Leg{
			{Venue: "hyperliquid", Symbol: "BTCUSD", Kind: exchange.KindPerp, Side: exchange.SideBuy, Price: "100.1", Size: "1"},
			{Venue: "grvt", Symbol: "BTCUSD", Kind: exchange.KindPerp, Side: exchange.SideSell, Price: "100.8", Size: "1"},
		},
	}
	venues := strategy.Venues{"hyperliquid": dual.A, "grvt": dual.B}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()

	results, err := strategy.PlaceDecision(ctx, venues, d)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want DeadlineExceeded in chain, got %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("want 2 leg results, got %d", len(results))
	}

	var timedOut, succeeded int
	for _, r := range results {
		if r.Err != nil {
			if !errors.Is(r.Err, context.DeadlineExceeded) {
				t.Fatalf("leg %d err: %v", r.Index, r.Err)
			}
			timedOut++
		} else if r.Ack.Status == "filled" || r.Ack.Status == "accepted" {
			succeeded++
		}
	}
	if timedOut != 1 || succeeded != 1 {
		t.Fatalf("want 1 timeout + 1 success, got timedOut=%d succeeded=%d results=%+v", timedOut, succeeded, results)
	}
}

func TestPlaceDecision_TwoLegsOrderTimeout(t *testing.T) {
	dual := fake.NewDual("hyperliquid", "grvt", time.Unix(1, 0).UTC())
	dual.A.SetOrderDelay(80 * time.Millisecond)
	dual.B.SetOrderDelay(80 * time.Millisecond)

	d := strategy.Decision{
		TraceID: "arb-two-leg-timeout",
		Legs: []strategy.Leg{
			{Venue: "hyperliquid", Symbol: "BTCUSD", Kind: exchange.KindPerp, Side: exchange.SideBuy, Price: "100.1", Size: "1"},
			{Venue: "grvt", Symbol: "BTCUSD", Kind: exchange.KindPerp, Side: exchange.SideSell, Price: "100.8", Size: "1"},
		},
	}
	venues := strategy.Venues{"hyperliquid": dual.A, "grvt": dual.B}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()

	results, err := strategy.PlaceDecision(ctx, venues, d)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want DeadlineExceeded in chain, got %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("want 2 results, got %d", len(results))
	}
	for _, r := range results {
		if r.Err == nil || !errors.Is(r.Err, context.DeadlineExceeded) {
			t.Fatalf("leg %d want deadline, got ack=%+v err=%v", r.Index, r.Ack, r.Err)
		}
	}
}

func TestPlaceDecision_TwoLegsOrderDelaySuccess(t *testing.T) {
	dual := fake.NewDual("hyperliquid", "grvt", time.Unix(1, 0).UTC())
	dual.A.SetOrderDelay(15 * time.Millisecond)
	dual.B.SetOrderDelay(15 * time.Millisecond)

	d := strategy.Decision{
		TraceID: "arb-two-leg-delay-ok",
		Legs: []strategy.Leg{
			{Venue: "hyperliquid", Symbol: "BTCUSD", Kind: exchange.KindPerp, Side: exchange.SideBuy, Price: "100.1", Size: "1"},
			{Venue: "grvt", Symbol: "BTCUSD", Kind: exchange.KindPerp, Side: exchange.SideSell, Price: "100.8", Size: "1"},
		},
	}
	venues := strategy.Venues{"hyperliquid": dual.A, "grvt": dual.B}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	results, err := strategy.PlaceDecision(ctx, venues, d)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("want 2 results, got %d", len(results))
	}
	for _, r := range results {
		if r.Err != nil || (r.Ack.Status != "filled" && r.Ack.Status != "accepted") {
			t.Fatalf("leg %d: %+v", r.Index, r)
		}
	}
}

func TestSnapshotBooks_OneLegBookTimeout(t *testing.T) {
	dual := fake.NewDual("hyperliquid", "grvt", time.Unix(1, 0).UTC())
	dual.A.SetBook(exchange.Book{
		Symbol: "BTCUSD", Kind: exchange.KindPerp,
		Bids: []exchange.Level{{Price: "100.0", Size: "1"}},
		Asks: []exchange.Level{{Price: "100.1", Size: "1"}},
	})
	dual.B.SetBook(exchange.Book{
		Symbol: "BTCUSD", Kind: exchange.KindPerp,
		Bids: []exchange.Level{{Price: "100.8", Size: "1"}},
		Asks: []exchange.Level{{Price: "100.9", Size: "1"}},
	})
	dual.A.SetBookDelay(80 * time.Millisecond)
	dual.B.SetBookDelay(0)

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()

	var errA, errB error
	var bookB exchange.Book
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, errA = dual.A.SnapshotBook(ctx, "BTCUSD")
	}()
	go func() {
		defer wg.Done()
		bookB, errB = dual.B.SnapshotBook(ctx, "BTCUSD")
	}()
	wg.Wait()

	if errA == nil || !errors.Is(errA, context.DeadlineExceeded) {
		t.Fatalf("venue A want book timeout, got %v", errA)
	}
	if errB != nil {
		t.Fatalf("venue B want success, got %v", errB)
	}
	if bookB.Symbol != "BTCUSD" {
		t.Fatalf("venue B book: %+v", bookB)
	}
}

func TestSnapshotBooks_TwoLegsBookTimeout(t *testing.T) {
	dual := fake.NewDual("hyperliquid", "grvt", time.Unix(1, 0).UTC())
	dual.A.SetBook(exchange.Book{
		Symbol: "BTCUSD", Kind: exchange.KindPerp,
		Bids: []exchange.Level{{Price: "100.0", Size: "1"}},
		Asks: []exchange.Level{{Price: "100.1", Size: "1"}},
	})
	dual.B.SetBook(exchange.Book{
		Symbol: "BTCUSD", Kind: exchange.KindPerp,
		Bids: []exchange.Level{{Price: "100.8", Size: "1"}},
		Asks: []exchange.Level{{Price: "100.9", Size: "1"}},
	})
	dual.A.SetBookDelay(80 * time.Millisecond)
	dual.B.SetBookDelay(80 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()

	errCh := make(chan error, 2)
	go func() { _, err := dual.A.SnapshotBook(ctx, "BTCUSD"); errCh <- err }()
	go func() { _, err := dual.B.SnapshotBook(ctx, "BTCUSD"); errCh <- err }()
	for i := 0; i < 2; i++ {
		err := <-errCh
		if err == nil || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("want both book timeouts, got %v", err)
		}
	}
}
