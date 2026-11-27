package captured

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"go.yaml.in/yaml/v3"
)

// FillWorker is the worker of a captured pattern's `fill` steps: the engine fills the step's files
// from the template, with no step worker and no model. The API, the run workflow and the
// declaration name it from here.
const FillWorker = "procedure:fill"

// The base pattern's facts the declaration changes (research R1).
const (
	// reviewCheck marks the review step, the one run in rounds; reviewChecks are the checks that
	// judge findings, which no fill step writes.
	reviewCheck     = "NoBlockingFindingOpen"
	witnessesOutput = "witnesses"
	// testsCheck runs every test of the project: the filled review step writes the witnesses after
	// the code step's run and no fix round follows, so it runs them there (ADR 0101).
	testsCheck = "AllTestsPass"
)

var reviewChecks = map[string]bool{"FindingsCarryEvidence": true, reviewCheck: true}

// Declaration gives a captured pattern's declaration from its base pattern's (pattern.yaml, valid
// against pattern.schema.json): each agent step's worker `procedure:fill`, `limits.attempts` and
// `limits.review_rounds` 1, the review step without FindingsCarryEvidence and
// NoBlockingFindingOpen, with AllTestsPass after its other checks (once) and with outputs
// `[witnesses]`, everything else equal.
//
// A `fill` step keeps no `agent_digest`: it runs no agent definition. Comments, key order and
// anchors of the base are kept. A base that is not one YAML mapping with `limits` and a list of
// `steps`, each a mapping with a `worker`, is an error.
func Declaration(base []byte) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(base, &doc); err != nil {
		return nil, fmt.Errorf("captured: the base pattern is unreadable: %w", err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("captured: the base pattern is not a YAML mapping")
	}
	top := doc.Content[0]
	limits := value(top, "limits")
	if limits == nil || limits.Kind != yaml.MappingNode {
		return nil, errors.New("captured: the base pattern has no limits")
	}
	for _, k := range []string{"attempts", "review_rounds"} {
		set(limits, k, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: "1"})
	}
	steps := value(top, "steps")
	if steps == nil || steps.Kind != yaml.SequenceNode || len(steps.Content) == 0 {
		return nil, errors.New("captured: the base pattern has no steps")
	}
	for i, st := range steps.Content {
		if st.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("captured: step %d of the base pattern is not a mapping", i+1)
		}
		w := value(st, "worker")
		if w == nil || w.Kind != yaml.ScalarNode {
			return nil, fmt.Errorf("captured: step %d of the base pattern has no worker", i+1)
		}
		if strings.HasPrefix(w.Value, "agent:") {
			set(st, "worker", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: FillWorker})
			remove(st, "agent_digest")
		}
		checks := value(st, "checks")
		if checks == nil || checks.Kind != yaml.SequenceNode || !holds(checks, reviewCheck) {
			continue
		}
		kept := checks.Content[:0:0]
		for _, c := range checks.Content {
			if !reviewChecks[c.Value] {
				kept = append(kept, c)
			}
		}
		if !holds(checks, testsCheck) {
			kept = append(kept, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: testsCheck})
		}
		checks.Content = kept
		set(st, "outputs", &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Style: yaml.FlowStyle,
			Content: []*yaml.Node{{Kind: yaml.ScalarNode, Tag: "!!str", Value: witnessesOutput}}})
	}
	var b bytes.Buffer
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return nil, fmt.Errorf("captured: the declaration cannot be written: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("captured: the declaration cannot be written: %w", err)
	}
	return b.Bytes(), nil
}

// value is the value of key in mapping m, or nil.
func value(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// set makes key's value v in mapping m, keeping its place, or adds it at the end.
func set(m *yaml.Node, key string, v *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			v.LineComment, v.HeadComment = m.Content[i+1].LineComment, m.Content[i+1].HeadComment
			m.Content[i+1] = v
			return
		}
	}
	m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, v)
}

// remove takes key and its value out of mapping m, with the comments above the key.
func remove(m *yaml.Node, key string) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content = append(m.Content[:i:i], m.Content[i+2:]...)
			return
		}
	}
}

func holds(seq *yaml.Node, s string) bool {
	for _, c := range seq.Content {
		if c.Kind == yaml.ScalarNode && c.Value == s {
			return true
		}
	}
	return false
}
