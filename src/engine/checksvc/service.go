package checksvc

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/agentic-platform/check/api/gen"
	"github.com/agentic-platform/engine/canonical"
	"github.com/agentic-platform/engine/checks"
	"github.com/agentic-platform/engine/procedures"
)

// service implements the operations. It keeps nothing between calls, except the project
// worker's record of each project's last commit (history.go), on disk in Config.History.
type service struct {
	cfg    Config
	limits limits
	// locks holds one lock per project name, so two calls of the project worker never change one
	// project at once.
	locks sync.Map
}

var _ gen.StrictServerInterface = (*service)(nil)

// reasonMax is the most characters of a verdict's or a procedure's reason.
const reasonMax = 65536

// bounded runs work within the call's time limit, with the call's slot taken over from the front
// and released when work has ended. A work still running at the limit has its context cancelled,
// which kills every process it started, and the call answers 504 TimeLimitExceeded.
func bounded[T any](ctx context.Context, seconds int, work func(ctx context.Context) (T, error)) (T, error) {
	sl, _ := ctx.Value(slotKey{}).(*slot)
	if sl != nil {
		sl.taken.Store(true)
	}
	limit := time.Duration(seconds) * time.Second
	wctx, cancel := context.WithTimeout(ctx, limit)
	type result struct {
		v   T
		err error
	}
	done := make(chan result, 1)
	go func() {
		defer func() {
			if sl != nil {
				sl.release()
			}
		}()
		defer cancel()
		v, err := work(wctx)
		done <- result{v, err}
	}()
	var zero T
	select {
	case r := <-done:
		if errors.Is(wctx.Err(), context.DeadlineExceeded) {
			return zero, timeLimit(seconds)
		}
		return r.v, r.err
	case <-wctx.Done():
		if !errors.Is(wctx.Err(), context.DeadlineExceeded) {
			// The work ended (its cancel ran) or the caller went away, which ends the work.
			r := <-done
			return r.v, r.err
		}
		// Give the killed processes a moment to be reaped, so a 504 means they are gone.
		select {
		case <-done:
		case <-time.After(10 * time.Second):
		}
		return zero, timeLimit(seconds)
	}
}

func timeLimit(seconds int) error {
	return fault(504, gen.ErrorCodeTimeLimitExceeded, fmt.Sprintf("the call did not finish within %d seconds; it was stopped with every process it started", seconds))
}

// scrub makes a reason name project paths only (checks.HidePaths), and at most reasonMax
// characters.
func (s *service) scrub(reason, dir string) string {
	if dir != "" {
		reason = checks.HidePaths(reason, dir)
	}
	if utf8.RuneCountInString(reason) > reasonMax {
		r := []rune(reason)
		reason = string(r[:reasonMax-3]) + "..."
	}
	if reason == "" {
		reason = "no reason given"
	}
	return reason
}

// ---- runCheck ----

func (s *service) RunCheck(ctx context.Context, req gen.RunCheckRequestObject) (gen.RunCheckResponseObject, error) {
	name := req.CheckName
	impl, served := checkByName[name]
	if s.cfg.Procedure != "" || s.cfg.History != "" || !served {
		why := "this deployment serves no check " + name
		if name == contractLintPasses {
			why += " (the engine judges it with its contract-lint service)"
		}
		return nil, fault(404, gen.ErrorCodeUnknownCheck, why)
	}
	body := *req.Body
	for _, a := range body.Approved {
		if sum(a.Content) != a.HashValue {
			return nil, fault(422, gen.ErrorCodeInputHashMismatch, "the approved "+string(a.Part)+" part's content does not have its sha256")
		}
	}
	return bounded(ctx, body.TimeLimitSeconds, func(ctx context.Context) (gen.RunCheckResponseObject, error) {
		dir, err := s.projectDir(body.Project)
		if err != nil {
			return nil, err
		}
		p, err := readProject(ctx, dir, body.Project, s.limits)
		if err != nil {
			return nil, err
		}
		v := gen.CheckVerdict{Check: name, Refs: []gen.Ref{}, ProjectFiles: p.list()}
		var results []checks.CheckResult
		if len(p.bad) > 0 {
			results = []checks.CheckResult{{Check: name, Reason: "the project holds entries no check reads: " + strings.Join(p.bad, "; ")}}
		} else if faults := s.binding(p, body); len(faults) > 0 {
			results = []checks.CheckResult{{Check: name, Reason: strings.Join(faults, "; ")}}
		} else {
			c := &call{dir: dir, req: body, project: p, before: beforeTree(body.Before), runner: s.runner(ctx)}
			results = impl(c)
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		v.Passed, v.Reason = verdict(name, results)
		v.Reason = s.scrub(v.Reason, dir)
		return gen.RunCheck200JSONResponse(v), nil
	})
}

// verdict joins a check's results: passed only when there is a result and every one passed, the
// reason the failing results' reasons, or the passing ones', joined by "; ".
func verdict(name string, results []checks.CheckResult) (bool, string) {
	if len(results) == 0 {
		return false, name + " gave no result, and a check never passes by default"
	}
	var failed, passed []string
	for _, r := range results {
		if r.Passed {
			passed = append(passed, r.Reason)
		} else {
			failed = append(failed, r.Reason)
		}
	}
	if len(failed) > 0 {
		return false, strings.Join(failed, "; ")
	}
	return true, strings.Join(passed, "; ")
}

// binding names each output and each earlier step's output given as an input that does not
// concern the files the engine stored (Principle XX; research, "Binding verdicts to stored
// files"). Every output's framed hash is recomputed from the folder and must equal the request's.
// An input is bound through `before`: each of its files in the folder equals its entry in
// `before`, by path and hash, none missing or added, except a path the judged step may write;
// and when no step after its producer (in earlier_steps' order) nor the judged step may write any
// of its paths, its framed hash is also recomputed and must equal the request's. The review
// step's findings, an output of a step with no writable entries and a slice/<part> input are not
// sets of files.
func (s *service) binding(p *project, body gen.CheckRequest) []string {
	var faults []string
	st := body.Step
	for _, o := range body.Outputs {
		entries, ok := outputEntries(st.Outputs, st.Writable, o.Name)
		switch {
		case !ok:
			faults = append(faults, "the output "+o.Name+" is not one the step "+st.Name+" declares")
		case o.Name == findingsOutput || len(st.Writable) == 0:
		case canonical.FramedHash(p.matching(entries)) != o.HashValue:
			faults = append(faults, fmt.Sprintf("the output %s in the folder hashes to %s, not %s", o.Name, canonical.FramedHash(p.matching(entries)), o.HashValue))
		}
	}
	before := beforeTree(body.Before)
	for _, in := range body.Inputs {
		if strings.HasPrefix(in.Name, "slice/") {
			continue
		}
		found := false
		for i, e := range body.EarlierSteps {
			entries, ok := outputEntries(e.Outputs, e.Writable, in.Name)
			if !ok {
				continue
			}
			found = true
			if in.Name != findingsOutput && len(e.Writable) > 0 {
				faults = append(faults, bindInput(p, before, in, entries, st.Writable, laterWritable(body.EarlierSteps[i+1:], st.Writable))...)
			}
			break
		}
		if !found {
			faults = append(faults, "the input "+in.Name+" is no earlier step's output")
		}
	}
	return faults
}

// laterWritable is the writable entries of the steps after an input's producer and of the judged
// step.
func laterWritable(later []gen.EarlierStep, judged []string) []string {
	out := append([]string{}, judged...)
	for _, e := range later {
		out = append(out, e.Writable...)
	}
	return out
}

// bindInput binds one input whose files match entries: through before, except the paths the
// judged step may write, and by its framed hash when no path of it is in later.
func bindInput(p *project, before checks.Tree, in gen.ArtifactRef, entries, judged, later []string) []string {
	var changed []string
	written := false
	paths := map[string]bool{}
	for path := range before {
		if checks.MatchGlob(entries, path) {
			paths[path] = true
		}
	}
	for path := range p.files {
		if checks.MatchGlob(entries, path) {
			paths[path] = true
		}
	}
	for _, path := range sortedSet(paths) {
		if checks.MatchGlob(later, path) {
			written = true
		}
		if checks.MatchGlob(judged, path) {
			continue
		}
		want, was := before[path]
		content, is := p.files[path]
		switch {
		case was && !is:
			changed = append(changed, path+" is missing")
		case !was && is:
			changed = append(changed, path+" was added")
		case was && is && sum(content) != want:
			changed = append(changed, path+" was changed")
		}
	}
	var faults []string
	if len(changed) > 0 {
		faults = append(faults, fmt.Sprintf("the input %s differs from before: %s", in.Name, strings.Join(changed, ", ")))
	}
	if !written {
		if h := canonical.FramedHash(p.matching(entries)); h != in.HashValue {
			faults = append(faults, fmt.Sprintf("the input %s in the folder hashes to %s, not %s", in.Name, h, in.HashValue))
		}
	}
	return faults
}

func sortedSet(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func beforeTree(before gen.ProjectFiles) checks.Tree {
	t := checks.Tree{}
	for _, f := range before {
		t[f.Path] = f.HashValue
	}
	return t
}

// ---- readStepFiles ----

func (s *service) ReadStepFiles(ctx context.Context, req gen.ReadStepFilesRequestObject) (gen.ReadStepFilesResponseObject, error) {
	if s.cfg.Procedure != "" || s.cfg.History != "" {
		return nil, fault(404, gen.ErrorCodeUnknownStep, "this deployment does not serve readStepFiles")
	}
	body := *req.Body
	return bounded(ctx, body.TimeLimitSeconds, func(ctx context.Context) (gen.ReadStepFilesResponseObject, error) {
		dir, err := s.projectDir(body.Project)
		if err != nil {
			return nil, err
		}
		p, err := readProject(ctx, dir, body.Project, s.limits)
		if err != nil {
			return nil, err
		}
		for _, path := range p.large {
			if checks.MatchGlob(body.Step.Writable, path) {
				return nil, fault(422, gen.ErrorCodeStepFilesTooLarge, fmt.Sprintf("the step's file %s alone is larger than %d bytes", path, s.limits.ProjectFileMax))
			}
		}
		if err := p.strict(body.Project); err != nil {
			return nil, err
		}
		files := p.matching(body.Step.Writable)
		var size int64
		out := []gen.FileContent{}
		for _, f := range files {
			size += int64(len(f.Content))
			out = append(out, gen.FileContent{Path: f.Path, HashValue: sum(f.Content), Content: f.Content})
		}
		if size > s.limits.StepFilesMax {
			return nil, fault(422, gen.ErrorCodeStepFilesTooLarge, fmt.Sprintf("the step's files hold %d bytes, more than %d", size, s.limits.StepFilesMax))
		}
		removed := []gen.ProjectPath{}
		for _, f := range body.Before {
			if _, still := p.files[f.Path]; !still && checks.MatchGlob(body.Step.Writable, f.Path) {
				removed = append(removed, f.Path)
			}
		}
		return gen.ReadStepFiles200JSONResponse(gen.StepFiles{HashValue: canonical.FramedHash(files), Files: out, Removed: removed, ProjectFiles: p.list()}), nil
	})
}

// ---- runProcedure ----

func (s *service) RunProcedure(ctx context.Context, req gen.RunProcedureRequestObject) (gen.RunProcedureResponseObject, error) {
	name := req.ProcedureName
	body := *req.Body
	switch {
	case s.cfg.Procedure == "":
		return nil, fault(404, gen.ErrorCodeUnknownProcedure, "this deployment serves no procedure")
	case name != s.cfg.Procedure:
		return nil, fault(404, gen.ErrorCodeUnknownProcedure, "this worker serves the procedure "+s.cfg.Procedure+" only, not "+name)
	case body.Step.Name != s.cfg.Step:
		return nil, fault(404, gen.ErrorCodeUnknownProcedure, "this worker serves the step "+s.cfg.Step+" only, not "+body.Step.Name)
	}
	run, known := procedureByName[name]
	if !known {
		return nil, fault(404, gen.ErrorCodeUnknownProcedure, "no procedure "+name+" exists")
	}
	var inputs []procedures.Input
	for _, in := range body.Inputs {
		if sum(in.Content) != in.HashValue {
			return nil, fault(422, gen.ErrorCodeInputHashMismatch, "the input "+in.Name+"'s content does not have its sha256")
		}
		inputs = append(inputs, procedures.Input{Name: in.Name, SHA256: in.HashValue, Content: in.Content})
	}
	return bounded(ctx, body.TimeLimitSeconds, func(ctx context.Context) (gen.RunProcedureResponseObject, error) {
		dir, err := s.projectDir(body.Project)
		if err != nil {
			return nil, err
		}
		p, err := readProject(ctx, dir, body.Project, s.limits)
		if err != nil {
			return nil, err
		}
		if err := p.strict(body.Project); err != nil {
			return nil, err
		}
		res, err := run(ctx, s, inputs)
		switch {
		case ctx.Err() != nil:
			return nil, ctx.Err()
		case errors.Is(err, procedures.ErrInputHashMismatch):
			return nil, fault(422, gen.ErrorCodeInputHashMismatch, s.scrub(err.Error(), dir))
		case err != nil:
			res = procedures.Result{Reason: "the procedure could not run: " + err.Error()}
		}
		outs, files, why := s.takeOutputs(body.Step, res)
		result := gen.ProcedureResult{Procedure: name, Outputs: []gen.ProcedureOutput{}}
		if why != "" {
			result.Reason = s.scrub(why, dir)
			result.ProjectFiles = p.list()
			return gen.RunProcedure200JSONResponse(result), nil
		}
		// Writing starts only within the time limit, and stops before the next file once it has
		// passed; the 504 then leaves the writable paths partly written, and a repeat converges
		// them (contract, runProcedure).
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if path, err := writeOutputs(ctx, p, body.Step.Writable, files); ctx.Err() != nil {
			return nil, ctx.Err()
		} else if err != nil {
			return nil, fault(500, gen.ErrorCodeWriteFailed, "writing or removing "+path+" failed; the step's writable paths may hold partial files, and a repeat converges them")
		}
		after, err := readProject(ctx, dir, body.Project, s.limits)
		if err != nil {
			return nil, err
		}
		result.Completed, result.Reason, result.Outputs, result.ProjectFiles = true, s.scrub(res.Reason, dir), outs, after.list()
		return gen.RunProcedure200JSONResponse(result), nil
	})
}

// takeOutputs checks a completed procedure's outputs against the step: one per declared output in
// its order, each file's path a ProjectPath matching that output's writable entries, no path in
// two outputs, at most `x-limits.procedure-output-max-bytes` in all. It returns them in the
// contract's form with every file, or why the procedure did not complete.
func (s *service) takeOutputs(st gen.StepFacts, res procedures.Result) ([]gen.ProcedureOutput, []canonical.File, string) {
	if !res.Completed {
		return nil, nil, res.Reason
	}
	if len(res.Outputs) != len(st.Outputs) {
		return nil, nil, fmt.Sprintf("the procedure produced %d outputs, the step declares %d", len(res.Outputs), len(st.Outputs))
	}
	var outs []gen.ProcedureOutput
	var all []canonical.File
	seen := map[string]bool{}
	var size int64
	for i, o := range res.Outputs {
		if o.Name != st.Outputs[i] {
			return nil, nil, fmt.Sprintf("the procedure's output %d is %s, the step declares %s", i+1, o.Name, st.Outputs[i])
		}
		entries, _ := outputEntries(st.Outputs, st.Writable, o.Name)
		out := gen.ProcedureOutput{Name: o.Name, HashValue: canonical.FramedHash(o.Files), Files: []gen.FileContent{}}
		for _, f := range o.Files {
			if !procedures.IsProjectPath(f.Path) || !checks.MatchGlob(entries, f.Path) || seen[f.Path] {
				return nil, nil, fmt.Sprintf("the procedure's file %q is not a project path of the output %s's writable entries, or is in two outputs", f.Path, o.Name)
			}
			seen[f.Path] = true
			size += int64(len(f.Content))
			out.Files = append(out.Files, gen.FileContent{Path: f.Path, HashValue: sum(f.Content), Content: f.Content})
			all = append(all, f)
		}
		outs = append(outs, out)
	}
	if size > s.limits.ProcedureOutMax {
		return nil, nil, fmt.Sprintf("the procedure's files hold %d bytes, more than %d; nothing was written", size, s.limits.ProcedureOutMax)
	}
	return outs, all, ""
}

// procedureByName maps each fixed procedure's name (a step's worker `procedure:<name>`) to its
// implementation in engine/procedures.
var procedureByName = map[string]func(ctx context.Context, s *service, in []procedures.Input) (procedures.Result, error){
	"contract-check": func(ctx context.Context, _ *service, in []procedures.Input) (procedures.Result, error) {
		return procedures.ContractCheck(ctx, in)
	},
	"generate": func(ctx context.Context, s *service, in []procedures.Input) (procedures.Result, error) {
		return procedures.Generate(ctx, procedures.GenerateConfig{OapiCodegen: s.cfg.OapiCodegen, Template: s.cfg.Template, Env: s.env()}, in)
	},
}

// ---- running go ----

// env is the environment of every command a check or procedure runs, never the process's own
// (which holds CHECK_TOKEN and the other settings): checks.CommandEnv of Config.Env with the
// oapi-codegen binary's folder first on PATH (GenerationRepeatable runs it through go generate).
// No CHECK_* variable and no entry holding the token is passed, whatever Config.Env says.
func (s *service) env() []string {
	extra := append([]string{}, s.cfg.Env...)
	if s.cfg.OapiCodegen != "" {
		path := os.Getenv("PATH")
		for _, e := range extra {
			if v, ok := strings.CutPrefix(e, "PATH="); ok {
				path = v
			}
		}
		extra = append(extra, "PATH="+filepath.Dir(s.cfg.OapiCodegen)+string(os.PathListSeparator)+path)
	}
	var env []string
	for _, e := range checks.CommandEnv(extra...) {
		if !strings.HasPrefix(e, "CHECK_") && !strings.Contains(e, s.cfg.Token) {
			env = append(env, e)
		}
	}
	return env
}

// runner is the go toolchain for one call: offline, outside any workspace, the local toolchain
// only, each command in its own process group, killed whole when the call's context ends.
type runner struct {
	ctx context.Context
	env []string
}

func (s *service) runner(ctx context.Context) checks.GoRunner {
	return runner{ctx: ctx, env: s.env()}
}

func (r runner) Go(dir string, args ...string) (checks.GoResult, error) {
	cmd := exec.CommandContext(r.ctx, "go", args...)
	cmd.Dir = dir
	cmd.Env = r.env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 2 * time.Second
	out, err := cmd.CombinedOutput()
	if cmd.Process != nil {
		// Whatever the command left running in its group (a test binary's children) ends with it.
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	if r.ctx.Err() != nil {
		return checks.GoResult{}, fmt.Errorf("stopped: %w", r.ctx.Err())
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return checks.GoResult{ExitCode: exit.ExitCode(), Output: string(out)}, nil
	}
	if err != nil {
		return checks.GoResult{}, err
	}
	return checks.GoResult{Output: string(out)}, nil
}
