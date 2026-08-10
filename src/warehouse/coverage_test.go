package warehouse_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/src/warehouse"
)

func TestSQLite_OpenFailure(t *testing.T) {
	dir := t.TempDir()
	// Opening a directory path as a DB should fail schema init / open.
	if _, err := warehouse.OpenSQLite(dir); err == nil {
		t.Fatal("want open failure on directory path")
	}
}

func TestSQLite_CloseNilAndCanceled(t *testing.T) {
	var nilStore *warehouse.SQLite
	if err := nilStore.Close(); err != nil {
		t.Fatal(err)
	}

	st, err := warehouse.OpenSQLite(filepath.Join(t.TempDir(), "hhld.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := st.AppendBook(ctx, exchange.Book{Venue: "hl", Symbol: "BTCUSD", Kind: exchange.KindPerp, Time: time.Now()}); err == nil {
		t.Fatal("want append book ctx error")
	}
	if err := st.AppendTick(ctx, exchange.Tick{Venue: "hl", Symbol: "BTCUSD", Kind: exchange.KindPerp, Price: "1", Size: "1", Time: time.Now()}); err == nil {
		t.Fatal("want append tick ctx error")
	}
	if _, err := st.QueryBooks(ctx, "BTCUSD", time.Unix(0, 0), time.Now()); err == nil {
		t.Fatal("want query books ctx error")
	}
	if _, err := st.QueryTicks(ctx, "BTCUSD", time.Unix(0, 0), time.Now()); err == nil {
		t.Fatal("want query ticks ctx error")
	}
}

func TestSQLite_EmptyQueryRange(t *testing.T) {
	st, err := warehouse.OpenSQLite(filepath.Join(t.TempDir(), "hhld.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ts := time.Unix(300, 0).UTC()
	_ = st.AppendBook(context.Background(), exchange.Book{
		Venue: "hl", Symbol: "BTCUSD", Kind: exchange.KindPerp,
		Bids: []exchange.Level{{Price: "1", Size: "1"}},
		Asks: []exchange.Level{{Price: "2", Size: "1"}},
		Time: ts,
	})
	_ = st.AppendTick(context.Background(), exchange.Tick{
		Venue: "hl", Symbol: "BTCUSD", Kind: exchange.KindPerp,
		Price: "1.5", Size: "1", Side: exchange.SideSell, Time: ts,
	})

	books, err := st.QueryBooks(context.Background(), "ETHUSD", ts.Add(-time.Hour), ts.Add(time.Hour))
	if err != nil || len(books) != 0 {
		t.Fatalf("want empty books, got %+v err=%v", books, err)
	}
	ticks, err := st.QueryTicks(context.Background(), "ETHUSD", ts.Add(-time.Hour), ts.Add(time.Hour))
	if err != nil || len(ticks) != 0 {
		t.Fatalf("want empty ticks, got %+v err=%v", ticks, err)
	}
}
