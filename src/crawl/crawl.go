package crawl

import (
	"context"

	"github.com/nguyenducthinhdl/hhld/src/warehouse"
)

// Crawler normalizes market data into a warehouse.Store.
// Implementations are stubs until live HL/GRVT adapters (P8) replace sample sources.
type Crawler interface {
	Run(ctx context.Context, store warehouse.Store) error
}
