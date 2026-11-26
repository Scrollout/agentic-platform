// T031: failing tests for the run page showing a RunStarted's pattern_reason, proof, source_run
// and parameters as the terminal's RunStarted event line gives them (src/cli/contract.md "Event
// lines": `slice <slice>  pattern <pattern>[  reason <pattern_reason>][  proof][  source <run>]
// [  parameters <name>=<value>,...]`, the parameters in byte order of their names; the engine
// contract's run-event.schema.json since T001). They use the page tests' helpers (eventsPage,
// rowOf, cellUnder). Each case's expected cell is computed by hand from its event.
package page_test

import (
	"strings"
	"testing"
)

// rsRecord is the page's Record cell of the RunStarted event with this seq.
func rsRecord(t *testing.T, events, seq string) string {
	t.Helper()
	body := eventsPage(t, events)
	return cellUnder(t, body, rowOf(t, body, seq), "Record")
}

// The parameters come in byte order of their names: "a-b" (0x2d) < "a1" (0x31) < "ab" (0x62),
// declared here as ab, a1, a-b (and, in the second event, in another unsorted order).
// Covers T031: reason, proof, source and parameters in the contract's order, and the sort.
func TestRsRunPageShowsCapturedRunStartedFieldsInOrder(t *testing.T) {
	events := `[
{"seq":8001,"at":"2026-11-14T10:00:00Z","run":"RUN-1","kind":"RunStarted","slice":"S-4","pattern":"cap-pattern@1.0.0",
 "pattern_reason":"chosen","proof":true,"source_run":"RUN-3","parameters":{"ab":"third","a1":"second","a-b":"first"}},
{"seq":8002,"at":"2026-11-14T10:00:01Z","run":"RUN-2","kind":"RunStarted","slice":"S-5","pattern":"cap-pattern@1.0.0",
 "pattern_reason":"fits","source_run":"RUN-3","parameters":{"zeta":"z","alpha":"two words","mid-1":"9"}}
]`
	body := eventsPage(t, events)
	for seq, want := range map[string]string{
		"8001": "slice S-4  pattern cap-pattern@1.0.0  reason chosen  proof  source RUN-3  parameters a-b=first,a1=second,ab=third",
		"8002": "slice S-5  pattern cap-pattern@1.0.0  reason fits  source RUN-3  parameters alpha=two words,mid-1=9,zeta=z",
	} {
		if got := cellUnder(t, body, rowOf(t, body, seq), "Record"); got != want {
			t.Errorf("seq %s: Record cell\n got %q\nwant %q", seq, got, want)
		}
	}
}

// A run of no captured pattern: only the reason (base) follows the pattern; no proof, source or
// parameters. Covers T031 "leaves them out when the event does not carry them".
func TestRsRunPageShowsBaseReasonOnly(t *testing.T) {
	got := rsRecord(t, `[{"seq":8101,"at":"2026-11-14T10:00:00Z","run":"RUN-1","kind":"RunStarted","slice":"S-4",
 "pattern":"build-slice@1.2.0","pattern_reason":"base"}]`, "8101")
	if want := "slice S-4  pattern build-slice@1.2.0  reason base"; got != want {
		t.Errorf("Record cell %q, want %q", got, want)
	}
}

// An event recorded before feature 011 carries none of the new fields: the page shows none of
// them, and no placeholder for them.
func TestRsRunPageLeavesOutFieldsAnOlderRunStartedLacks(t *testing.T) {
	got := rsRecord(t, `[{"seq":8201,"at":"2026-11-14T10:00:00Z","run":"RUN-1","kind":"RunStarted","slice":"S-4",
 "pattern":"build-slice@1.2.0"}]`, "8201")
	if want := "slice S-4  pattern build-slice@1.2.0"; got != want {
		t.Errorf("Record cell %q, want %q", got, want)
	}
	for _, label := range []string{"reason", "proof", "source", "parameters", "null", "nil", "(none)"} {
		if strings.Contains(got, label) {
			t.Errorf("Record cell of an older RunStarted shows %q: %q", label, got)
		}
	}
}

// Proof absent (the contract's form for every run that is not a proof): no proof word.
// Covers T031 "proof absent".
func TestRsRunPageShowsNoProofWhenAbsent(t *testing.T) {
	got := rsRecord(t, `[{"seq":8301,"at":"2026-11-14T10:00:00Z","run":"RUN-1","kind":"RunStarted","slice":"S-4","pattern":"p@1",
 "pattern_reason":"fits","source_run":"RUN-3","parameters":{"n":"1"}}]`, "8301")
	if want := "slice S-4  pattern p@1  reason fits  source RUN-3  parameters n=1"; got != want {
		t.Errorf("Record cell %q, want %q", got, want)
	}
}

// A captured pattern with no parameter: parameters is an empty object, which the schema allows
// (and requires with source_run). The page shows no "parameters" label for it, as for an empty
// inputs list. Covers T031 empty parameters.
func TestRsRunPageLeavesOutEmptyParameters(t *testing.T) {
	got := rsRecord(t, `[{"seq":8401,"at":"2026-11-14T10:00:00Z","run":"RUN-1","kind":"RunStarted","slice":"S-4",
 "pattern":"p@1","pattern_reason":"chosen","proof":true,"source_run":"RUN-3","parameters":{}}]`, "8401")
	if want := "slice S-4  pattern p@1  reason chosen  proof  source RUN-3"; got != want {
		t.Errorf("Record cell %q, want %q", got, want)
	}
}

// The page escapes a parameter value, name and source as it does the other fields, even though
// the schema allows only parameter_value characters. Covers T031 escaping.
func TestRsRunPageEscapesRunStartedFields(t *testing.T) {
	events := `[{"seq":8501,"at":"2026-11-14T10:00:00Z","run":"RUN-1","kind":"RunStarted","slice":"S-4","pattern":"p@1",
 "pattern_reason":"chosen","source_run":"<img src=s>","parameters":{"a":"<img src=v>","b":"x&y","<i>":"z"}}]`
	body := eventsPage(t, events)
	row := rowOf(t, body, "8501")
	if strings.Contains(row, "<img") || strings.Contains(row, "<i>") {
		t.Errorf("row carries unescaped markup:\n%s", row)
	}
	for _, want := range []string{"source &lt;img src=s&gt;", "a=&lt;img src=v&gt;", "b=x&amp;y", "&lt;i&gt;=z"} {
		if !strings.Contains(row, want) {
			t.Errorf("row lacks the escaped %q:\n%s", want, row)
		}
	}
}
