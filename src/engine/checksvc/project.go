package checksvc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/agentic-platform/check/api/gen"
	"github.com/agentic-platform/engine/canonical"
	"github.com/agentic-platform/engine/checks"
	"github.com/agentic-platform/engine/procedures"
)

// project is a project folder as one call read it: every regular file whose path is a
// ProjectPath, with its bytes, and every entry it may not read.
type project struct {
	dir   string
	files map[string][]byte
	// bad names each entry that is not a regular file or a folder, or whose path is not a
	// ProjectPath, in walk order; nothing below such a folder is read.
	bad []string
	// large names each regular file larger than `x-limits.project-file-max-bytes`, which is never
	// read (it is in bad too).
	large []string
}

// projectDir returns the folder of the project named name under the projects folder, or the 422
// UnreadableProject the contract gives when it is missing, not a folder or a symbolic link.
func (s *service) projectDir(name string) (string, error) {
	dir := filepath.Join(s.cfg.Project, name)
	fi, err := os.Lstat(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "", unreadable("the project folder %s does not exist under the projects folder", name)
	case err != nil:
		return "", unreadable("the project folder %s cannot be read", name)
	case fi.Mode()&fs.ModeSymlink != 0:
		return "", unreadable("the project folder %s is a symbolic link, which is never followed", name)
	case !fi.IsDir():
		return "", unreadable("the project %s is not a folder", name)
	}
	return dir, nil
}

func unreadable(format string, a ...any) error {
	return fault(422, gen.ErrorCodeUnreadableProject, fmt.Sprintf(format, a...))
}

// readProject reads every regular file of dir without following a symbolic link: the walk reads
// entries with lstat, and each file is opened with O_NOFOLLOW. More than lim.ProjectFilesMax files
// is 422 UnreadableProject; a file larger than lim.ProjectFileMax is never read whole (at most one
// byte more is), and is an entry no operation reads. Only the last part of a path is protected
// against a symbolic link that appears after the walk (O_NOFOLLOW); a folder swapped for a link
// between the walk and the open is followed. The service runs with the project mounted read-only
// in its own container (constitution VIII), which bounds what such a race can reach.
func readProject(ctx context.Context, dir, name string, lim limits) (*project, error) {
	max := lim.ProjectFilesMax
	p := &project{dir: dir, files: map[string][]byte{}}
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
		switch {
		case !procedures.IsProjectPath(rel):
			p.bad = append(p.bad, fmt.Sprintf("%q is not a project path", rel))
		case d.Type()&fs.ModeSymlink != 0:
			p.bad = append(p.bad, rel+" is a symbolic link")
		case d.IsDir():
			return nil
		case !d.Type().IsRegular():
			p.bad = append(p.bad, rel+" is not a regular file or a folder")
		default:
			if len(p.files) >= max {
				return errTooMany
			}
			b, err := readNoFollow(path, lim.ProjectFileMax)
			switch {
			case errors.Is(err, errTooLarge):
				p.bad = append(p.bad, fmt.Sprintf("%s is larger than %d bytes", rel, lim.ProjectFileMax))
				p.large = append(p.large, rel)
				return nil
			case err != nil:
				p.bad = append(p.bad, rel+" cannot be read")
				return nil
			}
			p.files[rel] = b
			return nil
		}
		if d.IsDir() {
			return filepath.SkipDir
		}
		return nil
	})
	switch {
	case err == nil:
		return p, nil
	case errors.Is(err, errTooMany):
		return nil, unreadable("the project folder %s holds more than %d files", name, max)
	case ctx.Err() != nil:
		return nil, ctx.Err()
	}
	return nil, unreadable("the project folder %s cannot be read", name)
}

var errTooMany = errors.New("too many files")

var errTooLarge = errors.New("file too large")

// readNoFollow reads a regular file of at most max bytes, opened without following a link.
func readNoFollow(path string, max int64) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	if fi.Size() > max {
		return nil, errTooLarge
	}
	b, err := io.ReadAll(io.LimitReader(f, max+1))
	if err == nil && int64(len(b)) > max {
		return nil, errTooLarge
	}
	return b, err
}

// strict is the 422 UnreadableProject that readStepFiles and runProcedure answer when the folder
// holds an entry they may not read.
func (p *project) strict(name string) error {
	if len(p.bad) == 0 {
		return nil
	}
	return unreadable("the project folder %s holds %s", name, strings.Join(p.bad, "; "))
}

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// tree is the project's files by hash.
func (p *project) tree() checks.Tree { return checks.HashTree(p.files) }

// list is the contract's ProjectFiles: every file by path in byte order with its hash.
func (p *project) list() gen.ProjectFiles {
	out := gen.ProjectFiles{}
	for _, path := range sortedPaths(p.files) {
		out = append(out, gen.ProjectFile{Path: path, HashValue: sum(p.files[path])})
	}
	return out
}

func sortedPaths(files map[string][]byte) []string {
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths
}

// matching is the files whose paths match the glob entries (the catalogue's dialect), by path.
func (p *project) matching(entries []string) []canonical.File {
	var out []canonical.File
	for _, path := range sortedPaths(p.files) {
		if checks.MatchGlob(entries, path) {
			out = append(out, canonical.File{Path: path, Content: p.files[path]})
		}
	}
	return out
}

// outputEntries is the writable entries whose files make up the step's output name: all of them
// for a step with one output, entry i for output i otherwise. ok is false for a name the step
// does not declare.
func outputEntries(outputs, writable []string, name string) ([]string, bool) {
	for i, o := range outputs {
		if o != name {
			continue
		}
		if len(outputs) == 1 {
			return writable, true
		}
		return writable[i : i+1], true
	}
	return nil, false
}

// writeOutputs makes the files matching entries exactly files: each is written, replacing any
// file at its path, and every other file matching entries that the folder held is removed. No
// write or removal follows a symbolic link (the folder was read without one, and each file is
// opened with O_NOFOLLOW). It returns the path that failed.
func writeOutputs(ctx context.Context, p *project, entries []string, files []canonical.File) (string, error) {
	keep := map[string]bool{}
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return f.Path, err
		}
		keep[f.Path] = true
		if err := mkdirs(p.dir, f.Path); err != nil {
			return f.Path, err
		}
		target := filepath.Join(p.dir, filepath.FromSlash(f.Path))
		h, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|syscall.O_NOFOLLOW, 0o644)
		if err != nil {
			return f.Path, err
		}
		_, err = h.Write(f.Content)
		if cerr := h.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return f.Path, err
		}
	}
	for _, path := range sortedPaths(p.files) {
		if err := ctx.Err(); err != nil {
			return path, err
		}
		if checks.MatchGlob(entries, path) && !keep[path] {
			if err := os.Remove(filepath.Join(p.dir, filepath.FromSlash(path))); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return path, err
			}
		}
	}
	return "", nil
}

// mkdirs makes every folder above path, each part checked with lstat to be a folder, not a link.
func mkdirs(dir, path string) error {
	parts := strings.Split(path, "/")
	cur := dir
	for _, part := range parts[:len(parts)-1] {
		cur = filepath.Join(cur, part)
		fi, err := os.Lstat(cur)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			if err := os.Mkdir(cur, 0o755); err != nil {
				return err
			}
		case err != nil:
			return err
		case !fi.IsDir():
			return errors.New("a part of the path is not a folder")
		}
	}
	return nil
}
