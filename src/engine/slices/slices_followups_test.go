// Tests for T037d (example IDs never reused; an answer that changes a part records "changed"),
// T037e (no local path or URL host in a check reason that carries a runner's error or a lint
// finding's text) and T040b's lock item (a slow oasdiff or lint call does not block other slices,
// and concurrent changes to one slice lose nothing). Written by a separate author.
//
// Decisions where the data model is silent:
//   - data-model.md says E-<n> is "unique in the slice and never reused (check ExampleTagged)", so
//     a revision that brings back an E-ID an earlier revision dropped fails ExampleTagged on the
//     rules part; the slice is stored with the failed check, not refused.
//   - an answer adds no "changed" entry; the "revised" entry's Refs name the parts that changed.
//   - two concurrent changes to one slice: the later waits (the contract's replaceContract lists
//     no code for a conflict), so both land; a refusal is accepted only with a listed code.
package slices_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentic-platform/engine/slices"
	"github.com/agentic-platform/slice"
)

// ---- T037d ----

func fuExamples(tags ...[2]string) string { // each: rule, example
	var b strings.Builder
	b.WriteString("Feature: Notes\n")
	for _, t := range tags {
		b.WriteString("  Rule: " + t[0] + "\n    @" + t[0] + " @" + t[1] + "\n    Scenario: s " + t[1] + "\n      Given a note\n")
	}
	return b.String()
}

func fuRules(sl slices.Slice, examples string) slices.RulesPart {
	return slices.RulesPart{Rules: sl.Rules, Terms: sl.Terms, Examples: examples}
}

func fuExampleFails(sl slices.Slice) []slices.Check {
	var out []slices.Check
	for _, c := range checksNamed(sl, slices.CheckExampleTagged) {
		if !c.Passed {
			out = append(out, c)
		}
	}
	return out
}

func TestADroppedExampleIDIsNotReusedByALaterRevision(t *testing.T) {
	ctx := context.Background()
	s, _, _ := setup(t)
	sl := create(t, s, "Let me keep notes.")
	// Revision 1 drops E-2 and gives R-2 a new scenario, E-3.
	sl, err := s.ReplaceRules(ctx, sl.ID, fuRules(sl, fuExamples([2]string{"R-1", "E-1"}, [2]string{"R-2", "E-3"})))
	if err != nil {
		t.Fatal(err)
	}
	if f := fuExampleFails(sl); len(f) != 0 {
		t.Fatalf("a fresh E-3 must pass ExampleTagged: %+v", f)
	}
	// Revision 2 brings E-2 back.
	sl, err = s.ReplaceRules(ctx, sl.ID, fuRules(sl, fuExamples([2]string{"R-1", "E-1"}, [2]string{"R-2", "E-3"}, [2]string{"R-2", "E-2"})))
	if err != nil {
		t.Fatalf("checks never make a change fail: %v", err)
	}
	f := fuExampleFails(sl)
	if len(f) == 0 || f[0].Part != "rules" || !strings.Contains(f[0].Reason, "E-2") {
		t.Fatalf("reusing the dropped E-2 must fail ExampleTagged on rules, naming E-2: %+v", f)
	}
	if _, err := s.Approve(ctx, sl.ID, slices.PartRules, sl.Parts.Rules); code(t, err) != "PartFailsChecks" {
		t.Errorf("rules that reuse E-2 cannot be approved: %v", err)
	}
	// Control: a never-used E-4 passes.
	sl, err = s.ReplaceRules(ctx, sl.ID, fuRules(sl, fuExamples([2]string{"R-1", "E-1"}, [2]string{"R-2", "E-3"}, [2]string{"R-2", "E-4"})))
	if err != nil {
		t.Fatal(err)
	}
	if f := fuExampleFails(sl); len(f) != 0 {
		t.Errorf("a new E-4 must pass: %+v", f)
	}
}

func TestADroppedExampleIDStaysDroppedAcrossOtherChanges(t *testing.T) {
	// The memory of dropped IDs must not depend on the dropped scenario being in the latest
	// revision only: drop E-2, then change goals, then reuse E-2.
	ctx := context.Background()
	s, _, _ := setup(t)
	sl := create(t, s, "Let me keep notes.")
	sl, err := s.ReplaceRules(ctx, sl.ID, fuRules(sl, fuExamples([2]string{"R-1", "E-1"}, [2]string{"R-2", "E-3"})))
	if err != nil {
		t.Fatal(err)
	}
	if sl, err = s.ReplaceGoals(ctx, sl.ID, []slices.Goal{{ID: "G-1", Text: "Keep"}, {ID: "G-2", Text: "List"}}); err != nil {
		t.Fatal(err)
	}
	sl, err = s.ReplaceRules(ctx, sl.ID, fuRules(sl, fuExamples([2]string{"R-1", "E-1"}, [2]string{"R-2", "E-2"})))
	if err != nil {
		t.Fatal(err)
	}
	if len(fuExampleFails(sl)) == 0 {
		t.Error("E-2 was dropped earlier; reusing it must fail ExampleTagged")
	}
}

func TestAnAnswerThatChangesAPartNamesItInTheRevisedEntry(t *testing.T) {
	s, _, sl := twoQuestions(t) // the revision changes goals only
	got, err := s.Answer(context.Background(), sl.ID, []slices.Answer{{Question: "Q-1", Option: intp(0)}, {Question: "Q-2", Text: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"proposed", "answered", "revised"}; !equalStrings(historyKinds(got), want) {
		t.Fatalf("history %v, want %v", historyKinds(got), want)
	}
	if refs := got.History[2].Refs; !equalStrings(refs, []string{"goals"}) {
		t.Errorf("revised must name exactly the changed parts, got %v", refs)
	}
}

// ---- T037e ----

type fuPathDiff struct {
	err      error
	findings []slices.ContractFinding
}

func (d fuPathDiff) Diff(context.Context, string, string) ([]slices.ContractFinding, error) {
	return d.findings, d.err
}

type fuPathLint struct {
	err      error
	findings []slices.LintFinding
}

func (l fuPathLint) Lint(context.Context, string) ([]slices.LintFinding, error) {
	return l.findings, l.err
}

func fuService(t *testing.T, a slices.Analyst, d slices.ContractDiff, l slices.ContractLint) *slices.Service {
	t.Helper()
	schemas, err := slice.LoadSchemas("../api/preview/2026-11-12-preview")
	if err != nil {
		t.Fatal(err)
	}
	pattern, err := os.ReadFile("../../catalogue/api-feature-go/pattern.yaml")
	if err != nil {
		t.Fatal(err)
	}
	s, err := slices.New(slices.Config{Schemas: schemas, Pattern: pattern, Analyst: a, Diff: d, Lint: l})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func fuNoLocal(t *testing.T, what string, cs []slices.Check) {
	t.Helper()
	for _, c := range cs {
		for _, bad := range []string{"/home/", "/tmp/", "y.yaml", "file:", "://", "example.com", "internal.corp"} {
			if strings.Contains(c.Reason, bad) {
				t.Errorf("%s reason leaks %q: %q", what, bad, c.Reason)
			}
		}
	}
}

func fuFailed(cs []slices.Check) []slices.Check {
	var out []slices.Check
	for _, c := range cs {
		if !c.Passed {
			out = append(out, c)
		}
	}
	return out
}

func fuNoRunnerText(t *testing.T, what string, cs []slices.Check, words ...string) {
	t.Helper()
	for _, c := range cs {
		for _, w := range words {
			if strings.Contains(c.Reason, w) {
				t.Errorf("%s reason carries runner text %q: %q", what, w, c.Reason)
			}
		}
	}
}

func TestADiffRunnerErrorKeepsPathsAndURLsOutOfTheReason(t *testing.T) {
	d := fuPathDiff{err: errors.New("oasdiff: open /home/x/y.yaml: no such file; base file:///tmp/z; fetch https://internal.corp:8443/spec")}
	s := fuService(t, &fakeAnalyst{results: []analystResult{{p: notesProposal(t)}}}, d, &fakeLint{})
	sl := create(t, s, "Let me keep notes.")
	cs := fuFailed(checksNamed(sl, slices.CheckContractCompatibility))
	if len(cs) != 1 || !strings.Contains(cs[0].Reason, "could not run") {
		t.Fatalf("the check must fail saying it could not run: %+v", cs)
	}
	fuNoLocal(t, "ContractCompatibility", cs)
	fuNoRunnerText(t, "ContractCompatibility", cs, "oasdiff", "no such file", "internal", "8443", "base", "fetch")
	// The same on a later change.
	sl, err := s.ReplaceContract(context.Background(), sl.ID, strings.Replace(sl.Contract, "The notes.", "All the notes.", 1))
	if err != nil {
		t.Fatal(err)
	}
	cs = fuFailed(checksNamed(sl, slices.CheckContractCompatibility))
	if len(cs) != 1 {
		t.Fatalf("%+v", cs)
	}
	fuNoLocal(t, "ContractCompatibility after a change", cs)
	fuNoRunnerText(t, "ContractCompatibility after a change", cs, "oasdiff", "no such file", "internal", "8443")
}

func TestADiffFindingTextKeepsPathsOutOfTheReason(t *testing.T) {
	d := fuPathDiff{findings: []slices.ContractFinding{{Level: "error", ID: "response-removed",
		Text: "removed the response at /home/x/y.yaml (file:///tmp/z, https://example.com/a)"}}}
	s := fuService(t, &fakeAnalyst{results: []analystResult{{p: notesProposal(t)}}}, d, &fakeLint{})
	sl := create(t, s, "Let me keep notes.")
	cs := fuFailed(checksNamed(sl, slices.CheckContractCompatibility))
	if len(cs) != 1 || !strings.Contains(cs[0].Reason, "response-removed") {
		t.Fatalf("the finding fails the check and names its ID: %+v", cs)
	}
	fuNoLocal(t, "ContractCompatibility finding", cs)
}

func TestALintRunnerErrorKeepsPathsAndURLsOutOfTheReason(t *testing.T) {
	l := fuPathLint{err: errors.New("spectral: ENOENT /home/x/y.yaml; ruleset file:///tmp/z; https://example.com/ruleset.json")}
	s := fuService(t, &fakeAnalyst{results: []analystResult{{p: notesProposal(t)}}}, &fakeDiff{}, l)
	sl := create(t, s, "Let me keep notes.")
	cs := fuFailed(checksNamed(sl, slices.CheckContractLint))
	if len(cs) != 1 || !strings.Contains(cs[0].Reason, "could not run") {
		t.Fatalf("the check must fail saying it could not run: %+v", cs)
	}
	fuNoLocal(t, "ContractLint", cs)
	fuNoRunnerText(t, "ContractLint", cs, "spectral", "ENOENT", "ruleset", "example")
}

func TestALintFindingTextKeepsPathsOutOfTheReason(t *testing.T) {
	l := fuPathLint{findings: []slices.LintFinding{
		{Level: "error", Code: "operation-operationId", Text: "/home/x/y.yaml#/paths/~1notes: missing; see file:///tmp/z and https://example.com/rules"}}}
	s := fuService(t, &fakeAnalyst{results: []analystResult{{p: notesProposal(t)}}}, &fakeDiff{}, l)
	sl := create(t, s, "Let me keep notes.")
	cs := fuFailed(checksNamed(sl, slices.CheckContractLint))
	if len(cs) != 1 || !strings.Contains(cs[0].Reason, "operation-operationId") {
		t.Fatalf("the finding fails the check and names its rule: %+v", cs)
	}
	fuNoLocal(t, "ContractLint finding", cs)
}

// ---- T040b: the lock ----

// fuGate blocks the call whose contract holds the marker until released.
type fuGate struct {
	marker  string
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func newGate(marker string) *fuGate {
	return &fuGate{marker: marker, entered: make(chan struct{}, 8), release: make(chan struct{})}
}

func (g *fuGate) open() { g.once.Do(func() { close(g.release) }) }

func (g *fuGate) wait(ctx context.Context, contract string) {
	if !strings.Contains(contract, g.marker) {
		return
	}
	g.entered <- struct{}{}
	select {
	case <-g.release:
	case <-ctx.Done():
	}
}

type fuSlowLint struct{ g *fuGate }

func (l fuSlowLint) Lint(ctx context.Context, c string) ([]slices.LintFinding, error) {
	l.g.wait(ctx, c)
	return nil, nil
}

type fuSlowDiff struct{ g *fuGate }

func (d fuSlowDiff) Diff(ctx context.Context, _, c string) ([]slices.ContractFinding, error) {
	d.g.wait(ctx, c)
	return nil, nil
}

func within(t *testing.T, what string, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { f(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("%s did not complete within 2s while another slice's contract change waited in oasdiff or lint", what)
	}
}

func fuSlowChange(t *testing.T, mk func(g *fuGate) *slices.Service, g *fuGate) {
	t.Helper()
	ctx := context.Background()
	s := mk(g)
	a := create(t, s, "Let me keep notes.")
	b := create(t, s, "Let me keep other notes.")
	newContract := strings.Replace(a.Contract, "The notes.", "All the notes. SLOW", 1)
	defer g.open()

	type res struct {
		sl  slices.Slice
		err error
	}
	slow := make(chan res, 1)
	go func() { sl, err := s.ReplaceContract(ctx, a.ID, newContract); slow <- res{sl, err} }()
	select {
	case <-g.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the slow runner was never called")
	}

	within(t, "Get of another slice", func() {
		if _, err := s.Get(b.ID); err != nil {
			t.Error(err)
		}
	})
	within(t, "List", func() {
		if n := len(s.List()); n != 2 {
			t.Errorf("List = %d slices", n)
		}
	})
	within(t, "ReplaceGoals of another slice", func() {
		if _, err := s.ReplaceGoals(ctx, b.ID, []slices.Goal{{ID: "G-1", Text: "Keep"}, {ID: "G-2", Text: "List"}}); err != nil {
			t.Error(err)
		}
	})
	within(t, "Get of the changing slice", func() {
		if got, err := s.Get(a.ID); err != nil || got.Contract != a.Contract {
			t.Errorf("until the change lands the slice is unchanged: %v", err)
		}
	})
	// A concurrent change to the same slice's goals must not be lost either.
	goalsDone := make(chan error, 1)
	go func() {
		_, err := s.ReplaceGoals(ctx, a.ID, []slices.Goal{{ID: "G-1", Text: "Keep it"}, {ID: "G-2", Text: "List it"}})
		goalsDone <- err
	}()

	g.open()
	r := <-slow
	if r.err != nil {
		t.Fatal(r.err)
	}
	if err := <-goalsDone; err != nil {
		t.Fatalf("a change to the same slice must wait, not be refused or lost (replaceContract/replaceGoals list no conflict code): %v", err)
	}
	fin, err := s.Get(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fin.Contract != newContract || fin.Goals[0].Text != "Keep it" {
		t.Errorf("both changes must land: contract changed %v, goals %q", fin.Contract == newContract, fin.Goals[0].Text)
	}
	changed := 0
	for _, h := range fin.History {
		if h.Kind == "changed" {
			changed++
		}
	}
	if changed != 2 {
		t.Errorf("history %v must hold two changed entries", historyKinds(fin))
	}
}

func TestASlowLintDoesNotBlockOtherSlices(t *testing.T) {
	g := newGate("SLOW")
	fuSlowChange(t, func(g *fuGate) *slices.Service {
		return fuService(t, &fakeAnalyst{results: []analystResult{{p: notesProposal(t)}}}, &fakeDiff{}, fuSlowLint{g})
	}, g)
}

func TestASlowDiffDoesNotBlockOtherSlices(t *testing.T) {
	g := newGate("SLOW")
	fuSlowChange(t, func(g *fuGate) *slices.Service {
		return fuService(t, &fakeAnalyst{results: []analystResult{{p: notesProposal(t)}}}, fuSlowDiff{g}, &fakeLint{})
	}, g)
}

func TestTwoConcurrentContractChangesToOneSliceLoseNeither(t *testing.T) {
	ctx := context.Background()
	s, _, _ := setup(t)
	a := create(t, s, "Let me keep notes.")
	c2 := strings.Replace(a.Contract, "The notes.", "Notes two.", 1)
	c3 := strings.Replace(a.Contract, "The notes.", "Notes three.", 1)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, c := range []string{c2, c3} {
		wg.Add(1)
		go func() { defer wg.Done(); _, errs[i] = s.ReplaceContract(ctx, a.ID, c) }()
	}
	wg.Wait()
	ok := 0
	for _, err := range errs {
		if err == nil {
			ok++
			continue
		}
		var e *slices.Error
		if !errors.As(err, &e) || (e.Code != "RunActive" && e.Code != "InvalidRequest") {
			t.Errorf("a refusal must carry a code replaceContract lists: %v", err)
		}
	}
	fin, _ := s.Get(a.ID)
	changed := 0
	for _, h := range fin.History {
		if h.Kind == "changed" {
			changed++
		}
	}
	if changed != ok || ok == 0 {
		t.Errorf("%d changes succeeded but history holds %d changed entries", ok, changed)
	}
	if fin.Contract != c2 && fin.Contract != c3 {
		t.Errorf("the contract must be one of the two written")
	}
	if ok == 2 && len(checksNamed(fin, slices.CheckContractCompatibility)) == 0 {
		t.Error("checks must follow the final contract")
	}
}

// ---- the example-ID sequence across a revision whose examples are not Gherkin ----

func fuNotesExamples(t *testing.T, sl slices.Slice) {
	t.Helper()
	if !strings.Contains(sl.Examples, "@E-1") || !strings.Contains(sl.Examples, "@E-2") {
		t.Fatalf("the notes proposal is expected to carry E-1 and E-2: %q", sl.Examples)
	}
}

func fuSequenceScenario(t *testing.T, s *slices.Service, sl slices.Slice, restart func() *slices.Service) {
	t.Helper()
	ctx := context.Background()
	fuNotesExamples(t, sl)
	// Revision whose examples are not Gherkin: the check fails, nothing is dropped yet.
	sl, err := s.ReplaceRules(ctx, sl.ID, fuRules(sl, "these are not Gherkin examples"))
	if err != nil {
		t.Fatal(err)
	}
	if len(fuExampleFails(sl)) == 0 {
		t.Fatal("examples that are not Gherkin must fail ExampleTagged")
	}
	// Valid again with E-1 only: E-2 is recorded dropped.
	sl, err = s.ReplaceRules(ctx, sl.ID, fuRules(sl, fuExamples([2]string{"R-1", "E-1"})))
	if err != nil {
		t.Fatal(err)
	}
	if f := fuExampleFails(sl); len(f) != 0 {
		t.Fatalf("E-1 alone must pass: %+v", f)
	}
	if restart != nil {
		s = restart()
	}
	// A later revision reusing E-2 for another scenario fails.
	sl, err = s.ReplaceRules(ctx, sl.ID, fuRules(sl, fuExamples([2]string{"R-1", "E-1"}, [2]string{"R-1", "E-2"})))
	if err != nil {
		t.Fatal(err)
	}
	f := fuExampleFails(sl)
	if len(f) == 0 || !strings.Contains(f[0].Reason, "E-2") {
		t.Fatalf("E-2 was dropped after the non-Gherkin revision; reusing it must fail naming E-2: %+v", f)
	}
}

func TestExampleIDDroppedAfterANonGherkinRevisionIsNotReused(t *testing.T) {
	s, _, _ := setup(t)
	fuSequenceScenario(t, s, create(t, s, "Let me keep notes."), nil)
}

func TestExampleIDDroppedAfterANonGherkinRevisionStaysDroppedAcrossARestart(t *testing.T) {
	rec := newMemRecords()
	s := serviceOn(t, rec)
	fuSequenceScenario(t, s, create(t, s, "Let me keep notes."), func() *slices.Service { return serviceOn(t, rec) })
}

// ---- the engine logs the runner's error; the reason does not carry it ----

type fuLogBuf struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *fuLogBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}
func (l *fuLogBuf) String() string { l.mu.Lock(); defer l.mu.Unlock(); return l.b.String() }

func TestRunnerErrorsGoToTheConfiguredLoggerInFull(t *testing.T) {
	schemas, err := slice.LoadSchemas("../api/preview/2026-11-12-preview")
	if err != nil {
		t.Fatal(err)
	}
	pattern, err := os.ReadFile("../../catalogue/api-feature-go/pattern.yaml")
	if err != nil {
		t.Fatal(err)
	}
	const diffErr = "oasdiff: open /home/x/y.yaml: no such file"
	const lintErr = "spectral: ENOENT /home/x/ruleset.json"
	buf := &fuLogBuf{}
	s, err := slices.New(slices.Config{Schemas: schemas, Pattern: pattern,
		Analyst: &fakeAnalyst{results: []analystResult{{p: notesProposal(t)}}},
		Diff:    fuPathDiff{err: errors.New(diffErr)}, Lint: fuPathLint{err: errors.New(lintErr)},
		Log: slog.New(slog.NewTextHandler(buf, nil))})
	if err != nil {
		t.Fatal(err)
	}
	sl := create(t, s, "Let me keep notes.")
	out := buf.String()
	// The text handler may quote and escape; compare on the distinctive words.
	for _, w := range []string{"oasdiff", "no such file", "/home/x/y.yaml", "spectral", "ENOENT", "/home/x/ruleset.json"} {
		if !strings.Contains(out, w) {
			t.Errorf("the log must carry the runner's full error; missing %q in %q", w, out)
		}
	}
	for _, c := range fuFailed(sl.Checks) {
		for _, w := range []string{"oasdiff", "spectral", "ENOENT", "no such file"} {
			if strings.Contains(c.Reason, w) {
				t.Errorf("reason carries %q: %q", w, c.Reason)
			}
		}
	}
}
