package sim_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/src/sim"
	"github.com/nguyenducthinhdl/hhld/src/warehouse"
)

func TestInputFromStore(t *testing.T) {
	ctx := context.Background()
	st, err := warehouse.OpenSQLite(filepath.Join(t.TempDir(), "hhld.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ts := time.Unix(200, 0).UTC()
	if err := st.AppendBook(ctx, exchange.Book{
		Venue: "grvt", Symbol: "BTCUSD", Kind: exchange.KindPerp,
		Bids: []exchange.Level{{Price: "101", Size: "1"}},
		Time: ts,
	}); err != nil {
		t.Fatal(err)
	}

	in, err := sim.InputFromStore(ctx, st, "BTCUSD", ts.Add(-time.Hour), ts.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(in.Books) != 1 || in.Books[0].Venue != "grvt" {
		t.Fatalf("input: %+v", in)
	}
}
