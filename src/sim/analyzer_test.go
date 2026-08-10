package sim_test

import (
	"context"
	"testing"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/src/risk"
	"github.com/nguyenducthinhdl/hhld/src/sim"
	"github.com/nguyenducthinhdl/hhld/src/strategy"
)

// Compile-time check: stubs must satisfy Analyzer so research/side-project or alternate
// empirical implementations can swap without changing callers (spec/tech-stack.md).
var _ sim.Analyzer = (*stubAnalyzer)(nil)

type stubAnalyzer struct{}

func (stubAnalyzer) WinningRate(ctx context.Context, in sim.Input, s strategy.Strategy, r risk.Risk, f sim.Filter) (float64, error) {
	return 0.6, nil
}

func (stubAnalyzer) WinningDistribution(ctx context.Context, in sim.Input, s strategy.Strategy, r risk.Risk, f sim.Filter) (sim.Distribution, error) {
	now := time.Unix(1, 0).UTC()
	// Dims match the constitution’s distribution keys (spec/trading.md, spec/roadmap/p6.md).
	samples := []sim.WinSample{{
		Dims: sim.OutcomeDims{
			Symbol: "BTCUSD", Gap: 0.7, Volume1: "1", Volume2: "1",
			Exchange1: "hyperliquid", Exchange2: "grvt", Time: now,
		},
		Won: true, PnL: "0.7",
	}}
	return sim.Distribution{Samples: samples, WinRate: 1}, nil
}

// TestAnalyzer_Contract locks the Analyzer API shape used by Risk calibration.
//
// Why: Before Replay existed, this guarded the interface so WinningRate /
// WinningDistribution stay swappable (formula stats here; ML in a research side
// project — spec/tech-stack.md “Research (side project)”). OutcomeDims must carry
// symbol, gap, volumes, exchanges, time for distribution analysis.
func TestAnalyzer_Contract(t *testing.T) {
	a := stubAnalyzer{}
	rate, err := a.WinningRate(context.Background(), sim.Input{}, nil, nil, sim.Filter{Symbol: "BTCUSD"})
	if err != nil || rate != 0.6 {
		t.Fatalf("rate=%v err=%v", rate, err)
	}
	dist, err := a.WinningDistribution(context.Background(), sim.Input{}, nil, nil, sim.Filter{})
	if err != nil || len(dist.Samples) != 1 || dist.Samples[0].Dims.Exchange1 != "hyperliquid" {
		t.Fatalf("%+v err=%v", dist, err)
	}
	_ = exchange.KindPerp
}
