package risk

import (
	"fmt"
	"strconv"

	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/src/strategy"
)

// Explain is the hover/audit breakdown of a two-leg arb: gap formula, size, fees.
type Explain struct {
	Formula     string       `json:"formula"`
	Size        float64      `json:"size"`
	SizeModeled float64      `json:"size_modeled"`
	Gross       float64      `json:"gross"`
	Fee         float64      `json:"fee"`
	Latency     float64      `json:"latency"`
	Net         float64      `json:"net"`
	Legs        []LegExplain `json:"legs,omitempty"`
}

// LegExplain is one buy or sell leg with fee/commission parts.
type LegExplain struct {
	Venue           string  `json:"venue"`
	Side            string  `json:"side"`
	Book            string  `json:"book"`
	Price           string  `json:"price"`
	Size            string  `json:"size"`
	Notional        float64 `json:"notional"`
	RateBps         float64 `json:"rate_bps"`
	RateFee         float64 `json:"rate_fee"`
	Fixed           float64 `json:"fixed"`
	CommissionBps   float64 `json:"commission_bps"`
	CommissionRate  float64 `json:"commission_rate"`
	CommissionFixed float64 `json:"commission_fixed"`
	Fee             float64 `json:"fee"`
}

// ExplainDecision models miss-more economics for a Decision (buy+sell legs).
func ExplainDecision(d strategy.Decision, p Params) (Explain, bool) {
	var buy, sell *strategy.Leg
	for i := range d.Legs {
		leg := &d.Legs[i]
		switch leg.Side {
		case exchange.SideBuy:
			if buy == nil {
				buy = leg
			}
		case exchange.SideSell:
			if sell == nil {
				sell = leg
			}
		}
	}
	if buy == nil || sell == nil {
		return Explain{}, false
	}
	return ExplainQuote(string(buy.Venue), buy.Price, string(sell.Venue), sell.Price, buy.Size, p)
}

// ExplainQuote models buy-ask / sell-bid at size using Params fee schedule and partial-fill.
func ExplainQuote(buyVenue, buyPrice, sellVenue, sellPrice, sizeStr string, p Params) (Explain, bool) {
	buyPx, err1 := strconv.ParseFloat(buyPrice, 64)
	sellPx, err2 := strconv.ParseFloat(sellPrice, 64)
	sz, err3 := strconv.ParseFloat(sizeStr, 64)
	if err1 != nil || err2 != nil || err3 != nil || sz <= 0 || buyPx <= 0 || sellPx <= 0 {
		return Explain{}, false
	}
	pff := p.PartialFillFactor
	if pff <= 0 || pff > 1 {
		pff = 1
	}
	modeled := sz * pff
	fees := p.FeeSchedule()
	buyParts := fees.Parts(exchange.VenueID(buyVenue), exchange.SideBuy, buyPx, modeled)
	sellParts := fees.Parts(exchange.VenueID(sellVenue), exchange.SideSell, sellPx, modeled)
	fee := buyParts.Total + sellParts.Total
	lat := p.LatencyPenalty * modeled
	gross := (sellPx - buyPx) * modeled
	gapPx := sellPx - buyPx
	ex := Explain{
		Formula: fmt.Sprintf(
			"gap = sell_bid(%s) - buy_ask(%s) = %s - %s = %.6g",
			sellVenue, buyVenue, sellPrice, buyPrice, gapPx,
		),
		Size:        sz,
		SizeModeled: modeled,
		Gross:       gross,
		Fee:         fee,
		Latency:     lat,
		Net:         gross - fee - lat,
		Legs: []LegExplain{
			legExplain(buyVenue, "buy", "ask", buyPrice, modeled, buyParts),
			legExplain(sellVenue, "sell", "bid", sellPrice, modeled, sellParts),
		},
	}
	return ex, true
}

func legExplain(venue, side, book, price string, size float64, parts FeeParts) LegExplain {
	px, _ := strconv.ParseFloat(price, 64)
	return LegExplain{
		Venue:           venue,
		Side:            side,
		Book:            book,
		Price:           price,
		Size:            strconv.FormatFloat(size, 'f', -1, 64),
		Notional:        px * size,
		RateBps:         parts.RateBps,
		RateFee:         parts.RateFee,
		Fixed:           parts.Fixed,
		CommissionBps:   parts.CommissionBps,
		CommissionRate:  parts.CommissionRate,
		CommissionFixed: parts.CommissionFixed,
		Fee:             parts.Total,
	}
}
