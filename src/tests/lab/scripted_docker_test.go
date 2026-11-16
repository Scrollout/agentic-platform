//go:build docker

package lab

// The lab's model switch (T054k; research D34, ADR 0038), written before the code. Docker, no key,
// no model. What the lab gains:
//
//   - `AP_LAB_MODEL` (environment, read by Start): unset, empty or `replay` is today's lab (the
//     replay server answers every model request); `scripted` makes the step workers' model the
//     scripted server (`src/tests/recorded/scripted`) answering from the lab's step set, while the
//     analyst keeps the replay server. Any other value fails Start. `synth-steps.sh` sets it.
//   - `func Steps(set string) Option`: the lab's step recording set, a folder holding
//     `answers.json` and its content files and, once made, `mappings/` and `__files/`. A relative
//     set is under `src/tests/recorded/steps/` (`Steps("happy")`, `Steps("planted/outside-scope")`);
//     an absolute one is used as is. With `AP_LAB_MODEL=scripted` a lab without Steps fails Start.
//   - `func (l *Lab) StepModel() string`: the model URL the step workers get as
//     MODEL_GATEWAY_URL (`http://<address>:8080` on the lab network).
//   - In scripted mode `l.Misses` also counts the requests no script answered.
//   - When the lab ends (its t's cleanups), every exchange the scripted server answered is written
//     into the set by `recording write`: `mappings/<name>.json` and `__files/<name>.sse`, named as
//     the scripted server names them; the set's `answers.json` and content files are unchanged.
//
// This test does not run a build: it sends the step agents' kind of request to StepModel from a
// one-off container on the lab network, as a step worker would.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const scriptedAnswers = `{"answers":[{"step":"code","attempt":1,"turns":[
 {"write":[{"path":"internal/notes/notes.go","content":"files/notes.go.txt"}]},
 {"text":"Done."}]}]}`

const scriptedContent = "package notes\n\n// \"quoted\" \\ and é\n"

// stepRequest is a code step's first request as Docker Agent sends it.
func stepRequest(first string) string {
	b, _ := json.Marshal(map[string]any{
		"model": "claude-haiku-5-5", "max_tokens": 16000, "stream": true,
		"messages": []any{map[string]any{"role": "user", "content": []any{map[string]any{
			"type": "text", "text": first}}}},
		"tools": []any{map[string]any{"name": "write_file", "input_schema": map[string]any{"type": "object"}}},
	})
	return string(b)
}

func TestScriptedLabAnswersFromTheScriptsAndWritesTheExchangesIntoTheSet(t *testing.T) {
	set := t.TempDir()
	if err := os.MkdirAll(filepath.Join(set, "files"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(set, "answers.json"), []byte(scriptedAnswers), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(set, "files", "notes.go.txt"), []byte(scriptedContent), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AP_LAB_MODEL", "scripted")
	body := stepRequest("Run RUN-1, step code, attempt 1.\nThe project is the folder /projects/p1.\n")

	t.Run("lab", func(t *testing.T) { // the lab ends with this subtest, which writes the exchanges
		l := Start(t, Steps(set))
		post := func(b string) (string, int) {
			return Run("docker", append(append([]string{"run", "--rm", "--network", l.Net}, labelArgs()...),
				Pin(t, "alpine"), "wget", "-q", "-O", "-", "--header", "Content-Type: application/json",
				"--post-data", b, l.StepModel()+"/v1/messages?beta=true")...)
		}
		out, code := post(body)
		if code != 0 {
			t.Fatalf("the scripted step model did not answer the code step: exit %d: %s", code, out)
		}
		for _, want := range []string{"event: message_start", "write_file", `/projects/p1/internal/notes/notes.go`, "event: message_stop"} {
			if !strings.Contains(out, want) {
				t.Errorf("the answer lacks %q:\n%.600s", want, out)
			}
		}
		if n := l.Misses(t); n != 0 {
			t.Errorf("%d misses after an answered request", n)
		}
		// A request no script answers is a miss.
		if _, code := post(stepRequest("Run RUN-1, step code, attempt 2.\nThe project is the folder /projects/p1.\n")); code == 0 {
			t.Error("an attempt no script answers was answered")
		}
		if n := l.Misses(t); n != 1 {
			t.Errorf("l.Misses = %d after one unanswered step request, want 1", n)
		}
	})

	name := "run-1-code-a1-t1"
	raw, err := os.ReadFile(filepath.Join(set, "mappings", name+".json"))
	if err != nil {
		t.Fatalf("the exchange was not written into the set: %v", err)
	}
	var m struct {
		Request struct {
			URL          string `json:"url"`
			BodyPatterns []struct {
				EqualToJSON any `json:"equalToJson"`
			} `json:"bodyPatterns"`
		} `json:"request"`
		Response struct {
			BodyFileName string `json:"bodyFileName"`
		} `json:"response"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	var sent any
	_ = json.Unmarshal([]byte(body), &sent)
	if m.Request.URL != "/v1/messages?beta=true" || len(m.Request.BodyPatterns) != 1 ||
		!jsonEqual(m.Request.BodyPatterns[0].EqualToJSON, sent) || m.Response.BodyFileName != name+".sse" {
		t.Errorf("mapping %s does not match the request sent or name its body:\n%s", name, raw)
	}
	sse, err := os.ReadFile(filepath.Join(set, "__files", name+".sse"))
	if err != nil || !strings.Contains(string(sse), "write_file") {
		t.Errorf("__files/%s.sse missing or not the answer: %v", name, err)
	}
	if es, _ := os.ReadDir(filepath.Join(set, "mappings")); len(es) != 1 {
		t.Errorf("%d mappings in the set, want 1 (a miss writes none)", len(es))
	}
	if b, _ := os.ReadFile(filepath.Join(set, "answers.json")); string(b) != scriptedAnswers {
		t.Error("the set's answers.json changed")
	}
}

// jsonEqual compares two decoded JSON values by their canonical encoding.
func jsonEqual(a, b any) bool {
	ja, err1 := json.Marshal(a)
	jb, err2 := json.Marshal(b)
	return err1 == nil && err2 == nil && string(ja) == string(jb)
}
