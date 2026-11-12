// Tests for the engine's slice logic (task T029). They fail until T037 implements package
// slices (import path github.com/agentic-platform/engine/slices): the package does not exist yet.
//
// The analyst (a model) and oasdiff are outside the package, so it takes two small interfaces and
// the tests use fakes. Spectral, the pattern-fit check (T035) and the real analyst agent are not
// exercised here. The package runs slice.Trace (T033) itself for the link checks; the tests rely
// on it for "a goal has no rule" and "a rule has no scenario".
//
// Minimal API the tests call (package slices). Reasons come from data-model.md and the contract
// (openapi.yaml, slice-defs.schema.json).
//
//	// Part names are the contract's `part` enum.
//	type Part string
//	const (PartGoals Part = "goals"; PartRules Part = "rules"; PartContract Part = "contract")
//
//	// Record types carry the contract's JSON names (slice-defs.schema.json) as json tags, so a
//	// proposal fixture in the analyst's output format unmarshals into Proposal. Rule's
//	// precondition, trigger and feature are omitempty. A nil Terms slice (in a Rule or on its
//	// own) is the empty list.
//	type Goal struct{ ID, Text string }                      // id, text
//	type Term struct{ ID, Name, Definition string }          // id, name, definition
//	type Rule struct{ ID, Goal, Pattern, Precondition, Trigger, Feature, System, Response string; Terms []string }
//	type RulesPart struct{ Rules []Rule; Examples string; Terms []Term } // data-model: rules covers rules, examples, terms
//	type QuestionDraft struct{ About, Text string; Options []string }
//	type Proposal struct {
//	    Goals []Goal; Terms []Term; Rules []Rule; Examples, Contract string
//	    Questions []QuestionDraft
//	    Model, AgentDigest string // json "model", "agent_digest": history names them (data-model)
//	}
//
//	// The analyst: the model behind the analyst agent. Input is the description; Current and
//	// Questions are set when revising after answers (later task, not tested here).
//	type AnalystInput struct{ Description string; Current *Proposal; Questions []Question }
//	type Analyst interface{ Propose(ctx context.Context, in AnalystInput) (Proposal, error) }
//	// An analyst that finds more than one use case returns ErrMultipleUseCases (the proposal
//	// schema has no field to say it); the service answers NarrowDescription.
//	var ErrMultipleUseCases error
//
//	// oasdiff. Diff compares revision with base; base is "" (an empty document) when no
//	// contract of the slice was approved yet (research D4: compatibility baseline). Level is
//	// "error", "warn" or "info"; only "error" fails the check.
//	type ContractFinding struct{ Level, ID, Text string }
//	type ContractDiff interface{ Diff(ctx context.Context, base, revision string) ([]ContractFinding, error) }
//
//	type Config struct {
//	    Schemas *slice.Schemas // contract rules for IDs and the proposal schema
//	    Analyst Analyst
//	    Diff    ContractDiff
//	    Now     func() time.Time // nil means time.Now
//	}
//	func New(cfg Config) (*Service, error) // error for a nil Schemas, Analyst or Diff
//
//	// Create proposes a slice (createSlice). Result.Reused is true, and Slice is the existing
//	// slice, when the same description was already proposed AND that slice is approved (spec
//	// edge case: "the previously approved slice is offered for reuse"); the analyst is not run.
//	type Result struct{ Slice Slice; Reused bool }
//	func (s *Service) Create(ctx context.Context, description string) (Result, error)
//	func (s *Service) Get(id string) (Slice, error)       // *Error{Code:"NotFound"} when unknown
//	func (s *Service) ReplaceGoals(ctx context.Context, id string, goals []Goal) (Slice, error)
//	func (s *Service) ReplaceRules(ctx context.Context, id string, p RulesPart) (Slice, error)
//	func (s *Service) ReplaceContract(ctx context.Context, id, contract string) (Slice, error)
//	func (s *Service) Approve(ctx context.Context, id string, part Part, contentHash string) (Slice, error)
//
//	// Checks never make Create or Replace* fail: a slice that breaks a check is stored with
//	// the failed Check in Checks (FR-005, data-model `checks[]`), and Approve refuses the
//	// failing part. An error return means the call could not be carried out.
//	type Check struct{ Part, Check string; Passed bool; Reason string } // check_result; Part is a part_ref
//	const CheckContractCompatibility = "ContractCompatibility"          // check_name for the oasdiff check
//	type Parts struct{ Goals, Rules, Contract string }                  // current hashes, lowercase hex SHA-256
//	type Approval struct{ Part Part; ContentHash string }
//	type Question struct{ ID, About, Text string; Options []string }
//	type HistoryEntry struct{ Kind, Model, AgentDigest string; Refs []string }
//	type Slice struct {
//	    ID, Description, Contract, Examples, State string
//	    Goals []Goal; Terms []Term; Rules []Rule
//	    Questions []Question; Parts Parts; Approvals []Approval; Checks []Check; History []HistoryEntry
//	}
//
//	// Error is the API's problem in domain form. Code is an openapi.yaml ErrorCode
//	// (NarrowDescription, StaleContentHash, PartFailsChecks, NotFound); Failures lists the
//	// failing checks of PartFailsChecks. Callers use errors.As.
//	type Error struct{ Code, Detail string; Failures []Check }
//	func (e *Error) Error() string
//
// Decisions taken where the spec is silent are listed in the report that came with this file;
// each is also marked "RESOLVED" at its test.
package slices_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentic-platform/engine/slices"
	"github.com/agentic-platform/slice"
)

// ---- fakes and helpers ----

type analystResult struct {
	p   slices.Proposal
	err error
}

type fakeAnalyst struct {
	results []analystResult // one per call, in order; the last repeats
	calls   int
	inputs  []slices.AnalystInput
}

func (a *fakeAnalyst) Propose(_ context.Context, in slices.AnalystInput) (slices.Proposal, error) {
	a.inputs = append(a.inputs, in)
	i := a.calls
	if i >= len(a.results) {
		i = len(a.results) - 1
	}
	a.calls++
	return a.results[i].p, a.results[i].err
}

type diffCall struct{ base, revision string }

type fakeDiff struct {
	calls []diffCall
	// findings decides the answer for a call; nil means no findings.
	findings func(base, revision string) []slices.ContractFinding
	err      error
}

func (d *fakeDiff) Diff(_ context.Context, base, revision string) ([]slices.ContractFinding, error) {
	d.calls = append(d.calls, diffCall{base, revision})
	if d.err != nil {
		return nil, d.err
	}
	if d.findings == nil {
		return nil, nil
	}
	return d.findings(base, revision), nil
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func notesProposal(t *testing.T) slices.Proposal {
	t.Helper()
	var p slices.Proposal
	if err := json.Unmarshal(fixture(t, "proposal-notes.json"), &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func newService(t *testing.T, a slices.Analyst, d slices.ContractDiff) *slices.Service {
	t.Helper()
	schemas, err := slice.LoadSchemas("../api/preview/2026-11-12-preview")
	if err != nil {
		t.Fatal(err)
	}
	// A real slice always has a pattern: the catalogue's, whose conditions run as checks.
	pattern, err := os.ReadFile("../../catalogue/api-feature-go/pattern.yaml")
	if err != nil {
		t.Fatal(err)
	}
	s, err := slices.New(slices.Config{Schemas: schemas, Analyst: a, Diff: d, Pattern: pattern, Lint: &fakeLint{}})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// setup returns a service whose analyst proposes the notes fixture every time.
func setup(t *testing.T) (*slices.Service, *fakeAnalyst, *fakeDiff) {
	t.Helper()
	a := &fakeAnalyst{results: []analystResult{{p: notesProposal(t)}}}
	d := &fakeDiff{}
	return newService(t, a, d), a, d
}

func create(t *testing.T, s *slices.Service, description string) slices.Slice {
	t.Helper()
	r, err := s.Create(context.Background(), description)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return r.Slice
}

func sha(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func code(t *testing.T, err error) string {
	t.Helper()
	var e *slices.Error
	if !errors.As(err, &e) {
		t.Fatalf("want *slices.Error, got %T: %v", err, err)
	}
	return e.Code
}

func failedChecks(s slices.Slice, part string) []slices.Check {
	var out []slices.Check
	for _, c := range s.Checks {
		if !c.Passed && c.Part == part {
			out = append(out, c)
		}
	}
	return out
}

func approval(s slices.Slice, part slices.Part) (slices.Approval, bool) {
	for _, a := range s.Approvals {
		if a.Part == part {
			return a, true
		}
	}
	return slices.Approval{}, false
}

func approveAll(t *testing.T, s *slices.Service, sl slices.Slice) slices.Slice {
	t.Helper()
	ctx := context.Background()
	var err error
	for _, p := range []struct {
		part slices.Part
		hash string
	}{{slices.PartGoals, sl.Parts.Goals}, {slices.PartRules, sl.Parts.Rules}, {slices.PartContract, sl.Parts.Contract}} {
		if sl, err = s.Approve(ctx, sl.ID, p.part, p.hash); err != nil {
			t.Fatalf("approve %s: %v", p.part, err)
		}
	}
	return sl
}

// Canonical forms of the notes fixture's parts, written by hand under RFC 8785: keys sorted,
// no whitespace, strings with only the required escapes (\n and \t, and no \u escape for < > &,
// non-ASCII or U+2028, which Go's encoding/json would escape).
const (
	canonGoals = `{"goals":[{"id":"G-1","text":"Keep a <note> & café"},{"id":"G-2","text":"List\tnotes"}]}`

	canonRules = `{"examples":"Feature: Notes\n  Rule: R-1\n    @R-1 @E-1\n    Scenario: Keep\n      Given a note\n  Rule: R-2\n    @R-2 @E-2\n    Scenario: List\n      Given notes\n",` +
		`"rules":[{"goal":"G-1","id":"R-1","pattern":"event","response":"shall store the note","system":"the notes service","terms":["T-1"],"trigger":"the developer submits a <note>"},` +
		`{"goal":"G-2","id":"R-2","pattern":"ubiquitous","response":"shall list notes","system":"the notes service","terms":[]}],` +
		"\"terms\":[{\"definition\":\"a short text kept by the service\",\"id\":\"T-1\",\"name\":\"Note\"}]}"

	canonContract = `{"contract":"openapi: 3.1.0\ninfo:\n  title: Notes\n  version: '1'\npaths:\n  /notes:\n    get:\n      operationId: listNotes\n      x-rules: [R-2]\n      responses:\n        '200':\n          description: The notes.\n    post:\n      operationId: keepNote\n      x-rules: [R-1]\n      responses:\n        '201':\n          description: The note was kept.\n"}`
)

// ---- part hashes (T029: SHA-256 of RFC 8785 canonical JSON; D17) ----

func TestPartHashesAreSHA256OfCanonicalJSON(t *testing.T) {
	s, _, _ := setup(t)
	sl := create(t, s, "Let me keep notes.")
	want := slices.Parts{Goals: sha(canonGoals), Rules: sha(canonRules), Contract: sha(canonContract)}
	if sl.Parts != want {
		t.Errorf("parts\n got %+v\nwant %+v", sl.Parts, want)
	}
	// The same hashes after a read back.
	got, err := s.Get(sl.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Parts != want {
		t.Errorf("Get parts %+v, want %+v", got.Parts, want)
	}
}

func TestNilListsHashAsEmptyArrays(t *testing.T) {
	s, _, _ := setup(t)
	sl := create(t, s, "Let me keep notes.")
	rules := append([]slices.Rule(nil), sl.Rules...)
	rules[1].Terms = nil // canonical form has "terms":[] for it
	sl2, err := s.ReplaceRules(context.Background(), sl.ID, slices.RulesPart{Rules: rules, Examples: sl.Examples, Terms: sl.Terms})
	if err != nil {
		t.Fatal(err)
	}
	if sl2.Parts.Rules != sha(canonRules) {
		t.Errorf("nil rule terms hash %s, want the hash of the canonical form with []", sl2.Parts.Rules)
	}
	// A slice with no terms at all hashes "terms":[], not null.
	p := notesProposal(t)
	p.Terms = nil
	for i := range p.Rules {
		p.Rules[i].Terms = nil
	}
	s2 := newService(t, &fakeAnalyst{results: []analystResult{{p: p}}}, &fakeDiff{})
	got := create(t, s2, "no terms")
	wantRules := `{"examples":` + mustJSONString(t, p.Examples) +
		`,"rules":[{"goal":"G-1","id":"R-1","pattern":"event","response":"shall store the note","system":"the notes service","terms":[],"trigger":"the developer submits a <note>"},` +
		`{"goal":"G-2","id":"R-2","pattern":"ubiquitous","response":"shall list notes","system":"the notes service","terms":[]}],"terms":[]}`
	if got.Parts.Rules != sha(wantRules) {
		t.Errorf("empty lists must hash as [] (got %s)", got.Parts.Rules)
	}
}

// mustJSONString renders a string the RFC 8785 way for text that only needs \n escapes.
func mustJSONString(t *testing.T, v string) string {
	t.Helper()
	if strings.ContainsAny(v, "\"\\\t\r<>&") {
		t.Fatalf("helper only handles newlines: %q", v)
	}
	return `"` + strings.ReplaceAll(v, "\n", `\n`) + `"`
}

func TestRulesPartCoversRulesExamplesAndTerms(t *testing.T) {
	ctx := context.Background()
	base := func() (*slices.Service, slices.Slice) {
		s, _, _ := setup(t)
		return s, create(t, s, "Let me keep notes.")
	}
	cases := []struct {
		name   string
		change func(p *slices.RulesPart)
	}{
		{"rule", func(p *slices.RulesPart) { p.Rules[0].Response = "shall store the note forever" }},
		{"examples", func(p *slices.RulesPart) { p.Examples += "\n" }},
		{"term", func(p *slices.RulesPart) { p.Terms[0].Definition = "different" }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, sl := base()
			p := slices.RulesPart{Rules: append([]slices.Rule(nil), sl.Rules...), Examples: sl.Examples, Terms: append([]slices.Term(nil), sl.Terms...)}
			c.change(&p)
			got, err := s.ReplaceRules(ctx, sl.ID, p)
			if err != nil {
				t.Fatal(err)
			}
			if got.Parts.Rules == sl.Parts.Rules {
				t.Errorf("changing the %s did not change the rules hash", c.name)
			}
			if got.Parts.Goals != sl.Parts.Goals || got.Parts.Contract != sl.Parts.Contract {
				t.Errorf("changing the %s changed another part's hash", c.name)
			}
		})
	}
	t.Run("goals and contract are separate parts", func(t *testing.T) {
		s, sl := base()
		g, err := s.ReplaceGoals(ctx, sl.ID, []slices.Goal{sl.Goals[0], {ID: "G-2", Text: "List all notes"}})
		if err != nil {
			t.Fatal(err)
		}
		if g.Parts.Goals == sl.Parts.Goals || g.Parts.Rules != sl.Parts.Rules || g.Parts.Contract != sl.Parts.Contract {
			t.Errorf("goals change moved hashes: before %+v after %+v", sl.Parts, g.Parts)
		}
		c, err := s.ReplaceContract(ctx, sl.ID, string(fixture(t, "contract-notes-v1.yaml")))
		if err != nil {
			t.Fatal(err)
		}
		if c.Parts.Contract == sl.Parts.Contract || c.Parts.Rules != sl.Parts.Rules || c.Parts.Goals != g.Parts.Goals {
			t.Errorf("contract change moved hashes: before %+v after %+v", g.Parts, c.Parts)
		}
	})
}

// ---- approvals ----

func TestApproveRecordsTheReviewedHash(t *testing.T) {
	s, _, _ := setup(t)
	sl := create(t, s, "Let me keep notes.")
	got, err := s.Approve(context.Background(), sl.ID, slices.PartGoals, sl.Parts.Goals)
	if err != nil {
		t.Fatal(err)
	}
	a, ok := approval(got, slices.PartGoals)
	if !ok || a.ContentHash != sl.Parts.Goals {
		t.Fatalf("approvals %+v, want goals at %s", got.Approvals, sl.Parts.Goals)
	}
	if got.State == "approved" {
		t.Error("one approved part must not make the slice approved")
	}
	if got.History[len(got.History)-1].Kind != "approved" {
		t.Errorf("history %+v must end with approved", got.History)
	}
}

func TestAllThreePartsApprovedMakesTheSliceApproved(t *testing.T) {
	s, _, _ := setup(t)
	sl := approveAll(t, s, create(t, s, "Let me keep notes."))
	if sl.State != "approved" || len(sl.Approvals) != 3 {
		t.Errorf("state %q approvals %+v", sl.State, sl.Approvals)
	}
}

func TestApproveAtAStaleHashFails(t *testing.T) {
	ctx := context.Background()
	s, _, _ := setup(t)
	sl := create(t, s, "Let me keep notes.")
	changed, err := s.ReplaceGoals(ctx, sl.ID, []slices.Goal{sl.Goals[0], {ID: "G-2", Text: "Show notes"}})
	if err != nil {
		t.Fatal(err)
	}
	// The developer reviewed the goals before the change.
	_, err = s.Approve(ctx, sl.ID, slices.PartGoals, sl.Parts.Goals)
	if c := code(t, err); c != "StaleContentHash" {
		t.Errorf("code %q, want StaleContentHash", c)
	}
	if got, _ := s.Get(sl.ID); len(got.Approvals) != 0 {
		t.Errorf("a refused approval was recorded: %+v", got.Approvals)
	}
	// Any other hash is stale too, even a well-formed one for another part.
	_, err = s.Approve(ctx, sl.ID, slices.PartGoals, changed.Parts.Rules)
	if c := code(t, err); c != "StaleContentHash" {
		t.Errorf("another part's hash: code %q, want StaleContentHash", c)
	}
	// The current hash works.
	if _, err := s.Approve(ctx, sl.ID, slices.PartGoals, changed.Parts.Goals); err != nil {
		t.Errorf("approve at the current hash: %v", err)
	}
}

func TestApproveUnknownSliceIsNotFound(t *testing.T) {
	s, _, _ := setup(t)
	_, err := s.Approve(context.Background(), "S-99", slices.PartGoals, sha("x"))
	if c := code(t, err); c != "NotFound" {
		t.Errorf("code %q, want NotFound", c)
	}
}

// RESOLVED: the contract's code for "the part fails a check" is PartFailsChecks (approvePart
// x-error-codes), carrying `failures`; StaleContentHash is only for a changed part.
func TestApproveAFailingPartFailsAndNamesTheFailure(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		part slices.Part
		id   string // the item the failure names
		edit func(t *testing.T, s *slices.Service, sl slices.Slice) slices.Slice
	}{
		{"goal without a rule", slices.PartGoals, "G-3", func(t *testing.T, s *slices.Service, sl slices.Slice) slices.Slice {
			got, err := s.ReplaceGoals(ctx, sl.ID, append(append([]slices.Goal(nil), sl.Goals...), slices.Goal{ID: "G-3", Text: "Delete notes"}))
			if err != nil {
				t.Fatal(err)
			}
			return got
		}},
		{"rule without an example", slices.PartRules, "R-3", func(t *testing.T, s *slices.Service, sl slices.Slice) slices.Slice {
			rules := append(append([]slices.Rule(nil), sl.Rules...), slices.Rule{
				ID: "R-3", Goal: "G-1", Pattern: "ubiquitous", System: "the notes service", Response: "shall keep notes", Terms: []string{},
			})
			got, err := s.ReplaceRules(ctx, sl.ID, slices.RulesPart{Rules: rules, Examples: sl.Examples, Terms: sl.Terms})
			if err != nil {
				t.Fatal(err)
			}
			return got
		}},
		{"rule using an undefined term", slices.PartRules, "T-9", func(t *testing.T, s *slices.Service, sl slices.Slice) slices.Slice {
			rules := append([]slices.Rule(nil), sl.Rules...)
			rules[1].Terms = []string{"T-9"}
			got, err := s.ReplaceRules(ctx, sl.ID, slices.RulesPart{Rules: rules, Examples: sl.Examples, Terms: sl.Terms})
			if err != nil {
				t.Fatal(err)
			}
			return got
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, _, _ := setup(t)
			sl := c.edit(t, s, create(t, s, "Let me keep notes."))
			if len(failedChecks(sl, string(c.part))) == 0 {
				t.Fatalf("no failed check for the %s part: %+v", c.part, sl.Checks)
			}
			hash := map[slices.Part]string{slices.PartGoals: sl.Parts.Goals, slices.PartRules: sl.Parts.Rules}[c.part]
			_, err := s.Approve(ctx, sl.ID, c.part, hash) // at the current hash: not stale
			var e *slices.Error
			if !errors.As(err, &e) || e.Code != "PartFailsChecks" {
				t.Fatalf("err %v, want PartFailsChecks", err)
			}
			found := false
			for _, f := range e.Failures {
				if f.Part == string(c.part) && !f.Passed && strings.Contains(f.Reason, c.id) {
					found = true
				}
			}
			if !found {
				t.Errorf("failures %+v do not name %s on part %s", e.Failures, c.id, c.part)
			}
			if got, _ := s.Get(sl.ID); len(got.Approvals) != 0 {
				t.Errorf("a failing part was approved: %+v", got.Approvals)
			}
		})
	}
}

// Checks are per part: a failing goals part does not stop the rules part from being approved.
func TestAFailingPartDoesNotBlockOtherParts(t *testing.T) {
	ctx := context.Background()
	s, _, _ := setup(t)
	sl := create(t, s, "Let me keep notes.")
	bad, err := s.ReplaceGoals(ctx, sl.ID, append(append([]slices.Goal(nil), sl.Goals...), slices.Goal{ID: "G-3", Text: "Delete notes"}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Approve(ctx, sl.ID, slices.PartRules, bad.Parts.Rules); err != nil {
		t.Errorf("approve rules while goals fail: %v", err)
	}
	if _, err := s.Approve(ctx, sl.ID, slices.PartContract, bad.Parts.Contract); err != nil {
		t.Errorf("approve contract while goals fail: %v", err)
	}
}

// ---- changing an approved part withdraws its approval (FR-007, US1.5) ----

func TestChangingAnApprovedPartWithdrawsItsApproval(t *testing.T) {
	ctx := context.Background()
	changes := []struct {
		part  slices.Part
		apply func(t *testing.T, s *slices.Service, sl slices.Slice) (slices.Slice, error)
	}{
		{slices.PartGoals, func(t *testing.T, s *slices.Service, sl slices.Slice) (slices.Slice, error) {
			return s.ReplaceGoals(ctx, sl.ID, []slices.Goal{sl.Goals[0], {ID: "G-2", Text: "Show notes"}})
		}},
		{slices.PartRules, func(t *testing.T, s *slices.Service, sl slices.Slice) (slices.Slice, error) {
			return s.ReplaceRules(ctx, sl.ID, slices.RulesPart{Rules: sl.Rules, Examples: sl.Examples + "\n", Terms: sl.Terms})
		}},
		{slices.PartContract, func(t *testing.T, s *slices.Service, sl slices.Slice) (slices.Slice, error) {
			return s.ReplaceContract(ctx, sl.ID, string(fixture(t, "contract-notes-v1.yaml")))
		}},
	}
	for _, c := range changes {
		t.Run(string(c.part), func(t *testing.T) {
			s, _, _ := setup(t)
			approved := approveAll(t, s, create(t, s, "Let me keep notes."))
			got, err := c.apply(t, s, approved)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := approval(got, c.part); ok {
				t.Errorf("approval of %s survived its change", c.part)
			}
			for _, other := range []slices.Part{slices.PartGoals, slices.PartRules, slices.PartContract} {
				if other == c.part {
					continue
				}
				if a, ok := approval(got, other); !ok || a.ContentHash == "" {
					t.Errorf("approval of %s was withdrawn by a change to %s", other, c.part)
				}
			}
			if got.State != "checked" {
				t.Errorf("state %q, want checked", got.State)
			}
			if last := got.History[len(got.History)-1]; last.Kind != "changed" {
				t.Errorf("history ends with %q, want changed", last.Kind)
			}
			// Stored state agrees, and the part needs approval again at its new hash.
			again, _ := s.Get(got.ID)
			if _, ok := approval(again, c.part); ok {
				t.Error("stored slice still holds the withdrawn approval")
			}
		})
	}
}

// RESOLVED: data-model.md says an approval "holds only while parts[part] equals content_hash",
// so replacing a part with identical content is not a change and keeps the approval.
func TestReplacingAPartWithIdenticalContentKeepsItsApproval(t *testing.T) {
	ctx := context.Background()
	s, _, _ := setup(t)
	approved := approveAll(t, s, create(t, s, "Let me keep notes."))
	got, err := s.ReplaceGoals(ctx, approved.ID, approved.Goals)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := approval(got, slices.PartGoals); !ok || got.State != "approved" {
		t.Errorf("identical goals withdrew the approval: state %q approvals %+v", got.State, got.Approvals)
	}
}

// ---- description handling ----

func TestResubmittedDescriptionOffersTheApprovedSlice(t *testing.T) {
	ctx := context.Background()
	s, a, _ := setup(t)
	first := approveAll(t, s, create(t, s, "Let me keep notes."))
	r, err := s.Create(ctx, "Let me keep notes.")
	if err != nil {
		t.Fatal(err)
	}
	if !r.Reused || r.Slice.ID != first.ID {
		t.Errorf("got reused=%v slice %s, want the existing %s offered", r.Reused, r.Slice.ID, first.ID)
	}
	if a.calls != 1 {
		t.Errorf("analyst ran %d times, want 1: the description must not be proposed again", a.calls)
	}
}

// RESOLVED: spec edge case says "previously approved slice". A slice not yet approved is not
// offered; the same description is proposed again as a new slice.
func TestResubmittingAnUnapprovedDescriptionProposesANewSlice(t *testing.T) {
	ctx := context.Background()
	s, a, _ := setup(t)
	first := create(t, s, "Let me keep notes.")
	r, err := s.Create(ctx, "Let me keep notes.")
	if err != nil {
		t.Fatal(err)
	}
	if r.Reused || r.Slice.ID == first.ID || a.calls != 2 {
		t.Errorf("reused=%v id %s (first %s) analyst calls %d; want a new slice", r.Reused, r.Slice.ID, first.ID, a.calls)
	}
}

func TestADifferentDescriptionIsNotReused(t *testing.T) {
	ctx := context.Background()
	s, a, _ := setup(t)
	approveAll(t, s, create(t, s, "Let me keep notes."))
	r, err := s.Create(ctx, "Let me keep reminders.")
	if err != nil {
		t.Fatal(err)
	}
	if r.Reused || a.calls != 2 {
		t.Errorf("reused=%v calls=%d", r.Reused, a.calls)
	}
}

func TestMoreThanOneUseCaseAsksToNarrowTheDescription(t *testing.T) {
	a := &fakeAnalyst{results: []analystResult{{err: slices.ErrMultipleUseCases}}}
	s := newService(t, a, &fakeDiff{})
	_, err := s.Create(context.Background(), "Let me keep notes, and also pay invoices.")
	if c := code(t, err); c != "NarrowDescription" {
		t.Errorf("code %q, want NarrowDescription", c)
	}
	if _, err := s.Get("S-1"); err == nil {
		t.Error("a slice was stored for a description that must be narrowed")
	}
}

// RESOLVED: the task asks for NarrowDescription on an empty description although the contract's
// minLength 1 would give InvalidRequest at the API; the service is the one that asks to narrow.
// The analyst is not run for it. Whitespace only counts as empty.
func TestEmptyDescriptionAsksToNarrowWithoutRunningTheAnalyst(t *testing.T) {
	for _, d := range []string{"", "   ", "\n\t "} {
		s, a, _ := setup(t)
		_, err := s.Create(context.Background(), d)
		if c := code(t, err); c != "NarrowDescription" {
			t.Errorf("description %q: code %q, want NarrowDescription", d, c)
		}
		if a.calls != 0 {
			t.Errorf("description %q: analyst ran %d times", d, a.calls)
		}
	}
}

func TestAnalystFailureIsNotNarrowDescription(t *testing.T) {
	boom := errors.New("model unavailable")
	s := newService(t, &fakeAnalyst{results: []analystResult{{err: boom}}}, &fakeDiff{})
	_, err := s.Create(context.Background(), "Let me keep notes.")
	if err == nil {
		t.Fatal("want an error")
	}
	var e *slices.Error
	if errors.As(err, &e) && e.Code == "NarrowDescription" {
		t.Error("an analyst failure must not ask the developer to narrow")
	}
	if !errors.Is(err, boom) {
		t.Errorf("err %v must wrap the analyst's error", err)
	}
}

// ---- the proposal becomes a slice ----

func TestCreateBuildsTheSliceFromTheProposal(t *testing.T) {
	s, a, _ := setup(t)
	sl := create(t, s, "Let me keep notes.")
	if !strings.HasPrefix(sl.ID, "S-") || sl.Description != "Let me keep notes." || sl.State != "checked" {
		t.Errorf("id %q description %q state %q", sl.ID, sl.Description, sl.State)
	}
	if a.inputs[0].Description != "Let me keep notes." {
		t.Errorf("analyst got %q", a.inputs[0].Description)
	}
	if len(sl.Goals) != 2 || len(sl.Rules) != 2 || len(sl.Terms) != 1 || sl.Contract == "" {
		t.Errorf("slice %+v", sl)
	}
	if len(sl.History) != 1 || sl.History[0].Kind != "proposed" ||
		sl.History[0].Model != "claude-haiku-test" || sl.History[0].AgentDigest != strings.Repeat("b", 64) {
		t.Errorf("history %+v: a proposal names the model and agent digest", sl.History)
	}
	second := create(t, s, "Another description.")
	if second.ID == sl.ID {
		t.Error("slice IDs must be unique")
	}
}

func TestQuestionsGetPlatformIDsAndOpenTheSlice(t *testing.T) {
	p := notesProposal(t)
	p.Questions = []slices.QuestionDraft{{About: "R-1", Text: "Is a note length-limited?", Options: []string{"No", "Yes, 1000 characters"}}}
	s := newService(t, &fakeAnalyst{results: []analystResult{{p: p}}}, &fakeDiff{})
	sl := create(t, s, "Let me keep notes.")
	if len(sl.Questions) != 1 || sl.Questions[0].ID != "Q-1" || sl.Questions[0].About != "R-1" {
		t.Errorf("questions %+v", sl.Questions)
	}
	if sl.State != "questions-open" {
		t.Errorf("state %q, want questions-open", sl.State)
	}
}

// ---- contract compatibility (FR-004, research D4) ----

func errorFinding(text string) []slices.ContractFinding {
	return []slices.ContractFinding{{Level: "error", ID: "new-required-request-parameter", Text: text}}
}

func TestFirstContractIsDiffedAgainstAnEmptyDocument(t *testing.T) {
	a := &fakeAnalyst{results: []analystResult{{p: notesProposal(t)}}}
	d := &fakeDiff{}
	s := newService(t, a, d)
	sl := create(t, s, "Let me keep notes.")
	if len(d.calls) != 1 || d.calls[0].base != "" || d.calls[0].revision != sl.Contract {
		t.Fatalf("diff calls %+v, want one call with base \"\" and the slice's contract", d.calls)
	}
	if len(failedChecks(sl, "contract")) != 0 {
		t.Errorf("a clean diff failed the contract: %+v", sl.Checks)
	}
	// Still no approved contract: a revision is again compared with the empty document.
	if _, err := s.ReplaceContract(context.Background(), sl.ID, string(fixture(t, "contract-notes-v1.yaml"))); err != nil {
		t.Fatal(err)
	}
	if got := d.calls[len(d.calls)-1]; got.base != "" {
		t.Errorf("base %q before any approval, want empty", got.base)
	}
}

func TestFirstContractWithAnErrorFindingIsRejectedNamingTheChange(t *testing.T) {
	ctx := context.Background()
	const change = "response property 'id' became required"
	d := &fakeDiff{findings: func(base, rev string) []slices.ContractFinding { return errorFinding(change) }}
	s := newService(t, &fakeAnalyst{results: []analystResult{{p: notesProposal(t)}}}, d)
	sl := create(t, s, "Let me keep notes.")

	fails := failedChecks(sl, "contract")
	if len(fails) != 1 || fails[0].Check != slices.CheckContractCompatibility || !strings.Contains(fails[0].Reason, change) {
		t.Fatalf("contract checks %+v, want one failed %s naming %q", sl.Checks, slices.CheckContractCompatibility, change)
	}
	_, err := s.Approve(ctx, sl.ID, slices.PartContract, sl.Parts.Contract)
	var e *slices.Error
	if !errors.As(err, &e) || e.Code != "PartFailsChecks" {
		t.Fatalf("err %v, want PartFailsChecks", err)
	}
	if len(e.Failures) == 0 || !strings.Contains(e.Failures[0].Reason, change) {
		t.Errorf("failures %+v do not name the change", e.Failures)
	}
}

func TestOnlyErrorLevelFindingsFailTheContract(t *testing.T) {
	d := &fakeDiff{findings: func(base, rev string) []slices.ContractFinding {
		return []slices.ContractFinding{
			{Level: "warn", ID: "api-operation-id-removed", Text: "a warning"},
			{Level: "info", ID: "endpoint-added", Text: "an addition"},
		}
	}}
	s := newService(t, &fakeAnalyst{results: []analystResult{{p: notesProposal(t)}}}, d)
	sl := create(t, s, "Let me keep notes.")
	if f := failedChecks(sl, "contract"); len(f) != 0 {
		t.Errorf("warn and info findings failed the contract: %+v", f)
	}
	if _, err := s.Approve(context.Background(), sl.ID, slices.PartContract, sl.Parts.Contract); err != nil {
		t.Errorf("approve: %v", err)
	}
}

func TestChangedContractIsDiffedAgainstTheApprovedOneAndBreakingChangeRejected(t *testing.T) {
	ctx := context.Background()
	v1 := string(fixture(t, "contract-notes-v1.yaml"))
	v2 := string(fixture(t, "contract-notes-v2-breaking.yaml"))
	const change = "removed the path /notes"
	d := &fakeDiff{findings: func(base, rev string) []slices.ContractFinding {
		if base == v1 && rev == v2 {
			return errorFinding(change)
		}
		return nil
	}}
	s := newService(t, &fakeAnalyst{results: []analystResult{{p: notesProposal(t)}}}, d)
	sl := create(t, s, "Let me keep notes.")
	sl, err := s.ReplaceContract(ctx, sl.ID, v1)
	if err != nil {
		t.Fatal(err)
	}
	if sl, err = s.Approve(ctx, sl.ID, slices.PartContract, sl.Parts.Contract); err != nil {
		t.Fatalf("approve v1: %v", err)
	}

	got, err := s.ReplaceContract(ctx, sl.ID, v2)
	if err != nil {
		t.Fatal(err)
	}
	if last := d.calls[len(d.calls)-1]; last.base != v1 || last.revision != v2 {
		t.Errorf("last diff call %+v, want base = the approved contract, revision = the new one", last)
	}
	fails := failedChecks(got, "contract")
	if len(fails) != 1 || !strings.Contains(fails[0].Reason, change) {
		t.Fatalf("checks %+v, want the breaking change named", got.Checks)
	}
	if _, ok := approval(got, slices.PartContract); ok {
		t.Error("the contract approval must be withdrawn by the change")
	}
	_, err = s.Approve(ctx, got.ID, slices.PartContract, got.Parts.Contract)
	if c := code(t, err); c != "PartFailsChecks" {
		t.Errorf("approving the breaking contract: code %q, want PartFailsChecks", c)
	}

	// Going back to a compatible contract passes again, and the failed check no longer shows.
	back, err := s.ReplaceContract(ctx, got.ID, v1)
	if err != nil {
		t.Fatal(err)
	}
	if f := failedChecks(back, "contract"); len(f) != 0 {
		t.Errorf("stale failed check kept: %+v", f)
	}
}

// RESOLVED: spec is silent on a tool failure. A check that could not run must not pass: either
// the call returns an error or the check is recorded as failed, never a silent pass.
func TestContractDiffFailureNeverPasses(t *testing.T) {
	d := &fakeDiff{err: errors.New("oasdiff missing")}
	s := newService(t, &fakeAnalyst{results: []analystResult{{p: notesProposal(t)}}}, d)
	r, err := s.Create(context.Background(), "Let me keep notes.")
	if err != nil {
		return
	}
	if len(failedChecks(r.Slice, "contract")) == 0 {
		t.Errorf("diff tool failed but the contract check passed: %+v", r.Slice.Checks)
	}
	if _, err := s.Approve(context.Background(), r.Slice.ID, slices.PartContract, r.Slice.Parts.Contract); err == nil {
		t.Error("the contract was approved although its diff never ran")
	}
}
