package cli

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	mrand "math/rand/v2"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/agentic-platform/cli/api/gen"
)

// Exit codes of the run commands (contract.md, "Run states and exit codes").
const (
	exitWaits   = 2 // waiting for the developer, interrupted or handed over
	exitExpired = 5
	exitDiffer  = 7
)

// recordedStop is what a signal prints while a mutating call's answer has not arrived; see names
// the command that shows whether it was recorded.
func recordedStop(see string) string {
	return "stopped; the request may have been recorded: see " + see
}

var artifactHash = regexp.MustCompile(`^[0-9a-f]{64}$`)

// esc prints store text with every control character (and any byte that is not UTF-8) escaped as
// strconv.Quote writes it, without the quotes (contract.md, "Store text in the terminal").
func esc(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		r, n := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && n == 1:
			fmt.Fprintf(&b, `\x%02x`, s[i])
		case unicode.IsControl(r):
			q := strconv.QuoteRune(r)
			b.WriteString(q[1 : len(q)-1])
		default:
			b.WriteString(s[i : i+n])
		}
		i += n
	}
	return b.String()
}

func stamp(t time.Time) string { return t.Format(time.RFC3339Nano) }

func needRun(cmd, s string) error {
	if !runID.MatchString(s) {
		return usagef("%s: %q is not a run ID (RUN-<n>)", cmd, s)
	}
	return nil
}

// ---- calls with the contract's retries ----

// lostError is a call whose every try got no answer (no reply, 502, 503, 504 or 429).
type lostError struct {
	op  string
	err error
}

func (l *lostError) Error() string {
	return fmt.Sprintf("engine: no answer to %s after %d tries (%v)", l.op, tries(), l.err)
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// send makes one engine call, sending it again after a lost answer by the contract's retry rule
// (retryRule); a 429 waits at least its Retry-After. It returns the
// first answer that is not lost, with its body read.
func (s *session) send(ctx context.Context, op string, do func(context.Context) (*http.Response, error)) (*http.Response, error) {
	var last error
	var retryAfter time.Duration
	for try := 0; try < tries(); try++ {
		if try > 0 {
			d := max(retryRule().Wait(try-1, mrand.Float64), retryAfter)
			if err := sleep(ctx, d); err != nil {
				return nil, err
			}
		}
		retryAfter = 0
		rsp, err := do(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			last = err
			continue
		}
		body, err := io.ReadAll(rsp.Body)
		_ = rsp.Body.Close()
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			last = err
			continue
		}
		rsp.Body = io.NopCloser(bytes.NewReader(body))
		switch rsp.StatusCode {
		case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			last = fmt.Errorf("HTTP %d", rsp.StatusCode)
			continue
		case http.StatusTooManyRequests:
			last = fmt.Errorf("HTTP %d", rsp.StatusCode)
			if n, err := strconv.Atoi(strings.TrimSpace(rsp.Header.Get("Retry-After"))); err == nil && n > 0 {
				retryAfter = time.Duration(n) * time.Second
			}
			continue
		}
		return rsp, nil
	}
	return nil, &lostError{op: op, err: last}
}

// call sends one engine call with retries and reads its answer, which must have the status want.
func call[T response](ctx context.Context, s *session, op string, want int,
	do func(context.Context) (*http.Response, error), parse func(*http.Response) (T, error)) (T, error) {
	var zero T
	rsp, err := s.send(ctx, op, do)
	if err != nil {
		return zero, err
	}
	r, err := parse(rsp)
	if err != nil {
		return zero, failf(exitFailed, "engine: cannot read the answer to %s: %v", op, err)
	}
	if err := expect(r, nil, want); err != nil {
		return zero, err
	}
	return r, nil
}

func noBody(op string) error { return failf(exitFailed, "engine: the answer to %s has no body", op) }

// newKey is one Idempotency-Key: 32 lowercase hex digits from the operating system's random source.
func newKey() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", failf(exitFailed, "the random source failed: %v", err)
	}
	return hex.EncodeToString(b), nil
}

type accepts interface {
	response
	accepted() *gen.Operation
}

// mutate sends a run command's one mutating call with one Idempotency-Key, resent unchanged after a
// lost answer, and returns the accepted operation.
func mutate[T accepts](ctx context.Context, s *session, op string, do func(context.Context, *string) (*http.Response, error),
	parse func(*http.Response) (T, error)) (*gen.Operation, error) {
	return mutateSee(ctx, s, op, "runs", do, parse)
}

// mutateSee is mutate for any command; see names the command that shows whether a call whose
// answer was lost was recorded.
func mutateSee[T accepts](ctx context.Context, s *session, op, see string, do func(context.Context, *string) (*http.Response, error),
	parse func(*http.Response) (T, error)) (*gen.Operation, error) {
	key, err := newKey()
	if err != nil {
		return nil, err
	}
	*s.detach = recordedStop(see)
	r, err := call(ctx, s, op, http.StatusAccepted, func(ctx context.Context) (*http.Response, error) { return do(ctx, &key) }, parse)
	var lost *lostError
	if errors.As(err, &lost) {
		return nil, failf(exitFailed, "engine: no answer to %s after %d tries (Idempotency-Key %s); it may have been recorded: see %s", op, tries(), key, see)
	}
	if err != nil {
		return nil, err
	}
	if r.accepted() == nil {
		return nil, noBody(op)
	}
	return r.accepted(), nil
}

type startRun struct{ *gen.StartRunResponse }
type resumeRun struct{ *gen.ResumeRunResponse }
type replayRun struct{ *gen.ReplayRunResponse }
type acceptHandOver struct{ *gen.AcceptHandOverResponse }

func (r startRun) accepted() *gen.Operation       { return r.JSON202 }
func (r resumeRun) accepted() *gen.Operation      { return r.JSON202 }
func (r replayRun) accepted() *gen.Operation      { return r.JSON202 }
func (r acceptHandOver) accepted() *gen.Operation { return r.JSON202 }

// wrap turns a generated parser into one returning the wrapper W around its response.
func wrap[R any, W any](parse func(*http.Response) (*R, error), w func(*R) W) func(*http.Response) (W, error) {
	return func(rsp *http.Response) (W, error) {
		r, err := parse(rsp)
		if err != nil {
			var zero W
			return zero, err
		}
		return w(r), nil
	}
}

func (s *session) getRun(ctx context.Context, id string) (*gen.Run, error) {
	r, err := call(ctx, s, "getRun", http.StatusOK, func(ctx context.Context) (*http.Response, error) {
		return s.api.GetRun(ctx, id, &gen.GetRunParams{ApiVersion: APIVersion})
	}, gen.ParseGetRunResponse)
	if err != nil {
		return nil, err
	}
	if r.JSON200 == nil {
		return nil, noBody("getRun")
	}
	return r.JSON200, nil
}

func (s *session) events(ctx context.Context, id string, token *string) (*gen.RunEventList, error) {
	r, err := call(ctx, s, "listRunEvents", http.StatusOK, func(ctx context.Context) (*http.Response, error) {
		return s.api.ListRunEvents(ctx, id, &gen.ListRunEventsParams{ApiVersion: APIVersion, PageToken: token})
	}, gen.ParseListRunEventsResponse)
	if err != nil {
		return nil, err
	}
	if r.JSON200 == nil {
		return nil, noBody("listRunEvents")
	}
	return r.JSON200, nil
}

func (s *session) operation(ctx context.Context, id string) (*gen.Operation, error) {
	r, err := call(ctx, s, "getOperation", http.StatusOK, func(ctx context.Context) (*http.Response, error) {
		return s.api.GetOperation(ctx, id, &gen.GetOperationParams{ApiVersion: APIVersion})
	}, gen.ParseGetOperationResponse)
	if err != nil {
		return nil, err
	}
	if r.JSON200 == nil {
		return nil, noBody("getOperation")
	}
	return r.JSON200, nil
}

// ---- following a run ----

func eventLine(e gen.RunEventSchema) string {
	f := []string{strconv.Itoa(e.Seq), stamp(e.At), esc(string(e.Kind))}
	add := func(format string, a ...any) { f = append(f, fmt.Sprintf(format, a...)) }
	str := func(name string, v *string) {
		if v != nil && *v != "" {
			add("%s%s", name, esc(*v))
		}
	}
	list := func(name string, v *[]string) {
		if v != nil && len(*v) > 0 {
			l := make([]string, len(*v))
			for i, x := range *v {
				l[i] = esc(x)
			}
			add("%s%s", name, strings.Join(l, ","))
		}
	}
	usage := func() {
		if e.Usage != nil {
			add("tokens %d", e.Usage.Tokens)
			add("cost $%.4f", e.Usage.CostUsd)
		}
	}
	changed := func() {
		if e.ChangedLines != nil && len(*e.ChangedLines) > 0 {
			l := make([]string, len(*e.ChangedLines))
			for i, c := range *e.ChangedLines {
				l[i] = fmt.Sprintf("%s:%d", esc(c.File), c.Line)
			}
			add("changed %s", strings.Join(l, ","))
		}
	}
	attempt := func() {
		str("worker ", e.Worker)
		str("model ", e.Model)
		str("agent ", e.AgentDigest)
		list("inputs ", e.Inputs)
		list("artifacts ", e.Artifacts)
		str("reuse ", e.ReuseKey)
	}
	str("", e.Step)
	if e.Attempt != nil {
		add("attempt %d", *e.Attempt)
	}
	if e.Round != nil {
		add("round %d", *e.Round)
	}
	var free []*string
	switch e.Kind {
	case "RunStarted":
		str("slice ", e.Slice)
		str("pattern ", e.Pattern)
		if e.PatternReason != nil && *e.PatternReason != "" {
			add("reason %s", esc(string(*e.PatternReason)))
		}
		if e.Proof != nil && bool(*e.Proof) {
			add("proof")
		}
		str("source ", e.SourceRun)
		if e.Parameters != nil && len(*e.Parameters) > 0 {
			add("parameters %s", parameterList(*e.Parameters))
		}
	case "AttemptMade":
		attempt()
		usage()
	case "StepAccepted":
		attempt()
		changed()
	case "ResultReused":
		list("artifacts ", e.Artifacts)
		str("reuse ", e.ReuseKey)
		if e.OpenRound != nil && bool(*e.OpenRound) { // a reused open review round (contract.md, Event lines)
			add("open round")
		}
		changed()
		if e.Check != nil {
			free = append(free, &e.Check.Reason)
		}
	case "CheckPassed", "CheckFailed":
		if e.Check != nil {
			str("", &e.Check.Name)
			free = append(free, &e.Check.Reason)
		}
	case "StepRejected":
		usage()
	case "FindingRecorded":
		if fd := e.Finding; fd != nil {
			if fd.Blocking {
				add("blocking")
			} else {
				add("suggestion")
			}
			str("", &fd.Rule)
			if fd.File != nil {
				if fd.Line != nil {
					add("%s:%d", esc(*fd.File), *fd.Line)
				} else {
					str("", fd.File)
				}
			}
			str("check ", fd.Check)
			free = append(free, &fd.Text)
		}
	}
	list("refs ", e.Refs)
	for _, t := range append(free, e.Detail) {
		str("", t)
	}
	return strings.Join(f, "  ")
}

// parameterList is a run's parameter values as `<name>=<value>,...`, in byte order of their names.
func parameterList(ps gen.ParameterValues) string {
	names := make([]string, 0, len(ps))
	for n := range ps {
		names = append(names, n)
	}
	sort.Strings(names)
	l := make([]string, len(names))
	for i, n := range names {
		l[i] = esc(n) + "=" + esc(ps[n])
	}
	return strings.Join(l, ",")
}

// patternFields are the run's `pattern` and, when recorded, `reason`, `proof` and `source`
// fields, as the run's header prints them.
func patternFields(r *gen.Run) string {
	f := "pattern " + esc(r.Pattern)
	if r.PatternReason != nil && *r.PatternReason != "" {
		f += "  reason " + esc(string(*r.PatternReason))
	}
	if r.Proof != nil && bool(*r.Proof) {
		f += "  proof"
	}
	if r.SourceRun != nil && *r.SourceRun != "" {
		f += "  source " + esc(*r.SourceRun)
	}
	return f
}

// endLine is the run's end line and the exit code of its state.
func endLine(r *gen.Run) (string, int) {
	reason := func(sep string) string {
		if r.Reason == nil || *r.Reason == "" {
			return ""
		}
		return sep + esc(*r.Reason)
	}
	expires := ""
	if r.ExpiresAt != nil {
		expires = "; it expires " + stamp(*r.ExpiresAt)
	}
	switch r.Status {
	case gen.RunStatusWaitingForDeveloper:
		return fmt.Sprintf("%s waits for the developer%s%s. Take it over with take-over %s, or cancel it with cancel %s.",
			r.Id, reason(": "), expires, r.Id, r.Id), exitWaits
	case gen.RunStatusInterrupted:
		if r.WaitsFor != nil && *r.WaitsFor == gen.RunWaitsForStepWorkers {
			return fmt.Sprintf("%s is interrupted%s%s. It continues by itself once its step workers run: start them with ap build-workers --resume %s, or cancel it with cancel %s.",
				r.Id, reason(": "), expires, r.Id, r.Id), exitWaits
		}
		return fmt.Sprintf("%s is interrupted%s%s. Resume it with resume %s, or cancel it with cancel %s.",
			r.Id, reason(": "), expires, r.Id, r.Id), exitWaits
	case gen.RunStatusEnded:
		outcome := ""
		if r.Outcome != nil {
			outcome = string(*r.Outcome)
		}
		code := exitFailed
		switch gen.RunOutcome(outcome) {
		case gen.RunOutcomeDone:
			return r.Id + " ended done", exitOK
		case gen.RunOutcomeHandedOver:
			code = exitWaits
		case gen.RunOutcomeExpired:
			code = exitExpired
		case gen.RunOutcomeCancelled:
			code = exitCancelled
		}
		return fmt.Sprintf("%s ended %s%s", r.Id, esc(outcome), reason(": ")), code
	}
	return fmt.Sprintf("%s is %s%s", r.Id, esc(string(r.Status)), reason(": ")), exitFailed
}

// follow prints the run's record from its first event, one line per event in seq order, then the
// end line, and returns when the run is no longer running, with the exit code of its state. known is
// the run as already read, or nil (contract.md, "Following a run" and "Inspecting runs").
func (s *session) followRun(ctx context.Context, id string, known *gen.Run) error {
	*s.detach = fmt.Sprintf("stopped following %s; it continues. Follow it with run %s, cancel it with cancel %s.", id, id, id)
	lost := func(err error) error {
		var l *lostError
		if errors.As(err, &l) {
			return failf(exitFailed, "engine: lost the engine while following %s (%v); the run goes on or is interrupted: see run %s", id, l.err, id)
		}
		return err
	}
	run := known
	settled := run != nil && run.Status != gen.RunStatusRunning
	var token *string
	last := 0
	delay := s.follow
	for {
		page, err := s.events(ctx, id, token)
		if err != nil {
			return lost(err)
		}
		fresh := 0
		for _, e := range page.Items {
			if e.Seq > last {
				fmt.Fprintln(s.out, eventLine(e))
				last = e.Seq
				fresh++
			}
		}
		if page.NextPageToken == nil {
			break // the whole record of an ended run
		}
		token = page.NextPageToken
		if fresh > 0 {
			delay = s.follow
			continue
		}
		if settled {
			break
		}
		if err := sleep(ctx, delay); err != nil {
			return err
		}
		delay = min(2*delay, 2*time.Second)
		if run, err = s.getRun(ctx, id); err != nil {
			return lost(err)
		}
		settled = run.Status != gen.RunStatusRunning
	}
	if run == nil || run.Status == gen.RunStatusRunning {
		var err error
		if run, err = s.getRun(ctx, id); err != nil {
			return lost(err)
		}
	}
	op, err := s.operation(ctx, run.Operation)
	if err != nil {
		return lost(err)
	}
	if op.Status == gen.OperationStatusFailed {
		return refused(op.Error)
	}
	line, code := endLine(run)
	fmt.Fprintln(s.out, line)
	if run.Proof != nil && bool(*run.Proof) {
		if code, err = s.proofLine(ctx, run, code); err != nil {
			return lost(err)
		}
	}
	if code == exitOK {
		return nil
	}
	return &failure{code: code}
}

// ---- build, resume, replay ----

func parseBuild(args []string) (func(context.Context, *session) error, error) {
	if len(args) != 2 {
		return nil, usagef("build: want a slice and a project")
	}
	if err := needSlice("build", args[0]); err != nil {
		return nil, err
	}
	slice, project := args[0], args[1]
	return func(ctx context.Context, s *session) error {
		op, err := mutate(ctx, s, "startRun", func(ctx context.Context, key *string) (*http.Response, error) {
			return s.api.StartRun(ctx, slice, &gen.StartRunParams{ApiVersion: APIVersion, IdempotencyKey: key},
				gen.StartRunRequest{Project: project})
		}, wrap(gen.ParseStartRunResponse, func(r *gen.StartRunResponse) startRun { return startRun{r} }))
		if err != nil {
			return err
		}
		id := op.Target
		*s.detach = fmt.Sprintf("stopped following %s; it continues. Follow it with run %s, cancel it with cancel %s.", id, id, id)
		run, err := s.getRun(ctx, id)
		var l *lostError
		if errors.As(err, &l) {
			return failf(exitFailed, "engine: lost the engine while following %s (%v); the run goes on or is interrupted: see run %s", id, l.err, id)
		}
		if err != nil {
			return err
		}
		with := ""
		if run.PatternReason != nil && *run.PatternReason != "" {
			with = fmt.Sprintf(" with %s (%s)", esc(run.Pattern), esc(string(*run.PatternReason)))
		}
		if err := s.print("%s started: building %s into %s%s (operation %s)\n", id, slice, esc(project), with, op.Id); err != nil {
			return err
		}
		return s.followRun(ctx, id, run)
	}, nil
}

func parseResume(args []string) (func(context.Context, *session) error, error) {
	if len(args) != 1 {
		return nil, usagef("resume: want exactly one run")
	}
	if err := needRun("resume", args[0]); err != nil {
		return nil, err
	}
	id := args[0]
	return func(ctx context.Context, s *session) error {
		op, err := mutate(ctx, s, "resumeRun", func(ctx context.Context, key *string) (*http.Response, error) {
			return s.api.ResumeRun(ctx, id, &gen.ResumeRunParams{ApiVersion: APIVersion, IdempotencyKey: key})
		}, wrap(gen.ParseResumeRunResponse, func(r *gen.ResumeRunResponse) resumeRun { return resumeRun{r} }))
		if err != nil {
			return err
		}
		fmt.Fprintf(s.out, "%s resumed (operation %s)\n", op.Target, op.Id)
		return s.followRun(ctx, op.Target, nil)
	}, nil
}

func parseReplay(args []string) (func(context.Context, *session) error, error) {
	if len(args) != 2 {
		return nil, usagef("replay: want a run and a project")
	}
	if err := needRun("replay", args[0]); err != nil {
		return nil, err
	}
	id, project := args[0], args[1]
	return func(ctx context.Context, s *session) error {
		op, err := mutate(ctx, s, "replayRun", func(ctx context.Context, key *string) (*http.Response, error) {
			return s.api.ReplayRun(ctx, id, &gen.ReplayRunParams{ApiVersion: APIVersion, IdempotencyKey: key},
				gen.RunRequest{Project: project})
		}, wrap(gen.ParseReplayRunResponse, func(r *gen.ReplayRunResponse) replayRun { return replayRun{r} }))
		if err != nil {
			return err
		}
		fmt.Fprintf(s.out, "%s started: replaying %s into %s (operation %s)\n", op.Target, id, esc(project), op.Id)
		return s.followRun(ctx, op.Target, nil)
	}, nil
}

// ---- take-over ----

func parseTakeOver(args []string) (func(context.Context, *session) error, error) {
	if len(args) != 1 {
		return nil, usagef("take-over: want exactly one run")
	}
	if err := needRun("take-over", args[0]); err != nil {
		return nil, err
	}
	id := args[0]
	return func(ctx context.Context, s *session) error {
		op, err := mutate(ctx, s, "acceptHandOver", func(ctx context.Context, key *string) (*http.Response, error) {
			return s.api.AcceptHandOver(ctx, id, &gen.AcceptHandOverParams{ApiVersion: APIVersion, IdempotencyKey: key})
		}, wrap(gen.ParseAcceptHandOverResponse, func(r *gen.AcceptHandOverResponse) acceptHandOver { return acceptHandOver{r} }))
		if err != nil {
			return err
		}
		if _, err := s.finish(ctx, op); err != nil {
			return err
		}
		run, err := s.getRun(ctx, id)
		if err != nil {
			return err
		}
		reason := ""
		if run.Reason != nil && *run.Reason != "" {
			reason = ": " + esc(*run.Reason)
		}
		fmt.Fprintf(s.out, "%s ended handed-over%s. The project %s is yours to finish.\n", run.Id, reason, esc(run.Project))
		return nil
	}, nil
}

// ---- runs and run ----

func parseRuns(args []string) (func(context.Context, *session) error, error) {
	if len(args) != 0 {
		return nil, usagef("runs: takes no arguments")
	}
	return func(ctx context.Context, s *session) error {
		var items []gen.Run
		var token *string
		for {
			r, err := call(ctx, s, "listRuns", http.StatusOK, func(ctx context.Context) (*http.Response, error) {
				return s.api.ListRuns(ctx, &gen.ListRunsParams{ApiVersion: APIVersion, PageToken: token})
			}, gen.ParseListRunsResponse)
			if err != nil {
				return err
			}
			if r.JSON200 == nil {
				return noBody("listRuns")
			}
			items = append(items, r.JSON200.Items...)
			if r.JSON200.NextPageToken == nil {
				break
			}
			token = r.JSON200.NextPageToken
		}
		if len(items) == 0 {
			fmt.Fprintln(s.out, "No runs yet. Start one with build <slice> <project>.")
			return nil
		}
		cell := func(v *string) string {
			if v == nil || *v == "" {
				return "-"
			}
			return esc(*v)
		}
		tw := tabwriter.NewWriter(s.out, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "RUN\tSLICE\tSTATUS\tOUTCOME\tPROJECT\tCREATED\tREPLAY OF")
		for _, it := range items {
			var outcome *string
			if it.Outcome != nil {
				o := string(*it.Outcome)
				outcome = &o
			}
			status := string(it.Status)
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", cell(&it.Id), cell(&it.Slice), cell(&status), cell(outcome),
				cell(&it.Project), stamp(it.CreatedAt), cell(it.ReplayOf))
		}
		return tw.Flush()
	}, nil
}

func parseRun(args []string) (func(context.Context, *session) error, error) {
	if len(args) != 1 {
		return nil, usagef("run: want exactly one run")
	}
	if err := needRun("run", args[0]); err != nil {
		return nil, err
	}
	id := args[0]
	return func(ctx context.Context, s *session) error {
		*s.detach = fmt.Sprintf("stopped following %s; it continues. Follow it with run %s, cancel it with cancel %s.", id, id, id)
		run, err := s.getRun(ctx, id)
		var l *lostError
		if errors.As(err, &l) {
			return failf(exitFailed, "engine: lost the engine while following %s (%v); the run goes on or is interrupted: see run %s", id, l.err, id)
		}
		if err != nil {
			return err
		}
		header := fmt.Sprintf("%s  slice %s  %s  project %s  operation %s  created %s",
			run.Id, esc(run.Slice), patternFields(run), esc(run.Project), esc(run.Operation), stamp(run.CreatedAt))
		if run.ReplayOf != nil && *run.ReplayOf != "" {
			header += "  replay of " + esc(*run.ReplayOf)
		}
		fmt.Fprintln(s.out, header)
		return s.followRun(ctx, id, run)
	}, nil
}

// ---- compare ----

func parseCompare(args []string) (func(context.Context, *session) error, error) {
	if len(args) != 2 {
		return nil, usagef("compare: want two runs")
	}
	for _, a := range args {
		if err := needRun("compare", a); err != nil {
			return nil, err
		}
	}
	left, right := args[0], args[1]
	return func(ctx context.Context, s *session) error {
		var diffs []gen.Difference
		var token *string
		for {
			r, err := call(ctx, s, "compareRuns", http.StatusOK, func(ctx context.Context) (*http.Response, error) {
				return s.api.CompareRuns(ctx, left, &gen.CompareRunsParams{ApiVersion: APIVersion, With: right, PageToken: token})
			}, gen.ParseCompareRunsResponse)
			if err != nil {
				return err
			}
			if r.JSON200 == nil {
				return noBody("compareRuns")
			}
			diffs = append(diffs, r.JSON200.Items...)
			if r.JSON200.NextPageToken == nil {
				break
			}
			token = r.JSON200.NextPageToken
		}
		if len(diffs) == 0 {
			fmt.Fprintf(s.out, "%s and %s do not differ.\n", left, right)
			return nil
		}
		value := func(v *string) string {
			if v == nil {
				return "-"
			}
			return esc(*v)
		}
		fmt.Fprintf(s.out, "%s and %s differ in %d place(s):\n", left, right, len(diffs))
		tw := tabwriter.NewWriter(s.out, 0, 4, 2, ' ', 0)
		fmt.Fprintf(tw, "STEP\tFIELD\t%s\t%s\n", left, right)
		for _, d := range diffs {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", esc(d.Step), esc(string(d.Field)), value(d.Left), value(d.Right))
		}
		if err := tw.Flush(); err != nil {
			return err
		}
		return &failure{code: exitDiffer}
	}, nil
}

// ---- artifact ----

func parseArtifact(args []string) (func(context.Context, *session) error, error) {
	if len(args) != 2 {
		return nil, usagef("artifact: want a run and a hash")
	}
	if err := needRun("artifact", args[0]); err != nil {
		return nil, err
	}
	if !artifactHash.MatchString(args[1]) {
		return nil, usagef("artifact: %q is not a hash (64 lowercase hex digits)", args[1])
	}
	id, hash := args[0], args[1]
	return func(ctx context.Context, s *session) error {
		r, err := call(ctx, s, "getArtifact", http.StatusOK, func(ctx context.Context) (*http.Response, error) {
			return s.api.GetArtifact(ctx, id, hash, &gen.GetArtifactParams{ApiVersion: APIVersion})
		}, gen.ParseGetArtifactResponse)
		if err != nil {
			return err
		}
		if _, err := s.out.Write(r.Body); err != nil {
			return failf(exitFailed, "artifact: writing standard output: %v", err)
		}
		return nil
	}, nil
}
