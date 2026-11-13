package slices

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/agentic-platform/engine/checks"
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
	// steps are the names of the pattern's steps, in its order; nil when there is no pattern or
	// its steps cannot be read.
	steps []string
	now   func() time.Time
	log   *slog.Logger

	mu   sync.Mutex
	last int // the number of the last slice ID given out; IDs are never reused
	// reserved holds the IDs Reserve gave out that no slice holds yet; CreateAs takes one.
	reserved map[string]bool
	// unsaved holds the IDs Reserve gave out while the ID sequence could not be stored; they are
	// not reservations, so CreateAs refuses them and this service never gives them out again.
	unsaved map[string]bool
	slices  map[string]*record
	// changing serialises the changes of one slice (replace), so two of them never interleave
	// while oasdiff and the lint run without s.mu. Taken before s.mu, never while holding it.
	changing map[string]*sync.Mutex
	// records keeps the slices durably (nil: in memory only). seqPending is set while the ID
	// sequence Reserve advanced is not yet stored; no slice is stored until it is.
	records    Records
	seqPending bool
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
	// dropped maps the example IDs (E-<n>) earlier revisions dropped from the slice to a digest of
	// the scenario each was dropped with; an example ID is never reused, so one that comes back on
	// another scenario fails ExampleTagged.
	dropped map[string]string
	// examplesBase is the slice's last examples that parsed, which the next change of the
	// examples is compared with to find the IDs it drops.
	examplesBase string
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
	log := cfg.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	s := &Service{schemas: cfg.Schemas, analyst: cfg.Analyst, diff: cfg.Diff, lint: cfg.Lint, pattern: cfg.Pattern,
		attempts: attemptsOf(cfg.Pattern), steps: stepsOf(cfg.Pattern), now: now, log: log, reserved: map[string]bool{}, unsaved: map[string]bool{}, slices: map[string]*record{}, changing: map[string]*sync.Mutex{},
		records: cfg.Records}
	if s.records != nil {
		if err := s.load(); err != nil {
			return nil, err
		}
	}
	return s, nil
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

// stepsOf reads the step names from a pattern.yaml, its one home. A missing or unreadable
// pattern, or a step without a name, gives no steps (fail closed: no worker can register and no
// build can start).
func stepsOf(pattern []byte) []string {
	var p struct {
		Steps []struct {
			Name string `yaml:"name"`
		} `yaml:"steps"`
	}
	if pattern == nil || yaml.Unmarshal(pattern, &p) != nil {
		return nil
	}
	names := make([]string, 0, len(p.Steps))
	for _, st := range p.Steps {
		if st.Name == "" {
			return nil
		}
		names = append(names, st.Name)
	}
	return names
}

// Steps returns the names of the pattern's steps, in the pattern's order. It is empty when there
// is no pattern or its steps cannot be read.
func (s *Service) Steps() []string { return slices.Clone(s.steps) }

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
	id, err := s.nextID()
	if err != nil {
		return Result{}, err
	}
	rec.slice.ID = id
	if err := s.save(rec); err != nil {
		return Result{}, err
	}
	s.slices[rec.slice.ID] = rec
	return Result{Slice: clone(rec.slice)}, nil
}

// Reserve gives out the next slice ID from the sequence Create also draws from, without creating
// a slice; CreateAs then creates the slice under it. An ID given out is never given out again,
// whether or not a slice is created under it, and not after the slice is deleted or the service
// restarts on the same Records. When storing the sequence fails, the ID returned is no
// reservation: CreateAs refuses it with OperationFailed and nothing is stored under it, and
// Create fails the same way, until a later attempt stores the sequence.
func (s *Service) Reserve() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, err := s.nextID()
	if err != nil {
		id = "S-" + strconv.Itoa(s.last)
		s.unsaved[id] = true
		return id
	}
	s.reserved[id] = true
	return id
}

// CreateAs is Create under an ID Reserve gave out, with Create's refusals. A malformed ID, one
// Reserve did not give out, or one a slice already holds gives InvalidRequest before the analyst
// runs; one Reserve gave out while it could not store the ID sequence gives OperationFailed. A refused CreateAs leaves the ID reserved, so it may be retried; an approved slice of the
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
	if err := s.storeSequence(); err != nil {
		return Result{}, err
	}
	rec.slice.ID = id
	if err := s.save(rec); err != nil {
		return Result{}, err
	}
	delete(s.reserved, id)
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
	if s.unsaved[id] {
		return &Error{Code: "OperationFailed", Detail: "slice ID " + id + " was not reserved: the slice ID sequence could not be stored"}
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

	p, sl, attempts, err := s.propose(ctx, AnalystInput{Description: description}, 0)
	if err != nil {
		return Result{}, nil, err
	}
	for i, q := range p.Questions {
		sl.Questions = append(sl.Questions, Question{ID: "Q-" + strconv.Itoa(i+1), About: q.About, Text: q.Text,
			Options: slices.Clone(q.Options)})
	}
	rec := &record{slice: sl, examplesBase: sl.Examples}
	if err := s.recheck(ctx, rec, true); err != nil {
		return Result{}, nil, err
	}
	rec.slice.History = []HistoryEntry{{Kind: "proposed", At: s.now(), Model: p.Model, AgentDigest: p.AgentDigest,
		Refs: []string{string(PartGoals), string(PartRules), string(PartContract)}, Attempts: attempts}}
	rec.slice.State = state(&rec.slice)
	return Result{}, rec, nil
}

// maxQuestions is the most questions a slice may have (FR-006; slice.schema.json questions
// maxItems, which the proposal schema also gives).
const maxQuestions = 10

// propose asks the analyst for a proposal from base (a description, and when revising the
// current proposal and the questions) and builds the slice from it; asked is the number of
// questions the slice already has, which count against the limit with the new ones. An unusable
// proposal is sent back with its faults until one is usable or s.attempts calls were made; it
// returns the number of calls made. An analyst error, a description of several use cases and too
// many questions are returned at once, not retried.
func (s *Service) propose(ctx context.Context, base AnalystInput, asked int) (Proposal, Slice, int, error) {
	description := base.Description
	in := base
	for attempt := 1; ; attempt++ {
		p, err := s.analyst.Propose(ctx, in)
		if errors.Is(err, ErrMultipleUseCases) {
			return Proposal{}, Slice{}, 0, &Error{Code: "NarrowDescription", Detail: "the description covers more than one use case; describe one"}
		}
		if err != nil {
			return Proposal{}, Slice{}, 0, fmt.Errorf("slices: the analyst failed: %w", err)
		}
		sl := clone(Slice{ // own the analyst's lists
			Description: description, Contract: p.Contract, Examples: p.Examples,
			Goals: p.Goals, Terms: p.Terms, Rules: p.Rules,
		})
		normalise(&sl)
		if asked+len(p.Questions) > maxQuestions {
			return Proposal{}, Slice{}, 0, &Error{Code: "TooManyQuestions",
				Detail: fmt.Sprintf("the slice would need %d questions, more than %d; narrow the description", asked+len(p.Questions), maxQuestions)}
		}
		faults, err := s.unusable(&sl, p.Questions)
		if err != nil {
			return Proposal{}, Slice{}, 0, err
		}
		if len(faults) == 0 {
			return p, sl, attempt, nil
		}
		if attempt >= s.attempts {
			return Proposal{}, Slice{}, 0, &Error{Code: "OperationFailed", Detail: fmt.Sprintf(
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
// of slice.Trace (examples that are not Gherkin or a contract that is not YAML), and a contract
// checks.Fit cannot read as one YAML document without duplicate keys. Other faults are
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
	contractRead := true
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
			contractRead = contractRead && f.Part != string(PartContract)
		}
	}
	// A contract that YAML parses but checks.Fit cannot read (duplicate keys, several documents)
	// was not checked at all; the analyst is asked for a readable one. An unreadable pattern is
	// no fault of the proposal: it stays a stored failed check.
	if s.pattern != nil && contractRead {
		for _, r := range checks.Fit(s.pattern, doc) {
			if r.Unreadable && !slices.Contains(out, r.Reason) {
				out = append(out, r.Reason)
			}
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
// The service's lock is not held across oasdiff and the lint call, so they hold up no other slice.
// Changes of one slice wait for each other, so both land. A contract change reads the slice under
// the lock, runs both without it, then takes the lock again and applies the change to the slice
// as it is then (an approval or an answer may have changed it). Their results depend only on the new contract and the approved
// baseline, so a change that landed meanwhile is kept and the change applies on top of it; only
// when the baseline changed meanwhile do they run again, at most maxRechecks times, then the
// change ends with OperationFailed. The change is stored under the lock before it is reported.
func (s *Service) replace(ctx context.Context, id string, part Part, set func(*Slice)) (Slice, error) {
	s.mu.Lock()
	if _, err := s.find(id); err != nil {
		s.mu.Unlock()
		return Slice{}, err
	}
	m := s.changing[id]
	if m == nil {
		m = &sync.Mutex{}
		s.changing[id] = m
	}
	s.mu.Unlock()
	m.Lock()
	defer m.Unlock()

	var ran *contractRun
	for runs := 0; ; {
		s.mu.Lock()
		r, err := s.find(id)
		if err != nil {
			s.mu.Unlock()
			return Slice{}, err
		}
		next := &record{slice: clone(r.slice), baseline: r.baseline, compat: r.compat, lint: r.lint}
		set(&next.slice)
		next.slice = clone(next.slice) // own the caller's lists
		normalise(&next.slice)
		if err := setParts(&next.slice); err != nil {
			s.mu.Unlock()
			return Slice{}, &Error{Code: "InvalidRequest", Detail: err.Error()}
		}
		if next.slice.Parts.of(part) == r.slice.Parts.of(part) {
			s.mu.Unlock()
			return clone(r.slice), nil
		}
		if part == PartContract && (ran == nil || ran.baseline != next.baseline || ran.contract != next.slice.Contract) {
			if runs >= maxRechecks {
				s.mu.Unlock()
				return Slice{}, &Error{Code: "OperationFailed", Detail: fmt.Sprintf(
					"the approved contract changed %d times while the new contract was checked; change it again", runs)}
			}
			runs++
			ran = &contractRun{baseline: next.baseline, contract: next.slice.Contract}
			s.mu.Unlock()
			ran.compat = s.compatibility(ctx, ran.baseline, ran.contract)
			ran.lint = s.lintContract(ctx, ran.contract)
			continue
		}
		if part == PartContract {
			next.compat, next.lint = ran.compat, ran.lint
		}
		res, err := s.apply(r, next, part)
		s.mu.Unlock()
		return res, err
	}
}

// maxRechecks is how often a contract change runs oasdiff and the lint before it gives up on a
// baseline that keeps changing.
const maxRechecks = 3

// contractRun is the result of oasdiff and the lint for a contract against a baseline.
type contractRun struct {
	baseline, contract string
	compat, lint       []Check
}

// apply finishes a change of part from r to next, whose compatibility and lint results are
// current, stores it and reports it. The caller holds s.mu.
func (s *Service) apply(r, next *record, part Part) (Slice, error) {
	next.dropped, next.examplesBase = dropExamples(r.dropped, r.examplesBase, next.slice.Examples)
	if err := s.recheck(context.Background(), next, false); err != nil {
		return Slice{}, err
	}
	next.slice.Approvals = slices.DeleteFunc(next.slice.Approvals, func(a Approval) bool { return a.Part == part })
	now := s.now()
	next.slice.History = append(next.slice.History, HistoryEntry{Kind: "changed", At: now, Refs: []string{string(part)}})
	withdrawFailing(&next.slice, now)
	next.slice.State = state(&next.slice)
	if err := s.save(next); err != nil {
		return Slice{}, err
	}
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
	next := &record{slice: clone(r.slice), baseline: r.baseline, compat: r.compat, lint: r.lint, dropped: r.dropped, examplesBase: r.examplesBase}
	next.slice.Approvals = append(slices.DeleteFunc(next.slice.Approvals, func(a Approval) bool { return a.Part == part }),
		Approval{Part: part, ContentHash: contentHash, At: now})
	if part == PartContract {
		next.baseline = next.slice.Contract
	}
	next.slice.History = append(next.slice.History, HistoryEntry{Kind: "approved", At: now, Refs: []string{string(part)}})
	next.slice.State = state(&next.slice)
	if err := s.save(next); err != nil {
		return Slice{}, err
	}
	*r = *next
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
			onlyPattern = onlyPattern && c.Pattern
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
		if c.Pattern && !c.Passed {
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
	cs, err := s.partChecks(&r.slice, r.dropped)
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
