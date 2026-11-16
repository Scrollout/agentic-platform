package runevents

// Tests for the parser of `docker-agent run --exec --json` output (T109, "The trial's usage").
// Written before the code by a separate author. The fixtures in testdata/ are named by what
// they show; analyst-replay-zero-usage.ndjson is a real stream from the pinned docker-agent
// against the replay server, the others are hand-written from its event shapes.
//
// The rules these tests fix:
//   - Answer: the concatenated `content` of the agent_choice events between the last two
//     message_added events, i.e. the last assistant message, which is what `--last` prints.
//     agent_choice events of earlier messages (text sent with tool calls, a sub-agent's
//     session) are not part of it.
//   - Usage: per session_id, the last token_usage event's usage.input_tokens,
//     usage.output_tokens and usage.cost (each a running total), summed over sessions.
//   - Errors: a line that is not JSON; no stream_stopped, or a last stream_stopped whose
//     reason is not "normal"; an empty answer; an empty stream.

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func open(t *testing.T, name string) *os.File {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

func want(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func parse(t *testing.T, name string) Run {
	t.Helper()
	r, err := Parse(open(t, name))
	if err != nil {
		t.Fatalf("%s: unexpected error: %v", name, err)
	}
	return r
}

func TestRealAnalystStream(t *testing.T) {
	r := parse(t, "analyst-replay-zero-usage.ndjson")
	if r.Answer != want(t, "analyst-replay-zero-usage.answer") {
		t.Fatalf("answer is not the agent_choice content:\n%s", r.Answer)
	}
	if !json.Valid([]byte(r.Answer)) {
		t.Fatalf("the analyst's answer should be one JSON document:\n%s", r.Answer)
	}
	if r.Usage != (Usage{}) {
		t.Fatalf("usage = %+v, want zero (the replay server reports none)", r.Usage)
	}
}

func TestAnswerIsLastMessageAfterToolRounds(t *testing.T) {
	r := parse(t, "step-tool-rounds-then-split-answer.ndjson")
	if got, w := r.Answer, want(t, "step-tool-rounds-then-split-answer.answer"); got != w {
		t.Fatalf("answer = %q, want %q (text sent with tool calls is not the answer)", got, w)
	}
	if u := (Usage{InputTokens: 700, OutputTokens: 90, CostUSD: 0.0014}); !sameUsage(r.Usage, u) {
		t.Fatalf("usage = %+v, want the last token_usage %+v, not a sum of running totals", r.Usage, u)
	}
}

func TestUsageIsLastPerSessionSummed(t *testing.T) {
	r := parse(t, "two-sessions-running-usage.ndjson")
	// root's last: 1200/340/0.0031; helper's last: 500/80/0.0009.
	if u := (Usage{InputTokens: 1700, OutputTokens: 420, CostUSD: 0.004}); !sameUsage(r.Usage, u) {
		t.Fatalf("usage = %+v, want %+v", r.Usage, u)
	}
	if r.Answer != "Final answer." {
		t.Fatalf("answer = %q, want root's last message only, not the sub-session's text", r.Answer)
	}
}

func sameUsage(a, b Usage) bool {
	return a.InputTokens == b.InputTokens && a.OutputTokens == b.OutputTokens &&
		math.Abs(a.CostUSD-b.CostUSD) < 1e-9
}

func TestUsageJSONShape(t *testing.T) {
	// The shape scripts/ci-evals-summary.py reads from each trial's "usage".
	b, err := json.Marshal(Usage{InputTokens: 3, OutputTokens: 4, CostUSD: 0.5})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if len(m) != 3 || m["input_tokens"] != 3.0 || m["output_tokens"] != 4.0 || m["cost_usd"] != 0.5 {
		t.Fatalf("usage JSON = %s, want {input_tokens, output_tokens, cost_usd}", b)
	}
}

func TestBadStreamsFail(t *testing.T) {
	for _, name := range []string{
		"not-json-line.ndjson",
		"no-stream-stopped.ndjson",
		"stream-stopped-not-normal.ndjson",
		"no-answer-after-tool-round.ndjson",
		"empty.ndjson",
	} {
		t.Run(strings.TrimSuffix(name, ".ndjson"), func(t *testing.T) {
			if r, err := Parse(open(t, name)); err == nil {
				t.Fatalf("no error; got answer %q", r.Answer)
			}
		})
	}
}

// The bad fixtures differ from a good one by one thing each, so each error above is for its own
// reason: removing that difference makes the stream parse.
func TestBadFixturesAreOneChangeFromGood(t *testing.T) {
	good := want(t, "step-tool-rounds-then-split-answer.ndjson")
	lines := strings.SplitAfter(good, "\n")
	notJSON := strings.Replace(want(t, "not-json-line.ndjson"), "WARN: not an event\n", "", 1)
	notNormal := strings.Replace(want(t, "stream-stopped-not-normal.ndjson"), `"reason":"error"`, `"reason":"normal"`, 1)
	noStop := want(t, "no-stream-stopped.ndjson") + lines[len(lines)-2]
	for name, s := range map[string]string{"not-json-line": notJSON, "stream-stopped-not-normal": notNormal, "no-stream-stopped": noStop} {
		if s != good {
			t.Fatalf("%s is not the good stream with one change", name)
		}
	}
	if _, err := Parse(bytes.NewReader([]byte(good))); err != nil {
		t.Fatalf("good stream: %v", err)
	}
}
