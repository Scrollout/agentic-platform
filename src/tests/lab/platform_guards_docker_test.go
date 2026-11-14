//go:build docker

// T054i, from the fresh review: guards the first tests left open. The check service mounts the
// projects folder read-only and the project worker read-write with its records on a volume
// (compose.yaml); and the sweep finds a lab resource by its name alone, as the replay server's
// container carries no lab label (replay.sh is unchanged, T054f).
package lab

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"testing"
)

type mount struct {
	Type, Source, Destination string
	RW                        bool
}

func mountsOf(t testing.TB, container string) map[string]mount {
	t.Helper()
	var ms []mount
	if err := json.Unmarshal([]byte(Docker(t, "inspect", "-f", "{{json .Mounts}}", container)), &ms); err != nil {
		t.Fatal(err)
	}
	out := map[string]mount{}
	for _, m := range ms {
		out[m.Destination] = m
	}
	return out
}

// TestBuildServicesMounts: the check service's /projects is read-only, the project worker's
// read-write over the same host folder, and the project worker's records are on a writable volume.
func TestBuildServicesMounts(t *testing.T) {
	l := Start(t, Build())
	c, p := mountsOf(t, l.Check), mountsOf(t, l.ProjectWorker)
	cp, ok := c["/projects"]
	if !ok || cp.Type != "bind" || cp.RW {
		t.Errorf("the check service's /projects is %+v (present %v), want a read-only bind mount", cp, ok)
	}
	pp, ok := p["/projects"]
	if !ok || pp.Type != "bind" || !pp.RW {
		t.Errorf("the project worker's /projects is %+v (present %v), want a read-write bind mount", pp, ok)
	}
	if cp.Source == "" || cp.Source != pp.Source {
		t.Errorf("the check service mounts %q and the project worker %q, want the same projects folder", cp.Source, pp.Source)
	}
	if st, ok := p["/var/lib/project-worker"]; !ok || st.Type != "volume" || !st.RW {
		t.Errorf("the project worker's /var/lib/project-worker is %+v (present %v), want a writable volume", st, ok)
	}
	if _, ok := c["/var/lib/project-worker"]; ok {
		t.Error("the check service mounts the project worker's records")
	}
}

// TestStartSweepsUnlabelledByName: an unlabelled container named as a lab's replay server of a
// process that no longer runs is removed by the next lab.Start; one of a running process is kept.
func TestStartSweepsUnlabelledByName(t *testing.T) {
	dead := exec.Command("true")
	if err := dead.Run(); err != nil {
		t.Fatal(err)
	}
	live := exec.Command("sleep", "3600")
	if err := live.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { live.Process.Kill(); live.Wait() })
	plant := func(pid int) string {
		name := fmt.Sprintf("aptest-lab-replay-%d-1", pid)
		t.Cleanup(func() { Run("docker", "rm", "-f", name) })
		Docker(t, "create", "--name", name, "--network", "none", Pin(t, "alpine"), "true")
		return name
	}
	d, k := plant(dead.Process.Pid), plant(live.Process.Pid)

	Start(t)

	if exists("container", d) {
		t.Errorf("lab.Start left the unlabelled %s of the ended process %d", d, dead.Process.Pid)
	}
	if !exists("container", k) {
		t.Errorf("lab.Start removed the unlabelled %s of the running process %d", k, live.Process.Pid)
	}
}
