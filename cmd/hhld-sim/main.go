package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/admin"
	"github.com/nguyenducthinhdl/hhld/src/config"
	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/src/sim"
)

func main() {
	ndjson := flag.String("ndjson", "data/crawl/btcusd-live.ndjson", "crawl/sample NDJSON path")
	configPath := flag.String("config", "configs/default.json", "HHLD config JSON")
	venueA := flag.String("venue-a", "", "override venue A")
	venueB := flag.String("venue-b", "", "override venue B")
	addr := flag.String("addr", "127.0.0.1:8080", "HTTP listen address")
	flag.Parse()

	cfg, err := config.LoadJSON(*configPath)
	if err != nil {
		cfg = config.Default()
		log.Printf("config: using Default() (%v)", err)
	}
	if *venueA != "" {
		cfg.Venues.A = exchange.VenueID(*venueA)
	}
	if *venueB != "" {
		cfg.Venues.B = exchange.VenueID(*venueB)
	}

	in, err := sim.InputFromNDJSON(*ndjson, "")
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("loaded %d books, %d ticks from %s", len(in.Books), len(in.Ticks), *ndjson)

	var mu sync.Mutex
	base := cfg
	current := cfg

	run := func(cfg config.Config) sim.Series {
		s, err := sim.Trace(context.Background(), in, cfg)
		if err != nil {
			log.Printf("trace: %v", err)
			return sim.Series{Message: err.Error()}
		}
		return s
	}

	h := admin.Handler{
		Auditor: admin.NewMemory(nil),
		SimGet: func() any {
			mu.Lock()
			defer mu.Unlock()
			return run(current)
		},
		SimRun: func(body []byte) (any, error) {
			var o sim.Overlay
			if len(body) > 0 {
				if err := json.Unmarshal(body, &o); err != nil {
					return nil, err
				}
			}
			mu.Lock()
			defer mu.Unlock()
			current = sim.ApplyOverlay(base, o)
			return run(current), nil
		},
	}

	mux := http.NewServeMux()
	h.Register(mux)
	srv := &http.Server{Addr: *addr, Handler: mux}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		sh, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(sh)
	}()

	log.Printf("hhld-sim http://%s/sim", *addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
