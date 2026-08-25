// Package hyperliquid is a Hyperliquid Exchange adapter.
// Books/ticks via REST POST /info and WS l2Book/trades.
// Default New is read-only (ErrReadOnly on orders). NewLive enables signed writes.
package hyperliquid

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/nguyenducthinhdl/hhld/src/exchange"
)

const venueID = exchange.VenueID("hyperliquid")

// Ensure Adapter implements Exchange.
var _ exchange.Exchange = (*Adapter)(nil)

// Config wires endpoints and HHLD→coin symbol map.
type Config struct {
	REST    string
	WS      string
	Symbols map[exchange.Symbol]string // HHLD → HL coin (e.g. BTCUSD → BTC)
	Kind    exchange.Kind
	HTTP    *http.Client
	Dial    exchange.DialWS
	// ReconnectWait is backoff between WS reconnects (default 1s).
	ReconnectWait time.Duration
}

// Adapter is a Hyperliquid client (market data; optionally signed writes via NewLive).
type Adapter struct {
	cfg       Config
	auth      *Auth
	pk        *ecdsa.PrivateKey
	meta      metaCache
	lastNonce atomic.Int64
}

// New builds a read-only adapter. Defaults mainnet URLs and KindPerp when unset.
func New(cfg Config) *Adapter {
	return newAdapter(cfg, nil)
}

// NewLive builds a write-capable adapter. Callers must enforce HHLD_LIVE_ORDERS
// before constructing (place path); this constructor only loads the key.
func NewLive(cfg Config, auth Auth) (*Adapter, error) {
	if strings.TrimSpace(auth.AccountAddress) == "" || strings.TrimSpace(auth.PrivateKeyHex) == "" {
		return nil, fmt.Errorf("hyperliquid: Auth.AccountAddress and PrivateKeyHex required")
	}
	pk, err := parsePrivateKey(auth.PrivateKeyHex)
	if err != nil {
		return nil, err
	}
	a := newAdapter(cfg, &auth)
	a.pk = pk
	return a, nil
}

func newAdapter(cfg Config, auth *Auth) *Adapter {
	if cfg.REST == "" || cfg.WS == "" {
		ep := exchange.DefaultHyperliquidMainnet()
		if auth != nil && auth.Testnet {
			ep = exchange.DefaultHyperliquidTestnet()
		}
		if cfg.REST == "" {
			cfg.REST = ep.REST
		}
		if cfg.WS == "" {
			cfg.WS = ep.WS
		}
	}
	if cfg.Kind == "" {
		cfg.Kind = exchange.KindPerp
	}
	if cfg.HTTP == nil {
		cfg.HTTP = exchange.DefaultHTTPClient()
	}
	if cfg.Dial == nil {
		cfg.Dial = defaultDial
	}
	if cfg.ReconnectWait <= 0 {
		cfg.ReconnectWait = time.Second
	}
	if cfg.Symbols == nil {
		cfg.Symbols = map[exchange.Symbol]string{}
	}
	return &Adapter{cfg: cfg, auth: auth}
}

func defaultDial(ctx context.Context, url string, header http.Header) (exchange.WSConn, error) {
	d := websocket.Dialer{}
	conn, _, err := d.DialContext(ctx, url, header)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

func (a *Adapter) ID() exchange.VenueID { return venueID }

// Endpoints returns the REST/WS hosts this adapter was constructed with.
func (a *Adapter) Endpoints() exchange.AdapterEndpoints {
	return exchange.AdapterEndpoints{REST: a.cfg.REST, WS: a.cfg.WS}
}

func (a *Adapter) coin(symbol exchange.Symbol) (string, error) {
	c, ok := a.cfg.Symbols[symbol]
	if !ok || c == "" {
		return "", fmt.Errorf("hyperliquid: no symbol map for %s", symbol)
	}
	return c, nil
}

func (a *Adapter) SnapshotBook(ctx context.Context, symbol exchange.Symbol) (exchange.Book, error) {
	coin, err := a.coin(symbol)
	if err != nil {
		return exchange.Book{}, err
	}
	body, _ := json.Marshal(map[string]any{"type": "l2Book", "coin": coin})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.cfg.REST+"/info", bytes.NewReader(body))
	if err != nil {
		return exchange.Book{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := a.cfg.HTTP.Do(req)
	if err != nil {
		return exchange.Book{}, fmt.Errorf("hyperliquid: snapshot: %w", err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return exchange.Book{}, err
	}
	if res.StatusCode != http.StatusOK {
		return exchange.Book{}, fmt.Errorf("hyperliquid: snapshot status %d: %s", res.StatusCode, raw)
	}
	return parseL2Book(raw, symbol, a.cfg.Kind)
}

// SubscribeBook streams l2Book until ctx is canceled. Reconnects on drop and treats
// the next book as a fresh snapshot (spec/roadmap.md P8 reconnect).
// Note: Hyperliquid l2Book pushes are full snapshots each message — publish them as
// market.SnapshotEvent (not deltas) via market.BridgeBooks.
func (a *Adapter) SubscribeBook(ctx context.Context, symbol exchange.Symbol, h exchange.BookHandler) error {
	coin, err := a.coin(symbol)
	if err != nil {
		return err
	}
	sub, _ := json.Marshal(map[string]any{
		"method": "subscribe",
		"subscription": map[string]any{
			"type": "l2Book",
			"coin": coin,
		},
	})
	return a.runWS(ctx, sub, func(channel string, data json.RawMessage) error {
		if channel != "l2Book" {
			return nil
		}
		book, err := parseL2Book(data, symbol, a.cfg.Kind)
		if err != nil {
			return err
		}
		h(book)
		return nil
	})
}

// SubscribeTicks streams trades until ctx is canceled.
func (a *Adapter) SubscribeTicks(ctx context.Context, symbol exchange.Symbol, h exchange.TickHandler) error {
	coin, err := a.coin(symbol)
	if err != nil {
		return err
	}
	sub, _ := json.Marshal(map[string]any{
		"method": "subscribe",
		"subscription": map[string]any{
			"type": "trades",
			"coin": coin,
		},
	})
	return a.runWS(ctx, sub, func(channel string, data json.RawMessage) error {
		if channel != "trades" {
			return nil
		}
		ticks, err := parseTrades(data, symbol, a.cfg.Kind)
		if err != nil {
			return err
		}
		for _, tk := range ticks {
			h(tk)
		}
		return nil
	})
}

type wsEnvelope struct {
	Channel string          `json:"channel"`
	Data    json.RawMessage `json:"data"`
}

func (a *Adapter) runWS(ctx context.Context, subscribeMsg []byte, onMsg func(channel string, data json.RawMessage) error) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := a.wsSession(ctx, subscribeMsg, onMsg)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(a.cfg.ReconnectWait):
			}
			continue
		}
	}
}

func (a *Adapter) wsSession(ctx context.Context, subscribeMsg []byte, onMsg func(channel string, data json.RawMessage) error) error {
	conn, err := a.cfg.Dial(ctx, a.cfg.WS, nil)
	if err != nil {
		return fmt.Errorf("hyperliquid: dial: %w", err)
	}
	defer conn.Close()

	if err := conn.WriteMessage(websocket.TextMessage, subscribeMsg); err != nil {
		return fmt.Errorf("hyperliquid: subscribe: %w", err)
	}

	type result struct {
		msg []byte
		err error
	}
	ch := make(chan result, 1)
	var once sync.Once
	go func() {
		for {
			_, msg, err := conn.ReadMessage()
			once.Do(func() {})
			select {
			case ch <- result{msg, err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case r := <-ch:
			if r.err != nil {
				return r.err
			}
			var env wsEnvelope
			if err := json.Unmarshal(r.msg, &env); err != nil {
				continue
			}
			if env.Channel == "subscriptionResponse" {
				continue
			}
			if err := onMsg(env.Channel, env.Data); err != nil {
				return err
			}
		}
	}
}

type hlLevel struct {
	Px string `json:"px"`
	Sz string `json:"sz"`
}

type hlBook struct {
	Coin   string      `json:"coin"`
	Time   int64       `json:"time"`
	Levels [][]hlLevel `json:"levels"`
}

func parseL2Book(raw []byte, symbol exchange.Symbol, kind exchange.Kind) (exchange.Book, error) {
	var b hlBook
	if err := json.Unmarshal(raw, &b); err != nil {
		return exchange.Book{}, fmt.Errorf("hyperliquid: book json: %w", err)
	}
	if len(b.Levels) < 2 {
		return exchange.Book{}, fmt.Errorf("hyperliquid: book levels missing")
	}
	bids := levelsFrom(b.Levels[0])
	asks := levelsFrom(b.Levels[1])
	ts := time.UnixMilli(b.Time).UTC()
	if b.Time == 0 {
		ts = time.Now().UTC()
	}
	return exchange.Book{
		Venue: venueID, Symbol: symbol, Kind: kind,
		Bids: bids, Asks: asks, Time: ts,
	}, nil
}

func levelsFrom(in []hlLevel) []exchange.Level {
	out := make([]exchange.Level, 0, len(in))
	for _, lv := range in {
		out = append(out, exchange.Level{Price: lv.Px, Size: lv.Sz})
	}
	return out
}

type hlTrade struct {
	Coin string `json:"coin"`
	Side string `json:"side"`
	Px   string `json:"px"`
	Sz   string `json:"sz"`
	Time int64  `json:"time"`
}

func parseTrades(raw []byte, symbol exchange.Symbol, kind exchange.Kind) ([]exchange.Tick, error) {
	var trades []hlTrade
	if err := json.Unmarshal(raw, &trades); err != nil {
		return nil, fmt.Errorf("hyperliquid: trades json: %w", err)
	}
	out := make([]exchange.Tick, 0, len(trades))
	for _, tr := range trades {
		side := exchange.Side("")
		switch tr.Side {
		case "B", "buy", "Buy":
			side = exchange.SideBuy
		case "A", "S", "sell", "Sell":
			side = exchange.SideSell
		}
		ts := time.UnixMilli(tr.Time).UTC()
		if tr.Time == 0 {
			ts = time.Now().UTC()
		}
		out = append(out, exchange.Tick{
			Venue: venueID, Symbol: symbol, Kind: kind,
			Price: tr.Px, Size: tr.Sz, Side: side, Time: ts,
		})
	}
	return out, nil
}
