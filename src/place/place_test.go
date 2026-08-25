package place_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/admin"
	"github.com/nguyenducthinhdl/hhld/src/config"
	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/src/exchange/fake"
	"github.com/nguyenducthinhdl/hhld/src/ledger"
	"github.com/nguyenducthinhdl/hhld/src/place"
	"github.com/nguyenducthinhdl/hhld/src/secrets"
)

func clearGates(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"HL_TESTNET_PRIVATE_KEY", "HL_MAINNET_PRIVATE_KEY", "HL_ACCOUNT_ADDRESS",
		"GRVT_STAGING_API_KEY", "GRVT_TESTNET_API_KEY", "GRVT_PROD_API_KEY",
		"GRVT_STAGING_SIGNING_KEY", "GRVT_TESTNET_SIGNING_KEY", "GRVT_PROD_SIGNING_KEY",
		"GRVT_COOKIE", "GRAVITY_COOKIE",
		secrets.KillEnv, secrets.VenueEnvVar, secrets.LiveOrders,
	} {
		t.Setenv(k, "")
	}
}

func testCfg() config.Config {
	cfg := config.Default()
	cfg.Env = "local"
	return cfg
}

func TestLocal_PlacesAndLedgers(t *testing.T) {
	clearGates(t)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "hhld-ledger.db")
	st, err := ledger.Open(path, "local")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ex := fake.New("hyperliquid", nil)
	ex.SetBook(exchange.Book{
		Symbol: "BTCUSD", Kind: exchange.KindPerp, Time: time.Now().UTC(),
		Bids: []exchange.Level{{Price: "100000", Size: "1"}},
		Asks: []exchange.Level{{Price: "100001", Size: "1"}},
	})
	res, err := place.Local(ctx, place.Request{
		Cfg: testCfg(), Env: "local", Venue: "hyperliquid",
		Kind: exchange.KindPerp, Symbol: "BTCUSD", Side: exchange.SideBuy,
		Size: "0.00015", Ledger: st, Exchange: ex,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Ack.Status != "filled" || res.Get.Status != "filled" || res.TraceID == "" {
		t.Fatalf("result %+v", res)
	}
	if res.Ack.OrderID != res.Get.OrderID {
		t.Fatalf("ack/get mismatch %+v", res)
	}
	raw, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(raw)), "private") || strings.Contains(string(raw), "0xabc") {
		t.Fatalf("secrets in json: %s", raw)
	}
	orders, err := st.ListOrders(ctx, admin.Filter{TraceID: res.TraceID})
	if err != nil || len(orders) != 1 {
		t.Fatalf("orders %+v %v", orders, err)
	}
	if orders[0].Env != "local" || orders[0].TIF != exchange.TIFIOC {
		t.Fatalf("stamp %+v", orders[0])
	}
	got, err := ex.GetOrder(ctx, res.Ack.ClientOrderID)
	if err != nil || got.OrderID != res.Ack.OrderID {
		t.Fatalf("GetOrder cloid: %+v %v", got, err)
	}
}

func TestLocal_RefuseLiveKey(t *testing.T) {
	clearGates(t)
	t.Setenv("HL_TESTNET_PRIVATE_KEY", "0xabc")
	_, err := place.Local(context.Background(), place.Request{
		Cfg: testCfg(), Env: "local", Venue: "hyperliquid",
		Kind: exchange.KindPerp, Symbol: "BTCUSD", Side: exchange.SideBuy, Size: "0.00015",
		LedgerPath: filepath.Join(t.TempDir(), "x.db"),
	})
	if err == nil || !strings.Contains(err.Error(), "live venue keys") {
		t.Fatalf("got %v", err)
	}
}

func TestLocal_Kill(t *testing.T) {
	clearGates(t)
	t.Setenv(secrets.KillEnv, "1")
	_, err := place.Local(context.Background(), place.Request{
		Cfg: testCfg(), Env: "local", Venue: "hyperliquid",
		Kind: exchange.KindPerp, Symbol: "BTCUSD", Side: exchange.SideBuy, Size: "0.00015",
		LedgerPath: filepath.Join(t.TempDir(), "x.db"),
	})
	if err == nil || !strings.Contains(err.Error(), "kill") {
		t.Fatalf("got %v", err)
	}
}

func TestLocal_VenueEnvNotImplemented(t *testing.T) {
	clearGates(t)
	_, err := place.Local(context.Background(), place.Request{
		Cfg: testCfg(), Env: "testnet", Venue: "hyperliquid",
		Kind: exchange.KindPerp, Symbol: "BTCUSD", Side: exchange.SideBuy, Size: "0.00015",
		LedgerPath: filepath.Join(t.TempDir(), "x.db"),
	})
	if err == nil || !strings.Contains(err.Error(), "Local only") {
		t.Fatalf("got %v", err)
	}
}

func TestRun_ProdRefused(t *testing.T) {
	clearGates(t)
	_, err := place.Run(context.Background(), place.Request{
		Cfg: testCfg(), Env: "prod", Venue: "hyperliquid",
		Kind: exchange.KindPerp, Symbol: "BTCUSD", Side: exchange.SideBuy, Size: "0.00015",
		LedgerPath: filepath.Join(t.TempDir(), "x.db"),
	})
	if err == nil || !strings.Contains(err.Error(), "not in this slice") {
		t.Fatalf("got %v", err)
	}
}

func TestVenue_Gates(t *testing.T) {
	clearGates(t)
	cfg := testCfg()
	cfg.Env = "testnet"
	path := filepath.Join(t.TempDir(), "x.db")
	_, err := place.Venue(context.Background(), place.Request{
		Cfg: cfg, Env: "testnet", Venue: "hyperliquid",
		Kind: exchange.KindPerp, Symbol: "BTCUSD", Side: exchange.SideBuy, Size: "0.00015",
		LedgerPath: path,
	})
	if err == nil || !strings.Contains(err.Error(), "LIVE_ORDERS") {
		t.Fatalf("live orders: %v", err)
	}
	t.Setenv(secrets.LiveOrders, "1")
	_, err = place.Venue(context.Background(), place.Request{
		Cfg: cfg, Env: "testnet", Venue: "grvt",
		Kind: exchange.KindPerp, Symbol: "BTCUSD", Side: exchange.SideBuy, Size: "0.00015",
		LedgerPath: path,
	})
	if err == nil || !strings.Contains(err.Error(), "hyperliquid") {
		t.Fatalf("grvt: %v", err)
	}
	t.Setenv(secrets.HLAccountAddress, "0xabc")
	t.Setenv(secrets.HLTestnetPrivateKey, "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	ex := fake.New("hyperliquid", nil)
	ex.SetBook(exchange.Book{
		Symbol: "BTCUSD", Kind: exchange.KindPerp, Time: time.Now().UTC(),
		Bids: []exchange.Level{{Price: "100000", Size: "1"}},
		Asks: []exchange.Level{{Price: "100001", Size: "1"}},
	})
	res, err := place.Venue(context.Background(), place.Request{
		Cfg: cfg, Env: "testnet", Venue: "hyperliquid",
		Kind: exchange.KindPerp, Symbol: "BTCUSD", Side: exchange.SideBuy, Size: "0.00015",
		LedgerPath: path,
		Exchange:   ex,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(res.TraceID, "testnet-") {
		t.Fatalf("trace %q", res.TraceID)
	}
}

func TestRun_Local(t *testing.T) {
	clearGates(t)
	res, err := place.Run(context.Background(), place.Request{
		Cfg: testCfg(), Env: "local", Venue: "hyperliquid",
		Kind: exchange.KindPerp, Symbol: "BTCUSD", Side: exchange.SideBuy,
		Size: "0.00015", LedgerPath: filepath.Join(t.TempDir(), "hhld-ledger.db"),
	})
	if err != nil || !strings.HasPrefix(res.TraceID, "local-") {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestLocal_ConfigEnvPin(t *testing.T) {
	clearGates(t)
	cfg := testCfg()
	cfg.Env = "prod"
	_, err := place.Local(context.Background(), place.Request{
		Cfg: cfg, Env: "local", Venue: "hyperliquid",
		Kind: exchange.KindPerp, Symbol: "BTCUSD", Side: exchange.SideBuy, Size: "0.00015",
		LedgerPath: filepath.Join(t.TempDir(), "x.db"),
	})
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("got %v", err)
	}
}

func TestLocal_SpotRequiresMap(t *testing.T) {
	clearGates(t)
	cfg := testCfg()
	spec := cfg.SymbolMap[0].Venues["hyperliquid"]
	spec.SpotSymbolName = ""
	cfg.SymbolMap[0].Venues["hyperliquid"] = spec
	_, err := place.Local(context.Background(), place.Request{
		Cfg: cfg, Env: "local", Venue: "hyperliquid",
		Kind: exchange.KindSpot, Symbol: "BTCUSD", Side: exchange.SideBuy, Size: "0.00015",
		LedgerPath: filepath.Join(t.TempDir(), "x.db"),
	})
	if err == nil || !strings.Contains(err.Error(), "spot_symbol_name") {
		t.Fatalf("got %v", err)
	}
}

func TestCheckLocalLoop(t *testing.T) {
	clearGates(t)
	if err := place.CheckLocalLoop("local"); err != nil {
		t.Fatal(err)
	}
	t.Setenv(secrets.VenueEnvVar, "staging")
	if err := place.CheckLocalLoop("local"); err == nil {
		t.Fatal("want env pin")
	}
	t.Setenv(secrets.VenueEnvVar, "")
	t.Setenv(secrets.KillEnv, "1")
	if err := place.CheckLocalLoop("local"); err == nil || !strings.Contains(err.Error(), "kill") {
		t.Fatalf("kill: %v", err)
	}
	t.Setenv(secrets.KillEnv, "")
	t.Setenv("GRVT_STAGING_API_KEY", "k")
	if err := place.CheckLocalLoop("local"); err == nil {
		t.Fatal("want refuse keys")
	}
}

func TestLocal_SeedsTOBWhenPriceEmpty(t *testing.T) {
	clearGates(t)
	res, err := place.Local(context.Background(), place.Request{
		Cfg: testCfg(), Env: "local", Venue: "hyperliquid",
		Kind: exchange.KindPerp, Symbol: "BTCUSD", Side: exchange.SideBuy,
		Size: "0.00015", LedgerPath: filepath.Join(t.TempDir(), "hhld-ledger.db"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Ack.Status != "filled" || res.TraceID == "" {
		t.Fatalf("%+v", res)
	}
}

func TestLocal_SellSeedsBid(t *testing.T) {
	clearGates(t)
	res, err := place.Local(context.Background(), place.Request{
		Cfg: testCfg(), Env: "local", Venue: "grvt",
		Symbol: "BTCUSD", Side: exchange.SideSell,
		Size: "0.00015", LedgerPath: filepath.Join(t.TempDir(), "hhld-ledger.db"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Ack.Status != "filled" {
		t.Fatalf("%+v", res)
	}
}

func TestLocal_EnvVarPin(t *testing.T) {
	clearGates(t)
	t.Setenv(secrets.VenueEnvVar, "staging")
	_, err := place.Local(context.Background(), place.Request{
		Cfg: testCfg(), Env: "local", Venue: "hyperliquid",
		Kind: exchange.KindPerp, Symbol: "BTCUSD", Side: exchange.SideBuy, Size: "0.00015",
		LedgerPath: filepath.Join(t.TempDir(), "x.db"),
	})
	if err == nil || !strings.Contains(err.Error(), "HHLD_VENUE_ENV") {
		t.Fatalf("got %v", err)
	}
}

func TestLocal_Rejects(t *testing.T) {
	clearGates(t)
	path := filepath.Join(t.TempDir(), "x.db")
	if _, err := place.Local(context.Background(), place.Request{Cfg: testCfg(), Env: "local"}); err == nil {
		t.Fatal("want required fields")
	}
	if _, err := place.Local(context.Background(), place.Request{
		Cfg: testCfg(), Env: "local", Venue: "hyperliquid", Symbol: "BTCUSD",
		Side: "hold", Size: "0.00015", LedgerPath: path,
	}); err == nil {
		t.Fatal("want bad side")
	}
	if _, err := place.Local(context.Background(), place.Request{
		Cfg: testCfg(), Env: "local", Venue: "binance", Symbol: "BTCUSD",
		Side: exchange.SideBuy, Size: "0.00015", LedgerPath: path,
	}); err == nil {
		t.Fatal("want unknown venue map")
	}
	if _, err := place.Local(context.Background(), place.Request{
		Cfg: testCfg(), Env: "local", Venue: "hyperliquid", Symbol: "BTCUSD",
		Side: exchange.SideBuy, Size: "1", LedgerPath: path,
	}); err == nil || !strings.Contains(err.Error(), "risk") {
		t.Fatalf("oversize: %v", err)
	}
	ex := fake.New("grvt", nil)
	if _, err := place.Local(context.Background(), place.Request{
		Cfg: testCfg(), Env: "local", Venue: "hyperliquid", Symbol: "BTCUSD",
		Side: exchange.SideBuy, Size: "0.00015", LedgerPath: path, Exchange: ex,
	}); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("venue id: %v", err)
	}
}
