package api

import (
	"fmt"
	"maps"
	"sort"
	"strings"

	"github.com/agentic-platform/engine/api/gen"
	"github.com/agentic-platform/engine/captured"
	"github.com/agentic-platform/engine/machine"
	sl "github.com/agentic-platform/engine/slices"
)

// Which pattern a run uses (feature 011, research R6; data model "States", "Runs that continue"):
// at startRun the newest approved usable captured pattern that fits the slice, unless the request
// names a pattern; at replayRun the captured pattern the earlier run used, while it is usable. The
// fit itself is package engine/captured's; this file judges the states and builds the start.

// usable reports whether r may be used by a run that is not a proof: approved, and its latest
// proof passed under the method m with the current parameters (data model, "usable"). The caller
// has completed r's proofs first (completeProofs), as every reader of a proof's state does.
func (r *capturedRecord) usable(m string) bool {
	return r.State == gen.CapturedPatternStateApproved && r.provenUnder(m)
}

// needsProof says in words why an approved r is not usable under the method m.
func (r *capturedRecord) needsProof(m string) string {
	p := r.latest()
	switch {
	case p == nil:
		return "it has no proof"
	case p.Outcome == gen.ProofOutcomeRunning:
		return fmt.Sprintf("its latest proof, run %s, has not ended", p.Run)
	case p.Outcome != gen.ProofOutcomePassed:
		return fmt.Sprintf("its latest proof, run %s, failed", p.Run)
	case p.Method != m:
		return fmt.Sprintf("its latest proof, run %s, passed under the method %s, not the engine's current method %s", p.Run, p.Method, m)
	}
	return fmt.Sprintf("its latest proof, run %s, proved other parameter values than its current ones", p.Run)
}

// namedRefusal refuses a run of a slice other than a proof slice that names the captured pattern
// r, whose proofs are complete, in the contract's order: CapturedPatternWithdrawn, then
// CapturedPatternNotApproved while it is proposed or proven, then ProofRequired while it is
// approved and not usable under the method m. nil for a usable one.
func namedRefusal(r *capturedRecord, m string) error {
	switch r.State {
	case gen.CapturedPatternStateWithdrawn:
		return stateRefusal(r)
	case gen.CapturedPatternStateProposed, gen.CapturedPatternStateProven:
		return &sl.Error{Code: string(gen.ErrorCodeCapturedPatternNotApproved),
			Detail: fmt.Sprintf("captured pattern %s is %s, not approved", r.ID, r.State)}
	}
	if !r.usable(m) {
		return &sl.Error{Code: string(gen.ErrorCodeProofRequired),
			Detail: fmt.Sprintf("captured pattern %s needs a new proof: %s", r.ID, r.needsProof(m))}
	}
	return nil
}

// capturedRun is the start of a run of the captured pattern r that is not a proof: its
// declaration, the reason, the values its `fill` steps fill in (an empty map for a captured
// pattern with no parameter) and its source run.
func capturedRun(r *capturedRecord, reason gen.PatternReason, values map[string]string) (RunStart, error) {
	p, err := r.pattern()
	if err != nil {
		return RunStart{}, err
	}
	if values == nil {
		values = map[string]string{}
	}
	return RunStart{Definition: p, PatternReason: string(reason), Parameters: maps.Clone(values), SourceRun: r.SourceRun,
		Fill: &RunFill{Captured: r.ID, Template: maps.Clone(r.Template), Values: maps.Clone(values)}}, nil
}

// fit judges whether the approved parts of slice fit the captured pattern r (captured.Fit, with
// each filled output's step's writable entries). An unreadable template is the engine's fault: an
// error, so nothing is filled from it.
func (s *sliceServer) fit(r *capturedRecord, approved captured.Copy) (map[string]string, bool, captured.Refusal, error) {
	tmpl, err := s.templateFiles(r.Template)
	if err != nil {
		return nil, false, captured.Refusal{}, fmt.Errorf("captured pattern %s: %w", r.ID, err)
	}
	p, err := r.pattern()
	if err != nil {
		return nil, false, captured.Refusal{}, err
	}
	writable := map[string][]string{}
	for _, st := range p.Steps {
		if st.Worker == fillWorker {
			for _, o := range st.Outputs {
				writable[o] = st.Writable
			}
		}
	}
	values, ok, ref := captured.Fit(r.src(), tmpl, writable, r.Parameters, approved)
	return values, ok, ref, nil
}

// approvedCopy is the slice's approved parts as a captured pattern's fit reads them.
func (s *sliceServer) approvedCopy(slice string) (captured.Copy, error) {
	parts, err := s.svc.ApprovedParts(slice)
	if err != nil {
		return captured.Copy{}, err
	}
	return captured.Copy{Goals: parts["goals"], Rules: parts["rules"], Contract: parts["contract"]}, nil
}

// onlyValue reports whether ref refuses a value and nothing else.
func onlyValue(ref captured.Refusal) bool {
	return ref.Parameter != nil && ref.Place == nil && len(ref.Paths) == 0
}

// namedStart is the start of a run of slice naming the captured pattern named (not its proof
// slice), refused as namedRefusal does, then PatternDoesNotFit naming the first place the slice
// differs, the refused value or the paths. It judges the record as it is under cmu, never the copy
// named was read from, so a withdrawal or an erasure that completed since is seen (NotFound when
// erased) and never overwritten.
func (s *sliceServer) namedStart(slice string, named *capturedRecord) (RunStart, error) {
	s.cmu.Lock()
	defer s.cmu.Unlock()
	r, err := s.readCaptured(named.ID)
	if err != nil {
		return RunStart{}, err
	}
	m, err := currentMethod()
	if err != nil {
		return RunStart{}, err
	}
	if err := s.completeAndSave(r, m); err != nil {
		return RunStart{}, err
	}
	if err := namedRefusal(r, m); err != nil {
		return RunStart{}, err
	}
	approved, err := s.approvedCopy(slice)
	if err != nil {
		return RunStart{}, err
	}
	values, ok, ref, err := s.fit(r, approved)
	switch {
	case err != nil:
		return RunStart{}, err
	case !ok:
		return RunStart{}, &sl.Error{Code: string(gen.ErrorCodePatternDoesNotFit),
			Detail: fmt.Sprintf("slice %s does not fit the captured pattern %s: %s", slice, r.ID, ref.Detail)}
	}
	return capturedRun(r, gen.PatternReasonChosen, values)
}

// chosenStart is the pattern choice of a run of slice that names no pattern: every approved
// captured pattern is judged, the most recently approved first (of two approved at the same
// instant, the later captured); the first that fits and is usable is used (`fits`), otherwise the
// base pattern (`base`). Each judged one that fits but is not usable is named `needs-proof`, and
// each refused only for a value `refused-value`, in the order judged; a proposed, proven or
// withdrawn one is not judged.
func (s *sliceServer) chosenStart(slice string) (RunStart, error) {
	base := RunStart{PatternReason: string(gen.PatternReasonBase)}
	if s.captured == nil || s.runs == nil {
		return base, nil
	}
	s.cmu.Lock()
	defer s.cmu.Unlock()
	all, err := s.allCaptured()
	if err != nil {
		return RunStart{}, err
	}
	var judged []*capturedRecord
	for _, r := range all {
		if r.State == gen.CapturedPatternStateApproved && r.Approval != nil {
			judged = append(judged, r)
		}
	}
	if len(judged) == 0 {
		return base, nil
	}
	sort.SliceStable(judged, func(i, j int) bool {
		a, b := judged[i].Approval.At, judged[j].Approval.At
		if !a.Equal(b) {
			return a.After(b)
		}
		return judged[i].Number > judged[j].Number
	})
	m, err := currentMethod()
	if err != nil {
		return RunStart{}, err
	}
	approved, err := s.approvedCopy(slice)
	if err != nil {
		return RunStart{}, err
	}
	var chosen *capturedRecord
	var chosenValues map[string]string
	var notUsed []machine.NotUsed
	for _, r := range judged {
		if err := s.completeAndSave(r, m); err != nil {
			return RunStart{}, err
		}
		values, ok, ref, err := s.fit(r, approved)
		switch {
		case err != nil:
			return RunStart{}, err
		case !ok && onlyValue(ref):
			notUsed = append(notUsed, machine.NotUsed{Pattern: r.ID, Reason: string(gen.NotUsedReasonRefusedValue),
				Parameter: ref.Parameter.Parameter, Detail: ref.Parameter.Detail})
		case !ok:
		case !r.usable(m):
			notUsed = append(notUsed, machine.NotUsed{Pattern: r.ID, Reason: string(gen.NotUsedReasonNeedsProof), Detail: r.needsProof(m)})
		case chosen == nil:
			chosen, chosenValues = r, values
		}
	}
	if chosen == nil {
		base.NotUsed = notUsed
		return base, nil
	}
	start, err := capturedRun(chosen, gen.PatternReasonFits, chosenValues)
	start.NotUsed = notUsed
	return start, err
}

// capturedReplay is the pattern choice of a replay of a run of a captured pattern that is not a
// proof, whose RunStarted is started: the same captured pattern with the values the run recorded,
// no new fit, `chosen`; NotFound when it was erased, CapturedPatternWithdrawn, and ProofRequired
// while it is not usable.
func (s *sliceServer) capturedReplay(started machine.Event) (RunStart, error) {
	if _, err := s.capturedStore(); err != nil {
		return RunStart{}, err
	}
	id, version, _ := strings.Cut(started.Pattern, "@")
	if version != capturedVersion {
		return RunStart{}, fmt.Errorf("run of %s names version %s, captured patterns have %s", id, version, capturedVersion)
	}
	s.cmu.Lock()
	defer s.cmu.Unlock()
	r, err := s.readCaptured(id)
	if err != nil {
		return RunStart{}, err
	}
	m, err := currentMethod()
	if err != nil {
		return RunStart{}, err
	}
	if err := s.completeAndSave(r, m); err != nil {
		return RunStart{}, err
	}
	if err := namedRefusal(r, m); err != nil {
		return RunStart{}, err
	}
	return capturedRun(r, gen.PatternReasonChosen, started.Parameters)
}
