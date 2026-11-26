package machine

import (
	"context"
	"errors"
	"time"

	"github.com/agentic-platform/engine/canonical"
)

// Writer is what a History also does when it can put a filled step's files into the project (the
// check contract's writeStep, then readStepFiles): a captured pattern's `fill` steps need it, no
// other step does. A run finds it on its History by type assertion.
type Writer interface {
	// Write makes the files of the project that match r.Step.Writable exactly r.Files and
	// returns the project's files afterwards. A repeat gives the same answer. Nothing is
	// committed: the step's commit, once it is accepted, is History.Commit's. An error that
	// wraps ErrWriteRefused is a refusal a repeat cannot pass (the developer must act); any
	// other error is one a repeat can pass.
	Write(ctx context.Context, r WriteRequest) (WriteResult, error)
}

// ErrWriteRefused marks a write a repeat cannot pass: the run hands over to the developer rather
// than being interrupted. Test it with errors.Is.
var ErrWriteRefused = errors.New("the write was refused")

// WriteRequest is writeStep for one attempt of a filled step.
type WriteRequest struct {
	Run       string
	Step      Step // its Writable entries bound the paths
	Attempt   int  // from 1
	Round     int  // 0 for a fill step
	Project   string
	Before    []File           // the files the last accepted step left
	Files     []canonical.File // the step's complete output files; empty for an empty output
	TimeLimit time.Duration    // what is left of the run's time budget
}

// WriteResult is what a write left.
type WriteResult struct {
	Written, Removed []string // writeStep's answer, byte order
	ProjectFiles     []File   // the project's files after the write (readStepFiles), byte order of path
}
