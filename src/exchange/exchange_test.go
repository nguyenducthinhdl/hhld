package exchange_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/exchange"
)

// Compile-time check: stub implements Exchange.
var _ exchange.Exchange = (*stubExchange)(nil)

type stubExchange struct {
	id    exchange.VenueID
	book  exchange.Book
	acks  map[string]exchange.OrderAck
	nextN int
}

func newStub(id exchange.VenueID) *stubExchange {
	return &stubExchange{
		id: id,
		book: exchange.Book{
			Venue:  id,
			Symbol: "BTCUSD",
			Kind:   exchange.KindPerp,
			Bids:   []exchange.Level{{Price: "100", Size: "1"}},
			Asks:   []exchange.Level{{Price: "101", Size: "1"}},
			Time:   time.Unix(1, 0).UTC(),
		},
		acks: make(map[string]exchange.OrderAck),
	}
}

func (s *stubExchange) ID() exchange.VenueID { return s.id }

func (s *stubExchange) SnapshotBook(ctx context.Context, symbol exchange.Symbol) (exchange.Book, error) {
	b := s.book
	b.Symbol = symbol
	return b, nil
}

func (s *stubExchange) SubscribeBook(ctx context.Context, symbol exchange.Symbol, h exchange.BookHandler) error {
	b, err := s.SnapshotBook(ctx, symbol)
	if err != nil {
		return err
	}
	h(b)
	return nil
}

func (s *stubExchange) SubscribeTicks(ctx context.Context, symbol exchange.Symbol, h exchange.TickHandler) error {
	h(exchange.Tick{
		Venue:  s.id,
		Symbol: symbol,
		Kind:   exchange.KindPerp,
		Price:  "100.5",
		Size:   "0.1",
		Side:   exchange.SideBuy,
		Time:   time.Unix(2, 0).UTC(),
	})
	return nil
}

func (s *stubExchange) PlaceOrder(ctx context.Context, req exchange.OrderRequest) (exchange.OrderAck, error) {
	s.nextN++
	id := fmt.Sprintf("ord-%d", s.nextN)
	ack := exchange.OrderAck{
		OrderID:       id,
		ClientOrderID: req.ClientOrderID,
		TraceID:       req.TraceID,
		HedgeID:       req.HedgeID,
		Symbol:        req.Symbol,
		Status:        "accepted",
		Time:          time.Unix(3, 0).UTC(),
	}
	s.acks[id] = ack
	return ack, nil
}

func (s *stubExchange) CancelOrder(ctx context.Context, orderID string) error {
	delete(s.acks, orderID)
	return nil
}

func TestExchangeStub_SnapshotAndSubscribe(t *testing.T) {
	ctx := context.Background()
	ex := newStub("hyperliquid")

	book, err := ex.SnapshotBook(ctx, "BTCUSD")
	if err != nil {
		t.Fatal(err)
	}
	if book.Venue != "hyperliquid" || book.Symbol != "BTCUSD" || book.Kind != exchange.KindPerp {
		t.Fatalf("unexpected book: %+v", book)
	}
	if len(book.Bids) != 1 || book.Bids[0].Price != "100" {
		t.Fatalf("unexpected bids: %+v", book.Bids)
	}

	var gotBook exchange.Book
	if err := ex.SubscribeBook(ctx, "BTCUSD", func(b exchange.Book) { gotBook = b }); err != nil {
		t.Fatal(err)
	}
	if gotBook.Symbol != "BTCUSD" {
		t.Fatalf("subscribe book: %+v", gotBook)
	}

	var gotTick exchange.Tick
	if err := ex.SubscribeTicks(ctx, "BTCUSD", func(tk exchange.Tick) { gotTick = tk }); err != nil {
		t.Fatal(err)
	}
	if gotTick.Price != "100.5" || gotTick.Side != exchange.SideBuy {
		t.Fatalf("subscribe tick: %+v", gotTick)
	}
}

func TestExchangeStub_PlaceAndCancelOrder(t *testing.T) {
	ctx := context.Background()
	ex := newStub("grvt")

	ack, err := ex.PlaceOrder(ctx, exchange.OrderRequest{
		ClientOrderID: "c1",
		TraceID:       "t1",
		HedgeID:       "h1",
		Symbol:        "BTCUSD",
		Kind:          exchange.KindPerp,
		Side:          exchange.SideBuy,
		Price:         "100",
		Size:          "1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if ack.Status != "accepted" || ack.ClientOrderID != "c1" || ack.HedgeID != "h1" {
		t.Fatalf("unexpected ack: %+v", ack)
	}
	if err := ex.CancelOrder(ctx, ack.OrderID); err != nil {
		t.Fatal(err)
	}
	if _, ok := ex.acks[ack.OrderID]; ok {
		t.Fatal("expected order removed after cancel")
	}
}

func TestPredictionKind_OnBookAndOrder(t *testing.T) {
	ctx := context.Background()
	ex := newStub("polymarket")
	ex.book.Kind = exchange.KindPrediction
	ex.book.Symbol = "BTC-UP"

	book, err := ex.SnapshotBook(ctx, "BTC-UP")
	if err != nil {
		t.Fatal(err)
	}
	if book.Kind != exchange.KindPrediction {
		t.Fatalf("want prediction kind, got %s", book.Kind)
	}

	ack, err := ex.PlaceOrder(ctx, exchange.OrderRequest{
		ClientOrderID: "pred-1",
		Symbol:        "BTC-UP",
		Kind:          exchange.KindPrediction,
		Side:          exchange.SideBuy,
		Price:         "0.55",
		Size:          "10",
	})
	if err != nil {
		t.Fatal(err)
	}
	if ack.Symbol != "BTC-UP" {
		t.Fatalf("unexpected ack symbol: %+v", ack)
	}
}
