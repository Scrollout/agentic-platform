package machine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"sync"
)

// KeyParts are the ingredients of a step's reuse key (research D8): all known before the step runs.
type KeyParts struct {
	PatternVersion string   // the pattern's `<id>@<version>`
	Step           string   // the step's name
	Inputs         []string // the hashes of the step's inputs, in order
	AgentDigest    string   // the agent definition's digest; empty for a fixed procedure
	Model          string   // the model id the step's agent uses; empty for a fixed procedure
	// Round is the review round a review step's key covers (research D36); 0 for every other
	// step, which gives the key it had before the round was covered.
	Round int
}

// ReuseKey is the SHA-256 (lowercase hex) of the key's parts, each framed by its length and the
// inputs by their count, so no two sets of parts give the same bytes.
func ReuseKey(p KeyParts) string {
	h := sha256.New()
	field := func(v string) { h.Write([]byte(strconv.Itoa(len(v)) + ":" + v + "\n")) }
	field(p.PatternVersion)
	field(p.Step)
	field(p.AgentDigest)
	field(p.Model)
	field(strconv.Itoa(len(p.Inputs)))
	for _, in := range p.Inputs {
		field(in)
	}
	if p.Round != 0 { // after the counted inputs, so a key without a round is unchanged
		field("round")
		field(strconv.Itoa(p.Round))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// contextHash is the hash of the findings and changed lines a fix or a later review round is
// given: an input of that step like any other.
func contextHash(open []Finding, changed []Line) string {
	b, _ := json.Marshal(struct {
		Open    []Finding `json:"open"`
		Changed []Line    `json:"changed"`
	}{open, changed})
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// Accepted is an accepted step result, found under its reuse key (FR-019). Lines are the lines a
// fix changed, which the next review round is given; Files the project's files after the step.
// An open review round's result is kept the same way, marked Open, with the round's findings in
// recorded order (suggestions included) and its NoBlockingFindingOpen result as recorded (research
// D36; data-model, An open round reused).
type Accepted struct {
	Key       string
	Artifacts []string
	Run       string
	Lines     []Line
	Files     []File // the project's files after the step
	Open      bool
	Findings  []Finding
	Check     *EventCheck
}

// Results is the accepted-result store, in memory. A nil *Results finds nothing and keeps nothing.
type Results struct {
	mu sync.Mutex
	by map[string]Accepted
}

// NewResults makes an empty store.
func NewResults() *Results { return &Results{by: map[string]Accepted{}} }

// Get finds the result accepted under key.
func (s *Results) Get(key string) (Accepted, bool) {
	if s == nil {
		return Accepted{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.by[key]
	return a, ok
}

// Put keeps an accepted or open result; the first result under a key stays, open or accepted.
func (s *Results) Put(a Accepted) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.by[a.Key]; !ok {
		s.by[a.Key] = a
	}
}

// Len is the number of results kept.
func (s *Results) Len() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.by)
}
