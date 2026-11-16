package checks

// What the step checks, the fixed procedures (engine/procedures) and the check service
// (engine/checksvc) share, each with this one home: the catalogue's glob dialect, the rule that a
// reason names project paths only, and the environment of every command they run.

import (
	"fmt"
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

// NotOwn is t without the files that writable matches (MatchGlob's dialect: "dir/**" is every
// path below dir, any other entry one exact path). An earlier step's file that the judged step may
// write (the seam internal/app/app.go, which generate writes and code replaces) is the judged
// step's to change, so the "unchanged" checks judge only the others.
func NotOwn(t Tree, writable []string) Tree {
	out := Tree{}
	for p, h := range t {
		if !MatchGlob(writable, p) {
			out[p] = h
		}
	}
	return out
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
// process has, then extra in order (a later entry wins, so a call's own variables, CallFolder.Env,
// given last win over the host's), then OfflineGo.
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

// CallFolder is one call's own new, empty temporary folder below os.TempDir() (research D37, ADR
// 0041): every command of the call has GOCACHE, GOTMPDIR, TMPDIR, HOME and GOPATH inside it, and
// every scratch folder of the call is made in it (Scratch), so nothing a check writes there
// survives the call. The module cache is the one exception: it is resolved before the call sets
// its own GOPATH and passed as GOMODCACHE, outside the folder, shared and read-only in the image.
type CallFolder struct {
	dir string
	env []string
}

// callDirs are the folders a call's variables name, below its folder.
var callDirs = []struct{ name, dir string }{
	{"GOCACHE", "gocache"}, {"GOTMPDIR", "tmp"}, {"TMPDIR", "tmp"}, {"HOME", "home"}, {"GOPATH", "gopath"},
}

// NewCallFolder makes a call's folder. extra is the environment the call's commands are given
// besides the host's (a later entry wins); the module cache is its last GOMODCACHE, else the
// first entry of its last GOPATH with /pkg/mod, else the same read from this process, else go's
// default $HOME/go/pkg/mod. cacheBase, when not empty, is the read-only warm base cache (the
// image's /opt/gocache-base): its entries are copied into the call's GOCACHE, as files of the
// call's own, so the call starts warm and nothing it writes reaches the base or another call. A
// cacheBase that is not an existing folder is an error and no folder is left: an image whose base
// is missing is built wrong, and the call runs nothing. The caller removes the folder with Remove
// when the call ends.
func NewCallFolder(extra []string, cacheBase string) (*CallFolder, error) {
	modcache := ModuleCache(extra)
	if cacheBase != "" {
		if fi, err := os.Stat(cacheBase); err != nil || !fi.IsDir() {
			return nil, fmt.Errorf("the base Go cache %s is not an existing folder", cacheBase)
		}
	}
	dir, err := os.MkdirTemp("", "check-call-")
	if err != nil {
		return nil, err
	}
	c := &CallFolder{dir: dir}
	for _, v := range callDirs {
		p := filepath.Join(dir, v.dir)
		if err := os.MkdirAll(p, 0o700); err != nil {
			c.Remove()
			return nil, err
		}
		c.env = append(c.env, v.name+"="+p)
	}
	if cacheBase != "" {
		// os.CopyFS gives every copy a new modification time and owner-writable modes, so go's
		// cache trimming keeps the entries and a check may use and replace them in its own copy.
		if err := os.CopyFS(filepath.Join(dir, "gocache"), os.DirFS(cacheBase)); err != nil {
			c.Remove()
			return nil, fmt.Errorf("the base Go cache cannot be copied into the call's folder: %w", err)
		}
	}
	if modcache != "" {
		c.env = append(c.env, "GOMODCACHE="+modcache)
	}
	return c, nil
}

// Dir is the call's folder.
func (c *CallFolder) Dir() string { return c.dir }

// Env is the call's variables, to be given after every other entry so that they win.
func (c *CallFolder) Env() []string { return append([]string{}, c.env...) }

// TempDir is the call's TMPDIR, where its scratch folders are made.
func (c *CallFolder) TempDir() string { return filepath.Join(c.dir, "tmp") }

// Scratch makes a new scratch folder inside the call's folder (its TMPDIR).
func (c *CallFolder) Scratch(prefix string) (string, error) {
	return os.MkdirTemp(c.TempDir(), prefix)
}

// Remove removes the call's folder and everything in it, also what a test made read-only.
func (c *CallFolder) Remove() error {
	if err := os.RemoveAll(c.dir); err == nil {
		return nil
	}
	filepath.WalkDir(c.dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			os.Chmod(p, 0o700)
		}
		return nil
	})
	return os.RemoveAll(c.dir)
}

// ModuleCache is the module cache the commands given extra use: extra's last GOMODCACHE, else the
// first entry of its last GOPATH with /pkg/mod, else the same from this process's environment,
// else $HOME/go/pkg/mod (go's default GOPATH). Empty when none can be found.
func ModuleCache(extra []string) string {
	lookup := func(name string) (string, bool) {
		for i := len(extra) - 1; i >= 0; i-- {
			if v, ok := strings.CutPrefix(extra[i], name+"="); ok {
				return v, true
			}
		}
		return os.LookupEnv(name)
	}
	if m, _ := lookup("GOMODCACHE"); m != "" {
		return m
	}
	if p, _ := lookup("GOPATH"); p != "" {
		if first, _, _ := strings.Cut(p, string(os.PathListSeparator)); first != "" {
			return filepath.Join(first, "pkg", "mod")
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, "go", "pkg", "mod")
	}
	return ""
}

// Scratcher is a GoRunner whose call has its own folder for scratch folders (the check service's
// runner, CallFolder.Scratch).
type Scratcher interface {
	Scratch(prefix string) (string, error)
}

// scratch makes a check's scratch folder: inside the runner's call folder when it has one, else
// a new folder in os.TempDir().
func scratch(r GoRunner, prefix string) (string, error) {
	if s, ok := r.(Scratcher); ok {
		return s.Scratch(prefix)
	}
	return os.MkdirTemp("", prefix)
}
