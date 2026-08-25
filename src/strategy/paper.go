package strategy

import (
	"context"
	"fmt"
	"sync"

	"github.com/nguyenducthinhdl/hhld/src/exchange"
)

// Venues maps venue id → Exchange for paper (later live) execution.
type Venues map[exchange.VenueID]exchange.Exchange

// LegResult is the outcome of placing one Decision leg.
type LegResult struct {
	Index int
	Leg   Leg
	Ack   exchange.OrderAck
	Err   error
}

// PlaceDecision paper-places all legs concurrently on the matching venues.
// Partial acks may be returned when some legs succeed and others time out (1-leg failure).
func PlaceDecision(ctx context.Context, venues Venues, d Decision) ([]LegResult, error) {
	if len(d.Legs) == 0 {
		return nil, fmt.Errorf("paper: decision has no legs")
	}

	results := make([]LegResult, len(d.Legs))
	var wg sync.WaitGroup
	for i, leg := range d.Legs {
		wg.Add(1)
		go func(i int, leg Leg) {
			defer wg.Done()
			ex, ok := venues[leg.Venue]
			if !ok {
				results[i] = LegResult{
					Index: i,
					Leg:   leg,
					Err:   fmt.Errorf("paper: no exchange for venue %s", leg.Venue),
				}
				return
			}
			cloid := exchange.NewClientOrderID()
			ack, err := ex.PlaceOrder(ctx, exchange.OrderRequest{
				ClientOrderID: cloid,
				TraceID:       d.TraceID,
				HedgeID:       d.HedgeID,
				Symbol:        leg.Symbol,
				Kind:          leg.Kind,
				Side:          leg.Side,
				Price:         leg.Price,
				Size:          leg.Size,
				TIF:           exchange.TIFIOC,
			})
			results[i] = LegResult{Index: i, Leg: leg, Ack: ack, Err: err}
		}(i, leg)
	}
	wg.Wait()

	var firstErr error
	okCount := 0
	for _, r := range results {
		if r.Err != nil && firstErr == nil {
			firstErr = r.Err
		}
		if r.Err == nil {
			okCount++
		}
	}
	if firstErr != nil {
		return results, fmt.Errorf("paper: place decision %s (%d/%d legs ok): %w", d.TraceID, okCount, len(d.Legs), firstErr)
	}
	return results, nil
}
