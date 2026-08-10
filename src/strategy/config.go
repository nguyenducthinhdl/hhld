package strategy

import (
	"github.com/nguyenducthinhdl/hhld/src/config"
	"github.com/nguyenducthinhdl/hhld/src/exchange"
)

// ArbConfigFrom maps application Config into CrossVenueArb settings.
// Size per symbol is min(trading.size, symbol_map.max_volume_trade).
func ArbConfigFrom(cfg config.Config) ArbConfig {
	by := make(map[exchange.Symbol]string, len(cfg.Symbols))
	for _, s := range cfg.Symbols {
		by[s] = cfg.EffectiveSize(s)
	}
	// Also cover symbol_map keys not listed in symbols (defensive).
	for sym := range cfg.SymbolMap {
		s := exchange.Symbol(sym)
		if _, ok := by[s]; !ok {
			by[s] = cfg.EffectiveSize(s)
		}
	}
	return ArbConfig{
		Symbols:      append([]exchange.Symbol(nil), cfg.Symbols...),
		Size:         cfg.Trading.Size,
		SizeBySymbol: by,
		MinGap:       cfg.Trading.MinGap,
	}
}
