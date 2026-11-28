package cli

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/agentic-platform/cli/api/gen"
)

// Trial sets (contract.md, "Trial sets"): `trials` starts a set and follows it, `trial-set` shows
// one and follows it while it runs, `trial-sets` lists a slice's sets.

var (
	trialSetID = regexp.MustCompile(`^TS-[0-9]+$`)
	trialCount = regexp.MustCompile(`^[1-9][0-9]*$`)
)

// trialsMax is the most trials of one set: x-limits.trials-max of the embedded engine contract.
func trialsMax() int {
	var d struct {
		Limits struct {
			TrialsMax int `yaml:"trials-max"`
		} `yaml:"x-limits"`
	}
	if err := yaml.Unmarshal(engineContract, &d); err != nil || d.Limits.TrialsMax < 1 {
		panic(fmt.Sprintf("engine contract: x-limits.trials-max: %v (%d)", err, d.Limits.TrialsMax))
	}
	return d.Limits.TrialsMax
}

// trialsDefault is how many trials a set has when none is asked for: x-limits.trials-default of
// the embedded engine contract.
func trialsDefault() int {
	var d struct {
		Limits struct {
			TrialsDefault int `yaml:"trials-default"`
		} `yaml:"x-limits"`
	}
	if err := yaml.Unmarshal(engineContract, &d); err != nil || d.Limits.TrialsDefault < 1 {
		panic(fmt.Sprintf("engine contract: x-limits.trials-default: %v (%d)", err, d.Limits.TrialsDefault))
	}
	return d.Limits.TrialsDefault
}

type startTrialSet struct{ *gen.StartTrialSetResponse }

func (r startTrialSet) accepted() *gen.Operation { return r.JSON202 }

// parseTrials reads `trials <slice> <prefix> [--trials <k>]`; without --trials the engine's default
// applies.
func parseTrials(args []string) (func(context.Context, *session) error, error) {
	var count *int
	switch {
	case len(args) == 4 && args[2] == "--trials":
		if !trialCount.MatchString(args[3]) {
			return nil, usagef("trials: --trials takes a whole number from 1 to %d, not %q", trialsMax(), args[3])
		}
		k, err := strconv.Atoi(args[3])
		if err != nil || k > trialsMax() {
			return nil, usagef("trials: --trials takes a whole number from 1 to %d, not %q", trialsMax(), args[3])
		}
		count = &k
	case len(args) != 2:
		return nil, usagef("trials: want a slice and a project prefix, and optionally --trials <k>")
	}
	if err := needSlice("trials", args[0]); err != nil {
		return nil, err
	}
	slice, prefix := args[0], args[1]
	return func(ctx context.Context, s *session) error {
		op, err := mutateSee(ctx, s, "startTrialSet", "trial-sets "+slice, func(ctx context.Context, key *string) (*http.Response, error) {
			return s.api.StartTrialSet(ctx, slice, &gen.StartTrialSetParams{ApiVersion: APIVersion, IdempotencyKey: key},
				gen.TrialSetRequest{ProjectPrefix: prefix, Trials: count})
		}, wrap(gen.ParseStartTrialSetResponse, func(r *gen.StartTrialSetResponse) startTrialSet { return startTrialSet{r} }))
		if err != nil {
			return err
		}
		s.detachSet(op.Target, op.Id)
		if err := s.print("%s  operation %s\n", esc(op.Target), esc(op.Id)); err != nil {
			return err
		}
		return s.followSet(ctx, op.Target, op.Id)
	}, nil
}

func parseTrialSet(args []string) (func(context.Context, *session) error, error) {
	if len(args) != 1 {
		return nil, usagef("trial-set: want exactly one trial set")
	}
	if !trialSetID.MatchString(args[0]) {
		return nil, usagef("trial-set: %q is not a trial set ID (TS-<n>)", args[0])
	}
	id := args[0]
	return func(ctx context.Context, s *session) error {
		*s.detach = fmt.Sprintf("stopped following %s; it continues. Follow it with trial-set %s.", id, id)
		return s.followSet(ctx, id, "")
	}, nil
}

// parseTrialSets reads `trial-sets <slice>`: every page of the slice's sets in one table, newest
// first, as the engine orders them.
func parseTrialSets(args []string) (func(context.Context, *session) error, error) {
	if len(args) != 1 {
		return nil, usagef("trial-sets: want exactly one slice")
	}
	if err := needSlice("trial-sets", args[0]); err != nil {
		return nil, err
	}
	slice := args[0]
	return func(ctx context.Context, s *session) error {
		var items []gen.TrialSet
		var token *string
		seen := map[string]bool{}
		for {
			r, err := call(ctx, s, "listTrialSets", http.StatusOK, func(ctx context.Context) (*http.Response, error) {
				return s.api.ListTrialSets(ctx, slice, &gen.ListTrialSetsParams{ApiVersion: APIVersion, PageToken: token})
			}, gen.ParseListTrialSetsResponse)
			if err != nil {
				return err
			}
			if r.JSON200 == nil {
				return noBody("listTrialSets")
			}
			items = append(items, r.JSON200.Items...)
			next := r.JSON200.NextPageToken
			if next == nil {
				break
			}
			if seen[*next] {
				return failf(exitFailed, "engine: listTrialSets gave the page token %q again", esc(*next))
			}
			seen[*next] = true
			token = next
		}
		if len(items) == 0 {
			return s.print("No trial sets yet. Start one with trials %s <prefix>.\n", slice)
		}
		tw := tabwriter.NewWriter(s.out, 0, 4, 2, ' ', 0)
		_, _ = fmt.Fprintln(tw, "SET\tSTATE\tTRIALS\tDONE\tUNSTABLE\tMETHOD\tCREATED")
		for _, it := range items {
			unstable := "-"
			if it.Result.Unstable {
				unstable = "unstable"
			}
			method := it.Method
			if len(method) > 12 {
				method = method[:12]
			}
			_, _ = fmt.Fprintf(tw, "%s\t%s\t%d\t%d/%d\t%s\t%s\t%s\n", esc(it.Id), esc(string(it.State)), it.Trials,
				it.Result.Done, it.Result.Ended, unstable, esc(method), stamp(it.CreatedAt))
		}
		if err := tw.Flush(); err != nil {
			return failf(exitFailed, "writing standard output: %v", err)
		}
		return nil
	}, nil
}

// detachSet is what a signal prints while the set is followed: the set continues, and its
// operation is how to cancel it.
func (s *session) detachSet(id, op string) {
	*s.detach = fmt.Sprintf("stopped following %s (operation %s); it continues. Follow it with trial-set %s, cancel it with cancel %s.",
		esc(id), esc(op), esc(id), esc(op))
}

func (s *session) getTrialSet(ctx context.Context, id string) (*gen.TrialSet, error) {
	r, err := call(ctx, s, "getTrialSet", http.StatusOK, func(ctx context.Context) (*http.Response, error) {
		return s.api.GetTrialSet(ctx, id, &gen.GetTrialSetParams{ApiVersion: APIVersion})
	}, gen.ParseGetTrialSetResponse)
	if err != nil {
		return nil, err
	}
	if r.JSON200 == nil {
		return nil, noBody("getTrialSet")
	}
	return r.JSON200, nil
}

// readGrace is how long a read already sent may still finish after a signal, so that the operation
// it names can be printed with the detach message.
const readGrace = 2 * time.Second

// firstRead reads the set before its operation is known: a signal lets the read finish (up to
// readGrace), so the detach message names the operation.
func (s *session) firstRead(ctx context.Context, id string) (*gen.TrialSet, error) {
	inner, cancel := context.WithCancel(context.WithoutCancel(ctx))
	defer cancel()
	stop := context.AfterFunc(ctx, func() { time.AfterFunc(readGrace, cancel) })
	defer stop()
	set, err := s.getTrialSet(inner, id)
	if set != nil {
		s.detachSet(id, set.Operation)
	}
	if ctx.Err() != nil {
		return nil, &failure{code: exitFailed, cause: context.Canceled, msg: "stopped"}
	}
	return set, err
}

// followSet prints one line per trial as it ends, in trial order, until the set is no longer
// running, then the result, and ends with the exit code of the set's state. op is the set's
// operation, or "" when not known yet.
func (s *session) followSet(ctx context.Context, id, op string) error {
	lost := func(err error) error {
		var l *lostError
		if errors.As(err, &l) {
			return failf(exitFailed, "engine: lost the engine while following %s (%v); the set goes on: see trial-set %s", id, l.err, id)
		}
		return err
	}
	printed := map[int]bool{}
	delay := s.follow
	for {
		var set *gen.TrialSet
		var err error
		if op == "" {
			set, err = s.firstRead(ctx, id)
		} else {
			set, err = s.getTrialSet(ctx, id)
		}
		if err != nil {
			return lost(err)
		}
		if op == "" {
			op = set.Operation
		}
		for _, o := range set.Result.Outcomes {
			if o.Outcome == nil || printed[o.Trial] {
				continue
			}
			printed[o.Trial] = true
			if err := s.print("trial %d  %s  %s\n", o.Trial, esc(o.Run), esc(string(*o.Outcome))); err != nil {
				return err
			}
		}
		if set.State != gen.TrialSetStateRunning {
			return s.printResult(set)
		}
		o, err := s.operation(ctx, set.Operation)
		if err != nil {
			return lost(err)
		}
		if o.Status == gen.OperationStatusFailed {
			return refused(o.Error)
		}
		if err := sleep(ctx, delay); err != nil {
			return err
		}
		delay = min(2*delay, 2*time.Second)
	}
}

// printResult prints the result of a set that is no longer running and returns the exit code of
// its state (contract.md, "Trial sets").
func (s *session) printResult(set *gen.TrialSet) error {
	r := set.Result
	first := fmt.Sprintf("%s  slice %s  state %s", esc(set.Id), esc(set.Slice), esc(string(set.State)))
	if set.StoppedBecause != nil && *set.StoppedBecause != "" {
		first += "  stopped " + esc(string(*set.StoppedBecause))
	}
	reason := ""
	if set.FailedBecause != nil {
		reason = *set.FailedBecause
	}
	if set.State == gen.TrialSetStateFailed {
		first += "  failed " + esc(reason)
	}
	first += fmt.Sprintf("  done %d/%d", r.Done, r.Ended)
	if r.Unstable {
		first += "  unstable"
	}
	if r.MethodsMixed {
		first += "  methods mixed"
	}
	if err := s.print("%s\n", first); err != nil {
		return err
	}
	tw := tabwriter.NewWriter(s.out, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "RUN\tOUTCOME\tATTEMPTS\tROUNDS\tSECONDS\tTOKENS\tCOST")
	for _, o := range r.Outcomes {
		outcome := "-"
		if o.Outcome != nil {
			outcome = esc(string(*o.Outcome))
		}
		t := o.Totals
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", esc(o.Run), outcome, intCell(t.Attempts), intCell(t.ReviewRounds),
			secondsCell(t.Seconds), intCell(t.Tokens), costCell(t.CostUsd))
	}
	if err := tw.Flush(); err != nil {
		return failf(exitFailed, "writing standard output: %v", err)
	}
	tw = tabwriter.NewWriter(s.out, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "STEP\tREACHED\tATTEMPTS\tROUNDS\tSECONDS\tTOKENS\tCOST\tFILES")
	for _, st := range r.Steps {
		m := st.Measures
		_, _ = fmt.Fprintf(tw, "%s\t%d\t%s\t%s\t%s\t%s\t%s\t%s\n", esc(st.Step), st.Reached, spreadCell(m.Attempts, countValue),
			spreadCell(m.ReviewRounds, countValue), spreadCell(m.Seconds, countValue), spreadCell(m.Tokens, countValue),
			spreadCell(m.CostUsd, costValue), filesCell(st.Files))
	}
	if err := tw.Flush(); err != nil {
		return failf(exitFailed, "writing standard output: %v", err)
	}
	settled := set.State == gen.TrialSetStateEnded || set.State == gen.TrialSetStateStopped
	if p := r.Previous; settled && p != nil {
		line := fmt.Sprintf("compared with %s: done %d/%d -> %d/%d", esc(p.TrialSet), p.Done, p.Ended, r.Done, r.Ended)
		if p.LessReliable {
			line += "  less reliable"
		}
		if p.MethodsDiffer {
			line += "  methods differ"
		}
		if err := s.print("%s\n", line); err != nil {
			return err
		}
	}
	switch {
	case set.State == gen.TrialSetStateCancelled:
		return &failure{code: exitCancelled}
	case set.State == gen.TrialSetStateFailed: // the reason is the set's, not its operation's
		return failf(exitRefused, "trial set %s failed: %s", esc(set.Id), esc(reason))
	case settled && r.Ended >= 1 && r.Done == r.Ended:
		return nil
	case settled:
		return &failure{code: exitDiffer}
	}
	return failf(exitFailed, "trial set %s ended in the unknown state %q", esc(set.Id), esc(string(set.State)))
}

func intCell(v *int) string {
	if v == nil {
		return "-"
	}
	return strconv.Itoa(*v)
}

func secondsCell(v *float32) string {
	if v == nil {
		return "-"
	}
	return strconv.FormatFloat(math.Round(float64(*v)), 'f', 0, 64)
}

func costCell(v *float32) string {
	if v == nil {
		return "-"
	}
	return costValue(float64(*v), false)
}

// countValue is a whole count or whole seconds; a median that falls between two whole values keeps
// one decimal.
func countValue(v float64, median bool) string {
	w := math.Round(v)
	if median && math.Abs(v-w) >= 0.05 {
		return strconv.FormatFloat(v, 'f', 1, 64)
	}
	return strconv.FormatFloat(w, 'f', 0, 64)
}

func costValue(v float64, _ bool) string { return fmt.Sprintf("$%.2f", v) }

// spreadCell is `<lowest>/<median>/<highest>`, or `-` when no trial carries the measure.
func spreadCell(sp *gen.Spread, value func(float64, bool) string) string {
	if sp == nil {
		return "-"
	}
	return value(float64(sp.Lowest), false) + "/" + value(float64(sp.Median), true) + "/" + value(float64(sp.Highest), false)
}

// filesCell is `same`, the groups of trials with equal files as `1,2|3`, or `-` when no trial
// accepted the step.
func filesCell(f *gen.StepFiles) string {
	if f == nil {
		return "-"
	}
	if f.Same {
		return "same"
	}
	groups := make([]string, 0, len(f.Groups))
	for _, g := range f.Groups {
		nums := make([]string, len(g.Trials))
		for i, n := range g.Trials {
			nums[i] = strconv.Itoa(n)
		}
		groups = append(groups, strings.Join(nums, ","))
	}
	if len(groups) == 0 {
		return "-"
	}
	return strings.Join(groups, "|")
}
