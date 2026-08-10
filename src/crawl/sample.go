package crawl

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/src/warehouse"
)

// SampleFile loads NDJSON book/tick records from path into the warehouse.
// Each line is one JSON object with "type": "book" or "tick".
type SampleFile struct {
	Path string
}

// bookLine is one NDJSON book record in data/samples/*.ndjson.
type bookLine struct {
	Type  string           `json:"type"`
	Venue exchange.VenueID `json:"venue"`
	Symbol exchange.Symbol `json:"symbol"`
	Kind  exchange.Kind    `json:"kind"`
	Bids  []exchange.Level `json:"bids"`
	Asks  []exchange.Level `json:"asks"`
	Time  time.Time        `json:"time"`
}

type tickLine struct {
	Type   string           `json:"type"`
	Venue  exchange.VenueID `json:"venue"`
	Symbol exchange.Symbol  `json:"symbol"`
	Kind   exchange.Kind    `json:"kind"`
	Price  string           `json:"price"`
	Size   string           `json:"size"`
	Side   exchange.Side    `json:"side"`
	Time   time.Time        `json:"time"`
}

func (c SampleFile) Run(ctx context.Context, store warehouse.Store) error {
	if store == nil {
		return fmt.Errorf("crawl: store required")
	}
	if strings.TrimSpace(c.Path) == "" {
		return fmt.Errorf("crawl: sample path required")
	}
	f, err := os.Open(c.Path)
	if err != nil {
		return fmt.Errorf("crawl: open sample %q: %w", c.Path, err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	// Sample files are small; allow up to 1 MiB per line for book depth JSON.
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 1024*1024)

	lineNo := 0
	for sc.Scan() {
		lineNo++
		if err := ctx.Err(); err != nil {
			return err
		}
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var kind struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(line), &kind); err != nil {
			return fmt.Errorf("crawl: line %d: %w", lineNo, err)
		}
		switch kind.Type {
		case "book":
			var rec bookLine
			if err := json.Unmarshal([]byte(line), &rec); err != nil {
				return fmt.Errorf("crawl: line %d book: %w", lineNo, err)
			}
			if err := store.AppendBook(ctx, exchange.Book{
				Venue: rec.Venue, Symbol: rec.Symbol, Kind: rec.Kind,
				Bids: rec.Bids, Asks: rec.Asks, Time: rec.Time.UTC(),
			}); err != nil {
				return err
			}
		case "tick":
			var rec tickLine
			if err := json.Unmarshal([]byte(line), &rec); err != nil {
				return fmt.Errorf("crawl: line %d tick: %w", lineNo, err)
			}
			if err := store.AppendTick(ctx, exchange.Tick{
				Venue: rec.Venue, Symbol: rec.Symbol, Kind: rec.Kind,
				Price: rec.Price, Size: rec.Size, Side: rec.Side, Time: rec.Time.UTC(),
			}); err != nil {
				return err
			}
		default:
			return fmt.Errorf("crawl: line %d: unknown type %q", lineNo, kind.Type)
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("crawl: read sample: %w", err)
	}
	return nil
}
