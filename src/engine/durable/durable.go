// Package durable runs a run to its end across platform stops: the run is a Temporal workflow,
// Run, whose code is the step machine's rules (src/engine/machine), and every call it makes is an
// activity that reports how long it ran (research R3; data model, "The engine's durable
// interface"). A workflow's inputs, results and activity payloads hold IDs, hashes, numbers,
// check names and short reasons only, never a prompt, completion, file content or finding text
// (FR-016): a finding travels by the hash of its JSON in the artifact store, and the activities
// read and write the content there.
package durable

import (
	"time"

	"github.com/agentic-platform/engine/machine"
)

// Where the engine's workflows run (data model, "Execution state in Temporal"; ADR 0070).
const (
	Namespace       = "ap"
	RunsQueue       = "runs"       // the Run workflow and its activities
	OperationsQueue = "operations" // the Operation workflow (user story 2)
)

// The activities' names, as registered.
const (
	CallWorker     = "CallWorker"
	RunCheck       = "RunCheck"
	RestoreProject = "RestoreProject"
	CommitStep     = "CommitStep"
	LookupReuse    = "LookupReuse"
	PutResult      = "PutResult"
	RecordEvent    = "RecordEvent"
)

// The activities' application error types.
const (
	// WorkersNotReady: CallWorker found the slice's step workers not running (machine.ErrWorkersNotReady).
	WorkersNotReady = "WorkersNotReady"
	// ModelUnavailable: CallWorker's model could not be reached (machine.ErrModelUnavailable).
	ModelUnavailable = "ModelUnavailable"
	// SeqTaken: RecordEvent found the event's seq held with other content (store.ErrSeqTaken).
	SeqTaken = "SeqTaken"
)

// The Run workflow's signals; none carries a payload.
const (
	SignalWorkersReady   = "workers-ready"
	SignalResume         = "resume"
	SignalAcceptHandOver = "accept-hand-over"
	SignalCancel         = "cancel"
)

// RunInput starts a run.
type RunInput struct {
	Run, Slice, Project string
	Pattern             machine.Pattern
	Model               string            // the model the pattern's agents use (part of a reuse key)
	Inputs              map[string]string // the slice's part hashes by input name
	Refs                []string          // the slice's refs, on every event without refs of its own
	ReplayOf            string            // "" for a run that is not a replay
}

// RunResult is an ended run's outcome.
type RunResult struct{ Outcome machine.Outcome }

// Finding is a machine.Finding without its text, which stays in the artifact store under SHA256,
// the hash of the finding's JSON. Blocking is as the run holds it: a finding the agent claimed
// blocking that no witness confirmed is not.
type Finding struct {
	SHA256   string           `json:"sha256"`
	Rule     string           `json:"rule"`
	File     string           `json:"file,omitempty"`
	Line     int              `json:"line,omitempty"`
	Blocking bool             `json:"blocking"`
	Check    string           `json:"check,omitempty"`
	Witness  *machine.Witness `json:"witness,omitempty"`
}

// WorkerCall is one attempt of a step (machine.Request), with findings by hash.
type WorkerCall struct {
	Run, Slice, Project string
	Step                machine.Step
	Attempt, Round      int
	Inputs              []string       // hashes of the step's declared inputs, in order
	Open                []Finding      // the open blocking findings (a fix, a later round)
	ChangedLines        []machine.Line // a later round: the lines the fix changed
	Before              []machine.File
	Artifacts           map[string]string
	TimeLimit           time.Duration // the call's time limit
}

// WorkerResult is machine.Result without content: outputs by hash, findings by hash.
type WorkerResult struct {
	Model                string
	Outputs              map[string]string
	Usage                machine.Usage
	Findings             []Finding
	ChangedLines         []machine.Line
	Before, ProjectFiles []machine.File
	Took                 time.Duration // how long the call ran
}

// CheckCall is one check of one attempt (machine.CheckRequest), with the attempt's result by hash.
type CheckCall struct {
	Run, Slice, Project  string
	Step                 machine.Step
	Attempt, Round       int
	Name                 string
	Inputs               []string
	Model                string            // the model the attempt's worker named
	Outputs              map[string]string // the attempt's outputs by name
	Findings             []Finding         // the attempt's findings, in the worker's order
	Changed              []machine.Line    // the lines the attempt reports changing (a fix)
	Before, ProjectFiles []machine.File
	Open                 []Finding      // the attempt's scope: the open findings it was given
	ChangedLines         []machine.Line // and the lines the fix before it changed
	TimeLimit            time.Duration
}

// CheckResult is machine.CheckResult and the check's running time.
type CheckResult struct {
	Passed    bool
	Reason    string
	Refs      []string
	Confirmed []int
	Took      time.Duration
}

// Ran is RestoreProject's and CommitStep's result.
type Ran struct{ Took time.Duration }

// ReuseLookup asks for the result accepted, or the review round left open, under a step's reuse
// key: Key, or, when Key is empty, the key LookupReuse computes from Parts, the text of the Open
// findings (read from the artifact store) and Changed, as feature 001 does (machine.KeyOf).
type ReuseLookup struct {
	Key       string
	Slice     string // the run's slice, which stores the result's findings by hash
	Parts     machine.KeyParts
	Open      []Finding
	Changed   []machine.Line
	TimeLimit time.Duration
}

// Reused is the result kept under a key (machine.Accepted), findings by hash; Found is false
// when none is.
type Reused struct {
	Key       string   // the key looked up
	Inputs    []string // the inputs it covers (the step's, then the findings' and lines' hash)
	Found     bool
	Artifacts []string
	Run       string // the run that accepted it, or left the round open
	Lines     []machine.Line
	Files     []machine.File
	Open      bool
	Findings  []Finding
	Check     *machine.EventCheck
}

// ReusePut keeps a result under its reuse key; the first result under a key stays.
type ReusePut struct {
	Key       string
	Artifacts []string
	Run       string
	Lines     []machine.Line
	Files     []machine.File
	Open      bool
	Findings  []Finding
	Check     *machine.EventCheck
	TimeLimit time.Duration
}

// EventRecord is one event of a run's record. Event.Finding is nil: a FindingRecorded's finding is
// Finding. Open, on the event that hands a run over with open findings and on the event that
// later ends it, are those findings: RecordEvent reads their text from the artifact store and
// appends the list to Event.Detail (feature 001's detail).
type EventRecord struct {
	Slice     string
	Event     machine.Event
	Finding   *Finding
	Open      []Finding
	Project   string         // Event.Project, which machine.Event does not marshal
	Files     []machine.File // Event.Files, likewise
	TimeLimit time.Duration
}
