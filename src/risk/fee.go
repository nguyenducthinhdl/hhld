package risk

import (
	"fmt"

	"github.com/nguyenducthinhdl/hhld/src/exchange"
)

// VenueFee is one venue's trading-cost model (miss-more / paper fills).
// Components are additive so exchanges that mix rate + flat + commission are covered:
//
//	total = notional*(RateBps+CommissionBps)/10_000 + Fixed + CommissionFixed
//
// Use only the fields that apply (others stay 0). Examples:
//   - rate only:     {RateBps: 5}
//   - fixed only:    {Fixed: 0.10}
//   - rate + commission: {RateBps: 3.5, CommissionFixed: 0.01}
type VenueFee struct {
	// RateBps is venue trading fee in basis points of notional (price*size).
	RateBps float64
	// Fixed is a flat venue trading fee per fill/leg (quote currency).
	Fixed float64
	// CommissionBps is broker/referral commission in basis points of notional.
	CommissionBps float64
	// CommissionFixed is a flat commission per fill/leg.
	CommissionFixed float64
}

// FeeSchedule resolves per-venue fees; unknown venues fall back to DefaultRateBps.
type FeeSchedule struct {
	// DefaultRateBps applies when a venue has no entry in ByVenue (legacy fee_bps_per_leg).
	DefaultRateBps float64
	// ByVenue maps exchange id → fee model.
	ByVenue map[exchange.VenueID]VenueFee
}

// Cost returns the modeled fee for one leg on venue at price/size.
func (s FeeSchedule) Cost(venue exchange.VenueID, price, size float64) float64 {
	if price <= 0 || size <= 0 {
		return 0
	}
	fee, ok := s.ByVenue[venue]
	if !ok {
		return rateFee(price, size, s.DefaultRateBps)
	}
	return fee.Cost(price, size)
}

// Cost returns total fee for one fill given price and size.
func (f VenueFee) Cost(price, size float64) float64 {
	if price <= 0 || size <= 0 {
		return 0
	}
	return rateFee(price, size, f.RateBps) +
		f.Fixed +
		rateFee(price, size, f.CommissionBps) +
		f.CommissionFixed
}

func rateFee(price, size, bps float64) float64 {
	if bps <= 0 {
		return 0
	}
	return price * size * (bps / 10_000)
}

// LegFee is the rate-only helper (DefaultRateBps path). Prefer FeeSchedule.Cost for venue fees.
func LegFee(price, size, feeBpsPerLeg float64) float64 {
	return rateFee(price, size, feeBpsPerLeg)
}

// FormatFee formats a fee amount as a decimal string for exchange.Fill.Fee.
func FormatFee(fee float64) string {
	return fmt.Sprintf("%.8f", fee)
}
