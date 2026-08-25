package exchange

import (
	"context"
	"net/http"
	"time"
)

// WSConn is the WebSocket subset adapters need (injectable for tests).
type WSConn interface {
	ReadMessage() (messageType int, p []byte, err error)
	WriteMessage(messageType int, data []byte) error
	Close() error
}

// DialWS opens a WebSocket to url. Adapters use this for live feeds; tests inject fakes.
type DialWS func(ctx context.Context, url string, header http.Header) (WSConn, error)

// AdapterEndpoints holds REST/WS URLs for a venue.
type AdapterEndpoints struct {
	REST string
	WS   string
}

// DefaultHyperliquidMainnet returns public HL market endpoints.
func DefaultHyperliquidMainnet() AdapterEndpoints {
	return AdapterEndpoints{
		REST: "https://api.hyperliquid.xyz",
		WS:   "wss://api.hyperliquid.xyz/ws",
	}
}

// DefaultHyperliquidTestnet returns HL testnet market endpoints.
func DefaultHyperliquidTestnet() AdapterEndpoints {
	return AdapterEndpoints{
		REST: "https://api.hyperliquid-testnet.xyz",
		WS:   "wss://api.hyperliquid-testnet.xyz/ws",
	}
}

// DefaultGRVTMainnet returns public GRVT market-data endpoints (full).
func DefaultGRVTMainnet() AdapterEndpoints {
	return AdapterEndpoints{
		REST: "https://market-data.grvt.io",
		WS:   "wss://market-data.grvt.io/ws/full",
	}
}

// DefaultHTTPClient is a short-timeout client for market snapshots.
func DefaultHTTPClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Second}
}

// DefaultGRVTStaging returns GRVT staging public market-data endpoints (full).
func DefaultGRVTStaging() AdapterEndpoints {
	return AdapterEndpoints{
		REST: "https://market-data.staging.gravitymarkets.io",
		WS:   "wss://market-data.staging.gravitymarkets.io/ws/full",
	}
}

// DefaultGRVTTestnet returns GRVT testnet public market-data endpoints (full).
func DefaultGRVTTestnet() AdapterEndpoints {
	return AdapterEndpoints{
		REST: "https://market-data.testnet.grvt.io",
		WS:   "wss://market-data.testnet.grvt.io/ws/full",
	}
}

// MarketEndpoints returns public book REST/WS for env+venue. Hosts are never read from JSON.
func MarketEndpoints(env string, venue VenueID) AdapterEndpoints {
	switch env {
	case "staging":
		if venue == "hyperliquid" {
			return DefaultHyperliquidTestnet()
		}
		if venue == "grvt" {
			return DefaultGRVTStaging()
		}
	case "testnet":
		if venue == "hyperliquid" {
			return DefaultHyperliquidTestnet()
		}
		if venue == "grvt" {
			return DefaultGRVTTestnet()
		}
	}
	if venue == "hyperliquid" {
		return DefaultHyperliquidMainnet()
	}
	return DefaultGRVTMainnet()
}
