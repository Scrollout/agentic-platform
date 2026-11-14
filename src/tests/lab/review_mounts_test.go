package lab

// T076: the review step's mount and the skeleton with review/ (research D35, ADR 0039), written
// before the change by a separate author; runs without Docker. The review container sees the
// project read-only with review/ writable on top and `.git` read-only; every other step sees
// review/ read-only, generate included, whose project root is writable (an explicit read-only
// mount of review/ on top of the root); the skeleton holds review/.

import (
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

func TestReviewMountsReviewWritable(t *testing.T) {
	root := repoRoot(t)
	got, err := StepMounts(root, "/host/projects", "notes")
	if err != nil {
		t.Fatal(err)
	}
	pdir, inside := filepath.Join("/host/projects", "notes"), "/projects/notes"
	want := []Mount{
		{pdir, inside, true},
		{filepath.Join(pdir, ".git"), inside + "/.git", true},
		{filepath.Join(pdir, "review"), inside + "/review", false},
	}
	if g := byTarget(got["review"]); !reflect.DeepEqual(g, byTarget(want)) {
		t.Errorf("review mounts\n %+v\nwant\n %+v", g, byTarget(want))
	}
}

// Every step but the review sees review/ read-only: no writable mount covers it.
func TestOtherStepsSeeReviewReadOnly(t *testing.T) {
	root := repoRoot(t)
	got, err := StepMounts(root, "/host/projects", "notes")
	if err != nil {
		t.Fatal(err)
	}
	target := "/projects/notes/review/r1/a_test.go"
	for name, ms := range got {
		if name == "review" {
			continue
		}
		// The innermost mount holding the path decides (the longest target that is a prefix).
		var best *Mount
		for i, m := range ms {
			if target == m.Target || len(target) > len(m.Target) && target[:len(m.Target)+1] == m.Target+"/" {
				if best == nil || len(m.Target) > len(best.Target) {
					best = &ms[i]
				}
			}
		}
		if best == nil || !best.ReadOnly {
			t.Errorf("%s can write under review/: %+v", name, ms)
		}
	}
}

func TestSkeletonHoldsReview(t *testing.T) {
	got, err := SkeletonFolders(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(got, "review") {
		t.Errorf("the skeleton %v lacks review", got)
	}
}
