package exchange_test

import (
	"testing"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/exchange"
)

func TestManualClock_Deterministic(t *testing.T) {
	start := time.Unix(1_700_000_000, 0).UTC()
	c := exchange.NewManualClock(start)
	if !c.Now().Equal(start) {
		t.Fatalf("now=%v want %v", c.Now(), start)
	}
	c.Advance(2 * time.Second)
	want := start.Add(2 * time.Second)
	if !c.Now().Equal(want) {
		t.Fatalf("after advance now=%v want %v", c.Now(), want)
	}
	c.Set(start)
	if !c.Now().Equal(start) {
		t.Fatalf("after set now=%v want %v", c.Now(), start)
	}
}
