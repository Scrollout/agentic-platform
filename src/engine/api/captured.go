package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/agentic-platform/engine/api/gen"
	"github.com/agentic-platform/engine/canonical"
	"github.com/agentic-platform/engine/captured"
	"github.com/agentic-platform/engine/machine"
	"github.com/agentic-platform/engine/method"
	sl "github.com/agentic-platform/engine/slices"
	"github.com/agentic-platform/engine/store"
	"go.yaml.in/yaml/v3"
)

// The /captured-patterns resource (engine contract, tag captured-patterns): capture a done run as a
// pattern that builds later slices of the same kind with no model call, change its parameters
// and approve it once a proof passed. Its rules (candidates, forms, places) are package
// engine/captured's; this file keeps the records, judges the states and runs the operations.
// makeProofSlice and the proofs' runs are in proof_slice.go, withdrawal and erasure in
// captured_erase.go.

// errNotImplemented is what a captured-pattern call answers on an engine that keeps no slices.
var errNotImplemented = errors.New("captured patterns are not implemented yet")

// fillWorker is the worker of a captured pattern's `fill` steps, which need no step worker.
const fillWorker = captured.FillWorker

// capturedVersion is every captured pattern's version: an approved one never changes.
const capturedVersion = "1.0.0"

// capturedList is the list that issues page tokens for captured patterns.
const capturedList = "captured-patterns"

// CapturedStore keeps the captured patterns' records and holds the artifacts of their templates;
// *store.Store has it. A Config.Store without it keeps no captured pattern: every
// captured-pattern call is the engine's fault (500).
type CapturedStore interface {
	// PutCapturedPattern holds the artifacts named by holds for the captured pattern, then
	// replaces its record, durably.
	PutCapturedPattern(id string, record []byte, holds []string) error
	// CapturedPattern returns the record, or an error wrapping store.ErrNotFound.
	CapturedPattern(id string) ([]byte, error)
	// CapturedPatterns returns every record by ID.
	CapturedPatterns() (map[string][]byte, error)
	// EraseCapturedPattern erases the record and the artifacts only it holds, keeping the ID taken
	// for good; calling it again completes an interrupted erasure.
	EraseCapturedPattern(id string) error
	// CapturedPatternErased reports whether the ID was erased (or its erasure begun).
	CapturedPatternErased(id string) (bool, error)
}

// capturedRecord is a captured pattern as the engine keeps it (data model, "Captured pattern").
type capturedRecord struct {
	ID          string             `json:"id"`
	Number      int                `json:"number"`    // the order of capture: newest has the highest
	Operation   string             `json:"operation"` // the capture that made it
	Base        string             `json:"base"`
	Method      string             `json:"method"`
	SourceRun   string             `json:"source_run"`
	SourceSlice string             `json:"source_slice"`
	Parts       machine.PartHashes `json:"parts"`
	Copy        capturedCopy       `json:"copy"`
	Declaration string             `json:"declaration"`
	// Template is each filled output's artifact hash, by output name.
	Template   map[string]string            `json:"template"`
	Parameters []captured.Parameter         `json:"parameters"`
	ProofSlice string                       `json:"proof_slice,omitempty"`
	Proofs     []gen.Proof                  `json:"proofs"`
	Approval   *gen.CapturedPatternApproval `json:"approval,omitempty"`
	State      gen.CapturedPatternState     `json:"state"`
	CreatedAt  time.Time                    `json:"created_at"`
}

// capturedCopy is the source slice's approved parts, each as its canonical JSON.
type capturedCopy struct {
	Goals    string `json:"goals"`
	Rules    string `json:"rules"`
	Contract string `json:"contract"`
}

func (r *capturedRecord) ref() string { return r.ID + "@" + capturedVersion }

func (r *capturedRecord) src() captured.Copy {
	return captured.Copy{Goals: []byte(r.Copy.Goals), Rules: []byte(r.Copy.Rules), Contract: []byte(r.Copy.Contract)}
}

// values are the parameters' source values by name.
func (r *capturedRecord) values() map[string]string {
	out := make(map[string]string, len(r.Parameters))
	for _, p := range r.Parameters {
		out[p.Name] = p.Value
	}
	return out
}

// pattern is the captured pattern's declaration as a run follows it, under its own ID and version.
func (r *capturedRecord) pattern() (machine.Pattern, error) {
	var p sl.Pattern
	if err := yaml.Unmarshal([]byte(r.Declaration), &p); err != nil {
		return machine.Pattern{}, fmt.Errorf("captured pattern %s: its declaration is unreadable: %w", r.ID, err)
	}
	mp := MachinePattern(p)
	mp.ID, mp.Version = r.ID, capturedVersion
	return mp, nil
}

// latest is the newest proof, nil before the first.
func (r *capturedRecord) latest() *gen.Proof {
	if len(r.Proofs) == 0 {
		return nil
	}
	return &r.Proofs[len(r.Proofs)-1]
}

// provenUnder reports whether the latest proof passed under the method with the current
// parameters: what approval and `usable` need.
func (r *capturedRecord) provenUnder(m string) bool {
	p := r.latest()
	return p != nil && p.Outcome == gen.ProofOutcomePassed && p.Method == m && maps.Equal(p.Parameters, r.values())
}

// CapturedParameters gives a captured pattern's parameters with their places from the records
// the engine API keeps in Store: what a run's `fill` steps are filled with
// (durable.Activities.Captured). An unknown captured pattern is an error wrapping
// store.ErrNotFound.
type CapturedParameters struct{ Store CapturedStore }

// Parameters returns the captured pattern's parameters.
func (c CapturedParameters) Parameters(id string) ([]captured.Parameter, error) {
	r, err := decodeCaptured(c.Store, id)
	if err != nil {
		return nil, err
	}
	return r.Parameters, nil
}

func decodeCaptured(st CapturedStore, id string) (*capturedRecord, error) {
	b, err := st.CapturedPattern(id)
	if err != nil {
		return nil, err
	}
	var r capturedRecord
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("captured pattern %s: its record is unreadable: %w", id, err)
	}
	return &r, nil
}

// currentMethod is the engine's method identity.
func currentMethod() (string, error) {
	id, err := method.Current()
	if err != nil {
		return "", fmt.Errorf("the engine's method identity: %w", err)
	}
	return id.String(), nil
}

func capturedNotFound(id string) error {
	return &sl.Error{Code: string(gen.ErrorCodeNotFound), Detail: "captured pattern " + id + " does not exist"}
}

// capturedStore is the store of captured patterns, or the engine's fault when it keeps none.
func (s *sliceServer) capturedStore() (CapturedStore, error) {
	if s.captured == nil || s.runs == nil {
		return nil, errors.New("the engine keeps no captured patterns: its store keeps no runs or captured patterns")
	}
	return s.captured, nil
}

// readCaptured returns the captured pattern's record: InvalidRequest for an ID outside the
// contract's pattern_id rule, NotFound for an unknown one.
func (s *sliceServer) readCaptured(id string) (*capturedRecord, error) {
	st, err := s.capturedStore()
	if err != nil {
		return nil, err
	}
	if !fits(s.contract.patternID, id) {
		return nil, &sl.Error{Code: string(gen.ErrorCodeInvalidRequest), Detail: fmt.Sprintf("%q is not a pattern ID", id)}
	}
	r, err := decodeCaptured(st, id)
	if errors.Is(err, store.ErrNotFound) {
		return nil, capturedNotFound(id)
	}
	return r, err
}

func (s *sliceServer) saveCaptured(r *capturedRecord) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	holds := slices.Sorted(maps.Values(r.Template))
	return s.captured.PutCapturedPattern(r.ID, b, holds)
}

// allCaptured returns every captured pattern, newest first.
func (s *sliceServer) allCaptured() ([]*capturedRecord, error) {
	st, err := s.capturedStore()
	if err != nil {
		return nil, err
	}
	recs, err := st.CapturedPatterns()
	if err != nil {
		return nil, err
	}
	out := make([]*capturedRecord, 0, len(recs))
	for id := range recs {
		r, err := decodeCaptured(st, id)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Number > out[j].Number })
	return out, nil
}

// ---- capture ----

// CaptureRun refuses at once, before any operation exists: an unknown run (NotFound), one that
// has not ended done (RunNotDone, naming its state), one that cannot be a source
// (RunNotCapturable, sourceRefusal), and an ID a captured pattern holds or a capture in progress
// will hold (CapturedPatternExists). Otherwise it accepts the capture, whose target is the new
// captured pattern's ID `<base pattern id>-<name>`, name defaulting to the slice ID in lower case.
func (s *sliceServer) CaptureRun(ctx context.Context, req gen.CaptureRunRequestObject) (gen.CaptureRunResponseObject, error) {
	if _, err := s.capturedStore(); err != nil {
		return nil, err
	}
	name := ""
	if req.Body.Name != nil {
		name = *req.Body.Name
	}
	s.cmu.Lock()
	defer s.cmu.Unlock()
	id, err := s.capturable(req.Body.Run, name)
	if err != nil {
		return s.refuse("captureRun", err)
	}
	op, err := s.ops.accept(ctx, "captureRun", id)
	if err != nil {
		return unavailableFor(err), nil
	}
	return gen.CaptureRun202JSONResponse{AcceptedJSONResponse: gen.AcceptedJSONResponse{Body: op}}, nil
}

// capturable gives the ID a capture of the run under name takes, or its refusal. The caller
// holds s.cmu.
func (s *sliceServer) capturable(run, name string) (string, error) {
	_, events, _, ok, err := s.runs.find(run)
	switch {
	case err != nil:
		return "", err
	case !ok:
		return "", runNotFound(run)
	}
	if v := machine.StateOf(events); v.Status != machine.StatusEnded || v.Outcome != machine.OutcomeDone {
		return "", &sl.Error{Code: string(gen.ErrorCodeRunNotDone), Detail: fmt.Sprintf("run %s is %s, not done", run, describe(v))}
	}
	if err := s.sourceRefusal(run, events[0]); err != nil {
		return "", err
	}
	if name == "" {
		name = strings.ToLower(events[0].Slice)
	}
	id := s.runs.pattern.ID + "-" + name
	switch _, err := s.captured.CapturedPattern(id); {
	case err == nil:
		return "", &sl.Error{Code: string(gen.ErrorCodeCapturedPatternExists), Detail: "captured pattern " + id + " exists"}
	case !errors.Is(err, store.ErrNotFound):
		return "", err
	}
	switch erased, err := s.captured.CapturedPatternErased(id); {
	case err != nil:
		return "", err
	case erased:
		return "", &sl.Error{Code: string(gen.ErrorCodeCapturedPatternExists), Detail: "captured pattern " + id + " was erased: its ID is never given again"}
	}
	open, err := s.ops.records.OpenOperations()
	if err != nil {
		return "", err
	}
	for _, o := range open {
		if o.Kind == "captureRun" && o.Target == id {
			return "", &sl.Error{Code: string(gen.ErrorCodeCapturedPatternExists),
				Detail: fmt.Sprintf("operation %s is capturing %s", o.ID, id)}
		}
	}
	return id, nil
}

// sourceRefusal refuses RunNotCapturable a run that cannot be a captured pattern's source,
// naming the cause: a proof, a run of a captured pattern, a run under another pattern than the
// engine's base pattern (naming both), a run whose RunStarted records no part hashes, and a run
// whose slice changed after it started (a part's hash differs from the recorded one, or the part
// is no longer approved, naming the part).
func (s *sliceServer) sourceRefusal(run string, started machine.Event) error {
	if s.runs.noRun != "" {
		return errors.New(s.runs.noRun)
	}
	refuse := func(format string, a ...any) error {
		return &sl.Error{Code: string(gen.ErrorCodeRunNotCapturable), Detail: fmt.Sprintf(format, a...)}
	}
	base := s.runs.pattern.ID + "@" + s.runs.pattern.Version
	switch {
	case started.Proof:
		return refuse("run %s is a proof of %s", run, started.Pattern)
	case started.SourceRun != "" || started.Parameters != nil:
		return refuse("run %s was built from the captured pattern %s", run, started.Pattern)
	case started.Pattern != base:
		return refuse("run %s was built under the pattern %s, not the engine's base pattern %s", run, started.Pattern, base)
	case started.PartHashes == nil:
		return refuse("run %s records no part hashes: it started before captured patterns existed, or replays such a run", run)
	}
	slc, err := s.svc.Get(started.Slice)
	if err != nil {
		return err
	}
	for _, p := range []struct {
		part          sl.Part
		recorded, now string
	}{{sl.PartGoals, started.PartHashes.Goals, slc.Parts.Goals}, {sl.PartRules, started.PartHashes.Rules, slc.Parts.Rules},
		{sl.PartContract, started.PartHashes.Contract, slc.Parts.Contract}} {
		if p.now != p.recorded {
			return refuse("the %s part of slice %s changed after run %s started", p.part, started.Slice, run)
		}
		if !slices.ContainsFunc(slc.Approvals, func(a sl.Approval) bool { return a.Part == p.part && a.ContentHash == p.recorded }) {
			return refuse("the %s part of slice %s is no longer approved", p.part, started.Slice)
		}
	}
	return nil
}

// capture is the work of a capture: it judges the run again, copies the slice's approved parts,
// reads the template (each filled output's last accepted files), proposes the parameters and
// records the captured pattern, which then holds the template's artifacts. A capture that fails
// records nothing; a repeat finds the captured pattern it recorded.
func (s *sliceServer) capture(rec store.OperationRecord, body []byte) (WorkResult, error) {
	id := rec.Target
	switch r, err := s.readCaptured(id); {
	case err == nil && r.Operation == rec.ID:
		return WorkResult{CapturedPattern: id}, nil
	case err == nil:
		return WorkResult{}, &WorkError{Code: gen.ErrorCodeOperationFailed, Detail: "captured pattern " + id + " exists"}
	case !isNotFound(err):
		return WorkResult{}, err
	}
	var req gen.CaptureRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return WorkResult{}, err
	}
	_, events, _, ok, err := s.runs.find(req.Run)
	switch {
	case err != nil:
		return WorkResult{}, err
	case !ok:
		return WorkResult{}, s.workError("captureRun", runNotFound(req.Run))
	}
	started := events[0]
	if err := s.sourceRefusal(req.Run, started); err != nil {
		return WorkResult{}, s.workError("captureRun", err)
	}
	parts, err := s.svc.ApprovedParts(started.Slice)
	if err != nil {
		return WorkResult{}, s.workError("captureRun", err)
	}
	for part, want := range map[string]string{"goals": started.PartHashes.Goals, "rules": started.PartHashes.Rules, "contract": started.PartHashes.Contract} {
		if sum := sha256.Sum256(parts[part]); hex.EncodeToString(sum[:]) != want {
			return WorkResult{}, &WorkError{Code: gen.ErrorCodeOperationFailed,
				Detail: fmt.Sprintf("the %s part of slice %s changed after run %s started", part, started.Slice, req.Run)}
		}
	}
	decl, err := captured.Declaration(s.svc.PatternDeclaration())
	if err != nil {
		return WorkResult{}, err
	}
	m, err := currentMethod()
	if err != nil {
		return WorkResult{}, err
	}
	r := &capturedRecord{ID: id, Operation: rec.ID, Base: started.Pattern, Method: m, SourceRun: req.Run, SourceSlice: started.Slice,
		Parts: *started.PartHashes, Copy: capturedCopy{Goals: string(parts["goals"]), Rules: string(parts["rules"]), Contract: string(parts["contract"])},
		Declaration: string(decl), Proofs: []gen.Proof{}, State: gen.CapturedPatternStateProposed}
	filled, err := r.pattern()
	if err != nil {
		return WorkResult{}, err
	}
	if r.Template, _, err = lastOutputs(events, s.runs.pattern, filled); err != nil {
		return WorkResult{}, fmt.Errorf("run %s: %w", req.Run, err)
	}
	tmpl, err := s.templateFiles(r.Template)
	if err != nil {
		return WorkResult{}, err
	}
	if r.Parameters, err = captured.Propose(r.src(), tmpl); err != nil {
		return WorkResult{}, err
	}
	s.cmu.Lock()
	defer s.cmu.Unlock()
	all, err := s.allCaptured()
	if err != nil {
		return WorkResult{}, err
	}
	for _, o := range all {
		r.Number = max(r.Number, o.Number)
		if o.ID == id {
			return WorkResult{}, &WorkError{Code: gen.ErrorCodeOperationFailed, Detail: "captured pattern " + id + " exists"}
		}
	}
	switch erased, err := s.captured.CapturedPatternErased(id); {
	case err != nil:
		return WorkResult{}, err
	case erased:
		return WorkResult{}, &WorkError{Code: gen.ErrorCodeOperationFailed, Detail: "captured pattern " + id + " was erased: its ID is never given again"}
	}
	r.Number++
	r.CreatedAt = s.ops.now().UTC()
	if err := s.saveCaptured(r); err != nil {
		return WorkResult{}, Retryable(err)
	}
	return WorkResult{CapturedPattern: id}, nil
}

func isNotFound(err error) bool {
	var e *sl.Error
	return errors.As(err, &e) && e.Code == string(gen.ErrorCodeNotFound)
}

// lastOutputs gives, for each output of each of filled's `fill` steps, the artifact hash of that
// step's last acceptance in the run's record (StepAccepted or ResultReused, after any review
// round's fix) and the step, the run having followed the pattern ran. An output with no
// acceptance is an error.
func lastOutputs(events []machine.Event, ran, filled machine.Pattern) (hashes, steps map[string]string, err error) {
	hashes, steps = map[string]string{}, map[string]string{}
	for _, f := range filled.Steps {
		if f.Worker != fillWorker {
			continue
		}
		i := slices.IndexFunc(ran.Steps, func(s machine.Step) bool { return s.Name == f.Name })
		if i < 0 {
			return nil, nil, fmt.Errorf("the run's pattern has no step %s", f.Name)
		}
		for _, o := range f.Outputs {
			at := slices.Index(ran.Steps[i].Outputs, o)
			if at < 0 {
				return nil, nil, fmt.Errorf("step %s of the run's pattern has no output %s", f.Name, o)
			}
			for _, e := range events {
				if e.Step == f.Name && !e.OpenRound && (e.Kind == "StepAccepted" || e.Kind == "ResultReused") && at < len(e.Artifacts) {
					hashes[o] = e.Artifacts[at]
				}
			}
			if hashes[o] == "" {
				return nil, nil, fmt.Errorf("step %s was never accepted with its output %s", f.Name, o)
			}
			steps[o] = f.Name
		}
	}
	return hashes, steps, nil
}

// templateFiles reads each output's files from the artifact store.
func (s *sliceServer) templateFiles(hashes map[string]string) (captured.Template, error) {
	out := captured.Template{}
	for o, h := range hashes {
		b, err := s.runs.st.Artifact(h)
		if err != nil {
			return nil, fmt.Errorf("the template's output %s (%s) cannot be read: %w", o, h, err)
		}
		files, err := canonical.Unframe(b)
		if err != nil {
			return nil, fmt.Errorf("the template's output %s (%s) is not a set of files: %w", o, h, err)
		}
		out[o] = files
	}
	return out, nil
}

// ---- reads ----

func (s *sliceServer) ListCapturedPatterns(_ context.Context, req gen.ListCapturedPatternsRequestObject) (gen.ListCapturedPatternsResponseObject, error) {
	s.cmu.Lock()
	defer s.cmu.Unlock()
	all, err := s.allCaptured()
	if err != nil {
		return nil, err
	}
	numbers := make([]int, len(all))
	for i, r := range all {
		numbers[i] = r.Number
	}
	from, to, next, ok := s.pages.keysetPage(capturedList, req.Params.PageToken, numbers)
	if !ok {
		return problemResponse(badPageToken()), nil
	}
	m, err := currentMethod()
	if err != nil {
		return nil, err
	}
	list := gen.CapturedPatternList{Items: make([]gen.CapturedPattern, 0, to-from), NextPageToken: next}
	for _, r := range all[from:to] {
		if err := s.completeAndSave(r, m); err != nil {
			return nil, err
		}
		list.Items = append(list.Items, capturedView(r, m))
	}
	return gen.ListCapturedPatterns200JSONResponse{CapturedPatternListJSONResponse: gen.CapturedPatternListJSONResponse{Body: list}}, nil
}

func (s *sliceServer) GetCapturedPattern(_ context.Context, req gen.GetCapturedPatternRequestObject) (gen.GetCapturedPatternResponseObject, error) {
	s.cmu.Lock()
	defer s.cmu.Unlock()
	r, err := s.readCaptured(req.CapturedPatternId)
	if err != nil {
		return s.refuse("getCapturedPattern", err)
	}
	m, err := currentMethod()
	if err != nil {
		return nil, err
	}
	if err := s.completeAndSave(r, m); err != nil {
		return nil, err
	}
	return gen.GetCapturedPattern200JSONResponse{CapturedPatternJSONResponse: gen.CapturedPatternJSONResponse{Body: capturedView(r, m)}}, nil
}

// capturedView is the captured pattern as the contract shows it, under the engine's method m.
func capturedView(r *capturedRecord, m string) gen.CapturedPattern {
	v := gen.CapturedPattern{Id: r.ID, Version: capturedVersion, Base: r.Base, Method: r.Method, CreatedAt: r.CreatedAt,
		Parts: gen.PartHashes{Goals: r.Parts.Goals, Rules: r.Parts.Rules, Contract: r.Parts.Contract}, State: r.State,
		Parameters: make([]gen.Parameter, len(r.Parameters)), Proofs: slices.Clone(r.Proofs), Approval: r.Approval,
		ProofSlice: opt(r.ProofSlice), Usable: r.usable(m)}
	if v.Proofs == nil {
		v.Proofs = []gen.Proof{}
	}
	v.Source.Run, v.Source.Slice = r.SourceRun, r.SourceSlice
	v.Template.Tests, v.Template.Code, v.Template.Witnesses = r.Template["tests"], r.Template["code"], r.Template["witnesses"]
	for i, p := range r.Parameters {
		places := make([]gen.ParameterPlace, len(p.Places))
		for j, pl := range p.Places {
			places[j] = gen.ParameterPlace{Part: pl.Part, In: gen.ParameterPlaceIn(pl.In), Line: pl.Line, Column: pl.Column,
				Form: gen.ParameterPlaceForm(pl.Form)}
		}
		v.Parameters[i] = gen.Parameter{Name: p.Name, Kind: gen.ParameterKind(p.Kind), Value: p.Value, Source: gen.ParameterSource(p.Source),
			Origin: gen.ParameterOrigin(p.Origin), Places: places}
	}
	return v
}

// ---- proofs ----

// completeAndSave completes the captured pattern's proofs (completeProofs) and keeps the record
// when that changed it. The caller holds s.cmu.
func (s *sliceServer) completeAndSave(r *capturedRecord, m string) error {
	changed, err := s.completeProofs(r, m)
	if err != nil || !changed {
		return err
	}
	return s.saveCaptured(r)
}

// completeProofs is the one Proof-completion function every reader of a proof's state goes
// through (data model, "Proof"): each run of the captured pattern's proof slice whose RunStarted
// has `proof: true` and names the captured pattern gets a Proof (`running`, under the run's
// method, with the values it proves) when it has none; a running Proof whose run was handed over
// or has ended is completed: a hand-over fails it naming the failing check (and, once the run
// then expired or was cancelled, that end too, also on a Proof completed before), an end other than
// done fails it naming the end, and done passes it when the filled outputs' hashes equal the
// template's, else fails it naming each differing file; an interrupted or running run leaves it
// running. A proof that passes with the current parameters makes a proposed captured pattern
// proven; one that fails makes a proven one proposed. Completing again changes nothing. m is the
// engine's method, a Proof's method when its run records none. It reports whether r changed.
func (s *sliceServer) completeProofs(r *capturedRecord, m string) (bool, error) {
	if r.ProofSlice == "" || s.runs == nil {
		return false, nil
	}
	changed := false
	for _, e := range s.runs.st.Runs() {
		if e.Slice != r.ProofSlice {
			continue
		}
		events, _, err := s.runs.read(e.Run)
		if err != nil {
			return false, err
		}
		if len(events) == 0 || !events[0].Proof || events[0].Pattern != r.ref() {
			continue
		}
		i := slices.IndexFunc(r.Proofs, func(p gen.Proof) bool { return p.Run == e.Run })
		if i < 0 {
			values := gen.ParameterValues{}
			maps.Copy(values, events[0].Parameters)
			r.Proofs = append(r.Proofs, gen.Proof{Run: e.Run, Method: methodOf(events, m), Outcome: gen.ProofOutcomeRunning,
				Parameters: values, Failures: []gen.ProofFailure{}})
			i, changed = len(r.Proofs)-1, true
		}
		if r.Proofs[i].Outcome != gen.ProofOutcomeRunning && !endUnnamed(r.Proofs[i], events) {
			continue
		}
		outcome, failures, err := s.proofEnd(r, events)
		if err != nil {
			return false, err
		}
		if outcome == gen.ProofOutcomeRunning {
			continue
		}
		p := &r.Proofs[i]
		p.Outcome, p.Failures, changed = outcome, failures, true
		switch {
		case outcome == gen.ProofOutcomePassed && r.State == gen.CapturedPatternStateProposed && maps.Equal(p.Parameters, r.values()):
			r.State = gen.CapturedPatternStateProven
		case outcome == gen.ProofOutcomeFailed && r.State == gen.CapturedPatternStateProven:
			r.State = gen.CapturedPatternStateProposed
		}
	}
	return changed, nil
}

// methodOf is the method identity a run's steps were judged under, or m when it records none.
func methodOf(events []machine.Event, m string) string {
	for _, e := range events {
		if e.Method != "" {
			return e.Method
		}
	}
	return m
}

// endUnnamed reports whether a proof that failed at its run's hand-over does not yet name the
// end its run came to since (expired or cancelled), which completing it again adds.
func endUnnamed(p gen.Proof, events []machine.Event) bool {
	reason, ok := runEnd(machine.StateOf(events))
	return ok && p.Outcome == gen.ProofOutcomeFailed && !slices.ContainsFunc(p.Failures, func(f gen.ProofFailure) bool { return f.Reason == reason })
}

// runEnd is the failure reason that names the end of a run that ended other than done or handed
// over (expired, cancelled); ok is false for any other run.
func runEnd(v machine.Run) (reason string, ok bool) {
	if v.Status != machine.StatusEnded || v.Outcome == machine.OutcomeDone || v.Outcome == machine.OutcomeHandedOver {
		return "", false
	}
	return "the run ended " + string(v.Outcome), true
}

// proofEnd judges a proof run's record: its outcome (running while it has not been handed over
// or ended) and, when it failed, why: a hand-over names the failing check, followed by the end
// the run came to after it (expired, cancelled) once it has.
func (s *sliceServer) proofEnd(r *capturedRecord, events []machine.Event) (gen.ProofOutcome, []gen.ProofFailure, error) {
	v := machine.StateOf(events)
	for i, e := range events {
		if e.Kind != "RunHandedOver" {
			continue
		}
		var failures []gen.ProofFailure
		for j := i - 1; j >= 0 && failures == nil; j-- {
			if c := events[j]; c.Kind == "CheckFailed" && c.Check != nil {
				step, check := c.Step, c.Check.Name
				failures = []gen.ProofFailure{{Step: &step, Check: &check, Reason: c.Check.Reason}}
			}
		}
		if failures == nil {
			reason := "the run was handed over"
			if e.Detail != "" {
				reason += ": " + e.Detail
			}
			failures = []gen.ProofFailure{{Reason: reason}}
		}
		if reason, ok := runEnd(v); ok {
			failures = append(failures, gen.ProofFailure{Reason: reason})
		}
		return gen.ProofOutcomeFailed, failures, nil
	}
	switch {
	case v.Status != machine.StatusEnded:
		return gen.ProofOutcomeRunning, nil, nil
	case v.Outcome != machine.OutcomeDone:
		return gen.ProofOutcomeFailed, []gen.ProofFailure{{Reason: "the run ended " + string(v.Outcome)}}, nil
	}
	filled, err := r.pattern()
	if err != nil {
		return "", nil, err
	}
	got, steps, err := lastOutputs(events, filled, filled)
	if err != nil {
		return gen.ProofOutcomeFailed, []gen.ProofFailure{{Reason: err.Error()}}, nil
	}
	failures := []gen.ProofFailure{}
	for _, o := range slices.Sorted(maps.Keys(r.Template)) {
		if got[o] == r.Template[o] {
			continue
		}
		step := steps[o]
		paths, err := s.differing(r.Template[o], got[o])
		if err != nil || len(paths) == 0 {
			failures = append(failures, gen.ProofFailure{Step: &step, Reason: fmt.Sprintf("the output %s differs from the template", o)})
			continue
		}
		for _, p := range paths {
			path := p
			failures = append(failures, gen.ProofFailure{Step: &step, Path: &path, Reason: "the file differs from the template"})
		}
	}
	if len(failures) > 0 {
		return gen.ProofOutcomeFailed, failures, nil
	}
	return gen.ProofOutcomePassed, failures, nil
}

// differing names the paths whose files differ between two framed file sets, in byte order.
func (s *sliceServer) differing(want, got string) ([]string, error) {
	sets := [2]map[string][]byte{{}, {}}
	for i, h := range []string{want, got} {
		b, err := s.runs.st.Artifact(h)
		if err != nil {
			return nil, err
		}
		files, err := canonical.Unframe(b)
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			sets[i][f.Path] = f.Content
		}
	}
	var paths []string
	for p, c := range sets[0] {
		if g, ok := sets[1][p]; !ok || !bytes.Equal(g, c) {
			paths = append(paths, p)
		}
	}
	for p := range sets[1] {
		if _, ok := sets[0][p]; !ok {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	return paths, nil
}

// ---- busy ----

// capturedBusy refuses CapturedPatternBusy while another of the captured pattern's operations
// has not ended, and, with proofRuns, while a run of its proof slice is starting or has not
// ended. The caller holds s.cmu.
func (s *sliceServer) capturedBusy(r *capturedRecord, proofRuns bool) error {
	if err := s.otherOperation(r, ""); err != nil {
		return err
	}
	if !proofRuns {
		return nil
	}
	return s.proofRunBusy(r.ProofSlice)
}

// otherOperation refuses CapturedPatternBusy while an operation of the captured pattern other
// than except has not ended.
func (s *sliceServer) otherOperation(r *capturedRecord, except string) error {
	open, err := s.ops.records.OpenOperations()
	if err != nil {
		return err
	}
	for _, o := range open {
		if isCapturedKind(o.Kind) && o.Target == r.ID && o.ID != except {
			return &sl.Error{Code: string(gen.ErrorCodeCapturedPatternBusy), Detail: fmt.Sprintf("operation %s of captured pattern %s has not ended", o.ID, r.ID)}
		}
	}
	return nil
}

// proofRunBusy is CapturedPatternBusy while a run of the proof slice is starting (accepted, its
// RunStarted not yet recorded) or has not ended, nil for no proof slice ("").
func (s *sliceServer) proofRunBusy(slice string) error {
	if slice == "" || s.runs == nil {
		return nil
	}
	run, err := s.runs.active(slice)
	switch {
	case err != nil:
		return err
	case run != "":
		return &sl.Error{Code: string(gen.ErrorCodeCapturedPatternBusy),
			Detail: fmt.Sprintf("run %s of its proof slice %s has not ended", run, slice)}
	}
	return nil
}

// ---- parameters ----

// ReplaceParameters refuses at once, before any operation exists: an unknown captured pattern
// (NotFound), an approved one (CapturedPatternApproved), a withdrawn one
// (CapturedPatternWithdrawn), and a busy one (CapturedPatternBusy: another of its operations, or
// a run of its proof slice that is starting or has not ended). The body's schema is the front's.
// Otherwise it accepts the change, whose target is the captured pattern.
func (s *sliceServer) ReplaceParameters(ctx context.Context, req gen.ReplaceParametersRequestObject) (gen.ReplaceParametersResponseObject, error) {
	if s.runs != nil {
		s.runs.mu.Lock() // no proof starts between the busy check and the acceptance
		defer s.runs.mu.Unlock()
	}
	s.cmu.Lock()
	defer s.cmu.Unlock()
	r, err := s.readCaptured(req.CapturedPatternId)
	if err != nil {
		return s.refuse("replaceParameters", err)
	}
	if err := stateRefusal(r); err != nil {
		return s.refuse("replaceParameters", err)
	}
	if err := s.capturedBusy(r, true); err != nil {
		return s.refuse("replaceParameters", err)
	}
	op, err := s.ops.accept(ctx, "replaceParameters", r.ID)
	if err != nil {
		return unavailableFor(err), nil
	}
	return gen.ReplaceParameters202JSONResponse{AcceptedJSONResponse: gen.AcceptedJSONResponse{Body: op}}, nil
}

// stateRefusal refuses a change of the parameters of an approved or withdrawn captured pattern.
func stateRefusal(r *capturedRecord) error {
	switch r.State {
	case gen.CapturedPatternStateApproved:
		return &sl.Error{Code: string(gen.ErrorCodeCapturedPatternApproved), Detail: "captured pattern " + r.ID + " is approved, so it never changes"}
	case gen.CapturedPatternStateWithdrawn:
		return &sl.Error{Code: string(gen.ErrorCodeCapturedPatternWithdrawn), Detail: "captured pattern " + r.ID + " is withdrawn"}
	}
	return nil
}

// replaceParameters is the work of a parameters change: each parameter sent with the name and
// value it had keeps its kind, source and origin; any other gets them from the slice (a value
// among the slice's names or declared values is of that candidate's kind and source; any other is
// a literal, a number when it has a number's form, else a string) and origin `added`; then
// engine/captured judges them and finds their places (ParameterRefused, naming the parameter and
// why). The accepted parameters replace the old ones, and a proven captured pattern is proposed
// again. A repeat gives the same parameters.
func (s *sliceServer) replaceParameters(id string, body []byte) (WorkResult, error) {
	var req gen.ParametersRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return WorkResult{}, err
	}
	r, err := s.readCaptured(id)
	if err != nil {
		return WorkResult{}, s.workError("replaceParameters", err)
	}
	src := r.src()
	tmpl, err := s.templateFiles(r.Template)
	if err != nil {
		return WorkResult{}, err
	}
	cands, err := captured.Candidates(src)
	if err != nil {
		return WorkResult{}, err
	}
	params := make([]captured.Parameter, len(req.Parameters))
	for i, p := range req.Parameters {
		if j := slices.IndexFunc(r.Parameters, func(o captured.Parameter) bool { return o.Name == p.Name && o.Value == p.Value }); j >= 0 {
			old := r.Parameters[j]
			params[i] = captured.Parameter{Name: p.Name, Value: p.Value, Kind: old.Kind, Source: old.Source, Origin: old.Origin}
			continue
		}
		kind, source := kindOf(cands, p.Value)
		params[i] = captured.Parameter{Name: p.Name, Value: p.Value, Kind: kind, Source: source, Origin: captured.OriginAdded}
	}
	located, err := captured.Locate(src, tmpl, params)
	var refused *captured.Refused
	switch {
	case errors.As(err, &refused):
		return WorkResult{}, &WorkError{Code: gen.ErrorCodeParameterRefused, Detail: refused.Error()}
	case err != nil:
		return WorkResult{}, err
	}
	s.cmu.Lock()
	defer s.cmu.Unlock()
	if r, err = s.readCaptured(id); err != nil {
		return WorkResult{}, s.workError("replaceParameters", err)
	}
	if err := stateRefusal(r); err != nil {
		return WorkResult{}, s.workError("replaceParameters", err)
	}
	if located == nil {
		located = []captured.Parameter{}
	}
	r.Parameters = located
	if r.State == gen.CapturedPatternStateProven {
		r.State = gen.CapturedPatternStateProposed
	}
	if err := s.saveCaptured(r); err != nil {
		return WorkResult{}, Retryable(err)
	}
	return WorkResult{CapturedPattern: id}, nil
}

// kindOf gives a value's kind and source: a candidate's when the value is one (a name of the
// slice, or a value its contract declares, proposed or left out by Propose's cut), else a
// literal number when the value has the number form engine/captured judges, else a literal
// string.
func kindOf(cands []captured.Parameter, value string) (captured.Kind, captured.Source) {
	if i := slices.IndexFunc(cands, func(c captured.Parameter) bool { return c.Value == value }); i >= 0 {
		return cands[i].Kind, cands[i].Source
	}
	// Fill judges each value's form against its kind before it fills anything, and fills nothing
	// from no files: it is engine/captured's own form rule.
	asNumber := []captured.Parameter{{Name: "value", Kind: captured.KindNumber, Value: value}}
	if _, err := captured.Fill(nil, asNumber, map[string]string{"value": value}); err == nil {
		return captured.KindNumber, captured.SourceLiteral
	}
	return captured.KindString, captured.SourceLiteral
}

// ---- approval ----

// ApproveCapturedPattern refuses at once, before any operation exists: an unknown captured
// pattern (NotFound), a withdrawn one (CapturedPatternWithdrawn), a busy one
// (CapturedPatternBusy), and, unless it is approved already, one whose latest proof did not pass
// under the engine's method with its current parameters (ProofRequired). Otherwise it accepts
// the approval; approving an approved one changes nothing.
func (s *sliceServer) ApproveCapturedPattern(ctx context.Context, req gen.ApproveCapturedPatternRequestObject) (gen.ApproveCapturedPatternResponseObject, error) {
	s.cmu.Lock()
	defer s.cmu.Unlock()
	r, err := s.readCaptured(req.CapturedPatternId)
	if err != nil {
		return s.refuse("approveCapturedPattern", err)
	}
	if r.State == gen.CapturedPatternStateWithdrawn {
		return s.refuse("approveCapturedPattern", stateRefusal(r))
	}
	if err := s.capturedBusy(r, false); err != nil {
		return s.refuse("approveCapturedPattern", err)
	}
	if err := s.approvable(r); err != nil {
		return s.refuse("approveCapturedPattern", err)
	}
	op, err := s.ops.accept(ctx, "approveCapturedPattern", r.ID)
	if err != nil {
		return unavailableFor(err), nil
	}
	return gen.ApproveCapturedPattern202JSONResponse{AcceptedJSONResponse: gen.AcceptedJSONResponse{Body: op}}, nil
}

// approvable completes r's proofs, keeping any change, and refuses ProofRequired unless r is
// approved, or proven with its latest proof passed under the engine's method with the current
// parameters. The caller holds s.cmu.
func (s *sliceServer) approvable(r *capturedRecord) error {
	if r.State == gen.CapturedPatternStateApproved {
		return nil
	}
	m, err := currentMethod()
	if err != nil {
		return err
	}
	if err := s.completeAndSave(r, m); err != nil {
		return err
	}
	if r.State != gen.CapturedPatternStateProven || !r.provenUnder(m) {
		return &sl.Error{Code: string(gen.ErrorCodeProofRequired),
			Detail: fmt.Sprintf("captured pattern %s is %s: its latest proof has not passed under the engine's method with its current parameters", r.ID, r.State)}
	}
	return nil
}

// approve is the work of an approval: it records `by` client, `at` and the proof it approved, once.
func (s *sliceServer) approve(id string) (WorkResult, error) {
	s.cmu.Lock()
	defer s.cmu.Unlock()
	r, err := s.readCaptured(id)
	if err != nil {
		return WorkResult{}, s.workError("approveCapturedPattern", err)
	}
	if r.State == gen.CapturedPatternStateApproved {
		return WorkResult{CapturedPattern: id}, nil
	}
	if r.State == gen.CapturedPatternStateWithdrawn {
		return WorkResult{}, s.workError("approveCapturedPattern", stateRefusal(r))
	}
	if err := s.approvable(r); err != nil {
		return WorkResult{}, s.workError("approveCapturedPattern", err)
	}
	r.Approval = &gen.CapturedPatternApproval{By: gen.CapturedPatternApprovalByClient, At: s.ops.now().UTC(), Proof: r.latest().Run}
	r.State = gen.CapturedPatternStateApproved
	if err := s.saveCaptured(r); err != nil {
		return WorkResult{}, Retryable(err)
	}
	return WorkResult{CapturedPattern: id}, nil
}

// ---- operations ----

// capturedWork does a captured-pattern operation's work from its record and body.
func (s *sliceServer) capturedWork(ctx context.Context, rec store.OperationRecord, body []byte) (WorkResult, error) {
	if _, err := s.capturedStore(); err != nil {
		return WorkResult{}, err
	}
	switch rec.Kind {
	case "captureRun":
		return s.capture(rec, body)
	case "replaceParameters":
		return s.replaceParameters(rec.Target, body)
	case "approveCapturedPattern":
		return s.approve(rec.Target)
	case "makeProofSlice":
		return s.makeProofSlice(ctx, rec.Target)
	case "withdrawCapturedPattern":
		return s.withdraw(rec.Target)
	case "eraseCapturedPattern":
		return s.erase(ctx, rec)
	}
	return WorkResult{}, fmt.Errorf("operation %s: no work for the kind %s yet", rec.ID, rec.Kind)
}

// workerSteps are the steps of the pattern ref names that need a step worker: every step of the
// base pattern, and of a captured pattern every step but its `fill` steps.
func (s *sliceServer) workerSteps(ref string) ([]string, error) {
	if s.runs == nil || s.captured == nil || ref == s.runs.pattern.ID+"@"+s.runs.pattern.Version {
		return s.svc.Steps(), nil
	}
	id, version, _ := strings.Cut(ref, "@")
	if version != capturedVersion || !fits(s.contract.patternID, id) {
		return s.svc.Steps(), nil
	}
	r, err := decodeCaptured(s.captured, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return s.svc.Steps(), nil
	case err != nil:
		return nil, err
	}
	p, err := r.pattern()
	if err != nil {
		return nil, err
	}
	var steps []string
	for _, st := range p.Steps {
		if st.Worker != fillWorker {
			steps = append(steps, st.Name)
		}
	}
	return steps, nil
}

// ---- an engine that keeps no slices ----

func (s *server) CaptureRun(context.Context, gen.CaptureRunRequestObject) (gen.CaptureRunResponseObject, error) {
	return nil, errNotImplemented
}

func (s *server) ListCapturedPatterns(context.Context, gen.ListCapturedPatternsRequestObject) (gen.ListCapturedPatternsResponseObject, error) {
	return nil, errNotImplemented
}

func (s *server) GetCapturedPattern(context.Context, gen.GetCapturedPatternRequestObject) (gen.GetCapturedPatternResponseObject, error) {
	return nil, errNotImplemented
}

func (s *server) ReplaceParameters(context.Context, gen.ReplaceParametersRequestObject) (gen.ReplaceParametersResponseObject, error) {
	return nil, errNotImplemented
}

func (s *server) MakeProofSlice(context.Context, gen.MakeProofSliceRequestObject) (gen.MakeProofSliceResponseObject, error) {
	return nil, errNotImplemented
}

func (s *server) ApproveCapturedPattern(context.Context, gen.ApproveCapturedPatternRequestObject) (gen.ApproveCapturedPatternResponseObject, error) {
	return nil, errNotImplemented
}

func (s *server) WithdrawCapturedPattern(context.Context, gen.WithdrawCapturedPatternRequestObject) (gen.WithdrawCapturedPatternResponseObject, error) {
	return nil, errNotImplemented
}

func (s *server) EraseCapturedPattern(context.Context, gen.EraseCapturedPatternRequestObject) (gen.EraseCapturedPatternResponseObject, error) {
	return nil, errNotImplemented
}

func (p problemResponse) VisitCaptureRunResponse(w http.ResponseWriter) error { return p.write(w) }
func (p problemResponse) VisitListCapturedPatternsResponse(w http.ResponseWriter) error {
	return p.write(w)
}
func (p problemResponse) VisitGetCapturedPatternResponse(w http.ResponseWriter) error {
	return p.write(w)
}
func (p problemResponse) VisitReplaceParametersResponse(w http.ResponseWriter) error {
	return p.write(w)
}
func (p problemResponse) VisitApproveCapturedPatternResponse(w http.ResponseWriter) error {
	return p.write(w)
}

func (u unavailableAnswer) VisitCaptureRunResponse(w http.ResponseWriter) error { return u.write(w) }
func (u unavailableAnswer) VisitReplaceParametersResponse(w http.ResponseWriter) error {
	return u.write(w)
}
func (u unavailableAnswer) VisitApproveCapturedPatternResponse(w http.ResponseWriter) error {
	return u.write(w)
}
