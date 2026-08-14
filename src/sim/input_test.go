package sim_test

import (
	"context"
	"os"
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

func TestInputFromNDJSON_SampleAndEmpty(t *testing.T) {
	in, err := sim.InputFromNDJSON(filepath.Join("..", "..", "data", "samples", "btcusd_books.ndjson"), "BTCUSD")
	if err != nil {
		t.Fatal(err)
	}
	if len(in.Books) != 4 {
		t.Fatalf("want 4 books, got %d", len(in.Books))
	}
	venues := sim.DistinctVenues(in.Books)
	if len(venues) != 2 {
		t.Fatalf("venues: %v", venues)
	}

	empty := filepath.Join(t.TempDir(), "empty.ndjson")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	in2, err := sim.InputFromNDJSON(empty, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(in2.Books) != 0 || len(in2.Ticks) != 0 {
		t.Fatalf("empty: %+v", in2)
	}
	missing, err := sim.InputFromNDJSON(filepath.Join(t.TempDir(), "nope.ndjson"), "")
	if err != nil || len(missing.Books) != 0 {
		t.Fatalf("missing: %+v %v", missing, err)
	}
}

func TestInputFromNDJSON_GenericVenues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pair.ndjson")
	body := `{"type":"book","venue":"ex_a","symbol":"BTCUSD","kind":"perp","bids":[{"price":"1","size":"1"}],"asks":[{"price":"2","size":"1"}],"time":"2023-11-14T22:13:20Z"}
{"type":"book","venue":"ex_b","symbol":"BTCUSD","kind":"perp","bids":[{"price":"3","size":"1"}],"asks":[{"price":"4","size":"1"}],"time":"2023-11-14T22:13:20Z"}
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	in, err := sim.InputFromNDJSON(path, "BTCUSD")
	if err != nil {
		t.Fatal(err)
	}
	got := sim.DistinctVenues(in.Books)
	if len(got) != 2 || got[0] != "ex_a" || got[1] != "ex_b" {
		t.Fatalf("%v", got)
	}
}
