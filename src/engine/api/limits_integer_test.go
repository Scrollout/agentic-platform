package api

// The x-limits integer check, over all four limits (review of the engine group).

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

var limitKeys = []string{
	"requests-per-second-per-token",
	"requests-per-second-per-address-without-token",
	"idempotency-key-retention-hours",
	"request-body-max-bytes",
}

// withLimit returns the contract with the line of x-limits key replaced by repl
// ("" removes it). The line is found in the file, so the test fails if it moves.
func withLimit(t *testing.T, raw []byte, key, repl string) []byte {
	t.Helper()
	line := "  " + key + ": " + strconv.Itoa(contractLimit(t, key)) + "\n"
	if strings.Count(string(raw), line) != 1 {
		t.Fatalf("the contract has no single line %q to modify", line)
	}
	if repl != "" {
		repl = "  " + key + ": " + repl + "\n"
	}
	return []byte(strings.Replace(string(raw), line, repl, 1))
}

func TestEveryLimitMustBeAPositiveInteger(t *testing.T) {
	raw, err := os.ReadFile(contractPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseContract(raw, apiVersion); err != nil {
		t.Fatalf("the unmodified contract is refused: %v", err)
	}
	bad := map[string]string{
		"fraction": "2.5", "just over": "3.5", "quoted": `"3"`, "single quoted": `'3'`, "boolean": "true",
		"list": "[3]", "map": "{n: 3}", "words": "three", "zero": "0", "negative": "-1", "null": "null", "empty": "",
		"missing": "",
	}
	for _, key := range limitKeys {
		for name, v := range bad {
			t.Run(key+"/"+name, func(t *testing.T) {
				repl := v
				if name == "empty" {
					repl = " "
				}
				mutated := withLimit(t, raw, key, repl)
				if name == "empty" { // "key: " with nothing after it
					mutated = []byte(strings.Replace(string(mutated), "  "+key+":  \n", "  "+key+":\n", 1))
				}
				_, err := parseContract(mutated, apiVersion)
				if err == nil {
					t.Fatalf("parseContract accepted %s: %s", key, v)
				}
				if !strings.Contains(err.Error(), key) {
					t.Errorf("the error does not name %s: %v", key, err)
				}
			})
		}
	}
}

// Each key reaches its own field: changing one limit to a different valid value
// changes that limit and no other.
func TestEachLimitIsWiredToItsOwnField(t *testing.T) {
	raw, err := os.ReadFile(contractPath)
	if err != nil {
		t.Fatal(err)
	}
	base, err := parseContract(raw, apiVersion)
	if err != nil {
		t.Fatal(err)
	}
	read := func(c contract) [4]int64 {
		return [4]int64{int64(c.perSecond), int64(c.perAddress), int64(c.keepKeys / time.Hour), c.maxBody}
	}
	for i, key := range limitKeys {
		want := read(base)
		want[i] += 7
		c, err := parseContract(withLimit(t, raw, key, strconv.FormatInt(want[i], 10)), apiVersion)
		if err != nil {
			t.Fatalf("%s=%d refused: %v", key, want[i], err)
		}
		if got := read(c); got != want {
			t.Errorf("changing %s: limits are %v, want %v", key, got, want)
		}
	}
}
