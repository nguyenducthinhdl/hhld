// Package fake provides an in-memory Exchange for deterministic dual-venue tests.
// No network I/O: books and ticks are pushed by the test or a scripted feed.
package fake

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/exchange"
)

// Ensure Fake implements exchange.Exchange and P10 GetOrder reconcile.
var _ exchange.Exchange = (*Exchange)(nil)

// Exchange is a fake venue driven by SetBook / PushTick.
type Exchange struct {
	id    exchange.VenueID
	clock exchange.Clock

	mu         sync.RWMutex
	books      map[exchange.Symbol]exchange.Book
	bookSubs   map[exchange.Symbol][]exchange.BookHandler
	tickSubs   map[exchange.Symbol][]exchange.TickHandler
	deltaSubs  map[exchange.Symbol][]deltaHandler
	acks       map[string]exchange.OrderAck
	nextOrd    int
	bookDelay  time.Duration // simulated book-path latency (Snapshot/Subscribe)
	orderDelay time.Duration // simulated order-path latency (Place/Cancel)
}

// New creates a fake venue. If clock is nil, a ManualClock at Unix(0) UTC is used.
func New(id exchange.VenueID, clock exchange.Clock) *Exchange {
	if clock == nil {
		clock = exchange.NewManualClock(time.Unix(0, 0).UTC())
	}
	return &Exchange{
		id:        id,
		clock:     clock,
		books:     make(map[exchange.Symbol]exchange.Book),
		bookSubs:  make(map[exchange.Symbol][]exchange.BookHandler),
		tickSubs:  make(map[exchange.Symbol][]exchange.TickHandler),
		deltaSubs: make(map[exchange.Symbol][]deltaHandler),
		acks:      make(map[string]exchange.OrderAck),
	}
}

// Dual holds two fake venues that share one ManualClock for arb tests.
type Dual struct {
	Clock *exchange.ManualClock
	A     *Exchange
	B     *Exchange
}

// NewDual builds venues aID and bID with a shared ManualClock starting at start.
func NewDual(aID, bID exchange.VenueID, start time.Time) *Dual {
	clock := exchange.NewManualClock(start)
	return &Dual{
		Clock: clock,
		A:     New(aID, clock),
		B:     New(bID, clock),
	}
}

func (e *Exchange) ID() exchange.VenueID { return e.id }

// SetBookDelay sets wall-clock sleep before SnapshotBook / SubscribeBook complete.
// Use with context deadlines to simulate book-path timeouts (see spec/networking.md).
func (e *Exchange) SetBookDelay(d time.Duration) {
	e.mu.Lock()
	e.bookDelay = d
	e.mu.Unlock()
}

// SetOrderDelay sets wall-clock sleep before PlaceOrder / CancelOrder complete.
// Use with context deadlines to simulate order-path timeouts.
func (e *Exchange) SetOrderDelay(d time.Duration) {
	e.mu.Lock()
	e.orderDelay = d
	e.mu.Unlock()
}

func (e *Exchange) sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (e *Exchange) bookPathDelay() time.Duration {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.bookDelay
}

func (e *Exchange) orderPathDelay() time.Duration {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.orderDelay
}

// SetBook stores a book snapshot (Time set from clock if zero) and notifies subscribers.
func (e *Exchange) SetBook(book exchange.Book) {
	e.mu.Lock()
	book.Venue = e.id
	if book.Time.IsZero() {
		book.Time = e.clock.Now()
	}
	e.books[book.Symbol] = book
	subs := append([]exchange.BookHandler(nil), e.bookSubs[book.Symbol]...)
	e.mu.Unlock()

	for _, h := range subs {
		h(book)
	}
}

// PushDelta notifies delta subscribers without replacing the stored snapshot.
// Size "0" means delete that price level (same semantics as market.BookDelta).
// For event-driven tests, prefer publishing market.DeltaEvent on a market.Bus.
func (e *Exchange) PushDelta(symbol exchange.Symbol, bids, asks []exchange.Level) {
	e.mu.RLock()
	handlers := append([]deltaHandler(nil), e.deltaSubs[symbol]...)
	e.mu.RUnlock()
	for _, h := range handlers {
		h(symbol, bids, asks, e.clock.Now())
	}
}

// SubscribeDelta registers for PushDelta notifications (P8.5 fake helper).
func (e *Exchange) SubscribeDelta(symbol exchange.Symbol, h func(symbol exchange.Symbol, bids, asks []exchange.Level, t time.Time)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.deltaSubs[symbol] = append(e.deltaSubs[symbol], h)
}

type deltaHandler func(symbol exchange.Symbol, bids, asks []exchange.Level, t time.Time)

// PushTick notifies tick subscribers. Time is set from clock if zero.
func (e *Exchange) PushTick(tick exchange.Tick) {
	e.mu.RLock()
	tick.Venue = e.id
	if tick.Time.IsZero() {
		tick.Time = e.clock.Now()
	}
	subs := append([]exchange.TickHandler(nil), e.tickSubs[tick.Symbol]...)
	e.mu.RUnlock()

	for _, h := range subs {
		h(tick)
	}
}

func (e *Exchange) SnapshotBook(ctx context.Context, symbol exchange.Symbol) (exchange.Book, error) {
	if err := e.sleep(ctx, e.bookPathDelay()); err != nil {
		return exchange.Book{}, fmt.Errorf("fake: book delay: %w", err)
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	book, ok := e.books[symbol]
	if !ok {
		return exchange.Book{}, fmt.Errorf("fake: no book for %s on %s", symbol, e.id)
	}
	return book, nil
}

// SubscribeBook registers h and immediately delivers the current book if present.
// Later SetBook calls notify h. Book-path delay is applied before registration/delivery.
func (e *Exchange) SubscribeBook(ctx context.Context, symbol exchange.Symbol, h exchange.BookHandler) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := e.sleep(ctx, e.bookPathDelay()); err != nil {
		return fmt.Errorf("fake: book delay: %w", err)
	}
	e.mu.Lock()
	e.bookSubs[symbol] = append(e.bookSubs[symbol], h)
	book, ok := e.books[symbol]
	e.mu.Unlock()

	if ok {
		h(book)
	}
	return nil
}

// SubscribeTicks registers h for PushTick updates. Returns nil after registration.
func (e *Exchange) SubscribeTicks(ctx context.Context, symbol exchange.Symbol, h exchange.TickHandler) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	e.mu.Lock()
	e.tickSubs[symbol] = append(e.tickSubs[symbol], h)
	e.mu.Unlock()
	return nil
}

func (e *Exchange) PlaceOrder(ctx context.Context, req exchange.OrderRequest) (exchange.OrderAck, error) {
	if err := e.sleep(ctx, e.orderPathDelay()); err != nil {
		return exchange.OrderAck{}, fmt.Errorf("fake: order delay: %w", err)
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := validateReq(req); err != nil {
		return exchange.OrderAck{}, err
	}
	book, hasBook := e.books[req.Symbol]
	fillPx, err := matchIOC(req, book, hasBook)
	if err != nil {
		return exchange.OrderAck{}, err
	}

	e.nextOrd++
	id := fmt.Sprintf("%s-ord-%d", e.id, e.nextOrd)
	cloid := req.ClientOrderID
	if cloid == "" {
		cloid = exchange.NewClientOrderID()
	}
	ack := exchange.OrderAck{
		OrderID:       id,
		ClientOrderID: cloid,
		TraceID:       req.TraceID,
		HedgeID:       req.HedgeID,
		Symbol:        req.Symbol,
		Status:        "filled",
		Time:          e.clock.Now(),
	}
	_ = fillPx
	e.acks[id] = ack
	e.acks[cloid] = ack
	return ack, nil
}

func validateReq(req exchange.OrderRequest) error {
	switch req.Kind {
	case exchange.KindPerp, exchange.KindSpot, exchange.KindPrediction:
	case "":
		return fmt.Errorf("fake: kind required")
	default:
		return fmt.Errorf("fake: unknown kind %q", req.Kind)
	}
	if req.Symbol == "" {
		return fmt.Errorf("fake: symbol required")
	}
	if req.Side != exchange.SideBuy && req.Side != exchange.SideSell {
		return fmt.Errorf("fake: side required")
	}
	sz, err := strconv.ParseFloat(req.Size, 64)
	if err != nil || sz <= 0 {
		return fmt.Errorf("fake: bad size %q", req.Size)
	}
	return nil
}

// matchIOC returns the fill price. Empty Price needs a TOB. Explicit Price fills
// at limit if it crosses TOB, or at limit if there is no book (paper legs).
func matchIOC(req exchange.OrderRequest, book exchange.Book, hasBook bool) (string, error) {
	if hasBook && book.Kind != "" && req.Kind != "" && book.Kind != req.Kind {
		return "", fmt.Errorf("fake: kind mismatch book=%s req=%s", book.Kind, req.Kind)
	}
	if strings.TrimSpace(req.Price) == "" {
		if !hasBook {
			return "", fmt.Errorf("fake: no book for %s", req.Symbol)
		}
		tob, err := tobPrice(book, req.Side)
		if err != nil {
			return "", err
		}
		return tob, nil
	}
	px, err := strconv.ParseFloat(req.Price, 64)
	if err != nil || px <= 0 {
		return "", fmt.Errorf("fake: bad price %q", req.Price)
	}
	if !hasBook {
		return req.Price, nil
	}
	tob, err := tobPrice(book, req.Side)
	if err != nil {
		return "", err
	}
	tobPx, err := strconv.ParseFloat(tob, 64)
	if err != nil {
		return "", fmt.Errorf("fake: bad tob %q", tob)
	}
	if req.Side == exchange.SideBuy && px+1e-12 < tobPx {
		return "", fmt.Errorf("fake: ioc not marketable buy limit=%s ask=%s", req.Price, tob)
	}
	if req.Side == exchange.SideSell && px-1e-12 > tobPx {
		return "", fmt.Errorf("fake: ioc not marketable sell limit=%s bid=%s", req.Price, tob)
	}
	return tob, nil
}

func tobPrice(book exchange.Book, side exchange.Side) (string, error) {
	if side == exchange.SideBuy {
		if len(book.Asks) == 0 || strings.TrimSpace(book.Asks[0].Price) == "" {
			return "", fmt.Errorf("fake: empty ask")
		}
		return book.Asks[0].Price, nil
	}
	if len(book.Bids) == 0 || strings.TrimSpace(book.Bids[0].Price) == "" {
		return "", fmt.Errorf("fake: empty bid")
	}
	return book.Bids[0].Price, nil
}

func (e *Exchange) CancelOrder(ctx context.Context, orderID string) error {
	if err := e.sleep(ctx, e.orderPathDelay()); err != nil {
		return fmt.Errorf("fake: order delay: %w", err)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.acks, orderID)
	return nil
}

// GetOrder looks up a paper ack by order id or client order id (P10 reconcile).
func (e *Exchange) GetOrder(_ context.Context, id string) (exchange.OrderAck, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if ack, ok := e.acks[id]; ok {
		return ack, nil
	}
	return exchange.OrderAck{}, exchange.ErrOrderNotFound
}
