package hyperliquid_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/src/exchange/hyperliquid"
)

func TestAdapter_SnapshotBook(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/info" || r.Method != http.MethodPost {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["type"] != "l2Book" || body["coin"] != "BTC" {
			t.Fatalf("body: %+v", body)
		}
		_, _ = w.Write([]byte(`{
			"coin":"BTC","time":1700000000123,
			"levels":[
				[{"px":"100.0","sz":"1.5","n":1}],
				[{"px":"100.1","sz":"2.0","n":1}]
			]
		}`))
	}))
	defer srv.Close()

	ad := hyperliquid.New(hyperliquid.Config{
		REST:    srv.URL,
		WS:      "ws://unused",
		Symbols: map[exchange.Symbol]string{"BTCUSD": "BTC"},
		HTTP:    srv.Client(),
	})
	book, err := ad.SnapshotBook(context.Background(), "BTCUSD")
	if err != nil {
		t.Fatal(err)
	}
	if book.Venue != "hyperliquid" || book.Symbol != "BTCUSD" {
		t.Fatalf("%+v", book)
	}
	if len(book.Bids) != 1 || book.Bids[0].Price != "100.0" || book.Asks[0].Price != "100.1" {
		t.Fatalf("levels: %+v", book)
	}
	if !book.Time.Equal(time.UnixMilli(1700000000123).UTC()) {
		t.Fatalf("time: %v", book.Time)
	}
}

func TestAdapter_ReadOnlyOrders(t *testing.T) {
	ad := hyperliquid.New(hyperliquid.Config{Symbols: map[exchange.Symbol]string{"BTCUSD": "BTC"}})
	if ad.ID() != "hyperliquid" {
		t.Fatal(ad.ID())
	}
	if _, err := ad.PlaceOrder(context.Background(), exchange.OrderRequest{}); !errors.Is(err, exchange.ErrReadOnly) {
		t.Fatalf("place: %v", err)
	}
	if _, err := ad.GetOrder(context.Background(), "x"); !errors.Is(err, exchange.ErrReadOnly) {
		t.Fatalf("get: %v", err)
	}
	if _, err := ad.SnapshotBook(context.Background(), "NOPE"); err == nil {
		t.Fatal("want missing symbol")
	}
}

type fakeWS struct {
	mu   sync.Mutex
	in   chan []byte
	out  chan []byte
	done chan struct{}
}

func newFakeWS(msgs ...[]byte) *fakeWS {
	f := &fakeWS{
		in:   make(chan []byte, 8),
		out:  make(chan []byte, 8),
		done: make(chan struct{}),
	}
	for _, m := range msgs {
		f.out <- m
	}
	return f
}

func (f *fakeWS) ReadMessage() (int, []byte, error) {
	select {
	case <-f.done:
		return 0, nil, errors.New("closed")
	case m := <-f.out:
		return websocket.TextMessage, m, nil
	}
}

func (f *fakeWS) WriteMessage(_ int, data []byte) error {
	select {
	case f.in <- append([]byte(nil), data...):
		return nil
	case <-f.done:
		return errors.New("closed")
	}
}

func (f *fakeWS) Close() error {
	select {
	case <-f.done:
	default:
		close(f.done)
	}
	return nil
}

func TestAdapter_SubscribeBookAndTicks(t *testing.T) {
	bookMsg, _ := json.Marshal(map[string]any{
		"channel": "l2Book",
		"data": map[string]any{
			"coin": "BTC", "time": 1700000001000,
			"levels": []any{
				[]any{map[string]any{"px": "99", "sz": "1"}},
				[]any{map[string]any{"px": "101", "sz": "1"}},
			},
		},
	})
	tradeMsg, _ := json.Marshal(map[string]any{
		"channel": "trades",
		"data": []any{
			map[string]any{"coin": "BTC", "side": "B", "px": "100.5", "sz": "0.1", "time": 1700000002000},
		},
	})

	t.Run("book", func(t *testing.T) {
		ws := newFakeWS(bookMsg)
		ad := hyperliquid.New(hyperliquid.Config{
			Symbols:       map[exchange.Symbol]string{"BTCUSD": "BTC"},
			ReconnectWait: 50 * time.Millisecond,
			Dial: func(ctx context.Context, url string, header http.Header) (exchange.WSConn, error) {
				return ws, nil
			},
		})
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		got := make(chan exchange.Book, 1)
		go func() {
			_ = ad.SubscribeBook(ctx, "BTCUSD", func(b exchange.Book) {
				got <- b
				cancel()
			})
		}()
		select {
		case b := <-got:
			if b.Bids[0].Price != "99" || b.Asks[0].Price != "101" {
				t.Fatalf("%+v", b)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for book")
		}
		select {
		case sub := <-ws.in:
			var m map[string]any
			_ = json.Unmarshal(sub, &m)
			if m["method"] != "subscribe" {
				t.Fatalf("sub: %s", sub)
			}
		case <-time.After(time.Second):
			t.Fatal("no subscribe write")
		}
	})

	t.Run("ticks", func(t *testing.T) {
		ws := newFakeWS(tradeMsg)
		ad := hyperliquid.New(hyperliquid.Config{
			Symbols: map[exchange.Symbol]string{"BTCUSD": "BTC"},
			Dial: func(ctx context.Context, url string, header http.Header) (exchange.WSConn, error) {
				return ws, nil
			},
		})
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		got := make(chan exchange.Tick, 1)
		go func() {
			_ = ad.SubscribeTicks(ctx, "BTCUSD", func(tk exchange.Tick) {
				got <- tk
				cancel()
			})
		}()
		select {
		case tk := <-got:
			if tk.Price != "100.5" || tk.Side != exchange.SideBuy {
				t.Fatalf("%+v", tk)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for tick")
		}
	})
}
