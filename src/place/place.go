// Package place is the one-leg OrderRequest path (P11).
// Local uses the fake matcher; testnet/staging Hyperliquid uses signed writes.
package place

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/admin"
	"github.com/nguyenducthinhdl/hhld/src/config"
	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/src/exchange/fake"
	"github.com/nguyenducthinhdl/hhld/src/exchange/hyperliquid"
	"github.com/nguyenducthinhdl/hhld/src/ledger"
	"github.com/nguyenducthinhdl/hhld/src/risk"
	"github.com/nguyenducthinhdl/hhld/src/secrets"
	"github.com/nguyenducthinhdl/hhld/src/strategy"
)

// DefaultLedgerPath is the process-local SQLite file (gitignored via *.db).
const DefaultLedgerPath = "hhld-ledger.db"

// Request is one place (local fake or venue live).
type Request struct {
	Cfg        config.Config
	Env        string
	Venue      exchange.VenueID
	Kind       exchange.Kind
	Symbol     exchange.Symbol
	Side       exchange.Side
	Size       string
	Price      string
	LedgerPath string
	// Ledger if set is used and not closed.
	Ledger *ledger.SQLite
	// Exchange if set is injected (tests). Local expects fake; Venue expects live HL.
	Exchange exchange.Exchange
}

// Result is the operator-facing JSON (no secrets).
type Result struct {
	Ack     exchange.OrderAck `json:"ack"`
	Get     exchange.OrderAck `json:"get"`
	TraceID string            `json:"trace_id"`
}

type bookSeeder interface {
	SetBook(exchange.Book)
}

// CheckLocalLoop is the start gate for -trade-local / -paper-live (never load keys).
func CheckLocalLoop(flagEnv string) error {
	if err := secrets.EnvPin(flagEnv); err != nil {
		return err
	}
	if err := secrets.RequireNoLiveKeys(); err != nil {
		return err
	}
	if secrets.KillEngaged() {
		return fmt.Errorf("place: kill switch engaged (HHLD_KILL or hhld.kill)")
	}
	return nil
}

// Run dispatches local fake place vs HL testnet/staging venue place.
func Run(ctx context.Context, req Request) (Result, error) {
	env := secrets.NormalizeEnv(req.Env)
	switch {
	case env == secrets.EnvLocal:
		return Local(ctx, req)
	case secrets.HLTestnetEnv(env):
		return Venue(ctx, req)
	default:
		return Result{}, fmt.Errorf("place: -env %s not in this slice (use local|testnet|staging)", env)
	}
}

// Local runs env pin → refuse keys → kill → risk → fake PlaceOrder → GetOrder → ledger.
func Local(ctx context.Context, req Request) (Result, error) {
	env := secrets.NormalizeEnv(req.Env)
	if env != secrets.EnvLocal {
		return Result{}, fmt.Errorf("place: Local only supports -env local (use Run/Venue for testnet|staging)")
	}
	if err := secrets.EnvPin(env); err != nil {
		return Result{}, err
	}
	if err := secrets.RefuseLocal(env); err != nil {
		return Result{}, err
	}
	if secrets.KillEngaged() {
		return Result{}, fmt.Errorf("place: kill switch engaged (HHLD_KILL or hhld.kill)")
	}
	if err := req.Cfg.PinEnv(env); err != nil {
		return Result{}, err
	}
	return execute(ctx, req, env, "local-", req.Exchange)
}

// Venue runs pin → LIVE_ORDERS → HL auth → kill → risk → live PlaceOrder → GetOrder → ledger.
func Venue(ctx context.Context, req Request) (Result, error) {
	env := secrets.NormalizeEnv(req.Env)
	if !secrets.HLTestnetEnv(env) {
		return Result{}, fmt.Errorf("place: Venue only supports -env testnet|staging")
	}
	if err := secrets.EnvPin(env); err != nil {
		return Result{}, err
	}
	if err := secrets.RequireLiveOrders(); err != nil {
		return Result{}, err
	}
	if secrets.KillEngaged() {
		return Result{}, fmt.Errorf("place: kill switch engaged (HHLD_KILL or hhld.kill)")
	}
	if req.Venue != "hyperliquid" {
		return Result{}, fmt.Errorf("place: signed %s adapters not in this slice (hyperliquid testnet only)", req.Venue)
	}
	if err := req.Cfg.PinEnv(env); err != nil {
		return Result{}, err
	}
	auth, err := secrets.LoadHLAuth(env)
	if err != nil {
		return Result{}, err
	}

	ex := req.Exchange
	if ex == nil {
		ep := exchange.MarketEndpoints(env, req.Venue)
		live, err := hyperliquid.NewLive(hyperliquid.Config{
			REST:    ep.REST,
			WS:      ep.WS,
			Symbols: nativeMap(req.Cfg, req.Venue),
			Kind:    kindOrPerp(req.Kind),
		}, hyperliquid.Auth{
			AccountAddress: auth.AccountAddress,
			PrivateKeyHex:  auth.PrivateKeyHex,
			Testnet:        auth.Testnet,
		})
		if err != nil {
			return Result{}, err
		}
		ex = live
	}
	return execute(ctx, req, env, "testnet-", ex)
}

func kindOrPerp(k exchange.Kind) exchange.Kind {
	if k == "" {
		return exchange.KindPerp
	}
	return k
}

func nativeMap(cfg config.Config, venue exchange.VenueID) map[exchange.Symbol]string {
	out := make(map[exchange.Symbol]string)
	for _, e := range cfg.SymbolMap {
		if spec, ok := e.Venues[string(venue)]; ok && spec.SymbolName != "" {
			out[exchange.Symbol(e.Symbol)] = spec.SymbolName
		}
	}
	return out
}

func execute(ctx context.Context, req Request, env, tracePrefix string, ex exchange.Exchange) (Result, error) {
	if req.Venue == "" || req.Symbol == "" || req.Size == "" {
		return Result{}, fmt.Errorf("place: venue, symbol, and size are required")
	}
	kind := kindOrPerp(req.Kind)
	if req.Side != exchange.SideBuy && req.Side != exchange.SideSell {
		return Result{}, fmt.Errorf("place: side must be buy or sell")
	}
	if _, err := req.Cfg.NativeInstrument(req.Venue, req.Symbol, kind); err != nil {
		return Result{}, err
	}

	st := req.Ledger
	if st == nil {
		path := req.LedgerPath
		if path == "" {
			path = DefaultLedgerPath
		}
		var err error
		st, err = ledger.Open(path, env)
		if err != nil {
			return Result{}, err
		}
		defer func() { _ = st.Close() }()
	}

	if ex == nil {
		ex = fake.New(req.Venue, nil)
	}
	if ex.ID() != req.Venue {
		return Result{}, fmt.Errorf("place: exchange id %s does not match -venue %s", ex.ID(), req.Venue)
	}

	price := strings.TrimSpace(req.Price)
	now := time.Now().UTC()
	if price == "" {
		if env == secrets.EnvLocal {
			px, err := tobOrSeed(ctx, ex, req.Symbol, kind, req.Side, now)
			if err != nil {
				return Result{}, err
			}
			price = px
		} else if b, err := ex.SnapshotBook(ctx, req.Symbol); err == nil {
			if px, err := tobFromBook(b, req.Side); err == nil {
				price = px
			}
		}
	}

	traceID := tracePrefix + strings.TrimPrefix(exchange.NewClientOrderID(), "0x")[:16]
	d := strategy.Decision{
		TraceID: traceID,
		Reason:  "hhld-place " + env,
		Legs: []strategy.Leg{{
			Venue:  req.Venue,
			Symbol: req.Symbol,
			Kind:   kind,
			Side:   req.Side,
			Price:  price,
			Size:   req.Size,
		}},
	}
	gate := risk.NewGate(risk.ParamsFromConfig(req.Cfg))
	release, acq := gate.TryAcquire(d)
	if !acq.OK {
		return Result{}, fmt.Errorf("place: risk acquire: %s", acq.Reason)
	}
	defer release()
	v, err := gate.Evaluate(ctx, d, risk.MarketView{Now: now})
	if err != nil {
		return Result{}, err
	}
	if !v.OK {
		return Result{}, fmt.Errorf("place: risk: %s", v.Reason)
	}

	ack, err := ex.PlaceOrder(ctx, exchange.OrderRequest{
		ClientOrderID: exchange.NewClientOrderID(),
		TraceID:       traceID,
		Symbol:        req.Symbol,
		Kind:          kind,
		Side:          req.Side,
		Price:         req.Price, // empty → TOB IOC (fake or live)
		Size:          req.Size,
		TIF:           exchange.TIFIOC,
	})
	got := exchange.OrderAck{}
	getErr := err
	if err == nil {
		got, getErr = ex.GetOrder(ctx, ack.OrderID)
		if getErr != nil && ack.ClientOrderID != "" {
			got, getErr = ex.GetOrder(ctx, ack.ClientOrderID)
		}
		// IOC fills can vanish from orderStatus (or user addr may be the agent).
		// Place ack is authoritative when already terminal.
		if getErr != nil && (ack.Status == "filled" || ack.Status == "resting") {
			got = ack
			getErr = nil
		}
	}
	leg := d.Legs[0]
	if strings.TrimSpace(leg.Price) == "" {
		leg.Price = price
	}
	results := []strategy.LegResult{{
		Index: 0,
		Leg:   leg,
		Ack:   ack,
		Err:   err,
	}}
	fees := risk.ParamsFromConfig(req.Cfg).FeeSchedule()
	if recErr := admin.RecordPaperDecision(ctx, st, st, d, results, fees); recErr != nil {
		return Result{}, recErr
	}
	if err != nil {
		return Result{}, err
	}
	if getErr != nil {
		return Result{Ack: ack, TraceID: traceID}, fmt.Errorf("place: GetOrder: %w", getErr)
	}
	return Result{Ack: ack, Get: got, TraceID: traceID}, nil
}

func tobOrSeed(ctx context.Context, ex exchange.Exchange, symbol exchange.Symbol, kind exchange.Kind, side exchange.Side, now time.Time) (string, error) {
	if b, err := ex.SnapshotBook(ctx, symbol); err == nil {
		return tobFromBook(b, side)
	}
	s, ok := ex.(bookSeeder)
	if !ok {
		return "", fmt.Errorf("place: no book for %s (pass -price)", symbol)
	}
	s.SetBook(exchange.Book{
		Symbol: symbol,
		Kind:   kind,
		Time:   now,
		Bids:   []exchange.Level{{Price: "100000", Size: "1"}},
		Asks:   []exchange.Level{{Price: "100001", Size: "1"}},
	})
	b, err := ex.SnapshotBook(ctx, symbol)
	if err != nil {
		return "", err
	}
	return tobFromBook(b, side)
}

func tobFromBook(b exchange.Book, side exchange.Side) (string, error) {
	if side == exchange.SideBuy {
		if len(b.Asks) == 0 || strings.TrimSpace(b.Asks[0].Price) == "" {
			return "", fmt.Errorf("place: empty ask")
		}
		return b.Asks[0].Price, nil
	}
	if len(b.Bids) == 0 || strings.TrimSpace(b.Bids[0].Price) == "" {
		return "", fmt.Errorf("place: empty bid")
	}
	return b.Bids[0].Price, nil
}
