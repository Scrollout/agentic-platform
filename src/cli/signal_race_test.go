// T055 review regression: a signal that arrives after a command has its own result (an end line, a
// compare table) must not turn that result into a detach (contract.md, "Following a run": Ctrl-C
// detaches only while following). The order is made deterministic through standard output: the
// writer sends the signal on Env.Signals when the command writes its last line, waits until the
// client has taken it, and only then returns; the command then ends for its own reason.
//
// A refusal (exit 4) cannot be ordered this way through the seams: between the engine's answer and
// Run printing the problem the client writes nothing and calls nothing, so no test hook runs there,
// and a signal before the answer is read is a legitimate "stopped; the request may have been
// recorded". It has no test here.
package cli_test

import (
	"context"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	cli "github.com/agentic-platform/cli"
)

// signalOnLine is standard output that sends sig on sigs once the output holds mark.
type signalOnLine struct {
	syncBuf
	mark string
	sigs chan os.Signal
	sig  os.Signal
	once sync.Once
	sent bool
}

func (w *signalOnLine) Write(p []byte) (int, error) {
	n, err := w.syncBuf.Write(p)
	if strings.Contains(w.syncBuf.String(), w.mark) {
		w.once.Do(func() {
			select {
			case w.sigs <- w.sig: // unbuffered: the client's signal reader has it
				w.sent = true
				time.Sleep(100 * time.Millisecond) // let it record the signal before the command returns
			case <-time.After(2 * time.Second):
			}
		})
	}
	return n, err
}

func runSignalledAt(t *testing.T, e *eng, mark string, sig os.Signal, args ...string) (result, bool) {
	t.Helper()
	sigs := make(chan os.Signal) // unbuffered, so the send completes only when the client receives it
	out := &signalOnLine{mark: mark, sigs: sigs, sig: sig}
	var errb syncBuf
	env := e.env()
	env.Signals = sigs
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	code := cli.Run(ctx, args, env, strings.NewReader(""), out, &errb)
	return result{code, out.String(), errb.String()}, out.sent
}

func TestASignalAfterTheCommandHasItsResultKeepsTheResult(t *testing.T) {
	waiting := endStates("RUN-7", "OP-12")[1]
	expired := endStates("RUN-7", "OP-12")[4]
	cases := []struct {
		name, mark string
		args       []string
		serve      func(t *testing.T, e *eng)
		exit       int
	}{
		{"build ends waiting", "waits for the developer", []string{"build", "S-1", "notes"}, func(t *testing.T, e *eng) {
			buildRoutes(e)
			stateRun(t, "RUN-7", "OP-12", waiting, 0).register(e)
		}, 2},
		{"run ends expired", "ended expired", []string{"run", "RUN-7"}, func(t *testing.T, e *eng) {
			stateRun(t, "RUN-7", "OP-12", expired, 0).register(e)
		}, 5},
		{"compare differs", "STEP", []string{"compare", "RUN-7", "RUN-8"}, func(t *testing.T, e *eng) {
			e.reply("GET /runs/RUN-7/comparison", 200, map[string]any{"items": []any{
				map[string]any{"step": "tests", "field": "artifacts", "left": rhArt, "right": rhArt2}}})
		}, 7},
	}
	for _, c := range cases {
		for _, s := range []os.Signal{os.Interrupt, syscall.SIGTERM} {
			t.Run(c.name+"/"+s.String(), func(t *testing.T) {
				e := newEng(t)
				c.serve(t, e)
				r, sent := runSignalledAt(t, e, c.mark, s, c.args...)
				if !sent {
					t.Fatalf("the client never took the signal after writing %q (stdout %q)", c.mark, r.out)
				}
				wantExit(t, r, c.exit)
				wantAbsent(t, "stderr", r.err, "stopped")
				if e.anyPathWith("/cancel") {
					t.Error("a signal must not cancel anything")
				}
			})
		}
	}
}
