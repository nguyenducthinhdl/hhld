package market_test

import (
	"testing"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/src/market"
)

func TestBookStore_SnapshotThenDelta(t *testing.T) {
	st := market.NewBookStore()
	ts := time.Unix(1, 0).UTC()
	snap := exchange.Book{
		Venue: "grvt", Symbol: "BTCUSD", Kind: exchange.KindPerp, Time: ts,
		Bids: []exchange.Level{{Price: "100", Size: "2"}, {Price: "99", Size: "1"}},
		Asks: []exchange.Level{{Price: "101", Size: "2"}},
	}
	got, err := st.Apply(market.SnapshotEvent(snap))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Bids) != 2 {
		t.Fatalf("%+v", got)
	}

	// Update size at 100, delete 99, add ask 102.
	_, err = st.Apply(market.DeltaEvent(market.BookDelta{
		Venue: "grvt", Symbol: "BTCUSD", Time: ts.Add(time.Second), Seq: 1,
		Bids: []exchange.Level{{Price: "100", Size: "3"}, {Price: "99", Size: "0"}},
		Asks: []exchange.Level{{Price: "102", Size: "1"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	book, ok := st.Get("grvt", "BTCUSD")
	if !ok {
		t.Fatal("missing book")
	}
	if len(book.Bids) != 1 || book.Bids[0].Price != "100" || book.Bids[0].Size != "3" {
		t.Fatalf("bids: %+v", book.Bids)
	}
	if len(book.Asks) != 2 {
		t.Fatalf("asks: %+v", book.Asks)
	}
}

func TestBookStore_DeltaBeforeSnapshotRejected(t *testing.T) {
	st := market.NewBookStore()
	_, err := st.Apply(market.DeltaEvent(market.BookDelta{
		Venue: "grvt", Symbol: "BTCUSD",
		Bids: []exchange.Level{{Price: "100", Size: "1"}},
	}))
	if err == nil {
		t.Fatal("want delta-before-snapshot error")
	}
}

func TestBookStore_SeqBackwardRejected(t *testing.T) {
	st := market.NewBookStore()
	_, _ = st.Apply(market.SnapshotEvent(exchange.Book{
		Venue: "grvt", Symbol: "BTCUSD", Kind: exchange.KindPerp,
		Bids: []exchange.Level{{Price: "100", Size: "1"}},
	}))
	_, err := st.Apply(market.DeltaEvent(market.BookDelta{
		Venue: "grvt", Symbol: "BTCUSD", Seq: 5,
		Bids: []exchange.Level{{Price: "100", Size: "2"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.Apply(market.DeltaEvent(market.BookDelta{
		Venue: "grvt", Symbol: "BTCUSD", Seq: 5,
		Bids: []exchange.Level{{Price: "100", Size: "3"}},
	}))
	if err == nil {
		t.Fatal("want backward seq error")
	}
}

func TestBookStore_Clear(t *testing.T) {
	st := market.NewBookStore()
	_, _ = st.Apply(market.SnapshotEvent(exchange.Book{
		Venue: "hl", Symbol: "BTCUSD", Bids: []exchange.Level{{Price: "1", Size: "1"}},
	}))
	st.Clear("hl", "BTCUSD")
	if _, ok := st.Get("hl", "BTCUSD"); ok {
		t.Fatal("want cleared")
	}
}
