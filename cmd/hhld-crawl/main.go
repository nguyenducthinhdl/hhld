// Command hhld-crawl loads sample or fake market data into the local SQLite warehouse.
//
//	# from bundled sample (P7)
//	go run ./cmd/hhld-crawl -sample data/samples/btcusd_books.ndjson -db ./hhld.db
//
//	# scripted fake dual feed (no file)
//	go run ./cmd/hhld-crawl -fake -db ./hhld.db
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/nguyenducthinhdl/hhld/src/crawl"
	"github.com/nguyenducthinhdl/hhld/src/warehouse"
)

func main() {
	sample := flag.String("sample", "", "NDJSON sample file path (books/ticks)")
	fake := flag.Bool("fake", false, "use scripted fake dual-venue books")
	db := flag.String("db", "hhld.db", "SQLite warehouse path")
	flag.Parse()

	if *sample == "" && !*fake {
		flag.Usage()
		os.Exit(2)
	}
	if *sample != "" && *fake {
		log.Fatal("choose one of -sample or -fake")
	}

	if err := os.MkdirAll(filepath.Dir(*db), 0o755); err != nil && filepath.Dir(*db) != "." {
		log.Fatalf("mkdir: %v", err)
	}

	st, err := warehouse.OpenSQLite(*db)
	if err != nil {
		log.Fatalf("open warehouse: %v", err)
	}
	defer st.Close()

	ctx := context.Background()
	var runner crawl.Crawler
	switch {
	case *fake:
		runner = crawl.FakeDual{}
	default:
		runner = crawl.SampleFile{Path: *sample}
	}
	if err := runner.Run(ctx, st); err != nil {
		log.Fatalf("crawl: %v", err)
	}

	fmt.Printf("crawled into %s\n", *db)
}
