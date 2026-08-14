package risk

import (
	"fmt"

	"github.com/nguyenducthinhdl/hhld/src/exchange"
)

// SideFee is one order side's trading-cost model (miss-more / paper fills).
// Components are additive so exchanges that mix rate + flat + commission are covered:
//
//	total = notional*(RateBps+CommissionBps)/10_000 + Fixed + CommissionFixed
//
// Use only the fields that apply (others stay 0). Examples:
//   - rate only:     {RateBps: 5}
//   - fixed only:    {Fixed: 0.10}
//   - rate + commission: {RateBps: 3.5, CommissionFixed: 0.01}
type SideFee struct {
	// RateBps is venue trading fee in basis points of notional (price*size).
	RateBps float64
	// Fixed is a flat venue trading fee per fill/leg (quote currency).
	Fixed float64
	// CommissionBps is broker/referral commission in basis points of notional.
	CommissionBps float64
	// CommissionFixed is a flat commission per fill/leg.
	CommissionFixed float64
}

// VenueFee is per-side fees for one venue (buy vs sell may differ).
type VenueFee struct {
	Buy  SideFee
	Sell SideFee
}

// Both copies this side schedule onto buy and sell.
func (f SideFee) Both() VenueFee {
	return VenueFee{Buy: f, Sell: f}
}

// For returns the schedule for side (sell vs buy).
func (f VenueFee) For(side exchange.Side) SideFee {
	if side == exchange.SideSell {
		return f.Sell
	}
	return f.Buy
}

// FeeSchedule resolves per-venue, per-side fees; unknown venues fall back to DefaultRateBps.
type FeeSchedule struct {
	// DefaultRateBps applies when a venue has no entry in ByVenue (legacy fee_bps_per_leg).
	DefaultRateBps float64
	// ByVenue maps exchange id → buy/sell fee model.
	ByVenue map[exchange.VenueID]VenueFee
}

// Cost returns the modeled fee for one leg on venue/side at price/size.
func (s FeeSchedule) Cost(venue exchange.VenueID, side exchange.Side, price, size float64) float64 {
	if price <= 0 || size <= 0 {
		return 0
	}
	fee, ok := s.ByVenue[venue]
	if !ok {
		return rateFee(price, size, s.DefaultRateBps)
	}
	return fee.For(side).Cost(price, size)
}

// Cost returns total fee for one fill given price and size.
func (f SideFee) Cost(price, size float64) float64 {
	return f.Parts(price, size).Total
}

// FeeParts is the additive breakdown of one venue fill.
type FeeParts struct {
	RateBps         float64
	CommissionBps   float64
	RateFee         float64
	Fixed           float64
	CommissionRate  float64
	CommissionFixed float64
	Total           float64
}

// Parts splits Cost into rate / fixed / commission components.
func (f SideFee) Parts(price, size float64) FeeParts {
	p := FeeParts{RateBps: f.RateBps, CommissionBps: f.CommissionBps}
	if price <= 0 || size <= 0 {
		return p
	}
	p.RateFee = rateFee(price, size, f.RateBps)
	p.Fixed = f.Fixed
	p.CommissionRate = rateFee(price, size, f.CommissionBps)
	p.CommissionFixed = f.CommissionFixed
	p.Total = p.RateFee + p.Fixed + p.CommissionRate + p.CommissionFixed
	return p
}

// Parts resolves the venue/side schedule (or DefaultRateBps) and splits the fill cost.
func (s FeeSchedule) Parts(venue exchange.VenueID, side exchange.Side, price, size float64) FeeParts {
	fee, ok := s.ByVenue[venue]
	if !ok {
		return SideFee{RateBps: s.DefaultRateBps}.Parts(price, size)
	}
	return fee.For(side).Parts(price, size)
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
