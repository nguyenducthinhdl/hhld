package hyperliquid

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/exchange"
)

// Auth is agent credentials for signed PlaceOrder / CancelOrder / GetOrder.
// AccountAddress is the master account used for orderStatus queries.
type Auth struct {
	AccountAddress string
	PrivateKeyHex  string
	Testnet        bool // phantom Agent source "b"; false → "a"
}

// orderWire field order matches Python SDK (a,b,p,s,r,t,c).
type orderWire struct {
	Asset      int           `msgpack:"a" json:"a"`
	IsBuy      bool          `msgpack:"b" json:"b"`
	LimitPx    string        `msgpack:"p" json:"p"`
	Size       string        `msgpack:"s" json:"s"`
	ReduceOnly bool          `msgpack:"r" json:"r"`
	OrderType  orderWireType `msgpack:"t" json:"t"`
	Cloid      *string       `msgpack:"c,omitempty" json:"c,omitempty"`
}

type orderWireType struct {
	Limit *orderWireLimit `msgpack:"limit,omitempty" json:"limit,omitempty"`
}

type orderWireLimit struct {
	Tif string `msgpack:"tif" json:"tif"`
}

type orderAction struct {
	Type     string      `msgpack:"type" json:"type"`
	Orders   []orderWire `msgpack:"orders" json:"orders"`
	Grouping string      `msgpack:"grouping" json:"grouping"`
}

type cancelAction struct {
	Type    string            `msgpack:"type" json:"type"`
	Cancels []cancelOrderWire `msgpack:"cancels" json:"cancels"`
}

type cancelOrderWire struct {
	Asset   int   `msgpack:"a" json:"a"`
	OrderID int64 `msgpack:"o" json:"o"`
}

type cancelByCloidAction struct {
	Type    string              `msgpack:"type" json:"type"`
	Cancels []cancelByCloidWire `msgpack:"cancels" json:"cancels"`
}

type cancelByCloidWire struct {
	Asset    int    `msgpack:"asset" json:"asset"`
	ClientID string `msgpack:"cloid" json:"cloid"`
}

type orderStatusInfo struct {
	Ack  exchange.OrderAck
	Coin string
}

func (a *Adapter) liveReady() (*ecdsa.PrivateKey, error) {
	if a.auth == nil || a.pk == nil {
		return nil, exchange.ErrReadOnly
	}
	return a.pk, nil
}

func (a *Adapter) PlaceOrder(ctx context.Context, req exchange.OrderRequest) (exchange.OrderAck, error) {
	if _, err := a.liveReady(); err != nil {
		return exchange.OrderAck{}, err
	}
	if req.Kind == exchange.KindSpot {
		return exchange.OrderAck{}, fmt.Errorf("hyperliquid: spot place not in this slice (perp only)")
	}
	coin, err := a.coin(req.Symbol)
	if err != nil {
		return exchange.OrderAck{}, err
	}
	asset, err := a.meta.assetIndex(ctx, a.cfg.HTTP, a.cfg.REST, coin)
	if err != nil {
		return exchange.OrderAck{}, err
	}

	price := strings.TrimSpace(req.Price)
	if price == "" {
		book, err := a.SnapshotBook(ctx, req.Symbol)
		if err != nil {
			return exchange.OrderAck{}, err
		}
		price, err = tobPrice(book, req.Side)
		if err != nil {
			return exchange.OrderAck{}, err
		}
	}
	px, err := floatToWire(price)
	if err != nil {
		return exchange.OrderAck{}, fmt.Errorf("hyperliquid: price: %w", err)
	}
	sz, err := floatToWire(req.Size)
	if err != nil {
		return exchange.OrderAck{}, fmt.Errorf("hyperliquid: size: %w", err)
	}
	cloid := strings.TrimSpace(req.ClientOrderID)
	if cloid == "" {
		cloid = exchange.NewClientOrderID()
	}
	if !strings.HasPrefix(cloid, "0x") {
		cloid = "0x" + cloid
	}

	ow := orderWire{
		Asset:      asset,
		IsBuy:      req.Side == exchange.SideBuy,
		LimitPx:    px,
		Size:       sz,
		ReduceOnly: req.ReduceOnly,
		OrderType:  orderWireType{Limit: &orderWireLimit{Tif: "Ioc"}},
		Cloid:      &cloid,
	}
	action := orderAction{Type: "order", Orders: []orderWire{ow}, Grouping: "na"}
	nonce := a.nextNonce()

	raw, err := a.postExchange(ctx, action, nonce)
	if err != nil {
		return exchange.OrderAck{
			ClientOrderID: cloid, TraceID: req.TraceID, HedgeID: req.HedgeID,
			Symbol: req.Symbol, Status: "unknown", Time: time.Now().UTC(),
		}, fmt.Errorf("%w: %v", exchange.ErrUnknownAck, err)
	}
	return parsePlaceResponse(raw, cloid, req)
}

func (a *Adapter) CancelOrder(ctx context.Context, orderID string) error {
	if _, err := a.liveReady(); err != nil {
		return err
	}
	orderID = strings.TrimSpace(orderID)
	if orderID == "" {
		return fmt.Errorf("hyperliquid: cancel id required")
	}
	info, err := a.queryOrderStatus(ctx, orderID)
	if err != nil {
		return err
	}
	asset, err := a.meta.assetIndex(ctx, a.cfg.HTTP, a.cfg.REST, info.Coin)
	if err != nil {
		return err
	}
	if strings.HasPrefix(orderID, "0x") {
		cloid := info.Ack.ClientOrderID
		if cloid == "" {
			cloid = orderID
		}
		action := cancelByCloidAction{
			Type:    "cancelByCloid",
			Cancels: []cancelByCloidWire{{Asset: asset, ClientID: cloid}},
		}
		_, err = a.postExchange(ctx, action, a.nextNonce())
		return err
	}
	oid, err := strconv.ParseInt(info.Ack.OrderID, 10, 64)
	if err != nil {
		return fmt.Errorf("hyperliquid: cancel oid: %w", err)
	}
	action := cancelAction{
		Type:    "cancel",
		Cancels: []cancelOrderWire{{Asset: asset, OrderID: oid}},
	}
	_, err = a.postExchange(ctx, action, a.nextNonce())
	return err
}

func (a *Adapter) GetOrder(ctx context.Context, id string) (exchange.OrderAck, error) {
	info, err := a.queryOrderStatus(ctx, id)
	if err != nil {
		return exchange.OrderAck{}, err
	}
	return info.Ack, nil
}

func (a *Adapter) queryOrderStatus(ctx context.Context, id string) (orderStatusInfo, error) {
	if a.auth == nil || strings.TrimSpace(a.auth.AccountAddress) == "" {
		return orderStatusInfo{}, exchange.ErrReadOnly
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return orderStatusInfo{}, fmt.Errorf("hyperliquid: get id required")
	}
	var oid any = id
	if n, err := strconv.ParseInt(id, 10, 64); err == nil {
		oid = n
	}
	body, _ := json.Marshal(map[string]any{
		"type": "orderStatus",
		"user": a.auth.AccountAddress,
		"oid":  oid,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.cfg.REST+"/info", bytes.NewReader(body))
	if err != nil {
		return orderStatusInfo{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := a.cfg.HTTP.Do(req)
	if err != nil {
		return orderStatusInfo{}, fmt.Errorf("hyperliquid: orderStatus: %w", err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return orderStatusInfo{}, err
	}
	if res.StatusCode != http.StatusOK {
		return orderStatusInfo{}, fmt.Errorf("hyperliquid: orderStatus status %d: %s", res.StatusCode, raw)
	}
	return parseOrderStatus(raw, id, a.cfg.Symbols)
}

func (a *Adapter) postExchange(ctx context.Context, action any, nonce int64) ([]byte, error) {
	sig, err := signL1Action(a.pk, action, "", nonce, !a.auth.Testnet)
	if err != nil {
		return nil, err
	}
	payload := map[string]any{
		"action":    action,
		"nonce":     nonce,
		"signature": sig,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.cfg.REST+"/exchange", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := a.cfg.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("hyperliquid: exchange status %d: %s", res.StatusCode, raw)
	}
	return raw, nil
}

func (a *Adapter) nextNonce() int64 {
	for {
		last := a.lastNonce.Load()
		candidate := time.Now().UnixMilli()
		if candidate <= last {
			candidate = last + 1
		}
		if a.lastNonce.CompareAndSwap(last, candidate) {
			return candidate
		}
	}
}

func tobPrice(b exchange.Book, side exchange.Side) (string, error) {
	if side == exchange.SideBuy {
		if len(b.Asks) == 0 || strings.TrimSpace(b.Asks[0].Price) == "" {
			return "", fmt.Errorf("hyperliquid: empty ask")
		}
		return b.Asks[0].Price, nil
	}
	if len(b.Bids) == 0 || strings.TrimSpace(b.Bids[0].Price) == "" {
		return "", fmt.Errorf("hyperliquid: empty bid")
	}
	return b.Bids[0].Price, nil
}

func floatToWire(s string) (string, error) {
	s = strings.TrimSpace(s)
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return "", err
	}
	rounded := fmt.Sprintf("%.8f", f)
	parsed, err := strconv.ParseFloat(rounded, 64)
	if err != nil {
		return "", err
	}
	if abs64(parsed-f) >= 1e-12 {
		return "", fmt.Errorf("float_to_wire rounding %s", s)
	}
	if rounded == "-0.00000000" {
		rounded = "0.00000000"
	}
	out := strings.TrimRight(rounded, "0")
	out = strings.TrimRight(out, ".")
	if out == "" || out == "-" {
		return "0", nil
	}
	return out, nil
}

func abs64(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func parsePlaceResponse(raw []byte, cloid string, req exchange.OrderRequest) (exchange.OrderAck, error) {
	ack := exchange.OrderAck{
		ClientOrderID: cloid,
		TraceID:       req.TraceID,
		HedgeID:       req.HedgeID,
		Symbol:        req.Symbol,
		Time:          time.Now().UTC(),
	}
	var top struct {
		Status   string          `json:"status"`
		Response json.RawMessage `json:"response"`
	}
	if err := json.Unmarshal(raw, &top); err != nil {
		ack.Status = "unknown"
		return ack, fmt.Errorf("%w: bad exchange json: %v", exchange.ErrUnknownAck, err)
	}
	if top.Status != "ok" {
		msg := string(top.Response)
		var s string
		if json.Unmarshal(top.Response, &s) == nil {
			msg = s
		}
		ack.Status = "rejected"
		return ack, fmt.Errorf("hyperliquid: place rejected: %s", msg)
	}
	var resp struct {
		Type string `json:"type"`
		Data struct {
			Statuses []json.RawMessage `json:"statuses"`
		} `json:"data"`
	}
	if err := json.Unmarshal(top.Response, &resp); err != nil {
		ack.Status = "unknown"
		return ack, fmt.Errorf("%w: bad response: %v", exchange.ErrUnknownAck, err)
	}
	if len(resp.Data.Statuses) == 0 {
		ack.Status = "unknown"
		return ack, fmt.Errorf("%w: empty statuses", exchange.ErrUnknownAck)
	}
	var st struct {
		Resting *struct {
			Oid   int64   `json:"oid"`
			Cloid *string `json:"cloid"`
		} `json:"resting"`
		Filled *struct {
			Oid     json.Number `json:"oid"`
			TotalSz string      `json:"totalSz"`
			AvgPx   string      `json:"avgPx"`
		} `json:"filled"`
		Error *string `json:"error"`
	}
	if err := json.Unmarshal(resp.Data.Statuses[0], &st); err != nil {
		ack.Status = "unknown"
		return ack, fmt.Errorf("%w: status json: %v", exchange.ErrUnknownAck, err)
	}
	if st.Error != nil {
		ack.Status = "rejected"
		return ack, fmt.Errorf("hyperliquid: %s", *st.Error)
	}
	if st.Filled != nil {
		ack.OrderID = st.Filled.Oid.String()
		ack.Status = "filled"
		return ack, nil
	}
	if st.Resting != nil {
		ack.OrderID = strconv.FormatInt(st.Resting.Oid, 10)
		ack.Status = "resting"
		return ack, nil
	}
	ack.Status = "unknown"
	return ack, fmt.Errorf("%w: unrecognized status %s", exchange.ErrUnknownAck, resp.Data.Statuses[0])
}

func parseOrderStatus(raw []byte, id string, symbols map[exchange.Symbol]string) (orderStatusInfo, error) {
	var qr struct {
		Status string `json:"status"`
		Order  struct {
			Status string `json:"status"`
			Order  struct {
				Oid   int64   `json:"oid"`
				Cloid *string `json:"cloid"`
				Coin  string  `json:"coin"`
			} `json:"order"`
		} `json:"order"`
	}
	if err := json.Unmarshal(raw, &qr); err != nil {
		return orderStatusInfo{}, fmt.Errorf("hyperliquid: orderStatus json: %w", err)
	}
	if qr.Status == "unknownOid" || qr.Status != "order" {
		return orderStatusInfo{}, exchange.ErrOrderNotFound
	}
	sym := exchange.Symbol("")
	for hhld, coin := range symbols {
		if coin == qr.Order.Order.Coin {
			sym = hhld
			break
		}
	}
	cloid := id
	if qr.Order.Order.Cloid != nil && *qr.Order.Order.Cloid != "" {
		cloid = *qr.Order.Order.Cloid
	}
	status := strings.ToLower(qr.Order.Status)
	switch status {
	case "open":
		status = "resting"
	}
	return orderStatusInfo{
		Coin: qr.Order.Order.Coin,
		Ack: exchange.OrderAck{
			OrderID:       strconv.FormatInt(qr.Order.Order.Oid, 10),
			ClientOrderID: cloid,
			Symbol:        sym,
			Status:        status,
			Time:          time.Now().UTC(),
		},
	}, nil
}
