package hyperliquid

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
)

// metaCache maps perp coin → asset index from POST /info {type:meta}.
type metaCache struct {
	mu    sync.Mutex
	byCoin map[string]int
}

func (m *metaCache) assetIndex(ctx context.Context, httpClient *http.Client, rest, coin string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.byCoin == nil {
		idx, err := fetchMeta(ctx, httpClient, rest)
		if err != nil {
			return 0, err
		}
		m.byCoin = idx
	}
	i, ok := m.byCoin[coin]
	if !ok {
		return 0, fmt.Errorf("hyperliquid: coin %s not in meta universe", coin)
	}
	return i, nil
}

func (m *metaCache) invalidate() {
	m.mu.Lock()
	m.byCoin = nil
	m.mu.Unlock()
}

type metaResp struct {
	Universe []struct {
		Name string `json:"name"`
	} `json:"universe"`
}

func fetchMeta(ctx context.Context, httpClient *http.Client, rest string) (map[string]int, error) {
	body, _ := json.Marshal(map[string]any{"type": "meta"})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rest+"/info", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("hyperliquid: meta: %w", err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("hyperliquid: meta status %d: %s", res.StatusCode, raw)
	}
	var mr metaResp
	if err := json.Unmarshal(raw, &mr); err != nil {
		return nil, fmt.Errorf("hyperliquid: meta json: %w", err)
	}
	out := make(map[string]int, len(mr.Universe))
	for i, u := range mr.Universe {
		if u.Name != "" {
			out[u.Name] = i
		}
	}
	return out, nil
}
