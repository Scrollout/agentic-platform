// Package cli is the terminal client (contract.md): a client of the engine API, through the generated
// client in api/gen only, that adds no behaviour of its own. Run is the whole program; the binary in
// cmd/cli only fills Env from the environment and exits with Run's result.
package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/agentic-platform/cli/api/gen"
)

// APIVersion is the engine contract version this client is generated from.
const APIVersion = "2026-11-12-preview"

// Exit codes (contract.md).
const (
	exitOK        = 0
	exitFailed    = 1 // a check or approval failed
	exitUsage     = 3
	exitRefused   = 4 // refused by the engine; the problem's code is printed
	exitCancelled = 6
)

// Env is what the client takes from its environment: ENGINE_URL, ENGINE_TOKEN and EDITOR, and the
// signals it receives. The first SIGINT or SIGTERM on Signals stops following, never the work, and
// Run exits 130 or 143 (contract.md, "Following a run"); nil means no signal arrives.
type Env struct {
	EngineURL, EngineToken, Editor string
	Signals                        <-chan os.Signal
}

var (
	sliceID     = regexp.MustCompile(`^S-[0-9]+$`)
	runID       = regexp.MustCompile(`^RUN-[0-9]+$`)
	operationID = regexp.MustCompile(`^OP-[0-9]+$`)
)

// failure is an error that carries the exit code it ends the command with.
type failure struct {
	code  int
	msg   string
	prob  *gen.Problem
	cause error // the context's error when the context's cancellation stopped the command
}

func (f *failure) Error() string { return f.msg }
func (f *failure) Unwrap() error { return f.cause }

func usagef(format string, a ...any) error {
	return &failure{code: exitUsage, msg: fmt.Sprintf(format, a...) + "\n" + usageText}
}

func failf(code int, format string, a ...any) error {
	return &failure{code: code, msg: fmt.Sprintf(format, a...)}
}

func refused(p *gen.Problem) error {
	if p == nil {
		p = &gen.Problem{Code: gen.ErrorCodeOperationFailed, Title: "the operation failed"}
	}
	return &failure{code: exitRefused, msg: problemText(p), prob: p}
}

func problemText(p *gen.Problem) string {
	var b strings.Builder
	fmt.Fprintf(&b, "refused: %s", esc(string(p.Code)))
	if p.Title != "" && p.Title != string(p.Code) {
		fmt.Fprintf(&b, ": %s", esc(p.Title))
	}
	if p.Detail != nil && *p.Detail != "" {
		fmt.Fprintf(&b, ": %s", esc(*p.Detail))
	}
	if p.Failures != nil {
		for _, f := range *p.Failures {
			fmt.Fprintf(&b, "\n  failed  %s (%s): %s", esc(f.Check), esc(f.Part), esc(f.Reason))
		}
	}
	return b.String()
}

const usageText = `usage:
  describe [description...]            propose a slice (the description is read from standard input without arguments)
  questions <slice>                    show open questions and answer them, one line each from standard input
  show <slice> [goals|rules|contract]  show a part, its hash and its check results
  edit <slice> goals|rules|contract    open a part in $EDITOR and save it
  approve <slice> goals|rules|contract approve a part at the hash shown
  slices                               list slices
  cancel <run|operation>               cancel a run or other running work
  delete <slice> [--yes]               erase a slice and its records after confirmation
  build <slice> <project>              build the slice into the new project folder and follow the run
  resume <run>                         continue an interrupted run and follow it
  replay <run> <project>               replay an ended run into the new project folder and follow it
  take-over <run>                      accept the hand-over of a run waiting for the developer
  runs                                 list runs
  run <run>                            show a run's record and follow it while it runs
  compare <run> <other>                show the differences between two runs
  artifact <run> <hash>                write an artifact's content to standard output
  capture <run> [--name <name>]        capture a done run as a pattern and print its parameters
  patterns                             list captured patterns
  pattern <id>                         show a captured pattern: parameters and their places, proofs, state, usable
  pattern <id> --drop <parameter>      remove a parameter
  pattern <id> --add [<name>=]<value>  add a parameter (the name is the value when none is given)
  proof-slice <id>                     make or give the captured pattern's proof slice and print its ID
  approve-pattern <id>                 approve a proven captured pattern`

// Run runs one command and returns its exit code.
func Run(ctx context.Context, args []string, env Env, stdin io.Reader, stdout, stderr io.Writer) int {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var mu sync.Mutex
	var got os.Signal
	if env.Signals != nil {
		go func() {
			select {
			case sig, ok := <-env.Signals:
				if ok {
					mu.Lock()
					got = sig
					mu.Unlock()
					cancel()
				}
			case <-ctx.Done():
			}
		}()
	}
	detach := "stopped"
	err := run(ctx, args, env, stdin, stdout, &detach)
	if err == nil {
		return exitOK
	}
	mu.Lock()
	sig := got
	mu.Unlock()
	// A signal detaches only when it stopped the command; a command that already had its own result
	// (an end line, a refusal, a compare table) keeps it.
	if sig != nil && errors.Is(err, context.Canceled) {
		fmt.Fprintln(stderr, detach)
		if n, ok := sig.(syscall.Signal); ok {
			return 128 + int(n)
		}
		return 130
	}
	if msg := err.Error(); msg != "" {
		fmt.Fprintln(stderr, msg)
	}
	var f *failure
	if errors.As(err, &f) {
		return f.code
	}
	return exitFailed
}

type session struct {
	api    *gen.ClientWithResponses
	env    Env
	in     *bufio.Reader
	stdin  io.Reader
	out    io.Writer
	follow time.Duration // first wait between reads of an operation or of a run's record
	detach *string       // what a signal prints: what the client stopped and how to go on
}

func run(ctx context.Context, args []string, env Env, stdin io.Reader, stdout io.Writer, detach *string) error {
	if len(args) == 0 {
		return usagef("no command")
	}
	cmd, rest := args[0], args[1:]
	if cmd == "help" || cmd == "-h" || cmd == "--help" {
		fmt.Fprintln(stdout, usageText)
		return nil
	}
	var do func(context.Context, *session) error
	var err error
	switch cmd {
	case "describe":
		do, err = parseDescribe(rest)
	case "questions":
		do, err = parseQuestions(rest)
	case "show":
		do, err = parseShow(rest)
	case "edit":
		do, err = parseEdit(rest, env)
	case "approve":
		do, err = parseApprove(rest)
	case "slices":
		do, err = parseSlices(rest)
	case "cancel":
		do, err = parseCancel(rest)
	case "delete":
		do, err = parseDelete(rest)
	case "build":
		do, err = parseBuild(rest)
	case "resume":
		do, err = parseResume(rest)
	case "replay":
		do, err = parseReplay(rest)
	case "take-over":
		do, err = parseTakeOver(rest)
	case "runs":
		do, err = parseRuns(rest)
	case "run":
		do, err = parseRun(rest)
	case "compare":
		do, err = parseCompare(rest)
	case "artifact":
		do, err = parseArtifact(rest)
	case "capture":
		do, err = parseCapture(rest)
	case "patterns":
		do, err = parsePatterns(rest)
	case "pattern":
		do, err = parsePattern(rest)
	case "proof-slice":
		do, err = parseProofSlice(rest)
	case "approve-pattern":
		do, err = parseApprovePattern(rest)
	default:
		return usagef("unknown command %q", cmd)
	}
	if err != nil {
		return err
	}
	if env.EngineURL == "" || env.EngineToken == "" {
		return failf(exitUsage, "ENGINE_URL and ENGINE_TOKEN must be set (the launcher sets them)")
	}
	s, err := newSession(env, stdin, stdout, detach)
	if err != nil {
		return err
	}
	return do(ctx, s)
}

func newSession(env Env, stdin io.Reader, stdout io.Writer, detach *string) (*session, error) {
	token := env.EngineToken
	api, err := gen.NewClientWithResponses(env.EngineURL,
		gen.WithHTTPClient(&http.Client{Timeout: 2 * time.Minute}),
		gen.WithRequestEditorFn(func(_ context.Context, r *http.Request) error {
			r.Header.Set("Authorization", "Bearer "+token)
			return nil
		}))
	if err != nil {
		return nil, failf(exitUsage, "ENGINE_URL: %v", err)
	}
	return &session{api: api, env: env, in: bufio.NewReader(stdin), stdin: stdin, out: stdout,
		follow: 100 * time.Millisecond, detach: detach}, nil
}

// printer writes a command's output and keeps the first write error, which done reports, so a
// command whose output could not be written does not exit 0.
type printer struct {
	w   io.Writer
	err error
}

// f writes one formatted piece of output, unless an earlier write failed.
func (p *printer) f(format string, a ...any) {
	if p.err == nil {
		_, p.err = fmt.Fprintf(p.w, format, a...)
	}
}

// done is the first write error as the command's failure (exit 1), or nil.
func (p *printer) done() error {
	if p.err != nil {
		return failf(exitFailed, "writing standard output: %v", p.err)
	}
	return nil
}

// print writes one formatted piece of the command's output and reports a write error as its failure.
func (s *session) print(format string, a ...any) error {
	p := &printer{w: s.out}
	p.f(format, a...)
	return p.done()
}

// response is what every generated response offers.
type response interface {
	StatusCode() int
	GetBody() []byte
}

// expect turns a generated call's result into an error unless it has the wanted status. A problem
// body becomes a refusal carrying its code.
func expect(r response, err error, want int) error {
	if err != nil {
		f := &failure{code: exitFailed, msg: fmt.Sprintf("engine: %v", err)}
		if errors.Is(err, context.Canceled) {
			f.cause = context.Canceled
		}
		return f
	}
	if r.StatusCode() == want {
		return nil
	}
	var p gen.Problem
	if json.Unmarshal(r.GetBody(), &p) == nil && p.Code != "" {
		return refused(&p)
	}
	return failf(exitFailed, "engine: unexpected HTTP %d: %s", r.StatusCode(), strings.TrimSpace(string(r.GetBody())))
}

// wait reads the operation until it ends and returns it as it ended.
func (s *session) wait(ctx context.Context, op *gen.Operation) (*gen.Operation, error) {
	if op == nil {
		return nil, failf(exitFailed, "engine: no operation in the response")
	}
	*s.detach = fmt.Sprintf("stopped following %s; it continues. Cancel it with cancel %s.", op.Id, op.Id)
	delay := s.follow
	for op.Status == gen.OperationStatusAccepted || op.Status == gen.OperationStatusRunning {
		t := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			t.Stop()
			return nil, &failure{code: exitFailed, cause: ctx.Err(),
				msg: fmt.Sprintf("stopped following %s: %v (it continues; `cancel %s` stops it)", op.Id, ctx.Err(), op.Id)}
		case <-t.C:
		}
		if delay < 2*time.Second {
			delay *= 2
		}
		id := op.Id
		r, err := s.api.GetOperationWithResponse(ctx, id, &gen.GetOperationParams{ApiVersion: APIVersion})
		if err := expect(r, err, http.StatusOK); err != nil {
			return nil, err
		}
		if r.JSON200 == nil {
			return nil, failf(exitFailed, "engine: operation %s has no body", id)
		}
		op = r.JSON200
	}
	return op, nil
}

// finish follows an accepted operation and maps how it ended to an error: failed is a refusal with
// the operation's problem, cancelled exits 6.
func (s *session) finish(ctx context.Context, op *gen.Operation) (*gen.Operation, error) {
	op, err := s.wait(ctx, op)
	if err != nil {
		return nil, err
	}
	switch op.Status {
	case gen.OperationStatusSucceeded:
		return op, nil
	case gen.OperationStatusCancelled:
		return nil, failf(exitCancelled, "operation %s was cancelled", op.Id)
	case gen.OperationStatusFailed:
		return nil, refused(op.Error)
	}
	return nil, failf(exitFailed, "operation %s ended with unknown status %q", op.Id, op.Status)
}

func (s *session) getSlice(ctx context.Context, id string) (*gen.Slice, error) {
	r, err := s.api.GetSliceWithResponse(ctx, id, &gen.GetSliceParams{ApiVersion: APIVersion})
	if err := expect(r, err, http.StatusOK); err != nil {
		return nil, err
	}
	if r.JSON200 == nil {
		return nil, failf(exitFailed, "engine: slice %s has no body", id)
	}
	return r.JSON200, nil
}

// readLine reads one line of standard input without its line break; ok is false at its end with
// nothing read.
func (s *session) readLine() (line string, ok bool) {
	l, err := s.in.ReadString('\n')
	if err != nil && l == "" {
		return "", false
	}
	return strings.TrimRight(l, "\r\n"), true
}
