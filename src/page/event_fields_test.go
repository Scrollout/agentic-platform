// T103: failing tests for the run view showing every field a run event carries (US2.9, FR-024;
// src/cli/contract.md "Event lines"; the engine contract's RunEvent schema). They use the page
// tests' helpers (host, do) and their own stand-in engine, which serves one fixed event list.
package page_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/agentic-platform/page"
)

// eventsPage serves events (a JSON array body) as run-f's only page of events and returns the
// run view's HTML.
func eventsPage(t *testing.T, events string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/runs/run-f/events" {
			http.NotFound(w, r)
			return
		}
		io.WriteString(w, `{"items":`+events+`}`)
	}))
	t.Cleanup(srv.Close)
	h := page.NewHandler(page.Config{EngineURL: srv.URL, Token: "page-secret", Host: host})
	w := do(h, "GET", "/runs/run-f", host)
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	return w.Body.String()
}

// rowOf returns the table row holding the cell of the event with this seq.
func rowOf(t *testing.T, body string, seq string) string {
	t.Helper()
	for _, row := range strings.Split(body, "<tr") {
		if strings.Contains(row, ">"+seq+"</td>") {
			return row
		}
	}
	t.Fatalf("no row for seq %s in page:\n%s", seq, body)
	return ""
}

var cellRe = regexp.MustCompile(`(?s)<t[dh][^>]*>(.*?)</t[dh]>`)

func cellsOf(row string) []string {
	var out []string
	for _, m := range cellRe.FindAllStringSubmatch(row, -1) {
		out = append(out, strings.TrimSpace(m[1]))
	}
	return out
}

// cellUnder returns the text of the row's cell under the header named header (found by name).
func cellUnder(t *testing.T, body, row, header string) string {
	t.Helper()
	for _, chunk := range strings.Split(body, "<tr") {
		if !strings.Contains(chunk, "<th") {
			continue
		}
		for i, h := range cellsOf(chunk) {
			if h == header {
				if cells := cellsOf(row); i < len(cells) {
					return cells[i]
				}
				t.Fatalf("row has no cell under %q:\n%s", header, row)
			}
		}
	}
	t.Errorf("the page has no %q header", header)
	return ""
}

func hex64(unit string) string { return strings.Repeat(unit, 8) }

var (
	hA = hex64("a1b2c3d4")
	hB = hex64("0f1e2d3c")
	hC = hex64("9a8b7c6d")
	hD = hex64("1122aabb")
	hE = hex64("3344ccdd")
	hF = hex64("5566eeff")
)

func TestRunViewShowsEveryFieldAnEventCarries(t *testing.T) {
	events := `[
{"seq":7001,"at":"2026-11-14T10:00:00Z","run":"RUN-1","kind":"RunStarted","slice":"S-4","pattern":"build-slice@1.2.0","detail":"started by developer"},
{"seq":7002,"at":"2026-11-14T10:00:01Z","run":"RUN-1","kind":"AttemptMade","step":"build-step","attempt":2,"round":3,
 "worker":"agent:coder-bee","model":"model-x-1","agent_digest":"` + hA + `","pattern":"build-slice@1.2.0",
 "inputs":["` + hB + `","` + hC + `"],"artifacts":["` + hD + `","` + hE + `"],"reuse_key":"` + hF + `",
 "usage":{"tokens":1234,"cost_usd":0.0123},"refs":["R-1/E-2","G-1"],"detail":"attempt note"},
{"seq":7003,"at":"2026-11-14T10:00:02Z","run":"RUN-1","kind":"StepAccepted","step":"build-step","attempt":2,"round":3,
 "worker":"agent:coder-bee","model":"model-y-2","agent_digest":"` + hB + `","pattern":"build-slice@1.2.0",
 "inputs":["` + hC + `","` + hA + `"],"artifacts":["` + hE + `","` + hD + `"],"reuse_key":"` + hA + `",
 "changed_lines":[{"file":"pkg/alpha.go","line":12},{"file":"pkg/beta_test.go","line":345}],"refs":["R-2","T-3"],"detail":"accepted note"},
{"seq":7004,"at":"2026-11-14T10:00:03Z","run":"RUN-1","kind":"ResultReused","step":"fix-step","round":2,"open_round":true,
 "artifacts":["` + hC + `","` + hF + `"],"reuse_key":"` + hD + `",
 "check":{"name":"NoBlockingFindingOpen","passed":false,"reason":"one blocking finding is open"},
 "changed_lines":[{"file":"cmd/main.go","line":7}],"refs":["R-5"],"detail":"review round 2 left blocking findings open"},
{"seq":7005,"at":"2026-11-14T10:00:04Z","run":"RUN-1","kind":"CheckFailed","step":"build-step","attempt":1,
 "check":{"name":"AllTestsPass","passed":false,"reason":"two tests failed"},"refs":["R-1/E-1","contract/createWidget","G-2"],"detail":"check note"},
{"seq":7006,"at":"2026-11-14T10:00:05Z","run":"RUN-1","kind":"CheckPassed","step":"build-step","attempt":1,
 "check":{"name":"TestsFailAgainstStub","passed":true,"reason":"the tests fail against the stub"},"refs":["R-1","T-1"]},
{"seq":7007,"at":"2026-11-14T10:00:06Z","run":"RUN-1","kind":"StepRejected","step":"build-step","attempt":3,"round":1,"open_round":true,
 "artifacts":["` + hA + `"],"reuse_key":"` + hE + `","usage":{"tokens":987,"cost_usd":1.5},"detail":"review round 1 left blocking findings open"},
{"seq":7009,"at":"2026-11-14T10:00:08Z","run":"RUN-1","kind":"ResultReused","step":"fix-step","round":1,
 "artifacts":["` + hC + `"],"reuse_key":"` + hD + `",
 "check":{"name":"AllTestsPass","passed":true,"reason":"all pass"},"detail":"reused as is"},
{"seq":7010,"at":"2026-11-14T10:00:09Z","run":"RUN-1","kind":"FindingRecorded","step":"review-step","round":2,
 "finding":{"rule":"R-4","file":"pkg/delta.go","line":5,"text":"naming could be clearer","blocking":false},"refs":["R-4"]},
{"seq":7008,"at":"2026-11-14T10:00:07Z","run":"RUN-1","kind":"FindingRecorded","step":"review-step","round":2,
 "finding":{"rule":"R-3","file":"pkg/gamma.go","line":88,"text":"the total is wrong","blocking":true,"check":"FindingsCarryEvidence",
  "witness":{"file":"review/r3/total_test.go","test":"TestR3TotalWrong"}},"refs":["R-3","G-1"],"detail":"finding note"}
]`
	body := eventsPage(t, events)
	want := map[string][]string{
		"7001": {"RunStarted", "S-4", "build-slice@1.2.0", "started by developer"},
		"7002": {"AttemptMade", "build-step", "2", "3", "agent:coder-bee", "model-x-1", hA, hB + "," + hC, hD + "," + hE, hF,
			"1234", "$0.0123", "R-1/E-2,G-1", "attempt note"},
		"7003": {"StepAccepted", "model-y-2", "agent:coder-bee", hB, hC + "," + hA, hE + "," + hD, hA,
			"pkg/alpha.go:12,pkg/beta_test.go:345", "R-2,T-3", "accepted note"},
		"7004": {"ResultReused", "fix-step", "2", hC + "," + hF, hD, "cmd/main.go:7", "NoBlockingFindingOpen",
			"one blocking finding is open", "R-5", "review round 2 left blocking findings open"},
		"7005": {"CheckFailed", "AllTestsPass", "two tests failed", "R-1/E-1,contract/createWidget,G-2", "check note"},
		"7006": {"CheckPassed", "TestsFailAgainstStub", "the tests fail against the stub", "R-1,T-1"},
		"7007": {"StepRejected", "3", hA, hE, "987", "$1.5000", "review round 1 left blocking findings open"},
		"7008": {"FindingRecorded", "review-step", "R-3", "pkg/gamma.go", "88", "the total is wrong", "FindingsCarryEvidence",
			"review/r3/total_test.go", "TestR3TotalWrong", "R-3,G-1", "finding note"},
	}
	for seq, values := range want {
		row := rowOf(t, body, seq)
		for _, v := range values {
			if !strings.Contains(row, v) {
				t.Errorf("row of seq %s lacks %q:\n%s", seq, v, row)
			}
		}
	}
	// Open-round events say "open round" (the contract's label); a ResultReused without it does not.
	for _, seq := range []string{"7004", "7007"} {
		if r := rowOf(t, body, seq); !strings.Contains(r, "open round") {
			t.Errorf("row of seq %s (open_round) lacks %q:\n%s", seq, "open round", r)
		}
	}
	if r := rowOf(t, body, "7009"); strings.Contains(strings.ToLower(r), "open round") {
		t.Errorf("row of seq 7009 (no open_round) shows open round:\n%s", r)
	}
	// Round and attempt are shown exactly, in the cells under their headers.
	for seq, ra := range map[string][2]string{"7002": {"3", "2"}, "7003": {"3", "2"}, "7004": {"2", ""},
		"7005": {"", "1"}, "7007": {"1", "3"}, "7008": {"2", ""}, "7009": {"1", ""}, "7001": {"", ""}} {
		row := rowOf(t, body, seq)
		if got := cellUnder(t, body, row, "Round"); got != ra[0] {
			t.Errorf("seq %s: Round cell %q, want %q", seq, got, ra[0])
		}
		if got := cellUnder(t, body, row, "Attempt"); got != ra[1] {
			t.Errorf("seq %s: Attempt cell %q, want %q", seq, got, ra[1])
		}
	}
	// A finding's kind, and where it is.
	if r := rowOf(t, body, "7008"); !strings.Contains(r, "blocking") || strings.Contains(r, "suggestion") {
		t.Errorf("blocking finding's row should say blocking, not suggestion:\n%s", r)
	}
	if r := rowOf(t, body, "7010"); !strings.Contains(r, "suggestion") || strings.Contains(r, "blocking") {
		t.Errorf("non-blocking finding's row should say suggestion, not blocking:\n%s", r)
	}
	if c := cellUnder(t, body, rowOf(t, body, "7008"), "Finding"); !strings.Contains(c, "pkg/gamma.go:88") {
		t.Errorf("Finding cell lacks pkg/gamma.go:88: %q", c)
	}
	if c := cellUnder(t, body, rowOf(t, body, "7010"), "Finding"); !strings.Contains(c, "pkg/delta.go:5") {
		t.Errorf("Finding cell lacks pkg/delta.go:5: %q", c)
	}
}

func TestRunViewLeavesOutFieldsAnEventDoesNotCarry(t *testing.T) {
	body := eventsPage(t, `[{"seq":7101,"at":"2026-11-14T11:00:00Z","run":"RUN-1","kind":"RunResumed"}]`)
	row := strings.ToLower(rowOf(t, body, "7101"))
	if !strings.Contains(row, "runresumed") {
		t.Fatalf("row lacks its kind:\n%s", row)
	}
	for _, label := range []string{"round", "slice", "pattern", "worker", "model", "agent", "inputs", "artifacts",
		"reuse", "open", "changed", "tokens", "cost", "$", "usage", "witness", "check", "finding", "blocking",
		"suggestion", "refs", "attempt", "[]", "(none)", "<nil>", "null"} {
		if strings.Contains(row, label) {
			t.Errorf("row of an event with only seq, at, kind and run shows %q:\n%s", label, row)
		}
	}
}

func TestRunViewEscapesEventFields(t *testing.T) {
	// The schemas restrict worker and the witness test; the page must escape whatever the store holds.
	events := `[
{"seq":7201,"at":"2026-11-14T12:00:00Z","run":"RUN-1","kind":"AttemptMade","step":"build-step","attempt":1,
 "worker":"agent:<img src=w>","model":"<img src=m>","agent_digest":"` + hA + `","pattern":"build-slice@1.2.0",
 "inputs":[],"artifacts":[],"reuse_key":"` + hB + `"},
{"seq":7202,"at":"2026-11-14T12:00:01Z","run":"RUN-1","kind":"StepAccepted","step":"build-step","attempt":1,
 "worker":"agent:coder-bee","model":"m","agent_digest":"` + hA + `","pattern":"build-slice@1.2.0",
 "inputs":[],"artifacts":[],"reuse_key":"` + hB + `","changed_lines":[{"file":"<img src=f>.go","line":3}]},
{"seq":7203,"at":"2026-11-14T12:00:02Z","run":"RUN-1","kind":"FindingRecorded","step":"review-step","round":1,
 "finding":{"rule":"R-1","file":"a.go","line":1,"text":"t","blocking":true,
  "witness":{"file":"review/r1/x_test.go","test":"TestR1<img src=t>"}}}
]`
	body := eventsPage(t, events)
	for seq, marker := range map[string]string{"7201": "&lt;img src=", "7202": "&lt;img src=f&gt;.go", "7203": "TestR1&lt;img src=t&gt;"} {
		row := rowOf(t, body, seq)
		if strings.Contains(row, "<img") {
			t.Errorf("row of seq %s carries unescaped markup:\n%s", seq, row)
		}
		if !strings.Contains(row, marker) {
			t.Errorf("row of seq %s lacks the escaped value %q:\n%s", seq, marker, row)
		}
	}
	if r := rowOf(t, body, "7201"); !strings.Contains(r, "agent:&lt;img src=w&gt;") || !strings.Contains(r, "&lt;img src=m&gt;") {
		t.Errorf("worker or model not shown escaped:\n%s", r)
	}
}
