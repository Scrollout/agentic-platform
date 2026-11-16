package steps

import (
	"bytes"
	"errors"
	"slices"

	"github.com/agentic-platform/engine/canonical"
	"github.com/agentic-platform/engine/machine"
)

// maxDiffCells bounds the work of one file's diff (lines before times lines after, once a common
// start and end are set aside). A larger change counts every line in between as changed: more
// lines for the review to see, never fewer.
const maxDiffCells = 4_000_000

// changedLines are the lines, numbered from 1, of the step's files that the attempt added or
// changed compared with the project's files when it started (before): every line of a new file,
// and in a changed file every line outside a longest common subsequence of lines with its earlier
// content, plus the line after a removal that no such line shows. load gives an earlier file's content by its hash; when
// it cannot, every line of that file counts as changed. Removed files have no lines.
func changedLines(files []canonical.File, before []machine.File, load func(string) ([]byte, error)) []machine.Line {
	if load == nil {
		load = func(string) ([]byte, error) { return nil, errNoLoad }
	}
	was := map[string]string{}
	for _, f := range before {
		was[f.Path] = f.SHA256
	}
	out := []machine.Line{}
	for _, f := range files {
		after := splitLines(f.Content)
		var old [][]byte
		switch h, ok := was[f.Path]; {
		case !ok:
		case h == sum(f.Content):
			continue
		default:
			b, err := load(h)
			if err != nil || sum(b) != h {
				old = nil
			} else {
				old = splitLines(b)
			}
		}
		for _, n := range diffLines(old, after) {
			out = append(out, machine.Line{File: f.Path, Line: n})
		}
	}
	return out
}

var errNoLoad = errors.New("no source of earlier files")

// splitLines splits content into lines without their line feeds; a last line feed ends a line.
func splitLines(b []byte) [][]byte {
	if len(b) == 0 {
		return nil
	}
	lines := bytes.Split(b, []byte("\n"))
	if len(lines[len(lines)-1]) == 0 {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// diffLines returns the numbers (from 1) of after's lines that are not in a longest common
// subsequence with old, plus the line after each removal of old lines.
func diffLines(old, after [][]byte) []int {
	pre := 0
	for pre < len(old) && pre < len(after) && bytes.Equal(old[pre], after[pre]) {
		pre++
	}
	suf := 0
	for suf < len(old)-pre && suf < len(after)-pre && bytes.Equal(old[len(old)-1-suf], after[len(after)-1-suf]) {
		suf++
	}
	o, a := old[pre:len(old)-suf], after[pre:len(after)-suf]
	changed := map[int]bool{}
	var removed []int     // after's middle index where old lines were removed
	mark := func(i int) { // i indexes after's middle; past its end is the first line of the common end
		if n := pre + i + 1; n <= len(after) {
			changed[n] = true
		} else if len(after) > 0 {
			changed[len(after)] = true
		}
	}
	switch {
	case len(o) == 0 && len(a) == 0:
	case len(o) == 0 || len(a) == 0 || len(o)*len(a) > maxDiffCells:
		for i := range a {
			mark(i)
		}
		if len(a) == 0 {
			mark(0)
		}
	default:
		// lcs[i][j]: the longest common subsequence of o[i:] and a[j:].
		lcs := make([][]int32, len(o)+1)
		for i := range lcs {
			lcs[i] = make([]int32, len(a)+1)
		}
		for i := len(o) - 1; i >= 0; i-- {
			for j := len(a) - 1; j >= 0; j-- {
				if bytes.Equal(o[i], a[j]) {
					lcs[i][j] = lcs[i+1][j+1] + 1
				} else {
					lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
				}
			}
		}
		i, j := 0, 0
		for i < len(o) || j < len(a) {
			switch {
			case i < len(o) && j < len(a) && bytes.Equal(o[i], a[j]):
				i, j = i+1, j+1
			case j < len(a) && (i == len(o) || lcs[i][j+1] >= lcs[i+1][j]):
				mark(j) // added or changed
				j++
			default:
				removed = append(removed, j)
				i++
			}
		}
	}
	// A removal next to an added or changed line is shown by that line; one between unchanged
	// lines is shown by the line after it.
	for _, j := range removed {
		if n := pre + j; !changed[n] && !changed[n+1] {
			mark(j)
		}
	}
	out := make([]int, 0, len(changed))
	for n := range changed {
		out = append(out, n)
	}
	slices.Sort(out)
	return out
}
