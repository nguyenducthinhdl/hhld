package risk_test

import (
	"context"
	"testing"

	"github.com/nguyenducthinhdl/hhld/src/risk"
	"github.com/nguyenducthinhdl/hhld/src/strategy"
)

func TestFormulaEstimator_Estimate(t *testing.T) {
	e := risk.NewFormulaEstimator()
	est, err := e.Estimate(context.Background(), strategy.Decision{TraceID: "t"}, risk.MarketView{})
	if err != nil {
		t.Fatal(err)
	}
	if est.Method != "formula" || est.WinRate != 0.5 || est.Confidence != 0.95 {
		t.Fatalf("unexpected estimate: %+v", est)
	}
}

func TestCompose_Manager(t *testing.T) {
	g := risk.NewGate(risk.DefaultParams())
	m := risk.Compose{Gate: g, Estimator: risk.NewFormulaEstimator()}
	var _ risk.Manager = m
	v, err := m.Evaluate(context.Background(), strategy.Decision{TraceID: "t"}, risk.MarketView{})
	if err != nil {
		t.Fatal(err)
	}
	if v.OK {
		t.Fatalf("empty legs should fail gate, got %+v", v)
	}
	est, err := m.Estimate(context.Background(), strategy.Decision{TraceID: "t"}, risk.MarketView{})
	if err != nil {
		t.Fatal(err)
	}
	if est.Method != "formula" {
		t.Fatalf("%+v", est)
	}
}
