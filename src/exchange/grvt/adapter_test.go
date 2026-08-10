package grvt_test

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
	"github.com/nguyenducthinhdl/hhld/src/exchange/grvt"
)

func TestAdapter_SnapshotBook(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/full/v1/book" || r.Method != http.MethodPost {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["instrument"] != "BTC_USDT_Perp" {
			t.Fatalf("body: %+v", body)
		}
		_, _ = w.Write([]byte(`{
			"result": {
				"event_time": "1700000000000000000",
				"instrument": "BTC_USDT_Perp",
				"bids": [{"price":"100.0","size":"1","num_orders":1}],
				"asks": [{"price":"100.2","size":"2","num_orders":1}]
			}
		}`))
	}))
	defer srv.Close()

	ad := grvt.New(grvt.Config{
		REST:    srv.URL,
		WS:      "ws://unused",
		Symbols: map[exchange.Symbol]string{"BTCUSD": "BTC_USDT_Perp"},
		HTTP:    srv.Client(),
	})
	book, err := ad.SnapshotBook(context.Background(), "BTCUSD")
	if err != nil {
		t.Fatal(err)
	}
	if book.Venue != "grvt" || book.Bids[0].Price != "100.0" || book.Asks[0].Price != "100.2" {
		t.Fatalf("%+v", book)
	}
	want := time.Unix(0, 1700000000000000000).UTC()
	if !book.Time.Equal(want) {
		t.Fatalf("time got %v want %v", book.Time, want)
	}
}

func TestAdapter_ReadOnlyOrders(t *testing.T) {
	ad := grvt.New(grvt.Config{Symbols: map[exchange.Symbol]string{"BTCUSD": "BTC_USDT_Perp"}})
	if ad.ID() != "grvt" {
		t.Fatal(ad.ID())
	}
	if _, err := ad.PlaceOrder(context.Background(), exchange.OrderRequest{}); !errors.Is(err, exchange.ErrReadOnly) {
		t.Fatalf("place: %v", err)
	}
	if err := ad.CancelOrder(context.Background(), "x"); !errors.Is(err, exchange.ErrReadOnly) {
		t.Fatalf("cancel: %v", err)
	}
}

type fakeWS struct {
	mu   sync.Mutex
	in   chan []byte
	out  chan []byte
	done chan struct{}
}

func newFakeWS(msgs ...[]byte) *fakeWS {
	f := &fakeWS{in: make(chan []byte, 8), out: make(chan []byte, 8), done: make(chan struct{})}
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
		"stream":   "v1.book.s",
		"selector": "BTC_USDT_Perp@500-10",
		"feed": map[string]any{
			"event_time": "1700000001000000000",
			"instrument": "BTC_USDT_Perp",
			"bids":       []any{map[string]any{"price": "99", "size": "1"}},
			"asks":       []any{map[string]any{"price": "101", "size": "1"}},
		},
	})
	tradeMsg, _ := json.Marshal(map[string]any{
		"stream": "v1.trade",
		"feed": map[string]any{
			"event_time": "1700000002000000000",
			"instrument": "BTC_USDT_Perp",
			"price":      "100.5",
			"size":       "0.2",
			"side":       "BUY",
		},
	})

	t.Run("book", func(t *testing.T) {
		ws := newFakeWS(
			[]byte(`{"stream":"v1.book.s","subs":["BTC_USDT_Perp@500-10"],"unsubs":[]}`),
			bookMsg,
		)
		ad := grvt.New(grvt.Config{
			Symbols: map[exchange.Symbol]string{"BTCUSD": "BTC_USDT_Perp"},
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
			if b.Bids[0].Price != "99" {
				t.Fatalf("%+v", b)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timeout")
		}
	})

	t.Run("ticks", func(t *testing.T) {
		ws := newFakeWS(tradeMsg)
		ad := grvt.New(grvt.Config{
			Symbols: map[exchange.Symbol]string{"BTCUSD": "BTC_USDT_Perp"},
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
			t.Fatal("timeout")
		}
	})
}

func TestAdapter_SubscribeBookDeltas(t *testing.T) {
	snapMsg, _ := json.Marshal(map[string]any{
		"stream": "v1.book.d",
		"feed": map[string]any{
			"event_time":      "1700000001000000000",
			"instrument":      "BTC_USDT_Perp",
			"bids":            []any{map[string]any{"price": "100", "size": "1"}},
			"asks":            []any{map[string]any{"price": "101", "size": "1"}},
			"sequence_number": "0",
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
	ws := newFakeWS(
		[]byte(`{"stream":"v1.book.d","subs":["BTC_USDT_Perp@500"],"unsubs":[]}`),
		snapMsg,
		deltaMsg,
	)
	ad := grvt.New(grvt.Config{
		Symbols: map[exchange.Symbol]string{"BTCUSD": "BTC_USDT_Perp"},
		Dial: func(ctx context.Context, url string, header http.Header) (exchange.WSConn, error) {
			return ws, nil
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reconnectN := 0
	snaps := make(chan exchange.Book, 2)
	deltas := make(chan struct {
		bids []exchange.Level
		seq  uint64
	}, 2)

	go func() {
		_ = ad.SubscribeBookDeltas(ctx, "BTCUSD",
			func(symbol exchange.Symbol) { reconnectN++ },
			func(b exchange.Book) { snaps <- b },
			func(venue exchange.VenueID, symbol exchange.Symbol, kind exchange.Kind, bids, asks []exchange.Level, tm time.Time, seq uint64) {
				deltas <- struct {
					bids []exchange.Level
					seq  uint64
				}{bids, seq}
				cancel()
			},
		)
	}()

	select {
	case b := <-snaps:
		if b.Bids[0].Price != "100" || b.Bids[0].Size != "1" {
			t.Fatalf("snap: %+v", b)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting snapshot")
	}
	select {
	case d := <-deltas:
		if d.seq != 2 || len(d.bids) != 1 || d.bids[0].Size != "0" {
			t.Fatalf("delta: %+v", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting delta")
	}
	if reconnectN < 1 {
		t.Fatalf("reconnect called %d", reconnectN)
	}
}
