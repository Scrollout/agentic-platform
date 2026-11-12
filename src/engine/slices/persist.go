package slices

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Records keeps the service's slices durably, such as the run store in the engine's volume. The
// service owns the records' format (readable JSON); Records only keeps bytes. Every method returns
// only after the change is durable, and the service reports a change only after its record is
// written.
type Records interface {
	// SliceRecords returns every stored slice record by slice ID.
	SliceRecords() (map[string][]byte, error)
	// PutSliceRecord replaces the slice's record atomically.
	PutSliceRecord(id string, record []byte) error
	// DeleteSliceRecord removes the slice's record; a missing one is no error.
	DeleteSliceRecord(id string) error
	// SliceSequence returns the stored ID sequence, nil when none was stored yet.
	SliceSequence() ([]byte, error)
	// PutSliceSequence replaces the stored ID sequence atomically.
	PutSliceSequence(seq []byte) error
}

// recordFormat is the version of the record layout below; a record of another version is refused.
const recordFormat = 1

// The record of one slice on disk: the slice as the service holds it, and what it keeps beside it
// (the compatibility baseline, the latest compatibility and lint results, and the example IDs
// dropped earlier), so that a restart
// neither loses the baseline nor calls the lint service or the analyst again.
type (
	diskRecord struct {
		Format   int         `json:"format"`
		Slice    diskSlice   `json:"slice"`
		Baseline string      `json:"baseline"`
		Compat   []diskCheck `json:"compat"`
		Lint     []diskCheck `json:"lint"`
		// Dropped maps the example IDs earlier revisions dropped to a digest of their scenario. A
		// record written before it was kept has none, so the IDs its examples carry now are the
		// only ones known used.
		Dropped map[string]string `json:"dropped_examples,omitempty"`
		// ExamplesBase is the slice's last examples that parsed; a record without it takes the
		// examples it holds.
		ExamplesBase *string `json:"examples_base,omitempty"`
	}
	diskSlice struct {
		ID          string         `json:"id"`
		Description string         `json:"description"`
		State       string         `json:"state"`
		Goals       []Goal         `json:"goals"`
		Terms       []Term         `json:"terms"`
		Rules       []Rule         `json:"rules"`
		Examples    string         `json:"examples"`
		Contract    string         `json:"contract"`
		Questions   []diskQuestion `json:"questions"`
		Parts       diskParts      `json:"parts"`
		Approvals   []diskApproval `json:"approvals"`
		Checks      []diskCheck    `json:"checks"`
		History     []diskHistory  `json:"history"`
	}
	diskQuestion struct {
		ID         string      `json:"id"`
		About      string      `json:"about"`
		Text       string      `json:"text"`
		Options    []string    `json:"options"`
		Answer     *diskAnswer `json:"answer"`
		AnsweredAt time.Time   `json:"answered_at"`
	}
	diskAnswer struct {
		Question string `json:"question"`
		Option   *int   `json:"option"`
		Text     string `json:"text"`
	}
	diskParts struct {
		Goals    string `json:"goals"`
		Rules    string `json:"rules"`
		Contract string `json:"contract"`
	}
	diskApproval struct {
		Part        Part      `json:"part"`
		ContentHash string    `json:"content_hash"`
		At          time.Time `json:"at"`
	}
	diskCheck struct {
		Part        string `json:"part"`
		Check       string `json:"check"`
		Passed      bool   `json:"passed"`
		Reason      string `json:"reason"`
		FromPattern bool   `json:"from_pattern"`
	}
	diskHistory struct {
		Kind        string    `json:"kind"`
		At          time.Time `json:"at"`
		Model       string    `json:"model"`
		AgentDigest string    `json:"agent_digest"`
		Refs        []string  `json:"refs"`
		Attempts    int       `json:"attempts,omitempty"` // absent in records written before it was kept
	}
	diskSequence struct {
		Last int `json:"last"` // the number of the last slice ID given out
	}
)

// exampleID is the form of an example ID (data-model.md), scenarioDigest that of a scenario's digest (slice.ExampleScenarios).
var (
	exampleID      = regexp.MustCompile(`^E-[0-9]+$`)
	scenarioDigest = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

func toDiskChecks(cs []Check) []diskCheck {
	if cs == nil {
		return nil
	}
	out := make([]diskCheck, len(cs))
	for i, c := range cs {
		out[i] = diskCheck{Part: c.Part, Check: c.Check, Passed: c.Passed, Reason: c.Reason, FromPattern: c.Pattern}
	}
	return out
}

func fromDiskChecks(cs []diskCheck) []Check {
	if cs == nil {
		return nil
	}
	out := make([]Check, len(cs))
	for i, c := range cs {
		out[i] = Check{Part: c.Part, Check: c.Check, Passed: c.Passed, Reason: c.Reason, Pattern: c.FromPattern}
	}
	return out
}

func encodeRecord(r *record) ([]byte, error) {
	sl := clone(r.slice)
	d := diskRecord{Format: recordFormat, Baseline: r.baseline, Compat: toDiskChecks(r.compat), Lint: toDiskChecks(r.lint),
		Dropped: maps.Clone(r.dropped), ExamplesBase: &r.examplesBase,
		Slice: diskSlice{ID: sl.ID, Description: sl.Description, State: sl.State, Goals: sl.Goals, Terms: sl.Terms,
			Rules: sl.Rules, Examples: sl.Examples, Contract: sl.Contract, Checks: toDiskChecks(sl.Checks),
			Parts: diskParts{Goals: sl.Parts.Goals, Rules: sl.Parts.Rules, Contract: sl.Parts.Contract}}}
	if sl.Questions != nil {
		d.Slice.Questions = make([]diskQuestion, len(sl.Questions))
	}
	for i, q := range sl.Questions {
		d.Slice.Questions[i] = diskQuestion{ID: q.ID, About: q.About, Text: q.Text, Options: q.Options, AnsweredAt: q.AnsweredAt}
		if a := q.Answer; a != nil {
			d.Slice.Questions[i].Answer = &diskAnswer{Question: a.Question, Option: a.Option, Text: a.Text}
		}
	}
	if sl.Approvals != nil {
		d.Slice.Approvals = make([]diskApproval, len(sl.Approvals))
	}
	for i, a := range sl.Approvals {
		d.Slice.Approvals[i] = diskApproval{Part: a.Part, ContentHash: a.ContentHash, At: a.At}
	}
	if sl.History != nil {
		d.Slice.History = make([]diskHistory, len(sl.History))
	}
	for i, h := range sl.History {
		d.Slice.History[i] = diskHistory{Kind: h.Kind, At: h.At, Model: h.Model, AgentDigest: h.AgentDigest, Refs: h.Refs, Attempts: h.Attempts}
	}
	return json.MarshalIndent(d, "", "  ")
}

// decodeRecord reads the record stored under id and checks it before the service serves it: the
// layout version, the slice ID against the contract's rule, the state, each approval's part and
// hash, and that the stored part hashes are those of the stored parts, so that a damaged record
// is refused rather than served.
func (s *Service) decodeRecord(id string, b []byte) (*record, error) {
	var d diskRecord
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&d); err != nil {
		return nil, fmt.Errorf("unreadable: %w", err)
	}
	if dec.More() {
		return nil, fmt.Errorf("unreadable: data after the record")
	}
	if d.Format != recordFormat {
		return nil, fmt.Errorf("format %d, want %d", d.Format, recordFormat)
	}
	if _, err := s.schemas.ParseID(d.Slice.ID); err != nil || d.Slice.ID != id {
		return nil, fmt.Errorf("holds slice ID %q", d.Slice.ID)
	}
	if !slices.Contains([]string{"proposed", "questions-open", "checked", "approved"}, d.Slice.State) {
		return nil, fmt.Errorf("unknown state %q", d.Slice.State)
	}
	sl := Slice{ID: d.Slice.ID, Description: d.Slice.Description, State: d.Slice.State, Goals: d.Slice.Goals,
		Terms: d.Slice.Terms, Rules: d.Slice.Rules, Examples: d.Slice.Examples, Contract: d.Slice.Contract,
		Checks: fromDiskChecks(d.Slice.Checks)}
	if d.Slice.Questions != nil {
		sl.Questions = make([]Question, len(d.Slice.Questions))
	}
	for i, q := range d.Slice.Questions {
		sl.Questions[i] = Question{ID: q.ID, About: q.About, Text: q.Text, Options: q.Options, AnsweredAt: q.AnsweredAt}
		if a := q.Answer; a != nil {
			sl.Questions[i].Answer = &Answer{Question: a.Question, Option: a.Option, Text: a.Text}
		}
	}
	if d.Slice.Approvals != nil {
		sl.Approvals = make([]Approval, len(d.Slice.Approvals))
	}
	for i, a := range d.Slice.Approvals {
		if !slices.Contains(allParts, a.Part) {
			return nil, fmt.Errorf("approval of unknown part %q", a.Part)
		}
		if _, err := s.schemas.ParseHash(a.ContentHash); err != nil {
			return nil, fmt.Errorf("approval of %s: %w", a.Part, err)
		}
		sl.Approvals[i] = Approval{Part: a.Part, ContentHash: a.ContentHash, At: a.At}
	}
	if d.Slice.History != nil {
		sl.History = make([]HistoryEntry, len(d.Slice.History))
	}
	for i, h := range d.Slice.History {
		if h.Attempts < 0 {
			return nil, fmt.Errorf("history entry %d has %d attempts", i, h.Attempts)
		}
		sl.History[i] = HistoryEntry{Kind: h.Kind, At: h.At, Model: h.Model, AgentDigest: h.AgentDigest, Refs: h.Refs, Attempts: h.Attempts}
	}
	want := Parts{Goals: d.Slice.Parts.Goals, Rules: d.Slice.Parts.Rules, Contract: d.Slice.Parts.Contract}
	if err := setParts(&sl); err != nil {
		return nil, fmt.Errorf("parts: %w", err)
	}
	if sl.Parts != want {
		return nil, fmt.Errorf("the stored part hashes are not those of the stored parts")
	}
	base := sl.Examples
	if d.ExamplesBase != nil {
		base = *d.ExamplesBase
	}
	for e, digest := range d.Dropped {
		if !exampleID.MatchString(e) || !scenarioDigest.MatchString(digest) {
			return nil, fmt.Errorf("dropped example ID %q", e)
		}
	}
	return &record{slice: sl, baseline: d.Baseline, compat: fromDiskChecks(d.Compat), lint: fromDiskChecks(d.Lint),
		dropped: d.Dropped, examplesBase: base}, nil
}

// load reads every stored slice and the ID sequence. Any record that cannot be read or checked
// stops the service (fail closed), naming the slice.
func (s *Service) load() error {
	recs, err := s.records.SliceRecords()
	if err != nil {
		return fmt.Errorf("slices: reading the slice records: %w", err)
	}
	for id, b := range recs {
		r, err := s.decodeRecord(id, b)
		if err != nil {
			return fmt.Errorf("slices: the record of slice %s: %w", id, err)
		}
		s.slices[id] = r
		if n, err := strconv.Atoi(strings.TrimPrefix(id, "S-")); err == nil && n > s.last {
			s.last = n
		}
	}
	seq, err := s.records.SliceSequence()
	if err != nil {
		return fmt.Errorf("slices: reading the slice ID sequence: %w", err)
	}
	if seq != nil {
		var d diskSequence
		dec := json.NewDecoder(bytes.NewReader(seq))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&d); err != nil || d.Last < 0 {
			return fmt.Errorf("slices: the slice ID sequence is unreadable: %v", err)
		}
		s.last = max(s.last, d.Last)
	}
	return nil
}

// save writes r's record. The caller holds s.mu and reports the change only when save succeeds.
func (s *Service) save(r *record) error {
	if s.records == nil {
		return nil
	}
	b, err := encodeRecord(r)
	if err != nil {
		return err
	}
	if err := s.records.PutSliceRecord(r.slice.ID, b); err != nil {
		return fmt.Errorf("slices: storing slice %s: %w", r.slice.ID, err)
	}
	return nil
}

// nextID gives out the next slice ID and stores the sequence before the ID is reported. The
// caller holds s.mu. The number is used up even when storing fails, so it is never given twice
// by this service; the sequence stays pending until a later store succeeds. A failed store
// gives OperationFailed.
func (s *Service) nextID() (string, error) {
	s.last++
	s.seqPending = true
	if err := s.storeSequence(); err != nil {
		return "", err
	}
	return "S-" + strconv.Itoa(s.last), nil
}

// storeSequence stores the ID sequence if a change to it is not stored yet. The caller holds s.mu.
func (s *Service) storeSequence() error {
	if !s.seqPending || s.records == nil {
		s.seqPending = false
		return nil
	}
	b, _ := json.Marshal(diskSequence{Last: s.last}) // a struct of an int cannot fail
	if err := s.records.PutSliceSequence(b); err != nil {
		// The cause stays out of the detail, which reaches the client.
		return &Error{Code: "OperationFailed", Detail: "the slice ID sequence could not be stored"}
	}
	s.seqPending = false
	return nil
}
