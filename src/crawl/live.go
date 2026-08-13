package crawl

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/src/exchange/fake"
	"github.com/nguyenducthinhdl/hhld/src/exchange/grvt"
	"github.com/nguyenducthinhdl/hhld/src/exchange/hyperliquid"
)

const defaultSnapshotInterval = 2 * time.Second

// Live captures market data from configured exchanges into NDJSON.
type Live struct {
	Cfg LiveConfig
	// Exchanges optional inject for tests (key = feed exchange name).
	Exchanges map[string]exchange.Exchange
}

// Run captures feeds until ctx is canceled.
func (l Live) Run(ctx context.Context) error {
	if err := l.Cfg.Validate(); err != nil {
		return err
	}
	w, err := OpenNDJSON(l.Cfg.Output)
	if err != nil {
		return err
	}
	defer w.Close()

	var wg sync.WaitGroup
	errCh := make(chan error, len(l.Cfg.Feeds))
	for _, feed := range l.Cfg.Feeds {
		feed := feed
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := l.runFeed(ctx, feed, w); err != nil && ctx.Err() == nil {
				errCh <- err
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		return err
	}
	return ctx.Err()
}

func (l Live) runFeed(ctx context.Context, feed FeedConfig, w *NDJSONWriter) error {
	ex, err := l.exchange(feed.Exchange)
	if err != nil {
		return err
	}
	sym := exchange.Symbol(feed.Symbol)
	native, _ := l.Cfg.NativeSymbol(ex.ID(), sym)

	switch feed.method() {
	case MethodSnapshotBook:
		return l.runSnapshotBook(ctx, feed, ex, sym, native, w)
	case MethodSubscribeBook:
		return l.runSubscribeBook(ctx, feed, ex, sym, native, w)
	case MethodSubscribeTicks:
		return l.runSubscribeTicks(ctx, feed, ex, sym, native, w)
	default:
		return fmt.Errorf("crawl: unknown method %q", feed.Method)
	}
}

func (l Live) runSnapshotBook(ctx context.Context, feed FeedConfig, ex exchange.Exchange, sym exchange.Symbol, native string, w *NDJSONWriter) error {
	interval := feed.interval(defaultSnapshotInterval)
	t := time.NewTicker(interval)
	defer t.Stop()
	method := feed.method()
	log.Printf("crawl: %s %s %s poll every %s", feed.Exchange, sym, method, interval)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			start := time.Now().UTC()
			b, err := ex.SnapshotBook(ctx, sym)
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				log.Printf("crawl: %s %s snapshot: %v", feed.Exchange, sym, err)
				continue
			}
			recvMs := time.Since(start).Milliseconds()
			if err := w.WriteLine(bookRecord(method, native, time.Now().UTC(), recvMs, b)); err != nil {
				return err
			}
		}
	}
}

func (l Live) runSubscribeBook(ctx context.Context, feed FeedConfig, ex exchange.Exchange, sym exchange.Symbol, native string, w *NDJSONWriter) error {
	method := feed.method()
	log.Printf("crawl: %s %s %s stream", feed.Exchange, sym, method)
	return ex.SubscribeBook(ctx, sym, func(b exchange.Book) {
		crawled := time.Now().UTC()
		recvMs := int64(0)
		if !b.Time.IsZero() {
			recvMs = crawled.Sub(b.Time).Milliseconds()
			if recvMs < 0 {
				recvMs = 0
			}
		}
		if err := w.WriteLine(bookRecord(method, native, crawled, recvMs, b)); err != nil {
			log.Printf("crawl: write book: %v", err)
		}
	})
}

func (l Live) runSubscribeTicks(ctx context.Context, feed FeedConfig, ex exchange.Exchange, sym exchange.Symbol, native string, w *NDJSONWriter) error {
	method := feed.method()
	log.Printf("crawl: %s %s %s stream", feed.Exchange, sym, method)
	return ex.SubscribeTicks(ctx, sym, func(t exchange.Tick) {
		crawled := time.Now().UTC()
		recvMs := int64(0)
		if !t.Time.IsZero() {
			recvMs = crawled.Sub(t.Time).Milliseconds()
			if recvMs < 0 {
				recvMs = 0
			}
		}
		if err := w.WriteLine(tickRecord(method, native, crawled, recvMs, t)); err != nil {
			log.Printf("crawl: write tick: %v", err)
		}
	})
}

func (l Live) exchange(name string) (exchange.Exchange, error) {
	if l.Exchanges != nil {
		if ex, ok := l.Exchanges[name]; ok {
			return ex, nil
		}
	}
	venue := exchange.VenueID(name)
	symbols, err := l.symbolMapForVenue(venue)
	if err != nil {
		return nil, err
	}
	kind := l.Cfg.kind()
	switch venue {
	case "hyperliquid":
		return hyperliquid.New(hyperliquid.Config{Symbols: symbols, Kind: kind}), nil
	case "grvt":
		return grvt.New(grvt.Config{Symbols: symbols, Kind: kind}), nil
	case "fake":
		return fake.New(venue, nil), nil
	default:
		return nil, fmt.Errorf("crawl: unsupported exchange %q (hyperliquid, grvt, fake)", name)
	}
}

func (l Live) symbolMapForVenue(venue exchange.VenueID) (map[exchange.Symbol]string, error) {
	out := make(map[exchange.Symbol]string)
	for _, feed := range l.Cfg.Feeds {
		if exchange.VenueID(feed.Exchange) != venue {
			continue
		}
		sym := exchange.Symbol(feed.Symbol)
		if _, ok := out[sym]; ok {
			continue
		}
		native, err := l.Cfg.NativeSymbol(venue, sym)
		if err != nil {
			return nil, err
		}
		out[sym] = native
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("crawl: no symbols for exchange %s", venue)
	}
	return out, nil
}
