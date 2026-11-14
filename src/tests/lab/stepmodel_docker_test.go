//go:build docker

package lab

// The step workers' model (T054k; owner's approval of 2026-11-14 in the T054k task text): every
// step-agent container that l.StepWorkers starts gets as its model the host of l.StepModel(), the
// scripted server with AP_LAB_MODEL=scripted and the replay server otherwise. It is read from the
// started container (`docker inspect`): its MODEL_GATEWAY_URL is l.StepModel(). Procedure workers
// call no model and are not checked.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stepAgentModels is each started step agent's MODEL_GATEWAY_URL, by step.
func stepAgentModels(t *testing.T, l *Lab, slice, project string) map[string]string {
	t.Helper()
	steps, err := patternSteps(l.root)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, s := range steps {
		if !strings.HasPrefix(s.Worker, "agent:") {
			continue
		}
		env := Docker(t, "inspect", "-f", "{{range .Config.Env}}{{println .}}{{end}}", stepWorkerName(slice, s.Name, project))
		for _, kv := range strings.Split(env, "\n") {
			if v, ok := strings.CutPrefix(kv, "MODEL_GATEWAY_URL="); ok {
				got[s.Name] = v
			}
		}
		if _, ok := got[s.Name]; !ok {
			t.Errorf("step %s: its container has no MODEL_GATEWAY_URL", s.Name)
		}
	}
	if len(got) == 0 {
		t.Fatal("the pattern has no agent step")
	}
	return got
}

func TestStepWorkersUseTheLabsStepModel(t *testing.T) {
	for _, mode := range []string{"scripted", "replay"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("AP_LAB_MODEL", mode)
			var opts []Option
			opts = append(opts, Build())
			if mode == "scripted" {
				set := t.TempDir()
				if err := os.WriteFile(filepath.Join(set, "answers.json"),
					[]byte(`{"answers":[{"step":"tests","turns":[{"text":"Done."}]}]}`), 0o644); err != nil {
					t.Fatal(err)
				}
				opts = append(opts, Steps(set))
			}
			l := Start(t, opts...)
			want := l.StepModel()
			if want == "" {
				t.Fatal("l.StepModel() is empty")
			}
			slice := approvedExample(t, l)
			l.StepWorkers(t, slice, labProject)
			for step, got := range stepAgentModels(t, l, slice, labProject) {
				if got != want {
					t.Errorf("%s mode, step %s: MODEL_GATEWAY_URL = %s, want l.StepModel() = %s", mode, step, got, want)
				}
			}
		})
	}
}
