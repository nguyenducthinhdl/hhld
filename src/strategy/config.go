package strategy

import (
	"github.com/nguyenducthinhdl/hhld/src/config"
	"github.com/nguyenducthinhdl/hhld/src/exchange"
)

// ArbConfigFrom maps application Config into CrossVenueArb settings.
// Size per symbol is trading.max_size (floored to min_size).
func ArbConfigFrom(cfg config.Config) ArbConfig {
	syms := cfg.Symbols()
	by := make(map[exchange.Symbol]string, len(syms))
	gaps := make(map[exchange.Symbol]float64, len(syms))
	size := ""
	minGap := 0.0
	for i, e := range cfg.SymbolMap {
		by[e.Symbol] = cfg.EffectiveSize(e.Symbol)
		gaps[e.Symbol] = e.Trading.MinGap
		if i == 0 {
			size = e.Trading.MaxSize
			minGap = e.Trading.MinGap
		}
	}
	return ArbConfig{
		Symbols:        append([]exchange.Symbol(nil), syms...),
		Size:           size,
		SizeBySymbol:   by,
		MinGap:         minGap,
		MinGapBySymbol: gaps,
	}
}
