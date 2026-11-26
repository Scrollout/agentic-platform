package slices_test

// T048 (feature 011, US2; data model "Proof slice", spec Clarifications 2026-11-26 about the
// hand-changed proof slice; contract makeProofSlice and the `changed` kind of slice.schema.json):
// ProofSlice restores a proof slice's hand-changed part from the copy. Written before the code
// (T049) by a test author; the code author must not change it (ADR 0029).
//
// A proof slice is changed by hand the only way a developer can: the slices service's Replace*
// calls on it. Busy is the API's to judge (the service does not know runs), so it is tested in
// captured_proof_slice_restore_test.go.
//
// Edge cases checked: one part changed (goals, rules, contract each); two parts changed, one
// `changed` entry naming both; the restored slice's parts equal the copy by hash and by
// ApprovedParts; the same slice ID and no new slice; history order (`changed`, then `approved`
// per unapproved part in part order, a `withdrawn` entry staying before them); an unchanged
// approved slice (no entry, nothing changed); a part changed back to the copy but unapproved
// (approved, no `changed`); a part changed by hand and then a method change that withdrew
// approvals; a restore whose check fails only for the copy (PartFailsChecks, the slice as it
// was, then a passing ask restores); asking twice after a restore (the second changes nothing).
//
// Left out, and why: a copy that is not three canonical parts (existing behaviour, not changed
// by T048); two captured patterns' proof slices (proofSliceLocked selects by proof_of, covered
// through the API by T027); concurrency of two restores (the slice's changing mutex is the
// existing seam, and the API serializes operations per captured pattern).

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	islices "github.com/agentic-platform/engine/slices"
)

// prLint passes every contract except one that contains failOn, which it fails with an error.
type prLint struct {
	mu     sync.Mutex
	failOn string
}

func (l *prLint) Lint(_ context.Context, contract string) ([]islices.LintFinding, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.failOn != "" && strings.Contains(contract, l.failOn) {
		return []islices.LintFinding{{Level: "error", Code: "operation-description", Text: "invalid"}}, nil
	}
	return nil, nil
}

func (l *prLint) setFailOn(s string) { l.mu.Lock(); l.failOn = s; l.mu.Unlock() }

const prOf = "CP-1"

// prRig is a service holding an approved source slice S-1 and its proof slice S-2.
type prRig struct {
	t     *testing.T
	svc   *islices.Service
	lint  *prLint
	rec   islices.Records
	parts map[string][]byte // the copy
	ps    islices.Slice     // the proof slice as made
}

func newPRRig(t *testing.T) *prRig {
	t.Helper()
	rec := newCmRecords()
	lint := &prLint{}
	a := &fakeAnalyst{results: []analystResult{{p: notesProposal(t)}}}
	svc := cmOpen(t, rec, cmID(t, "a"), apiPattern(t), a, &cmDiff{}, lint)
	src := approveAll(t, svc, create(t, svc, cmDesc))
	parts, err := svc.ApprovedParts(src.ID)
	if err != nil {
		t.Fatal(err)
	}
	r := &prRig{t: t, svc: svc, lint: lint, rec: rec, parts: parts}
	ps, err := svc.ProofSlice(context.Background(), prOf, parts)
	if err != nil {
		t.Fatalf("ProofSlice (made): %v", err)
	}
	if ps.State != "approved" {
		t.Fatalf("the proof slice is %q as made, want approved", ps.State)
	}
	r.ps = ps
	return r
}

// change replaces a part of the proof slice by hand.
func (r *prRig) change(part islices.Part) islices.Slice {
	r.t.Helper()
	ctx := context.Background()
	sl, err := r.svc.Get(r.ps.ID)
	if err != nil {
		r.t.Fatal(err)
	}
	switch part {
	case islices.PartGoals:
		g := slices.Clone(sl.Goals)
		g[0].Text += " and keep it for good"
		sl, err = r.svc.ReplaceGoals(ctx, r.ps.ID, g)
	case islices.PartRules:
		rules := slices.Clone(sl.Rules)
		rules[0].Response += " and confirm it"
		sl, err = r.svc.ReplaceRules(ctx, r.ps.ID, islices.RulesPart{Rules: rules, Examples: sl.Examples, Terms: sl.Terms})
	case islices.PartContract:
		sl, err = r.svc.ReplaceContract(ctx, r.ps.ID, strings.Replace(sl.Contract, "listNotes", "listAllNotes", 1))
	}
	if err != nil {
		r.t.Fatalf("hand change of %s: %v", part, err)
	}
	if partHash(sl, part) == partHash(r.ps, part) {
		r.t.Fatalf("the hand change of %s left the part equal to the copy", part)
	}
	return sl
}

func partHash(sl islices.Slice, part islices.Part) string {
	switch part {
	case islices.PartGoals:
		return sl.Parts.Goals
	case islices.PartRules:
		return sl.Parts.Rules
	}
	return sl.Parts.Contract
}

// ask is ProofSlice of the same captured pattern with the same copy.
func (r *prRig) ask() (islices.Slice, error) {
	return r.svc.ProofSlice(context.Background(), prOf, r.parts)
}

func (r *prRig) askOK() islices.Slice {
	r.t.Helper()
	sl, err := r.ask()
	if err != nil {
		r.t.Fatalf("ProofSlice: %v", err)
	}
	return sl
}

func (r *prRig) equalsTheCopy(sl islices.Slice) {
	r.t.Helper()
	if sl.ID != r.ps.ID {
		r.t.Errorf("slice %s, want the same slice %s", sl.ID, r.ps.ID)
	}
	if sl.Parts != r.ps.Parts {
		r.t.Errorf("parts %+v, want the copy's %+v", sl.Parts, r.ps.Parts)
	}
	for name, b := range r.parts {
		if want := sha(string(b)); partHash(sl, islices.Part(name)) != want {
			r.t.Errorf("part %s hash %s, want the copy's %s", name, partHash(sl, islices.Part(name)), want)
		}
	}
	if sl.State != "approved" || len(sl.Approvals) != 3 {
		r.t.Errorf("state %q with %d approvals, want approved with 3", sl.State, len(sl.Approvals))
	}
	if got, err := r.svc.ApprovedParts(sl.ID); err != nil {
		r.t.Errorf("ApprovedParts: %v", err)
	} else if !reflect.DeepEqual(got, r.parts) {
		r.t.Errorf("ApprovedParts differ from the copy")
	}
	if n := len(r.svc.List()); n != 2 {
		r.t.Errorf("%d slices, want S-1 and the proof slice", n)
	}
}

// sameRecord checks that two answers hold the same slice: its ID, parts, approvals, state and
// history (the check results are rerun by each ask, and their reasons may be worded differently).
func sameRecord(t *testing.T, what string, got, want islices.Slice) {
	t.Helper()
	if got.ID != want.ID || got.Parts != want.Parts || got.State != want.State ||
		!reflect.DeepEqual(got.Approvals, want.Approvals) || !reflect.DeepEqual(got.History, want.History) {
		t.Errorf("%s changed the slice:\n got %+v %+v %q %+v\nwant %+v %+v %q %+v", what,
			got.Parts, got.Approvals, got.State, got.History, want.Parts, want.Approvals, want.State, want.History)
	}
}

// wantEntries checks the history entries a restore added after the first n: a `changed` entry
// naming exactly the parts in changed (none when empty), then one `approved` entry per part in
// approved, in that order, and nothing else.
func wantEntries(t *testing.T, sl islices.Slice, n int, changed, approved []string) {
	t.Helper()
	if len(sl.History) < n {
		t.Errorf("history has %d entries, fewer than the %d before", len(sl.History), n)
		return
	}
	got := sl.History[n:]
	if len(changed) > 0 {
		if len(got) == 0 || got[0].Kind != "changed" {
			t.Errorf("entries added %+v, want a changed entry first", got)
			return
		}
		g, w := slices.Clone(got[0].Refs), slices.Clone(changed)
		slices.Sort(g)
		slices.Sort(w)
		if !slices.Equal(g, w) {
			t.Errorf("the changed entry names %v, want exactly the restored parts %v", got[0].Refs, changed)
		}
		got = got[1:]
	}
	var refs []string
	for _, e := range got {
		if e.Kind != "approved" || len(e.Refs) != 1 {
			t.Errorf("entry %+v, want an approved entry of one part", e)
			continue
		}
		refs = append(refs, e.Refs[0])
	}
	if !slices.Equal(refs, approved) {
		t.Errorf("approved entries for %v, want one each for %v in that order", refs, approved)
	}
}

func TestProofSliceRestoresAHandChangedPart(t *testing.T) { // T048, data model "Proof slice": one changed entry, then approved per unapproved part
	for _, part := range []islices.Part{islices.PartGoals, islices.PartRules, islices.PartContract} {
		t.Run(string(part), func(t *testing.T) {
			r := newPRRig(t)
			changed := r.change(part)
			if changed.State == "approved" {
				t.Fatalf("a hand change of %s leaves the slice approved", part)
			}

			got := r.askOK()
			r.equalsTheCopy(got)
			wantEntries(t, got, len(changed.History), []string{string(part)}, []string{string(part)})

			// Asked again, it changes nothing.
			again := r.askOK()
			sameRecord(t, "a second ask", again, got)
		})
	}
}

func TestProofSliceRestoresTwoHandChangedPartsInOneChangedEntry(t *testing.T) { // T048, "recorded as one changed entry (refs the restored parts)"
	r := newPRRig(t)
	r.change(islices.PartGoals)
	changed := r.change(islices.PartRules)

	got := r.askOK()
	r.equalsTheCopy(got)
	wantEntries(t, got, len(changed.History), []string{"goals", "rules"}, []string{"goals", "rules"})
}

func TestProofSliceOfAnUnchangedApprovedSliceGetsNoNewEntry(t *testing.T) { // T048, regression guard: "an unchanged, approved slice gets no new entry"
	r := newPRRig(t)
	got := r.askOK()
	sameRecord(t, "an ask of an unchanged proof slice", got, r.ps)
	wantEntries(t, got, len(r.ps.History), nil, nil)
	r.equalsTheCopy(got)
}

func TestProofSliceWhoseRestoreFailsItsChecksIsLeftAsItWas(t *testing.T) { // T048, PartFailsChecks "leaves the slice as it was"
	r := newPRRig(t)
	r.change(islices.PartContract) // the hand change passes the lint; the copy's contract will not
	r.lint.setFailOn("listNotes")
	before, err := r.svc.Get(r.ps.ID)
	if err != nil {
		t.Fatal(err)
	}

	_, err = r.ask()
	var e *islices.Error
	if !errors.As(err, &e) || e.Code != "PartFailsChecks" || len(e.Failures) == 0 {
		t.Fatalf("ProofSlice = %v, want PartFailsChecks listing the failures", err)
	}
	after, err := r.svc.Get(r.ps.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Errorf("a failed restore changed the slice:\n got %+v\nwant %+v", after, before)
	}
	if n := len(r.svc.List()); n != 2 {
		t.Errorf("%d slices, want 2", n)
	}

	// Once the check passes, the next ask restores.
	r.lint.setFailOn("")
	got := r.askOK()
	r.equalsTheCopy(got)
	wantEntries(t, got, len(before.History), []string{"contract"}, []string{"contract"})
}

func TestProofSliceRestoresAfterAMethodChangeWithdrewApprovals(t *testing.T) { // T048, data model "Proof slice"
	r := newPRRig(t)
	r.change(islices.PartGoals)

	// The engine restarts under method B whose lint rejects the contract: the read withdraws the
	// contract's approval, and the goals part is already unapproved.
	r.lint.setFailOn("listNotes")
	b := cmOpen(t, r.rec, cmID(t, "b"), apiPattern(t), &fakeAnalyst{results: []analystResult{{p: notesProposal(t)}}}, &cmDiff{}, r.lint)
	stale, err := b.Get(r.ps.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(stale.History, func(h islices.HistoryEntry) bool { return h.Kind == "withdrawn" }) {
		t.Fatalf("history holds no withdrawn entry after the method change: the setup is wrong")
	}
	r.lint.setFailOn("")
	r.svc = b

	got := r.askOK()
	r.equalsTheCopy(got)
	if got.ChecksMethod != cmID(t, "b") {
		t.Errorf("checks_method %q, want %q", got.ChecksMethod, cmID(t, "b"))
	}
	wantEntries(t, got, len(stale.History), []string{"goals"}, []string{"goals", "contract"})
}

func TestProofSliceApprovesAPartChangedBackToTheCopyWithoutAChangedEntry(t *testing.T) { // T048, regression guard: only a part that differs is restored
	r := newPRRig(t)
	r.change(islices.PartGoals)
	sl, err := r.svc.ReplaceGoals(context.Background(), r.ps.ID, r.ps.Goals) // back to the copy's text
	if err != nil {
		t.Fatal(err)
	}
	if sl.Parts != r.ps.Parts {
		t.Fatalf("the goals were not changed back to the copy: the setup is wrong")
	}
	if sl.State == "approved" {
		t.Fatalf("the slice is approved after a hand change and back; the test needs the part unapproved")
	}

	got := r.askOK()
	r.equalsTheCopy(got)
	wantEntries(t, got, len(sl.History), nil, []string{"goals"})
}
