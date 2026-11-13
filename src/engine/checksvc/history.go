package checksvc

// The project worker (research D30, ADR 0032): commitStep records an accepted step as one git
// commit on the branch `main` in the project's `.git`, and restoreStep makes the project folder
// hold its last commit's files again before an attempt. The contract's info.description (Version
// history, The project worker's record) and the two operations give the rules; this file follows
// them in their order.
//
// Git runs only as the plumbing below, with every option fixed here (gitOptions, gitEnv): no
// system or global configuration, no replace objects, no fsmonitor, no hooks, no automatic gc, no
// signing. Blobs are written with `hash-object --no-filters`, so no attribute, filter or line-ending
// rule of the project changes what is committed; paths reach git only on standard input. Before
// any git command runs over a project with a record, the history guard (inspect) reads `.git` as
// files and compares it with the record this worker keeps in Config.History.

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha1" //nolint:gosec // git's object IDs, not a security hash
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/agentic-platform/check/api/gen"
	"github.com/agentic-platform/engine/canonical"
	"github.com/agentic-platform/engine/checks"
	"github.com/agentic-platform/engine/procedures"
)

// The commit identity (contract, commitStep): author and committer of every commit.
const (
	commitName  = "agentic-platform"
	commitEmail = "noreply@localhost"
	branchRef   = "refs/heads/main"
	headContent = "ref: " + branchRef + "\n"
	zeroID      = "0000000000000000000000000000000000000000"
)

// gitOptions come before every git command.
var gitOptions = []string{
	"-c", "core.fsmonitor=false",
	"-c", "core.hooksPath=/dev/null",
	"-c", "gc.auto=0",
	"-c", "commit.gpgSign=false",
}

// Files in `.git` that make git read objects or history from elsewhere; the guard refuses each.
var foreignGitFiles = []string{"commondir", "gitdir", "shallow", "info/grafts", "objects/info/alternates"}

var objectID = regexp.MustCompile(`^[0-9a-f]{40}$`)

func notServed() error {
	return fault(404, gen.ErrorCodeVersionHistoryNotServed, "this deployment does not keep version histories; the project worker does")
}

func historyChanged(format string, a ...any) error {
	return fault(409, gen.ErrorCodeHistoryChanged, "the project's version history is not the one the project worker made: "+fmt.Sprintf(format, a...))
}

func stateDiffers(format string, a ...any) error {
	return fault(409, gen.ErrorCodeProjectStateDiffers, fmt.Sprintf(format, a...))
}

func historyWriteFailed(format string, a ...any) error {
	return fault(500, gen.ErrorCodeWriteFailed, fmt.Sprintf(format, a...))
}

// ---- the commit a request names ----

// stamp is what a commit's trailers name.
type stamp struct {
	Run        string `json:"run"`
	Step       string `json:"step"`
	Round      *int   `json:"round,omitempty"`
	Attempt    *int   `json:"attempt,omitempty"`
	ReusedFrom string `json:"reused_from,omitempty"`
}

// message is the commit message for the stamp (contract, commitStep): a subject, an empty line,
// then the trailers, each line ending in a line feed. It is a function of the stamp alone, so two
// commits have the same trailers exactly when they have the same message.
func (t stamp) message() string {
	var b strings.Builder
	if t.ReusedFrom != "" {
		fmt.Fprintf(&b, "Step %s reused in %s\n\n", t.Step, t.Run)
	} else {
		fmt.Fprintf(&b, "Step %s accepted in %s\n\n", t.Step, t.Run)
	}
	fmt.Fprintf(&b, "Run: %s\nStep: %s\n", t.Run, t.Step)
	if t.Round != nil {
		fmt.Fprintf(&b, "Round: %d\n", *t.Round)
	}
	if t.ReusedFrom != "" {
		fmt.Fprintf(&b, "Reused-from: %s\n", t.ReusedFrom)
	} else if t.Attempt != nil {
		fmt.Fprintf(&b, "Attempt: %d\n", *t.Attempt)
	}
	return b.String()
}

// wanted is the commit a commitStep request makes, apart from its tree: the parent's files
// (before), the commit's files (project_files), its message and its date.
type wanted struct {
	stamp  stamp
	before gen.ProjectFiles
	files  gen.ProjectFiles
	date   int64
}

// ident is the author or committer line's value for the date.
func ident(date int64) string {
	return fmt.Sprintf("%s <%s> %d +0000", commitName, commitEmail, date)
}

// ---- the record ----

// record is the project worker's record of a project's last commit, kept outside the project.
// Commit empty is the provisional record written right after the repository was created. A
// claimed record (ClaimedAt set, everything else empty but Project) is the one a passing first
// touch writes before anything else (FR-025); the no-record rules apply to it.
type record struct {
	Commit string `json:"commit"`
	Parent string `json:"parent"`
	stamp
	Config    string `json:"config_sha256"`
	Project   string `json:"project,omitempty"`
	ClaimedAt string `json:"claimed_at,omitempty"`
}

func (r record) claimed() bool { return r.ClaimedAt != "" }

func (s *service) recordPath(project string) string {
	return filepath.Join(s.cfg.History, project+".json")
}

// loadRecord reads the project's record; nil when it has none. Only the record's own file is read,
// so a temporary file a stopped write left beside it changes nothing.
func (s *service) loadRecord(project string) (*record, error) {
	b, err := readNoFollow(s.recordPath(project), 1<<20)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, nil
	case err != nil:
		return nil, unreadable("the version history of %s cannot be read: its record cannot be read", project)
	}
	var r record
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	bad := dec.Decode(&r) != nil
	if r.claimed() {
		_, terr := time.Parse(time.RFC3339, r.ClaimedAt)
		bad = bad || terr != nil || r.Project != project || r.Commit != "" || r.Parent != "" || r.Config != "" ||
			r.stamp.message() != (stamp{}).message()
	} else {
		bad = bad || r.Project != "" || (r.Commit != "" && !objectID.MatchString(r.Commit)) ||
			(r.Parent != "" && !objectID.MatchString(r.Parent)) || (r.Commit == "" && r.Parent != "") || len(r.Config) != 64
	}
	if bad {
		return nil, historyChanged("the project worker's record of %s is not one it wrote", project)
	}
	return &r, nil
}

// firstTouch applies the FR-025 guard to a run's first touch of a project with no record
// (restoreStep for attempt 1, commitStep with reused_from) and, when it passes, claims the
// project: the claimed record is written atomically before anything else changes. It returns
// the record the call goes on with: nil for no record or a claimed one, which follow the same
// no-record rules.
func (s *service) firstTouch(dir, project string, rec *record, first bool) (*record, error) {
	if rec == nil && first {
		if err := newProject(dir, project); err != nil {
			return nil, err
		}
		claim := record{Project: project, ClaimedAt: time.Now().UTC().Format(time.RFC3339)}
		if err := s.saveRecord(project, claim); err != nil {
			return nil, historyWriteFailed("the version history of %s could not be written", project)
		}
		return nil, nil
	}
	if rec != nil && rec.claimed() {
		return nil, nil
	}
	return rec, nil
}

// saveRecord replaces the project's record atomically: a temporary file in the same folder,
// written and synced, then renamed over the old record, and the folder synced.
func (s *service) saveRecord(project string, r record) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(s.cfg.History, "."+project+".*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(append(b, '\n'))
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, s.recordPath(project))
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if d, err := os.Open(s.cfg.History); err == nil {
		err = d.Sync()
		_ = d.Close()
		return err
	}
	return nil
}

// ---- one project at a time ----

// lockProject waits for the project's lock, or for ctx to end.
func (s *service) lockProject(ctx context.Context, project string) (func(), error) {
	v, _ := s.locks.LoadOrStore(project, make(chan struct{}, 1))
	ch := v.(chan struct{})
	select {
	case ch <- struct{}{}:
		return func() { <-ch }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// ---- running git ----

// repo runs git over one project's `.git`, each command in its own process group, killed whole
// when ctx ends.
type repo struct {
	ctx     context.Context
	bin     string
	dir     string // the project folder, every command's working folder
	gitDir  string
	date    int64 // the commit date; also the reflog's
	objects *objectReader
}

func (s *service) repo(ctx context.Context, dir string, date int64) *repo {
	bin := s.cfg.Git
	if bin == "" {
		bin = "git"
	}
	return &repo{ctx: ctx, bin: bin, dir: dir, gitDir: filepath.Join(dir, gitFolder), date: date}
}

// env is the whole environment of every git command; nothing of the process's own is passed but
// PATH. GIT_DIR names the repository, so git never searches for one.
func (r *repo) env() []string {
	date := fmt.Sprintf("%d +0000", r.date)
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"LC_ALL=C",
		"TZ=UTC",
		"GIT_DIR=" + r.gitDir,
		"GIT_NO_REPLACE_OBJECTS=1",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_ATTR_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_OPTIONAL_LOCKS=0",
		"GIT_AUTHOR_NAME=" + commitName, "GIT_AUTHOR_EMAIL=" + commitEmail, "GIT_AUTHOR_DATE=" + date,
		"GIT_COMMITTER_NAME=" + commitName, "GIT_COMMITTER_EMAIL=" + commitEmail, "GIT_COMMITTER_DATE=" + date,
	}
}

func (r *repo) command(args ...string) *exec.Cmd {
	cmd := exec.CommandContext(r.ctx, r.bin, append(append([]string{}, gitOptions...), args...)...)
	cmd.Dir = r.dir
	cmd.Env = r.env()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 2 * time.Second
	return cmd
}

// run runs one git command with stdin, and returns its standard output.
func (r *repo) run(stdin []byte, args ...string) ([]byte, error) {
	cmd := r.command(args...)
	cmd.Stdin = bytes.NewReader(stdin)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	if r.ctx.Err() != nil {
		return nil, r.ctx.Err()
	}
	if err != nil {
		return nil, fmt.Errorf("git %s: %v: %s", args[0], err, strings.TrimSpace(errOut.String()))
	}
	return out.Bytes(), nil
}

func (r *repo) close() {
	if r.objects != nil {
		r.objects.close()
		r.objects = nil
	}
}

// objectReader reads objects through one `git cat-file --batch`.
type objectReader struct {
	cmd  *exec.Cmd
	in   io.WriteCloser
	out  *bufio.Reader
	sums map[string]string // blob ID -> SHA-256 of its bytes
}

var errMissing = errors.New("object missing")

func (r *repo) reader() (*objectReader, error) {
	if r.objects != nil {
		return r.objects, nil
	}
	cmd := r.command("cat-file", "--batch")
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	r.objects = &objectReader{cmd: cmd, in: in, out: bufio.NewReader(out), sums: map[string]string{}}
	return r.objects, nil
}

func (o *objectReader) close() {
	_ = o.in.Close()
	_ = syscall.Kill(-o.cmd.Process.Pid, syscall.SIGKILL)
	_ = o.cmd.Wait()
}

// read returns an object's type and bytes; at most max bytes are accepted.
func (r *repo) read(id string, max int64) (string, []byte, error) {
	if !objectID.MatchString(id) {
		return "", nil, errMissing
	}
	o, err := r.reader()
	if err != nil {
		return "", nil, err
	}
	if _, err := io.WriteString(o.in, id+"\n"); err != nil {
		return "", nil, err
	}
	line, err := o.out.ReadString('\n')
	if err != nil {
		return "", nil, err
	}
	f := strings.Fields(line)
	if len(f) == 2 && f[1] == "missing" {
		return "", nil, errMissing
	}
	if len(f) != 3 || f[0] != id {
		return "", nil, errors.New("git cat-file gave an unexpected answer")
	}
	size, err := strconv.ParseInt(f[2], 10, 64)
	if err != nil || size < 0 {
		return "", nil, errors.New("git cat-file gave an unexpected size")
	}
	if size > max {
		// The object is never read; the reader cannot go on.
		r.close()
		return "", nil, fmt.Errorf("the object %s is larger than %d bytes", id, max)
	}
	body := make([]byte, size+1)
	if _, err := io.ReadFull(o.out, body); err != nil {
		return "", nil, err
	}
	return f[1], body[:size], nil
}

// commitObject is a commit's parts as git stores them.
type commitObject struct {
	raw     []byte
	tree    string
	parents []string
	message string
}

func (r *repo) commit(id string) (*commitObject, error) {
	typ, b, err := r.read(id, 1<<20)
	if err != nil {
		return nil, err
	}
	if typ != "commit" {
		return nil, errMissing
	}
	c := &commitObject{raw: b}
	head, msg, ok := bytes.Cut(b, []byte("\n\n"))
	if !ok {
		return nil, errors.New("a commit without a message")
	}
	c.message = string(msg)
	for _, line := range strings.Split(string(head), "\n") {
		if v, ok := strings.CutPrefix(line, "tree "); ok {
			c.tree = v
		} else if v, ok := strings.CutPrefix(line, "parent "); ok {
			c.parents = append(c.parents, v)
		}
	}
	return c, nil
}

// files lists a tree's files, by path, with their blob IDs. plain is false when the tree holds
// anything but non-executable regular files and folders (a link, an executable, a submodule).
func (r *repo) files(tree string) (map[string]string, bool, error) {
	out := map[string]string{}
	plain := true
	var walk func(id, prefix string) error
	walk = func(id, prefix string) error {
		typ, b, err := r.read(id, 64<<20)
		if err != nil {
			return err
		}
		if typ != "tree" {
			return errors.New("not a tree")
		}
		for len(b) > 0 {
			sp := bytes.IndexByte(b, ' ')
			nul := bytes.IndexByte(b, 0)
			if sp < 0 || nul < sp || len(b) < nul+21 {
				return errors.New("a tree that cannot be read")
			}
			mode, name := string(b[:sp]), string(b[sp+1:nul])
			child := hex.EncodeToString(b[nul+1 : nul+21])
			b = b[nul+21:]
			path := prefix + name
			switch mode {
			case "40000":
				if err := walk(child, path+"/"); err != nil {
					return err
				}
			case "100644":
				if len(out) > checkContract.limits.ProjectFilesMax {
					return errors.New("a tree with too many files")
				}
				out[path] = child
			default:
				plain = false
			}
		}
		return nil
	}
	if err := walk(tree, ""); err != nil {
		return nil, false, err
	}
	return out, plain, nil
}

// blobSum is the SHA-256 of a blob's bytes.
func (r *repo) blobSum(id string, max int64) (string, error) {
	if o, _ := r.reader(); o != nil {
		if h, ok := o.sums[id]; ok {
			return h, nil
		}
	}
	b, err := r.blob(id, max)
	if err != nil {
		return "", err
	}
	h := sum(b)
	if r.objects != nil {
		r.objects.sums[id] = h
	}
	return h, nil
}

func (r *repo) blob(id string, max int64) ([]byte, error) {
	typ, b, err := r.read(id, max)
	if err != nil {
		return nil, err
	}
	if typ != "blob" {
		return nil, errMissing
	}
	return b, nil
}

// holds reports whether a tree holds exactly the files listed, each a non-executable regular file
// with its hash; diff names the first path that differs.
func (r *repo) holds(tree string, list gen.ProjectFiles, max int64) (ok bool, diff string, err error) {
	files, plain, err := r.files(tree)
	if err != nil {
		return false, "", err
	}
	if !plain {
		return false, "an entry that is not a non-executable regular file", nil
	}
	want := map[string]string{}
	for _, f := range list {
		want[f.Path] = f.HashValue
	}
	for _, p := range sortedKeys(files) {
		h, listed := want[p]
		if !listed {
			return false, p, nil
		}
		got, err := r.blobSum(files[p], max)
		if err != nil {
			return false, "", err
		}
		if got != h {
			return false, p, nil
		}
	}
	for _, f := range list {
		if _, there := files[f.Path]; !there {
			return false, f.Path, nil
		}
	}
	return true, "", nil
}

// makes reports whether the commit id is exactly the commit w makes over parent: its parent, its
// author, committer, dates and message, its tree holding w.files and its parent's tree w.before
// (none before the first commit).
func (r *repo) makes(id, parent string, w wanted, max int64) (bool, error) {
	c, err := r.commit(id)
	if err != nil {
		return false, err
	}
	var want bytes.Buffer
	fmt.Fprintf(&want, "tree %s\n", c.tree)
	if parent != "" {
		fmt.Fprintf(&want, "parent %s\n", parent)
	}
	fmt.Fprintf(&want, "author %s\ncommitter %s\n\n%s", ident(w.date), ident(w.date), w.stamp.message())
	if !bytes.Equal(c.raw, want.Bytes()) {
		return false, nil
	}
	if ok, _, err := r.holds(c.tree, w.files, max); err != nil || !ok {
		return false, err
	}
	if parent == "" {
		return len(w.before) == 0, nil
	}
	pc, err := r.commit(parent)
	if err != nil {
		return false, err
	}
	ok, _, err := r.holds(pc.tree, w.before, max)
	return ok, err
}

// ---- the history guard ----

// inspect is the history check's first part, on `.git` as files only, before any git command
// runs: with a record, it returns what the branch names ("" when it is missing), or 409
// HistoryChanged.
func inspect(dir string, rec *record) (string, error) {
	gd := filepath.Join(dir, gitFolder)
	fi, err := os.Lstat(gd)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "", historyChanged(".git is missing")
	case err != nil:
		return "", unreadable("the project's version history cannot be read")
	case !fi.IsDir():
		return "", historyChanged(".git is not a folder")
	}
	err = filepath.WalkDir(gd, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&fs.ModeSymlink != 0 {
			rel, _ := filepath.Rel(dir, path)
			return historyChanged("%s is a symbolic link", filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		var p *problemError
		if errors.As(err, &p) {
			return "", err
		}
		return "", unreadable("the project's version history cannot be read")
	}
	for _, name := range foreignGitFiles {
		if _, err := os.Lstat(filepath.Join(gd, filepath.FromSlash(name))); !errors.Is(err, fs.ErrNotExist) {
			return "", historyChanged(".git/%s exists", name)
		}
	}
	if b, err := readNoFollow(filepath.Join(gd, "HEAD"), 4096); err != nil || string(b) != headContent {
		return "", historyChanged(".git/HEAD is not %q", headContent)
	}
	if b, err := readNoFollow(filepath.Join(gd, "config"), 1<<20); err != nil || sum(b) != rec.Config {
		return "", historyChanged(".git/config differs from the one the project worker wrote")
	}
	switch b, err := readNoFollow(filepath.Join(gd, "packed-refs"), 64<<20); {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return "", historyChanged(".git/packed-refs cannot be read")
	default:
		for _, line := range strings.Split(string(b), "\n") {
			if f := strings.Fields(line); len(f) >= 2 && f[1] == branchRef {
				return "", historyChanged(".git/packed-refs names %s", branchRef)
			}
		}
	}
	switch b, err := readNoFollow(filepath.Join(gd, filepath.FromSlash(branchRef)), 4096); {
	case errors.Is(err, fs.ErrNotExist):
		return "", nil
	case err != nil:
		return "", historyChanged("the branch main cannot be read")
	default:
		id := strings.TrimSuffix(string(b), "\n")
		if len(b) != 41 || !objectID.MatchString(id) {
			return "", historyChanged("the branch main does not name a commit")
		}
		return id, nil
	}
}

// guard runs the whole history check over a project with a record and returns its last commit
// ("" before the first). w is the commit the request makes; nil for restoreStep, which makes
// none.
func (s *service) guard(r *repo, rec *record, w *wanted) (string, error) {
	branch, err := inspect(r.dir, rec)
	if err != nil {
		return "", err
	}
	if rec.Commit == "" {
		if branch != "" {
			return "", historyChanged("the branch main exists, but the project worker made no commit yet")
		}
		return "", nil
	}
	c, err := r.commit(rec.Commit)
	switch {
	case errors.Is(err, errMissing):
		return "", historyChanged("the recorded commit %s is missing", rec.Commit)
	case err != nil:
		return "", unreadable("the project's version history cannot be read")
	}
	wantParents := []string{}
	if rec.Parent != "" {
		wantParents = []string{rec.Parent}
	}
	if strings.Join(c.parents, " ") != strings.Join(wantParents, " ") {
		return "", historyChanged("the recorded commit %s has another parent", rec.Commit)
	}
	if c.message != rec.stamp.message() {
		return "", historyChanged("the recorded commit %s has other trailers", rec.Commit)
	}
	switch {
	case branch == rec.Commit:
		return branch, nil
	case branch == rec.Parent && w != nil:
		// A call stopped between recording the commit and moving the branch to it: only the
		// same request may finish it.
		ok, err := r.makes(rec.Commit, rec.Parent, *w, s.limits.ProjectFileMax)
		if err != nil {
			return "", unreadable("the project's version history cannot be read")
		}
		if !ok {
			return "", historyChanged("the branch main is at the parent of the recorded commit %s, which this request does not make", rec.Commit)
		}
		return branch, nil
	case branch == "":
		return "", historyChanged("the branch main is missing")
	default:
		return "", historyChanged("the branch main names %s, a commit the project worker did not record", branch)
	}
}

// ---- a new project (FR-025) ----

// newProject is the guard on a run's first touch of a project the worker has no record for
// (restoreStep for attempt 1, commitStep with reused_from): the folder may hold folders only,
// with no file or symbolic link at any depth and a missing or empty `.git`; otherwise 409
// ProjectExists naming the first path found, before anything changes. No link is followed.
func newProject(dir, name string) error {
	exists := func(why string) error {
		return fault(409, gen.ErrorCodeProjectExists, "the project "+name+" already exists: "+why+"; a run builds only into a new project")
	}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == dir {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		rel = filepath.ToSlash(rel)
		if rel == gitFolder && d.IsDir() {
			es, err := os.ReadDir(path)
			if err != nil {
				return err
			}
			if len(es) > 0 {
				return exists(".git holds " + es[0].Name())
			}
			return fs.SkipDir
		}
		if !d.IsDir() {
			return exists("it holds " + rel)
		}
		return nil
	})
	if err != nil {
		var p *problemError
		if errors.As(err, &p) {
			return err
		}
		return unreadable("the project folder %s cannot be read", name)
	}
	return nil
}

// ---- commitStep ----

func (s *service) CommitStep(ctx context.Context, req gen.CommitStepRequestObject) (gen.CommitStepResponseObject, error) {
	if s.cfg.History == "" {
		return nil, notServed()
	}
	body := *req.Body
	reused := body.ReusedFrom != nil
	switch {
	case (body.Attempt != nil) == reused:
		return nil, fault(400, gen.ErrorCodeInvalidRequest, "send exactly one of attempt and reused_from")
	case body.Outputs != nil && !reused:
		return nil, fault(400, gen.ErrorCodeInvalidRequest, "send outputs only with reused_from")
	}
	var outputs []gen.ReusedOutput
	if body.Outputs != nil {
		outputs = *body.Outputs
	}
	seen := map[string]bool{}
	for _, o := range outputs {
		if _, declared := outputEntries(body.Step.Outputs, body.Step.Writable, o.Name); !declared || seen[o.Name] {
			return nil, fault(400, gen.ErrorCodeInvalidRequest, "the output "+o.Name+" is not one the step "+body.Step.Name+" declares, or is sent twice")
		}
		seen[o.Name] = true
	}
	w := wanted{
		stamp:  stamp{Run: body.Run, Step: body.Step.Name, Round: body.Round, Attempt: body.Attempt},
		before: body.Before, files: body.ProjectFiles, date: body.AcceptedAt.Unix(),
	}
	if reused {
		w.stamp.ReusedFrom = *body.ReusedFrom
		w.stamp.Attempt = nil
	}
	return bounded(ctx, body.TimeLimitSeconds, func(ctx context.Context) (gen.CommitStepResponseObject, error) {
		unlock, err := s.lockProject(ctx, body.Project)
		if err != nil {
			return nil, err
		}
		defer unlock()
		v, err := s.commitStep(ctx, body, outputs, w)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, err
		}
		return gen.CommitStep200JSONResponse(v), nil
	})
}

func (s *service) commitStep(ctx context.Context, body gen.CommitRequest, outputs []gen.ReusedOutput, w wanted) (gen.ProjectVersion, error) {
	var none gen.ProjectVersion
	dir, err := s.projectDir(body.Project)
	if err != nil {
		return none, err
	}
	rec, err := s.loadRecord(body.Project)
	if err != nil {
		return none, err
	}
	if rec, err = s.firstTouch(dir, body.Project, rec, body.ReusedFrom != nil); err != nil {
		return none, err
	}
	r := s.repo(ctx, dir, w.date)
	defer r.close()
	max := s.limits.ProjectFileMax
	cantRead := func() error { return unreadable("the version history of %s cannot be read", body.Project) }

	// The history check, then (1) repeat and (2) starting state.
	head := ""
	if rec != nil {
		if head, err = s.guard(r, rec, &w); err != nil {
			return none, err
		}
	} else if fi, err := os.Lstat(filepath.Join(dir, gitFolder)); err == nil && !fi.IsDir() {
		return none, historyChanged(".git is not a folder")
	}
	if head != "" {
		c, err := r.commit(head)
		if err != nil {
			return none, cantRead()
		}
		if c.message == w.stamp.message() {
			parent := ""
			if len(c.parents) == 1 {
				parent = c.parents[0]
			}
			ok, err := r.makes(head, parent, w, max)
			switch {
			case err != nil:
				return none, cantRead()
			case !ok || len(c.parents) > 1:
				return none, stateDiffers("the last commit %s records this step with other files or another date", head)
			}
			return gen.ProjectVersion{Commit: head, Created: false, ProjectFiles: nonNilFiles(w.files)}, nil
		}
		ok, diff, err := r.holds(c.tree, w.before, max)
		switch {
		case err != nil:
			return none, cantRead()
		case !ok:
			return none, stateDiffers("the last commit does not hold the files before lists: %s differs", diff)
		}
	} else if len(w.before) > 0 {
		return none, stateDiffers("the project has no commit, so it holds none of the files before lists")
	}

	// (3) A reused result: its files are written as runProcedure writes a completed result.
	if body.ReusedFrom != nil {
		var files []canonical.File
		var size int64
		inOutput := map[string]bool{}
		for _, o := range outputs {
			if sum(o.Content) != o.HashValue {
				return none, fault(422, gen.ErrorCodeInputHashMismatch, "the output "+o.Name+"'s content does not have its sha256")
			}
		}
		for _, o := range outputs {
			entries, _ := outputEntries(body.Step.Outputs, body.Step.Writable, o.Name)
			fs, err := canonical.Unframe(o.Content)
			if err != nil {
				return none, fault(400, gen.ErrorCodeInvalidRequest, "the output "+o.Name+"'s content is not a framed form")
			}
			for _, f := range fs {
				if !procedures.IsProjectPath(f.Path) || !checks.MatchGlob(entries, f.Path) || inOutput[f.Path] {
					return none, fault(400, gen.ErrorCodeInvalidRequest, fmt.Sprintf("the output %s's file %q is not a project path of its writable entries, or is in two outputs", o.Name, f.Path))
				}
				inOutput[f.Path] = true
				size += int64(len(f.Content))
				files = append(files, f)
			}
		}
		if size > s.limits.StepFilesMax {
			return none, fault(400, gen.ErrorCodeInvalidRequest, fmt.Sprintf("the outputs hold %d bytes of files, more than %d", size, s.limits.StepFilesMax))
		}
		p, err := readProject(ctx, dir, body.Project, s.limits)
		if err != nil {
			return none, err
		}
		if err := p.strict(body.Project); err != nil {
			return none, err
		}
		if path, err := writeOutputs(ctx, p, body.Step.Writable, files); err != nil {
			if ctx.Err() != nil {
				return none, ctx.Err()
			}
			return none, historyWriteFailed("writing or removing %s failed; a repeat converges the files", path)
		}
	}

	// (4) Ending state.
	p, err := readProject(ctx, dir, body.Project, s.limits)
	if err != nil {
		return none, err
	}
	if err := p.strict(body.Project); err != nil {
		return none, err
	}
	if diff := firstDiff(p.list(), w.files); diff != "" {
		return none, stateDiffers("the folder does not hold the files project_files lists: %s differs; nothing was committed", diff)
	}

	// (5) Commit, creating the repository first when the project has no record.
	if rec == nil {
		cfg, err := s.createRepository(r)
		if err != nil {
			return none, err
		}
		rec = &record{Config: cfg}
		if err := s.saveRecord(body.Project, *rec); err != nil {
			return none, historyWriteFailed("the version history of %s could not be written", body.Project)
		}
	}
	id, err := s.writeCommit(r, p, head, w)
	if err != nil {
		return none, err
	}
	if err := s.saveRecord(body.Project, record{Commit: id, Parent: head, stamp: w.stamp, Config: rec.Config}); err != nil {
		return none, historyWriteFailed("the version history of %s could not be written", body.Project)
	}
	old := head
	if old == "" {
		old = zeroID
	}
	if _, err := r.run(nil, "update-ref", branchRef, id, old); err != nil {
		return none, historyWriteFailed("the branch main of %s could not be moved; a repeat makes at most one commit", body.Project)
	}
	return gen.ProjectVersion{Commit: id, Created: true, ProjectFiles: p.list()}, nil
}

// createRepository empties the contents of `.git` (never the folder itself, which step containers
// mount), creating it when it is missing, runs `git init --template=` with the branch main, and
// returns the SHA-256 of the `.git/config` it wrote.
func (s *service) createRepository(r *repo) (string, error) {
	fail := func() (string, error) {
		return "", historyWriteFailed("the version history could not be created")
	}
	entries, err := os.ReadDir(r.gitDir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if err := os.Mkdir(r.gitDir, 0o755); err != nil {
			return fail()
		}
	case err != nil:
		return fail()
	}
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(r.gitDir, e.Name())); err != nil {
			return fail()
		}
	}
	if _, err := r.run(nil, "init", "--quiet", "--template=", "--initial-branch=main"); err != nil {
		return fail()
	}
	b, err := readNoFollow(filepath.Join(r.gitDir, "config"), 1<<20)
	if err != nil {
		return fail()
	}
	return sum(b), nil
}

// writeCommit writes the project's files as blobs, a fresh index and its tree, and the commit
// over parent, and returns the commit's ID. It moves no branch.
func (s *service) writeCommit(r *repo, p *project, parent string, w wanted) (string, error) {
	fail := func(what string) (string, error) {
		return "", historyWriteFailed("the version history could not be written (%s); a repeat makes at most one commit", what)
	}
	paths := sortedPaths(p.files)
	if len(paths) > 0 {
		out, err := r.run([]byte(strings.Join(paths, "\n")+"\n"), "hash-object", "-w", "--no-filters", "--stdin-paths")
		if err != nil {
			return fail("blobs")
		}
		ids := strings.Fields(string(out))
		if len(ids) != len(paths) {
			return fail("blobs")
		}
		for i, path := range paths {
			if ids[i] != blobID(p.files[path]) {
				return "", stateDiffers("the file %s changed during the call; nothing was committed", path)
			}
		}
	}
	var index bytes.Buffer
	for _, path := range paths {
		fmt.Fprintf(&index, "100644 %s\t%s\x00", blobID(p.files[path]), path)
	}
	if err := os.Remove(filepath.Join(r.gitDir, "index")); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fail("index")
	}
	if _, err := r.run(index.Bytes(), "update-index", "-z", "--index-info"); err != nil {
		return fail("index")
	}
	out, err := r.run(nil, "write-tree")
	if err != nil {
		return fail("tree")
	}
	tree := strings.TrimSpace(string(out))
	args := []string{"commit-tree", tree}
	if parent != "" {
		args = append(args, "-p", parent)
	}
	out, err = r.run([]byte(w.stamp.message()), args...)
	if err != nil {
		return fail("commit")
	}
	id := strings.TrimSpace(string(out))
	if !objectID.MatchString(id) {
		return fail("commit")
	}
	return id, nil
}

// blobID is git's ID of a blob holding b.
func blobID(b []byte) string {
	h := sha1.New() //nolint:gosec // git's object IDs
	fmt.Fprintf(h, "blob %d\x00", len(b))
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}

// firstDiff names the first path in which two file lists differ, or "".
func firstDiff(a, b gen.ProjectFiles) string {
	am, bm := map[string]string{}, map[string]string{}
	for _, f := range a {
		am[f.Path] = f.HashValue
	}
	for _, f := range b {
		bm[f.Path] = f.HashValue
	}
	var diffs []string
	for p, h := range am {
		if bm[p] != h {
			diffs = append(diffs, p)
		}
	}
	for p := range bm {
		if _, ok := am[p]; !ok {
			diffs = append(diffs, p)
		}
	}
	if len(diffs) == 0 {
		return ""
	}
	sort.Strings(diffs)
	return diffs[0]
}

func nonNilFiles(f gen.ProjectFiles) gen.ProjectFiles {
	if f == nil {
		return gen.ProjectFiles{}
	}
	return f
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ---- restoreStep ----

func (s *service) RestoreStep(ctx context.Context, req gen.RestoreStepRequestObject) (gen.RestoreStepResponseObject, error) {
	if s.cfg.History == "" {
		return nil, notServed()
	}
	body := *req.Body
	return bounded(ctx, body.TimeLimitSeconds, func(ctx context.Context) (gen.RestoreStepResponseObject, error) {
		unlock, err := s.lockProject(ctx, body.Project)
		if err != nil {
			return nil, err
		}
		defer unlock()
		v, err := s.restoreStep(ctx, body)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, err
		}
		return gen.RestoreStep200JSONResponse(v), nil
	})
}

func (s *service) restoreStep(ctx context.Context, body gen.RestoreRequest) (gen.RestoredProject, error) {
	var none gen.RestoredProject
	dir, err := s.projectDir(body.Project)
	if err != nil {
		return none, err
	}
	rec, err := s.loadRecord(body.Project)
	if err != nil {
		return none, err
	}
	if rec, err = s.firstTouch(dir, body.Project, rec, body.Attempt == 1); err != nil {
		return none, err
	}
	r := s.repo(ctx, dir, 0)
	defer r.close()
	max := s.limits.ProjectFileMax
	cantRead := func() error { return unreadable("the version history of %s cannot be read", body.Project) }

	// The history check, then the last commit must hold before.
	committed := map[string]string{} // path -> blob ID
	head := ""
	if rec != nil {
		if head, err = s.guard(r, rec, nil); err != nil {
			return none, err
		}
	}
	if head == "" {
		if len(body.Before) > 0 {
			return none, stateDiffers("the project has no commit, so it holds none of the files before lists")
		}
	} else {
		c, err := r.commit(head)
		if err != nil {
			return none, cantRead()
		}
		ok, diff, err := r.holds(c.tree, body.Before, max)
		switch {
		case err != nil:
			return none, cantRead()
		case !ok:
			return none, stateDiffers("the last commit does not hold the files before lists: %s differs", diff)
		}
		if committed, _, err = r.files(c.tree); err != nil {
			return none, cantRead()
		}
	}

	// Read the folder as entries, and plan: nothing changes when an entry may not be removed.
	plan, err := s.scanForRestore(ctx, dir, body.Project, body.Step.Writable)
	if err != nil {
		return none, err
	}
	restored := map[string]bool{}
	failed := func(path string) (gen.RestoredProject, error) {
		return none, historyWriteFailed("writing or removing %s failed; the folder may hold partly restored files, and a repeat converges them", path)
	}
	for _, rel := range plan.remove {
		if err := ctx.Err(); err != nil {
			return none, err
		}
		if err := os.RemoveAll(filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
			return failed(rel)
		}
		if procedures.IsProjectPath(rel) {
			restored[rel] = true
		}
	}
	for _, rel := range plan.files {
		if _, keep := committed[rel]; keep {
			continue
		}
		if err := ctx.Err(); err != nil {
			return none, err
		}
		if err := os.Remove(filepath.Join(dir, filepath.FromSlash(rel))); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return failed(rel)
		}
		restored[rel] = true
	}
	for _, rel := range sortedKeys(committed) {
		if err := ctx.Err(); err != nil {
			return none, err
		}
		target := filepath.Join(dir, filepath.FromSlash(rel))
		if b, err := readNoFollow(target, max); err == nil && blobID(b) == committed[rel] {
			continue
		}
		content, err := r.blob(committed[rel], max)
		if err != nil {
			return none, cantRead()
		}
		if err := writeFile(dir, rel, content); err != nil {
			return failed(rel)
		}
		restored[rel] = true
	}

	after, err := readProject(ctx, dir, body.Project, s.limits)
	if err != nil {
		return none, err
	}
	if err := after.strict(body.Project); err != nil {
		return none, stateDiffers("after restoring, the folder holds entries it may not: something wrote to it during the call")
	}
	if diff := firstDiff(after.list(), body.Before); diff != "" {
		return none, stateDiffers("after restoring, the folder does not hold before: %s differs (something wrote to it during the call)", diff)
	}
	list := []gen.ProjectPath{}
	for rel := range restored {
		list = append(list, rel)
	}
	sort.Strings(list)
	return gen.RestoredProject{Restored: list, ProjectFiles: after.list()}, nil
}

// restorePlan is what restoreStep removes (entries the general rules would not read, where the
// step could have made them) and the regular files it found, outside `.git`.
type restorePlan struct {
	remove []string
	files  []string
}

// scanForRestore walks the project folder without following a link. An entry that is a link, not
// a regular file or a folder, or whose name is not a ProjectPath part, is removed when it lies
// below the folder of a `dir/**` entry or is at a path an exact entry names; anywhere else it is
// 422 UnreadableProject, before anything changes.
func (s *service) scanForRestore(ctx context.Context, dir, name string, writable []string) (*restorePlan, error) {
	plan := &restorePlan{}
	var refused []string
	removable := func(rel string) bool {
		for _, w := range writable {
			if d, ok := strings.CutSuffix(w, "/**"); ok {
				if strings.HasPrefix(rel, d+"/") {
					return true
				}
			} else if rel == w {
				return true
			}
		}
		return false
	}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if path == dir {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == gitFolder {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		link := d.Type()&fs.ModeSymlink != 0
		bad := link || (!d.IsDir() && !d.Type().IsRegular()) || !procedures.IsProjectPath(rel)
		if !bad {
			if !d.IsDir() {
				if len(plan.files) >= s.limits.ProjectFilesMax {
					return errTooMany
				}
				plan.files = append(plan.files, rel)
			}
			return nil
		}
		if removable(rel) {
			plan.remove = append(plan.remove, rel)
		} else {
			why := "is not a project path"
			switch {
			case link:
				why = "is a symbolic link"
			case !d.IsDir() && !d.Type().IsRegular():
				why = "is not a regular file or a folder"
			}
			refused = append(refused, fmt.Sprintf("%q %s", rel, why))
		}
		if d.IsDir() && !link {
			return filepath.SkipDir
		}
		return nil
	})
	switch {
	case err == nil:
	case errors.Is(err, errTooMany):
		return nil, unreadable("the project folder %s holds more than %d files", name, s.limits.ProjectFilesMax)
	case ctx.Err() != nil:
		return nil, ctx.Err()
	default:
		return nil, unreadable("the project folder %s cannot be read", name)
	}
	if len(refused) > 0 {
		return nil, unreadable("the project folder %s holds %s, outside the step's writable paths; nothing was changed", name, strings.Join(refused, "; "))
	}
	return plan, nil
}

// writeFile writes one project file in place, its folders made without following a link, the
// file opened without following one.
func writeFile(dir, rel string, content []byte) error {
	if err := mkdirs(dir, rel); err != nil {
		return err
	}
	h, err := os.OpenFile(filepath.Join(dir, filepath.FromSlash(rel)), os.O_WRONLY|os.O_CREATE|os.O_TRUNC|syscall.O_NOFOLLOW, 0o644)
	if err != nil {
		return err
	}
	_, err = h.Write(content)
	if cerr := h.Close(); err == nil {
		err = cerr
	}
	return err
}
