// Review tests for package slices (T037 review). They add to slices_test.go (T029) and change
// nothing in it. Each case comes from the contract (openapi.yaml, slice-defs.schema.json), the
// spec and data-model.md, not from the implementation.
//
// Decisions where the contract is not explicit (also marked RESOLVED at the test):
//   - approvePart's description limits PatternDoesNotFit to "for the contract, the pattern does not
//     fit". A pattern condition that fails on the rules part is a check failing on that part, so
//     approving it gives PartFailsChecks with the condition in `failures`.
//   - Pattern nil: no pattern was configured, so fit is unknown, and unknown must not pass
//     (constitution: deterministic checks decide; the check fails closed).
//   - A malformed slice ID, malformed hash or unknown part is InvalidRequest, as the API's path and
//     body schemas would answer.
//
// The Proposal type is typed, so "an unknown field" cannot be written in a fake analyst. The
// unknown-field case is covered by a value the schema rejects instead (a rule with an unknown
// pattern) and is listed in the report.
package slices_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentic-platform/engine/slices"
	"github.com/agentic-platform/slice"
)

// ---- goroutine-safe fakes and helpers (the T029 fakes are not) ----

type rvAnalyst struct {
	mu sync.Mutex
	p  slices.Proposal
	n  int
}

func (a *rvAnalyst) Propose(context.Context, slices.AnalystInput) (slices.Proposal, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.n++
	return a.p, nil
}

type rvDiff struct {
	mu       sync.Mutex
	onCall   func(base, revision string) // runs without the lock held; may block
	findings func(base, revision string) []slices.ContractFinding
}

func (d *rvDiff) Diff(_ context.Context, base, revision string) ([]slices.ContractFinding, error) {
	d.mu.Lock()
	on, f := d.onCall, d.findings
	d.mu.Unlock()
	if on != nil {
		on(base, revision)
	}
	if f == nil {
		return nil, nil
	}
	return f(base, revision), nil
}

func rvService(t *testing.T, p slices.Proposal, d slices.ContractDiff, pattern []byte) *slices.Service {
	t.Helper()
	schemas, err := slice.LoadSchemas("../api/preview/2026-11-12-preview")
	if err != nil {
		t.Fatal(err)
	}
	if d == nil {
		d = &rvDiff{}
	}
	s, err := slices.New(slices.Config{Schemas: schemas, Analyst: &rvAnalyst{p: p}, Diff: d, Pattern: pattern})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func rvPattern(t *testing.T, name string) []byte {
	t.Helper()
	return fixture(t, name)
}

func rvHash(s slices.Slice, part slices.Part) string {
	return map[slices.Part]string{slices.PartGoals: s.Parts.Goals, slices.PartRules: s.Parts.Rules, slices.PartContract: s.Parts.Contract}[part]
}

func rvAsError(t *testing.T, err error) *slices.Error {
	t.Helper()
	var e *slices.Error
	if !errors.As(err, &e) {
		t.Fatalf("want *slices.Error, got %T: %v", err, err)
	}
	return e
}

// ---- (1) ContradictionOpen ----

// Contract: approvePart is refused "while ... an open question concerns it" (ContradictionOpen).
// A question concerns a part when its `about` is an ID inside it (the schema's question_about
// does not allow a bare part name): goals hold
// G-n; rules hold R-n, T-n (terms are part of rules) and R-n/E-n; the contract holds
// contract/<operationId>.
func TestApprovingAPartAnOpenQuestionConcernsIsRefused(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		about string
		part  slices.Part
	}{
		{"G-1", slices.PartGoals}, {"G-2", slices.PartGoals},
		{"R-1", slices.PartRules}, {"T-1", slices.PartRules}, {"R-2/E-2", slices.PartRules},
		{"contract/listNotes", slices.PartContract},
	}
	for _, c := range cases {
		t.Run(c.about, func(t *testing.T) {
			p := notesProposal(t)
			p.Questions = []slices.QuestionDraft{{About: c.about, Text: "Is this meant?"}}
			s := rvService(t, p, nil, rvPattern(t, "pattern-contract-only.yaml"))
			sl := create(t, s, "Let me keep notes.")
			for _, part := range []slices.Part{slices.PartGoals, slices.PartRules, slices.PartContract} {
				_, err := s.Approve(ctx, sl.ID, part, rvHash(sl, part))
				if part == c.part {
					if got := rvAsError(t, err).Code; got != "ContradictionOpen" {
						t.Errorf("approve %s with an open question about %s: code %q, want ContradictionOpen", part, c.about, got)
					}
				} else if err != nil {
					t.Errorf("approve %s, which no question concerns: %v", part, err)
				}
			}
			if got, _ := s.Get(sl.ID); func() bool { _, ok := approval(got, c.part); return ok }() {
				t.Errorf("%s was approved with a question open", c.part)
			}
		})
	}
}

// ---- (2) TooManyQuestions ----

// Contract: createSlice lists TooManyQuestions as an operation error ("the slice would need more
// than 10 questions"); slice-proposal.schema.json allows at most 10 new questions.
func TestElevenQuestionsIsTooManyQuestions(t *testing.T) {
	mk := func(n int) slices.Proposal {
		p := notesProposal(t)
		for i := 0; i < n; i++ {
			p.Questions = append(p.Questions, slices.QuestionDraft{About: "R-1", Text: fmt.Sprintf("Question %d?", i+1)})
		}
		return p
	}
	s := rvService(t, mk(11), nil, nil)
	_, err := s.Create(context.Background(), "Let me keep notes.")
	if err == nil {
		t.Fatal("11 questions were accepted")
	}
	if got := rvAsError(t, err).Code; got != "TooManyQuestions" {
		t.Errorf("code %q, want TooManyQuestions", got)
	}
	if _, err := s.Get("S-1"); err == nil {
		t.Error("a slice was stored for a refused proposal")
	}
	// The boundary: 10 questions are fine, and get Q-1..Q-10.
	ok := rvService(t, mk(10), nil, nil)
	sl := create(t, ok, "Let me keep notes.")
	if len(sl.Questions) != 10 || sl.Questions[9].ID != "Q-10" {
		t.Errorf("10 questions: %+v", sl.Questions)
	}
}

// ---- (3) no Pattern configured ----

// RESOLVED: with no pattern the fit is unknown. Approving the contract must not pass unchecked:
// either a failed PatternFit result is recorded or the approval is refused.
// CONFLICT with slices_test.go: its setup configures no Pattern and approves contracts, so those
// tests need the owner's approval to change (see the report).
func TestNilPatternDoesNotLetTheContractThroughUnchecked(t *testing.T) {
	s := rvService(t, notesProposal(t), nil, nil)
	sl := create(t, s, "Let me keep notes.")
	var failed bool
	for _, c := range sl.Checks {
		if c.Check == "PatternFit" && !c.Passed {
			failed = true
		}
	}
	_, err := s.Approve(context.Background(), sl.ID, slices.PartContract, sl.Parts.Contract)
	if err == nil && !failed {
		t.Error("the contract was approved with no pattern configured and no failed PatternFit result")
	}
}

// Positive control: with a pattern whose conditions the contract meets, it can be approved, so the
// test above is not satisfied by refusing everything.
func TestContractMeetingThePatternCanBeApproved(t *testing.T) {
	s := rvService(t, notesProposal(t), nil, rvPattern(t, "pattern-contract-only.yaml"))
	sl := create(t, s, "Let me keep notes.")
	if _, err := s.Approve(context.Background(), sl.ID, slices.PartContract, sl.Parts.Contract); err != nil {
		t.Errorf("approve: %v", err)
	}
}

// ---- (4) a pattern condition that fails on the rules part ----

// RESOLVED: see the header. The failure is listed under the rules part and names the condition.
func TestApprovingRulesThatFailOnlyAPatternConditionIsPartFailsChecks(t *testing.T) {
	p := notesProposal(t)
	p.Rules[1].System = "another service" // R-1 and R-2 no longer name one system
	s := rvService(t, p, nil, rvPattern(t, "pattern-one-system.yaml"))
	sl := create(t, s, "Let me keep notes.")
	_, err := s.Approve(context.Background(), sl.ID, slices.PartRules, sl.Parts.Rules)
	e := rvAsError(t, err)
	if e.Code != "PartFailsChecks" {
		t.Errorf("code %q, want PartFailsChecks (PatternDoesNotFit is for approving the contract)", e.Code)
	}
	found := false
	for _, f := range e.Failures {
		if f.Part == "rules" && f.Check == "RulesNameOneSystem" && !f.Passed {
			found = true
		}
	}
	if !found {
		t.Errorf("failures %+v do not list the failed RulesNameOneSystem on rules", e.Failures)
	}
	// Goals have no pattern condition on them: still approvable.
	if _, err := s.Approve(context.Background(), sl.ID, slices.PartGoals, sl.Parts.Goals); err != nil {
		t.Errorf("approve goals: %v", err)
	}
}

// ---- (5) canonical JSON through the public API ----

// RFC 8785 section 3.2.2.2 fixes string serialisation: the escapes \b \t \n \f \r \" \\ for those
// characters, \u00xx with lower-case hex for any other character below U+0020, and every other
// character (DEL U+007F, non-ASCII, non-BMP, U+2028) as itself in UTF-8. Go's encoding/json
// differs on U+2028/2029 and on < > &, which is why the canonical bytes are written by hand here.
// (The RFC's published vectors are number and key-order vectors, 3.2.2.3 and 3.2.3; the key order
// cannot be varied through this API because every key is fixed by the contract's schemas, which
// the T029 fixture covers with sorted keys.)
func TestCanonicalStringEscapesThroughTheAPI(t *testing.T) {
	ctx := context.Background()
	text := "a\b\t\n\f\r\"\\\x01\x1f\x7f😀é /"
	// hand-written: \b \t \n \f \r \" \\ short forms; \u0001 \u001f; DEL, emoji, é, U+2028 raw; "/" not escaped.
	want := `{"goals":[{"id":"G-1","text":"a\b\t\n\f\r\"\\\u0001\u001f` + "\x7f😀é /" + `"}]}`

	s := rvService(t, notesProposal(t), nil, nil)
	sl := create(t, s, "Let me keep notes.")
	got, err := s.ReplaceGoals(ctx, sl.ID, []slices.Goal{{ID: "G-1", Text: text}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Parts.Goals != sha(want) {
		t.Errorf("goals hash %s, want sha256 of %q", got.Parts.Goals, want)
	}

	// The same rules in the contract part: the whole YAML text is one JSON string.
	contract := "openapi: \"3.1.0\"\r\n\tkey: \\ \x00\x7f😀"
	wantC := `{"contract":"openapi: \"3.1.0\"\r\n\tkey: \\ \u0000` + "\x7f😀" + `"}`
	c, err := s.ReplaceContract(ctx, sl.ID, contract)
	if err != nil {
		t.Fatal(err)
	}
	if c.Parts.Contract != sha(wantC) {
		t.Errorf("contract hash %s, want sha256 of %q", c.Parts.Contract, wantC)
	}
}

func TestInvalidUTF8InAPartIsInvalidRequest(t *testing.T) {
	ctx := context.Background()
	s := rvService(t, notesProposal(t), nil, nil)
	sl := create(t, s, "Let me keep notes.")
	for _, bad := range []string{"\xff", "ab\xc3", "\xed\xa0\x80" /* a lone surrogate */, "\xc0\xaf" /* overlong */} {
		goals := []slices.Goal{{ID: "G-1", Text: "x" + bad}}
		rules := slices.RulesPart{Rules: sl.Rules, Examples: sl.Examples + bad, Terms: sl.Terms}
		for name, call := range map[string]func() (slices.Slice, error){
			"goals":    func() (slices.Slice, error) { return s.ReplaceGoals(ctx, sl.ID, goals) },
			"rules":    func() (slices.Slice, error) { return s.ReplaceRules(ctx, sl.ID, rules) },
			"contract": func() (slices.Slice, error) { return s.ReplaceContract(ctx, sl.ID, "openapi: 3.1.0"+bad) },
		} {
			_, err := call()
			if err == nil {
				t.Errorf("%s with %q accepted", name, bad)
				continue
			}
			if got := rvAsError(t, err).Code; got != "InvalidRequest" {
				t.Errorf("%s with %q: code %q, want InvalidRequest", name, bad, got)
			}
		}
	}
	after, _ := s.Get(sl.ID)
	if after.Parts != sl.Parts {
		t.Errorf("refused changes altered the slice: %+v -> %+v", sl.Parts, after.Parts)
	}
}

// ---- (6) the analyst's proposal must match the proposal schema ----

func TestProposalBreakingTheSchemaMakesCreateFailAndStoresNothing(t *testing.T) {
	cases := map[string]func(p *slices.Proposal){
		"no goals (missing required content)": func(p *slices.Proposal) { p.Goals = nil },
		"no rules":                            func(p *slices.Proposal) { p.Rules = nil },
		"empty contract":                      func(p *slices.Proposal) { p.Contract = "" },
		"empty examples":                      func(p *slices.Proposal) { p.Examples = "" },
		"event rule without trigger":          func(p *slices.Proposal) { p.Rules[0].Trigger = "" },
		"unknown rule pattern":                func(p *slices.Proposal) { p.Rules[0].Pattern = "sometimes" },
		"bad goal id":                         func(p *slices.Proposal) { p.Goals[0].ID = "X-1" },
		"question about nothing valid":        func(p *slices.Proposal) { p.Questions = []slices.QuestionDraft{{About: "everything", Text: "?"}} },
		"six options": func(p *slices.Proposal) {
			p.Questions = []slices.QuestionDraft{{About: "R-1", Text: "?", Options: []string{"1", "2", "3", "4", "5", "6"}}}
		},
		"invalid UTF-8": func(p *slices.Proposal) { p.Goals[0].Text = "a\xff" },
	}
	for name, mut := range cases {
		t.Run(name, func(t *testing.T) {
			p := notesProposal(t)
			mut(&p)
			s := rvService(t, p, nil, nil)
			_, err := s.Create(context.Background(), "Let me keep notes.")
			if err == nil {
				t.Fatal("an invalid proposal was accepted")
			}
			var e *slices.Error
			if errors.As(err, &e) && e.Code == "NarrowDescription" {
				t.Errorf("an invalid proposal is not a narrowing request: %v", err)
			}
			if _, err := s.Get("S-1"); err == nil {
				t.Error("a slice was stored")
			}
		})
	}
}

// ---- (7) malformed identifiers ----

func TestMalformedIDHashAndPartAreInvalidRequest(t *testing.T) {
	ctx := context.Background()
	s := rvService(t, notesProposal(t), nil, nil)
	sl := create(t, s, "Let me keep notes.")
	good := sl.Parts.Goals
	expect := func(what string, err error) {
		t.Helper()
		if err == nil {
			t.Errorf("%s: accepted", what)
			return
		}
		if got := rvAsError(t, err).Code; got != "InvalidRequest" {
			t.Errorf("%s: code %q, want InvalidRequest", what, got)
		}
	}
	for _, id := range []string{"", "x", "S-", "s-1", "S-1 ", "S-01x", "../S-1", "S-1/../S-2", "RUN-1"} {
		_, err := s.Approve(ctx, id, slices.PartGoals, good)
		expect(fmt.Sprintf("approve id %q", id), err)
		_, err = s.ReplaceGoals(ctx, id, sl.Goals)
		expect(fmt.Sprintf("replaceGoals id %q", id), err)
		_, err = s.ReplaceRules(ctx, id, slices.RulesPart{Rules: sl.Rules, Examples: sl.Examples, Terms: sl.Terms})
		expect(fmt.Sprintf("replaceRules id %q", id), err)
		_, err = s.ReplaceContract(ctx, id, sl.Contract)
		expect(fmt.Sprintf("replaceContract id %q", id), err)
	}
	for _, h := range []string{"", "abc", strings.Repeat("a", 63), strings.Repeat("a", 65), strings.ToUpper(good), strings.Repeat("g", 64), "sha256:" + good} {
		_, err := s.Approve(ctx, sl.ID, slices.PartGoals, h)
		expect(fmt.Sprintf("approve hash %q", h), err)
	}
	for _, p := range []slices.Part{"", "Goals", "notes", "goals ", "R-1"} {
		_, err := s.Approve(ctx, sl.ID, p, good)
		expect(fmt.Sprintf("approve part %q", p), err)
	}
	if got, _ := s.Get(sl.ID); len(got.Approvals) != 0 {
		t.Errorf("a malformed request recorded an approval: %+v", got.Approvals)
	}
	// A well-formed ID that does not exist stays NotFound, not InvalidRequest.
	_, err := s.Approve(ctx, "S-999", slices.PartGoals, good)
	if got := rvAsError(t, err).Code; got != "NotFound" {
		t.Errorf("unknown slice: code %q, want NotFound", got)
	}
}

// ---- (8) concurrency (run with -race) ----

func TestConcurrentCreatesGetUniqueIDs(t *testing.T) {
	s := rvService(t, notesProposal(t), nil, nil)
	const n = 40
	ids := make(chan string, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := s.Create(context.Background(), fmt.Sprintf("description %d", i))
			if err != nil {
				t.Error(err)
				return
			}
			ids <- r.Slice.ID
		}()
	}
	wg.Wait()
	close(ids)
	seen := map[string]bool{}
	for id := range ids {
		if seen[id] {
			t.Errorf("ID %s given twice", id)
		}
		seen[id] = true
		if _, err := s.Get(id); err != nil {
			t.Errorf("Get(%s): %v", id, err)
		}
	}
	if len(seen) != n {
		t.Errorf("%d slices, want %d", len(seen), n)
	}
}

// rvInvariant: an approval holds only while the part's hash equals its content hash
// (data-model.md, Approval), and an approved slice has all three.
func rvInvariant(t *testing.T, s slices.Slice) {
	t.Helper()
	for _, a := range s.Approvals {
		if a.ContentHash != rvHash(s, a.Part) {
			t.Errorf("approval of %s at %s but the part is at %s", a.Part, a.ContentHash, rvHash(s, a.Part))
		}
	}
	if (s.State == "approved") != (len(s.Approvals) == 3) {
		t.Errorf("state %q with approvals %+v", s.State, s.Approvals)
	}
}

func TestConcurrentReplaceAndApproveLeaveNoApprovalOfOldContent(t *testing.T) {
	ctx := context.Background()
	s := rvService(t, notesProposal(t), nil, nil)
	sl := create(t, s, "Let me keep notes.")
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, err := s.ReplaceGoals(ctx, sl.ID, []slices.Goal{sl.Goals[0], {ID: "G-2", Text: fmt.Sprintf("variant %d", i)}})
			if err != nil {
				t.Error(err)
			}
		}()
		go func() {
			defer wg.Done()
			cur, err := s.Get(sl.ID)
			if err != nil {
				t.Error(err)
				return
			}
			_, err = s.Approve(ctx, sl.ID, slices.PartGoals, cur.Parts.Goals)
			if err != nil {
				if c := rvAsError(t, err).Code; c != "StaleContentHash" {
					t.Errorf("approve: %v", err)
				}
			}
		}()
	}
	wg.Wait()
	final, _ := s.Get(sl.ID)
	rvInvariant(t, final)
}

// A replace whose checks are slow (the oasdiff call) must not let an approval at the old hash
// through after the replace is done, and must leave a consistent slice.
func TestApprovalAtTheOldHashAfterAConcurrentReplaceFails(t *testing.T) {
	ctx := context.Background()
	v1 := string(fixture(t, "contract-notes-v1.yaml"))
	entered, gate := make(chan struct{}), make(chan struct{})
	var once sync.Once
	d := &rvDiff{onCall: func(base, rev string) {
		if rev == v1 {
			once.Do(func() { close(entered) })
			<-gate
		}
	}}
	s := rvService(t, notesProposal(t), d, rvPattern(t, "pattern-contract-only.yaml"))
	sl := create(t, s, "Let me keep notes.")
	old := sl.Parts.Contract

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if _, err := s.ReplaceContract(ctx, sl.ID, v1); err != nil {
			t.Error(err)
		}
	}()
	<-entered
	go func() { // the developer approves what they reviewed while the replace is being checked
		defer wg.Done()
		_, err := s.Approve(ctx, sl.ID, slices.PartContract, old)
		if err != nil {
			if c := rvAsError(t, err).Code; c != "StaleContentHash" {
				t.Errorf("approve during replace: %v", err)
			}
		}
	}()
	time.Sleep(100 * time.Millisecond)
	close(gate)
	wg.Wait()

	final, _ := s.Get(sl.ID)
	rvInvariant(t, final)
	if final.Parts.Contract == old {
		t.Fatal("the replace did not take effect")
	}
	_, err := s.Approve(ctx, sl.ID, slices.PartContract, old)
	if got := rvAsError(t, err).Code; got != "StaleContentHash" {
		t.Errorf("approve at the old hash after the replace: code %q, want StaleContentHash", got)
	}
	if _, err := s.Approve(ctx, sl.ID, slices.PartContract, final.Parts.Contract); err != nil {
		t.Errorf("approve at the new hash: %v", err)
	}
}
