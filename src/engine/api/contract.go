package api

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/agentic-platform/retry"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
	"go.yaml.in/yaml/v3"
)

// The contract the generated code in gen/ comes from: scripts/generate.sh generates from the
// newest contract version, and so does loadContract. The facts a client sees and the contract
// states (the version served, which token may call which operation, which operations take an
// api-version or an Idempotency-Key, the schema of each request body, the error codes each
// operation lists in `x-error-codes`, the operations list and the limits in `x-limits`) are read
// from it here, so they have one home. Add stable/*/... to the patterns when the first stable
// version exists.
//
//go:embed preview/*/openapi.yaml preview/*/*.schema.json
var contractFiles embed.FS

// Security scheme names in the contract, one per kind of token (research D12).
const (
	clientScheme = "clientToken"
	pageScheme   = "pageToken"
	workerScheme = "workerToken"
)

// contractOp is what the contract says about one operation.
type contractOp struct {
	id, name, summary string
	order             int             // position in the contract
	callers           map[string]bool // security schemes that may call it
	versioned         bool            // takes the api-version parameter
	idempotent        bool            // takes the Idempotency-Key parameter
	// codes are the operation's `x-error-codes`: by status code ("409"), and "operation" for the
	// codes its operation's `error` may carry.
	codes map[string][]string
	// bodyRef is the request body's schema reference ("#/components/schemas/GoalsPart"), "" for
	// an operation without a body; body is that schema, compiled by loadContract.
	bodyRef      string
	bodyRequired bool
	body         *jsonschema.Schema
}

// op returns the operation with operationId id.
func (c contract) op(id string) contractOp { return c.ops[c.byID[id]] }

// lists reports whether the operation's x-error-codes list code under key.
func (op contractOp) lists(key, code string) bool {
	for _, c := range op.codes[key] {
		if c == code {
			return true
		}
	}
	return false
}

type contract struct {
	version        string                // the api-version served
	dir            string                // its folder in contractFiles, "preview/2026-11-12-preview"
	ops            map[string]contractOp // by route, "GET /slices/{sliceId}"
	byID           map[string]string     // route by operationId
	perSecond      int                   // requests in any one-second window per token
	perAddress     int                   // requests without an accepted token in any one-second window per source address
	keepKeys       time.Duration         // how long an idempotency key is kept
	maxBody        int64                 // largest request body, in bytes
	retry          retry.Rule            // the retry rule of x-limits
	keyMin, keyMax int                   // Idempotency-Key length
	// runID and hash are the contract's rules for a run ID and an artifact hash, which the run
	// operations take as path and query parameters; patternID for a captured pattern's ID, which
	// the captured-pattern operations take as a path parameter.
	runID, hash, patternID *jsonschema.Schema
}

var engineContract = mustLoadContract()

func mustLoadContract() contract {
	c, err := loadContract(contractFiles)
	if err != nil {
		panic("engine contract: " + err.Error())
	}
	return c
}

// The parts of an OpenAPI 3.1 document loadContract reads.
type (
	document struct {
		Info struct {
			Version string `yaml:"version"`
		} `yaml:"info"`
		Limits struct { // yaml.Node, so a fraction or a string is refused, not truncated or converted
			PerSecond  yaml.Node `yaml:"requests-per-second-per-token"`
			PerAddress yaml.Node `yaml:"requests-per-second-per-address-without-token"`
			KeepHours  yaml.Node `yaml:"idempotency-key-retention-hours"`
			MaxBody    yaml.Node `yaml:"request-body-max-bytes"`
		} `yaml:"x-limits"`
		Security   *[]map[string][]string `yaml:"security"`
		Paths      yaml.Node              `yaml:"paths"`
		Components struct {
			Parameters map[string]parameter `yaml:"parameters"`
		} `yaml:"components"`
	}
	operationDoc struct {
		ID         string                 `yaml:"operationId"`
		Name       string                 `yaml:"x-operation-name"`
		Summary    string                 `yaml:"summary"`
		Security   *[]map[string][]string `yaml:"security"`
		Parameters []parameter            `yaml:"parameters"`
		ErrorCodes map[string][]string    `yaml:"x-error-codes"`
		Body       *struct {
			Required bool `yaml:"required"`
			Content  map[string]struct {
				Schema struct {
					Ref string `yaml:"$ref"`
				} `yaml:"schema"`
			} `yaml:"content"`
		} `yaml:"requestBody"`
	}
	parameter struct {
		Ref      string `yaml:"$ref"`
		Name     string `yaml:"name"`
		In       string `yaml:"in"`
		Required bool   `yaml:"required"`
		Schema   struct {
			Ref       string `yaml:"$ref"`
			MinLength *int   `yaml:"minLength"`
			MaxLength *int   `yaml:"maxLength"`
		} `yaml:"schema"`
	}
)

var (
	methods        = map[string]bool{"get": true, "put": true, "post": true, "delete": true, "patch": true, "head": true, "options": true, "trace": true}
	pathItemFields = map[string]bool{"summary": true, "description": true, "parameters": true}
	schemes        = map[string]bool{clientScheme: true, pageScheme: true, workerScheme: true}
)

func loadContract(files fs.FS) (contract, error) {
	paths, err := fs.Glob(files, "*/*/openapi.yaml")
	if err != nil || len(paths) == 0 {
		return contract{}, fmt.Errorf("no contract embedded")
	}
	sort.Slice(paths, func(i, j int) bool { return versionOf(paths[i]) < versionOf(paths[j]) })
	newest := paths[len(paths)-1]
	raw, err := fs.ReadFile(files, newest)
	if err != nil {
		return contract{}, err
	}
	c, err := parseContract(raw, versionOf(newest))
	if err != nil {
		return contract{}, fmt.Errorf("%s: %w", newest, err)
	}
	c.dir = path.Dir(newest)
	if err := c.compileBodies(files, c.dir, raw); err != nil {
		return contract{}, fmt.Errorf("%s: %w", newest, err)
	}
	return c, nil
}

// ContractFiles returns the files of the contract version the engine serves (its openapi.yaml
// and *.schema.json) at the root of a file system, for slice.LoadSchemasFS: the run store and
// the slices parse with the schemas of the same version the API serves.
func ContractFiles() fs.FS {
	sub, err := fs.Sub(contractFiles, engineContract.dir)
	if err != nil {
		panic("engine contract: " + err.Error()) // dir came from a glob over contractFiles
	}
	return sub
}

// schemaBase is the URL the contract's files are read under when request bodies are checked; it
// names no local path, so no fault a client sees carries one.
const schemaBase = "file:///contract/"

// compileBodies compiles each operation's request body schema from the contract in dir, with
// references resolved to the schema files beside it and nothing else.
func (c *contract) compileBodies(files fs.FS, dir string, openapi []byte) error {
	var doc any
	if err := yaml.Unmarshal(openapi, &doc); err != nil {
		return err
	}
	asJSON, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("the contract cannot be read as JSON: %w", err)
	}
	root, err := jsonschema.UnmarshalJSON(bytes.NewReader(asJSON))
	if err != nil {
		return err
	}
	comp := jsonschema.NewCompiler()
	comp.DefaultDraft(jsonschema.Draft2020)
	comp.UseLoader(contractLoader{files: files, dir: dir})
	if err := comp.AddResource(schemaBase+"openapi.yaml", root); err != nil {
		return err
	}
	for route, op := range c.ops {
		if op.bodyRef == "" {
			continue
		}
		sch, err := comp.Compile(schemaBase + "openapi.yaml" + op.bodyRef)
		if err != nil {
			return fmt.Errorf("%s request body: %w", route, err)
		}
		op.body = sch
		c.ops[route] = op
	}
	for _, r := range []struct {
		dst **jsonschema.Schema
		def string
	}{{&c.runID, "run_id"}, {&c.hash, "hash"}, {&c.patternID, "pattern_id"}} {
		if *r.dst, err = comp.Compile(schemaBase + "slice-defs.schema.json#/$defs/" + r.def); err != nil {
			return fmt.Errorf("the rule %s: %w", r.def, err)
		}
	}
	return nil
}

// fits reports whether a parameter's value meets the rule sch.
func fits(sch *jsonschema.Schema, value string) bool { return sch.Validate(value) == nil }

// contractLoader loads the JSON schema files of one contract version, and nothing else.
type contractLoader struct {
	files fs.FS
	dir   string
}

func (l contractLoader) Load(u string) (any, error) {
	name, ok := strings.CutPrefix(u, schemaBase)
	if !ok || !strings.HasSuffix(name, ".schema.json") || strings.Contains(name, "/") {
		return nil, fmt.Errorf("schema %s is outside the contract", u)
	}
	b, err := fs.ReadFile(l.files, l.dir+"/"+name)
	if err != nil {
		return nil, err
	}
	return jsonschema.UnmarshalJSON(bytes.NewReader(b))
}

// projectNameRule is the contract's rule for a project name. A body whose only faults are a
// string that breaks it is refused with InvalidProject, as the contract's ErrorCode says, where
// the operation lists that code; a value that is not a string is InvalidRequest.
const projectNameRule = schemaBase + "slice-defs.schema.json#/$defs/project_name"

// checkBody checks a request body against the operation's schema. It returns the problem code
// and the faults, or "" for a body the schema accepts or an operation without one.
func (op contractOp) checkBody(raw []byte) (code, detail string) {
	if op.body == nil || (!op.bodyRequired && len(raw) == 0) {
		return "", ""
	}
	v, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return "InvalidRequest", "the body is not one JSON value: " + err.Error()
	}
	err = op.body.Validate(v)
	if err == nil {
		return "", ""
	}
	var ve *jsonschema.ValidationError
	if !errors.As(err, &ve) {
		return "InvalidRequest", err.Error()
	}
	project := true
	var faults []string
	var walk func(e *jsonschema.ValidationError)
	walk = func(e *jsonschema.ValidationError) {
		if len(e.Causes) > 0 {
			for _, c := range e.Causes {
				walk(c)
			}
			return
		}
		_, wrongType := e.ErrorKind.(*kind.Type)
		project = project && !wrongType && strings.HasPrefix(e.SchemaURL, projectNameRule)
		msg := "breaks the rule " + strings.Join(e.ErrorKind.KeywordPath(), "/")
		if out := e.BasicOutput(); out != nil && out.Error != nil && !strings.Contains(out.Error.String(), "://") {
			msg = out.Error.String()
		}
		faults = append(faults, "at /"+strings.Join(e.InstanceLocation, "/")+": "+msg)
	}
	walk(ve)
	code = "InvalidRequest"
	if project && op.lists("400", "InvalidProject") {
		code = "InvalidProject"
	}
	return code, "the body does not match the contract: " + strings.Join(faults, "; ")
}

// positiveInt returns the value of n, which must be a plain YAML integer (tag !!int) of at least
// 1. A fraction, a quoted number, a boolean or a collection is refused.
func positiveInt(n *yaml.Node) (int64, error) {
	if n.Kind == 0 {
		return 0, fmt.Errorf("missing; a positive integer is required")
	}
	if n.Kind != yaml.ScalarNode || n.ShortTag() != "!!int" {
		return 0, fmt.Errorf("%q (line %d) is not an integer; a positive integer is required", n.Value, n.Line)
	}
	var v int64
	if err := n.Decode(&v); err != nil {
		return 0, fmt.Errorf("%q (line %d): %w", n.Value, n.Line, err)
	}
	if v < 1 {
		return 0, fmt.Errorf("%d (line %d) is not positive; a positive integer is required", v, n.Line)
	}
	return v, nil
}

// versionOf returns the version folder of a contract path, "preview/2026-11-12-preview/openapi.yaml".
func versionOf(path string) string { return strings.Split(path, "/")[1] }

// parseContract reads what the engine serves from the contract. Anything it does not
// understand is an error, never skipped.
func parseContract(raw []byte, version string) (contract, error) {
	var d document
	if err := yaml.Unmarshal(raw, &d); err != nil {
		return contract{}, err
	}
	if d.Info.Version != version {
		return contract{}, fmt.Errorf("info.version %q differs from its folder %q", d.Info.Version, version)
	}
	if d.Security != nil {
		return contract{}, fmt.Errorf("a top-level security is not supported; state it per operation")
	}
	var limits [4]int64
	for i, l := range []struct {
		key  string
		node *yaml.Node
	}{
		{"requests-per-second-per-token", &d.Limits.PerSecond},
		{"requests-per-second-per-address-without-token", &d.Limits.PerAddress},
		{"idempotency-key-retention-hours", &d.Limits.KeepHours},
		{"request-body-max-bytes", &d.Limits.MaxBody},
	} {
		n, err := positiveInt(l.node)
		if err != nil {
			return contract{}, fmt.Errorf("x-limits.%s: %w", l.key, err)
		}
		limits[i] = n
	}
	if limits[2] > math.MaxInt64/int64(time.Hour) {
		return contract{}, fmt.Errorf("x-limits.idempotency-key-retention-hours: %d hours is too long", limits[2])
	}
	rule, err := retryRuleOf(raw)
	if err != nil {
		return contract{}, fmt.Errorf("x-limits: %w", err)
	}
	c := contract{version: version, retry: rule, ops: map[string]contractOp{}, byID: map[string]string{}, perSecond: int(limits[0]), perAddress: int(limits[1]),
		keepKeys: time.Duration(limits[2]) * time.Hour, maxBody: limits[3]}

	paths, err := pairs(&d.Paths, "paths")
	if err != nil {
		return contract{}, err
	}
	for _, p := range paths {
		path := p.key
		item, err := pairs(p.value, path)
		if err != nil {
			return contract{}, err
		}
		var shared []parameter
		for _, f := range item {
			if f.key == "parameters" {
				if err := f.value.Decode(&shared); err != nil {
					return contract{}, fmt.Errorf("%s parameters: %w", path, err)
				}
			}
		}
		for _, f := range item {
			if pathItemFields[f.key] {
				continue
			}
			if !methods[f.key] {
				return contract{}, fmt.Errorf("%s: %q is not supported", path, f.key)
			}
			route := strings.ToUpper(f.key) + " " + path
			var o operationDoc
			if err := f.value.Decode(&o); err != nil {
				return contract{}, fmt.Errorf("%s: %w", route, err)
			}
			op, err := c.operation(o, append(append([]parameter{}, shared...), o.Parameters...), d.Components.Parameters)
			if err != nil {
				return contract{}, fmt.Errorf("%s: %w", route, err)
			}
			op.order = len(c.ops)
			c.ops[route] = op
			c.byID[op.id] = route
		}
	}
	if len(c.ops) == 0 {
		return contract{}, fmt.Errorf("no operations under paths")
	}
	return c, nil
}

func (c *contract) operation(o operationDoc, params []parameter, defs map[string]parameter) (contractOp, error) {
	op := contractOp{id: o.ID, name: o.Name, summary: strings.TrimSpace(o.Summary), callers: map[string]bool{}, codes: o.ErrorCodes}
	if op.id == "" || op.name == "" || op.summary == "" {
		return op, fmt.Errorf("lacks operationId, x-operation-name or summary")
	}
	if o.Body != nil {
		media, ok := o.Body.Content["application/json"]
		if len(o.Body.Content) != 1 || !ok || !strings.HasPrefix(media.Schema.Ref, "#/components/schemas/") {
			return op, fmt.Errorf("a request body must be application/json alone, with a $ref to a component schema")
		}
		op.bodyRef, op.bodyRequired = media.Schema.Ref, o.Body.Required
	}
	if o.Security == nil || len(*o.Security) == 0 {
		return op, fmt.Errorf("states no security")
	}
	for _, req := range *o.Security {
		if len(req) != 1 {
			return op, fmt.Errorf("a security requirement must name exactly one scheme: %v", req)
		}
		for scheme, scopes := range req {
			if !schemes[scheme] || len(scopes) != 0 {
				return op, fmt.Errorf("unknown security %q %v", scheme, scopes)
			}
			op.callers[scheme] = true
		}
	}
	for _, p := range params {
		if p.Ref != "" {
			name, ok := strings.CutPrefix(p.Ref, "#/components/parameters/")
			def, found := defs[name]
			if !ok || !found || def.Ref != "" {
				return op, fmt.Errorf("parameter $ref %q does not resolve to a component parameter", p.Ref)
			}
			p = def
		}
		switch {
		case p.Name == "api-version" && p.In == "query":
			if !p.Required {
				return op, fmt.Errorf("api-version must be required")
			}
			op.versioned = true
		case strings.EqualFold(p.Name, "Idempotency-Key") && p.In == "header":
			s := p.Schema
			if s.Ref != "" || s.MinLength == nil || s.MaxLength == nil || *s.MinLength > *s.MaxLength {
				return op, fmt.Errorf("Idempotency-Key needs an inline schema with minLength and maxLength")
			}
			if c.keyMax != 0 && (c.keyMin != *s.MinLength || c.keyMax != *s.MaxLength) {
				return op, fmt.Errorf("Idempotency-Key length differs from another operation's")
			}
			c.keyMin, c.keyMax = *s.MinLength, *s.MaxLength
			op.idempotent = true
		}
	}
	return op, nil
}

type pair struct {
	key   string
	value *yaml.Node
}

// pairs returns a YAML mapping's entries in document order.
func pairs(n *yaml.Node, what string) ([]pair, error) {
	if n.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s is not a mapping", what)
	}
	var out []pair
	for i := 0; i+1 < len(n.Content); i += 2 {
		out = append(out, pair{n.Content[i].Value, n.Content[i+1]})
	}
	return out, nil
}

// RetryRule returns the retry rule of the embedded contract's x-limits: how many times a call that
// failed retryably is tried again, and how long each retry waits.
func RetryRule() retry.Rule { return engineContract.retry }

// retryRuleOf returns the rule an OpenAPI document gives.
func retryRuleOf(openapi []byte) (retry.Rule, error) { return retry.Parse(openapi) }

// Retention is how long the embedded contract's x-limits keep an idempotency key, and an ended
// operation, after it ends (idempotency-key-retention-hours); the durable service keeps a closed
// workflow as long (ADR 0070).
func Retention() time.Duration { return engineContract.keepKeys }
