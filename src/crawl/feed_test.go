package crawl_test

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/config"
	"github.com/nguyenducthinhdl/hhld/src/crawl"
	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/src/exchange/fake"
)

func TestParseLiveConfig(t *testing.T) {
	raw := []byte(`{
		"output": "out.ndjson",
		"feeds": [
			{"exchange":"hyperliquid","symbol":"BTCUSD","method":"subscribe_book"},
			{"exchange":"grvt","symbol":"BTCUSD","method":"snapshot_book","interval":"1s"}
		]
	}`)
	cfg, err := crawl.ParseLiveConfigJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Output != "out.ndjson" || len(cfg.Feeds) != 2 {
		t.Fatalf("%+v", cfg)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadLiveConfig_ETHUSD(t *testing.T) {
	t.Chdir("../..")
	cfg, err := crawl.LoadLiveConfig("configs/crawl-ethusd.json")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Output != "data/crawl/ethusd-live-60m.ndjson" || cfg.Config != "configs/craw-ethusd.json" || len(cfg.Feeds) != 3 {
		t.Fatalf("%+v", cfg)
	}
	native, err := cfg.NativeSymbol("hyperliquid", "ETHUSD")
	if err != nil || native != "ETH" {
		t.Fatalf("hl: %q %v", native, err)
	}
	native, err = cfg.NativeSymbol("grvt", "ETHUSD")
	if err != nil || native != "ETH_USDT_Perp" {
		t.Fatalf("grvt: %q %v", native, err)
	}
	if _, err := cfg.NativeSymbol("hyperliquid", "ETHBTC"); err == nil {
		t.Fatal("ETHBTC must not be in symbol_map")
	}
}

func TestLoadLiveConfig_SOLUSD(t *testing.T) {
	t.Chdir("../..")
	cfg, err := crawl.LoadLiveConfig("configs/crawl-solusd.json")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Output != "data/crawl/solusd-live-60m.ndjson" || cfg.Config != "configs/craw-solusd.json" || len(cfg.Feeds) != 3 {
		t.Fatalf("%+v", cfg)
	}
	native, err := cfg.NativeSymbol("hyperliquid", "SOLUSD")
	if err != nil || native != "SOL" {
		t.Fatalf("hl: %q %v", native, err)
	}
	native, err = cfg.NativeSymbol("grvt", "SOLUSD")
	if err != nil || native != "SOL_USDT_Perp" {
		t.Fatalf("grvt: %q %v", native, err)
	}
}

func TestLoadLiveConfig_TRUMPUSD(t *testing.T) {
	t.Chdir("../..")
	cfg, err := crawl.LoadLiveConfig("configs/crawl-trumpusd.json")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Output != "data/crawl/trumpusd-live-120m.ndjson" || cfg.Config != "configs/craw-trumpusd.json" || len(cfg.Feeds) != 3 {
		t.Fatalf("%+v", cfg)
	}
	native, err := cfg.NativeSymbol("hyperliquid", "TRUMPUSD")
	if err != nil || native != "TRUMP" {
		t.Fatalf("hl: %q %v", native, err)
	}
	native, err = cfg.NativeSymbol("grvt", "TRUMPUSD")
	if err != nil || native != "TRUMP_USDT_Perp" {
		t.Fatalf("grvt: %q %v", native, err)
	}
}

func TestParseLiveConfig_InvalidMethod(t *testing.T) {
	_, err := crawl.ParseLiveConfigJSON([]byte(`{
		"output":"x.ndjson",
		"feeds":[{"exchange":"hl","symbol":"BTCUSD","method":"bad"}]
	}`))
	if err == nil {
		t.Fatal("want error")
	}
}

func TestLiveSnapshotToNDJSON(t *testing.T) {
	out := filepath.Join(t.TempDir(), "feed.ndjson")
	ts := time.Unix(1_700_000_000, 0).UTC()
	ex := fake.New("fake", exchange.NewManualClock(ts))
	ex.SetBook(exchange.Book{
		Symbol: "BTCUSD", Kind: exchange.KindPerp,
		Bids: []exchange.Level{{Price: "100", Size: "1"}},
		Asks: []exchange.Level{{Price: "101", Size: "1"}},
	})

	cfg := crawl.LiveConfig{
		Output: out,
		Kind:   exchange.KindPerp,
		SymbolMap: []config.SymbolEntry{{
			Symbol: "BTCUSD",
			Venues: map[string]config.VenueSpec{
				"fake": {SymbolName: "BTC"},
			},
		}},
		Feeds: []crawl.FeedConfig{{
			Exchange: "fake", Symbol: "BTCUSD", Method: "snapshot_book", Interval: "5ms",
		}},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	runner := crawl.Live{
		Cfg:       cfg,
		Exchanges: map[string]exchange.Exchange{"fake": ex},
	}
	if err := runner.Run(ctx); err != nil && err != context.DeadlineExceeded {
		t.Fatalf("run: %v", err)
	}

	f, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	var lines int
	for sc.Scan() {
		lines++
		var rec struct {
			Type   string `json:"type"`
			Venue  string `json:"venue"`
			Method string `json:"method"`
			Bids   []struct {
				Price string `json:"price"`
			} `json:"bids"`
		}
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			t.Fatalf("line %d: %v", lines, err)
		}
		if rec.Type != "book" || rec.Venue != "fake" || rec.Method != "snapshot_book" {
			t.Fatalf("bad record: %+v", rec)
		}
		if len(rec.Bids) != 1 || rec.Bids[0].Price != "100" {
			t.Fatalf("bids: %+v", rec.Bids)
		}
	}
	if lines == 0 {
		t.Fatal("want at least one NDJSON line")
	}
}
