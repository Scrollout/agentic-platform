package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"

	"github.com/agentic-platform/engine/api/gen"
	"github.com/agentic-platform/engine/machine"
)

// Runs is the API's port to the durable service, which runs each run as a workflow to its end
// across platform stops (data model, "The engine's durable interface"): it starts a run, tells it
// something, stops it, and says whether it exists. Package durable implements it on Temporal.
type Runs interface {
	// Start starts one run's workflow. Run is "RUN-<n>", TaskQueue "runs", and with
	// RejectDuplicate a run is never started twice.
	Start(ctx context.Context, r RunStart) error
	// Signal sends the named signal to the run: "workers-ready", "resume", "accept-hand-over" or
	// "cancel". None carries a payload.
	Signal(ctx context.Context, run, name string) error
	// Cancel cancels the run's workflow (the run's operation was cancelled); the run then
	// records RunCancelled.
	Cancel(ctx context.Context, run string) error
	// Exists says whether the run has a workflow.
	Exists(ctx context.Context, run string) (bool, error)
	// Closed says whether the run's workflow has closed and, when it did not complete, why (its
	// failure's message); a run with no workflow is ErrNotFound (T058).
	Closed(ctx context.Context, run string) (closed bool, reason string, err error)
	// Delete removes the run's workflow (erasing a slice).
	Delete(ctx context.Context, run string) error
}

// RunStart starts a run: its ID, slice and project; the pattern as "<id>@<version>" and the
// pattern itself (Definition) with the model its agent steps use (part of their reuse keys, ""
// for none); the slice's part hashes by input name and its refs; the operation that follows it
// and the run it replays, both recorded on its RunStarted.
type RunStart struct {
	Run, Slice, Project, ReplayOf string
	Operation                     string
	Pattern                       string // "<id>@<version>"
	Definition                    machine.Pattern
	Model                         string
	Inputs                        map[string]string // slice parts by hash
	Refs                          []string
	TaskQueue                     string
	RejectDuplicate               bool
}

// The signals a run's workflow takes (data model, "Execution state in Temporal").
const (
	SignalWorkersReady   = "workers-ready"
	SignalResume         = "resume"
	SignalAcceptHandOver = "accept-hand-over"
	SignalCancel         = "cancel"
)

// RunsQueue is the task queue the run workflows run on.
const RunsQueue = "runs"

var (
	// ErrUnavailable: the durable service cannot be reached; nothing was changed. Every Runs
	// method may return it (wrapped).
	ErrUnavailable = errors.New("the durable service cannot be reached")
	// ErrNotFound: the run, or the operation, has no workflow. Signal, Cancel and Delete may
	// return it (wrapped), and the Operations port's Open and Cancel.
	ErrNotFound = errors.New("no workflow")
)

// retryAfter is the Retry-After of a 503 answer, in whole seconds: the retry rule's first wait,
// at least 1.
func retryAfter() int {
	return max(1, int(math.Ceil(RetryRule().First.Seconds())))
}

// unavailable is the 503 answer of a state change the durable service could not take: nothing
// was accepted, and the same request may be sent again after Retry-After.
type unavailableAnswer struct{ detail string }

func unavailableFor(err error) unavailableAnswer {
	return unavailableAnswer{detail: fmt.Sprintf("nothing was accepted: %v; send the same request again", err)}
}

func (u unavailableAnswer) write(w http.ResponseWriter) error {
	status := http.StatusServiceUnavailable
	p := gen.UnavailableProblem{Type: "about:blank", Title: http.StatusText(status), Status: &status,
		Code: gen.UnavailableProblemCodeUnavailable, Detail: &u.detail}
	return writeJSONProblem(w, status, strconv.Itoa(retryAfter()), p)
}

func (u unavailableAnswer) VisitCreateSliceResponse(w http.ResponseWriter) error { return u.write(w) }
func (u unavailableAnswer) VisitDeleteSliceResponse(w http.ResponseWriter) error { return u.write(w) }
func (u unavailableAnswer) VisitReplaceGoalsResponse(w http.ResponseWriter) error {
	return u.write(w)
}
func (u unavailableAnswer) VisitReplaceRulesResponse(w http.ResponseWriter) error {
	return u.write(w)
}
func (u unavailableAnswer) VisitReplaceContractResponse(w http.ResponseWriter) error {
	return u.write(w)
}
func (u unavailableAnswer) VisitAnswerQuestionsResponse(w http.ResponseWriter) error {
	return u.write(w)
}
func (u unavailableAnswer) VisitApprovePartResponse(w http.ResponseWriter) error { return u.write(w) }
func (u unavailableAnswer) VisitStartRunResponse(w http.ResponseWriter) error    { return u.write(w) }
func (u unavailableAnswer) VisitResumeRunResponse(w http.ResponseWriter) error   { return u.write(w) }
func (u unavailableAnswer) VisitReplayRunResponse(w http.ResponseWriter) error   { return u.write(w) }
func (u unavailableAnswer) VisitAcceptHandOverResponse(w http.ResponseWriter) error {
	return u.write(w)
}
func (u unavailableAnswer) VisitRegisterWorkerResponse(w http.ResponseWriter) error {
	return u.write(w)
}
func (u unavailableAnswer) VisitCancelOperationResponse(w http.ResponseWriter) error {
	return u.write(w)
}

// writeJSONProblem writes a problem body with its status and, when not empty, Retry-After.
func writeJSONProblem(w http.ResponseWriter, status int, retry string, body any) error {
	w.Header().Set("Content-Type", "application/problem+json")
	if retry != "" {
		w.Header().Set("Retry-After", retry)
	}
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(body)
}
