package tests

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// TestSleeps records its process ID as a file named by it in the folder AP_TEST_PIDDIR names, then
// sleeps far longer than any time limit the tests give it.
func TestSleeps(t *testing.T) {
	if d := os.Getenv("AP_TEST_PIDDIR"); d != "" {
		pid := strconv.Itoa(os.Getpid())
		if err := os.WriteFile(filepath.Join(d, pid), []byte(pid), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(120 * time.Second)
}
