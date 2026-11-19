package durable

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/agentic-platform/engine/machine"
	"github.com/agentic-platform/engine/store"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
)

// heartbeatEvery is how often a running activity tells Temporal it is alive; Run gives each
// activity a heartbeat timeout of three times this, so a stopped engine is found within 30 s
// (research R3).
const heartbeatEvery = 10 * time.Second

// Artifacts is the artifact store the activities keep content in; *store.Store has it.
type Artifacts interface {
	// PutArtifact stores data for the slice and returns the lowercase hex SHA-256 of data.
	PutArtifact(slice string, data []byte) (string, error)
	// Artifact returns the bytes stored under a hash.
	Artifact(hash string) ([]byte, error)
}

// Events writes a run's record. Record writes e, whose Seq the workflow set, under the slice,
// with, on RunStarted, the run's operation and the run it replays: a seq already held with the
// same event succeeds without writing, one held with other content is an error wrapping
// store.ErrSeqTaken. api.RunRecord has it.
type Events interface {
	Record(slice string, e machine.Event, operation, replayOf string) error
}

// Activities are the calls the Run workflow makes, on the engine's ports: the step workers, the
// check service, the project worker's version history, the accepted-result store, the artifact
// store and the run record. Each call is given its time limit, heartbeats while it runs and
// reports how long it ran.
type Activities struct {
	Worker    machine.Worker
	Checker   machine.Checker
	History   machine.History
	Results   *machine.Results // nil: no result is reused or kept
	Artifacts Artifacts
	Events    Events
}

// timed runs call within limit (none when 0), heartbeating every heartbeatEvery, and returns how
// long it ran.
func timed[T any](ctx context.Context, limit time.Duration, call func(ctx context.Context) (T, error)) (T, time.Duration, error) {
	if limit > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, limit)
		defer cancel()
	}
	done := make(chan struct{})
	defer close(done)
	if activity.IsActivity(ctx) {
		go func() {
			t := time.NewTicker(heartbeatEvery)
			defer t.Stop()
			for {
				select {
				case <-done:
					return
				case <-t.C:
					activity.RecordHeartbeat(ctx)
				}
			}
		}()
	}
	began := time.Now()
	v, err := call(ctx)
	return v, time.Since(began), err
}

// failed is an activity's error for a call that failed: never retried by Temporal (Run retries
// by the declared rule, research R5).
func failed(kind string, err error) error {
	return temporal.NewNonRetryableApplicationError(err.Error(), kind, nil)
}

// CallWorker runs one attempt of a step on the step worker. Its error types are WorkersNotReady
// and ModelUnavailable; any other error is the attempt failing.
func (a *Activities) CallWorker(ctx context.Context, c WorkerCall) (WorkerResult, error) {
	open, err := a.findings(c.Open)
	if err != nil {
		return WorkerResult{}, failed("WorkerFailed", err)
	}
	req := machine.Request{Run: c.Run, Step: c.Step, Attempt: c.Attempt, Round: c.Round, Open: open,
		ChangedLines: c.ChangedLines, Inputs: c.Inputs, Slice: c.Slice, Project: c.Project, Before: c.Before,
		Artifacts: c.Artifacts}
	res, took, err := timed(ctx, c.TimeLimit, func(ctx context.Context) (machine.Result, error) { return a.Worker.Run(ctx, req) })
	switch {
	case errors.Is(err, machine.ErrWorkersNotReady):
		return WorkerResult{}, failed(WorkersNotReady, err)
	case errors.Is(err, machine.ErrModelUnavailable):
		return WorkerResult{}, failed(ModelUnavailable, err)
	case err != nil:
		return WorkerResult{}, failed("WorkerFailed", err)
	}
	fs, err := a.store(c.Slice, res.Findings)
	if err != nil {
		return WorkerResult{}, failed("WorkerFailed", err)
	}
	return WorkerResult{Model: res.Model, Outputs: res.Outputs, Usage: res.Usage, Findings: fs, ChangedLines: res.ChangedLines,
		Before: res.Before, ProjectFiles: res.ProjectFiles, Took: took}, nil
}

// RunCheck runs one check of an attempt on the check service, with the attempt's outputs and
// findings read from the artifact store. An error is a check that could not run.
func (a *Activities) RunCheck(ctx context.Context, c CheckCall) (CheckResult, error) {
	fs, err := a.findings(c.Findings)
	if err != nil {
		return CheckResult{}, failed("CheckFailed", err)
	}
	open, err := a.findings(c.Open)
	if err != nil {
		return CheckResult{}, failed("CheckFailed", err)
	}
	content := map[string][]byte{}
	for name, h := range c.Outputs {
		b, err := a.Artifacts.Artifact(h)
		if err != nil {
			return CheckResult{}, failed("CheckFailed", fmt.Errorf("output %s: %w", name, err))
		}
		content[name] = b
	}
	req := machine.CheckRequest{Run: c.Run, Step: c.Step, Attempt: c.Attempt, Name: c.Name, Round: c.Round, Inputs: c.Inputs,
		Slice: c.Slice, Project: c.Project, Open: open, ChangedLines: c.ChangedLines,
		Result: machine.Result{Model: c.Model, Outputs: c.Outputs, Findings: fs, ChangedLines: c.Changed, Before: c.Before,
			ProjectFiles: c.ProjectFiles, Content: content}}
	r, took, err := timed(ctx, c.TimeLimit, func(ctx context.Context) (machine.CheckResult, error) { return a.Checker.Check(ctx, req) })
	if err != nil {
		return CheckResult{}, failed("CheckFailed", err)
	}
	return CheckResult{Passed: r.Passed, Reason: r.Reason, Refs: r.Refs, Confirmed: r.Confirmed, Took: took}, nil
}

// RestoreProject makes the project folder hold the files the last accepted step left (the
// project worker's restoreStep).
func (a *Activities) RestoreProject(ctx context.Context, r machine.RestoreRequest) (Ran, error) {
	_, took, err := timed(ctx, r.TimeLimit, func(ctx context.Context) (struct{}, error) { return struct{}{}, a.History.Restore(ctx, r) })
	if err != nil {
		return Ran{}, failed("RestoreFailed", err)
	}
	return Ran{Took: took}, nil
}

// CommitStep records an accepted step, or a review round left open, as the project's next commit
// (the project worker's commitStep); a repeat of a commit already made creates nothing.
func (a *Activities) CommitStep(ctx context.Context, r machine.CommitRequest) (Ran, error) {
	_, took, err := timed(ctx, r.TimeLimit, func(ctx context.Context) (bool, error) { return a.History.Commit(ctx, r) })
	if err != nil {
		return Ran{}, failed("CommitFailed", err)
	}
	return Ran{Took: took}, nil
}

// LookupReuse finds the result kept under a reuse key, its findings stored by hash.
func (a *Activities) LookupReuse(_ context.Context, l ReuseLookup) (Reused, error) {
	inputs, key := l.Parts.Inputs, l.Key
	if key == "" {
		open, err := a.findings(l.Open)
		if err != nil {
			return Reused{}, failed("ReuseFailed", err)
		}
		inputs, key = machine.KeyOf(l.Parts, open, l.Changed)
	} else if len(l.Changed) > 0 {
		inputs, _ = machine.KeyOf(l.Parts, nil, l.Changed)
	}
	if a.Results == nil {
		return Reused{Key: key, Inputs: inputs}, nil
	}
	k, ok := a.Results.Get(key)
	if !ok {
		return Reused{Key: key, Inputs: inputs}, nil
	}
	fs, err := a.store(l.Slice, k.Findings)
	if err != nil {
		return Reused{}, failed("ReuseFailed", err)
	}
	return Reused{Key: key, Inputs: inputs, Found: true, Artifacts: k.Artifacts, Run: k.Run, Lines: k.Lines, Files: k.Files, Open: k.Open,
		Findings: fs, Check: k.Check}, nil
}

// PutResult keeps a result under its reuse key, its findings read from the artifact store.
func (a *Activities) PutResult(_ context.Context, p ReusePut) error {
	if a.Results == nil {
		return nil
	}
	fs, err := a.findings(p.Findings)
	if err != nil {
		return failed("ReuseFailed", err)
	}
	a.Results.Put(machine.Accepted{Key: p.Key, Artifacts: p.Artifacts, Run: p.Run, Lines: p.Lines, Files: p.Files,
		Open: p.Open, Findings: fs, Check: p.Check})
	return nil
}

// RecordEvent writes one event to the run's record, with its finding, and the text of the open
// findings its detail lists, read from the artifact store. Its error type SeqTaken is a seq held
// with other content.
func (a *Activities) RecordEvent(_ context.Context, r EventRecord) error {
	e := r.Event
	e.Project, e.Files = r.Project, r.Files
	if r.Finding != nil {
		fs, err := a.findings([]Finding{*r.Finding})
		if err != nil {
			return failed("RecordFailed", err)
		}
		e.Finding = &fs[0]
	}
	if len(r.Open) > 0 {
		open, err := a.findings(r.Open)
		if err != nil {
			return failed("RecordFailed", err)
		}
		e.Detail += ": " + openList(open)
	}
	if err := a.Events.Record(r.Slice, e, r.Operation, r.ReplayOf); err != nil {
		if errors.Is(err, store.ErrSeqTaken) {
			return failed(SeqTaken, err)
		}
		return failed("RecordFailed", err)
	}
	return nil
}

// openList is the list of open findings a hand-over's detail ends with (feature 001's form).
func openList(open []machine.Finding) string {
	list := make([]string, len(open))
	for i, f := range open {
		list[i] = fmt.Sprintf("%s at %s:%d (%s)", f.Rule, f.File, f.Line, f.Text)
	}
	return strings.Join(list, "; ")
}

// store keeps each finding's JSON in the artifact store and returns the findings by hash.
func (a *Activities) store(slice string, in []machine.Finding) ([]Finding, error) {
	var out []Finding
	for _, f := range in {
		b, err := json.Marshal(f)
		if err != nil {
			return nil, err
		}
		h, err := a.Artifacts.PutArtifact(slice, b)
		if err != nil {
			return nil, fmt.Errorf("storing a finding: %w", err)
		}
		out = append(out, Finding{SHA256: h, Rule: f.Rule, File: f.File, Line: f.Line, Blocking: f.Blocking, Check: f.Check,
			Witness: f.Witness})
	}
	return out, nil
}

// findings reads findings by hash from the artifact store; Blocking is the run's.
func (a *Activities) findings(in []Finding) ([]machine.Finding, error) {
	var out []machine.Finding
	for _, f := range in {
		b, err := a.Artifacts.Artifact(f.SHA256)
		if err != nil {
			return nil, fmt.Errorf("finding %s: %w", f.SHA256, err)
		}
		var m machine.Finding
		if err := json.Unmarshal(b, &m); err != nil {
			return nil, fmt.Errorf("finding %s: %w", f.SHA256, err)
		}
		m.Blocking = f.Blocking
		out = append(out, m)
	}
	return out, nil
}
