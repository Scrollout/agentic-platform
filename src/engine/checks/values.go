package checks

// The code step's value checks (catalogue README, "Declared values" and "Deployment values";
// ADR 0084, ADR 0085): a value the contract declares, or a value that changes with where the
// service runs, is never written as a literal in the step's own code.

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// reasonValueMax is how many characters of a model-written value a reason quotes.
const reasonValueMax = 64

// cutValue is s cut to reasonValueMax characters, quoted as Go's %q.
func cutValue(s string) string {
	r := []rune(s)
	if len(r) <= reasonValueMax {
		return fmt.Sprintf("%q", s)
	}
	return fmt.Sprintf("%q...", string(r[:reasonValueMax]))
}

// notJudged are the literals a declared value is never compared with (catalogue README).
var notJudged = []constant.Value{constant.MakeInt64(0), constant.MakeInt64(1), constant.MakeInt64(-1)}

// literals calls visit for every integer, floating-point and string literal of f that is not an
// import path or a struct tag, with the expression it stands in (a unary minus over it, when
// there is one) and its value (negated under that minus).
func literals(f *ast.File, visit func(at ast.Expr, lit *ast.BasicLit, v constant.Value)) {
	skip := map[*ast.BasicLit]bool{}
	for _, imp := range f.Imports {
		skip[imp.Path] = true
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.Field:
			if n.Tag != nil {
				skip[n.Tag] = true
			}
		case *ast.UnaryExpr:
			if lit, ok := n.X.(*ast.BasicLit); ok && n.Op == token.SUB && !skip[lit] && (lit.Kind == token.INT || lit.Kind == token.FLOAT) {
				skip[lit] = true
				if v := constant.MakeFromLiteral(lit.Value, lit.Kind, 0); v.Kind() != constant.Unknown {
					visit(n, lit, constant.UnaryOp(token.SUB, v, 0))
				}
			}
		case *ast.BasicLit:
			if skip[n] || (n.Kind != token.INT && n.Kind != token.FLOAT && n.Kind != token.STRING) {
				return true
			}
			if v := constant.MakeFromLiteral(n.Value, n.Kind, 0); v.Kind() != constant.Unknown {
				visit(n, n, v)
			}
		}
		return true
	})
}

// DeclaredValuesNotRepeated judges the step's own non-test .go files (matched by writable)
// against the approved contract's values: a literal that equals a declared number or string, or
// an enum value, fails, naming the file, the line, the value, the pointers that declare it and
// its Go home. approvedContract is the contract's YAML text.
func DeclaredValuesNotRepeated(project string, writable []string, approvedContract []byte) []CheckResult {
	const check = "DeclaredValuesNotRepeated"
	values, bad := projectDeclared(check, project, approvedContract)
	if bad != nil {
		return []CheckResult{*bad}
	}
	var judged []Declared
	for _, d := range values {
		if d.Value != nil && (d.Kind == kindNumber || d.Kind == kindString) {
			judged = append(judged, d)
		}
	}
	fset, paths, files, failed := judgedGoFiles(check, project, writable)
	if failed != nil {
		return failed
	}
	var out []CheckResult
	examined := 0
	for i, f := range files {
		literals(f, func(at ast.Expr, lit *ast.BasicLit, v constant.Value) {
			examined++
			if v.Kind() == constant.String {
				if constant.StringVal(v) == "" {
					return
				}
			} else {
				for _, e := range notJudged {
					if constant.Compare(v, token.EQL, e) {
						return
					}
				}
			}
			var match []Declared
			for _, d := range judged {
				if equalValue(v, d.Value) {
					match = append(match, d)
				}
			}
			if match == nil {
				return
			}
			shown := sourceText(at, lit)
			if v.Kind() == constant.String {
				shown = constant.StringVal(v)
			}
			out = append(out, fail(check, fmt.Sprintf("%s:%d repeats %s, declared at %s: %s; if the literal does not stand for this value, write it another way",
				paths[i], fset.Position(at.Pos()).Line, cutValue(shown), pointers(match), home(match))))
		})
	}
	if out != nil {
		return out
	}
	return []CheckResult{pass(check, fmt.Sprintf("examined %d files and %d literals against %d declared values: none repeated", len(files), examined, len(judged)))}
}

// equalValue says whether a literal's value equals a declared one: numbers by exact value,
// strings byte for byte, and never a string and a number.
func equalValue(a, b constant.Value) bool {
	if (a.Kind() == constant.String) != (b.Kind() == constant.String) {
		return false
	}
	return constant.Compare(a, token.EQL, b)
}

// sourceText is a number literal as written, with its minus when at is a unary minus over it.
func sourceText(at ast.Expr, lit *ast.BasicLit) string {
	if _, ok := at.(*ast.UnaryExpr); ok {
		return "-" + lit.Value
	}
	return lit.Value
}

func pointers(ds []Declared) string {
	var ps []string
	for _, d := range ds {
		ps = append(ps, "#"+d.Pointer) // after #, so the service does not take it for a host path
	}
	return strings.Join(ps, ", ")
}

// home says where the values in ds live in Go.
func home(ds []Declared) string {
	var names []string
	enum := false
	for _, d := range ds {
		if d.Name != "" {
			names = append(names, "gen."+d.Name)
		} else {
			enum = true
		}
	}
	const enumHome = "the enum constant oapi-codegen generated for it in gen/api.gen.go, where it exists"
	switch {
	case names == nil:
		return "use " + enumHome
	case enum:
		return "use " + strings.Join(names, " or ") + ", or " + enumHome
	default:
		return "use " + strings.Join(names, " or ")
	}
}

// The kinds of deployment value.
const (
	kindAddress    = "an address"
	kindCredential = "a credential" //nolint:gosec // the label of a kind of value, not a credential
	kindEnvRead    = "an environment read"
)

// credentialName is a name that holds a credential (catalogue README, "Deployment values").
var credentialName = regexp.MustCompile(`(?i)passw(or)?d|secret|token|api_?key|credential|private_?key`)

// credentialHeaders are the headers whose value is a credential, in lower case.
var credentialHeaders = map[string]bool{"authorization": true, "proxy-authorization": true, "cookie": true, "x-api-key": true}

// envReads are the functions of package os that read the environment.
var envReads = map[string]bool{"Getenv": true, "LookupEnv": true, "Environ": true, "ExpandEnv": true}

// hostName is a host name as a host:port literal may hold one.
var hostName = regexp.MustCompile(`^[A-Za-z0-9._-]*[A-Za-z][A-Za-z0-9._-]*$`)

// DeploymentValuesConfigured judges the step's own non-test .go files (matched by writable): an
// address, a credential or a read of the environment fails, naming the file, the line and the
// kind, never the literal's text.
func DeploymentValuesConfigured(project string, writable []string) []CheckResult {
	const check = "DeploymentValuesConfigured"
	fset, paths, files, failed := judgedGoFiles(check, project, writable)
	if failed != nil {
		return failed
	}
	var out []CheckResult
	for i, f := range files {
		for _, o := range deploymentOffences(f) {
			out = append(out, fail(check, fmt.Sprintf("%s:%d holds %s: belongs in configuration: read it through gen/config.go",
				paths[i], fset.Position(o.at).Line, o.kind)))
		}
	}
	if out != nil {
		return out
	}
	return []CheckResult{pass(check, fmt.Sprintf("examined %d files: no address, credential or environment read", len(files)))}
}

type offence struct {
	at   token.Pos
	kind string
}

// deploymentOffences are f's deployment values in source order.
func deploymentOffences(f *ast.File) []offence {
	osNames := map[string]bool{}
	for _, imp := range f.Imports {
		if p, _ := strconv.Unquote(imp.Path.Value); p == "os" {
			name := "os"
			if imp.Name != nil {
				name = imp.Name.Name
			}
			if name != "_" && name != "." {
				osNames[name] = true
			}
		}
	}
	handled := map[*ast.BasicLit]bool{}
	var out []offence
	// credential reports e as a credential when it is a non-empty literal (or literals joined by +).
	credential := func(e ast.Expr) {
		s, lits, ok := literalText(e)
		if !ok {
			return
		}
		for _, l := range lits {
			handled[l] = true
		}
		if s != "" {
			out = append(out, offence{e.Pos(), kindCredential})
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.CallExpr:
			sel, ok := n.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if x, ok := sel.X.(*ast.Ident); ok && osNames[x.Name] && envReads[sel.Sel.Name] {
				out = append(out, offence{n.Pos(), kindEnvRead})
			}
			switch sel.Sel.Name {
			case "SetBasicAuth":
				for _, a := range n.Args {
					credential(a)
				}
			case "Set", "Add":
				if len(n.Args) == 2 {
					if h, _, ok := literalText(n.Args[0]); ok && credentialHeaders[strings.ToLower(h)] {
						credential(n.Args[1])
					}
				}
			}
		case *ast.AssignStmt:
			if len(n.Lhs) == len(n.Rhs) {
				for j, l := range n.Lhs {
					if credentialName.MatchString(exprName(l)) {
						credential(n.Rhs[j])
					}
				}
			}
		case *ast.ValueSpec:
			if len(n.Names) == len(n.Values) {
				for j, id := range n.Names {
					if credentialName.MatchString(id.Name) {
						credential(n.Values[j])
					}
				}
			}
		case *ast.KeyValueExpr:
			if credentialName.MatchString(exprName(n.Key)) {
				credential(n.Value)
			}
		}
		return true
	})
	literals(f, func(_ ast.Expr, lit *ast.BasicLit, v constant.Value) {
		if handled[lit] || v.Kind() != constant.String {
			return
		}
		if isAddress(constant.StringVal(v)) {
			out = append(out, offence{lit.Pos(), kindAddress})
		}
	})
	sort.SliceStable(out, func(i, j int) bool { return out[i].at < out[j].at })
	return out
}

// exprName is the name an expression assigns to or keys: an identifier, a selector's field, or
// a string key; "" otherwise.
func exprName(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return e.Sel.Name
	case *ast.BasicLit:
		if s, err := strconv.Unquote(e.Value); err == nil && e.Kind == token.STRING {
			return s
		}
	}
	return ""
}

// literalText is the text of a string literal, or of string literals joined by +, with the
// literals it is made of.
func literalText(e ast.Expr) (string, []*ast.BasicLit, bool) {
	switch e := e.(type) {
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return "", nil, false
		}
		s, err := strconv.Unquote(e.Value)
		return s, []*ast.BasicLit{e}, err == nil
	case *ast.ParenExpr:
		return literalText(e.X)
	case *ast.BinaryExpr:
		if e.Op != token.ADD {
			return "", nil, false
		}
		a, la, ok := literalText(e.X)
		if !ok {
			return "", nil, false
		}
		b, lb, ok := literalText(e.Y)
		return a + b, append(la, lb...), ok
	}
	return "", nil, false
}

// isAddress says whether s is a URL with a scheme and a host, an IP address, or host:port with a
// port from 1 to 65535 and a host that is empty, localhost, an IP address or a name.
func isAddress(s string) bool {
	if u, err := url.Parse(s); err == nil && u.Scheme != "" && u.Host != "" {
		return true
	}
	if _, err := netip.ParseAddr(s); err == nil {
		return true
	}
	host, port, err := net.SplitHostPort(s)
	if err != nil || port == "" || strings.Trim(port, "0123456789") != "" {
		return false
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return false
	}
	if host == "" || host == "localhost" || hostName.MatchString(host) {
		return true
	}
	_, err = netip.ParseAddr(host)
	return err == nil
}
