package strategy

import (
	"github.com/nguyenducthinhdl/hhld/src/config"
	"github.com/nguyenducthinhdl/hhld/src/exchange"
)

// ArbConfigFrom maps application Config into CrossVenueArb settings.
func ArbConfigFrom(cfg config.Config) ArbConfig {
	return ArbConfig{
		Symbols: append([]exchange.Symbol(nil), cfg.Symbols...),
		Size:    cfg.Trading.Size,
		MinGap:  cfg.Trading.MinGap,
	}
}
