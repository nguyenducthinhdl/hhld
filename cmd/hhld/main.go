package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/admin"
	"github.com/nguyenducthinhdl/hhld/src/config"
	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/src/exchange/fake"
	"github.com/nguyenducthinhdl/hhld/src/ledger"
	"github.com/nguyenducthinhdl/hhld/src/market"
	"github.com/nguyenducthinhdl/hhld/src/paperlive"
	"github.com/nguyenducthinhdl/hhld/src/place"
	"github.com/nguyenducthinhdl/hhld/src/pnl"
	"github.com/nguyenducthinhdl/hhld/src/risk"
	"github.com/nguyenducthinhdl/hhld/src/secrets"
	"github.com/nguyenducthinhdl/hhld/src/strategy"
	"github.com/nguyenducthinhdl/hhld/src/viz"
)

func main() {
	configPath := flag.String("config", "configs/default.json", "path to config JSON")
	addr := flag.String("addr", "127.0.0.1:8080", "HTTP listen address")
	envFlag := flag.String("env", secrets.EnvLocal, "env ladder (must match config.env when set)")
	demo := flag.Bool("demo", false, "seed one paper arb and serve PnL/orders")
	demoMarket := flag.Bool("demo-market", false, "fake dual books + market dashboard (/trading/market)")
	paperLive := flag.Bool("paper-live", false, "live HL+GRVT books + paper place (PnL/orders/market)")
	liveMarket := flag.Bool("live-market", false, "alias of -paper-live (P9.5 dashboard name)")
	tradeLocal := flag.Bool("trade-local", false, "P11 local two-leg: fake place + ledger (fake books, or -live-books)")
	liveBooks := flag.Bool("live-books", false, "with -trade-local, stream public books (env-pinned REST/WS)")
	ledgerPath := flag.String("ledger", place.DefaultLedgerPath, "sqlite ledger path for -trade-local / -paper-live")
	flag.Parse()

	livePaper := *paperLive || *liveMarket
	if *liveBooks && !*tradeLocal {
		fmt.Fprintf(os.Stderr, "-live-books requires -trade-local\n")
		os.Exit(2)
	}
	if !*demo && !*demoMarket && !livePaper && !*tradeLocal {
		fmt.Fprintf(os.Stderr, "usage: hhld -demo | -demo-market | -paper-live | -trade-local [-live-books] [-env local] [-config path] [-addr host:port] [-ledger hhld-ledger.db]\n")
		os.Exit(2)
	}

	cfg, err := config.LoadJSON(*configPath)
	if err != nil {
		cfg = config.Default()
		log.Printf("config: using Default() (%v)", err)
	}
	if err := cfg.PinEnv(*envFlag); err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var tracker pnl.Tracker = pnl.NewMemory()
	var auditor admin.Auditor = admin.NewMemory(tracker)
	if *tradeLocal || livePaper {
		if err := place.CheckLocalLoop(*envFlag); err != nil {
			log.Fatal(err)
		}
		st, err := ledger.Open(*ledgerPath, secrets.NormalizeEnv(*envFlag))
		if err != nil {
			log.Fatal(err)
		}
		defer st.Close()
		tracker = st
		auditor = st
		log.Printf("ledger: %s env=%s", *ledgerPath, st.Env())
	}

	h := admin.Handler{Auditor: auditor}

	if *demo {
		seedDemoArb(ctx, auditor, tracker, cfg)
	}

	var cancelFeed context.CancelFunc
	var gate *risk.Gate
	if *demoMarket || livePaper || *tradeLocal {
		var feedCtx context.Context
		feedCtx, cancelFeed = context.WithCancel(ctx)
		var src viz.Source
		switch {
		case livePaper || (*tradeLocal && *liveBooks):
			var sess *paperlive.Session
			sess, err = paperlive.Start(feedCtx, paperlive.Options{
				Cfg:     cfg,
				Auditor: auditor,
				Tracker: tracker,
			})
			if err == nil {
				src = sess.Source
				gate = sess.Gate
			}
		default:
			src, gate, err = startDemoMarket(feedCtx, cfg, auditor, tracker)
		}
		if err != nil {
			log.Fatal(err)
		}
		h.Market = func(sym exchange.Symbol) any {
			return src.BuildSnapshot(sym)
		}
	}
	if gate != nil {
		h.Halted = gate.Halted
		h.Resume = gate.Resume
	}
	if cancelFeed != nil {
		defer cancelFeed()
	}

	mux := http.NewServeMux()
	h.Register(mux)
	srv := &http.Server{Addr: *addr, Handler: mux}

	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()

	log.Printf("hhld listening on http://%s", *addr)
	log.Printf("health: http://%s/health", *addr)
	if *demo || livePaper || *tradeLocal {
		log.Printf("pnl: http://%s/trading/pnl", *addr)
		log.Printf("orders: http://%s/trading/orders", *addr)
		log.Printf("forensics: http://%s/trading/forensics?trace_id=", *addr)
	}
	if *demoMarket || livePaper || *tradeLocal {
		log.Printf("market dashboard: http://%s/trading/market", *addr)
		log.Printf("halts: http://%s/trading/halts", *addr)
	}
	if livePaper {
		log.Printf("feeds: live Hyperliquid + GRVT (paper place via fake)")
	}
	if *tradeLocal && *liveBooks {
		log.Printf("feeds: env-pinned public books + fake place (ledger)")
	}
	if *tradeLocal && !*liveBooks {
		log.Printf("feeds: fake dual books + fake place (ledger)")
	}
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func seedDemoArb(ctx context.Context, aud admin.Auditor, tr pnl.Tracker, cfg config.Config) {
	d := strategy.Decision{
		TraceID: "demo-arb-1",
		Reason:  "demo seed",
		Legs: []strategy.Leg{
			{Venue: cfg.Venues.A, Symbol: "BTCUSD", Kind: exchange.KindPerp, Side: exchange.SideBuy, Price: "100.1", Size: "1"},
			{Venue: cfg.Venues.B, Symbol: "BTCUSD", Kind: exchange.KindPerp, Side: exchange.SideSell, Price: "100.8", Size: "1"},
		},
	}
	results := []strategy.LegResult{
		{Index: 0, Leg: d.Legs[0], Ack: exchange.OrderAck{OrderID: "demo-a", ClientOrderID: "demo-arb-1-0", Status: "accepted", Time: time.Now().UTC()}},
		{Index: 1, Leg: d.Legs[1], Ack: exchange.OrderAck{OrderID: "demo-b", ClientOrderID: "demo-arb-1-1", Status: "accepted", Time: time.Now().UTC()}},
	}
	fees := risk.ParamsFromConfig(cfg).FeeSchedule()
	if err := admin.RecordPaperDecision(ctx, aud, tr, d, results, fees); err != nil {
		log.Printf("demo seed: %v", err)
	}
}

func startDemoMarket(ctx context.Context, cfg config.Config, aud admin.Auditor, tr pnl.Tracker) (viz.Source, *risk.Gate, error) {
	store := market.NewBookStore()
	bus := market.NewBus(256)
	sigs := viz.NewSignalLog()
	ticks := viz.NewTickRing(48)

	dual := fake.NewDual(cfg.Venues.A, cfg.Venues.B, time.Now().UTC())
	src, gate, err := paperlive.Wire(cfg, store, bus, sigs, ticks, dual.A, dual.B, aud, tr)
	if err != nil {
		return viz.Source{}, nil, err
	}

	sym := primarySymbol(cfg)
	kind := cfg.KindFor(sym)

	publish := func(bidA, askA, bidB, askB string) {
		ts := time.Now().UTC()
		bus.Publish(market.SnapshotEvent(exchange.Book{
			Venue: cfg.Venues.A, Symbol: sym, Kind: kind, Time: ts,
			Bids: []exchange.Level{{Price: bidA, Size: "2"}},
			Asks: []exchange.Level{{Price: askA, Size: "2"}},
		}))
		bus.Publish(market.SnapshotEvent(exchange.Book{
			Venue: cfg.Venues.B, Symbol: sym, Kind: kind, Time: ts,
			Bids: []exchange.Level{{Price: bidB, Size: "2"}},
			Asks: []exchange.Level{{Price: askB, Size: "2"}},
		}))
		ticks.Push(exchange.Tick{
			Venue: cfg.Venues.A, Symbol: sym, Kind: kind,
			Price: askA, Size: "0.1", Side: exchange.SideBuy, Time: ts,
		})
	}
	publish("100.0", "100.1", "100.8", "100.9")

	go func() {
		n := 0
		t := time.NewTicker(1500 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				bus.Close()
				return
			case <-t.C:
				n++
				if n%2 == 1 {
					publish("100.0", "100.1", "100.8", "100.9")
				} else {
					publish("100.4", "100.5", "100.55", "100.6")
				}
			}
		}
	}()

	return src, gate, nil
}

func primarySymbol(cfg config.Config) exchange.Symbol {
	if syms := cfg.Symbols(); len(syms) > 0 {
		return syms[0]
	}
	return "BTCUSD"
}
