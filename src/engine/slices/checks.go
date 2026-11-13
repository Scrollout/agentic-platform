package slices

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/agentic-platform/engine/checks"
	"github.com/agentic-platform/slice"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
)

// partsDoc is a slice's parts as one JSON document, in the proposal schema's shape: what the
// schema check, slice.Trace and checks.Fit read.
type partsDoc struct {
	Goals    []Goal `json:"goals"`
	Terms    []Term `json:"terms"`
	Rules    []Rule `json:"rules"`
	Examples string `json:"examples"`
	Contract string `json:"contract"`
}

func docOf(s *Slice) ([]byte, error) {
	return json.Marshal(partsDoc{Goals: s.Goals, Terms: s.Terms, Rules: s.Rules, Examples: s.Examples, Contract: s.Contract})
}

// partChecks runs every check whose input is the parts alone, in a fixed order: the schema, the
// links between IDs, then the pattern's conditions. Each check reports on every part it covers:
// one failed result per fault, or one passed result. It does not run the compatibility check,
// whose result depends on the approved contract too.
//
// dropped holds the example IDs earlier revisions of the slice dropped; one that is back fails
// ExampleTagged.
func (s *Service) partChecks(sl *Slice, dropped map[string]string) ([]Check, error) {
	doc, err := docOf(sl)
	if err != nil {
		return nil, err
	}
	var out []Check

	schemaFails, err := schemaFailures(s.schemas.CheckProposal(doc))
	if err != nil {
		return nil, err
	}
	out = append(out, perPart(CheckSchema, schemaFails, "the part matches the contract's schema")...)

	var traceFails []Check
	for _, f := range slice.Trace(doc) {
		if f.Part == "" { // the document could not be read: the engine wrote it, so this is a fault
			return nil, fmt.Errorf("slices: traceability: %s", f.Reason)
		}
		traceFails = append(traceFails, Check{Part: f.Part, Check: CheckTraceability, Reason: f.Reason})
	}
	out = append(out, perPart(CheckTraceability, traceFails, "every link between the part's IDs holds")...)
	out = append(out, exampleTagged(sl.Examples, dropped)...)

	if s.pattern == nil { // fail closed: no pattern means nothing shows the slice fits
		out = append(out, Check{Part: string(PartContract), Check: "PatternFit", Pattern: true,
			Reason: "no catalogue pattern is configured, so nothing shows the slice fits"})
	} else {
		for _, r := range checks.Fit(s.pattern, doc) {
			out = append(out, Check{Part: r.Part, Check: r.Check, Passed: r.Passed, Reason: r.Reason, Pattern: true})
		}
	}
	return out, nil
}

// perPart returns the failures of one check, and a passed result for each part without one.
func perPart(name string, fails []Check, passed string) []Check {
	var out []Check
	for _, p := range allParts {
		n := 0
		for _, f := range fails {
			if f.Part == string(p) {
				out = append(out, f)
				n++
			}
		}
		if n == 0 {
			out = append(out, Check{Part: string(p), Check: name, Passed: true, Reason: passed})
		}
	}
	return out
}

// schemaFailures turns a schema validation error into one failed Schema check per innermost
// cause, on the part its location falls in. A cause at the document itself fails every part.
func schemaFailures(err error) ([]Check, error) {
	if err == nil {
		return nil, nil
	}
	var ve *jsonschema.ValidationError
	if !errors.As(err, &ve) {
		return nil, fmt.Errorf("slices: schema check: %w", err)
	}
	var out []Check
	var walk func(e *jsonschema.ValidationError)
	walk = func(e *jsonschema.ValidationError) {
		if len(e.Causes) > 0 {
			for _, c := range e.Causes {
				walk(c)
			}
			return
		}
		reason := schemaReason(e)
		parts := allParts
		if len(e.InstanceLocation) > 0 {
			if p, ok := partOfField[e.InstanceLocation[0]]; ok {
				parts = []Part{p}
			}
		}
		for _, p := range parts {
			out = append(out, Check{Part: string(p), Check: CheckSchema, Reason: fmt.Sprintf("the %s part, at %s: %s", p, pointer(e.InstanceLocation), reason)})
		}
	}
	walk(ve)
	return out, nil
}

// schemaReason says in words what a leaf validation error found, without the schema's location:
// a schema's URL is a local file path when the schemas are read from disk, and a reason must not
// carry one (T037a). A message that would carry a URL gives the keyword's name instead.
func schemaReason(e *jsonschema.ValidationError) string {
	msg := ""
	if out := e.BasicOutput(); out != nil && out.Error != nil {
		msg = out.Error.String()
	}
	if msg == "" || strings.Contains(msg, "://") || strings.Contains(msg, "file:") {
		msg = "the value breaks the schema rule " + strings.Join(e.ErrorKind.KeywordPath(), "/")
	}
	if r, ok := e.ErrorKind.(*kind.Required); ok && !strings.Contains(msg, "missing") {
		msg += " (missing " + strings.Join(r.Missing, ", ") + ")"
	}
	return msg
}

// pointer writes an instance location as a JSON pointer (RFC 6901) into the parts document.
func pointer(loc []string) string {
	if len(loc) == 0 {
		return "the document's root"
	}
	var b strings.Builder
	for _, t := range loc {
		b.WriteByte('/')
		b.WriteString(strings.NewReplacer("~", "~0", "/", "~1").Replace(t))
	}
	return b.String()
}

// partOfField maps a field of the parts document to the part it belongs to.
var partOfField = map[string]Part{
	"goals": PartGoals, "rules": PartRules, "terms": PartRules, "examples": PartRules, "contract": PartContract,
}

// What scrub looks at: each token of a text, a run of characters between spaces, quotes,
// backticks, parentheses, angle brackets, commas, semicolons and equals signs (square and curly
// brackets stay, so an IPv6 host or a templated API path is one token), without its trailing
// full stops and colons.
var (
	scrubToken = regexp.MustCompile("[^\\s\"'`()<>,;=]+")
	hostPort   = regexp.MustCompile(`^(?:\[[0-9A-Fa-f:.%]+\]|(?:[A-Za-z0-9-]+\.)*[A-Za-z][A-Za-z0-9-]*(?:\.[A-Za-z0-9-]+)*|[0-9]{1,3}(?:\.[0-9]{1,3}){3}):[0-9]{1,5}(?:/.*)?$`)
	drivePath  = regexp.MustCompile(`^[A-Za-z]:[\\/]`)
	fileName   = regexp.MustCompile(`^[^/\\]*[^./\\]\.[A-Za-z0-9]{1,10}$`)
)

// scrubFinding keeps locations out of a finding's text before it goes into a check reason, which
// reaches the client (T037e). A finding's text comes from the contract, so an API path such as
// GET /run/{id} stays; replaced are anything with a URL scheme (x://, file:), a host:port (a
// single label and IPv6 too), a Windows drive or UNC path, a ~/ path, and an absolute path that
// ends in a file name with an extension. A runner's error never goes into a reason: it goes to
// the engine's log.
func scrubFinding(text string) string {
	text = uncPath.ReplaceAllString(text, "<path>")
	return scrubToken.ReplaceAllStringFunc(text, func(tok string) string {
		core := strings.TrimRight(tok, ".:")
		tail := tok[len(core):]
		lower := strings.ToLower(core)
		switch {
		case core == "":
			return tok
		case strings.Contains(lower, "://"), strings.Contains(lower, "file:"):
			return "<url>" + tail
		case hostPort.MatchString(core):
			return "<host>" + tail
		case drivePath.MatchString(core), strings.HasPrefix(core, "~/"), strings.HasPrefix(core, `~\`):
			return "<path>" + tail
		case strings.HasPrefix(core, "/") && endsInFile(core):
			return "<path>" + tail
		}
		return tok
	})
}

// uncPath is a UNC path, \\server\share\..., replaced before the text is split into tokens.
var uncPath = regexp.MustCompile(`\\\\[^\s\\"'` + "`" + `]+\\[^\s"'` + "`" + `()<>,;]*`)

// endsInFile reports whether an absolute path ends in a file name with an extension, after any
// JSON pointer (#...), query (?...) or line suffix (:12) is cut off.
func endsInFile(p string) bool {
	if i := strings.IndexAny(p, "#?"); i >= 0 {
		p = p[:i]
	}
	if i := strings.IndexByte(p, ':'); i >= 0 {
		p = p[:i]
	}
	seg := p[strings.LastIndexAny(p, "/\\")+1:]
	return fileName.MatchString(seg)
}

// compatibility runs oasdiff's comparison of the contract with base ("" before any contract of
// the slice was approved). Each error-level finding fails the check and names the change; a
// comparison that cannot run fails it too.
func (s *Service) compatibility(ctx context.Context, base, contract string) []Check {
	against := "the previously approved contract"
	if base == "" {
		against = "an empty document"
	}
	findings, err := s.diff.Diff(ctx, base, contract)
	if err != nil {
		s.log.Error("the contract comparison could not run", "check", CheckContractCompatibility, "error", err.Error())
		return []Check{{Part: string(PartContract), Check: CheckContractCompatibility,
			Reason: fmt.Sprintf("the comparison with %s could not run, so the contract is not shown compatible", against)}}
	}
	var out []Check
	for _, f := range findings {
		if f.Level == "error" {
			out = append(out, Check{Part: string(PartContract), Check: CheckContractCompatibility,
				Reason: fmt.Sprintf("breaking change against %s: %s (%s)", against, scrubFinding(f.Text), f.ID)})
		}
	}
	if len(out) == 0 {
		out = []Check{{Part: string(PartContract), Check: CheckContractCompatibility, Passed: true,
			Reason: "no breaking change against " + against}}
	}
	return out
}

// lintContract runs the Spectral lint on the contract. Each error-level finding fails the check
// and names its rule; warnings and info do not fail it; a lint that cannot run fails it too, and
// so does no lint configured (fail closed, as a missing pattern does).
func (s *Service) lintContract(ctx context.Context, contract string) []Check {
	if s.lint == nil {
		return []Check{{Part: string(PartContract), Check: CheckContractLint,
			Reason: "no lint is configured, so the contract is not shown to follow OpenAPI's rules"}}
	}
	findings, err := s.lint.Lint(ctx, contract)
	if err != nil {
		s.log.Error("the contract lint could not run", "check", CheckContractLint, "error", err.Error())
		return []Check{{Part: string(PartContract), Check: CheckContractLint,
			Reason: "the lint could not run, so the contract is not shown to follow OpenAPI's rules"}}
	}
	var out []Check
	for _, f := range findings {
		if f.Level == "error" {
			out = append(out, Check{Part: string(PartContract), Check: CheckContractLint,
				Reason: fmt.Sprintf("lint rule %s: %s", f.Code, scrubFinding(f.Text))})
		}
	}
	if len(out) == 0 {
		out = []Check{{Part: string(PartContract), Check: CheckContractLint, Passed: true,
			Reason: "no lint error under OpenAPI's standard rules"}}
	}
	return out
}

// LintContract judges a contract as the ContractLint check judges a slice's (lintContract): it
// passes without a lint error, and fails when the lint finds one, cannot run or is not configured.
// The reason names each error, or says it passed. A run's ContractLintPasses uses it.
func (s *Service) LintContract(ctx context.Context, contract string) (passed bool, reason string, err error) {
	var reasons []string
	passed = true
	for _, c := range s.lintContract(ctx, contract) {
		passed = passed && c.Passed
		reasons = append(reasons, c.Reason)
	}
	return passed, strings.Join(reasons, "; "), nil
}

// exampleTagged is the ExampleTagged check on the rules part: slice.ExampleTags's findings, and
// each example ID that an earlier revision dropped and the examples now give to another scenario
// (an E-<n> is never reused, data-model.md), as failed results, or one passed result. Bringing
// back the scenario the ID was dropped with is no reuse. Examples that are not Gherkin fail it, as
// they fail slice.Trace's check.
func exampleTagged(examples string, dropped map[string]string) []Check {
	fs, err := slice.ExampleTags(examples)
	if err != nil {
		return []Check{{Part: string(PartRules), Check: CheckExampleTagged,
			Reason: "the examples are not valid Gherkin, so their tags cannot be read: " + err.Error()}}
	}
	var out []Check
	for _, f := range fs {
		out = append(out, Check{Part: f.Part, Check: CheckExampleTagged, Reason: f.Reason})
	}
	now, _ := slice.ExampleScenarios(examples) // the examples parsed above
	for _, id := range slices.Sorted(maps.Keys(now)) {
		if was, ok := dropped[id]; ok && was != now[id] {
			out = append(out, Check{Part: string(PartRules), Check: CheckExampleTagged,
				Reason: fmt.Sprintf("example ID %s was dropped by an earlier revision and is never reused for another scenario; give the scenario a new E-<n>", id)})
		}
	}
	if len(out) == 0 {
		return []Check{{Part: string(PartRules), Check: CheckExampleTagged, Passed: true,
			Reason: "every scenario carries its rule's tag and a unique example tag"}}
	}
	return out
}

// dropExamples returns the example IDs dropped after the examples changed from base, the last
// examples of the slice that parsed, to after, and the base for the next change. Dropped are
// those of dropped that after does not bring back with the same scenario, and those base carries
// and after does not, each with the scenario it was dropped with; an ID that after gives to
// another scenario stays dropped with its first scenario. Examples after that are not Gherkin
// change nothing and leave base as it is, so a drop across an invalid revision still counts.
func dropExamples(dropped map[string]string, base, after string) (map[string]string, string) {
	now, err := slice.ExampleScenarios(after)
	if err != nil {
		return maps.Clone(dropped), base
	}
	was, err := slice.ExampleScenarios(base)
	if err != nil { // only a record from before the base was kept can hold such a base
		was = nil
	}
	out := map[string]string{}
	for id, d := range dropped {
		if now[id] != d {
			out[id] = d
		}
	}
	for id, d := range was {
		if _, ok := now[id]; !ok {
			if _, ok := out[id]; !ok {
				out[id] = d
			}
		}
	}
	if len(out) == 0 {
		out = nil
	}
	return out, after
}
