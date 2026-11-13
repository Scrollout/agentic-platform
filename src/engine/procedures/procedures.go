// Package procedures holds the api-feature-go pattern's fixed procedures: the steps no model
// runs (constitution XX). Each one computes a step's files from the step's inputs and returns them
// as the step's outputs; it writes nothing into the project, which is the procedure step worker's
// job (check contract, runProcedure). The same inputs give the same bytes.
//
// Inputs and outputs are artifacts in the check contract's form (info.description, Artifacts): an
// input named slice/<part> holds the part's RFC 8785 canonical JSON and its part hash; any other
// artifact is a set of project files in their framed form (canonical.Framed) and its SHA-256.
package procedures

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/agentic-platform/engine/canonical"
)

// Input is one input artifact of a step: its name in the pattern, its SHA-256 and its bytes.
type Input struct {
	Name    string
	SHA256  string
	Content []byte
}

// File is one project file: its project path and its bytes.
type File = canonical.File

// Output is one declared output of a step: its name in the pattern, its files in byte order of
// Path, and the SHA-256 of their framed form.
type Output struct {
	Name   string
	SHA256 string
	Files  []File
}

// Result is a procedure's result. With Completed, Outputs holds one entry per output the step
// declares, in the step's order; otherwise it is empty and Reason says why, naming project paths
// only. Reason is never empty.
type Result struct {
	Completed bool
	Reason    string
	Outputs   []Output
}

// ErrInputHashMismatch is returned when an input's content does not have the SHA-256 it carries:
// the caller sent something other than the artifact it names, so nothing is run.
var ErrInputHashMismatch = errors.New("an input's content does not have its SHA-256")

// Framed is the framed form of files (canonical.Framed, the one owner of that form).
func Framed(files []File) []byte { return canonical.Framed(files) }

// refused is a Result that did not complete.
func refused(format string, a ...any) Result {
	return Result{Reason: fmt.Sprintf(format, a...)}
}

// output builds an Output from files, sorted by path, with the hash of their framed form.
func output(name string, files []File) Output {
	files = slices.Clone(files)
	slices.SortFunc(files, func(a, b File) int { return strings.Compare(a.Path, b.Path) })
	return Output{Name: name, SHA256: canonical.FramedHash(files), Files: files}
}

// takeInputs checks every input's hash, then that the inputs are exactly the names in want, each
// once, and returns them by name. A hash mismatch is an error; a wrong set of names is a reason.
func takeInputs(in []Input, want ...string) (map[string]Input, string, error) {
	for _, i := range in {
		h := sha256.Sum256(i.Content)
		if hex.EncodeToString(h[:]) != i.SHA256 {
			return nil, "", fmt.Errorf("%w: %s", ErrInputHashMismatch, i.Name)
		}
	}
	got := map[string]Input{}
	for _, i := range in {
		if !slices.Contains(want, i.Name) {
			return nil, fmt.Sprintf("the step takes the inputs %s, not %q", strings.Join(want, ", "), i.Name), nil
		}
		if _, dup := got[i.Name]; dup {
			return nil, fmt.Sprintf("the input %s is given twice", i.Name), nil
		}
		got[i.Name] = i
	}
	for _, w := range want {
		if _, ok := got[w]; !ok {
			return nil, fmt.Sprintf("the input %s is missing (the step takes %s)", w, strings.Join(want, ", ")), nil
		}
	}
	return got, "", nil
}

// IsProjectPath reports whether p is a ProjectPath of the check contract: parts of ASCII letters,
// digits and . _ ~ + @ = , -, joined by single slashes, no part "." or "..", no part ".git" in any
// letter case (the version history is not part of the project), at most 256 characters. A file
// whose path is not one is never read, listed or written.
func IsProjectPath(p string) bool {
	if p == "" || len(p) > 256 {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if part == "" || part == "." || part == ".." || strings.EqualFold(part, ".git") {
			return false
		}
		for _, c := range []byte(part) {
			ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte("._~+@=,-", c) >= 0
			if !ok {
				return false
			}
		}
	}
	return true
}
