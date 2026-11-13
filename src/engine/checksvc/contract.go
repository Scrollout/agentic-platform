package checksvc

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.yaml.in/yaml/v3"
)

// The check contract the service serves and the engine schemas it refers to, copied into gen/ by
// scripts/generate.sh (embedded copies). The version served, the limits (`x-limits`) and the
// request body schemas are read from it, so they have one home.
//
//go:embed gen/check/api/preview/2026-11-13-preview/openapi.yaml gen/engine/api/preview/2026-11-12-preview/slice-defs.schema.json gen/engine/api/preview/2026-11-12-preview/pattern.schema.json gen/engine/api/preview/2026-11-12-preview/run-event.schema.json
var contractFiles embed.FS

const contractPath = "gen/check/api/preview/2026-11-13-preview/openapi.yaml"

// limits are the contract's `x-limits` the service enforces.
type limits struct {
	PerToken         int   `yaml:"requests-per-second-per-token"`
	PerAddress       int   `yaml:"requests-per-second-per-address-without-token"`
	ProjectFilesMax  int   `yaml:"project-files-max"`
	ProjectFileMax   int64 `yaml:"project-file-max-bytes"`
	ResponseBodyMax  int64 `yaml:"response-body-max-bytes"`
	RequestBodyMax   int64 `yaml:"request-body-max-bytes"`
	ProcedureOutMax  int64 `yaml:"procedure-output-max-bytes"`
	StepFilesMax     int64 `yaml:"step-files-max-bytes"`
	CallsAtOnce      int   `yaml:"calls-at-once"`
	CallTimeLimitMax int   `yaml:"call-time-limit-seconds-max"`
}

type contract struct {
	version string
	limits  limits
	// bodies are the request body schemas by operationId.
	bodies map[string]*jsonschema.Schema
}

var checkContract = mustLoadContract()

func mustLoadContract() contract {
	c, err := loadContract()
	if err != nil {
		panic("check contract: " + err.Error() + " (run scripts/generate.sh)")
	}
	return c
}

// schemaBase is the URL the contract's files are read under; it names no local path, so no fault
// a client sees carries one.
const schemaBase = "file:///contract/"

func loadContract() (contract, error) {
	raw, err := contractFiles.ReadFile(contractPath)
	if err != nil {
		return contract{}, err
	}
	var doc struct {
		Info struct {
			Version string `yaml:"version"`
		} `yaml:"info"`
		Limits limits `yaml:"x-limits"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return contract{}, err
	}
	l := doc.Limits
	for name, v := range map[string]int64{"requests-per-second-per-token": int64(l.PerToken), "requests-per-second-per-address-without-token": int64(l.PerAddress),
		"project-files-max": int64(l.ProjectFilesMax), "project-file-max-bytes": l.ProjectFileMax, "response-body-max-bytes": l.ResponseBodyMax, "request-body-max-bytes": l.RequestBodyMax, "procedure-output-max-bytes": l.ProcedureOutMax,
		"step-files-max-bytes": l.StepFilesMax, "calls-at-once": int64(l.CallsAtOnce), "call-time-limit-seconds-max": int64(l.CallTimeLimitMax)} {
		if v < 1 {
			return contract{}, fmt.Errorf("x-limits.%s must be a positive integer", name)
		}
	}
	if !strings.Contains(contractPath, "/"+doc.Info.Version+"/") {
		return contract{}, fmt.Errorf("info.version %q differs from its folder", doc.Info.Version)
	}
	var asAny any
	if err := yaml.Unmarshal(raw, &asAny); err != nil {
		return contract{}, err
	}
	asJSON, err := json.Marshal(asAny)
	if err != nil {
		return contract{}, err
	}
	root, err := jsonschema.UnmarshalJSON(bytes.NewReader(asJSON))
	if err != nil {
		return contract{}, err
	}
	comp := jsonschema.NewCompiler()
	comp.DefaultDraft(jsonschema.Draft2020)
	comp.AssertContent()
	comp.UseRegexpEngine(regexpEngine)
	comp.UseLoader(contractLoader{})
	self := schemaBase + strings.TrimPrefix(contractPath, "gen/")
	if err := comp.AddResource(self, root); err != nil {
		return contract{}, err
	}
	c := contract{version: doc.Info.Version, limits: l, bodies: map[string]*jsonschema.Schema{}}
	for op, schema := range map[string]string{opRunCheck: "CheckRequest", opReadStepFiles: "StepFilesRequest", opRunProcedure: "ProcedureRequest"} {
		s, err := comp.Compile(self + "#/components/schemas/" + schema)
		if err != nil {
			return contract{}, fmt.Errorf("%s: %w", schema, err)
		}
		c.bodies[op] = s
	}
	return c, nil
}

// The contract's facts its clients need, from the contract itself (their one home).

// APIVersion is the api-version the contract serves.
func APIVersion() string { return checkContract.version }

// CallTimeLimitMax is the longest time_limit_seconds a call may give (x-limits).
func CallTimeLimitMax() int { return checkContract.limits.CallTimeLimitMax }

// contractLoader loads the embedded schema files, and nothing else.
type contractLoader struct{}

func (contractLoader) Load(u string) (any, error) {
	name, ok := strings.CutPrefix(u, schemaBase)
	if !ok || !strings.HasSuffix(name, ".schema.json") {
		return nil, fmt.Errorf("schema %s is outside the contract", u)
	}
	b, err := fs.ReadFile(contractFiles, "gen/"+name)
	if err != nil {
		return nil, errors.New("schema " + name + " is not embedded")
	}
	return jsonschema.UnmarshalJSON(bytes.NewReader(b))
}

// noDotPart is the lookahead the contract's ProjectPath and Writable patterns start with: no part
// of the path is `.` or `..`. Go's regexp has no lookahead, so regexpEngine checks it apart and
// compiles the rest of the pattern.
const noDotPart = `^(?!(.*/)?\.\.?(/|$))`

func regexpEngine(pattern string) (jsonschema.Regexp, error) {
	rest, ok := strings.CutPrefix(pattern, noDotPart)
	if !ok {
		return regexp.Compile(pattern)
	}
	re, err := regexp.Compile("^" + rest)
	if err != nil {
		return nil, err
	}
	return dotFree{re: re, pattern: pattern}, nil
}

type dotFree struct {
	re      *regexp.Regexp
	pattern string
}

func (d dotFree) MatchString(s string) bool {
	for _, part := range strings.Split(s, "/") {
		if part == "." || part == ".." {
			return false
		}
	}
	return d.re.MatchString(s)
}

func (d dotFree) String() string { return d.pattern }

// The operations, by operationId.
const (
	opRunCheck      = "runCheck"
	opReadStepFiles = "readStepFiles"
	opRunProcedure  = "runProcedure"
)

// checkBody checks a request body against the operation's schema: exactly one JSON value the
// schema accepts. It returns the faults, or "" for a body it accepts.
func (c contract) checkBody(op string, raw []byte) string {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var probe any
	if err := dec.Decode(&probe); err != nil {
		return "the body is not one JSON value"
	}
	if _, err := dec.Token(); err == nil {
		return "the body holds more than one JSON value"
	}
	v, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return "the body is not one JSON value"
	}
	err = c.bodies[op].Validate(v)
	if err == nil {
		return ""
	}
	var ve *jsonschema.ValidationError
	if !errors.As(err, &ve) {
		return "the body does not match the contract"
	}
	var faults []string
	var walk func(e *jsonschema.ValidationError)
	walk = func(e *jsonschema.ValidationError) {
		if len(e.Causes) > 0 {
			for _, c := range e.Causes {
				walk(c)
			}
			return
		}
		msg := "breaks the rule " + strings.Join(e.ErrorKind.KeywordPath(), "/")
		if out := e.BasicOutput(); out != nil && out.Error != nil && !strings.Contains(out.Error.String(), "://") {
			msg = out.Error.String()
		}
		if len(faults) < 20 {
			faults = append(faults, "at /"+strings.Join(e.InstanceLocation, "/")+": "+msg)
		}
	}
	walk(ve)
	return "the body does not match the contract: " + strings.Join(faults, "; ")
}
