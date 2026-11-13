package checks

// What the step checks, the fixed procedures (engine/procedures) and the check service
// (engine/checksvc) share, each with this one home: the catalogue's glob dialect, the rule that a
// reason names project paths only, and the environment of every command they run.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// MatchGlob is the catalogue's glob dialect: "dir/**" matches every path below dir; any other
// entry is one exact path.
func MatchGlob(globs []string, p string) bool {
	for _, g := range globs {
		if dir, ok := strings.CutSuffix(g, "/**"); ok {
			if strings.HasPrefix(p, dir+"/") {
				return true
			}
		} else if g == p {
			return true
		}
	}
	return false
}

// HidePaths rewrites a tool's output so it names paths relative to dir only: dir's absolute path
// (as given, and with symbolic links resolved) is dropped and every other absolute path is
// replaced by `<host path>`.
func HidePaths(out, dir string) string {
	for _, d := range projectDirs(dir) {
		out = strings.ReplaceAll(out, d+"/", "")
		out = strings.ReplaceAll(out, d, ".")
	}
	return hostPath.ReplaceAllString(out, "${1}<host path>")
}

var hostPath = regexp.MustCompile(`(^|[\s'"(=:;])/[^\s'"():,;]+`)

// projectDirs is the folder's absolute path, and the same with symbolic links resolved when it
// differs: the two ways a go command may print it.
func projectDirs(dir string) []string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil
	}
	dirs := []string{abs}
	if real, err := filepath.EvalSymlinks(abs); err == nil && real != abs {
		dirs = append(dirs, real)
	}
	return dirs
}

// hostVariables are the only variables of this process's environment a command gets: where the
// tools and the Go caches are and the Go settings a host may give. No token and no setting of the
// engine or the check service (ENGINE_*, CHECK_*, AP_*) reaches code a worker wrote.
var hostVariables = []string{
	"PATH", "HOME", "TMPDIR", "XDG_CACHE_HOME", "GOROOT", "GOPATH", "GOCACHE", "GOMODCACHE", "GOFLAGS", "CGO_ENABLED",
}

// OfflineGo is what every go command is fixed to: no module proxy, no workspace, the local
// toolchain only. It comes last in CommandEnv, so nothing overrides it.
var OfflineGo = []string{"GOPROXY=off", "GOWORK=off", "GOTOOLCHAIN=local"}

// CommandEnv is the environment of a command a check or a procedure runs: the hostVariables this
// process has, then extra in order (a later entry wins), then OfflineGo.
func CommandEnv(extra ...string) []string {
	var env []string
	for _, name := range hostVariables {
		if v, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+v)
		}
	}
	env = append(env, extra...)
	return append(env, OfflineGo...)
}
