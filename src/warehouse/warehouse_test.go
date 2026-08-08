package warehouse_test

import (
	"context"
	"testing"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/src/warehouse"
)

var _ warehouse.Store = (*stubStore)(nil)

type stubStore struct {
	books []exchange.Book
	ticks []exchange.Tick
}

func (s *stubStore) AppendBook(ctx context.Context, b exchange.Book) error {
	s.books = append(s.books, b)
	return nil
}

func (s *stubStore) AppendTick(ctx context.Context, t exchange.Tick) error {
	s.ticks = append(s.ticks, t)
	return nil
}

func (s *stubStore) QueryBooks(ctx context.Context, symbol exchange.Symbol, from, to time.Time) ([]exchange.Book, error) {
	var out []exchange.Book
	for _, b := range s.books {
		if b.Symbol == symbol && !b.Time.Before(from) && !b.Time.After(to) {
			out = append(out, b)
		}
	}
	return out, nil
}

func (s *stubStore) QueryTicks(ctx context.Context, symbol exchange.Symbol, from, to time.Time) ([]exchange.Tick, error) {
	var out []exchange.Tick
	for _, tk := range s.ticks {
		if tk.Symbol == symbol && !tk.Time.Before(from) && !tk.Time.After(to) {
			out = append(out, tk)
		}
	}
	return out, nil
}

func TestStore_AppendAndQuery(t *testing.T) {
	ctx := context.Background()
	st := &stubStore{}
	ts := time.Unix(100, 0).UTC()

	if err := st.AppendBook(ctx, exchange.Book{
		Venue: "hyperliquid", Symbol: "BTCUSD", Kind: exchange.KindPerp, Time: ts,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.AppendTick(ctx, exchange.Tick{
		Venue: "hyperliquid", Symbol: "BTCUSD", Kind: exchange.KindPerp, Price: "100", Size: "1", Time: ts,
	}); err != nil {
		t.Fatal(err)
	}

	books, err := st.QueryBooks(ctx, "BTCUSD", ts.Add(-time.Second), ts.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 1 {
		t.Fatalf("want 1 book, got %d", len(books))
	}

	ticks, err := st.QueryTicks(ctx, "BTCUSD", ts.Add(-time.Second), ts.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if len(ticks) != 1 || ticks[0].Price != "100" {
		t.Fatalf("unexpected ticks: %+v", ticks)
	}
}
