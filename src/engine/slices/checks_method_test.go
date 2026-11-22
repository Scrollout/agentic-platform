// Tests for T013 (feature 006, US2, spec FR-005 to FR-008, research R4): a slice's check results
// are kept only under the method identity they were produced under. A service given identity B
// that reaches a slice whose record holds `checks_method` A, or none, runs every check again
// before it answers, stores the record with `checks_method` B and keeps the slice's content.
//
// The tests fail until the code adds, in package slices:
//
//	Slice.ChecksMethod string  // the identity every check ran under; "" when one could not run
//	                           // or for a record written before feature 006
//	// and a top-level JSON field "checks_method" (string) in the stored record, absent when not
//	// every check ran. Config.Method (exists) is the identity the service runs under.
//
// Stale records are made by creating slices with a service under A (or no identity) on a shared
// fake Records, then opening a service under B on the same Records. Existing fakes (analyst,
// records) are used unchanged; the diff and lint fakes here count calls under a mutex and can
// block, because the concurrency cases need that.
package slices_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentic-platform/engine/slices"
	"github.com/agentic-platform/slice"
)

const cmDesc = "Let me keep notes."

// cmID is a method identity as the engine gives it: 64 lowercase hex digits.
func cmID(_ *testing.T, digit string) string { return strings.Repeat(digit, 64) }

// cmRecords is the existing memRecords whose slice-record writes can be made to fail.
type cmRecords struct {
	*memRecords
	fmu  sync.Mutex
	fail bool
	puts int
}

func newCmRecords() *cmRecords { return &cmRecords{memRecords: newMemRecords()} }

func (r *cmRecords) PutSliceRecord(id string, b []byte) error {
	r.fmu.Lock()
	fail := r.fail
	if !fail {
		r.puts++
	}
	r.fmu.Unlock()
	if fail {
		return errors.New("disk full")
	}
	return r.memRecords.PutSliceRecord(id, b)
}

func (r *cmRecords) setFail(v bool) { r.fmu.Lock(); r.fail = v; r.fmu.Unlock() }

// cmDiff counts calls and can fail.
type cmDiff struct {
	mu  sync.Mutex
	n   int
	err error
}

func (d *cmDiff) Diff(_ context.Context, _, _ string) ([]slices.ContractFinding, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.n++
	return nil, d.err
}
func (d *cmDiff) calls() int       { d.mu.Lock(); defer d.mu.Unlock(); return d.n }
func (d *cmDiff) setErr(err error) { d.mu.Lock(); d.err = err; d.mu.Unlock() }

// cmLint counts calls, can report findings or fail, and blocks a contract containing slow until
// release is closed (entered receives one value per blocked call).
type cmLint struct {
	mu       sync.Mutex
	n        int
	findings []slices.LintFinding
	err      error
	slow     string
	entered  chan struct{}
	release  chan struct{}
}

func newCmLint() *cmLint {
	return &cmLint{entered: make(chan struct{}, 16), release: make(chan struct{})}
}

func (l *cmLint) Lint(_ context.Context, contract string) ([]slices.LintFinding, error) {
	l.mu.Lock()
	l.n++
	f, err, slow := l.findings, l.err, l.slow
	l.mu.Unlock()
	if slow != "" && strings.Contains(contract, slow) {
		l.entered <- struct{}{}
		select {
		case <-l.release:
		case <-time.After(10 * time.Second):
		}
	}
	return f, err
}
func (l *cmLint) calls() int { l.mu.Lock(); defer l.mu.Unlock(); return l.n }
func (l *cmLint) set(f []slices.LintFinding, err error) {
	l.mu.Lock()
	l.findings, l.err = f, err
	l.mu.Unlock()
}

var cmLintError = []slices.LintFinding{{Level: "error", Code: "operation-description", Text: "operation needs a description"}}

// cmOpen opens a service on rec under id (zero: no identity).
func cmOpen(t *testing.T, rec slices.Records, id string, pattern []byte, an slices.Analyst, d slices.ContractDiff, l slices.ContractLint) *slices.Service {
	t.Helper()
	schemas, err := slice.LoadSchemas("../api/preview/2026-11-12-preview")
	if err != nil {
		t.Fatal(err)
	}
	s, err := slices.New(slices.Config{Schemas: schemas, Analyst: an, Diff: d, Pattern: pattern, Lint: l, Records: rec, Method: id})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func cmPattern(t *testing.T, name string) []byte {
	t.Helper()
	if name == "" {
		return apiPattern(t)
	}
	b, err := os.ReadFile("testdata/" + name) // #nosec G304 -- name is a fixture name chosen by the test
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func cmDescription(i int) string { return cmDesc + strings.Repeat("!", i) }

// cmSeed stores approved slices S-1, S-2, ... (one per proposal, description cmDescription(i))
// under identity from, with a passing lint and diff.
func cmSeed(t *testing.T, rec slices.Records, from string, pattern []byte, proposals ...slices.Proposal) []slices.Slice {
	t.Helper()
	results := make([]analystResult, len(proposals))
	for i, p := range proposals {
		results[i] = analystResult{p: p}
	}
	s := cmOpen(t, rec, from, pattern, &fakeAnalyst{results: results}, &cmDiff{}, newCmLint())
	var out []slices.Slice
	for i := range proposals {
		sl := create(t, s, cmDescription(i))
		out = append(out, approveAll(t, s, sl))
	}
	return out
}

// cmUnder opens the service under B on rec with fresh counting fakes and an analyst that
// proposes the notes fixture.
func cmUnder(t *testing.T, rec slices.Records, b string) (*slices.Service, *fakeAnalyst, *cmDiff, *cmLint) {
	t.Helper()
	a := &fakeAnalyst{results: []analystResult{{p: notesProposal(t)}}}
	d, l := &cmDiff{}, newCmLint()
	return cmOpen(t, rec, b, apiPattern(t), a, d, l), a, d, l
}

// cmRecord returns the stored record of slice id as a generic JSON object.
func cmRecord(t *testing.T, rec slices.Records, id string) map[string]any {
	t.Helper()
	all, err := rec.SliceRecords()
	if err != nil {
		t.Fatal(err)
	}
	raw, ok := all[id]
	if !ok {
		t.Fatalf("no stored record of %s", id)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// cmStoredMethod is the record's top-level checks_method, "" when absent or empty.
func cmStoredMethod(t *testing.T, rec slices.Records, id string) string {
	t.Helper()
	v, present := cmRecord(t, rec, id)["checks_method"]
	if !present || v == nil {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		t.Fatalf("checks_method of %s is %T, want a string", id, v)
	}
	return s
}

// cmContent is what a re-check must keep byte for byte: the stored goals, rules, terms, examples
// and contract.
func cmContent(t *testing.T, rec slices.Records, id string) map[string]any {
	t.Helper()
	sl, _ := cmRecord(t, rec, id)["slice"].(map[string]any)
	out := map[string]any{}
	for _, k := range []string{"goals", "rules", "terms", "examples", "contract"} {
		out[k] = sl[k]
	}
	return out
}

func cmSameContent(t *testing.T, before, after slices.Slice) {
	t.Helper()
	if !reflect.DeepEqual(before.Goals, after.Goals) || !reflect.DeepEqual(before.Rules, after.Rules) ||
		!reflect.DeepEqual(before.Terms, after.Terms) || before.Examples != after.Examples ||
		before.Contract != after.Contract || before.Parts != after.Parts {
		t.Errorf("a re-check changed the slice's content:\nbefore %+v\nafter  %+v", before, after)
	}
}

// cmSameApprovals compares approvals by part, hash and instant (a time held in memory carries a
// monotonic reading that one loaded from a record lacks).
func cmSameApprovals(a, b []slices.Approval) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Part != b[i].Part || a[i].ContentHash != b[i].ContentHash || !a[i].At.Equal(b[i].At) {
			return false
		}
	}
	return true
}

func cmAsError(t *testing.T, err error) *slices.Error {
	t.Helper()
	var e *slices.Error
	if !errors.As(err, &e) {
		t.Fatalf("error %v is not a *slices.Error", err)
	}
	return e
}

func cmWithdrawnRefs(sl slices.Slice) []string {
	var refs []string
	for _, h := range sl.History {
		if h.Kind == "withdrawn" {
			refs = append(refs, h.Refs...)
		}
	}
	return refs
}

func cmFailed(sl slices.Slice, check string) []slices.Check {
	var out []slices.Check
	for _, c := range sl.Checks {
		if c.Check == check && !c.Passed {
			out = append(out, c)
		}
	}
	return out
}

// cmGo runs f in a goroutine and returns a function that waits for it, failing after d.
func cmGo[T any](f func() T) func(t *testing.T) T {
	ch := make(chan T, 1)
	go func() { ch <- f() }()
	return func(t *testing.T) T {
		t.Helper()
		select {
		case v := <-ch:
			return v
		case <-time.After(5 * time.Second):
			t.Fatal("a call did not finish in 5 s")
			panic("unreachable")
		}
	}
}

func cmEntered(t *testing.T, l *cmLint) {
	t.Helper()
	select {
	case <-l.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the re-check never called the lint")
	}
}

// ---- every way to a stale slice checks again first ----

func TestStaleRecordIsCheckedAgainBeforeEveryPathAnswers(t *testing.T) {
	ctx := context.Background()
	paths := []struct {
		name string
		run  func(s *slices.Service, sl slices.Slice) (slices.Slice, error)
	}{
		{"Get", func(s *slices.Service, sl slices.Slice) (slices.Slice, error) { return s.Get(sl.ID) }},
		{"Approve", func(s *slices.Service, sl slices.Slice) (slices.Slice, error) {
			return s.Approve(ctx, sl.ID, slices.PartGoals, sl.Parts.Goals)
		}},
		{"Buildable", func(s *slices.Service, sl slices.Slice) (slices.Slice, error) { return sl, s.Buildable(sl.ID) }},
		{"ReplaceGoals", func(s *slices.Service, sl slices.Slice) (slices.Slice, error) {
			return s.ReplaceGoals(ctx, sl.ID, sl.Goals) // the same goals: no change of content
		}},
		{"ApprovedParts", func(s *slices.Service, sl slices.Slice) (slices.Slice, error) {
			_, err := s.ApprovedParts(sl.ID)
			return sl, err
		}},
		{"CheckApprove", func(s *slices.Service, sl slices.Slice) (slices.Slice, error) {
			return sl, s.CheckApprove(sl.ID, slices.PartRules, sl.Parts.Rules)
		}},
		{"List", func(s *slices.Service, sl slices.Slice) (slices.Slice, error) {
			l := s.List()
			if len(l) != 1 {
				t.Fatalf("List returned %d slices, want 1", len(l))
			}
			return l[0], nil
		}},
		{"Create", func(s *slices.Service, sl slices.Slice) (slices.Slice, error) {
			r, err := s.Create(ctx, sl.Description)
			if err == nil && !r.Reused {
				t.Error("Create did not offer the approved slice of the same description, whose checks still pass")
			}
			return r.Slice, err
		}},
	}
	for _, from := range []struct {
		name string
		id   func(*testing.T) string
	}{{"under A", func(t *testing.T) string { return cmID(t, "a") }}, {"without identity", func(*testing.T) string { return "" }}} {
		for _, p := range paths {
			t.Run(from.name+"/"+p.name, func(t *testing.T) {
				rec := newCmRecords()
				seeded := cmSeed(t, rec, from.id(t), apiPattern(t), notesProposal(t))[0]
				b := cmID(t, "b")
				if m := cmStoredMethod(t, rec, seeded.ID); m == b {
					t.Fatalf("setup: the record already holds %s", m)
				}
				contentBefore := cmContent(t, rec, seeded.ID)
				s, an, d, l := cmUnder(t, rec, b)

				got, err := p.run(s, seeded)
				if err != nil {
					t.Fatalf("%s: %v", p.name, err)
				}
				if d.calls() != 1 || l.calls() != 1 {
					t.Errorf("every check must run once before the answer: compatibility %d, lint %d", d.calls(), l.calls())
				}
				if an.calls != 0 {
					t.Errorf("the analyst ran %d times; a re-check asks no model", an.calls)
				}
				if m := cmStoredMethod(t, rec, seeded.ID); m != b {
					t.Errorf("stored checks_method %q, want %q", m, b)
				}
				if !reflect.DeepEqual(contentBefore, cmContent(t, rec, seeded.ID)) {
					t.Errorf("the stored content changed:\nbefore %v\nafter  %v", contentBefore, cmContent(t, rec, seeded.ID))
				}
				cmSameContent(t, seeded, got)

				now, err := s.Get(seeded.ID)
				if err != nil {
					t.Fatal(err)
				}
				if now.ChecksMethod != b {
					t.Errorf("Slice.ChecksMethod %q after the re-check, want %q", now.ChecksMethod, b)
				}
				cmSameContent(t, seeded, now)
				if now.State != "approved" || !cmSameApprovals(now.Approvals, seeded.Approvals) {
					t.Errorf("checks pass under B, so the approvals hold: state %q, approvals %+v, want %+v", now.State, now.Approvals, seeded.Approvals)
				}
				if !reflect.DeepEqual(historyKinds(now), historyKinds(seeded)) && p.name != "Approve" {
					t.Errorf("history %v, want %v", historyKinds(now), historyKinds(seeded))
				}
				if d.calls() != 1 || l.calls() != 1 {
					t.Errorf("a slice already current was checked again: compatibility %d, lint %d", d.calls(), l.calls())
				}
			})
		}
	}
}

func TestAnswerOnAStaleSliceChecksAgainFirst(t *testing.T) {
	rec := newCmRecords()
	first := qProposal(t,
		slices.QuestionDraft{About: "R-1", Text: "Keep forever?", Options: []string{"yes", "no"}},
		slices.QuestionDraft{About: "G-1", Text: "Which notes?"})
	a := cmID(t, "a")
	sa := cmOpen(t, rec, a, apiPattern(t), &fakeAnalyst{results: []analystResult{{p: first}}}, &cmDiff{}, newCmLint())
	sl := create(t, sa, cmDesc)

	b := cmID(t, "b")
	revised := notesProposal(t)
	revised.Goals[0].Text = "Keep a note for good"
	an := &fakeAnalyst{results: []analystResult{{p: revised}}}
	d, l := &cmDiff{}, newCmLint()
	s := cmOpen(t, rec, b, apiPattern(t), an, d, l)
	got, err := s.Answer(context.Background(), sl.ID, []slices.Answer{{Question: "Q-1", Option: intp(0)}, {Question: "Q-2", Text: "All of them"}})
	if err != nil {
		t.Fatal(err)
	}
	if l.calls() < 1 || d.calls() < 1 {
		t.Errorf("Answer did not check the stale slice: compatibility %d, lint %d", d.calls(), l.calls())
	}
	if got.State == "questions-open" || len(got.Questions) != 2 || got.Questions[0].Answer == nil {
		t.Errorf("the answers are not recorded: %+v", got)
	}
	if m := cmStoredMethod(t, rec, sl.ID); m != b {
		t.Errorf("stored checks_method %q, want %q", m, b)
	}
	n := l.calls()
	if _, err := s.Get(sl.ID); err != nil || l.calls() != n {
		t.Errorf("the slice is current after Answer, yet a read checked again (err %v, lint %d -> %d)", err, n, l.calls())
	}
}

// ---- a part whose check now fails ----

func TestAPartThatNowFailsLosesItsApprovalAndBuildIsRefused(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(t *testing.T, rec *cmRecords) (*slices.Service, *cmLint, slices.Slice, string) // service under B, lint, seeded slice, failing part
	}{
		{"the lint finds an error in the contract", func(t *testing.T, rec *cmRecords) (*slices.Service, *cmLint, slices.Slice, string) {
			sl := cmSeed(t, rec, cmID(t, "a"), apiPattern(t), notesProposal(t))[0]
			s, _, _, l := cmUnder(t, rec, cmID(t, "b"))
			l.set(cmLintError, nil)
			return s, l, sl, "contract"
		}},
		{"a pattern condition fails on the rules", func(t *testing.T, rec *cmRecords) (*slices.Service, *cmLint, slices.Slice, string) {
			p := notesProposal(t)
			p.Rules[1].System = "another service"
			sl := cmSeed(t, rec, cmID(t, "a"), cmPattern(t, "pattern-contract-only.yaml"), p)[0]
			l := newCmLint()
			s := cmOpen(t, rec, cmID(t, "b"), cmPattern(t, "pattern-one-system.yaml"),
				&fakeAnalyst{results: []analystResult{{p: notesProposal(t)}}}, &cmDiff{}, l)
			return s, l, sl, "rules"
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := newCmRecords()
			s, _, seeded, part := c.prepare(t, rec)
			if seeded.State != "approved" {
				t.Fatalf("setup: state %q", seeded.State)
			}
			got, err := s.Get(seeded.ID)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := approval(got, slices.Part(part)); ok {
				t.Errorf("the %s part fails a check under B and keeps its approval", part)
			}
			for _, other := range []slices.Part{slices.PartGoals, slices.PartRules, slices.PartContract} {
				if string(other) == part {
					continue
				}
				if _, ok := approval(got, other); !ok {
					t.Errorf("the %s part passes and lost its approval", other)
				}
			}
			if refs := cmWithdrawnRefs(got); !reflect.DeepEqual(refs, []string{part}) {
				t.Errorf("withdrawn refs %v, want [%s]; history %v", refs, part, historyKinds(got))
			}
			if got.State != "checked" {
				t.Errorf("state %q, want checked", got.State)
			}
			cmSameContent(t, seeded, got)
			if err := s.Buildable(got.ID); err == nil {
				t.Error("Buildable accepts a slice whose part lost its approval")
			} else {
				_ = cmAsError(t, err)
			}
			if _, err := s.ApprovedParts(got.ID); err == nil {
				t.Error("ApprovedParts hands out the parts of a slice that is no longer approved")
			}
			if m := cmStoredMethod(t, rec, got.ID); m != cmID(t, "b") {
				t.Errorf("every check ran, so checks_method is B; stored %q", m)
			}
			// Create does not offer the slice as approved.
			r, err := s.Create(context.Background(), seeded.Description)
			if err != nil {
				t.Fatal(err)
			}
			if r.Reused || r.Slice.ID == seeded.ID {
				t.Errorf("Create offered %s as approved (Reused %v)", r.Slice.ID, r.Reused)
			}
		})
	}
}

func TestCheckApproveOfAFailingContractAfterTheRecheckIsRefused(t *testing.T) {
	rec := newCmRecords()
	sl := cmSeed(t, rec, cmID(t, "a"), apiPattern(t), notesProposal(t))[0]
	s, _, _, l := cmUnder(t, rec, cmID(t, "b"))
	l.set(cmLintError, nil)
	err := s.CheckApprove(sl.ID, slices.PartContract, sl.Parts.Contract)
	if e := cmAsError(t, err); e.Code != "PartFailsChecks" || len(e.Failures) == 0 {
		t.Errorf("CheckApprove: %+v, want PartFailsChecks naming the lint", e)
	}
}

// ---- a check that could not run ----

func TestACheckThatCouldNotRunFailsAndTheNextReadChecksAgain(t *testing.T) {
	for _, c := range []struct {
		name, check string
		break_      func(d *cmDiff, l *cmLint)
		fix         func(d *cmDiff, l *cmLint)
	}{
		{"lint", slices.CheckContractLint, func(d *cmDiff, l *cmLint) { l.set(nil, errors.New("lint is down")) }, func(d *cmDiff, l *cmLint) { l.set(nil, nil) }},
		{"compatibility", slices.CheckContractCompatibility, func(d *cmDiff, l *cmLint) { d.setErr(errors.New("oasdiff is down")) }, func(d *cmDiff, l *cmLint) { d.setErr(nil) }},
	} {
		t.Run(c.name, func(t *testing.T) {
			rec := newCmRecords()
			seeded := cmSeed(t, rec, cmID(t, "a"), apiPattern(t), notesProposal(t))[0]
			b := cmID(t, "b")
			s, _, d, l := cmUnder(t, rec, b)
			c.break_(d, l)

			got, err := s.Get(seeded.ID)
			if err != nil {
				t.Fatal(err)
			}
			fails := cmFailed(got, c.check)
			if len(fails) != 1 || fails[0].Reason == "" || fails[0].Part != "contract" {
				t.Errorf("want one failed %s on the contract with its reason, got %+v", c.check, fails)
			}
			if _, ok := approval(got, slices.PartContract); ok {
				t.Error("the contract keeps its approval though a check of it could not run")
			}
			if refs := cmWithdrawnRefs(got); !reflect.DeepEqual(refs, []string{"contract"}) {
				t.Errorf("withdrawn refs %v, want [contract]", refs)
			}
			if got.ChecksMethod != "" {
				t.Errorf("Slice.ChecksMethod %q, want none while a check could not run", got.ChecksMethod)
			}
			if err := s.Buildable(seeded.ID); err == nil {
				t.Error("Buildable accepts a slice with a check that could not run")
			}
			if m := cmStoredMethod(t, rec, seeded.ID); m != "" {
				t.Errorf("stored checks_method %q, want none", m)
			}

			dn, ln := d.calls(), l.calls()
			if _, err := s.Get(seeded.ID); err != nil {
				t.Fatal(err)
			}
			if d.calls() <= dn && l.calls() <= ln {
				t.Error("the next read did not check again")
			}
			c.fix(d, l)
			got, err = s.Get(seeded.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(cmFailed(got, c.check)) != 0 || got.ChecksMethod != b {
				t.Errorf("after the check can run again: failed %+v, ChecksMethod %q", cmFailed(got, c.check), got.ChecksMethod)
			}
			if m := cmStoredMethod(t, rec, seeded.ID); m != b {
				t.Errorf("stored checks_method %q, want %q", m, b)
			}
			dn, ln = d.calls(), l.calls()
			if _, err := s.Get(seeded.ID); err != nil || d.calls() != dn || l.calls() != ln {
				t.Error("a current slice was checked again")
			}
		})
	}
}

func TestAFreshSliceWhoseCheckCouldNotRunStoresNoMethod(t *testing.T) {
	rec := newCmRecords()
	s, _, _, l := cmUnder(t, rec, cmID(t, "b"))
	l.set(nil, errors.New("lint is down"))
	sl := create(t, s, cmDesc)
	if sl.ChecksMethod != "" || cmStoredMethod(t, rec, sl.ID) != "" {
		t.Errorf("ChecksMethod %q, stored %q; want none", sl.ChecksMethod, cmStoredMethod(t, rec, sl.ID))
	}
	n := l.calls()
	l.set(nil, nil)
	if got, err := s.Get(sl.ID); err != nil || got.ChecksMethod != cmID(t, "b") || l.calls() <= n {
		t.Errorf("the next read must check again: err %v, ChecksMethod %q, lint %d -> %d", err, got.ChecksMethod, n, l.calls())
	}
}

// ---- nothing to check ----

func TestARecordUnderTheServicesMethodIsNotCheckedAgain(t *testing.T) {
	rec := newCmRecords()
	b := cmID(t, "b")
	seeded := cmSeed(t, rec, b, apiPattern(t), notesProposal(t))[0]
	if m := cmStoredMethod(t, rec, seeded.ID); m != b {
		t.Fatalf("a slice created under B stores checks_method %q, want %q", m, b)
	}
	if seeded.ChecksMethod != b {
		t.Errorf("Slice.ChecksMethod %q, want %q", seeded.ChecksMethod, b)
	}
	puts := rec.puts
	s, an, d, l := cmUnder(t, rec, b) // a restart under the same identity
	ctx := context.Background()
	if _, err := s.Get(seeded.ID); err != nil {
		t.Error(err)
	}
	if len(s.List()) != 1 {
		t.Error("List")
	}
	if err := s.Buildable(seeded.ID); err != nil {
		t.Error(err)
	}
	if _, err := s.ApprovedParts(seeded.ID); err != nil {
		t.Error(err)
	}
	if err := s.CheckApprove(seeded.ID, slices.PartGoals, seeded.Parts.Goals); err != nil {
		t.Error(err)
	}
	if _, err := s.ReplaceGoals(ctx, seeded.ID, seeded.Goals); err != nil {
		t.Error(err)
	}
	if r, err := s.Create(ctx, seeded.Description); err != nil || !r.Reused {
		t.Errorf("Create: %v, reused %v", err, r.Reused)
	}
	if d.calls() != 0 || l.calls() != 0 || an.calls != 0 || rec.puts != puts {
		t.Errorf("nothing was to be checked: compatibility %d, lint %d, analyst %d, stores %d", d.calls(), l.calls(), an.calls, rec.puts-puts)
	}
}

func TestAServiceWithoutAnIdentityChecksNothingAgain(t *testing.T) {
	rec := newCmRecords()
	seeded := cmSeed(t, rec, cmID(t, "a"), apiPattern(t), notesProposal(t))[0]
	before := cmRecord(t, rec, seeded.ID)
	puts := rec.puts
	s, _, d, l := cmUnder(t, rec, "")
	ctx := context.Background()
	if _, err := s.Get(seeded.ID); err != nil {
		t.Error(err)
	}
	if len(s.List()) != 1 {
		t.Error("List")
	}
	if err := s.Buildable(seeded.ID); err != nil {
		t.Error(err)
	}
	if r, err := s.Create(ctx, seeded.Description); err != nil || !r.Reused {
		t.Errorf("Create: %v, reused %v", err, r.Reused)
	}
	if d.calls() != 0 || l.calls() != 0 || rec.puts != puts {
		t.Errorf("a service with no identity re-checked: compatibility %d, lint %d, stores %d", d.calls(), l.calls(), rec.puts-puts)
	}
	if !reflect.DeepEqual(before, cmRecord(t, rec, seeded.ID)) {
		t.Error("the stored record changed")
	}
	// A slice it creates stores no identity.
	sl, err := s.Create(ctx, "another description")
	if err != nil {
		t.Fatal(err)
	}
	if sl.Slice.ChecksMethod != "" || cmStoredMethod(t, rec, sl.Slice.ID) != "" {
		t.Errorf("a slice created without an identity has ChecksMethod %q, stored %q", sl.Slice.ChecksMethod, cmStoredMethod(t, rec, sl.Slice.ID))
	}
}

func TestListChecksEveryStaleSliceAndGetOnlyTheOneItReads(t *testing.T) {
	rec := newCmRecords()
	p2 := notesProposal(t)
	p2.Contract = strings.Replace(p2.Contract, "title: Notes", "title: Other", 1)
	seeded := cmSeed(t, rec, cmID(t, "a"), apiPattern(t), notesProposal(t), p2)
	b := cmID(t, "b")
	s, _, _, l := cmUnder(t, rec, b)
	l.set(cmLintError, nil)

	if _, err := s.Get("S-9"); cmAsError(t, err).Code != "NotFound" || l.calls() != 0 {
		t.Errorf("an unknown slice must be NotFound with nothing checked (lint %d)", l.calls())
	}
	if _, err := s.Get(seeded[0].ID); err != nil {
		t.Fatal(err)
	}
	if l.calls() != 1 || cmStoredMethod(t, rec, seeded[1].ID) == b {
		t.Errorf("Get of one slice checked another (lint %d, other stored %q)", l.calls(), cmStoredMethod(t, rec, seeded[1].ID))
	}
	list := s.List()
	if len(list) != 2 || l.calls() != 2 {
		t.Fatalf("List: %d slices, lint %d, want 2 and 2 (one slice was current)", len(list), l.calls())
	}
	for _, sl := range list {
		if sl.State != "checked" || sl.ChecksMethod != b {
			t.Errorf("%s in the list: state %q, ChecksMethod %q; the lint fails under B, so it is checked", sl.ID, sl.State, sl.ChecksMethod)
		}
		if m := cmStoredMethod(t, rec, sl.ID); m != b {
			t.Errorf("%s stored %q", sl.ID, m)
		}
	}
}

// ---- a store write that fails ----

func TestAFailedStoreLeavesTheRecordStaleAndTheAnswerShowsTheNewResults(t *testing.T) {
	rec := newCmRecords()
	seeded := cmSeed(t, rec, cmID(t, "a"), apiPattern(t), notesProposal(t))[0]
	raw, _ := rec.SliceRecords()
	before := string(raw[seeded.ID])
	b := cmID(t, "b")
	s, _, _, l := cmUnder(t, rec, b)
	l.set(cmLintError, nil)

	rec.setFail(true)
	got, err := s.Get(seeded.ID)
	if err != nil {
		t.Fatalf("Get: %v (the answer shows the new results; the store write is not its failure)", err)
	}
	if len(cmFailed(got, slices.CheckContractLint)) != 1 {
		t.Errorf("the answer does not show the new lint result: %+v", got.Checks)
	}
	if _, ok := approval(got, slices.PartContract); ok {
		t.Error("the answer keeps an approval the new results withdraw")
	}
	raw, _ = rec.SliceRecords()
	if string(raw[seeded.ID]) != before {
		t.Error("the stored record changed though the write failed")
	}
	n := l.calls()
	rec.setFail(false)
	if _, err := s.Get(seeded.ID); err != nil {
		t.Fatal(err)
	}
	if l.calls() <= n {
		t.Error("the next read did not check again")
	}
	if m := cmStoredMethod(t, rec, seeded.ID); m != b {
		t.Errorf("stored checks_method %q, want %q after the store works again", m, b)
	}
	n = l.calls()
	if _, err := s.Get(seeded.ID); err != nil || l.calls() != n {
		t.Error("a current slice was checked again")
	}
}

// ---- concurrency ----

func TestASlowLintDoesNotHoldAReadOfAnotherSlice(t *testing.T) {
	rec := newCmRecords()
	slow := notesProposal(t)
	slow.Contract = strings.Replace(slow.Contract, "title: Notes", "title: SlowOne", 1)
	seeded := cmSeed(t, rec, cmID(t, "a"), apiPattern(t), slow, notesProposal(t))
	s, _, _, l := cmUnder(t, rec, cmID(t, "b"))
	l.mu.Lock()
	l.slow = "SlowOne"
	l.mu.Unlock()

	waitSlow := cmGo(func() error { _, err := s.Get(seeded[0].ID); return err })
	cmEntered(t, l)
	waitFast := cmGo(func() error { _, err := s.Get(seeded[1].ID); return err })
	if err := waitFast(t); err != nil { // fails the test after 5 s if the slow lint holds it
		t.Fatal(err)
	}
	if m := cmStoredMethod(t, rec, seeded[1].ID); m != cmID(t, "b") {
		t.Errorf("the other slice was not checked and stored while the slow lint ran: %q", m)
	}
	close(l.release)
	if err := waitSlow(t); err != nil {
		t.Fatal(err)
	}
	if m := cmStoredMethod(t, rec, seeded[0].ID); m != cmID(t, "b") {
		t.Errorf("the slow slice stored %q", m)
	}
}

func TestTwoReadsOfOneStaleSliceRunItsChecksOnce(t *testing.T) {
	rec := newCmRecords()
	seeded := cmSeed(t, rec, cmID(t, "a"), apiPattern(t), notesProposal(t))[0]
	s, _, d, l := cmUnder(t, rec, cmID(t, "b"))
	l.mu.Lock()
	l.slow = "Notes"
	l.mu.Unlock()

	first := cmGo(func() error { _, err := s.Get(seeded.ID); return err })
	cmEntered(t, l)
	second := cmGo(func() error { _, err := s.Get(seeded.ID); return err })
	time.Sleep(50 * time.Millisecond) // the second read reaches the slice while the first checks
	close(l.release)
	if err := first(t); err != nil {
		t.Fatal(err)
	}
	if err := second(t); err != nil {
		t.Fatal(err)
	}
	if l.calls() != 1 || d.calls() != 1 {
		t.Errorf("two reads ran the checks %d (lint) and %d (compatibility) times, want once each", l.calls(), d.calls())
	}
}

// An Approve or Answer that arrives while a re-check runs is kept, and the results are current
// afterwards. The slice service takes the slice's own lock around a re-check, so the call may wait
// for it or land in it; either way both effects must stand.
func TestAnApproveDuringARecheckIsKept(t *testing.T) {
	rec := newCmRecords()
	a := cmID(t, "a")
	sa := cmOpen(t, rec, a, apiPattern(t), &fakeAnalyst{results: []analystResult{{p: notesProposal(t)}}}, &cmDiff{}, newCmLint())
	sl := create(t, sa, cmDesc)
	for _, p := range []struct {
		part slices.Part
		hash string
	}{{slices.PartRules, sl.Parts.Rules}, {slices.PartContract, sl.Parts.Contract}} {
		if _, err := sa.Approve(context.Background(), sl.ID, p.part, p.hash); err != nil {
			t.Fatal(err)
		}
	}
	b := cmID(t, "b")
	s, _, _, l := cmUnder(t, rec, b)
	l.mu.Lock()
	l.slow = "Notes"
	l.mu.Unlock()

	read := cmGo(func() error { _, err := s.Get(sl.ID); return err })
	cmEntered(t, l)
	approve := cmGo(func() error {
		_, err := s.Approve(context.Background(), sl.ID, slices.PartGoals, sl.Parts.Goals)
		return err
	})
	time.Sleep(50 * time.Millisecond)
	close(l.release)
	if err := read(t); err != nil {
		t.Fatal(err)
	}
	if err := approve(t); err != nil {
		t.Fatalf("Approve during the re-check: %v", err)
	}
	got, err := s.Get(sl.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "approved" || len(got.Approvals) != 3 || got.ChecksMethod != b {
		t.Errorf("state %q, %d approvals, ChecksMethod %q; want approved, 3, %q", got.State, len(got.Approvals), got.ChecksMethod, b)
	}
	if m := cmStoredMethod(t, rec, sl.ID); m != b {
		t.Errorf("stored checks_method %q, want %q", m, b)
	}
	cmSameContent(t, sl, got)
	if n := l.calls(); n != 1 {
		t.Errorf("lint ran %d times, want once", n)
	}
}

func TestAnAnswerDuringARecheckIsKept(t *testing.T) {
	rec := newCmRecords()
	first := qProposal(t, slices.QuestionDraft{About: "G-1", Text: "Which notes?"})
	sa := cmOpen(t, rec, cmID(t, "a"), apiPattern(t), &fakeAnalyst{results: []analystResult{{p: first}}}, &cmDiff{}, newCmLint())
	sl := create(t, sa, cmDesc)

	b := cmID(t, "b")
	revised := notesProposal(t)
	revised.Goals[0].Text = "Keep a note for good"
	an := &fakeAnalyst{results: []analystResult{{p: revised}}}
	l := newCmLint()
	s := cmOpen(t, rec, b, apiPattern(t), an, &cmDiff{}, l)
	l.mu.Lock()
	l.slow = "Notes"
	l.mu.Unlock()

	read := cmGo(func() error { _, err := s.Get(sl.ID); return err })
	cmEntered(t, l)
	answer := cmGo(func() error {
		_, err := s.Answer(context.Background(), sl.ID, []slices.Answer{{Question: "Q-1", Text: "All of them"}})
		return err
	})
	time.Sleep(50 * time.Millisecond)
	close(l.release)
	if err := read(t); err != nil {
		t.Fatal(err)
	}
	if err := answer(t); err != nil {
		t.Fatalf("Answer during the re-check: %v", err)
	}
	got, err := s.Get(sl.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Questions) != 1 || got.Questions[0].Answer == nil || got.Questions[0].Answer.Text != "All of them" {
		t.Errorf("the answer is lost: %+v", got.Questions)
	}
	if got.Goals[0].Text != "Keep a note for good" || an.calls != 1 {
		t.Errorf("the analyst's revision is lost or the analyst ran %d times", an.calls)
	}
	if got.ChecksMethod != b || cmStoredMethod(t, rec, sl.ID) != b {
		t.Errorf("results not current: ChecksMethod %q, stored %q", got.ChecksMethod, cmStoredMethod(t, rec, sl.ID))
	}
	n := l.calls()
	if _, err := s.Get(sl.ID); err != nil || l.calls() != n {
		t.Error("the slice was checked again after it was current")
	}
}
