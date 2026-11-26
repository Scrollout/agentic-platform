//go:build docker

package acceptance

// T014 (feature 011, US2; spec User Story 2, FR-008 to FR-010, SC-001, SC-004; quickstart 3;
// contracts/captured-patterns.md; src/cli/contract.md, `build`'s first line, the end lines and
// "Run states and exit codes"): a slice that equals a captured pattern's source except for its
// parameters is built by `ap build` with no model call.
//
// One lab, one source build with the scripted step model (a copy of the recorded `happy` set, as
// captured_prove_test.go does: the scripted model writes the exchanges it answers into its set), and
// the cases in order, each using what the ones before it made:
//
//	1. the `tasks` slice is created and its two questions answered, before the window below opens
//	2. the notes slice is built until done; its run is captured (parameters note and notes)
//	3. the proof builds the proof slice into a project; it passes
//	4. the captured pattern is approved
//	5. the `tasks` slice's parts (its own, with note/notes replaced) are replaced and approved
//	   through the slice API, which calls no analyst
//	6. `ap build` of the `tasks` slice uses the captured pattern, fills the files, ends done
//	7. two more builds give the same file hashes as the first (SC-004)
//	8. the model gateway's request journal and the scripted model's exchanges hold no request that
//	   began after the capture (SC-001)
//	9. `ap build --pattern api-feature-go` runs the agents (FR-010)
//
// The window of case 8 opens just before the capture request and closes when the third build has
// ended; the proof (case 3) is inside it, so a proof that called a model fails it too. Cases 1 and
// 2 call the analyst and the step model, before it opens. Both readers are shown to work: the
// journal and the exchanges list what happened before the window (not empty), and case 9 shows the
// readers seeing the agent's request.
//
// The `tasks` slice is made from the analyst's existing recordings (`analyst-example-notes`, the
// quickstart's example, and `analyst-notes-answered`, answered as US1 answers it), then its parts are
// replaced. A recording for a description about tasks would need a request body only a lab
// run can show (src/tests/recorded/README.md), so none is made here, and no test data folder is
// needed (see the report).
//
// Edge cases covered: the order of the runs (the source run, the proof run and the tasks runs are
// never active together: each is waited for, as RunActive would refuse otherwise); every captured
// build runs within a limit and fails the test when over it; the window's start is taken before the
// capture request, not after it, and the journal's start point is the set of request IDs seen (not a
// count: the journal keeps 100 entries and drops the oldest, which cannot hide a new one); the
// scripted exchanges are compared by name; a build whose step results are reused from an earlier
// run (ResultReused) instead of filled is accepted as no model call, and the record is judged
// as a whole; the base-pattern run is cancelled after the agent's first request (it would
// otherwise end handed over, as the scripted answers are the notes slice's); the file hashes leave
// out the version history, whose commits carry times.
// Left out: a rotated gateway log (the journal is WireMock's, read inside its container); a lab
// restart; the refusals of a named captured pattern (T013's engine tests); a slice that does not
// fit (T012's tests); whether builds 2 and 3 fill or reuse the first build's results (the
// contract does not say).
//
// Expected to fail first before T015: case 6 (`ap build` of the `tasks` slice): `startRun` has no
// choice of a captured pattern yet, so the run uses the base pattern, the first line carries
// `with api-feature-go@1.2.0 (base)`, not the captured pattern, and with no step workers the
// build ends WorkersNotReady. Cases 1 to 5 pass before it (they are US1's behaviour); cases 7 and
// 8 fail after it for want of the build; case 9 needs the `--pattern` flag of `build`, also T015's.
// Expected time: lab with images ~5 min, source build ~10 min, the proof 3 to 10 min, the `tasks`
// slice 1 min, three captured builds 1 to 3 min each, the base-pattern run ~3 min; ~35 min.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentic-platform/tests/lab"
)

// capParameter and capPattern are a captured pattern as far as this test reads it.
type capParameter struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type capPattern struct {
	ID         string         `json:"id"`
	State      string         `json:"state"`
	Usable     bool           `json:"usable"`
	Parameters []capParameter `json:"parameters"`
}

// capRun is a run as far as this test reads it (the contract's Run).
type capRun struct {
	ID            string            `json:"id"`
	Slice         string            `json:"slice"`
	Pattern       string            `json:"pattern"`
	PatternReason string            `json:"pattern_reason"`
	Parameters    map[string]string `json:"parameters"`
	SourceRun     string            `json:"source_run"`
	NotUsed       []json.RawMessage `json:"not_used"`
	Project       string            `json:"project"`
	Status        string            `json:"status"`
	Outcome       string            `json:"outcome"`
	Reason        string            `json:"reason"`
}

func capRunOf(t testing.TB, l *lab.Lab, id string) capRun {
	t.Helper()
	var r capRun
	if st := cpvGet(t, l, "/runs/"+id, &r); st != 200 {
		t.Fatalf("getRun %s = %d", id, st)
	}
	return r
}

// capNeed stops a case whose earlier case failed: its inputs are not there.
func capNeed(t testing.TB, ok bool, what string) {
	t.Helper()
	if !ok {
		t.Fatalf("an earlier case failed: %s", what)
	}
}

// ---- the case forms of a value (spec Clarifications, FR-003, FR-009) --------------------------

// capSubst replaces each source value in its three case forms (lower, first letter upper, upper)
// by the new value in the same form, longest source form first, in one pass.
type capSubst struct {
	to map[string]string
	re *regexp.Regexp
}

func capForms(s string) []string {
	return []string{s, strings.ToUpper(s[:1]) + s[1:], strings.ToUpper(s)}
}

func newCapSubst(values map[string]string) *capSubst {
	s := &capSubst{to: map[string]string{}}
	var from []string
	for src, dst := range values {
		sf, df := capForms(src), capForms(dst)
		for i := range sf {
			s.to[sf[i]] = df[i]
			from = append(from, sf[i])
		}
	}
	sort.Slice(from, func(i, j int) bool {
		if len(from[i]) != len(from[j]) {
			return len(from[i]) > len(from[j])
		}
		return from[i] < from[j]
	})
	for i := range from {
		from[i] = regexp.QuoteMeta(from[i])
	}
	s.re = regexp.MustCompile(strings.Join(from, "|"))
	return s
}

func (s *capSubst) text(in string) string {
	return s.re.ReplaceAllStringFunc(in, func(m string) string { return s.to[m] })
}

// value replaces in every string of a decoded JSON value (not in its keys).
func (s *capSubst) value(v any) any {
	switch x := v.(type) {
	case string:
		return s.text(x)
	case []any:
		out := make([]any, len(x))
		for i := range x {
			out[i] = s.value(x[i])
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = s.value(e)
		}
		return out
	}
	return v
}

// capBody is a slice-part replace request with the values replaced.
func capBody(t testing.TB, s *capSubst, body any) any {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	return s.value(v)
}

// ---- the models' request records (SC-001) -------------------------------------------------------

// capLog is what the two model servers had seen at one moment: the replay server (the gateway the
// analyst uses) by its journal's request IDs, the scripted server (the step agents') by the names
// of the exchanges it wrote before answering.
type capLog struct {
	gateway   map[string]string // request ID -> "METHOD url"
	exchanges map[string]bool
}

func capContainer(t testing.TB, l *lab.Lab, filter ...string) string {
	t.Helper()
	a := append([]string{"ps", "-q", "--filter", "network=" + l.Net}, filter...)
	out, code := lab.Run("docker", a...)
	ids := strings.Fields(out)
	if code != 0 || len(ids) != 1 {
		t.Fatalf("docker %v: exit %d, %d containers (%q), want exactly one", a, code, len(ids), out)
	}
	return ids[0]
}

func capSeen(t testing.TB, l *lab.Lab) capLog {
	t.Helper()
	gw := capContainer(t, l, "--filter", "label=ap.replay=1")
	out, code := lab.Run("docker", "exec", gw, "sh", "-c",
		`set -- $WIREMOCK_OPTIONS; exec wget -qO- "http://$2@127.0.0.1:8080/__admin/requests"`)
	var j struct {
		Requests []struct {
			ID      string `json:"id"`
			Request struct {
				Method string `json:"method"`
				URL    string `json:"url"`
			} `json:"request"`
		} `json:"requests"`
	}
	if code != 0 || json.Unmarshal([]byte(out), &j) != nil {
		t.Fatalf("the gateway's request journal: exit %d: %.300s", code, out)
	}
	seen := capLog{gateway: map[string]string{}, exchanges: map[string]bool{}}
	for _, r := range j.Requests {
		seen.gateway[r.ID] = r.Request.Method + " " + r.Request.URL
	}
	sc := capContainer(t, l, "--filter", "name=aptest-lab-scripted")
	out, code = lab.Run("docker", "exec", sc, "ls", "/out")
	if code != 0 {
		t.Fatalf("the scripted model's exchanges: exit %d: %.300s", code, out)
	}
	for _, n := range strings.Fields(out) {
		seen.exchanges[n] = true
	}
	return seen
}

// capNewSince lists what is in now and not in before.
func capNewSince(before, now capLog) []string {
	var added []string
	for id, r := range now.gateway {
		if _, ok := before.gateway[id]; !ok {
			added = append(added, "gateway "+r)
		}
	}
	for n := range now.exchanges {
		if !before.exchanges[n] {
			added = append(added, "step model "+n)
		}
	}
	sort.Strings(added)
	return added
}

// ---- the terminal client ------------------------------------------------------------------------

// capCLI is a client invocation that has started and is stopped (SIGINT, which detaches a build and
// leaves its run going) when it is over its limit.
type capCLI struct {
	cmd      *exec.Cmd
	out, err bytes.Buffer
	timer    *time.Timer
	timedOut bool
	mu       sync.Mutex
}

func capStart(t testing.TB, l *lab.Lab, bin string, limit time.Duration, args ...string) *capCLI {
	t.Helper()
	c := &capCLI{cmd: cpvCmd(t, l, bin, args...)}
	c.cmd.Stdout, c.cmd.Stderr = &c.out, &c.err
	if err := c.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	c.timer = time.AfterFunc(limit, func() {
		c.mu.Lock()
		c.timedOut = true
		c.mu.Unlock()
		_ = c.cmd.Process.Signal(os.Interrupt)
	})
	return c
}

// wait returns standard output, standard error and the exit code; a client over its limit fails the test.
func (c *capCLI) wait(t testing.TB, what string, limit time.Duration) (string, string, int) {
	t.Helper()
	err := c.cmd.Wait()
	c.timer.Stop()
	c.mu.Lock()
	over := c.timedOut
	c.mu.Unlock()
	if over {
		t.Fatalf("%s did not end within %s; output so far:\n%s%s", what, limit, c.out.String(), c.err.String())
	}
	return c.out.String(), c.err.String(), cpvExit(t, err)
}

var (
	capStarted = regexp.MustCompile(`^(RUN-[0-9]+) started: building (\S+) into (\S+)( with (\S+) \(([a-z]+)\))? \(operation OP-[0-9]+\)$`)
	capDone    = regexp.MustCompile(`^RUN-[0-9]+ ended done$`)
)

// capBuild is `ap build <slice> <project> [args]` to its end; it returns the run's ID, the lines
// printed and the exit code. The first line is checked against the contract's form here.
func capBuild(t *testing.T, l *lab.Lab, bin, slice, project string, limit time.Duration) (run string, lines []string, code int) {
	t.Helper()
	c := capStart(t, l, bin, limit, "build", slice, project)
	out, errOut, code := c.wait(t, "build "+slice+" "+project, limit)
	lines = cpvLines(out)
	if len(lines) == 0 {
		t.Fatalf("build %s %s printed nothing on standard output (exit %d): %s", slice, project, code, errOut)
	}
	m := capStarted.FindStringSubmatch(lines[0])
	if m == nil {
		t.Fatalf("build %s %s: the first line is %q, want `RUN-<n> started: building <slice> into <project> [with <pattern> (<reason>)] (operation OP-<m>)`; exit %d: %s", slice, project, lines[0], code, errOut)
	}
	return m[1], lines, code
}

// ---- the project's files ------------------------------------------------------------------------

// capProjectHashes is the SHA-256 of every file of a project folder by its path, the version
// history (.git) left out: its commits carry times.
func capProjectHashes(t testing.TB, l *lab.Lab, project string) map[string]string {
	t.Helper()
	root := filepath.Join(l.ProjectsDir(), project)
	hashes := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if !d.Type().IsRegular() {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		sum := sha256.Sum256(b)
		hashes[filepath.ToSlash(rel)] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatalf("reading the project %s: %v", project, err)
	}
	return hashes
}

// capDiff names the paths whose hashes differ between two projects.
func capDiff(a, b map[string]string) []string {
	var d []string
	for p, h := range a {
		if b[p] != h {
			d = append(d, p)
		}
	}
	for p := range b {
		if _, ok := a[p]; !ok {
			d = append(d, p)
		}
	}
	sort.Strings(d)
	return d
}

func TestCapturedSliceIsBuiltWithNoModel(t *testing.T) {
	// The scripted step model over a copy of `happy`, as captured_prove_test.go sets it up.
	set := t.TempDir()
	happy := filepath.Join(lab.Root(t), "src", "tests", "recorded", "steps", "happy")
	for _, c := range []struct{ from, to string }{{happy, set}, {filepath.Join(happy, "files"), filepath.Join(set, "files")}} {
		if err := cpvCopyFiles(c.from, c.to); err != nil {
			t.Fatal(err)
		}
	}
	requirePrerequisites(t)
	t.Setenv("AP_LAB_MODEL", "scripted")
	bin := buildCLI(t)
	l := lab.Start(t, lab.Build(), lab.Steps(set))

	const (
		notesProject = "notes"
		proofProject = "proof"
		buildLimit   = 15 * time.Minute // a captured build runs checks only
	)
	var (
		tasksSlice string // the `tasks` slice
		sourceRun  string
		captured   capPattern
		window     capLog // what the models had seen when the capture began
		subst      *capSubst
		pattern    string // `<id>@1.0.0`
	)

	// T014: "create the `tasks` slice first, from a recorded analyst answer". First, so the analyst
	// is not called inside the window (SC-001). Its parts are replaced in case 5.
	t.Run("1 the tasks slice is created and answered from the recorded analyst answers", func(t *testing.T) {
		tasksSlice = l.Create(t, lab.Example)
		answerExample(t, l, tasksSlice)
		if d := l.Get(t, tasksSlice); len(d.Goals) == 0 || len(d.Rules) == 0 || d.Contract == "" {
			t.Fatalf("the tasks slice has no parts after its answers: %+v", d)
		}
	})

	// T014: "build the notes slice from the recorded happy set until done; capture" (FR-001, FR-003).
	t.Run("2 the notes run is captured with note and notes as parameters", func(t *testing.T) {
		capNeed(t, tasksSlice != "", "the tasks slice was not made")
		notesSlice := l.Create(t, lab.Example)
		answerExample(t, l, notesSlice)
		for _, p := range []string{"goals", "rules", "contract"} {
			l.Approve(t, notesSlice, p)
		}
		l.StepWorkers(t, notesSlice, notesProject)
		sourceRun = startRun(t, l, notesSlice, notesProject).Target
		r := cpvWaitEnded(t, l, sourceRun, 30*time.Minute)
		l.StopStepWorkers(t, notesSlice, notesProject)
		if r.Status != "ended" || r.Outcome != "done" {
			t.Fatalf("the source run is %+v, want ended done", r)
		}

		// The window opens here, before the capture request; both readers must see the earlier
		// requests (the analyst's and the step agents'), or they read nothing.
		window = capSeen(t, l)
		if len(window.gateway) == 0 || len(window.exchanges) == 0 {
			t.Fatalf("before the capture the gateway's journal holds %d requests and the scripted model %d exchanges, want both to hold the earlier ones (the analyst's, the step agents'): the readers see nothing", len(window.gateway), len(window.exchanges))
		}

		op := l.Wait(t, l.Accept(t, "POST", "/captured-patterns", map[string]string{"run": sourceRun, "name": "notes-api"}))
		if op.Status != "succeeded" || op.Target == "" {
			t.Fatalf("capturing %s: %+v (error %+v)", sourceRun, op, op.Error)
		}
		if st := cpvGet(t, l, "/captured-patterns/"+op.Target, &captured); st != 200 {
			t.Fatalf("getCapturedPattern %s = %d", op.Target, st)
		}
		pattern = captured.ID + "@1.0.0"
		have := map[string]bool{}
		for _, p := range captured.Parameters {
			have[p.Value] = true
		}
		if !have["note"] || !have["notes"] {
			t.Fatalf("the captured pattern's parameters are %+v, want the values note and notes among them (quickstart 3: the notes slice's resource names)", captured.Parameters)
		}
		for _, p := range captured.Parameters {
			if p.Value != "note" && p.Value != "notes" {
				t.Logf("parameter %s=%s keeps its source value in the tasks slice", p.Name, p.Value)
			}
		}
		subst = newCapSubst(map[string]string{"note": "task", "notes": "tasks"})
	})

	// T014: "prove" (FR-004, FR-005): the proof builds the proof slice into a new project and passes.
	t.Run("3 the proof passes", func(t *testing.T) {
		capNeed(t, captured.ID != "", "the notes run was not captured")
		_, out, errOut, code := cpvProve(t, l, bin, captured.ID, proofProject, nil)
		if code != 0 {
			t.Fatalf("prove exit %d, want 0; output:\n%s\n%s", code, out, errOut)
		}
		cpvEndThenProof(t, out, `^RUN-[0-9]+ ended done$`, "Proof of "+captured.ID+": passed")
		if st := cpvGet(t, l, "/captured-patterns/"+captured.ID, &captured); st != 200 || captured.State != "proven" {
			t.Fatalf("after a passed proof the captured pattern is %d %+v, want proven", st, captured)
		}
	})

	// T014: "approve" (FR-006): by the client, `ap approve-pattern`; usable afterwards.
	t.Run("4 the captured pattern is approved and usable", func(t *testing.T) {
		capNeed(t, captured.State == "proven", "the proof did not pass")
		out, errOut, code := cpvCLI(t, l, bin, "approve-pattern", captured.ID)
		if code != 0 {
			t.Fatalf("approve-pattern exit %d, want 0; output:\n%s\n%s", code, out, errOut)
		}
		if st := cpvGet(t, l, "/captured-patterns/"+captured.ID, &captured); st != 200 || captured.State != "approved" || !captured.Usable {
			t.Fatalf("after approval the captured pattern is %d %+v, want approved and usable", st, captured)
		}
	})

	// T014: "replace and approve the tasks slice's parts (the notes slice with note/notes replaced)
	// through the slice API, which calls no analyst" (FR-008). The no-call half is case 8's.
	t.Run("5 the tasks slice's parts are replaced and approved through the slice API", func(t *testing.T) {
		capNeed(t, captured.State == "approved" && subst != nil, "the captured pattern is not approved")
		d := l.Get(t, tasksSlice)
		l.Replace(t, tasksSlice, "goals", capBody(t, subst, d.GoalsBody()))
		l.Replace(t, tasksSlice, "rules", capBody(t, subst, d.RulesBody()))
		l.Replace(t, tasksSlice, "contract", capBody(t, subst, d.ContractBody()))
		for _, p := range []string{"goals", "rules", "contract"} {
			l.Approve(t, tasksSlice, p)
		}
		after := l.Get(t, tasksSlice)
		if after.State != "approved" {
			t.Fatalf("the tasks slice is %q after its parts were replaced and approved, want approved (failed checks: %+v)", after.State, after.Failed())
		}
		if !strings.Contains(after.Contract, "/tasks") || strings.Contains(strings.ToLower(after.Contract), "note") {
			t.Errorf("the tasks slice's contract still names notes, or not tasks:\n%s", after.Contract)
		}
	})

	// The files a captured build must leave: the `happy` set's files with the values in their case
	// forms, in contents and paths (FR-009).
	wantFiles := func(t *testing.T) map[string][]byte {
		m := map[string][]byte{}
		for rel, f := range cpvTemplateFiles {
			b, err := os.ReadFile(filepath.Join(happy, "files", f))
			if err != nil {
				t.Fatal(err)
			}
			m[subst.text(rel)] = []byte(subst.text(string(b)))
		}
		return m
	}

	// capturedBuild runs `ap build` of the tasks slice into project, with the step workers up (a
	// `fill` step needs none, but the base pattern's procedure steps contract-check and generate
	// still run; they call no model; owner-approved 2026-11-26 after CI 45873468758), and checks
	// the contract's lines, the record and the files.
	capturedBuild := func(t *testing.T, project string) map[string]string {
		t.Helper()
		l.StepWorkers(t, tasksSlice, project)
		defer l.StopStepWorkers(t, tasksSlice, project)
		run, lines, code := capBuild(t, l, bin, tasksSlice, project, buildLimit)
		// FR-010, contract.md `build`: the first line names the captured pattern and why.
		m := capStarted.FindStringSubmatch(lines[0])
		if m[3] != project || m[2] != tasksSlice || m[5] != pattern || m[6] != "fits" {
			t.Errorf("the first line is %q, want it to name slice %s, project %s and `with %s (fits)`", lines[0], tasksSlice, project, pattern)
		}
		for _, ln := range lines {
			if strings.HasPrefix(ln, "Not used:") {
				t.Errorf("a `Not used:` line %q: the captured pattern is usable and fits, nothing was left out", ln)
			}
		}
		// contract.md "End lines" and "Run states and exit codes": ended done, exit 0.
		if last := lines[len(lines)-1]; code != 0 || !capDone.MatchString(last) {
			t.Fatalf("build into %s: exit %d, last line %q, want exit 0 and `RUN-<n> ended done`; output:\n%s", project, code, last, strings.Join(lines, "\n"))
		}
		r := capRunOf(t, l, run)
		if r.Status != "ended" || r.Outcome != "done" || r.Pattern != pattern || r.PatternReason != "fits" || r.SourceRun != sourceRun {
			t.Errorf("the run is %+v, want ended done with pattern %s, reason fits and source_run %s", r, pattern, sourceRun)
		}
		// FR-008: the fit names each parameter's value.
		for _, p := range captured.Parameters {
			want := p.Value
			if v, ok := subst.to[p.Value]; ok {
				want = v
			}
			if r.Parameters[p.Name] != want {
				t.Errorf("the run's parameter %s is %q, want %q (parameters %v)", p.Name, r.Parameters[p.Name], want, r.Parameters)
			}
		}
		if len(r.NotUsed) != 0 {
			t.Errorf("the run's not_used is %s, want none", r.NotUsed)
		}

		// FR-009, SC-001: no agent and no model in the run record; each of the three agent steps was
		// accepted by the fixed procedure `fill` or its result reused; checks ran and none failed.
		evs := events(t, l, run)
		wellFormedRecord(t, evs)
		for _, e := range evs {
			if e.Model != "" || e.AgentDigest != "" || strings.HasPrefix(e.Worker, "agent:") {
				t.Errorf("event %d %s of step %s names a model, an agent or an agent worker: %+v", e.Seq, e.Kind, e.Step, e)
			}
			if e.Kind == "StepAccepted" && (e.Step == "tests" || e.Step == "code" || e.Step == "review") && e.Worker != "procedure:fill" {
				t.Errorf("step %s was accepted from worker %q, want procedure:fill", e.Step, e.Worker)
			}
		}
		for _, s := range []string{"tests", "code", "review"} {
			if n := count(evs, "StepAccepted", s) + count(evs, "ResultReused", s); n != 1 {
				t.Errorf("step %s has %d StepAccepted or ResultReused events, want 1", s, n)
			}
		}
		// a later build of the same slice and values reuses the kept results instead of checking
		// again (owner-approved 2026-11-26 after CI 45877543492): checks passed or results reused
		passed, reused := count(evs, "CheckPassed", ""), count(evs, "ResultReused", "")
		if passed+reused == 0 || count(evs, "CheckFailed", "") != 0 {
			t.Errorf("the run record has %d passed and %d failed checks and %d reused results, want checks that passed or results reused, and no failed check", passed, count(evs, "CheckFailed", ""), reused)
		}

		// FR-009: the values in contents and paths, each in its case form; no file at a source path.
		for rel, want := range wantFiles(t) {
			have, err := os.ReadFile(filepath.Join(l.ProjectsDir(), project, rel))
			if err != nil || !bytes.Equal(have, want) {
				t.Errorf("%s/%s: %v; differs from the filled template: %v", project, rel, err, err == nil)
			}
		}
		for rel := range cpvTemplateFiles {
			if strings.Contains(rel, "notes") {
				if _, err := os.Stat(filepath.Join(l.ProjectsDir(), project, rel)); err == nil {
					t.Errorf("%s/%s exists: a path was not filled", project, rel)
				}
			}
		}
		return capProjectHashes(t, l, project)
	}

	// T014: "`ap build` ends done" (FR-008 to FR-010, US2.1, US2.3, US2.6, quickstart 3).
	// Planted change: the choice picks the base pattern (agents run, WorkersNotReady without step
	// workers), a fill leaves a source path or a case form unfilled, or `build` prints no pattern.
	var firstHashes map[string]string
	t.Run("6 ap build of the tasks slice uses the captured pattern and ends done", func(t *testing.T) {
		capNeed(t, subst != nil && captured.State == "approved", "the tasks slice's parts are not approved")
		firstHashes = capturedBuild(t, "tasks-1")
		if len(firstHashes) == 0 {
			t.Fatal("the project holds no file")
		}
	})

	// T014: "three builds give equal file hashes" (SC-004).
	// Planted change: a fill writes something that varies (a time, a run ID) into a file.
	var lastBuild time.Time
	t.Run("7 three builds give the same file hashes", func(t *testing.T) {
		capNeed(t, len(firstHashes) > 0, "the first captured build left no files")
		for _, p := range []string{"tasks-2", "tasks-3"} {
			h := capturedBuild(t, p)
			if d := capDiff(firstHashes, h); len(d) != 0 {
				t.Errorf("project %s differs from tasks-1 in %v", p, d)
			}
		}
		lastBuild = time.Now()
	})

	// T014: "the gateway's access log has no request from the capture to the build's end" (SC-001).
	// The scripted step model's exchanges are read too: the step agents use it in the lab, not the
	// gateway the analyst uses, and a fill that called either would fail here.
	// Planted change: the fill step, the proof, the part replaces or the build sends a model request.
	t.Run("8 no model request from the capture to the end of the builds", func(t *testing.T) {
		capNeed(t, !lastBuild.IsZero() && len(window.gateway) > 0, "the three builds did not end")
		if added := capNewSince(window, capSeen(t, l)); len(added) != 0 {
			t.Errorf("%d model requests since the capture, want 0: %v", len(added), added)
		}
	})

	// T014: "`--pattern api-feature-go` runs the agents" (FR-010: the developer chose another
	// pattern; US2.6). The step workers are started as the launcher's build-workers does. The scripted
	// answers are the notes slice's, so the run is cancelled once the agent's request is seen.
	// Planted change: `--pattern` is ignored (the captured pattern is used: no agent request), or the
	// base pattern is named but the fill still runs.
	t.Run("9 build --pattern api-feature-go runs the agents", func(t *testing.T) {
		capNeed(t, !lastBuild.IsZero(), "the three captured builds did not end")
		const project = "tasks-base"
		l.StepWorkers(t, tasksSlice, project)
		defer l.StopStepWorkers(t, tasksSlice, project)
		c := capStart(t, l, bin, 30*time.Minute, "build", tasksSlice, project, "--pattern", "api-feature-go")
		var run string
		deadline := time.Now().Add(5 * time.Minute)
		for run == "" && time.Now().Before(deadline) {
			for _, r := range cpvRuns(t, l) {
				if r.Slice == tasksSlice && r.Project == project {
					run = r.ID
				}
			}
			time.Sleep(200 * time.Millisecond)
		}
		if run == "" {
			_ = c.cmd.Process.Signal(os.Interrupt)
			t.Fatalf("no run of %s into %s within 5 minutes; output:\n%s%s", tasksSlice, project, c.out.String(), c.err.String())
		}
		r := capRunOf(t, l, run)
		if !strings.HasPrefix(r.Pattern, "api-feature-go@") || r.PatternReason != "chosen" {
			t.Errorf("the run is %+v, want the base pattern api-feature-go@<version> with reason chosen", r)
		}
		// The agents run: the step model receives the `tests` step's request (the first agent step).
		l.WaitModelRequest(t, run, "tests", 10*time.Minute)
		so, se, code := cpvCLI(t, l, bin, "cancel", run)
		t.Logf("cancel %s: exit %d %s%s", run, code, so, se)
		out, errOut, exit := c.wait(t, "build --pattern", 15*time.Minute)
		lines := cpvLines(out)
		if len(lines) == 0 {
			t.Fatalf("build --pattern printed nothing (exit %d): %s", exit, errOut)
		}
		if m := capStarted.FindStringSubmatch(lines[0]); m == nil || m[5] != r.Pattern || m[6] != "chosen" {
			t.Errorf("the first line is %q, want `with %s (chosen)`", lines[0], r.Pattern)
		}
		if len(capNewSince(window, capSeen(t, l))) == 0 {
			t.Error("the model servers show no new request: the readers cannot show that the agents ran")
		}
	})
}
