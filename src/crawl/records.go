package crawl

import (
	"time"

	"github.com/nguyenducthinhdl/hhld/src/exchange"
)

type levelLine struct {
	Price string `json:"price"`
	Size  string `json:"size"`
}

func levels(in []exchange.Level) []levelLine {
	if len(in) == 0 {
		return nil
	}
	out := make([]levelLine, len(in))
	for i, lv := range in {
		out[i] = levelLine{Price: lv.Price, Size: lv.Size}
	}
	return out
}

// BookRecord is one NDJSON book line for research export.
type BookRecord struct {
	Type              string           `json:"type"`
	CrawledAt         time.Time        `json:"crawled_at"`
	ReceiveLatencyMs  int64            `json:"receive_latency_ms"`
	ExchangeAgeMs     int64            `json:"exchange_age_ms"`
	Venue             exchange.VenueID `json:"venue"`
	Symbol            exchange.Symbol  `json:"symbol"`
	NativeSymbol      string           `json:"native_symbol,omitempty"`
	Kind              exchange.Kind    `json:"kind"`
	Method            string           `json:"method"`
	Bids              []levelLine      `json:"bids"`
	Asks              []levelLine      `json:"asks"`
	Time              time.Time        `json:"time"`
}

// TickRecord is one NDJSON tick line for research export.
type TickRecord struct {
	Type             string           `json:"type"`
	CrawledAt        time.Time        `json:"crawled_at"`
	ReceiveLatencyMs int64            `json:"receive_latency_ms"`
	ExchangeAgeMs    int64            `json:"exchange_age_ms"`
	Venue            exchange.VenueID `json:"venue"`
	Symbol           exchange.Symbol  `json:"symbol"`
	NativeSymbol     string           `json:"native_symbol,omitempty"`
	Kind             exchange.Kind    `json:"kind"`
	Method           string           `json:"method"`
	Price            string           `json:"price"`
	Size             string           `json:"size"`
	Side             exchange.Side    `json:"side,omitempty"`
	Time             time.Time        `json:"time"`
}

func bookRecord(method, native string, crawled time.Time, recvMs int64, b exchange.Book) BookRecord {
	exAge := int64(0)
	if !b.Time.IsZero() {
		age := crawled.Sub(b.Time)
		if age >= 0 && age < 24*time.Hour {
			exAge = age.Milliseconds()
		}
	}
	return BookRecord{
		Type:             "book",
		CrawledAt:        crawled.UTC(),
		ReceiveLatencyMs: recvMs,
		ExchangeAgeMs:    exAge,
		Venue:            b.Venue,
		Symbol:           b.Symbol,
		NativeSymbol:     native,
		Kind:             b.Kind,
		Method:           method,
		Bids:             levels(b.Bids),
		Asks:             levels(b.Asks),
		Time:             b.Time.UTC(),
	}
}

func tickRecord(method, native string, crawled time.Time, recvMs int64, t exchange.Tick) TickRecord {
	exAge := int64(0)
	if !t.Time.IsZero() {
		age := crawled.Sub(t.Time)
		if age >= 0 && age < 24*time.Hour {
			exAge = age.Milliseconds()
		}
	}
	return TickRecord{
		Type:             "tick",
		CrawledAt:        crawled.UTC(),
		ReceiveLatencyMs: recvMs,
		ExchangeAgeMs:    exAge,
		Venue:            t.Venue,
		Symbol:           t.Symbol,
		NativeSymbol:     native,
		Kind:             t.Kind,
		Method:           method,
		Price:            t.Price,
		Size:             t.Size,
		Side:             t.Side,
		Time:             t.Time.UTC(),
	}
}
