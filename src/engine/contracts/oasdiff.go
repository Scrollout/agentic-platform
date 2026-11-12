// Package contracts judges a contract revision against its baseline with oasdiff, used as a Go
// library the way `oasdiff breaking` runs in scripts/contract-diff.sh: all of oasdiff's checks,
// its default severity levels and options, reporting warnings and errors. Every finding's ID,
// level and text come from oasdiff's checker; this package adds no rule of its own (Principle VI).
//
// Contracts come from a model and a developer, so loading is closed: no external $ref is
// followed (no file or network read), an input above maxContractBytes is refused, and a
// document that is not OpenAPI 3 is an error, never an empty result.
package contracts

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/oasdiff/oasdiff/checker"
	"github.com/oasdiff/oasdiff/diff"
	"github.com/oasdiff/oasdiff/load"

	"github.com/agentic-platform/engine/slices"
)

// maxContractBytes bounds each document handed to the diff, so a huge input cannot exhaust
// memory while it is parsed.
const maxContractBytes = 4 << 20

// errExternalRef is what the loader's reader answers for every document it is asked to read.
var errExternalRef = errors.New("external $ref refused: a contract must be self-contained")

// Diff compares contracts with oasdiff. The zero value is not usable; call NewDiff.
type Diff struct{ localizer checker.Localizer }

var _ slices.ContractDiff = (*Diff)(nil)

// NewDiff returns oasdiff's diff with English finding texts.
func NewDiff() *Diff { return &Diff{localizer: checker.NewDefaultLocalizer()} }

// Diff returns oasdiff's breaking-change findings from base to revision. base "" stands for an
// empty document (no operation), so a first contract adds and never breaks. An error means the
// comparison could not be made: the caller must treat it as a failed check.
func (d *Diff) Diff(ctx context.Context, base, revision string) (out []slices.ContractFinding, err error) {
	// A panic in the libraries on hostile input fails the comparison instead of the engine.
	defer func() {
		if r := recover(); r != nil {
			out, err = nil, fmt.Errorf("the contracts could not be compared: %v", r)
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rev, err := loadSpec(ctx, revision, "revision")
	if err != nil {
		return nil, err
	}
	var bas *load.SpecInfo
	if base == "" {
		bas = emptySpec(rev.Spec.OpenAPI)
	} else if bas, err = loadSpec(ctx, base, "base"); err != nil {
		return nil, err
	}

	report, sources, err := diff.GetWithOperationsSourcesMap(diff.NewConfig(), bas, rev)
	if err != nil {
		return nil, fmt.Errorf("oasdiff could not compare the contracts: %w", err)
	}
	cfg := checker.NewConfig(checker.GetAllChecks())
	changes := checker.CheckBackwardCompatibilityUntilLevel(cfg, report, sources, checker.WARN)
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	out = make([]slices.ContractFinding, 0, len(changes))
	for _, c := range changes {
		out = append(out, slices.ContractFinding{Level: level(c.GetLevel()), ID: c.GetId(), Text: d.text(c)})
	}
	return out, nil
}

// text is oasdiff's own wording of a change, prefixed with the operation and path it concerns.
func (d *Diff) text(c checker.Change) string {
	parts := []string{c.GetOperation(), c.GetPath(), c.GetUncolorizedText(d.localizer)}
	if comment := c.GetComment(d.localizer); comment != "" {
		parts = append(parts, "("+comment+")")
	}
	var nonEmpty []string
	for _, p := range parts {
		if p != "" {
			nonEmpty = append(nonEmpty, p)
		}
	}
	return strings.Join(nonEmpty, " ")
}

// level maps oasdiff's level to the finding's: "error", "warn" or "info".
func level(l checker.Level) string {
	switch l {
	case checker.ERR:
		return "error"
	case checker.WARN:
		return "warn"
	default:
		return "info"
	}
}

// loadSpec parses one contract, JSON or YAML, from memory, the way the oasdiff CLI loads a
// file, except that it never reads anything else.
func loadSpec(ctx context.Context, doc, side string) (*load.SpecInfo, error) {
	if len(doc) > maxContractBytes {
		return nil, fmt.Errorf("the %s contract is %d bytes, above the limit of %d", side, len(doc), maxContractBytes)
	}
	if strings.TrimSpace(doc) == "" {
		return nil, fmt.Errorf("the %s contract is empty", side)
	}
	loader := openapi3.NewLoader()
	loader.Context = ctx
	loader.IncludeOrigin = true
	loader.IsExternalRefsAllowed = false
	loader.ReadFromURIFunc = func(*openapi3.Loader, *url.URL) ([]byte, error) { return nil, errExternalRef }

	// A fresh loader per side, as oasdiff advises, so its document cache cannot mix them up.
	info, err := load.NewSpecInfoFromData(loader, []byte(doc), side, load.WithLowercaseHeaders())
	if err != nil {
		return nil, fmt.Errorf("the %s contract cannot be loaded: %w", side, err)
	}
	if v := info.Spec.OpenAPI; !strings.HasPrefix(v, "3.") {
		return nil, fmt.Errorf("the %s contract is not an OpenAPI 3 document (openapi: %q)", side, v)
	}
	return info, nil
}

// emptySpec is the baseline of a slice whose contract was never approved: no paths at all, in
// the revision's OpenAPI version so the two documents differ only in what the revision adds.
func emptySpec(version string) *load.SpecInfo {
	spec := &openapi3.T{
		OpenAPI: version,
		Info:    &openapi3.Info{Title: "empty baseline", Version: "0"},
		Paths:   openapi3.NewPaths(),
	}
	return &load.SpecInfo{Url: "base", Spec: spec, Version: "0"}
}
