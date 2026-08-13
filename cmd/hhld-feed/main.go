// Command hhld-feed captures live market data from configured exchanges to NDJSON.
//
// Config JSON (see configs/crawl.json):
//
//	{
//	  "output": "data/crawl/btcusd-live.ndjson",
//	  "config": "configs/default.json",
//	  "duration": "5m",
//	  "feeds": [
//	    {"exchange": "hyperliquid", "symbol": "BTCUSD", "method": "subscribe_book"},
//	    {"exchange": "grvt", "symbol": "BTCUSD", "method": "snapshot_book", "interval": "2s"}
//	  ]
//	}
//
// Methods: snapshot_book (REST poll), subscribe_book (WS), subscribe_ticks (WS).
//
//	go run ./cmd/hhld-feed -config configs/crawl.json
//	go run ./cmd/hhld-feed -config configs/crawl.json -duration 30s -output /tmp/out.ndjson
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/crawl"
)

func main() {
	configPath := flag.String("config", "configs/crawl.json", "crawl config JSON path")
	duration := flag.String("duration", "", "override run duration (e.g. 30s, 5m); empty = config or until Ctrl+C")
	output := flag.String("output", "", "override output NDJSON path")
	flag.Parse()

	cfg, err := crawl.LoadLiveConfig(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	if *output != "" {
		cfg.Output = *output
	}
	if *duration != "" {
		cfg.Duration = *duration
	}
	if err := cfg.Validate(); err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if cfg.Duration != "" {
		d, err := time.ParseDuration(cfg.Duration)
		if err != nil {
			log.Fatalf("duration: %v", err)
		}
		if d > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, d)
			defer cancel()
		}
	}

	log.Printf("hhld-feed writing to %s (%d feeds)", cfg.Output, len(cfg.Feeds))
	if err := (crawl.Live{Cfg: cfg}).Run(ctx); err != nil && err != context.Canceled {
		log.Fatal(err)
	}
	fmt.Printf("wrote %s\n", cfg.Output)
}
