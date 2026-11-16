//go:build docker

package recorded

// T110(a): replay.sh start prints only the container name, as its header says, even when the
// pinned WireMock image is not in the local store (a fresh runner): the pull must be quiet.

import (
	"strings"
	"testing"
)

func TestStartPrintsOnlyTheNameWhenTheImageIsNotLocal(t *testing.T) {
	net := network(t, true)
	img := image(t)
	// Put the image back however this test ends, so later tests do not need the network.
	t.Cleanup(func() { run("docker", "pull", "-q", img) })
	if out, code := run("docker", "image", "rm", img); code != 0 && !strings.Contains(out, "No such image") {
		t.Fatalf("docker image rm %s: exit %d: %s", img, code, out)
	}
	name := uniq("aptest-pull")
	t.Cleanup(func() { run("docker", "rm", "-f", name) })
	out, code := replaySh(nil, "start", net, name)
	if code != 0 || out != name {
		t.Fatalf("start with the image missing: exit %d, output %q; want exit 0 and exactly %q", code, out, name)
	}
}
