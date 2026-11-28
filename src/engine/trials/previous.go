package trials

// SetSummary is what the comparison reads of one set of a slice.
type SetSummary struct {
	Number      int    // n of TS-<n>; "earlier" means a lower Number
	ID          string // "TS-<n>"
	State       string // the set's state as shown: "running", "ended", "stopped", "cancelled", "failed"
	Method      string // the set's `method`, recorded at acceptance
	Done, Ended int    // the set's Result.Done and Result.Ended (Of over its trials)
}

// Comparison is a set against the slice's previous set.
type Comparison struct {
	TrialSet      string  // the previous set's ID
	Method        string  // the previous set's method
	Done, Ended   int     // the previous set's counts
	ShareChange   float64 // set.Done/set.Ended - prev.Done/prev.Ended, in [-1, 1]
	LessReliable  bool    // exactly when ShareChange < 0
	MethodsDiffer bool    // set.Method != prev.Method
}

// comparable says whether a set can take part in a comparison: it ended or was stopped, with at
// least one ended trial.
func (s SetSummary) comparable() bool {
	return (s.State == "ended" || s.State == "stopped") && s.Ended >= 1
}

// share is the set's share of done trials among its ended ones; the set is comparable.
func (s SetSummary) share() float64 { return float64(s.Done) / float64(s.Ended) }

// Previous compares set with the slice's previous set: the one of others with the highest Number
// below set.Number whose State is "ended" or "stopped" and whose Ended >= 1. It is nil when set's
// own State is neither "ended" nor "stopped", when set.Ended is 0, or when no such set exists.
// others may be in any order and may include set itself and later sets (both ignored).
func Previous(set SetSummary, others []SetSummary) *Comparison {
	if !set.comparable() {
		return nil
	}
	var prev *SetSummary
	for i := range others {
		o := &others[i]
		if o.Number < set.Number && o.comparable() && (prev == nil || o.Number > prev.Number) {
			prev = o
		}
	}
	if prev == nil {
		return nil
	}
	// Equal fractions give equal correctly rounded quotients, so an equal share is exactly 0.
	change := set.share() - prev.share()
	return &Comparison{TrialSet: prev.ID, Method: prev.Method, Done: prev.Done, Ended: prev.Ended,
		ShareChange: change, LessReliable: change < 0, MethodsDiffer: set.Method != prev.Method}
}
