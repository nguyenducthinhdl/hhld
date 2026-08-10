package market_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/src/exchange/fake"
	"github.com/nguyenducthinhdl/hhld/src/exchange/grvt"
	"github.com/nguyenducthinhdl/hhld/src/market"
)

func TestBridgeBooks_PublishesSnapshots(t *testing.T) {
	ex := fake.New("hl", nil)
	bus := market.NewBus(8)
	store := market.NewBookStore()
	bus.Subscribe(func(ev market.BookEvent) {
		_, _ = store.Apply(ev)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = market.BridgeBooks(ctx, ex, "BTCUSD", bus)
	}()

	ex.SetBook(exchange.Book{
		Symbol: "BTCUSD", Kind: exchange.KindPerp,
		Bids: []exchange.Level{{Price: "100", Size: "1"}},
		Asks: []exchange.Level{{Price: "101", Size: "1"}},
	})
	deadline := time.Now().Add(2 * time.Second)
	for {
		if b, ok := store.Get("hl", "BTCUSD"); ok && len(b.Bids) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timeout waiting store")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	wg.Wait()
}

func TestBridgeFakeDeltas_AppliesViaBus(t *testing.T) {
	ex := fake.New("grvt", nil)
	bus := market.NewBus(8)
	store := market.NewBookStore()
	bus.Subscribe(func(ev market.BookEvent) {
		_, _ = store.Apply(ev)
	})
	market.BridgeFakeDeltas(ex, "BTCUSD", exchange.KindPerp, bus)

	_, err := store.Apply(market.SnapshotEvent(exchange.Book{
		Venue: "grvt", Symbol: "BTCUSD", Kind: exchange.KindPerp,
		Bids: []exchange.Level{{Price: "100", Size: "1"}, {Price: "99", Size: "2"}},
		Asks: []exchange.Level{{Price: "101", Size: "1"}},
	}))
	if err != nil {
		t.Fatal(err)
	}

	ex.PushDelta("BTCUSD",
		[]exchange.Level{{Price: "100", Size: "0"}},
		nil,
	)
	deadline := time.Now().Add(2 * time.Second)
	for {
		b, ok := store.Get("grvt", "BTCUSD")
		if ok && len(b.Bids) == 1 && b.Bids[0].Price == "99" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout: %+v ok=%v", b, ok)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

type bridgeFakeWS struct {
	out  chan []byte
	done chan struct{}
}

func (f *bridgeFakeWS) ReadMessage() (int, []byte, error) {
	select {
	case <-f.done:
		return 0, nil, errors.New("closed")
	case m := <-f.out:
		return websocket.TextMessage, m, nil
	}
}

func (f *bridgeFakeWS) WriteMessage(int, []byte) error { return nil }

func (f *bridgeFakeWS) Close() error {
	select {
	case <-f.done:
	default:
		close(f.done)
	}
	return nil
}

func TestBridgeGRVTDeltas_ClearsAndApplies(t *testing.T) {
	snapMsg, _ := json.Marshal(map[string]any{
		"stream": "v1.book.d",
		"feed": map[string]any{
			"event_time": "1700000001000000000",
			"instrument": "BTC_USDT_Perp",
			"bids":       []any{map[string]any{"price": "100", "size": "1"}},
			"asks":       []any{map[string]any{"price": "101", "size": "1"}},
		},
	})
	deltaMsg, _ := json.Marshal(map[string]any{
		"stream": "v1.book.d",
		"feed": map[string]any{
			"event_time":      "1700000002000000000",
			"instrument":      "BTC_USDT_Perp",
			"bids":            []any{map[string]any{"price": "100", "size": "0"}},
			"asks":            []any{},
			"sequence_number": "2",
		},
	})
	ws := &bridgeFakeWS{
		out:  make(chan []byte, 4),
		done: make(chan struct{}),
	}
	ws.out <- []byte(`{"stream":"v1.book.d","subs":["BTC_USDT_Perp@500"]}`)
	ws.out <- snapMsg
	ws.out <- deltaMsg

	ad := grvt.New(grvt.Config{
		Symbols: map[exchange.Symbol]string{"BTCUSD": "BTC_USDT_Perp"},
		Dial: func(ctx context.Context, url string, header http.Header) (exchange.WSConn, error) {
			return ws, nil
		},
	})
	bus := market.NewBus(8)
	store := market.NewBookStore()
	// Stale pre-reconnect state that Clear must wipe before the new snapshot.
	_, _ = store.Apply(market.SnapshotEvent(exchange.Book{
		Venue: "grvt", Symbol: "BTCUSD", Kind: exchange.KindPerp,
		Bids: []exchange.Level{{Price: "50", Size: "9"}},
		Asks: []exchange.Level{{Price: "51", Size: "9"}},
	}))
	bus.Subscribe(func(ev market.BookEvent) {
		_, _ = store.Apply(ev)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = market.BridgeGRVTDeltas(ctx, ad, "BTCUSD", store, bus) }()

	deadline := time.Now().Add(2 * time.Second)
	for {
		b, ok := store.Get("grvt", "BTCUSD")
		if ok && len(b.Bids) == 0 && len(b.Asks) == 1 && b.Asks[0].Price == "101" {
			cancel()
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout: ok=%v book=%+v", ok, b)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
