package risk

import (
	"context"

	"github.com/nguyenducthinhdl/hhld/src/strategy"
)

// FormulaEstimator is a placeholder detective estimator (neutral priors).
// Replace with historical-calibrated or ML estimators behind the same interface.
type FormulaEstimator struct {
	DefaultWinRate float64
	DefaultVaR     float64
	Confidence     float64
}

// NewFormulaEstimator returns conservative defaults when priors are unset.
func NewFormulaEstimator() *FormulaEstimator {
	return &FormulaEstimator{
		DefaultWinRate: 0.5,
		DefaultVaR:     0,
		Confidence:     0.95,
	}
}

func (e *FormulaEstimator) Estimate(ctx context.Context, d strategy.Decision, mkt MarketView) (Estimate, error) {
	if err := ctx.Err(); err != nil {
		return Estimate{}, err
	}
	_ = d
	_ = mkt
	wr := e.DefaultWinRate
	if wr <= 0 || wr > 1 {
		wr = 0.5
	}
	conf := e.Confidence
	if conf <= 0 || conf > 1 {
		conf = 0.95
	}
	return Estimate{
		WinRate:    wr,
		VaR:        e.DefaultVaR,
		Confidence: conf,
		Method:     "formula",
		Reason:     "neutral prior; calibrate from sim/history or ML later",
	}, nil
}

// Ensure FormulaEstimator implements Estimator.
var _ Estimator = (*FormulaEstimator)(nil)

// Compose builds a Manager from a hard gate and an estimator.
type Compose struct {
	Gate      *Gate
	Estimator Estimator
}

func (c Compose) Evaluate(ctx context.Context, d strategy.Decision, mkt MarketView) (Verdict, error) {
	return c.Gate.Evaluate(ctx, d, mkt)
}

func (c Compose) Estimate(ctx context.Context, d strategy.Decision, mkt MarketView) (Estimate, error) {
	if c.Estimator == nil {
		return NewFormulaEstimator().Estimate(ctx, d, mkt)
	}
	return c.Estimator.Estimate(ctx, d, mkt)
}

// Ensure Compose implements Manager.
var _ Manager = Compose{}
