package analyst

import (
	"bufio"
	"bytes"
	"strings"
)

// OpenTask reports whether backlog (specs/backlog.md) holds the open task id: a line starting
// "- [ ] <id> ". A quarantined case must name one, so a quarantine ends with its task.
func OpenTask(backlog []byte, id string) bool {
	if id == "" {
		return false
	}
	s := bufio.NewScanner(bytes.NewReader(backlog))
	s.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for s.Scan() {
		if strings.HasPrefix(s.Text(), "- [ ] "+id+" ") {
			return true
		}
	}
	return false
}
