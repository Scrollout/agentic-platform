// Package analyst holds the analyst agent's eval suite (task T025): deterministic graders, the
// cases they judge, and (behind the `evals` build tag) the harness that runs the agent in Docker.
//
// The graders read only the agent's output. No grader calls a model (constitution XIX, XX): each
// reuses the platform's own checks, so the eval and the engine cannot disagree about what a good
// proposal is.
package analyst

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/agentic-platform/engine/checks"
	"github.com/agentic-platform/slice"
	"go.yaml.in/yaml/v3"
)

// Grader names, in the order Grade reports them.
const (
	GOutput    = "OutputIsOneProposal" // the output is one JSON object
	GSchema    = "SchemaValid"         // slice.CheckProposal
	GTrace     = "FullyLinked"         // slice.Trace returns nothing
	GEARS      = "RulesHaveEARS"       // slice.RuleText derives a sentence for every rule
	GFit       = "FitsPattern"         // checks.Fit: every condition of api-feature-go passes
	GQuestions = "QuestionsBounded"    // at most 10 questions, each naming an existing part
	GOwned     = "NoPlatformFields"    // no platform-owned or developer-owned key anywhere
	GExamples  = "ExamplesPresent"     // every rule has a scenario (also in Trace; named for the skip-examples case)
)

// Result is one grader's verdict. Reason is never empty.
type Result struct {
	Grader string `json:"grader"`
	Passed bool   `json:"passed"`
	Reason string `json:"reason"`
}

// Grader holds what grading needs: the compiled schemas and the pattern's bytes.
type Grader struct {
	Schemas *slice.Schemas
	Pattern []byte
}

// MaxQuestions is the proposal's limit on new questions (FR, slice-proposal.schema.json).
const MaxQuestions = 10

// ownedKeys are keys the proposal must never hold: the platform makes them (state, approvals, run
// IDs, rule sentences, hashes, question IDs) or the developer does (approvals, answers).
var ownedKeys = map[string]bool{
	"state": true, "approvals": true, "approval": true, "approved": true, "approved_by": true,
	"approved_at": true, "run_id": true, "slice_id": true, "rule_text": true, "hash": true,
	"revision": true, "answer": true, "answers": true, "asked_at": true, "created_at": true,
	"updated_at": true,
}

// Extract returns the one JSON object in the agent's output. It accepts the whole output (after
// trimming) or the whole output inside one Markdown code fence, and nothing else: prose around
// the proposal means the agent did not output one JSON proposal.
func Extract(output []byte) ([]byte, error) {
	s := strings.TrimSpace(string(output))
	if m := fenced.FindStringSubmatch(s); m != nil {
		s = strings.TrimSpace(m[1])
	}
	if s == "" {
		return nil, fmt.Errorf("the output is empty")
	}
	dec := json.NewDecoder(strings.NewReader(s))
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("the output is not JSON: %v", err)
	}
	if _, ok := v.(map[string]any); !ok {
		return nil, fmt.Errorf("the output is JSON but not an object")
	}
	if dec.More() {
		return nil, fmt.Errorf("the output holds more than one JSON value")
	}
	return []byte(s), nil
}

var fenced = regexp.MustCompile("(?s)^```(?:json)?\\s*\\n(.*)\\n```$")

// Grade runs every grader on the agent's raw output and returns one result per grader. When the
// output is not one JSON object, every later grader fails with that reason (nothing is skipped).
func (g *Grader) Grade(output []byte) []Result {
	doc, err := Extract(output)
	if err != nil {
		out := []Result{{GOutput, false, err.Error()}}
		for _, n := range []string{GSchema, GTrace, GEARS, GFit, GQuestions, GOwned, GExamples} {
			out = append(out, Result{n, false, "no proposal to grade: " + err.Error()})
		}
		return out
	}
	return []Result{
		{GOutput, true, "the output is one JSON object"},
		g.schema(doc),
		g.trace(doc),
		g.ears(doc),
		g.fit(doc),
		g.questions(doc),
		g.owned(doc),
		g.examples(doc),
	}
}

// Passed reports whether every result passed.
func Passed(rs []Result) bool {
	for _, r := range rs {
		if !r.Passed {
			return false
		}
	}
	return true
}

// Failures lists the failing results as "grader: reason".
func Failures(rs []Result) []string {
	var f []string
	for _, r := range rs {
		if !r.Passed {
			f = append(f, r.Grader+": "+r.Reason)
		}
	}
	return f
}

func (g *Grader) schema(doc []byte) Result {
	if err := g.Schemas.CheckProposal(doc); err != nil {
		return Result{GSchema, false, oneLine(err.Error())}
	}
	return Result{GSchema, true, "the proposal is valid against slice-proposal.schema.json"}
}

func (g *Grader) trace(doc []byte) Result {
	fs := slice.Trace(doc)
	if len(fs) == 0 {
		return Result{GTrace, true, "every goal, rule, example, term and operation is linked"}
	}
	var b []string
	for _, f := range fs {
		b = append(b, fmt.Sprintf("%s %s: %s", f.Part, f.ID, f.Reason))
	}
	return Result{GTrace, false, oneLine(strings.Join(b, "; "))}
}

func (g *Grader) ears(doc []byte) Result {
	if _, err := slice.RuleText(doc); err != nil {
		return Result{GEARS, false, oneLine(err.Error())}
	}
	return Result{GEARS, true, "every rule has an EARS sentence"}
}

func (g *Grader) fit(doc []byte) Result {
	rs := checks.Fit(g.Pattern, doc)
	var failed []string
	for _, r := range rs {
		if !r.Passed {
			failed = append(failed, r.Check+": "+r.Reason)
		}
	}
	if len(rs) == 0 || len(failed) > 0 {
		if len(rs) == 0 {
			failed = []string{"Fit returned no result"}
		}
		return Result{GFit, false, oneLine(strings.Join(failed, "; "))}
	}
	return Result{GFit, true, fmt.Sprintf("all %d conditions of the pattern pass", len(rs))}
}

type proposalView struct {
	Goals []struct {
		ID string `json:"id"`
	} `json:"goals"`
	Terms []struct {
		ID string `json:"id"`
	} `json:"terms"`
	Rules []struct {
		ID string `json:"id"`
	} `json:"rules"`
	Examples  string `json:"examples"`
	Contract  string `json:"contract"`
	Questions []struct {
		About string `json:"about"`
		Text  string `json:"text"`
	} `json:"questions"`
}

var tagRe = regexp.MustCompile(`@(R-[0-9]+)\b[^\n]*?@(E-[0-9]+)\b|@(E-[0-9]+)\b[^\n]*?@(R-[0-9]+)\b`)

// questions checks the count and that every question's `about` names an item the proposal holds.
func (g *Grader) questions(doc []byte) Result {
	var p proposalView
	if err := json.Unmarshal(doc, &p); err != nil {
		return Result{GQuestions, false, "cannot read questions: " + err.Error()}
	}
	if len(p.Questions) > MaxQuestions {
		return Result{GQuestions, false, fmt.Sprintf("%d questions, at most %d allowed", len(p.Questions), MaxQuestions)}
	}
	known := map[string]bool{}
	for _, x := range p.Goals {
		known[x.ID] = true
	}
	for _, x := range p.Terms {
		known[x.ID] = true
	}
	for _, x := range p.Rules {
		known[x.ID] = true
	}
	for _, m := range tagRe.FindAllStringSubmatch(p.Examples, -1) {
		r, e := m[1], m[2]
		if r == "" {
			r, e = m[4], m[3]
		}
		known[r+"/"+e] = true
	}
	var ops yaml.Node
	if err := yaml.Unmarshal([]byte(p.Contract), &ops); err == nil {
		for _, id := range operationIDs(&ops) {
			known["contract/"+id] = true
		}
	}
	for i, q := range p.Questions {
		if strings.TrimSpace(q.Text) == "" {
			return Result{GQuestions, false, fmt.Sprintf("question %d has no text", i+1)}
		}
		if !known[q.About] {
			return Result{GQuestions, false, fmt.Sprintf("question %d is about %q, which is no part of the proposal", i+1, q.About)}
		}
	}
	return Result{GQuestions, true, fmt.Sprintf("%d questions, each naming a part the proposal holds", len(p.Questions))}
}

func operationIDs(n *yaml.Node) []string {
	var ids []string
	var walk func(*yaml.Node)
	walk = func(n *yaml.Node) {
		switch n.Kind {
		case yaml.DocumentNode, yaml.SequenceNode:
			for _, c := range n.Content {
				walk(c)
			}
		case yaml.MappingNode:
			for i := 0; i+1 < len(n.Content); i += 2 {
				if n.Content[i].Value == "operationId" && n.Content[i+1].Kind == yaml.ScalarNode {
					ids = append(ids, n.Content[i+1].Value)
				}
				walk(n.Content[i+1])
			}
		}
	}
	walk(n)
	return ids
}

// owned fails when any JSON object key anywhere in the output is platform- or developer-owned.
// The schema already refuses unknown keys; this names them whatever the schema says, so a schema
// regression cannot let an injected field pass.
func (g *Grader) owned(doc []byte) Result {
	var v any
	if err := json.Unmarshal(doc, &v); err != nil {
		return Result{GOwned, false, err.Error()}
	}
	var hits []string
	var walk func(path string, v any)
	walk = func(path string, v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, c := range x {
				if ownedKeys[strings.ToLower(k)] {
					hits = append(hits, path+"/"+k)
				}
				walk(path+"/"+k, c)
			}
		case []any:
			for i, c := range x {
				walk(fmt.Sprintf("%s/%d", path, i), c)
			}
		}
	}
	walk("", v)
	if len(hits) > 0 {
		return Result{GOwned, false, "owned keys present: " + strings.Join(sortStrings(hits), ", ")}
	}
	return Result{GOwned, true, "no platform-owned or developer-owned key"}
}

func sortStrings(s []string) []string {
	for i := range s {
		for j := i + 1; j < len(s); j++ {
			if s[j] < s[i] {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
	return s
}

var scenarioRe = regexp.MustCompile(`(?m)^\s*Scenario(?: Outline)?:`)

// examples fails when the examples hold no scenario or fewer scenarios than rules.
func (g *Grader) examples(doc []byte) Result {
	var p proposalView
	if err := json.Unmarshal(doc, &p); err != nil {
		return Result{GExamples, false, err.Error()}
	}
	n := len(scenarioRe.FindAllString(p.Examples, -1))
	if n == 0 {
		return Result{GExamples, false, "the examples hold no scenario"}
	}
	if n < len(p.Rules) {
		return Result{GExamples, false, fmt.Sprintf("%d scenarios for %d rules", n, len(p.Rules))}
	}
	return Result{GExamples, true, fmt.Sprintf("%d scenarios for %d rules", n, len(p.Rules))}
}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(bytes.NewBufferString(s).String()), " ")
	if len(s) > 400 {
		s = s[:400] + "..."
	}
	return s
}
