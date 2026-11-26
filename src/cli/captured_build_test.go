// T030 (feature 011, US2): `ap build --pattern` and the pattern-choice lines in the terminal client,
// written against src/cli/contract.md ("Commands", "Problems", "Following a run" with its event-line
// table, "Inspecting runs") and the engine contract's startRun, before the code exists. Every command
// runs in process through cli.Run against the stand-in engine of runs_fake_test.go (called, not
// edited). Every name here starts with cb so no other file collides with it.
//
// Cases that already pass are regression guards for US1 code (the `with` part, the RunStarted line and
// the header); the cases that fail today need `--pattern` and the `Not used:` lines (T015).
package cli_test

import (
	"encoding/json"
	"strings"
	"testing"
)

// ---- fixtures ----

const (
	cbRef  = "tdd-s-1@1.0.0"
	cbBase = "tdd@1.0.0"
)

// cbOrdered is a JSON object that keeps the order its pairs are given in (a Go map would be
// marshalled in byte order, hiding the case "declared order differs from byte order").
type cbOrdered [][2]string

func (o cbOrdered) MarshalJSON() ([]byte, error) {
	var b strings.Builder
	b.WriteByte('{')
	for i, kv := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(kv[0])
		v, _ := json.Marshal(kv[1])
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return []byte(b.String()), nil
}

// cbRun is the short done run RUN-7 (project notes, operation OP-12) whose record is RunStarted,
// StepStarted and RunCompleted. run holds fields added to the run itself (getRun), started the
// fields of its RunStarted event.
func cbRun(t *testing.T, run, started map[string]any) *fakeRun {
	fr := stateRun(t, "RUN-7", "OP-12", cpStates["done"], 1)
	for k, v := range run {
		fr.final[k] = v
	}
	f := map[string]any{"slice": "S-1", "pattern": cbBase}
	for k, v := range started {
		f[k] = v
	}
	fr.events[0] = ev(1, "RUN-7", "RunStarted", f)
	return fr
}

// cbRecord is the record cbRun prints after its first event line.
func cbRecord() []string {
	return []string{"2  " + at(2) + "  StepStarted  tests", "3  " + at(3) + "  RunCompleted"}
}

func cbStartedEvent(fields string) string {
	return "1  " + at(1) + "  RunStarted  slice S-1  pattern " + fields
}

// cbBodyOf is the JSON body of the n-th startRun call (from 0).
func cbBodyOf(t *testing.T, e *eng, n int) map[string]any {
	t.Helper()
	posts := e.got("POST", "/slices/S-1/runs")
	if len(posts) <= n {
		t.Fatalf("startRun calls = %d, want more than %d", len(posts), n)
	}
	var m map[string]any
	if err := json.Unmarshal(posts[n].Body, &m); err != nil {
		t.Fatalf("startRun body %q is not JSON: %v", posts[n].Body, err)
	}
	return m
}

// ---- the flag ----

// T030: contract.md commands table: `build <slice> <project> [--pattern <id>]`: `--pattern` names the
// pattern (the base pattern or a captured pattern); the id goes to startRun as `pattern`.
func TestCbPatternFlagSendsPatternInStartRunBody(t *testing.T) {
	for _, id := range []string{"tdd-s-1", "tdd", "a", strings.Repeat("a", 64)} {
		t.Run(id, func(t *testing.T) {
			e := newEng(t)
			buildRoutes(e)
			cbRun(t, nil, nil).register(e)
			r := e.run("build", "S-1", "notes", "--pattern", id)
			wantExit(t, r, 0)
			b := cbBodyOf(t, e, 0)
			if b["pattern"] != id || b["project"] != "notes" || len(b) != 2 {
				t.Errorf("startRun body = %v, want exactly project notes and pattern %q", b, id)
			}
			if n := len(e.got("POST", "/slices/S-1/runs")); n != 1 {
				t.Errorf("startRun calls = %d, want 1", n)
			}
		})
	}
}

// T030: regression guard: without `--pattern` the engine chooses, so startRun carries no `pattern`.
func TestCbNoPatternFlagSendsNoPattern(t *testing.T) {
	e := newEng(t)
	buildRoutes(e)
	cbRun(t, nil, nil).register(e)
	r := e.run("build", "S-1", "notes")
	wantExit(t, r, 0)
	if b := cbBodyOf(t, e, 0); len(b) != 1 || b["project"] != "notes" {
		t.Errorf("startRun body = %v, want only the project", b)
	}
}

// T030: `--pattern` with no value is a usage error (exit 3, contract "Run states and exit codes": a
// missing argument) and the engine is never called.
func TestCbPatternFlagWithoutValueIsUsageError(t *testing.T) {
	e := newEng(t)
	buildRoutes(e)
	r := e.run("build", "S-1", "notes", "--pattern")
	wantExit(t, r, 3)
	if e.total() != 0 {
		t.Errorf("engine calls = %d, want none for a usage error", e.total())
	}
	if r.out != "" {
		t.Errorf("stdout = %q, want nothing", r.out)
	}
}

// ---- the first line ----

// T030: contract.md "Following a run": `build` prints `RUN-<n> started: building <slice> into
// <project> with <pattern> (<pattern_reason>) (operation OP-<m>)`, the part `with <pattern>
// (<pattern_reason>)` read from the run; the line comes first, then the record, then the end line.
// The three reasons are regression guards for the US1 line.
func TestCbBuildFirstLineNamesPatternAndReason(t *testing.T) {
	for _, c := range []struct{ pattern, reason string }{
		{cbBase, "base"}, {cbRef, "fits"}, {cbRef, "chosen"},
	} {
		t.Run(c.reason+"/"+c.pattern, func(t *testing.T) {
			e := newEng(t)
			buildRoutes(e)
			cbRun(t, map[string]any{"pattern": c.pattern, "pattern_reason": c.reason},
				map[string]any{"pattern": c.pattern, "pattern_reason": c.reason}).register(e)
			r := e.run("build", "S-1", "notes")
			wantExit(t, r, 0)
			wantOut(t, r, lines(append([]string{
				"RUN-7 started: building S-1 into notes with " + c.pattern + " (" + c.reason + ") (operation OP-12)",
				cbStartedEvent(c.pattern + "  reason " + c.reason)}, append(cbRecord(), "RUN-7 ended done")...)...))
		})
	}
}

// T030: contract.md: the `with` part "is left out for a run that records no `pattern_reason`" (a run
// from before feature 011). Regression guard.
func TestCbBuildFirstLineWithoutReasonLeavesOutTheWithPart(t *testing.T) {
	e := newEng(t)
	buildRoutes(e)
	cbRun(t, nil, nil).register(e)
	r := e.run("build", "S-1", "notes")
	wantExit(t, r, 0)
	wantOut(t, r, lines(append([]string{startedLine, cbStartedEvent(cbBase)}, append(cbRecord(), "RUN-7 ended done")...)...))
	wantAbsent(t, "stdout", r.out, "Not used", " with ")
}

// T030: contract.md: "Each entry of the run's `not_used` then prints one line `Not used: <captured
// pattern>: <reason>`, `<reason>` being the entry's `detail`", after the first line and before any
// event.
func TestCbBuildPrintsOneNotUsedLineAfterTheFirstLine(t *testing.T) {
	e := newEng(t)
	buildRoutes(e)
	notUsed := []any{map[string]any{"pattern": "tdd-s-2", "reason": "needs-proof",
		"detail": "its latest proof did not pass under the current method"}}
	var out syncBuf
	var atFirstPage string
	fr := cbRun(t, map[string]any{"pattern": cbBase, "pattern_reason": "base", "not_used": notUsed},
		map[string]any{"pattern": cbBase, "pattern_reason": "base"})
	fr.onFirstPage = func() { atFirstPage = out.String() }
	fr.register(e)
	var errb syncBuf
	code := runTo(e, &out, &errb, "build", "S-1", "notes")
	r := result{code, out.String(), errb.String()}
	wantExit(t, r, 0)
	first := "RUN-7 started: building S-1 into notes with " + cbBase + " (base) (operation OP-12)"
	notUsedLine := "Not used: tdd-s-2: its latest proof did not pass under the current method"
	wantOut(t, r, lines(append([]string{first, notUsedLine, cbStartedEvent(cbBase + "  reason base")},
		append(cbRecord(), "RUN-7 ended done")...)...))
	if atFirstPage != first+"\n"+notUsedLine+"\n" {
		t.Errorf("before the first event was read, stdout was %q, want the run line and the Not used line", atFirstPage)
	}
}

// T030: two `not_used` entries print two lines, in the order the run records them (the order judged),
// each `Not used: <captured pattern>: <reason>`; for `refused-value` the reason is `<parameter>:
// <detail>` (contract.md, "Following a run"). The first entry's pattern sorts after the second's, so a
// sorted print would fail.
func TestCbBuildPrintsTwoNotUsedLinesInRecordedOrder(t *testing.T) {
	e := newEng(t)
	buildRoutes(e)
	notUsed := []any{
		map[string]any{"pattern": "tdd-s-9", "reason": "needs-proof", "detail": "its latest proof failed"},
		map[string]any{"pattern": "tdd-s-2", "reason": "refused-value", "parameter": "note-id",
			"detail": "9x is not a name"},
	}
	cbRun(t, map[string]any{"pattern": cbBase, "pattern_reason": "base", "not_used": notUsed},
		map[string]any{"pattern": cbBase, "pattern_reason": "base"}).register(e)
	r := e.run("build", "S-1", "notes")
	wantExit(t, r, 0)
	wantOut(t, r, lines(append([]string{
		"RUN-7 started: building S-1 into notes with " + cbBase + " (base) (operation OP-12)",
		"Not used: tdd-s-9: its latest proof failed",
		"Not used: tdd-s-2: note-id: 9x is not a name",
		cbStartedEvent(cbBase + "  reason base")}, append(cbRecord(), "RUN-7 ended done")...)...))
}

// T030: the `Not used:` reason is "escaped as store text" (contract.md): a control character in the
// detail prints as its escape, so the entry stays one line.
func TestCbNotUsedDetailIsEscapedAsStoreText(t *testing.T) {
	e := newEng(t)
	buildRoutes(e)
	notUsed := []any{map[string]any{"pattern": "tdd-s-2", "reason": "needs-proof", "detail": "a\nb\tc"}}
	cbRun(t, map[string]any{"pattern": cbBase, "pattern_reason": "base", "not_used": notUsed},
		map[string]any{"pattern": cbBase, "pattern_reason": "base"}).register(e)
	r := e.run("build", "S-1", "notes")
	wantExit(t, r, 0)
	if !cpHasLine(r.out, `Not used: tdd-s-2: a\nb\tc`) {
		t.Errorf("stdout lacks the escaped line `Not used: tdd-s-2: a\\nb\\tc`:\n%s", r.out)
	}
}

// T030: a run with an empty or absent `not_used` prints no `Not used:` line.
func TestCbNoNotUsedEntriesPrintNoNotUsedLine(t *testing.T) {
	for name, extra := range map[string]map[string]any{
		"absent": {}, "empty": {"not_used": []any{}},
	} {
		t.Run(name, func(t *testing.T) {
			e := newEng(t)
			buildRoutes(e)
			run := map[string]any{"pattern": cbRef, "pattern_reason": "fits"}
			for k, v := range extra {
				run[k] = v
			}
			cbRun(t, run, map[string]any{"pattern": cbRef, "pattern_reason": "fits"}).register(e)
			r := e.run("build", "S-1", "notes")
			wantExit(t, r, 0)
			wantAbsent(t, "stdout", r.out, "Not used")
		})
	}
}

// ---- the RunStarted event line and the run header ----

// T030: contract.md event table: `RunStarted` prints `slice <slice>  pattern <pattern>[  reason
// <pattern_reason>][  proof][  source <source_run>][  parameters <name>=<value>,...]`, then the free
// text `detail`; the header prints `RUN-<n>  slice <slice>  pattern <pattern>[  reason
// <pattern_reason>][  proof][  source <source_run>]  project <project>  operation OP-<m>  created
// <created_at>[  replay of RUN-<k>]` and never the parameters. The parameter cases print in byte order
// of their names (`-` 0x2d sorts before digits and letters: a-b, a1, note, note-id, notes) although
// the run declares them in another order; "none" and "empty" print no `parameters` field.
func TestCbRunHeaderAndRunStartedLine(t *testing.T) {
	declared := cbOrdered{{"notes", "tasks"}, {"note-id", "t-1"}, {"a1", "x"}, {"note", "task"}, {"a-b", "y"}}
	const sorted = "a-b=y,a1=x,note=task,note-id=t-1,notes=tasks"
	hdr := func(fields, tail string) string {
		return "RUN-7  slice S-1  pattern " + fields + "  project notes  operation OP-12  created " + tCreated + tail
	}
	for _, c := range []struct {
		name        string
		run, event  map[string]any
		header, evt string
	}{
		{"reason only", map[string]any{"pattern": cbRef, "pattern_reason": "fits"},
			map[string]any{"pattern": cbRef, "pattern_reason": "fits"},
			hdr(cbRef+"  reason fits", ""), cbRef + "  reason fits"},
		{"source and no parameters", map[string]any{"pattern": cbRef, "pattern_reason": "fits", "source_run": "RUN-3"},
			map[string]any{"pattern": cbRef, "pattern_reason": "fits", "source_run": "RUN-3"},
			hdr(cbRef+"  reason fits  source RUN-3", ""), cbRef + "  reason fits  source RUN-3"},
		{"empty parameters", map[string]any{"pattern": cbRef, "pattern_reason": "fits", "source_run": "RUN-3", "parameters": map[string]any{}},
			map[string]any{"pattern": cbRef, "pattern_reason": "fits", "source_run": "RUN-3", "parameters": map[string]any{}},
			hdr(cbRef+"  reason fits  source RUN-3", ""), cbRef + "  reason fits  source RUN-3"},
		{"one parameter", map[string]any{"pattern": cbRef, "pattern_reason": "fits", "source_run": "RUN-3", "parameters": map[string]any{"note": "task"}},
			map[string]any{"pattern": cbRef, "pattern_reason": "fits", "source_run": "RUN-3", "parameters": map[string]any{"note": "task"}},
			hdr(cbRef+"  reason fits  source RUN-3", ""), cbRef + "  reason fits  source RUN-3  parameters note=task"},
		{"parameters declared out of byte order", map[string]any{"pattern": cbRef, "pattern_reason": "fits", "source_run": "RUN-3", "parameters": declared},
			map[string]any{"pattern": cbRef, "pattern_reason": "fits", "source_run": "RUN-3", "parameters": declared},
			hdr(cbRef+"  reason fits  source RUN-3", ""), cbRef + "  reason fits  source RUN-3  parameters " + sorted},
		{"parameter value with a control character", map[string]any{"pattern": cbRef, "pattern_reason": "chosen", "parameters": map[string]any{"note": "a\nb"}},
			map[string]any{"pattern": cbRef, "pattern_reason": "chosen", "parameters": map[string]any{"note": "a\nb"}},
			hdr(cbRef+"  reason chosen", ""), cbRef + "  reason chosen  parameters note=a\\nb"},
		{"detail follows the fields", map[string]any{"pattern": cbRef, "pattern_reason": "fits", "source_run": "RUN-3", "parameters": declared, "replay_of": "RUN-2"},
			map[string]any{"pattern": cbRef, "pattern_reason": "fits", "source_run": "RUN-3", "parameters": declared, "detail": "a replay"},
			hdr(cbRef+"  reason fits  source RUN-3", "  replay of RUN-2"),
			cbRef + "  reason fits  source RUN-3  parameters " + sorted + "  a replay"},
		{"no reason, no captured pattern", nil, nil, hdr(cbBase, ""), cbBase},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newEng(t)
			cbRun(t, c.run, c.event).register(e)
			r := e.run("run", "RUN-7")
			wantExit(t, r, 0)
			wantOut(t, r, lines(append([]string{c.header, "1  " + at(1) + "  RunStarted  slice S-1  pattern " + c.evt},
				append(cbRecord(), "RUN-7 ended done")...)...))
			if strings.Contains(strings.SplitN(r.out, "\n", 2)[0], "parameters") {
				t.Errorf("the header prints parameters:\n%s", r.out)
			}
		})
	}
}

// T030: the same RunStarted line, with its parameters in byte order of their names, is printed while
// `build` follows the run (declared order notes, note-id, note).
func TestCbBuildFollowsRunStartedWithParametersInByteOrder(t *testing.T) {
	declared := cbOrdered{{"notes", "tasks"}, {"note-id", "t-1"}, {"note", "task"}}
	e := newEng(t)
	buildRoutes(e)
	cbRun(t, map[string]any{"pattern": cbRef, "pattern_reason": "fits", "source_run": "RUN-3", "parameters": declared},
		map[string]any{"pattern": cbRef, "pattern_reason": "fits", "source_run": "RUN-3", "parameters": declared}).register(e)
	r := e.run("build", "S-1", "notes", "--pattern", "tdd-s-1")
	wantExit(t, r, 0)
	wantOut(t, r, lines(append([]string{
		"RUN-7 started: building S-1 into notes with " + cbRef + " (fits) (operation OP-12)",
		cbStartedEvent(cbRef + "  reason fits  source RUN-3  parameters note=task,note-id=t-1,notes=tasks")},
		append(cbRecord(), "RUN-7 ended done")...)...))
}

// T030: a proof run's RunStarted line and header carry `proof` between `reason` and `source`.
func TestCbProofRunLineAndHeaderCarryProof(t *testing.T) {
	e := newEng(t)
	cbRun(t, map[string]any{"pattern": cbRef, "pattern_reason": "chosen", "proof": true, "source_run": "RUN-3"},
		map[string]any{"pattern": cbRef, "pattern_reason": "chosen", "proof": true, "source_run": "RUN-3"}).register(e)
	e.reply("GET /captured-patterns/tdd-s-1", 200, cpPattern("proven", false, nil, cpProofRec("RUN-7", "passed")))
	r := e.run("run", "RUN-7")
	wantExit(t, r, 0)
	ls := cpLines(r.out)
	wantHeader := "RUN-7  slice S-1  pattern " + cbRef + "  reason chosen  proof  source RUN-3  project notes  operation OP-12  created " + tCreated
	if ls[0] != wantHeader {
		t.Errorf("header = %q, want %q", ls[0], wantHeader)
	}
	if ls[1] != cbStartedEvent(cbRef+"  reason chosen  proof  source RUN-3") {
		t.Errorf("RunStarted line = %q", ls[1])
	}
}

// ---- refusals of startRun ----

// T030: contract.md "Problems": a refusal of startRun (exit 4) prints `refused: <code>: <title>:
// <detail>` on standard error (`: <title>` left out when it equals the code, `: <detail>` when there is
// none), nothing on standard output, and no run is followed. The codes are those the engine contract's
// startRun lists for a named or chosen pattern (409, and NotFound for a `pattern` naming none).
// ParameterRefused is not a startRun answer (it fails replaceParameters' operation), so it is not here.
func TestCbBuildRefusalsPrintCodeAndReasonExit4(t *testing.T) {
	for _, c := range []struct {
		status              int
		code, title, detail string
		want                string
	}{
		{409, "PatternDoesNotFit", "The pattern does not fit", "slice/rules line 4 column 9: the value 9x is not a name",
			"refused: PatternDoesNotFit: The pattern does not fit: slice/rules line 4 column 9: the value 9x is not a name\n"},
		{409, "CapturedPatternNotApproved", "CapturedPatternNotApproved", "tdd-s-1 is proven, not approved",
			"refused: CapturedPatternNotApproved: tdd-s-1 is proven, not approved\n"},
		{409, "ProofRequired", "A new proof is required", "its latest proof did not pass under the current method",
			"refused: ProofRequired: A new proof is required: its latest proof did not pass under the current method\n"},
		{409, "CapturedPatternWithdrawn", "CapturedPatternWithdrawn", "",
			"refused: CapturedPatternWithdrawn\n"},
		{409, "CapturedPatternBusy", "CapturedPatternBusy", "another operation of tdd-s-1 runs",
			"refused: CapturedPatternBusy: another operation of tdd-s-1 runs\n"},
		{404, "NotFound", "NotFound", "no pattern tdd-s-7", "refused: NotFound: no pattern tdd-s-7\n"},
	} {
		t.Run(c.code, func(t *testing.T) {
			e := newEng(t)
			e.refuse("POST /slices/S-1/runs", c.status, prob(c.code, c.title, c.detail))
			r := e.run("build", "S-1", "notes", "--pattern", "tdd-s-1")
			wantExit(t, r, 4)
			if r.err != c.want {
				t.Errorf("stderr = %q, want %q", r.err, c.want)
			}
			if r.out != "" {
				t.Errorf("stdout = %q, want nothing for a refusal", r.out)
			}
			if e.total() != 1 {
				t.Errorf("engine calls = %d, want only the refused startRun (no run is read, no retry)", e.total())
			}
			if b := cbBodyOf(t, e, 0); b["pattern"] != "tdd-s-1" {
				t.Errorf("startRun body = %v, want the pattern named", b)
			}
		})
	}
}

// T030: a PatternDoesNotFit refusal that lists its failures prints one `failed  <check> (<part>):
// <reason>` line per entry, in order (contract.md "Problems").
func TestCbBuildPatternDoesNotFitPrintsItsFailureLines(t *testing.T) {
	e := newEng(t)
	e.refuse("POST /slices/S-1/runs", 409, prob("PatternDoesNotFit", "PatternDoesNotFit", "the slice fails a condition",
		map[string]any{"check": "OnlyHttpOperations", "part": "contract", "reason": "the contract has no HTTP operation"},
		map[string]any{"check": "RulesNameOneSystem", "part": "rules", "reason": "two systems are named"}))
	r := e.run("build", "S-1", "notes")
	wantExit(t, r, 4)
	want := "refused: PatternDoesNotFit: the slice fails a condition\n" +
		"  failed  OnlyHttpOperations (contract): the contract has no HTTP operation\n" +
		"  failed  RulesNameOneSystem (rules): two systems are named\n"
	if r.err != want {
		t.Errorf("stderr = %q, want %q", r.err, want)
	}
	if r.out != "" {
		t.Errorf("stdout = %q, want nothing", r.out)
	}
}
