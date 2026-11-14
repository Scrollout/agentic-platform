//go:build docker

// The lab's model switch (T054k; research D34, ADR 0038). `AP_LAB_MODEL` unset, empty or `replay`
// is the replay server for every model request, serving a lab's step set (Steps) beside the
// committed recordings; `scripted` gives the step workers the scripted server
// (src/tests/recorded/scripted) answering from the set's answer script, while the analyst keeps
// the replay server, and writes every exchange it answered into the set when the lab ends.
package lab

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

const (
	modelReplay   = "replay"
	modelScripted = "scripted"
)

// Steps names the lab's step recording set: a folder holding `answers.json`, its content files and,
// once made, `mappings/` and `__files/`. A relative set is under `src/tests/recorded/steps/`; an
// absolute one is used as is.
func Steps(set string) Option {
	return func(o *options) { o.steps = set }
}

type labModel struct {
	mode     string
	set      string // the step set's folder, "" without Steps
	scripted string // the scripted server's container, in scripted mode
	stepIP   string // the step workers' model address
}

// modelOf reads AP_LAB_MODEL and the Steps option of o, and refuses an unknown model or a scripted
// lab without a step set.
func modelOf(t testing.TB, o *options) *labModel {
	t.Helper()
	set, has := o.steps, o.steps != ""
	m := &labModel{mode: os.Getenv("AP_LAB_MODEL")}
	switch m.mode {
	case "", modelReplay:
		m.mode = modelReplay
	case modelScripted:
		if !has {
			t.Fatal("AP_LAB_MODEL=scripted needs the lab's step set (lab.Steps)")
		}
	default:
		t.Fatalf("AP_LAB_MODEL=%q: want replay (or unset) or scripted", m.mode)
	}
	if has {
		if !filepath.IsAbs(set) {
			set = filepath.Join(Root(t), "src", "tests", "recorded", "steps", filepath.FromSlash(set))
		}
		if _, err := os.Stat(filepath.Join(set, "answers.json")); err != nil {
			t.Fatalf("the step set %s: %v", set, err)
		}
		m.set = set
	}
	return m
}

// startModel starts the replay server, and in scripted mode the scripted server, and returns the
// replay server's container name. bin holds the lab's built tools.
func (l *Lab) startModel(t *testing.T, m *labModel, bin string) string {
	t.Helper()
	l.model = m
	replay := exec.Command("sh", filepath.Join(l.root, "src/tests/recorded/replay.sh"), "start", l.Net, uniq("aptest-lab-replay"))
	replay.Env = os.Environ()
	if m.mode == modelReplay && m.set != "" {
		replay.Env = append(replay.Env, "REPLAY_RECORDINGS="+recordingsWithSet(t, l.root, m.set))
	}
	out, err := replay.CombinedOutput()
	if err != nil {
		t.Fatalf("replay.sh start: %v: %s", err, out)
	}
	gateway := strings.TrimSpace(string(out))
	if m.mode == modelReplay {
		m.stepIP = ip(t, gateway, l.Net)
		return gateway
	}

	build(t, bin, "scripted", "./recorded/scripted")
	build(t, bin, "recording", "./recorded/recording")
	exchanges := t.TempDir() // removed after the exchanges are written (cleanups run last first)
	m.scripted = uniq("aptest-lab-scripted")
	t.Cleanup(func() { writeExchanges(t, filepath.Join(bin, "recording"), exchanges, m.set) })
	t.Cleanup(func() { Run("docker", "rm", "-f", m.scripted) })
	a := append([]string{"run", "-d", "--name", m.scripted, "--network", l.Net}, labelArgs()...)
	a = append(a, runtimeArgs()...)
	a = append(a, hardened()...)
	// As the developer (the later --user wins), so it writes the exchanges the host then reads.
	a = append(a, "--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()),
		"--memory", "256m", "--pids-limit", "64",
		"-v", bin+":/s:ro", "-v", m.set+":/set:ro", "-v", exchanges+":/out",
		Pin(t, "alpine"), "/s/scripted", "-set", "/set", "-out", "/out", "-listen", ":8080")
	Docker(t, a...)
	deadline := time.Now().Add(60 * time.Second)
	for {
		if _, code := scriptedMisses(m.scripted); code == 0 {
			break
		}
		if time.Now().After(deadline) {
			logs, _ := Run("docker", "logs", m.scripted)
			t.Fatalf("the scripted server did not answer: %s", logs)
		}
		time.Sleep(300 * time.Millisecond)
	}
	m.stepIP = ip(t, m.scripted, l.Net)
	return gateway
}

// StepModel is the model URL the step workers get as MODEL_GATEWAY_URL: the scripted server in
// scripted mode, otherwise the replay server.
func (l *Lab) StepModel() string {
	if l.model == nil {
		return ""
	}
	return "http://" + l.stepModelHost() + ":8080"
}

// stepModelHost is the host of StepModel, which the step workers get as their `gateway`.
func (l *Lab) stepModelHost() string {
	if l.model == nil {
		return ""
	}
	return l.model.stepIP
}

// scriptedMisses is the scripted server's list of requests no script answered, as JSON.
func scriptedMisses(container string) (string, int) {
	return Run("docker", "exec", container, "wget", "-q", "-O", "-", "http://127.0.0.1:8080/__scripted/misses")
}

// scriptedMissCount is how many requests the scripted server could not answer; 0 in replay mode.
func (l *Lab) scriptedMissCount(t testing.TB) int {
	t.Helper()
	if l.model == nil || l.model.scripted == "" {
		return 0
	}
	out, code := scriptedMisses(l.model.scripted)
	var list []json.RawMessage
	if code != 0 || json.Unmarshal([]byte(out), &list) != nil {
		t.Fatalf("the scripted server's misses: exit %d: %s", code, out)
	}
	return len(list)
}

// writeExchanges writes every snapshot in dir into the set with `recording write`, by its name.
func writeExchanges(t testing.TB, recording, dir, set string) {
	t.Helper()
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Errorf("the scripted exchanges: %v", err)
		return
	}
	var names []string
	for _, e := range es {
		if n, ok := strings.CutSuffix(e.Name(), ".json"); ok && !strings.HasPrefix(n, ".") {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	for _, n := range names {
		in, err := os.Open(filepath.Join(dir, n+".json"))
		if err != nil {
			t.Errorf("exchange %s: %v", n, err)
			continue
		}
		cmd := exec.Command(recording, "write", "-dir", set, "-name", n)
		cmd.Stdin = in
		out, err := cmd.CombinedOutput()
		in.Close()
		if err != nil {
			t.Errorf("recording write %s into %s: %v: %s", n, set, err, out)
		}
	}
	if len(names) > 0 {
		t.Logf("lab: wrote %d exchanges into %s", len(names), set)
	}
}

// recordingsWithSet is a folder holding the committed recordings and the set's, which the replay
// server reads (WireMock reads the mappings' subfolders; the bodies share __files).
func recordingsWithSet(t testing.TB, root, set string) string {
	t.Helper()
	dir := t.TempDir()
	base := filepath.Join(root, "src", "tests", "recorded", "recordings")
	for _, c := range []struct{ from, to string }{
		{filepath.Join(base, "mappings"), filepath.Join(dir, "mappings", "base")},
		{filepath.Join(base, "__files"), filepath.Join(dir, "__files")},
		{filepath.Join(set, "mappings"), filepath.Join(dir, "mappings", "steps")},
		{filepath.Join(set, "__files"), filepath.Join(dir, "__files")},
	} {
		if err := copyFiles(c.from, c.to); err != nil {
			t.Fatalf("the replay server's recordings: %v", err)
		}
	}
	// The replay server reads them as 65532.
	if err := filepath.Walk(dir, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if fi.IsDir() {
			mode = 0o755
		}
		return os.Chmod(p, mode)
	}); err != nil {
		t.Fatal(err)
	}
	return dir
}

// copyFiles copies the files of from (not its subfolders) into to; a missing from copies nothing.
// A file already in to is refused, so two recordings never share a name.
func copyFiles(from, to string) error {
	es, err := os.ReadDir(from)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := os.MkdirAll(to, 0o755); err != nil {
		return err
	}
	for _, e := range es {
		if !e.Type().IsRegular() {
			continue
		}
		src, err := os.Open(filepath.Join(from, e.Name()))
		if err != nil {
			return err
		}
		dst, err := os.OpenFile(filepath.Join(to, e.Name()), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			src.Close()
			return fmt.Errorf("%s: %w", e.Name(), err)
		}
		_, err = io.Copy(dst, src)
		src.Close()
		if cerr := dst.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
	}
	return nil
}
