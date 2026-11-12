package api

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// The contract the generated code in gen/ comes from: scripts/generate.sh generates from the
// newest contract version, and so does loadContract. The facts a client sees and the contract
// states (the version served, which token may call which operation, which operations take an
// api-version or an Idempotency-Key, the operations list and the limits in `x-limits`) are read
// from it here, so they have one home. Add stable/*/openapi.yaml to the pattern when the first
// stable version exists.
//
//go:embed preview/*/openapi.yaml
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
}

type contract struct {
	version        string                // the api-version served
	ops            map[string]contractOp // by route, "GET /slices/{sliceId}"
	perSecond      int                   // requests in any one-second window per token
	keepKeys       time.Duration         // how long an idempotency key is kept
	maxBody        int64                 // largest request body, in bytes
	keyMin, keyMax int                   // Idempotency-Key length
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
		Limits struct {
			PerSecond int   `yaml:"requests-per-second-per-token"`
			KeepHours int   `yaml:"idempotency-key-retention-hours"`
			MaxBody   int64 `yaml:"request-body-max-bytes"`
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
	return c, nil
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
	l := d.Limits
	if l.PerSecond < 1 || l.KeepHours < 1 || l.MaxBody < 1 {
		return contract{}, fmt.Errorf("x-limits lacks a positive requests-per-second-per-token, idempotency-key-retention-hours or request-body-max-bytes")
	}
	c := contract{version: version, ops: map[string]contractOp{}, perSecond: l.PerSecond,
		keepKeys: time.Duration(l.KeepHours) * time.Hour, maxBody: l.MaxBody}

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
		}
	}
	if len(c.ops) == 0 {
		return contract{}, fmt.Errorf("no operations under paths")
	}
	return c, nil
}

func (c *contract) operation(o operationDoc, params []parameter, defs map[string]parameter) (contractOp, error) {
	op := contractOp{id: o.ID, name: o.Name, summary: strings.TrimSpace(o.Summary), callers: map[string]bool{}}
	if op.id == "" || op.name == "" || op.summary == "" {
		return op, fmt.Errorf("lacks operationId, x-operation-name or summary")
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
