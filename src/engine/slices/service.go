package slices

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/agentic-platform/slice"
	"go.yaml.in/yaml/v3"
)

// Service holds the slices of one installation. It is safe for concurrent use.
type Service struct {
	schemas *slice.Schemas
	analyst Analyst
	diff    ContractDiff
	lint    ContractLint // nil: a failed ContractLint check
	pattern []byte
	// attempts is the pattern's limits.attempts: how often Create asks the analyst for a usable
	// proposal. 1 when there is no pattern or it gives no positive limit (fail closed on cost).
	attempts int
	now      func() time.Time

	mu   sync.Mutex
	last int // the number of the last slice ID given out; IDs are never reused
	// reserved holds the IDs Reserve gave out that no slice holds yet; CreateAs takes one.
	reserved map[string]bool
	slices   map[string]*record
}

// record is a stored slice and what the service keeps beside it.
type record struct {
	slice Slice
	// baseline is the contract last approved, which a changed contract is compared with; "" until
	// a contract is approved. It outlives the approval a change withdraws (FR-004).
	baseline string
	// compat and lint are the latest compatibility and lint results, kept until the contract
	// changes.
	compat, lint []Check
}

// New returns a service. Schemas, Analyst and Diff are required.
func New(cfg Config) (*Service, error) {
	switch {
	case cfg.Schemas == nil:
		return nil, errors.New("slices: no contract schemas")
	case cfg.Analyst == nil:
		return nil, errors.New("slices: no analyst")
	case cfg.Diff == nil:
		return nil, errors.New("slices: no contract diff")
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Service{schemas: cfg.Schemas, analyst: cfg.Analyst, diff: cfg.Diff, lint: cfg.Lint, pattern: cfg.Pattern,
		attempts: attemptsOf(cfg.Pattern), now: now, reserved: map[string]bool{}, slices: map[string]*record{}}, nil
}

// attemptsOf reads limits.attempts from a pattern.yaml, its one home. A missing pattern, an
// unreadable one, or one without a positive limit gives one attempt.
func attemptsOf(pattern []byte) int {
	var p struct {
		Limits struct {
			Attempts int `yaml:"attempts"`
		} `yaml:"limits"`
	}
	if pattern == nil || yaml.Unmarshal(pattern, &p) != nil || p.Limits.Attempts < 1 {
		return 1
	}
	return p.Limits.Attempts
}

// Create proposes a slice from a description (createSlice). An approved slice of the same
// description is offered instead, without running the analyst. An empty description, or one the
// analyst finds covers more than one use case, gives NarrowDescription. A proposal that is
// unusable (it breaks the proposal schema, or its examples or contract do not parse) is sent back
// to the analyst with what was wrong, up to the pattern's limits.attempts; after the last attempt
// Create gives OperationFailed naming the faults, and stores nothing. Other failing checks are
// stored with the slice, not returned as an error.
func (s *Service) Create(ctx context.Context, description string) (Result, error) {
	res, rec, err := s.build(ctx, description)
	if err != nil || rec == nil {
		return res, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.last++
	rec.slice.ID = "S-" + strconv.Itoa(s.last)
	s.slices[rec.slice.ID] = rec
	return Result{Slice: clone(rec.slice)}, nil
}

// Reserve gives out the next slice ID from the sequence Create also draws from, without creating
// a slice; CreateAs then creates the slice under it. An ID given out is never given out again,
// whether or not a slice is created under it, and not after the slice is deleted.
func (s *Service) Reserve() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.last++
	id := "S-" + strconv.Itoa(s.last)
	s.reserved[id] = true
	return id
}

// CreateAs is Create under an ID Reserve gave out, with Create's refusals. A malformed ID, one
// Reserve did not give out, or one a slice already holds gives InvalidRequest before the analyst
// runs. A refused CreateAs leaves the ID reserved, so it may be retried; an approved slice of the
// same description is offered as in Create, and the ID stays reserved.
func (s *Service) CreateAs(ctx context.Context, id, description string) (Result, error) {
	if err := s.reservable(id); err != nil {
		return Result{}, err
	}
	res, rec, err := s.build(ctx, description)
	if err != nil || rec == nil {
		return res, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.reservableLocked(id); err != nil { // another CreateAs took it meanwhile
		return Result{}, err
	}
	delete(s.reserved, id)
	rec.slice.ID = id
	s.slices[id] = rec
	return Result{Slice: clone(rec.slice)}, nil
}

func (s *Service) reservable(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reservableLocked(id)
}

// reservableLocked reports whether CreateAs may create a slice under id. The caller holds s.mu.
func (s *Service) reservableLocked(id string) error {
	if _, err := s.schemas.ParseID(id); err != nil {
		return &Error{Code: "InvalidRequest", Detail: err.Error()}
	}
	if _, ok := s.slices[id]; ok || !s.reserved[id] {
		return &Error{Code: "InvalidRequest", Detail: "slice ID " + id + " was not reserved or is in use"}
	}
	return nil
}

// build makes the slice Create stores, without an ID. It returns no record and Result.Reused
// when an approved slice of the same description is offered instead.
func (s *Service) build(ctx context.Context, description string) (Result, *record, error) {
	if strings.TrimSpace(description) == "" {
		return Result{}, nil, &Error{Code: "NarrowDescription", Detail: "the description is empty; describe one use case"}
	}
	if sl, ok := s.approvedFor(description); ok {
		return Result{Slice: sl, Reused: true}, nil, nil
	}

	p, sl, err := s.propose(ctx, AnalystInput{Description: description}, 0)
	if err != nil {
		return Result{}, nil, err
	}
	for i, q := range p.Questions {
		sl.Questions = append(sl.Questions, Question{ID: "Q-" + strconv.Itoa(i+1), About: q.About, Text: q.Text,
			Options: slices.Clone(q.Options)})
	}
	rec := &record{slice: sl}
	if err := s.recheck(ctx, rec, true); err != nil {
		return Result{}, nil, err
	}
	rec.slice.History = []HistoryEntry{{Kind: "proposed", At: s.now(), Model: p.Model, AgentDigest: p.AgentDigest,
		Refs: []string{string(PartGoals), string(PartRules), string(PartContract)}}}
	rec.slice.State = state(&rec.slice)
	return Result{}, rec, nil
}

// maxQuestions is the most questions a slice may have (FR-006; slice.schema.json questions
// maxItems, which the proposal schema also gives).
const maxQuestions = 10

// propose asks the analyst for a proposal from base (a description, and when revising the
// current proposal and the questions) and builds the slice from it; asked is the number of
// questions the slice already has, which count against the limit with the new ones. An unusable
// proposal is sent back with its faults until one is usable or s.attempts calls were made. An
// analyst error is returned at once, not retried.
func (s *Service) propose(ctx context.Context, base AnalystInput, asked int) (Proposal, Slice, error) {
	description := base.Description
	in := base
	for attempt := 1; ; attempt++ {
		p, err := s.analyst.Propose(ctx, in)
		if errors.Is(err, ErrMultipleUseCases) {
			return Proposal{}, Slice{}, &Error{Code: "NarrowDescription", Detail: "the description covers more than one use case; describe one"}
		}
		if err != nil {
			return Proposal{}, Slice{}, fmt.Errorf("slices: the analyst failed: %w", err)
		}
		sl := clone(Slice{ // own the analyst's lists
			Description: description, Contract: p.Contract, Examples: p.Examples,
			Goals: p.Goals, Terms: p.Terms, Rules: p.Rules,
		})
		normalise(&sl)
		if asked+len(p.Questions) > maxQuestions {
			return Proposal{}, Slice{}, &Error{Code: "TooManyQuestions",
				Detail: fmt.Sprintf("the slice would need %d questions, more than %d; narrow the description", asked+len(p.Questions), maxQuestions)}
		}
		faults, err := s.unusable(&sl, p.Questions)
		if err != nil {
			return Proposal{}, Slice{}, err
		}
		if len(faults) == 0 {
			return p, sl, nil
		}
		if attempt >= s.attempts {
			return Proposal{}, Slice{}, &Error{Code: "OperationFailed", Detail: fmt.Sprintf(
				"the analyst's proposal was unusable after %d attempts (limits.attempts): %s", attempt, strings.Join(faults, "; "))}
		}
		prev := p
		in = base
		in.Previous, in.Errors = &prev, faults
	}
}

// unusable returns what makes the analyst's output, as the slice sl built from it and its
// questions, unusable: each way it breaks slice-proposal.schema.json (model output is parsed
// against its schema, never trusted; a nil list counts as the empty list), and each syntax finding
// of slice.Trace, examples that are not Gherkin or a contract that is not YAML. Other faults are
// check results, stored with the slice.
func (s *Service) unusable(sl *Slice, questions []QuestionDraft) ([]string, error) {
	doc, err := json.Marshal(struct {
		partsDoc
		Questions []QuestionDraft `json:"questions,omitempty"`
	}{partsDoc{Goals: sl.Goals, Terms: sl.Terms, Rules: sl.Rules, Examples: sl.Examples, Contract: sl.Contract}, questions})
	if err != nil {
		return nil, err
	}
	fails, err := schemaFailures(s.schemas.CheckProposal(doc))
	if err != nil {
		return nil, err
	}
	var out []string
	for _, f := range fails { // a fault at the document's root is reported once per part
		r := "the proposal breaks the proposal schema: " + f.Reason
		if !slices.Contains(out, r) {
			out = append(out, r)
		}
	}
	for _, f := range slice.Trace(doc) {
		switch {
		case f.Part == "": // the engine wrote the document, so this is a fault
			return nil, fmt.Errorf("slices: traceability: %s", f.Reason)
		case f.ID == "": // a syntax finding: the examples or the contract text does not parse
			out = append(out, f.Reason)
		}
	}
	return out, nil
}

// approvedFor returns the approved slice of exactly this description, if there is one.
func (s *Service) approvedFor(description string) (Slice, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.slices {
		if r.slice.Description == description && r.slice.State == "approved" {
			return clone(r.slice), true
		}
	}
	return Slice{}, false
}

// Get returns a slice, or NotFound.
func (s *Service) Get(id string) (Slice, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.find(id)
	if err != nil {
		return Slice{}, err
	}
	return clone(r.slice), nil
}

// List returns every slice, newest first (by the number of its ID).
func (s *Service) List() []Slice {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Slice, 0, len(s.slices))
	for _, r := range s.slices {
		out = append(out, clone(r.slice))
	}
	n := func(id string) int { v, _ := strconv.Atoi(strings.TrimPrefix(id, "S-")); return v }
	slices.SortFunc(out, func(a, b Slice) int { return n(b.ID) - n(a.ID) })
	return out
}

// find returns the stored slice with id. The caller holds s.mu.
func (s *Service) find(id string) (*record, error) {
	if _, err := s.schemas.ParseID(id); err != nil {
		return nil, &Error{Code: "InvalidRequest", Detail: err.Error()}
	}
	r, ok := s.slices[id]
	if !ok {
		return nil, &Error{Code: "NotFound", Detail: "slice " + id + " does not exist"}
	}
	return r, nil
}

// ReplaceGoals replaces the goals part.
func (s *Service) ReplaceGoals(ctx context.Context, id string, goals []Goal) (Slice, error) {
	return s.replace(ctx, id, PartGoals, func(sl *Slice) { sl.Goals = goals })
}

// ReplaceRules replaces the rules part: rules, examples and terms.
func (s *Service) ReplaceRules(ctx context.Context, id string, p RulesPart) (Slice, error) {
	return s.replace(ctx, id, PartRules, func(sl *Slice) { sl.Rules, sl.Examples, sl.Terms = p.Rules, p.Examples, p.Terms })
}

// ReplaceContract replaces the contract part.
func (s *Service) ReplaceContract(ctx context.Context, id, contract string) (Slice, error) {
	return s.replace(ctx, id, PartContract, func(sl *Slice) { sl.Contract = contract })
}

// replace changes one part. Content with the part's current hash is no change: the slice and its
// approval stay as they are. Otherwise the checks run again, the part's approval is withdrawn
// and the slice returns to checked (FR-007); every other part that now fails a check loses its
// approval too, recorded as withdrawn (FR-004).
//
// The lock is held deliberately for the whole change, the oasdiff comparison included, so that
// two changes to a slice cannot interleave between reading it and storing the result.
func (s *Service) replace(ctx context.Context, id string, part Part, set func(*Slice)) (Slice, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.find(id)
	if err != nil {
		return Slice{}, err
	}
	next := &record{slice: clone(r.slice), baseline: r.baseline, compat: r.compat, lint: r.lint}
	set(&next.slice)
	next.slice = clone(next.slice) // own the caller's lists
	normalise(&next.slice)
	if err := setParts(&next.slice); err != nil {
		return Slice{}, &Error{Code: "InvalidRequest", Detail: err.Error()}
	}
	if next.slice.Parts.of(part) == r.slice.Parts.of(part) {
		return clone(r.slice), nil
	}
	if err := s.recheck(ctx, next, part == PartContract); err != nil {
		return Slice{}, err
	}
	next.slice.Approvals = slices.DeleteFunc(next.slice.Approvals, func(a Approval) bool { return a.Part == part })
	now := s.now()
	next.slice.History = append(next.slice.History, HistoryEntry{Kind: "changed", At: now, Refs: []string{string(part)}})
	withdrawFailing(&next.slice, now)
	next.slice.State = state(&next.slice)
	*r = *next
	return clone(r.slice), nil
}

// Approve records the developer's approval of a part at the hash they reviewed (approvePart). A
// hash other than the part's current one gives StaleContentHash; an open question that concerns
// the part gives ContradictionOpen; a failing check gives PartFailsChecks, or PatternDoesNotFit
// for a contract whose only failures are pattern conditions, each listing the failures.
func (s *Service) Approve(ctx context.Context, id string, part Part, contentHash string) (Slice, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.approvable(id, part, contentHash)
	if err != nil {
		return Slice{}, err
	}
	for _, a := range r.slice.Approvals {
		if a.Part == part && a.ContentHash == contentHash {
			return clone(r.slice), nil // already approved at this hash
		}
	}

	now := s.now()
	r.slice.Approvals = append(slices.DeleteFunc(r.slice.Approvals, func(a Approval) bool { return a.Part == part }),
		Approval{Part: part, ContentHash: contentHash, At: now})
	if part == PartContract {
		r.baseline = r.slice.Contract
	}
	r.slice.History = append(r.slice.History, HistoryEntry{Kind: "approved", At: now, Refs: []string{string(part)}})
	r.slice.State = state(&r.slice)
	return clone(r.slice), nil
}

// CheckApprove refuses exactly as Approve would, and approves nothing.
func (s *Service) CheckApprove(id string, part Part, contentHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.approvable(id, part, contentHash)
	return err
}

// approvable returns the slice whose part Approve may approve at contentHash, or Approve's
// refusal. The caller holds s.mu.
func (s *Service) approvable(id string, part Part, contentHash string) (*record, error) {
	r, err := s.find(id)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(allParts, part) {
		return nil, &Error{Code: "InvalidRequest", Detail: fmt.Sprintf("%q is not a part", part)}
	}
	if _, err := s.schemas.ParseHash(contentHash); err != nil {
		return nil, &Error{Code: "InvalidRequest", Detail: err.Error()}
	}
	if contentHash != r.slice.Parts.of(part) {
		return nil, &Error{Code: "StaleContentHash",
			Detail: fmt.Sprintf("the %s part changed since it was shown; review it at its current hash", part)}
	}
	for _, q := range r.slice.Questions {
		if open(q) && concerns(q.About, part) {
			return nil, &Error{Code: "ContradictionOpen",
				Detail: fmt.Sprintf("question %s about %s is open and concerns the %s part; answer it first", q.ID, q.About, part)}
		}
	}
	var fails []Check
	onlyPattern := true
	for _, c := range r.slice.Checks {
		if c.Part == string(part) && !c.Passed {
			fails = append(fails, c)
			onlyPattern = onlyPattern && c.fromPattern
		}
	}
	if len(fails) > 0 {
		// The contract names PatternDoesNotFit for the contract part only; any other failing part,
		// pattern condition or not, fails its checks.
		code := "PartFailsChecks"
		if part == PartContract && onlyPattern {
			code = "PatternDoesNotFit"
		}
		return nil, &Error{Code: code, Detail: fmt.Sprintf("the %s part fails its checks", part), Failures: fails}
	}
	return r, nil
}

// Buildable reports whether a build of the slice may start: NotFound for an unknown slice,
// PatternDoesNotFit listing the failing pattern conditions, then NotApproved while a part holds
// no approval at its current hash or any check fails.
func (s *Service) Buildable(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.find(id)
	if err != nil {
		return err
	}
	var fails []Check
	for _, c := range r.slice.Checks {
		if c.fromPattern && !c.Passed {
			fails = append(fails, c)
		}
	}
	if len(fails) > 0 {
		return &Error{Code: "PatternDoesNotFit", Detail: "the slice fails a condition of the pattern", Failures: fails}
	}
	for _, part := range allParts {
		if !slices.ContainsFunc(r.slice.Approvals, func(a Approval) bool {
			return a.Part == part && a.ContentHash == r.slice.Parts.of(part)
		}) {
			return &Error{Code: "NotApproved", Detail: fmt.Sprintf("the %s part is not approved at its current hash", part)}
		}
	}
	if fails := failing(&r.slice); len(fails) > 0 {
		return &Error{Code: "NotApproved", Detail: "the slice fails a check", Failures: fails}
	}
	return nil
}

// failing returns the slice's failing checks.
func failing(sl *Slice) []Check {
	var out []Check
	for _, c := range sl.Checks {
		if !c.Passed {
			out = append(out, c)
		}
	}
	return out
}

// withdrawFailing withdraws the approval of every part that fails a check and, when it withdraws
// any, appends a withdrawn entry naming those parts. A slice never holds an approval of a part
// that fails a check (FR-004, FR-005).
func withdrawFailing(sl *Slice, at time.Time) {
	var refs []string
	sl.Approvals = slices.DeleteFunc(sl.Approvals, func(a Approval) bool {
		if slices.ContainsFunc(sl.Checks, func(c Check) bool { return c.Part == string(a.Part) && !c.Passed }) {
			refs = append(refs, string(a.Part))
			return true
		}
		return false
	})
	if len(refs) > 0 {
		sl.History = append(sl.History, HistoryEntry{Kind: "withdrawn", At: at, Refs: refs})
	}
}

// open reports whether a question is open: it has no answer yet.
func open(q Question) bool { return q.Answer == nil }

// concerns reports whether a question's subject (question_about: G-<n>, R-<n>, T-<n>,
// R-<n>/E-<n> or contract/<operationId>, or a part's name) lies in part. A subject of no known
// form concerns every part, so it blocks rather than slips through.
func concerns(about string, part Part) bool {
	switch {
	case about == string(PartGoals), strings.HasPrefix(about, "G-"):
		return part == PartGoals
	case about == string(PartRules), strings.HasPrefix(about, "R-"), strings.HasPrefix(about, "T-"), strings.HasPrefix(about, "E-"):
		return part == PartRules
	case about == string(PartContract), strings.HasPrefix(about, "contract/"):
		return part == PartContract
	default:
		return true
	}
}

// recheck sets the part hashes and runs the checks again; the compatibility check runs only when
// contractChanged, otherwise its last result is kept.
func (s *Service) recheck(ctx context.Context, r *record, contractChanged bool) error {
	if err := setParts(&r.slice); err != nil {
		return &Error{Code: "InvalidRequest", Detail: err.Error()}
	}
	cs, err := s.partChecks(&r.slice)
	if err != nil {
		return err
	}
	if contractChanged {
		r.compat = s.compatibility(ctx, r.baseline, r.slice.Contract)
		r.lint = s.lintContract(ctx, r.slice.Contract)
	}
	r.slice.Checks = append(append(cs, r.compat...), r.lint...)
	return nil
}

func setParts(sl *Slice) error {
	g, err := goalsHash(sl.Goals)
	if err != nil {
		return fmt.Errorf("goals: %w", err)
	}
	r, err := rulesHash(RulesPart{Rules: sl.Rules, Examples: sl.Examples, Terms: sl.Terms})
	if err != nil {
		return fmt.Errorf("rules: %w", err)
	}
	c, err := contractHash(sl.Contract)
	if err != nil {
		return fmt.Errorf("contract: %w", err)
	}
	sl.Parts = Parts{Goals: g, Rules: r, Contract: c}
	return nil
}

// state is the slice's lifecycle state: questions-open while a question is open, approved when
// all three parts hold an approval and no check fails, else checked.
func state(sl *Slice) string {
	switch {
	case slices.ContainsFunc(sl.Questions, open):
		return "questions-open"
	case len(sl.Approvals) == len(allParts) && len(failing(sl)) == 0:
		return "approved"
	default:
		return "checked"
	}
}

// normalise makes every nil list empty, so that the stored slice, its JSON and its hashes agree.
func normalise(sl *Slice) {
	sl.Goals, sl.Terms, sl.Rules = orEmpty(sl.Goals), orEmpty(sl.Terms), orEmpty(sl.Rules)
	for i := range sl.Rules {
		sl.Rules[i].Terms = orEmpty(sl.Rules[i].Terms)
	}
}

func orEmpty[T any](v []T) []T {
	if v == nil {
		return []T{}
	}
	return v
}

// clone copies a slice deeply, so neither the caller nor the store sees the other's changes.
func clone(sl Slice) Slice {
	c := sl
	c.Goals = slices.Clone(sl.Goals)
	c.Terms = slices.Clone(sl.Terms)
	c.Rules = slices.Clone(sl.Rules)
	for i := range c.Rules {
		c.Rules[i].Terms = slices.Clone(c.Rules[i].Terms)
	}
	c.Questions = slices.Clone(sl.Questions)
	for i := range c.Questions {
		c.Questions[i].Options = slices.Clone(c.Questions[i].Options)
		if a := c.Questions[i].Answer; a != nil {
			ac := *a
			if a.Option != nil {
				o := *a.Option
				ac.Option = &o
			}
			c.Questions[i].Answer = &ac
		}
	}
	c.Approvals = slices.Clone(sl.Approvals)
	c.Checks = slices.Clone(sl.Checks)
	c.History = slices.Clone(sl.History)
	for i := range c.History {
		c.History[i].Refs = slices.Clone(c.History[i].Refs)
	}
	return c
}
