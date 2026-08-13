// Package grvt is a read-only GRVT market-data Exchange adapter (P8).
// Books via POST full/v1/book and WS v1.book.s / v1.book.d; ticks via WS v1.trade. Orders return ErrReadOnly.
package grvt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/nguyenducthinhdl/hhld/src/exchange"
)

const venueID = exchange.VenueID("grvt")

var _ exchange.Exchange = (*Adapter)(nil)

// Config wires endpoints and HHLD→instrument symbol map.
type Config struct {
	REST    string
	WS      string
	Symbols map[exchange.Symbol]string // HHLD → GRVT instrument (e.g. BTC_USDT_Perp)
	Kind    exchange.Kind
	HTTP    *http.Client
	Dial    exchange.DialWS
	// BookDepth for REST snapshot and WS selector (10/50/100/500).
	BookDepth int
	// BookRateMS is WS publish rate for book.s (500 or 1000).
	BookRateMS int
	// TradeLimit is WS trade feed limit (50/200/500/1000).
	TradeLimit    int
	ReconnectWait time.Duration
}

// Adapter is a market-data-only GRVT client (full field names).
type Adapter struct {
	cfg Config
}

// New builds an adapter. Defaults mainnet full endpoints and KindPerp when unset.
func New(cfg Config) *Adapter {
	if cfg.REST == "" || cfg.WS == "" {
		ep := exchange.DefaultGRVTMainnet()
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
	if cfg.BookDepth <= 0 {
		cfg.BookDepth = 10
	}
	if cfg.BookRateMS <= 0 {
		cfg.BookRateMS = 500
	}
	if cfg.TradeLimit <= 0 {
		cfg.TradeLimit = 50
	}
	if cfg.ReconnectWait <= 0 {
		cfg.ReconnectWait = time.Second
	}
	if cfg.Symbols == nil {
		cfg.Symbols = map[exchange.Symbol]string{}
	}
	return &Adapter{cfg: cfg}
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

func (a *Adapter) instrument(symbol exchange.Symbol) (string, error) {
	inst, ok := a.cfg.Symbols[symbol]
	if !ok || inst == "" {
		return "", fmt.Errorf("grvt: no symbol map for %s", symbol)
	}
	return inst, nil
}

func (a *Adapter) SnapshotBook(ctx context.Context, symbol exchange.Symbol) (exchange.Book, error) {
	inst, err := a.instrument(symbol)
	if err != nil {
		return exchange.Book{}, err
	}
	body, _ := json.Marshal(map[string]any{
		"instrument": inst,
		"depth":      a.cfg.BookDepth,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.cfg.REST+"/full/v1/book", bytes.NewReader(body))
	if err != nil {
		return exchange.Book{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := a.cfg.HTTP.Do(req)
	if err != nil {
		return exchange.Book{}, fmt.Errorf("grvt: snapshot: %w", err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return exchange.Book{}, err
	}
	if res.StatusCode != http.StatusOK {
		return exchange.Book{}, fmt.Errorf("grvt: snapshot status %d: %s", res.StatusCode, raw)
	}
	return parseBookREST(raw, symbol, a.cfg.Kind)
}

func (a *Adapter) SubscribeBook(ctx context.Context, symbol exchange.Symbol, h exchange.BookHandler) error {
	inst, err := a.instrument(symbol)
	if err != nil {
		return err
	}
	// Selector format: instrument@rate-numLevels-depth (per GRVT docs 2026-08).
	// numLevels controls aggregation buckets; use 1 (finest) for best-price fidelity.
	feed := fmt.Sprintf("%s@%d-1-%d", inst, a.cfg.BookRateMS, a.cfg.BookDepth)
	sub, _ := json.Marshal(map[string]any{
		"stream":  "v1.book.s",
		"feed":    []string{feed},
		"method":  "subscribe",
		"is_full": true,
	})
	return a.runWS(ctx, sub, "v1.book.s", nil, func(feed json.RawMessage) error {
		book, err := parseBookFeed(feed, symbol, a.cfg.Kind)
		if err != nil {
			return err
		}
		h(book)
		return nil
	})
}

// DeltaHandler receives a normalized book delta (size "0" deletes a level).
type DeltaHandler func(venue exchange.VenueID, symbol exchange.Symbol, kind exchange.Kind, bids, asks []exchange.Level, t time.Time, seq uint64)

// SubscribeBookDeltas streams v1.book.d until ctx is canceled.
// onReconnect is called at the start of each WS session so callers can Clear the BookStore
// before the next snapshot/delta (P8 reconnect doctrine).
// The first feed after subscribe may be a full snapshot (seq 0); onSnapshot handles those.
func (a *Adapter) SubscribeBookDeltas(
	ctx context.Context,
	symbol exchange.Symbol,
	onReconnect func(symbol exchange.Symbol),
	onSnapshot exchange.BookHandler,
	onDelta DeltaHandler,
) error {
	inst, err := a.instrument(symbol)
	if err != nil {
		return err
	}
	feed := fmt.Sprintf("%s@%d", inst, a.cfg.BookRateMS)
	sub, _ := json.Marshal(map[string]any{
		"stream":  "v1.book.d",
		"feed":    []string{feed},
		"method":  "subscribe",
		"is_full": true,
	})
	gotSnap := false
	return a.runWS(ctx, sub, "v1.book.d", func() {
		gotSnap = false
		if onReconnect != nil {
			onReconnect(symbol)
		}
	}, func(raw json.RawMessage) error {
		// First feed after (re)connect is treated as a full snapshot; later feeds are deltas.
		if !gotSnap {
			book, err := parseBookFeed(raw, symbol, a.cfg.Kind)
			if err != nil {
				return err
			}
			gotSnap = true
			if onSnapshot != nil {
				onSnapshot(book)
			}
			return nil
		}
		d, err := parseDeltaFeed(raw, symbol, a.cfg.Kind)
		if err != nil {
			return err
		}
		if onDelta != nil {
			onDelta(d.Venue, d.Symbol, d.Kind, d.Bids, d.Asks, d.Time, d.Seq)
		}
		return nil
	})
}

func (a *Adapter) SubscribeTicks(ctx context.Context, symbol exchange.Symbol, h exchange.TickHandler) error {
	inst, err := a.instrument(symbol)
	if err != nil {
		return err
	}
	feed := fmt.Sprintf("%s@%d", inst, a.cfg.TradeLimit)
	sub, _ := json.Marshal(map[string]any{
		"stream":  "v1.trade",
		"feed":    []string{feed},
		"method":  "subscribe",
		"is_full": true,
	})
	return a.runWS(ctx, sub, "v1.trade", nil, func(feed json.RawMessage) error {
		ticks, err := parseTradeFeed(feed, symbol, a.cfg.Kind)
		if err != nil {
			return err
		}
		for _, tk := range ticks {
			h(tk)
		}
		return nil
	})
}

func (a *Adapter) PlaceOrder(ctx context.Context, req exchange.OrderRequest) (exchange.OrderAck, error) {
	return exchange.OrderAck{}, exchange.ErrReadOnly
}

func (a *Adapter) CancelOrder(ctx context.Context, orderID string) error {
	return exchange.ErrReadOnly
}

func (a *Adapter) runWS(ctx context.Context, subscribeMsg []byte, stream string, onSession func(), onFeed func(json.RawMessage) error) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if onSession != nil {
			onSession()
		}
		err := a.wsSession(ctx, subscribeMsg, stream, onFeed)
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

func (a *Adapter) wsSession(ctx context.Context, subscribeMsg []byte, stream string, onFeed func(json.RawMessage) error) error {
	conn, err := a.cfg.Dial(ctx, a.cfg.WS, nil)
	if err != nil {
		return fmt.Errorf("grvt: dial: %w", err)
	}
	defer conn.Close()
	if err := conn.WriteMessage(websocket.TextMessage, subscribeMsg); err != nil {
		return fmt.Errorf("grvt: subscribe: %w", err)
	}

	type result struct {
		msg []byte
		err error
	}
	ch := make(chan result, 1)
	var once sync.Once
	_ = once
	go func() {
		for {
			_, msg, err := conn.ReadMessage()
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
		var env struct {
			Stream   string          `json:"stream"`
			Selector string          `json:"selector"`
			Feed     json.RawMessage `json:"feed"`
			Subs     []string        `json:"subs"`
			Error    *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(r.msg, &env); err != nil {
			continue
		}
		if env.Error != nil {
			return fmt.Errorf("grvt: rpc error %d: %s", env.Error.Code, env.Error.Message)
		}
		if len(env.Subs) > 0 && len(env.Feed) == 0 {
			continue // subscribe ack
		}
		if env.Stream != "" && env.Stream != stream {
			continue
		}
		if len(env.Feed) == 0 || string(env.Feed) == "null" || string(env.Feed) == "{}" {
			continue
		}
		if err := onFeed(env.Feed); err != nil {
			return err
		}
		}
	}
}

type grvtLevel struct {
	Price string `json:"price"`
	Size  string `json:"size"`
}

type grvtBookBody struct {
	EventTime  string      `json:"event_time"`
	Instrument string      `json:"instrument"`
	Bids       []grvtLevel `json:"bids"`
	Asks       []grvtLevel `json:"asks"`
}

func parseBookREST(raw []byte, symbol exchange.Symbol, kind exchange.Kind) (exchange.Book, error) {
	var wrap struct {
		Result grvtBookBody `json:"result"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return exchange.Book{}, fmt.Errorf("grvt: book json: %w", err)
	}
	return bookFrom(wrap.Result, symbol, kind)
}

func parseBookFeed(raw []byte, symbol exchange.Symbol, kind exchange.Kind) (exchange.Book, error) {
	var body grvtBookBody
	if err := json.Unmarshal(raw, &body); err != nil {
		return exchange.Book{}, fmt.Errorf("grvt: book feed json: %w", err)
	}
	return bookFrom(body, symbol, kind)
}

func bookFrom(b grvtBookBody, symbol exchange.Symbol, kind exchange.Kind) (exchange.Book, error) {
	ts, err := parseUnixNano(b.EventTime)
	if err != nil {
		ts = time.Now().UTC()
	}
	bids := make([]exchange.Level, 0, len(b.Bids))
	for _, lv := range b.Bids {
		bids = append(bids, exchange.Level{Price: lv.Price, Size: lv.Size})
	}
	asks := make([]exchange.Level, 0, len(b.Asks))
	for _, lv := range b.Asks {
		asks = append(asks, exchange.Level{Price: lv.Price, Size: lv.Size})
	}
	return exchange.Book{
		Venue: venueID, Symbol: symbol, Kind: kind,
		Bids: bids, Asks: asks, Time: ts,
	}, nil
}

type deltaBody struct {
	EventTime      string      `json:"event_time"`
	Instrument     string      `json:"instrument"`
	Bids           []grvtLevel `json:"bids"`
	Asks           []grvtLevel `json:"asks"`
	SequenceNumber string      `json:"sequence_number"`
}

type deltaFeed struct {
	Venue  exchange.VenueID
	Symbol exchange.Symbol
	Kind   exchange.Kind
	Bids   []exchange.Level
	Asks   []exchange.Level
	Time   time.Time
	Seq    uint64
}

func parseDeltaFeed(raw []byte, symbol exchange.Symbol, kind exchange.Kind) (deltaFeed, error) {
	var body deltaBody
	if err := json.Unmarshal(raw, &body); err != nil {
		return deltaFeed{}, fmt.Errorf("grvt: delta feed json: %w", err)
	}
	ts, err := parseUnixNano(body.EventTime)
	if err != nil {
		ts = time.Now().UTC()
	}
	var seq uint64
	if body.SequenceNumber != "" {
		n, err := strconv.ParseUint(body.SequenceNumber, 10, 64)
		if err == nil {
			seq = n
		}
	}
	bids := make([]exchange.Level, 0, len(body.Bids))
	for _, lv := range body.Bids {
		bids = append(bids, exchange.Level{Price: lv.Price, Size: lv.Size})
	}
	asks := make([]exchange.Level, 0, len(body.Asks))
	for _, lv := range body.Asks {
		asks = append(asks, exchange.Level{Price: lv.Price, Size: lv.Size})
	}
	return deltaFeed{
		Venue: venueID, Symbol: symbol, Kind: kind,
		Bids: bids, Asks: asks, Time: ts, Seq: seq,
	}, nil
}

type grvtTrade struct {
	EventTime  string `json:"event_time"`
	Instrument string `json:"instrument"`
	Price      string `json:"price"`
	Size       string `json:"size"`
	Side       string `json:"side"`
	IsTakerBuyer *bool `json:"is_taker_buyer"`
}

func parseTradeFeed(raw []byte, symbol exchange.Symbol, kind exchange.Kind) ([]exchange.Tick, error) {
	// Feed may be one trade object or an array.
	var one grvtTrade
	if err := json.Unmarshal(raw, &one); err == nil && (one.Price != "" || one.Size != "") {
		return []exchange.Tick{tickFrom(one, symbol, kind)}, nil
	}
	var many []grvtTrade
	if err := json.Unmarshal(raw, &many); err != nil {
		return nil, fmt.Errorf("grvt: trade feed json: %w", err)
	}
	out := make([]exchange.Tick, 0, len(many))
	for _, tr := range many {
		out = append(out, tickFrom(tr, symbol, kind))
	}
	return out, nil
}

func tickFrom(tr grvtTrade, symbol exchange.Symbol, kind exchange.Kind) exchange.Tick {
	ts, err := parseUnixNano(tr.EventTime)
	if err != nil {
		ts = time.Now().UTC()
	}
	side := exchange.Side("")
	switch tr.Side {
	case "BUY", "buy", "Bid", "BID":
		side = exchange.SideBuy
	case "SELL", "sell", "Ask", "ASK":
		side = exchange.SideSell
	}
	if side == "" && tr.IsTakerBuyer != nil {
		if *tr.IsTakerBuyer {
			side = exchange.SideBuy
		} else {
			side = exchange.SideSell
		}
	}
	return exchange.Tick{
		Venue: venueID, Symbol: symbol, Kind: kind,
		Price: tr.Price, Size: tr.Size, Side: side, Time: ts,
	}
}

func parseUnixNano(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, fmt.Errorf("empty time")
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	// Heuristic: ns vs ms
	if n < 1e12 {
		return time.Unix(n, 0).UTC(), nil
	}
	if n < 1e14 {
		return time.UnixMilli(n).UTC(), nil
	}
	return time.Unix(0, n).UTC(), nil
}
