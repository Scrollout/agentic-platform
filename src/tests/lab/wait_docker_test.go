//go:build docker

package lab

// WaitModelRequest (T054l; research D34), written before the code. Docker, no key, no model, no
// image build. The method the lab gains, in a new file wait.go:
//
//	func (l *Lab) WaitModelRequest(t testing.TB, run, step string, limit time.Duration)
//
// It returns once the step model (l.StepModel()) has received a request whose message's first line
// starts `Run <run>, step <step>`, and calls t.Fatalf when the limit passes first. It sees the
// request while the model still holds its answer:
//
//   - replay mode: from the replay server's request journal, while the mapping's
//     `fixedDelayMilliseconds` holds the answer;
//   - scripted mode (AP_LAB_MODEL=scripted): from the snapshots in the scripted server's `-out`
//     folder, which the server writes before it holds a `delayMs` answer.
//
// The tests send the request themselves from a one-off container on the lab network, as
// scripted_docker_test.go does, so no build runs. The sender runs while WaitModelRequest is
// called; the held answer must still be unanswered when it returns.

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// heldFor is how long each test's model holds the answer. WaitModelRequest has to return well
// before the answer is sent; heldFor leaves room for the slowest start of the sender's container.
const heldFor = 40 * time.Second

// failingTB is a testing.TB whose Fatalf and Fatal stop the calling goroutine's function, as the
// real ones do, and record the message instead of failing the test that uses it.
type failingTB struct {
	testing.TB // the real test; only the methods below are replaced
	failed     bool
	msg        string
}

type fatalStop struct{}

func (f *failingTB) Helper()                   {}
func (f *failingTB) Logf(string, ...any)       {}
func (f *failingTB) Log(...any)                {}
func (f *failingTB) Cleanup(func())            {}
func (f *failingTB) Failed() bool              { return f.failed }
func (f *failingTB) Errorf(s string, a ...any) { f.failed, f.msg = true, fmt.Sprintf(s, a...) }
func (f *failingTB) Error(a ...any)            { f.failed, f.msg = true, fmt.Sprint(a...) }
func (f *failingTB) Fatalf(s string, a ...any) { f.Errorf(s, a...); panic(fatalStop{}) }
func (f *failingTB) Fatal(a ...any)            { f.Error(a...); panic(fatalStop{}) }
func (f *failingTB) FailNow()                  { f.failed = true; panic(fatalStop{}) }

// waitFails calls l.WaitModelRequest with the fake and reports how long it took, whether it failed
// the fake test, and the message.
func waitFails(t *testing.T, l *Lab, run, step string, limit time.Duration) (failed bool, msg string, took time.Duration) {
	t.Helper()
	f := &failingTB{TB: t}
	start := time.Now()
	func() {
		defer func() {
			if r := recover(); r != nil {
				if _, ok := r.(fatalStop); !ok {
					panic(r)
				}
			}
		}()
		l.WaitModelRequest(f, run, step, limit)
	}()
	return f.failed, f.msg, time.Since(start)
}

// sendHeld posts body to the step model from a container on the lab network, in the background,
// and returns a channel that closes when the answer has arrived (or the sender has failed).
func sendHeld(t *testing.T, l *Lab, body string) <-chan struct{} {
	t.Helper()
	name := uniq("aptest-lab-wait-client")
	args := append([]string{"run", "--rm", "--name", name, "--network", l.Net}, labelArgs()...)
	args = append(args, Pin(t, "alpine"), "wget", "-q", "-O", "-", "--header", "Content-Type: application/json",
		"--post-data", body, l.StepModel()+"/v1/messages?beta=true")
	cmd := exec.Command("docker", args...)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { Run("docker", "rm", "-f", name) })
	done := make(chan struct{})
	go func() { cmd.Wait(); close(done) }()
	return done
}

// requireHeld fails unless the sender still waits for its answer: the request was seen while its
// answer was held, not after it.
func requireHeld(t *testing.T, done <-chan struct{}, took time.Duration) {
	t.Helper()
	select {
	case <-done:
		t.Fatalf("WaitModelRequest returned after %s, when the held answer had already been sent: it must see the request while the answer is held", took)
	default:
	}
}

// requireLimit checks the three ways WaitModelRequest must fail at its limit: for a run that sent
// nothing, for a step that sent nothing, and with a message that names what it waited for.
func requireLimit(t *testing.T, l *Lab, run, step string) {
	t.Helper()
	for _, c := range []struct{ name, run, step string }{
		{"another run", "RUN-99", step},
		{"another step", run, "review"},
	} {
		failed, msg, took := waitFails(t, l, c.run, c.step, 3*time.Second)
		if !failed {
			t.Errorf("%s: WaitModelRequest(%s, %s) returned although no such request came", c.name, c.run, c.step)
			continue
		}
		if took < 2500*time.Millisecond || took > 20*time.Second {
			t.Errorf("%s: WaitModelRequest(%s, %s) failed after %s, want about its limit of 3s", c.name, c.run, c.step, took)
		}
		if !strings.Contains(msg, c.run) || !strings.Contains(msg, c.step) {
			t.Errorf("%s: the failure %q does not name the run %s and the step %s", c.name, msg, c.run, c.step)
		}
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWaitModelRequestSeesARequestHeldByTheReplayServer(t *testing.T) {
	t.Setenv("AP_LAB_MODEL", "replay")
	first := "Run RUN-7, step code, attempt 1.\nThe project is the folder /projects/p1.\n"
	body := stepRequest(first)
	var sent any
	if err := json.Unmarshal([]byte(body), &sent); err != nil {
		t.Fatal(err)
	}
	mapping, err := json.Marshal(map[string]any{
		"request": map[string]any{
			"method": "POST", "url": "/v1/messages?beta=true",
			"bodyPatterns": []any{map[string]any{"equalToJson": sent}},
		},
		"response": map[string]any{
			"status": 200, "bodyFileName": "run-7-code-a1-t1.sse",
			"headers":                map[string]string{"Content-Type": "text/event-stream"},
			"fixedDelayMilliseconds": int(heldFor / time.Millisecond),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	set := t.TempDir()
	writeFile(t, filepath.Join(set, "answers.json"), `{"answers":[{"step":"code","turns":[{"text":"Done."}]}]}`)
	writeFile(t, filepath.Join(set, "mappings", "run-7-code-a1-t1.json"), string(mapping))
	writeFile(t, filepath.Join(set, "__files", "run-7-code-a1-t1.sse"),
		"event: message_start\ndata: {\"type\":\"message_start\"}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	os.Chmod(set, 0o755)

	l := Start(t, Steps(set))

	// Nothing came yet: the wait fails at its limit.
	if failed, _, _ := waitFails(t, l, "RUN-7", "code", 2*time.Second); !failed {
		t.Fatal("WaitModelRequest(RUN-7, code) returned before any request was sent")
	}

	done := sendHeld(t, l, body)
	start := time.Now()
	l.WaitModelRequest(t, "RUN-7", "code", 60*time.Second)
	took := time.Since(start)
	requireHeld(t, done, took)
	if took >= heldFor {
		t.Errorf("WaitModelRequest took %s, as long as the mapping holds the answer (%s): it waited for the answer", took, heldFor)
	}
	requireLimit(t, l, "RUN-7", "code")
	if n := l.Misses(t); n != 0 {
		t.Errorf("%d misses: the request the test sent matched no mapping", n)
	}
}

func TestWaitModelRequestSeesARequestHeldByTheScriptedServer(t *testing.T) {
	t.Setenv("AP_LAB_MODEL", "scripted")
	set := t.TempDir()
	writeFile(t, filepath.Join(set, "answers.json"), `{"answers":[{"step":"code","attempt":1,"turns":[
 {"delayMs":40000,"write":[{"path":"internal/notes/notes.go","content":"files/notes.go.txt"}]},
 {"text":"Done."}]}]}`)
	writeFile(t, filepath.Join(set, "files", "notes.go.txt"), scriptedContent)

	l := Start(t, Steps(set))

	if failed, _, _ := waitFails(t, l, "RUN-1", "code", 2*time.Second); !failed {
		t.Fatal("WaitModelRequest(RUN-1, code) returned before any request was sent")
	}

	done := sendHeld(t, l, stepRequest("Run RUN-1, step code, attempt 1.\nThe project is the folder /projects/p1.\n"))
	start := time.Now()
	l.WaitModelRequest(t, "RUN-1", "code", 60*time.Second)
	took := time.Since(start)
	requireHeld(t, done, took)
	if took >= heldFor {
		t.Errorf("WaitModelRequest took %s, as long as the script holds the answer (%s): it waited for the answer", took, heldFor)
	}
	requireLimit(t, l, "RUN-1", "code")
	if n := l.Misses(t); n != 0 {
		t.Errorf("%d misses: the request the test sent matched no script", n)
	}
}
