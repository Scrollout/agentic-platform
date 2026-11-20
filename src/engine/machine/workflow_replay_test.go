package machine_test

// T020: the recorded histories of the Run workflow replay against the production workflow, and a
// planted nondeterministic change (a reordered activity) fails the same replay (research R2, R9;
// constitution, "Checks prove they can fail"). The histories are found by glob, so a new recording
// beside them is replayed without editing this file.

import (
	"compress/gzip"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentic-platform/engine/durable"
	historypb "go.temporal.io/api/history/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/log"
	temporalworker "go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

// recordedHistories reads every recording; it fails when there is none or one is empty or corrupt.
func recordedHistories(t *testing.T) map[string]*historypb.History {
	t.Helper()
	root, err := os.OpenRoot(filepath.Join("testdata", "histories"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	}()
	files, err := fs.Glob(root.FS(), "*.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no recorded history found under testdata/histories/*.json.gz")
	}
	out := map[string]*historypb.History{}
	for _, f := range files {
		out[filepath.Join("testdata", "histories", f)] = readHistory(t, root, f)
	}
	return out
}

// readHistory reads one recording; it fails when the file is not gzip, not a history, or empty.
func readHistory(t *testing.T, root *os.Root, name string) *historypb.History {
	t.Helper()
	fh, err := root.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := fh.Close(); err != nil {
			t.Error(err)
		}
	}()
	zr, err := gzip.NewReader(fh)
	if err != nil {
		t.Fatalf("%s: not gzip: %v", name, err)
	}
	h, err := client.HistoryFromJSON(zr, client.HistoryJSONOptions{})
	if err != nil {
		t.Fatalf("%s: not a workflow history: %v", name, err)
	}
	if len(h.Events) == 0 {
		t.Fatalf("%s: the history has no events", name)
	}
	return h
}

func replayLogger() log.Logger {
	return log.NewStructuredLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// reorderedRun is the planted defect: the production workflow's first two activities, in the other
// order (LookupReuse is scheduled before the run's first RecordEvent).
func reorderedRun(ctx workflow.Context, in durable.RunInput) (durable.RunResult, error) {
	_ = workflow.GetVersion(ctx, "run", workflow.DefaultVersion, 1)
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{StartToCloseTimeout: time.Minute})
	_ = workflow.ExecuteActivity(ctx, durable.LookupReuse, nil).Get(ctx, nil)
	_ = workflow.ExecuteActivity(ctx, durable.RecordEvent, nil).Get(ctx, nil)
	return durable.RunResult{}, nil
}

func TestReplayRecordedHistories(t *testing.T) {
	hs := recordedHistories(t)
	for f, h := range hs {
		t.Run(filepath.Base(f), func(t *testing.T) {
			t.Logf("examined %s: %d events", f, len(h.Events))
			r := temporalworker.NewWorkflowReplayer()
			r.RegisterWorkflowWithOptions(durable.Run, workflow.RegisterOptions{Name: "Run"})
			if err := r.ReplayWorkflowHistory(replayLogger(), h); err != nil {
				t.Fatalf("replay of %s failed: %v", f, err)
			}
		})
	}
}

func TestReplayPlantedReorderFails(t *testing.T) {
	hs := recordedHistories(t)
	for f, h := range hs {
		t.Run(filepath.Base(f), func(t *testing.T) {
			r := temporalworker.NewWorkflowReplayer()
			r.RegisterWorkflowWithOptions(reorderedRun, workflow.RegisterOptions{Name: "Run"})
			err := r.ReplayWorkflowHistory(replayLogger(), h)
			if err == nil {
				t.Fatalf("replay of %s against a workflow with a reordered activity passed", f)
			}
			if !strings.Contains(err.Error(), "TMPRL1100") && !strings.Contains(strings.ToLower(err.Error()), "nondeterministic") { // TMPRL1100: the SDK's nondeterminism code
				t.Fatalf("replay of %s failed, but not as a nondeterminism error: %v", f, err)
			}
			t.Logf("planted error for %s: %v", filepath.Base(f), err)
		})
	}
}
