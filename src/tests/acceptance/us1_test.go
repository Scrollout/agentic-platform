//go:build docker

// T031: user story 1 (spec scenarios US1.1 to US1.6, quickstart step 2) through the engine's HTTP
// API, in a lab (src/tests/lab) with the analyst answering from the recorded model. Subtests run
// in order and share one lab, as the story does: one slice is described, questioned, edited and
// approved, a second is described that the pattern does not fit. A missing Docker is a failure.
package acceptance

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/agentic-platform/tests/lab"
)

// A description the catalogue's pattern does not fit (quickstart step 2); the analyst-cli-python
// recording answers it with a proposal whose contract has `paths: {}`.
const cliDescription = "A command-line tool in Python that counts the words of a text file given on the command line and prints the number."

// A description no recording answers: the analyst's model call gets 404.
const unrecorded = "A weather widget that shows tomorrow's temperature for a city the user picks."

var hex64 = regexp.MustCompile(`^[0-9a-f]{64}$`)

func names(cs []lab.Check) []string {
	var n []string
	for _, c := range cs {
		n = append(n, c.Part+"/"+c.Check+": "+c.Reason)
	}
	return n
}

// onlyFailure requires that exactly the failed checks of d are the ones wanted: each "part/check"
// key with a substring its reason must hold.
func onlyFailure(t *testing.T, d lab.Doc, want map[string]string) {
	t.Helper()
	got := map[string]string{}
	for _, c := range d.Failed() {
		got[c.Part+"/"+c.Check] = c.Reason
	}
	for k, sub := range want {
		r, ok := got[k]
		if !ok {
			t.Errorf("no failed check %s; failed checks: %v", k, names(d.Failed()))
		} else if !strings.Contains(r, sub) {
			t.Errorf("check %s reason %q does not name %q", k, r, sub)
		}
	}
	for k, r := range got {
		if _, ok := want[k]; !ok {
			t.Errorf("unexpected failed check %s: %s", k, r)
		}
	}
}

// allPass requires every check to pass, and the lint result to be part of them (the slice's
// checks hold the latest result of every check, and the contract did not change).
func allPass(t *testing.T, d lab.Doc, when string) {
	t.Helper()
	for _, c := range d.Failed() {
		t.Errorf("%s: check %s/%s failed: %s", when, c.Part, c.Check, c.Reason)
	}
	have := map[string]bool{}
	for _, c := range d.Checks {
		have[c.Check] = true
	}
	for _, n := range []string{"Schema", "Traceability", "ExampleTagged", "ContractIsOpenApi31", "OnlyHttpOperations",
		"ContractRefsLocal", "RulesNameOneSystem", "ContractCompatibility", "ContractLint"} {
		if !have[n] {
			t.Errorf("%s: the slice's checks have no %s result: %v", when, n, names(d.Checks))
		}
	}
}

func TestUS1(t *testing.T) {
	l := lab.Start(t)
	var s lab.Doc
	var id string
	var original lab.Doc // the proposal as revised by the answers, to put back after each edit

	t.Run("US1.1 a description the pattern fits gives goals, terms, rules, examples and a contract passing their checks", func(t *testing.T) {
		id = l.Create(t, lab.Example)
		if id != "S-1" {
			t.Errorf("the first slice is %q, want S-1 (quickstart step 2)", id)
		}
		s = l.Get(t, id)
		if len(s.Goals) == 0 || len(s.Rules) == 0 || s.Examples == "" || s.Contract == "" {
			t.Fatalf("the slice lacks a part: %d goals, %d rules, examples %q, contract %q", len(s.Goals), len(s.Rules), s.Examples, s.Contract)
		}
		goals := map[string]bool{}
		for _, g := range s.Goals {
			goals[g["id"].(string)] = true
		}
		for _, r := range s.Rules {
			rid := r["id"].(string)
			if g, _ := r["goal"].(string); !goals[g] {
				t.Errorf("rule %s serves goal %q, which does not exist", rid, g)
			}
			if s.RuleText[rid] == "" {
				t.Errorf("rule %s has no EARS sentence in rule_text", rid)
			}
			if !strings.Contains(s.Examples, "Rule: "+rid) || !strings.Contains(s.Examples, "@"+rid+" ") {
				t.Errorf("the examples hold no tagged scenario under a `Rule: %s` block", rid)
			}
			if !strings.Contains(s.Contract, "x-rules") || !strings.Contains(s.Contract, rid) {
				t.Errorf("the contract does not link rule %s to an operation", rid)
			}
		}
		allPass(t, s, "after the proposal")
		for _, p := range []string{"goals", "rules", "contract"} {
			if !hex64.MatchString(s.Parts[p]) {
				t.Errorf("parts[%s] = %q, want a SHA-256", p, s.Parts[p])
			}
		}
		if len(s.History) == 0 || s.History[0].Kind != "proposed" || s.History[0].Model == "" || s.History[0].AgentDigest == "" {
			t.Errorf("history does not start with a proposal naming the model and the agent: %+v", s.History)
		}
		if len(s.Approvals) != 0 {
			t.Errorf("a new slice holds approvals: %+v", s.Approvals)
		}
		if len(s.Questions) > 10 {
			t.Errorf("%d questions, at most 10 allowed", len(s.Questions))
		}
		if len(s.Questions) == 0 || s.State != "questions-open" {
			t.Errorf("the recorded proposal asks 2 questions; state %q with %d questions", s.State, len(s.Questions))
		}
		st, _, b := l.Do("GET", "/slices", lab.ClientToken, nil, nil)
		if st != 200 || !strings.Contains(string(b), `"S-1"`) {
			t.Errorf("listSlices = %d %s, want S-1 in it", st, b)
		}
	})

	t.Run("US1.3 ambiguity gives questions that name their part; answers revise the slice", func(t *testing.T) {
		parts := map[string]string{"R-3": "rules", "contract/createNote": "contract"}
		for _, q := range s.Questions {
			if parts[q.About] == "" {
				t.Errorf("question %s is about %q, which names no part", q.ID, q.About)
			}
			if q.Text == "" || len(q.Answer) != 0 {
				t.Errorf("question %s: text %q, answer %s, want text and no answer yet", q.ID, q.Text, q.Answer)
			}
		}
		// A part with an open question cannot be approved.
		l.Refused(t, "POST", "/slices/"+id+"/approvals", map[string]string{"part": "rules", "content_hash": s.Parts["rules"]}, 409, "ContradictionOpen")
		l.Refused(t, "POST", "/slices/"+id+"/approvals", map[string]string{"part": "contract", "content_hash": s.Parts["contract"]}, 409, "ContradictionOpen")

		// Bad answers are refused and change nothing.
		bad := func(a ...any) any { return map[string]any{"answers": a} }
		q := func(id string, answer any) any { return map[string]any{"question": id, "answer": answer} }
		l.Refused(t, "POST", "/slices/"+id+"/answers", bad(q("Q-1", map[string]any{"option": 0, "text": "yes"})), 400, "InvalidRequest")
		l.Refused(t, "POST", "/slices/"+id+"/answers", bad(q("Q-1", map[string]any{"option": 9})), 400, "InvalidRequest")
		l.Refused(t, "POST", "/slices/"+id+"/answers", bad(), 400, "InvalidRequest")
		l.Refused(t, "POST", "/slices/"+id+"/answers", bad(q("Q-9", map[string]any{"text": "x"})), 409, "QuestionNotOpen")

		answers := bad(q("Q-1", map[string]any{"option": 0}), q("Q-2", map[string]any{"text": "400"}))
		if op := l.Wait(t, l.Accept(t, "POST", "/slices/"+id+"/answers", answers)); op.Status != "succeeded" || op.Result.Slice != id {
			t.Fatalf("answerQuestions: %+v (error %+v)", op, op.Error)
		}
		s = l.Get(t, id)
		for _, q := range s.Questions {
			if len(q.Answer) == 0 {
				t.Errorf("question %s is still open after being answered", q.ID)
			}
		}
		if s.State != "checked" {
			t.Errorf("state after the answers = %q, want checked", s.State)
		}
		if n := len(s.History); n < 3 || s.History[n-2].Kind != "answered" || s.History[n-1].Kind != "revised" {
			t.Errorf("history does not end with answered then revised: %+v", s.History)
		}
		allPass(t, s, "after the answers")
		l.Refused(t, "POST", "/slices/"+id+"/answers", bad(q("Q-1", map[string]any{"option": 1})), 409, "QuestionNotOpen")
		original = s
	})

	t.Run("US1.2 a rule without an example, a goal without a rule or an undefined term is rejected and named", func(t *testing.T) {
		fix := func(kind string, body any) {
			l.Replace(t, id, kind, body)
			allPass(t, l.Get(t, id), "after putting the "+kind+" back")
		}
		refuse := func(part, check, sub string) {
			d := l.Get(t, id)
			p := l.Refused(t, "POST", "/slices/"+id+"/approvals", map[string]string{"part": part, "content_hash": d.Parts[part]}, 409, "PartFailsChecks")
			found := false
			for _, f := range p.Failures {
				if f.Part == part && f.Check == check && !f.Passed && strings.Contains(f.Reason, sub) {
					found = true
				}
			}
			if !found {
				t.Errorf("the refusal's failures do not name %s %s %q: %+v", part, check, sub, p.Failures)
			}
		}

		// A rule without an example: R-2's only scenario removed.
		noExample := regexp.MustCompile(`(?s)\n    @R-2 @E-2\n.*?\n\n`).ReplaceAllString(original.Examples, "\n")
		if noExample == original.Examples {
			t.Fatalf("the test could not remove R-2's scenario from:\n%s", original.Examples)
		}
		l.Replace(t, id, "rules", map[string]any{"rules": original.Rules, "examples": noExample, "terms": original.Terms})
		onlyFailure(t, l.Get(t, id), map[string]string{"rules/Traceability": "R-2"})
		refuse("rules", "Traceability", "R-2")
		fix("rules", original.RulesBody())

		// A goal no rule serves.
		l.Replace(t, id, "goals", map[string]any{"goals": append(append([]map[string]any{}, original.Goals...), map[string]any{"id": "G-3", "text": "Keep a history"})})
		onlyFailure(t, l.Get(t, id), map[string]string{"goals/Traceability": "G-3"})
		refuse("goals", "Traceability", "G-3")
		fix("goals", original.GoalsBody())

		// A term no definition exists for.
		rules := append([]map[string]any{}, original.Rules...)
		r0 := map[string]any{}
		for k, v := range rules[0] {
			r0[k] = v
		}
		r0["terms"] = []string{"T-9"}
		rules[0] = r0
		l.Replace(t, id, "rules", map[string]any{"rules": rules, "examples": original.Examples, "terms": original.Terms})
		onlyFailure(t, l.Get(t, id), map[string]string{"rules/Traceability": "T-9"})
		refuse("rules", "Traceability", "T-9")
		fix("rules", original.RulesBody())
		if st := l.Get(t, id).State; st == "approved" {
			t.Errorf("a slice with failing checks is %s", st)
		}
	})

	t.Run("US1.4 a build of an unapproved slice is refused and nothing is built", func(t *testing.T) {
		p := l.Refused(t, "POST", "/slices/"+id+"/runs", map[string]string{"project": "notes"}, 409, "NotApproved")
		if p.Detail == "" {
			t.Errorf("the refusal gives no reason")
		}
		d := l.Get(t, id)
		if d.LatestRun != "" {
			t.Errorf("latest_run = %q after a refused build", d.LatestRun)
		}
		st, _, b := l.Do("GET", "/runs", lab.ClientToken, nil, nil)
		var rl struct{ Items []json.RawMessage }
		if json.Unmarshal(b, &rl) != nil || st != 200 || len(rl.Items) != 0 {
			t.Errorf("listRuns = %d %s, want no runs", st, b)
		}
		// A hash that is not the part's current one is refused, and approves nothing.
		l.Refused(t, "POST", "/slices/"+id+"/approvals", map[string]string{"part": "goals", "content_hash": strings.Repeat("0", 64)}, 409, "StaleContentHash")
		if a := l.Get(t, id).Approved(); len(a) != 0 {
			t.Errorf("approvals hold after a refused approval: %v", a)
		}
	})

	t.Run("US1.1 the developer approves goals, rules and contract", func(t *testing.T) {
		for _, p := range []string{"goals", "rules", "contract"} {
			l.Approve(t, id, p)
		}
		d := l.Get(t, id)
		if a := d.Approved(); !(a["goals"] && a["rules"] && a["contract"]) || d.State != "approved" {
			t.Errorf("state %q, approvals %+v, want all three parts approved", d.State, d.Approvals)
		}
		// Approved, the build is no longer refused as NotApproved: no worker is registered yet.
		l.Refused(t, "POST", "/slices/"+id+"/runs", map[string]string{"project": "notes"}, 409, "WorkersNotReady")
	})

	t.Run("US1.5 changing an approved part withdraws its approval; checks run again; nothing is built", func(t *testing.T) {
		// The goals change: only the goals approval is withdrawn.
		goals := append([]map[string]any{}, original.Goals...)
		g0 := map[string]any{}
		for k, v := range goals[0] {
			g0[k] = v
		}
		g0["text"] = g0["text"].(string) + " reliably"
		goals[0] = g0
		l.Replace(t, id, "goals", map[string]any{"goals": goals})
		d := l.Get(t, id)
		if a := d.Approved(); a["goals"] || !a["rules"] || !a["contract"] || d.State == "approved" {
			t.Errorf("after changing the goals: state %q, approved %v, want rules and contract only", d.State, a)
		}
		allPass(t, d, "after changing the goals")
		l.Refused(t, "POST", "/slices/"+id+"/runs", map[string]string{"project": "notes"}, 409, "NotApproved")
		l.Approve(t, id, "goals")
		if d := l.Get(t, id); d.State != "approved" {
			t.Errorf("state %q after approving the changed goals, want approved", d.State)
		}

		// Quickstart: delete a rule's only scenario.
		noExample := regexp.MustCompile(`(?s)\n    @R-1 @E-1\n.*?\n\n`).ReplaceAllString(d.Examples, "\n")
		if noExample == d.Examples {
			t.Fatalf("the test could not remove R-1's scenario from:\n%s", d.Examples)
		}
		l.Replace(t, id, "rules", map[string]any{"rules": d.Rules, "examples": noExample, "terms": d.Terms})
		d = l.Get(t, id)
		onlyFailure(t, d, map[string]string{"rules/Traceability": "R-1"})
		if a := d.Approved(); a["rules"] || !a["goals"] || !a["contract"] || d.State == "approved" {
			t.Errorf("after deleting R-1's scenario: state %q, approved %v, want goals and contract only", d.State, a)
		}
		p := l.Refused(t, "POST", "/slices/"+id+"/runs", map[string]string{"project": "notes"}, 409, "NotApproved")
		if !strings.Contains(p.Detail, "rules") {
			t.Errorf("the refusal's detail %q does not say the rules part is not approved", p.Detail)
		}
		// Put it back: the rules need approving again before a build.
		l.Replace(t, id, "rules", original.RulesBody())
		d = l.Get(t, id)
		allPass(t, d, "after putting the rules back")
		if d.Approved()["rules"] {
			t.Errorf("the rules approval came back by itself")
		}
		l.Refused(t, "POST", "/slices/"+id+"/runs", map[string]string{"project": "notes"}, 409, "NotApproved")
		l.Approve(t, id, "rules")
		if d := l.Get(t, id); d.State != "approved" {
			t.Errorf("state %q after approving the rules again, want approved", d.State)
		}
	})

	t.Run("US1.6 a description the pattern does not fit is named, cannot be approved and builds nothing", func(t *testing.T) {
		cid := l.Create(t, cliDescription)
		d := l.Get(t, cid)
		onlyFailure(t, d, map[string]string{"contract/OnlyHttpOperations": "paths"})
		if d.State == "approved" {
			t.Errorf("state %q", d.State)
		}
		p := l.Refused(t, "POST", "/slices/"+cid+"/approvals", map[string]string{"part": "contract", "content_hash": d.Parts["contract"]}, 409, "PatternDoesNotFit")
		if len(p.Failures) != 1 || p.Failures[0].Check != "OnlyHttpOperations" || !strings.Contains(p.Failures[0].Reason, "paths") {
			t.Errorf("the refusal does not name the failing condition and why: %+v", p.Failures)
		}
		// Refused with no part approved, and with the parts that do pass approved.
		p = l.Refused(t, "POST", "/slices/"+cid+"/runs", map[string]string{"project": "wordcount"}, 409, "PatternDoesNotFit")
		if len(p.Failures) == 0 || p.Failures[0].Check != "OnlyHttpOperations" {
			t.Errorf("startRun's refusal does not list the failing condition: %+v", p.Failures)
		}
		l.Approve(t, cid, "goals")
		l.Approve(t, cid, "rules")
		l.Refused(t, "POST", "/slices/"+cid+"/runs", map[string]string{"project": "wordcount"}, 409, "PatternDoesNotFit")
		if d := l.Get(t, cid); d.LatestRun != "" {
			t.Errorf("latest_run = %q after a refused build", d.LatestRun)
		}
		st, _, b := l.Do("GET", "/runs", lab.ClientToken, nil, nil)
		var rl struct{ Items []json.RawMessage }
		if json.Unmarshal(b, &rl) != nil || st != 200 || len(rl.Items) != 0 {
			t.Errorf("listRuns = %d %s, want no runs", st, b)
		}
	})

	// Every analyst request so far hit a recording.
	if n := l.Misses(t); n != 0 {
		t.Errorf("the replay server saw %d request(s) no recording matched:\n%s", n, l.MissesJSON(t))
	}

	t.Run("a description no recording answers fails the operation and the replay server lists the miss", func(t *testing.T) {
		op := l.Wait(t, l.Accept(t, "POST", "/slices", map[string]string{"description": unrecorded}))
		if op.Status != "failed" || op.Error == nil || op.Error.Code != "OperationFailed" {
			t.Fatalf("operation %+v (error %+v), want failed with OperationFailed", op, op.Error)
		}
		if l.Misses(t) == 0 {
			t.Errorf("the replay server lists no miss for an unrecorded description")
		}
	})
}
