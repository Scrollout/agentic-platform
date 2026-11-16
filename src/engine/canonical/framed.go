package canonical

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// File is one file of a set of files: its slash-separated path and its bytes.
type File struct {
	Path    string
	Content []byte
}

// Framed is the framed form of a set of files, which a step's output artifact and an agent
// definition's digest are hashed over (check contract, info.description, Artifacts): for each file
// in byte order of its path, the path, a line feed, the file's length in bytes in decimal, a line
// feed, then its bytes. No files give no bytes. The frame makes every set of files give different
// bytes, so the hash names the set. files is not changed.
func Framed(files []File) []byte {
	sorted := slices.Clone(files)
	slices.SortFunc(sorted, func(a, b File) int { return strings.Compare(a.Path, b.Path) })
	var b bytes.Buffer
	for _, f := range sorted {
		b.WriteString(f.Path + "\n" + strconv.Itoa(len(f.Content)) + "\n")
		b.Write(f.Content)
	}
	return b.Bytes()
}

// FramedHash is the SHA-256, in lowercase hex, of Framed(files).
func FramedHash(files []File) string {
	h := sha256.Sum256(Framed(files))
	return hex.EncodeToString(h[:])
}

// Unframe reads a framed form back into its files, in the order they appear. It refuses bytes that
// Framed would not write: a frame cut short, a length that is not a decimal without leading zeros,
// or paths that are not in strictly increasing byte order (which also refuses a path given twice).
// It does not judge the paths themselves.
func Unframe(b []byte) ([]File, error) {
	var out []File
	for len(b) > 0 {
		path, rest, ok := bytes.Cut(b, []byte("\n"))
		if !ok {
			return nil, errors.New("a file's path is not followed by a line feed")
		}
		num, rest, ok := bytes.Cut(rest, []byte("\n"))
		if !ok {
			return nil, fmt.Errorf("the length of %q is not followed by a line feed", path)
		}
		n, err := strconv.Atoi(string(num))
		if err != nil || n < 0 || strconv.Itoa(n) != string(num) {
			return nil, fmt.Errorf("the length of %q is not a decimal number of bytes", path)
		}
		if n > len(rest) {
			return nil, fmt.Errorf("%q is cut short: %d bytes announced, %d left", path, n, len(rest))
		}
		if len(out) > 0 && strings.Compare(out[len(out)-1].Path, string(path)) >= 0 {
			return nil, fmt.Errorf("%q does not follow %q in byte order", path, out[len(out)-1].Path)
		}
		out = append(out, File{Path: string(path), Content: bytes.Clone(rest[:n])})
		b = rest[n:]
	}
	return out, nil
}
