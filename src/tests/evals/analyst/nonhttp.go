package analyst

// T039b: the non-HTTP case and its grader. For a description of something that is not an HTTP API,
// the proposal states the developer's real use case in its goals and rules and has a contract with
// no HTTP operation (so the pattern does not fit, and the developer is told so by the engine). It
// must not fill the proposal with meta text about HTTP or about the analyst itself.
//
// Hooks needed outside this file (the implementer's, with the owner's approval of 2026-11-13):
//   - cases.go's Cases() lists {Name: CaseNonHTTP, Description: NonHTTPDescription};
//   - content.go's GradeCase handles CaseNonHTTP: it drops the GFit result (a misfit is expected)
//     and appends g.NonHTTP(out).

import (
	"encoding/json"
	"fmt"
	"strings"

	"go.yaml.in/yaml/v3"
)

const (
	CaseNonHTTP = "non-http"
	// GNonHTTP: the contract has no HTTP operation and the goals and rules state the real use case.
	GNonHTTP = "NonHttpUseCaseStated"
	// NonHTTPDescription is the case's description.
	NonHTTPDescription = "A command-line tool in Python that renames files."
)

// fillerWords must not appear in the goals or rules of the non-HTTP case's proposal: they are the
// analyst talking about itself or about the pattern instead of stating the use case.
var fillerWords = []string{"http", "api", "analyst", "pattern", "openapi", "contract", "endpoint", "no operation"}

type nonHTTPView struct {
	Goals []struct {
		Text string `json:"text"`
	} `json:"goals"`
	Rules []struct {
		Trigger  string `json:"trigger"`
		System   string `json:"system"`
		Response string `json:"response"`
	} `json:"rules"`
	Contract string `json:"contract"`
}

// NonHTTP grades the output of the non-HTTP case.
func (g *Grader) NonHTTP(out []byte) Result {
	fail := func(f string, a ...any) Result { return Result{GNonHTTP, false, fmt.Sprintf(f, a...)} }
	doc, err := Extract(out)
	if err != nil {
		return fail("%v", err)
	}
	var v nonHTTPView
	if err := json.Unmarshal(doc, &v); err != nil {
		return fail("the proposal cannot be read: %v", err)
	}
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(v.Contract), &root); err != nil {
		return fail("the contract is not YAML: %v", err)
	}
	if ops := httpOperations(&root); ops > 0 {
		return fail("the contract has %d HTTP operations for a description that is not an HTTP API", ops)
	}
	var text []string
	for _, goal := range v.Goals {
		text = append(text, goal.Text)
	}
	for _, r := range v.Rules {
		text = append(text, r.Trigger, r.System, r.Response)
	}
	all := strings.ToLower(strings.Join(text, " \n"))
	if len(v.Goals) == 0 || len(v.Rules) == 0 {
		return fail("the proposal has %d goals and %d rules, want the use case stated in both", len(v.Goals), len(v.Rules))
	}
	for _, w := range fillerWords {
		if strings.Contains(all, w) {
			return fail("the goals and rules mention %q: filler about the pattern or the analyst, not the use case", w)
		}
	}
	if !strings.Contains(all, "rename") {
		return fail("the goals and rules never say the tool renames files")
	}
	if !strings.Contains(all, "file") {
		return fail("the goals and rules never mention files")
	}
	return Result{GNonHTTP, true, "no HTTP operation, and the goals and rules state the file-renaming use case"}
}

// httpOperations counts the HTTP method keys under paths.
func httpOperations(root *yaml.Node) int {
	if root.Kind == yaml.DocumentNode && len(root.Content) == 1 {
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return 0
	}
	methods := map[string]bool{"get": true, "put": true, "post": true, "delete": true, "options": true, "head": true, "patch": true, "trace": true}
	n := 0
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != "paths" || root.Content[i+1].Kind != yaml.MappingNode {
			continue
		}
		for _, item := range root.Content[i+1].Content {
			if item.Kind != yaml.MappingNode {
				continue
			}
			for j := 0; j+1 < len(item.Content); j += 2 {
				if methods[item.Content[j].Value] {
					n++
				}
			}
		}
	}
	return n
}
