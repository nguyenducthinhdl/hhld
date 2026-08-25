package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/nguyenducthinhdl/hhld/src/config"
	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/src/place"
	"github.com/nguyenducthinhdl/hhld/src/secrets"
)

func main() {
	configPath := flag.String("config", "configs/default.json", "path to config JSON")
	venue := flag.String("venue", "hyperliquid", "venue id (hyperliquid|grvt)")
	env := flag.String("env", secrets.EnvLocal, "env ladder: local | testnet | staging (prod/mainnet not in this slice)")
	kind := flag.String("kind", "perp", "perp or spot")
	symbol := flag.String("symbol", "BTCUSD", "HHLD symbol")
	side := flag.String("side", "buy", "buy or sell")
	size := flag.String("size", "", "base size (required)")
	price := flag.String("price", "", "optional limit; empty fills IOC at TOB")
	ledgerPath := flag.String("ledger", place.DefaultLedgerPath, "sqlite ledger path")
	flag.Parse()

	if *size == "" {
		fmt.Fprintf(os.Stderr, "usage: hhld-place -config configs/default.json -venue hyperliquid -env local -kind perp -symbol BTCUSD -side buy -size 0.00003\n")
		fmt.Fprintf(os.Stderr, "       hhld-place -config configs/default-staging.json -venue hyperliquid -env testnet -kind perp -symbol BTCUSD -side buy -size 0.00003\n")
		os.Exit(2)
	}

	cfg, err := config.LoadJSON(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}

	res, err := place.Run(context.Background(), place.Request{
		Cfg:        cfg,
		Env:        *env,
		Venue:      exchange.VenueID(*venue),
		Kind:       exchange.Kind(*kind),
		Symbol:     exchange.Symbol(*symbol),
		Side:       exchange.Side(*side),
		Size:       *size,
		Price:      *price,
		LedgerPath: *ledgerPath,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "hhld-place: %v\n", err)
		os.Exit(1)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(res); err != nil {
		fmt.Fprintf(os.Stderr, "encode: %v\n", err)
		os.Exit(1)
	}
}
