package tests

import "testing"

// Two subtests, as many as the feature has scenarios, with names unlike theirs; the second fails.
func TestTable(t *testing.T) {
	for _, c := range []struct {
		name string
		ok   bool
	}{{"adds", true}, {"subtracts", false}} {
		t.Run(c.name, func(t *testing.T) {
			if !c.ok {
				t.Error("planted failure")
			}
		})
	}
}
