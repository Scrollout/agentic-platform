package analyst

// Tests of the creation-order check of ExampleContent when the listed texts are spread over
// several Then steps of one scenario (T199). Planted fixtures differ from the good one only in
// scenario E-2's Then steps, so each must fail for creation order and for nothing else.

import (
	"strings"
	"testing"
)

func TestContentOrderAcceptsTextsSplitOverThenSteps(t *testing.T) {
	r := newGrader(t).Content(contentFixture(t, "good-order-split-steps"))
	if !r.Passed {
		t.Fatalf("texts in creation order over two Then steps fail: %s", r.Reason)
	}
}

func TestContentOrderRejectsPlantedSplitSteps(t *testing.T) {
	g := newGrader(t)
	for _, name := range []string{
		"bad-order-split-reversed",      // second text expected first
		"bad-order-split-one-text",      // only one created text quoted; the other is created and listed in other scenarios
		"bad-order-split-repeated-text", // the same text quoted twice is not two notes
	} {
		r := g.Content(contentFixture(t, name))
		if r.Passed {
			t.Errorf("%s passes the content grader", name)
			continue
		}
		if r.Reason != "no scenario creates two notes and expects them listed in creation order" {
			t.Errorf("%s fails for %q, want only the creation-order miss", name, r.Reason)
		}
		if !strings.Contains(r.Reason, "creation order") {
			t.Errorf("%s: reason lacks %q", name, "creation order")
		}
	}
}
