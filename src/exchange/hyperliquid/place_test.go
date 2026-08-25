package hyperliquid_test

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/src/exchange/hyperliquid"
)

// Fixed secp256k1 key for deterministic signing tests.
const testKeyHex = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestSignL1_PlacePostsSignature(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/info":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["type"] == "meta" {
				_, _ = w.Write([]byte(`{"universe":[{"name":"BTC"},{"name":"ETH"}]}`))
				return
			}
			if body["type"] == "l2Book" {
				_, _ = w.Write([]byte(`{"coin":"BTC","time":1,"levels":[[{"px":"100","sz":"1"}],[{"px":"101","sz":"1"}]]}`))
				return
			}
			http.Error(w, "bad info", 400)
		case r.URL.Path == "/exchange":
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			_, _ = w.Write([]byte(`{"status":"ok","response":{"type":"order","data":{"statuses":[{"filled":{"totalSz":"0.00003","avgPx":"101","oid":42}}]}}}`))
		default:
			http.Error(w, "nope", 404)
		}
	}))
	defer srv.Close()

	ad, err := hyperliquid.NewLive(hyperliquid.Config{
		REST:    srv.URL,
		WS:      "ws://unused",
		Symbols: map[exchange.Symbol]string{"BTCUSD": "BTC"},
		HTTP:    srv.Client(),
	}, hyperliquid.Auth{
		AccountAddress: "0x1111111111111111111111111111111111111111",
		PrivateKeyHex:  "0x" + testKeyHex,
		Testnet:        true,
	})
	if err != nil {
		t.Fatal(err)
	}
	ack, err := ad.PlaceOrder(context.Background(), exchange.OrderRequest{
		ClientOrderID: "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Symbol:        "BTCUSD",
		Kind:          exchange.KindPerp,
		Side:          exchange.SideBuy,
		Size:          "0.00003",
		TIF:           exchange.TIFIOC,
	})
	if err != nil {
		t.Fatal(err)
	}
	if ack.Status != "filled" || ack.OrderID != "42" {
		t.Fatalf("%+v", ack)
	}
	sig, ok := gotBody["signature"].(map[string]any)
	if !ok {
		t.Fatalf("no signature: %+v", gotBody)
	}
	r, _ := sig["r"].(string)
	s, _ := sig["s"].(string)
	if !strings.HasPrefix(r, "0x") || !strings.HasPrefix(s, "0x") {
		t.Fatalf("sig: %+v", sig)
	}
	pk, err := crypto.HexToECDSA(testKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	if crypto.PubkeyToAddress(pk.PublicKey).Hex() == "" {
		t.Fatal("empty addr")
	}
}

func TestKeyAddress_Deterministic(t *testing.T) {
	pk, err := crypto.HexToECDSA(testKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	b, err := hex.DecodeString(testKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	pk2, err := crypto.ToECDSA(b)
	if err != nil {
		t.Fatal(err)
	}
	if crypto.PubkeyToAddress(pk.PublicKey) != crypto.PubkeyToAddress(pk2.PublicKey) {
		t.Fatal("mismatch")
	}
}

func TestLive_PlaceGetCancel(t *testing.T) {
	cloid := "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch r.URL.Path {
		case "/info":
			switch body["type"] {
			case "meta":
				_, _ = w.Write([]byte(`{"universe":[{"name":"BTC"}]}`))
			case "l2Book":
				_, _ = w.Write([]byte(`{"coin":"BTC","time":1,"levels":[[{"px":"100","sz":"1"}],[{"px":"101","sz":"1"}]]}`))
			case "orderStatus":
				_, _ = w.Write([]byte(`{
					"status":"order",
					"order":{"status":"filled","order":{"oid":99,"cloid":"` + cloid + `","coin":"BTC"}}
				}`))
			default:
				http.Error(w, "info", 400)
			}
		case "/exchange":
			action, _ := body["action"].(map[string]any)
			switch action["type"] {
			case "order":
				_, _ = w.Write([]byte(`{"status":"ok","response":{"type":"order","data":{"statuses":[{"filled":{"oid":99,"totalSz":"0.00003","avgPx":"101"}}]}}}`))
			case "cancel", "cancelByCloid":
				_, _ = w.Write([]byte(`{"status":"ok","response":{"type":"cancel","data":{"statuses":[null]}}}`))
			default:
				http.Error(w, "exchange", 400)
			}
		default:
			http.Error(w, "404", 404)
		}
	}))
	defer srv.Close()

	ad, err := hyperliquid.NewLive(hyperliquid.Config{
		REST: srv.URL, WS: "ws://unused",
		Symbols: map[exchange.Symbol]string{"BTCUSD": "BTC"},
		HTTP:    srv.Client(),
	}, hyperliquid.Auth{
		AccountAddress: "0x2222222222222222222222222222222222222222",
		PrivateKeyHex:  "0x" + testKeyHex,
		Testnet:        true,
	})
	if err != nil {
		t.Fatal(err)
	}
	ack, err := ad.PlaceOrder(context.Background(), exchange.OrderRequest{
		ClientOrderID: cloid,
		Symbol:        "BTCUSD",
		Kind:          exchange.KindPerp,
		Side:          exchange.SideBuy,
		Size:          "0.00003",
	})
	if err != nil || ack.OrderID != "99" {
		t.Fatalf("%+v %v", ack, err)
	}
	got, err := ad.GetOrder(context.Background(), cloid)
	if err != nil || got.Status != "filled" || got.Symbol != "BTCUSD" {
		t.Fatalf("%+v %v", got, err)
	}
	if err := ad.CancelOrder(context.Background(), cloid); err != nil {
		t.Fatal(err)
	}
}

func TestLive_UnknownAckOnExchangeDrop(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if r.URL.Path == "/info" && body["type"] == "meta" {
			_, _ = w.Write([]byte(`{"universe":[{"name":"BTC"}]}`))
			return
		}
		if r.URL.Path == "/info" && body["type"] == "l2Book" {
			_, _ = w.Write([]byte(`{"coin":"BTC","time":1,"levels":[[{"px":"100","sz":"1"}],[{"px":"101","sz":"1"}]]}`))
			return
		}
		if r.URL.Path == "/exchange" {
			hj, ok := w.(http.Hijacker)
			if !ok {
				http.Error(w, "no hijack", 500)
				return
			}
			conn, _, _ := hj.Hijack()
			_ = conn.Close()
			return
		}
		http.Error(w, "x", 400)
	}))
	defer srv.Close()

	ad, err := hyperliquid.NewLive(hyperliquid.Config{
		REST: srv.URL, WS: "ws://unused",
		Symbols: map[exchange.Symbol]string{"BTCUSD": "BTC"},
		HTTP:    &http.Client{Timeout: 2 * time.Second},
	}, hyperliquid.Auth{
		AccountAddress: "0x2222222222222222222222222222222222222222",
		PrivateKeyHex:  testKeyHex,
		Testnet:        true,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = ad.PlaceOrder(context.Background(), exchange.OrderRequest{
		Symbol: "BTCUSD", Kind: exchange.KindPerp, Side: exchange.SideBuy, Size: "0.00003",
	})
	if err == nil || !errors.Is(err, exchange.ErrUnknownAck) {
		t.Fatalf("want ErrUnknownAck, got %v", err)
	}
}

func TestNew_StillReadOnly(t *testing.T) {
	ad := hyperliquid.New(hyperliquid.Config{Symbols: map[exchange.Symbol]string{"BTCUSD": "BTC"}})
	if _, err := ad.PlaceOrder(context.Background(), exchange.OrderRequest{}); !errors.Is(err, exchange.ErrReadOnly) {
		t.Fatal(err)
	}
}
