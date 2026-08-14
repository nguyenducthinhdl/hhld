package sim

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/src/warehouse"
)

// InputFromStore loads market data from a warehouse Store for replay (P7 → P6).
func InputFromStore(ctx context.Context, st warehouse.Store, symbol exchange.Symbol, from, to time.Time) (Input, error) {
	books, err := st.QueryBooks(ctx, symbol, from, to)
	if err != nil {
		return Input{}, err
	}
	ticks, err := st.QueryTicks(ctx, symbol, from, to)
	if err != nil {
		return Input{}, err
	}
	return Input{Books: books, Ticks: ticks}, nil
}

type ndjsonKind struct {
	Type string `json:"type"`
}

type ndjsonBook struct {
	Venue  exchange.VenueID `json:"venue"`
	Symbol exchange.Symbol  `json:"symbol"`
	Kind   exchange.Kind    `json:"kind"`
	Bids   []exchange.Level `json:"bids"`
	Asks   []exchange.Level `json:"asks"`
	Time   time.Time        `json:"time"`
}

type ndjsonTick struct {
	Venue  exchange.VenueID `json:"venue"`
	Symbol exchange.Symbol  `json:"symbol"`
	Kind   exchange.Kind    `json:"kind"`
	Price  string           `json:"price"`
	Size   string           `json:"size"`
	Side   exchange.Side    `json:"side"`
	Time   time.Time        `json:"time"`
}

// InputFromNDJSON loads books/ticks from a crawl or sample NDJSON file.
// Missing or empty files yield an empty Input (no error). Extra crawl fields are ignored.
func InputFromNDJSON(path string, symbol exchange.Symbol) (Input, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Input{}, nil
		}
		return Input{}, fmt.Errorf("sim: open ndjson %q: %w", path, err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 4*1024*1024)

	var in Input
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var kind ndjsonKind
		if err := json.Unmarshal([]byte(line), &kind); err != nil {
			return Input{}, fmt.Errorf("sim: ndjson line %d: %w", lineNo, err)
		}
		switch kind.Type {
		case "book":
			var rec ndjsonBook
			if err := json.Unmarshal([]byte(line), &rec); err != nil {
				return Input{}, fmt.Errorf("sim: ndjson line %d book: %w", lineNo, err)
			}
			if symbol != "" && rec.Symbol != symbol {
				continue
			}
			in.Books = append(in.Books, exchange.Book{
				Venue: rec.Venue, Symbol: rec.Symbol, Kind: rec.Kind,
				Bids: rec.Bids, Asks: rec.Asks, Time: rec.Time.UTC(),
			})
		case "tick":
			var rec ndjsonTick
			if err := json.Unmarshal([]byte(line), &rec); err != nil {
				return Input{}, fmt.Errorf("sim: ndjson line %d tick: %w", lineNo, err)
			}
			if symbol != "" && rec.Symbol != symbol {
				continue
			}
			in.Ticks = append(in.Ticks, exchange.Tick{
				Venue: rec.Venue, Symbol: rec.Symbol, Kind: rec.Kind,
				Price: rec.Price, Size: rec.Size, Side: rec.Side, Time: rec.Time.UTC(),
			})
		default:
			// ignore unknown types for research files
		}
	}
	if err := sc.Err(); err != nil {
		return Input{}, fmt.Errorf("sim: read ndjson: %w", err)
	}
	return in, nil
}

// DistinctVenues returns sorted unique venue IDs from books.
func DistinctVenues(books []exchange.Book) []string {
	seen := map[string]struct{}{}
	for _, b := range books {
		if b.Venue == "" {
			continue
		}
		seen[string(b.Venue)] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for v := range seen {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
