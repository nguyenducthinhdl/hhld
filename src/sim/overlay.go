package sim

import (
	"time"

	"github.com/nguyenducthinhdl/hhld/src/config"
	"github.com/nguyenducthinhdl/hhld/src/exchange"
)

// Overlay is an in-memory config patch from POST /sim/run (does not write files).
type Overlay struct {
	VenueA            string                     `json:"venue_a"`
	VenueB            string                     `json:"venue_b"`
	Symbol            string                     `json:"symbol"`
	Size              string                     `json:"size"` // unused; prefer min_size / max_size
	MinSize           string                     `json:"min_size"`
	MaxSize           string                     `json:"max_size"`
	MinGap            *float64                   `json:"min_gap"`
	FeeBpsPerLeg      *float64                   `json:"fee_bps_per_leg"`
	LatencyPenalty    *float64                   `json:"latency_penalty"`
	PartialFillFactor *float64                   `json:"partial_fill_factor"`
	MaxBookAge        string                     `json:"max_book_age"`
	MaxInFlight       *int                       `json:"max_in_flight"`
	FeesByVenue       map[string]config.VenueFee `json:"fees_by_venue"`
}

// ApplyOverlay copies base and applies overlay knobs to the selected symbol row.
func ApplyOverlay(base config.Config, o Overlay) config.Config {
	cfg := base.Clone()
	if o.VenueA != "" {
		cfg.Venues.A = exchange.VenueID(o.VenueA)
	}
	if o.VenueB != "" {
		cfg.Venues.B = exchange.VenueID(o.VenueB)
	}
	sym := exchange.Symbol(o.Symbol)
	if o.Symbol != "" {
		cfg.UpdateSymbol(sym, func(*config.SymbolEntry) {})
		next := make([]config.SymbolEntry, 0, 1)
		for _, e := range cfg.SymbolMap {
			if e.Symbol == sym {
				next = append(next, e)
			}
		}
		if len(next) > 0 {
			cfg.SymbolMap = next
		}
	}
	cfg.UpdateSymbol(sym, func(e *config.SymbolEntry) {
		if o.MinSize != "" {
			e.Trading.MinSize = o.MinSize
		}
		if o.MaxSize != "" {
			e.Trading.MaxSize = o.MaxSize
		}
		if o.Size != "" && o.MaxSize == "" {
			e.Trading.MaxSize = o.Size
		}
		if o.MinGap != nil {
			e.Trading.MinGap = *o.MinGap
		}
		if o.FeeBpsPerLeg != nil {
			e.Risk.FeeBpsPerLeg = *o.FeeBpsPerLeg
		}
		if o.LatencyPenalty != nil {
			e.Risk.LatencyPenalty = *o.LatencyPenalty
		}
		if o.PartialFillFactor != nil {
			e.Risk.PartialFillFactor = *o.PartialFillFactor
		}
		if o.MaxBookAge != "" {
			if d, err := time.ParseDuration(o.MaxBookAge); err == nil {
				e.Risk.MaxBookAge = config.Duration(d)
			}
		}
		if o.FeesByVenue != nil {
			if e.Venues == nil {
				e.Venues = map[string]config.VenueSpec{}
			}
			for k, v := range o.FeesByVenue {
				spec := e.Venues[k]
				spec.Fees = v
				e.Venues[k] = spec
			}
		}
	})
	if o.MaxInFlight != nil {
		cfg.Risk.MaxInFlight = *o.MaxInFlight
	}
	return cfg
}
