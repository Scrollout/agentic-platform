package cli

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"text/tabwriter"

	"github.com/agentic-platform/cli/api/gen"
)

// The forms of the engine contract's slice-defs.schema.json the client checks before it calls:
// pattern_id for a captured pattern's ID, and parameter_name for the name `--add <value>` takes
// from its value (contract.md, "Captured patterns (feature 011)").
var (
	patternID     = regexp.MustCompile(`^[a-z0-9-]+$`)
	parameterName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)
)

const noParameters = "No parameters: only a slice identical to the source fits."

func needPattern(cmd, s string) error {
	if !patternID.MatchString(s) {
		return usagef("%s: %q is not a captured pattern ID (lowercase letters, digits and hyphens)", cmd, s)
	}
	return nil
}

type captureRun struct{ *gen.CaptureRunResponse }
type replaceParameters struct{ *gen.ReplaceParametersResponse }
type makeProofSlice struct{ *gen.MakeProofSliceResponse }
type approveCapturedPattern struct {
	*gen.ApproveCapturedPatternResponse
}
type withdrawCapturedPattern struct {
	*gen.WithdrawCapturedPatternResponse
}
type eraseCapturedPattern struct {
	*gen.EraseCapturedPatternResponse
}

func (r captureRun) accepted() *gen.Operation             { return r.JSON202 }
func (r replaceParameters) accepted() *gen.Operation      { return r.JSON202 }
func (r makeProofSlice) accepted() *gen.Operation         { return r.JSON202 }
func (r approveCapturedPattern) accepted() *gen.Operation { return r.JSON202 }
func (r withdrawCapturedPattern) accepted() *gen.Operation {
	return r.JSON202
}
func (r eraseCapturedPattern) accepted() *gen.Operation { return r.JSON202 }

func (s *session) getCapturedPattern(ctx context.Context, id string) (*gen.CapturedPattern, error) {
	r, err := call(ctx, s, "getCapturedPattern", http.StatusOK, func(ctx context.Context) (*http.Response, error) {
		return s.api.GetCapturedPattern(ctx, id, &gen.GetCapturedPatternParams{ApiVersion: APIVersion})
	}, gen.ParseGetCapturedPatternResponse)
	if err != nil {
		return nil, err
	}
	if r.JSON200 == nil {
		return nil, noBody("getCapturedPattern")
	}
	return r.JSON200, nil
}

// patternOf is the captured pattern an operation created or changed: its result's
// captured_pattern, else its target.
func patternOf(op *gen.Operation) string {
	if op.Result != nil && op.Result.CapturedPattern != nil && *op.Result.CapturedPattern != "" {
		return *op.Result.CapturedPattern
	}
	return op.Target
}

// ---- capture ----

func parseCapture(args []string) (func(context.Context, *session) error, error) {
	var run string
	var name *string
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--name":
			if i+1 >= len(args) || name != nil {
				return nil, usagef("capture: --name takes one name")
			}
			i++
			v := args[i]
			name = &v
		case strings.HasPrefix(a, "-"):
			return nil, usagef("capture: unknown option %q", a)
		case run != "":
			return nil, usagef("capture: want one run and at most --name <name>")
		default:
			run = a
		}
	}
	if run == "" {
		return nil, usagef("capture: want a run")
	}
	if err := needRun("capture", run); err != nil {
		return nil, err
	}
	return func(ctx context.Context, s *session) error {
		op, err := mutateSee(ctx, s, "captureRun", "patterns", func(ctx context.Context, key *string) (*http.Response, error) {
			return s.api.CaptureRun(ctx, &gen.CaptureRunParams{ApiVersion: APIVersion, IdempotencyKey: key},
				gen.CaptureRequest{Run: run, Name: name})
		}, wrap(gen.ParseCaptureRunResponse, func(r *gen.CaptureRunResponse) captureRun { return captureRun{r} }))
		if err != nil {
			return err
		}
		if op, err = s.finish(ctx, op); err != nil {
			return err
		}
		cp, err := s.getCapturedPattern(ctx, patternOf(op))
		if err != nil {
			return err
		}
		p := &printer{w: s.out}
		p.f("Captured %s from %s (operation %s).\n", esc(cp.Id), run, op.Id)
		writeParameters(p, cp.Parameters)
		return p.done()
	}, nil
}

// ---- patterns ----

func parsePatterns(args []string) (func(context.Context, *session) error, error) {
	if len(args) != 0 {
		return nil, usagef("patterns: takes no arguments")
	}
	return func(ctx context.Context, s *session) error {
		var items []gen.CapturedPattern
		var token *string
		for {
			r, err := call(ctx, s, "listCapturedPatterns", http.StatusOK, func(ctx context.Context) (*http.Response, error) {
				return s.api.ListCapturedPatterns(ctx, &gen.ListCapturedPatternsParams{ApiVersion: APIVersion, PageToken: token})
			}, gen.ParseListCapturedPatternsResponse)
			if err != nil {
				return err
			}
			if r.JSON200 == nil {
				return noBody("listCapturedPatterns")
			}
			items = append(items, r.JSON200.Items...)
			if r.JSON200.NextPageToken == nil {
				break
			}
			token = r.JSON200.NextPageToken
		}
		if len(items) == 0 {
			return s.print("No captured patterns yet. Capture a done run with capture <run>.\n")
		}
		tw := tabwriter.NewWriter(s.out, 0, 4, 2, ' ', 0)
		p := &printer{w: tw}
		p.f("PATTERN\tSTATE\tUSABLE\tPARAMETERS\tSOURCE\tPROOF SLICE\tCREATED\n")
		for _, it := range items {
			proof := "-"
			if it.ProofSlice != nil && *it.ProofSlice != "" {
				proof = esc(*it.ProofSlice)
			}
			p.f("%s\t%s\t%s\t%d\t%s\t%s\t%s\n", esc(it.Id), esc(string(it.State)), yesNo(it.Usable),
				len(it.Parameters), esc(it.Source.Run), proof, stamp(it.CreatedAt))
		}
		if err := p.done(); err != nil {
			return err
		}
		if err := tw.Flush(); err != nil {
			return failf(exitFailed, "writing standard output: %v", err)
		}
		return nil
	}, nil
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// ---- pattern, --drop, --add ----

func parsePattern(args []string) (func(context.Context, *session) error, error) {
	if len(args) != 1 && len(args) != 3 {
		return nil, usagef("pattern: want a captured pattern, then at most one --drop <parameter> or --add [<name>=]<value>")
	}
	id := args[0]
	if err := needPattern("pattern", id); err != nil {
		return nil, err
	}
	if len(args) == 1 {
		return func(ctx context.Context, s *session) error {
			cp, err := s.getCapturedPattern(ctx, id)
			if err != nil {
				return err
			}
			p := &printer{w: s.out}
			writePattern(p, cp)
			return p.done()
		}, nil
	}
	flag, arg := args[1], args[2]
	var change func(current []gen.ParameterValueRequest) ([]gen.ParameterValueRequest, error)
	switch flag {
	case "--drop":
		change = func(current []gen.ParameterValueRequest) ([]gen.ParameterValueRequest, error) {
			kept := make([]gen.ParameterValueRequest, 0, len(current))
			for _, p := range current {
				if p.Name != arg {
					kept = append(kept, p)
				}
			}
			if len(kept) == len(current) {
				return nil, usagef("pattern: %s has no parameter %q; nothing was changed", id, arg)
			}
			return kept, nil
		}
	case "--add":
		name, value, named := strings.Cut(arg, "=")
		if !named {
			name, value = arg, arg
			if !parameterName.MatchString(name) {
				return nil, usagef("pattern: --add %q: the value is not a parameter name (a lowercase letter, then lowercase letters, digits and hyphens, at most 64), so give one: --add <name>=<value>", arg)
			}
		}
		change = func(current []gen.ParameterValueRequest) ([]gen.ParameterValueRequest, error) {
			out := make([]gen.ParameterValueRequest, 0, len(current)+1)
			for _, p := range current {
				if p.Name != name { // a parameter of the same name is replaced by the one added
					out = append(out, p)
				}
			}
			return append(out, gen.ParameterValueRequest{Name: name, Value: value}), nil
		}
	default:
		return nil, usagef("pattern: unknown option %q (want --drop or --add)", flag)
	}
	return func(ctx context.Context, s *session) error {
		cp, err := s.getCapturedPattern(ctx, id)
		if err != nil {
			return err
		}
		current := make([]gen.ParameterValueRequest, len(cp.Parameters))
		for i, p := range cp.Parameters {
			current[i] = gen.ParameterValueRequest{Name: p.Name, Value: p.Value}
		}
		next, err := change(current)
		if err != nil {
			return err
		}
		op, err := mutateSee(ctx, s, "replaceParameters", "pattern "+id, func(ctx context.Context, key *string) (*http.Response, error) {
			return s.api.ReplaceParameters(ctx, id, &gen.ReplaceParametersParams{ApiVersion: APIVersion, IdempotencyKey: key},
				gen.ParametersRequest{Parameters: next})
		}, wrap(gen.ParseReplaceParametersResponse, func(r *gen.ReplaceParametersResponse) replaceParameters { return replaceParameters{r} }))
		if err != nil {
			return err
		}
		if op, err = s.finish(ctx, op); err != nil {
			return err
		}
		if len(next) == 0 {
			return s.print("%s has no parameter now (operation %s).\n%s\n", esc(id), op.Id, noParameters)
		}
		l := make([]string, len(next))
		for i, p := range next {
			l[i] = esc(p.Name) + "=" + esc(p.Value)
		}
		return s.print("%s has the parameters %s now (operation %s).\n", esc(id), strings.Join(l, ", "), op.Id)
	}, nil
}

// writePattern prints a captured pattern: its state and usable, where it came from, its
// parameters with their places, its proofs and its approval.
func writePattern(w *printer, cp *gen.CapturedPattern) {
	w.f("%s  %s  usable %s\n", esc(cp.Id), esc(string(cp.State)), yesNo(cp.Usable))
	w.f("base %s  source %s (slice %s)  created %s\n", esc(cp.Base), esc(cp.Source.Run), esc(cp.Source.Slice), stamp(cp.CreatedAt))
	if cp.ProofSlice != nil && *cp.ProofSlice != "" {
		w.f("proof slice %s\n", esc(*cp.ProofSlice))
	}
	writeParameters(w, cp.Parameters)
	if len(cp.Proofs) == 0 {
		w.f("proofs: none yet\n")
	} else {
		w.f("proofs, newest last:\n")
		for _, p := range cp.Proofs {
			w.f("  %s  %s\n", esc(p.Run), proofOutcome(p))
		}
	}
	if a := cp.Approval; a != nil {
		w.f("approved by %s at %s on the proof %s\n", esc(string(a.By)), stamp(a.At), esc(a.Proof))
	}
}

// writeParameters prints each parameter with every place its value occurs, or the contract's
// `No parameters` line for none.
func writeParameters(w *printer, ps []gen.Parameter) {
	if len(ps) == 0 {
		w.f("%s\n", noParameters)
		return
	}
	w.f("parameters:\n")
	for _, p := range ps {
		w.f("  %s  %s  value %s  (%s, %s)\n", esc(p.Name), esc(string(p.Kind)), esc(p.Value),
			esc(string(p.Source)), esc(string(p.Origin)))
		for _, pl := range p.Places {
			w.f("    %s  %s  line %d  column %d  %s\n", esc(pl.Part), esc(string(pl.In)), pl.Line, pl.Column, esc(string(pl.Form)))
		}
	}
}

// proofOutcome is a proof's outcome as the proof line gives it: `passed`, `running`, or
// `failed: <failure>; ...`.
func proofOutcome(p gen.Proof) string {
	if p.Outcome != gen.ProofOutcomeFailed {
		return esc(string(p.Outcome))
	}
	fs := make([]string, len(p.Failures))
	for i, f := range p.Failures {
		var at []string
		for _, v := range []*string{f.Step, f.Check, f.Path} {
			if v != nil && *v != "" {
				at = append(at, esc(*v))
			}
		}
		if len(at) == 0 {
			fs[i] = esc(f.Reason)
		} else {
			fs[i] = strings.Join(at, " ") + ": " + esc(f.Reason)
		}
	}
	if len(fs) == 0 {
		return "failed"
	}
	return "failed: " + strings.Join(fs, "; ")
}

// ---- proof-slice ----

func parseProofSlice(args []string) (func(context.Context, *session) error, error) {
	if len(args) != 1 {
		return nil, usagef("proof-slice: want exactly one captured pattern")
	}
	id := args[0]
	if err := needPattern("proof-slice", id); err != nil {
		return nil, err
	}
	return func(ctx context.Context, s *session) error {
		op, err := mutateSee(ctx, s, "makeProofSlice", "pattern "+id, func(ctx context.Context, key *string) (*http.Response, error) {
			return s.api.MakeProofSlice(ctx, id, &gen.MakeProofSliceParams{ApiVersion: APIVersion, IdempotencyKey: key})
		}, wrap(gen.ParseMakeProofSliceResponse, func(r *gen.MakeProofSliceResponse) makeProofSlice { return makeProofSlice{r} }))
		if err != nil {
			return err
		}
		if op, err = s.finish(ctx, op); err != nil {
			return err
		}
		// Standard output carries only the slice's ID, which the launcher's `prove` reads.
		if op.Result == nil || op.Result.Slice == nil || !sliceID.MatchString(*op.Result.Slice) {
			return failf(exitFailed, "engine: operation %s names no proof slice", op.Id)
		}
		return s.print("%s\n", *op.Result.Slice)
	}, nil
}

// ---- approve-pattern ----

func parseApprovePattern(args []string) (func(context.Context, *session) error, error) {
	if len(args) != 1 {
		return nil, usagef("approve-pattern: want exactly one captured pattern")
	}
	id := args[0]
	if err := needPattern("approve-pattern", id); err != nil {
		return nil, err
	}
	return func(ctx context.Context, s *session) error {
		op, err := mutateSee(ctx, s, "approveCapturedPattern", "pattern "+id, func(ctx context.Context, key *string) (*http.Response, error) {
			return s.api.ApproveCapturedPattern(ctx, id, &gen.ApproveCapturedPatternParams{ApiVersion: APIVersion, IdempotencyKey: key})
		}, wrap(gen.ParseApproveCapturedPatternResponse, func(r *gen.ApproveCapturedPatternResponse) approveCapturedPattern {
			return approveCapturedPattern{r}
		}))
		if err != nil {
			return err
		}
		if op, err = s.finish(ctx, op); err != nil {
			return err
		}
		return s.print("%s is approved (operation %s).\n", esc(patternOf(op)), op.Id)
	}, nil
}

// ---- withdraw-pattern ----

func parseWithdrawPattern(args []string) (func(context.Context, *session) error, error) {
	if len(args) != 1 {
		return nil, usagef("withdraw-pattern: want exactly one captured pattern")
	}
	id := args[0]
	if err := needPattern("withdraw-pattern", id); err != nil {
		return nil, err
	}
	return func(ctx context.Context, s *session) error {
		op, err := mutateSee(ctx, s, "withdrawCapturedPattern", "pattern "+id, func(ctx context.Context, key *string) (*http.Response, error) {
			return s.api.WithdrawCapturedPattern(ctx, id, &gen.WithdrawCapturedPatternParams{ApiVersion: APIVersion, IdempotencyKey: key})
		}, wrap(gen.ParseWithdrawCapturedPatternResponse, func(r *gen.WithdrawCapturedPatternResponse) withdrawCapturedPattern {
			return withdrawCapturedPattern{r}
		}))
		if err != nil {
			return err
		}
		if op, err = s.finish(ctx, op); err != nil {
			return err
		}
		return s.print("%s is withdrawn: no new run uses it (operation %s).\n", esc(patternOf(op)), op.Id)
	}, nil
}

// ---- erase-pattern ----

// parseErasePattern reads the captured pattern, shows what its erasure removes, and erases it only
// when the developer types its ID exactly; any other answer, or none, erases nothing.
func parseErasePattern(args []string) (func(context.Context, *session) error, error) {
	if len(args) != 1 {
		return nil, usagef("erase-pattern: want exactly one captured pattern")
	}
	id := args[0]
	if err := needPattern("erase-pattern", id); err != nil {
		return nil, err
	}
	return func(ctx context.Context, s *session) error {
		cp, err := s.getCapturedPattern(ctx, id)
		if err != nil {
			return err
		}
		p := &printer{w: s.out}
		p.f("This erases the captured pattern %s (%s):\n", esc(cp.Id), esc(string(cp.State)))
		p.f("  its template artifacts (tests %s, code %s, witnesses %s), unless a slice or another captured pattern also holds them\n",
			esc(cp.Template.Tests), esc(cp.Template.Code), esc(cp.Template.Witnesses))
		if cp.ProofSlice != nil && *cp.ProofSlice != "" {
			p.f("  its proof slice %s and that slice's runs:\n", esc(*cp.ProofSlice))
			if len(cp.Proofs) == 0 {
				p.f("    none\n")
			}
			for _, pr := range cp.Proofs {
				p.f("    run %s (proof %s)\n", esc(pr.Run), proofOutcome(pr))
			}
		} else {
			p.f("  no proof slice\n")
		}
		p.f("Runs of other slices built from it keep their records. Its ID is never given again.\n")
		p.f("Type the captured pattern's ID (%s) to erase it: ", esc(id))
		if err := p.done(); err != nil {
			return err
		}
		if answer, _ := s.readLine(); answer != id {
			_, _ = fmt.Fprintln(s.out)
			return failf(exitFailed, "erase-pattern: the answer was not %s; nothing was erased", id)
		}
		op, err := mutateSee(ctx, s, "eraseCapturedPattern", "patterns", func(ctx context.Context, key *string) (*http.Response, error) {
			return s.api.EraseCapturedPattern(ctx, id, &gen.EraseCapturedPatternParams{ApiVersion: APIVersion, IdempotencyKey: key})
		}, wrap(gen.ParseEraseCapturedPatternResponse, func(r *gen.EraseCapturedPatternResponse) eraseCapturedPattern {
			return eraseCapturedPattern{r}
		}))
		if err != nil {
			return err
		}
		if op, err = s.finish(ctx, op); err != nil {
			return err
		}
		return s.print("Erased %s (operation %s).\n", esc(id), op.Id)
	}, nil
}

// ---- the proof outcome line ----

// proofLine prints, after a proof's end line, how its proof ended, read from the captured pattern
// the run proves, and returns the exit code: 1 for a failed proof, else the run state's code
// (contract.md, "Run states and exit codes").
func (s *session) proofLine(ctx context.Context, run *gen.Run, code int) (int, error) {
	id, _, _ := strings.Cut(run.Pattern, "@")
	if !patternID.MatchString(id) {
		return 0, failf(exitFailed, "engine: the proof run %s names no captured pattern (pattern %q)", run.Id, esc(run.Pattern))
	}
	cp, err := s.getCapturedPattern(ctx, id)
	if err != nil {
		return 0, err
	}
	for i := len(cp.Proofs) - 1; i >= 0; i-- {
		if p := cp.Proofs[i]; p.Run == run.Id {
			if err := s.print("Proof of %s: %s\n", esc(id), proofOutcome(p)); err != nil {
				return 0, err
			}
			if p.Outcome == gen.ProofOutcomeFailed {
				return exitFailed, nil
			}
			return code, nil
		}
	}
	return 0, failf(exitFailed, "engine: the captured pattern %s has no proof of %s", esc(id), run.Id)
}
