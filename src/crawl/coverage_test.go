package crawl_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/crawl"
	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/src/warehouse"
)

func TestSampleFile_TicksAndComments(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mixed.ndjson")
	body := `# comment
{"type":"tick","venue":"hyperliquid","symbol":"BTCUSD","kind":"perp","price":"100.5","size":"0.25","side":"buy","time":"2023-11-14T22:13:21Z"}

{"type":"book","venue":"grvt","symbol":"BTCUSD","kind":"perp","bids":[{"price":"101","size":"1"}],"asks":[{"price":"101.1","size":"1"}],"time":"2023-11-14T22:13:21Z"}
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := warehouse.OpenSQLite(filepath.Join(dir, "hhld.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if err := (crawl.SampleFile{Path: path}).Run(context.Background(), st); err != nil {
		t.Fatal(err)
	}
	from := time.Date(2023, 11, 14, 0, 0, 0, 0, time.UTC)
	to := from.Add(48 * time.Hour)
	ticks, err := st.QueryTicks(context.Background(), "BTCUSD", from, to)
	if err != nil || len(ticks) != 1 || ticks[0].Price != "100.5" {
		t.Fatalf("ticks=%+v err=%v", ticks, err)
	}
	books, err := st.QueryBooks(context.Background(), "BTCUSD", from, to)
	if err != nil || len(books) != 1 {
		t.Fatalf("books=%+v err=%v", books, err)
	}
}

func TestSampleFile_Errors(t *testing.T) {
	st, err := warehouse.OpenSQLite(filepath.Join(t.TempDir(), "hhld.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if err := (crawl.SampleFile{}).Run(context.Background(), st); err == nil {
		t.Fatal("want empty path error")
	}
	if err := (crawl.SampleFile{Path: "x.ndjson"}).Run(context.Background(), nil); err == nil {
		t.Fatal("want nil store error")
	}
	missing := filepath.Join(t.TempDir(), "nested", "missing.ndjson")
	if err := (crawl.SampleFile{Path: missing}).Run(context.Background(), st); err != nil {
		t.Fatalf("missing file should be created empty: %v", err)
	}
	if _, err := os.Stat(missing); err != nil {
		t.Fatalf("want created file: %v", err)
	}

	bad := filepath.Join(t.TempDir(), "bad.ndjson")
	if err := os.WriteFile(bad, []byte(`not-json`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := (crawl.SampleFile{Path: bad}).Run(context.Background(), st); err == nil {
		t.Fatal("want JSON error")
	}

	unk := filepath.Join(t.TempDir(), "unk.ndjson")
	if err := os.WriteFile(unk, []byte(`{"type":"ohlc"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := (crawl.SampleFile{Path: unk}).Run(context.Background(), st); err == nil {
		t.Fatal("want unknown type error")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	okPath := filepath.Join(t.TempDir(), "ok.ndjson")
	_ = os.WriteFile(okPath, []byte(`{"type":"tick","venue":"hl","symbol":"BTCUSD","kind":"perp","price":"1","size":"1","time":"2023-11-14T22:13:21Z"}`+"\n"), 0o644)
	if err := (crawl.SampleFile{Path: okPath}).Run(ctx, st); err == nil {
		t.Fatal("want canceled context error")
	}
}

func TestFakeDual_FillsDefaultsAndCustomBooks(t *testing.T) {
	st, err := warehouse.OpenSQLite(filepath.Join(t.TempDir(), "hhld.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ts := time.Unix(50, 0).UTC()
	if err := (crawl.FakeDual{
		Symbol: "ETHUSD",
		Kind:   exchange.KindSpot,
		Books: []exchange.Book{{
			Venue: "hyperliquid", Time: ts,
			Bids: []exchange.Level{{Price: "1", Size: "1"}},
		}},
	}).Run(context.Background(), st); err != nil {
		t.Fatal(err)
	}
	books, err := st.QueryBooks(context.Background(), "ETHUSD", ts.Add(-time.Second), ts.Add(time.Second))
	if err != nil || len(books) != 1 || books[0].Kind != exchange.KindSpot {
		t.Fatalf("books=%+v err=%v", books, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (crawl.FakeDual{}).Run(ctx, st); err == nil {
		t.Fatal("want canceled context")
	}
}
