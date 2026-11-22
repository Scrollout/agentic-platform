// Tests for review finding B1 of US2 (research R4 "How"): a re-check under a new method runs with
// context.Background() and the checks' own time limits, never with the caller's context. A caller
// whose context is cancelled, before the call or while the lint runs, must not make the checks of
// a stale slice fail: no approval is withdrawn because of the cancellation, no "withdrawn" history
// entry is written, and the stored checks_method becomes B.
//
// The diff and lint fakes honour their context (they return ctx.Err() when it is done) and record
// whether the context they were given was done. Helpers come from checks_method_test.go.
package slices_test

import (
	"context"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	slicespkg "github.com/agentic-platform/engine/slices"
)

// cmCtxDiff is a ContractDiff that honours its context and records whether it was done.
type cmCtxDiff struct {
	mu   sync.Mutex
	done []bool
}

func (d *cmCtxDiff) Diff(ctx context.Context, _, _ string) ([]slicespkg.ContractFinding, error) {
	err := ctx.Err()
	d.mu.Lock()
	d.done = append(d.done, err != nil)
	d.mu.Unlock()
	return nil, err
}

// cmCtxLint is a ContractLint that honours its context and records whether it was done, at the
// call and when it answers. With block set it waits for release or for its context to end.
type cmCtxLint struct {
	mu      sync.Mutex
	done    []bool
	block   bool
	entered chan struct{}
	release chan struct{}
}

func newCmCtxLint(block bool) *cmCtxLint {
	return &cmCtxLint{block: block, entered: make(chan struct{}, 16), release: make(chan struct{})}
}

func (l *cmCtxLint) Lint(ctx context.Context, _ string) ([]slicespkg.LintFinding, error) {
	sawDone := ctx.Err() != nil
	if l.block {
		l.entered <- struct{}{}
		select {
		case <-l.release:
		case <-ctx.Done():
		case <-time.After(10 * time.Second):
		}
	}
	err := ctx.Err()
	l.mu.Lock()
	l.done = append(l.done, sawDone || err != nil)
	l.mu.Unlock()
	return nil, err
}

func (l *cmCtxLint) snapshot() []bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.done)
}

func (d *cmCtxDiff) snapshot() []bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.done)
}

func TestRecheckIgnoresACancelledCallerContext(t *testing.T) {
	type call func(ctx context.Context, s *slicespkg.Service, sl slicespkg.Slice) error
	scenarios := []struct {
		name string
		// questions: the slice has an open question about the goals, and only rules and contract
		// are approved (Answer needs an open question).
		questions bool
		run       call
	}{
		{"Approve", false, func(ctx context.Context, s *slicespkg.Service, sl slicespkg.Slice) error {
			_, err := s.Approve(ctx, sl.ID, slicespkg.PartGoals, sl.Parts.Goals)
			return err
		}},
		{"ReplaceGoals", false, func(ctx context.Context, s *slicespkg.Service, sl slicespkg.Slice) error {
			_, err := s.ReplaceGoals(ctx, sl.ID, sl.Goals)
			return err
		}},
		{"Create", false, func(ctx context.Context, s *slicespkg.Service, sl slicespkg.Slice) error {
			r, err := s.Create(ctx, sl.Description)
			if err == nil && !r.Reused {
				t.Error("Create did not offer the approved slice of the same description")
			}
			return err
		}},
		{"Answer", true, func(ctx context.Context, s *slicespkg.Service, sl slicespkg.Slice) error {
			_, err := s.Answer(ctx, sl.ID, []slicespkg.Answer{{Question: "Q-1", Text: "All of them"}})
			return err
		}},
	}
	for _, sc := range scenarios {
		for _, when := range []string{"cancelled before the call", "cancelled while the lint runs"} {
			t.Run(sc.name+"/"+when, func(t *testing.T) {
				rec := newCmRecords()
				a, b := cmID(t, "a"), cmID(t, "b")
				var seeded slicespkg.Slice
				var wantApproved []slicespkg.Part
				if sc.questions {
					sa := cmOpen(t, rec, a, apiPattern(t), &fakeAnalyst{results: []analystResult{
						{p: qProposal(t, slicespkg.QuestionDraft{About: "G-1", Text: "Which notes?"})}}}, &cmDiff{}, newCmLint())
					seeded = create(t, sa, cmDesc)
					for _, p := range []struct {
						part slicespkg.Part
						hash string
					}{{slicespkg.PartRules, seeded.Parts.Rules}, {slicespkg.PartContract, seeded.Parts.Contract}} {
						var err error
						if seeded, err = sa.Approve(context.Background(), seeded.ID, p.part, p.hash); err != nil {
							t.Fatal(err)
						}
					}
					wantApproved = []slicespkg.Part{slicespkg.PartRules, slicespkg.PartContract}
				} else {
					seeded = cmSeed(t, rec, a, apiPattern(t), notesProposal(t))[0]
					wantApproved = []slicespkg.Part{slicespkg.PartGoals, slicespkg.PartRules, slicespkg.PartContract}
				}

				revised := notesProposal(t)
				revised.Goals[0].Text = "Keep a note for good"
				an := &fakeAnalyst{results: []analystResult{{p: revised}}}
				d, l := &cmCtxDiff{}, newCmCtxLint(when == "cancelled while the lint runs")
				s := cmOpen(t, rec, b, apiPattern(t), an, d, l)

				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				var err error
				if when == "cancelled before the call" {
					cancel()
					err = sc.run(ctx, s, seeded)
				} else {
					wait := cmGo(func() error { return sc.run(ctx, s, seeded) })
					select {
					case <-l.entered:
					case <-time.After(5 * time.Second):
						t.Fatal("the re-check never called the lint")
					}
					cancel() // the caller gives up while the lint runs
					time.Sleep(50 * time.Millisecond)
					close(l.release)
					err = wait(t)
				}
				if err != nil {
					t.Fatalf("%s: %v (the checks do not run under the caller's context)", sc.name, err)
				}

				if got := l.snapshot(); len(got) == 0 || slices.Contains(got, true) {
					t.Errorf("lint contexts done: %v; want at least one call, none under a cancelled context", got)
				}
				if got := d.snapshot(); len(got) == 0 || slices.Contains(got, true) {
					t.Errorf("compatibility contexts done: %v; want at least one call, none under a cancelled context", got)
				}
				got, gerr := s.Get(seeded.ID)
				if gerr != nil {
					t.Fatal(gerr)
				}
				if refs := cmWithdrawnRefs(got); len(refs) != 0 {
					t.Errorf("approvals withdrawn because of the cancellation: %v; history %v", refs, historyKinds(got))
				}
				if slices.Contains(historyKinds(got), "withdrawn") {
					t.Errorf("history %v holds a withdrawn entry", historyKinds(got))
				}
				for _, p := range wantApproved {
					if _, ok := approval(got, p); !ok {
						t.Errorf("the %s approval was lost", p)
					}
				}
				if len(cmFailed(got, slicespkg.CheckContractLint)) != 0 || len(cmFailed(got, slicespkg.CheckContractCompatibility)) != 0 {
					t.Errorf("a contract check fails: %+v", got.Checks)
				}
				if m := cmStoredMethod(t, rec, seeded.ID); m != b {
					t.Errorf("stored checks_method %q, want %q", m, b)
				}
				if got.ChecksMethod != b {
					t.Errorf("ChecksMethod %q, want %q", got.ChecksMethod, b)
				}
				if !sc.questions && !reflect.DeepEqual(historyKinds(got), historyKinds(seeded)) {
					t.Errorf("history %v, want %v", historyKinds(got), historyKinds(seeded))
				}
			})
		}
	}
}
