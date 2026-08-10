package sim

import (
	"context"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/src/warehouse"
)

// InputFromStore loads market data from a warehouse Store for replay (P7 → P6).
func InputFromStore(ctx context.Context, st warehouse.Store, symbol exchange.Symbol, from, to time.Time) (Input, error) {
	books, err := st.QueryBooks(ctx, symbol, from, to)
	if err != nil {
		return Input{}, err
	}
	ticks, err := st.QueryTicks(ctx, symbol, from, to)
	if err != nil {
		return Input{}, err
	}
	return Input{Books: books, Ticks: ticks}, nil
}
