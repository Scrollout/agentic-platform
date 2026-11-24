# api-feature-go

Builds one use case of one service as a Go HTTP API from an approved slice. The declaration is
[`pattern.yaml`](pattern.yaml), valid against the engine's
[`pattern.schema.json`](../../engine/api/preview/2026-11-12-preview/pattern.schema.json). This file
states what its values mean, so the checks can be written from it.

## Fit conditions

A slice fits when all four pass. Each one is a rule on the slice's `contract` (YAML text) and
`rules`. None calls a model. The checks run when the contract is approved and again at `startRun`.
A failing condition is reported as a `check_result` with `check` set to the condition's name,
`part` set as below, `passed: false`, and a `reason` that names what failed. The contract is
parsed once as a single YAML document. Text that does not parse, holds more than one document, or
has a duplicate key fails every contract condition (fail closed).

| Condition | Part | Passes when | Reason on failure names |
|-----------|------|-------------|-------------------------|
| `ContractIsOpenApi31` | `contract` | the root is a mapping and its `openapi` value is a string matching `^3\.1\.[0-9]+$` | the value found, or that it is missing |
| `OnlyHttpOperations` | `contract` | `paths` is a mapping with at least one operation. Each path item has only the keys `get`, `put`, `post`, `delete`, `options`, `head`, `patch`, `trace`, `summary`, `description`, `servers`, `parameters` or `x-*`, and no `$ref`. No operation has a non-empty `callbacks`. `webhooks` and `components.pathItems` are absent or empty | each offending location, for example `webhooks/noteAdded` or `paths//notes/post/callbacks` |
| `ContractRefsLocal` | `contract` | every `$ref` value in the document is a string starting with `#/` | the location of each other `$ref` |
| `RulesNameOneSystem` | `rules` | there is at least one rule, and every rule's `system` is byte-for-byte equal to the first rule's | the distinct names, with the rule IDs that use each |

What the conditions keep out:

- `OnlyHttpOperations` admits only operations a client calls over HTTP. It refuses webhooks and
  callbacks, which the service would send, and refuses keys this version does not know
  (fail closed).
- `ContractRefsLocal` is there because generation runs without network, and a reference outside
  the document cannot be resolved there.
- `RulesNameOneSystem` compares the names exactly, with no trimming or case folding. If the slice
  says "the notes service" in one rule and "notes service" in another, it fails, and the developer
  is asked which name is right. The rule does not compare the name with the contract's
  `info.title`, because nothing links the two deterministically.

## Steps and project layout

Paths are relative to the new project's folder. Each step's container has only its own paths
mounted writable.

| Step | Worker | Writes | Accepted when |
|------|--------|--------|---------------|
| contract-check | procedure | `api/openapi.yaml`, `features/slice.feature` (copies of the approved contract and examples) | both hashes equal the approved hashes, Spectral passes, nothing else changed |
| generate | procedure | `gen/**` (oapi-codegen output and the stub), `go.mod`, `go.sum`, `vendor/**`, `internal/app/app.go` (the seam) | a second run gives identical files, the code builds, dependencies are allowed, nothing else changed |
| tests | agent | `tests/**` (godog step definitions) | the tests build, godog `--strict` binds and runs every example (pass or fail), the tests fail against the stub, dependencies are allowed, nothing else changed |
| code | agent | `internal/**`, `cmd/**` (replacing the seam) | all tests pass, and tests, generated code (the seam aside) and contract are unchanged, dependencies are allowed, nothing else changed |
| review | agent | nothing | every blocking finding carries evidence a check confirms, and none is open |

The seam (owner, 2026-11-13): the tests reach the code only through
`example.com/service/internal/app`, serving `gen.Handler(app.New())`. The generate procedure writes
`internal/app/app.go` (not part of the template) with `func New() gen.ServerInterface { return gen.Stub{} }`,
so the tests build and fail before any code exists. The code step replaces that file with the real
implementation (it may add packages under `internal/`) and keeps `New`'s signature. Since both
steps may write it, the "unchanged" checks of the code step do not judge it, and each step's
DependenciesAllowed judges only that step's own writable files.

`inputs` and `outputs` name artifacts. `slice/<part>` is an approved part of the slice. Any other
name is the accepted output of the earlier step that lists it under `outputs`.

## Module template

The generate step starts from [`template/`](template/), the new project's module before
generation. The generate procedure copies it, adds the approved contract, and writes the result
into the project, except `vendortools/`.

| File | What it declares |
|------|------------------|
| `template/go.mod` | The module path `example.com/service` (fixed, so the same contract gives the same files) and the pinned modules: `github.com/oapi-codegen/runtime v1.8.0`, the engine's version, as the one direct require, and `github.com/cucumber/godog v0.16.0`, the `src/deploy/images.lock` version, with the modules both need, marked `// indirect` |
| `template/go.sum` | The hash of every module above. The procedure vendors with `GOPROXY=off` and refuses the result when `go mod vendor` would change `go.mod` or `go.sum` or a module disagrees with its hash |
| `template/gen/oapi-codegen.yaml` | The project's oapi-codegen settings: package `gen`, strict server on `net/http`, models, output `gen/api.gen.go` |
| `template/gen/generate.go` | The `//go:generate oapi-codegen` line. The procedure runs it with the pinned oapi-codegen in `gen/`, and `GenerationRepeatable` runs it again with `go generate` |
| `template/gen/config.go` | The service's configuration, see [Configuration](#configuration) |
| `template/vendortools/tools.go` | The packages vendored besides those the generated code imports, so later steps build offline from `vendor/`: godog for the tests step, and the runtime packages generated code may import. Its `tools` build tag keeps it out of every build, and the procedure never writes it into the project |

The procedure also writes `gen/stub.go`, a `Stub` that answers every operation of the generated
`ServerInterface` with 501 Not Implemented, for the tests to fail against, `gen/declared.go`, the
contract's declared values as Go constants (see [Declared values](#declared-values)), and the seam
`internal/app/app.go`, whose `New` returns it.

## Declared values

A value the approved contract declares has one home in Go: a constant in `gen/declared.go`, or,
for an enum value, the constant oapi-codegen generates for it in `gen/api.gen.go` where it
generates one. The code step's code takes it from there and never repeats it as a literal. One
collector (`DeclaredValues` in `src/engine/checks`) finds the values for the generate procedure
and for both checks below, so they cannot disagree.

What the collector finds. The contract is parsed as the fit conditions parse it (one YAML
document, no duplicate key; anything else is an error, and the generate step and both checks fail
naming it).

- A **declared value** is the scalar under a mapping key `maxLength`, `minLength`, `maximum`,
  `minimum`, `exclusiveMaximum`, `exclusiveMinimum`, `maxItems`, `minItems`, `maxProperties`,
  `minProperties`, `multipleOf`, `default` or `const`. An **enum value** is a scalar item of a
  sequence under a key `enum`. A key whose value is not a scalar declares nothing (a property
  named `default` is a mapping, so it declares nothing).
- The walk is depth first in document order, mapping keys in their written order. A key
  `example`, `examples` or one starting `x-` hides its whole subtree, even when it names a
  property. Aliases and merge keys (`<<`) are not followed: a value under an anchor is found once,
  at the anchor.
- Each value has a JSON Pointer (RFC 6901) to it: `~` is written `~0`, `/` is written `~1`, and a
  sequence item is its 0-based index.
- Its kind comes from the YAML node's resolved tag. `!!str` is a string (so a quoted `"200"` is a
  string), `!!bool` a boolean and `!!null` a null. An `!!int` or `!!float` is a number when its
  text, after dropping a leading `+` and putting `0` before a leading `.` or `-.`, matches
  `^-?(0|[1-9][0-9]*)(\.[0-9]*)?([eE][-+]?[0-9]+)?$`. Any other number text (`.inf`, `.nan`,
  `010`, `0x1F`, `0o17`, `1_000`) is unjudged, since YAML and Go read it differently.
- Values stated only in the rules or their examples are not declared values.

Limits of the collector:

- An unquoted date or time (`default: 2020-01-01` resolves to `!!timestamp`) is unjudged; quoted,
  it is a string.
- A keyword whose value is a mapping or a sequence declares nothing itself, but the walk goes on
  inside it, so `default: {maxLength: 5}` declares 5 (its pointer ends `/default/maxLength`).
- A value under a hidden key (`example`, `examples`, `x-`) is never found, not through an alias
  outside it either, since aliases are not followed.
- An integer the Go compiler cannot hold as a constant (more than 512 bits, about 155 digits) is a
  declared number, so `gen/declared.go` does not build and the generate procedure refuses the
  step, naming the build's failure.

`gen/declared.go`. Line 1 is `// Code generated by the generate procedure from api/openapi.yaml.
DO NOT EDIT.`, then a blank line and `package gen`. Then, for each declared number or string in
document order (not enum items, booleans, nulls or unjudged numbers): a blank line, the comment
`// <Name> is declared at <pointer>.` with the pointer written as Go's `%q`, and
`const <Name> = <value>`, a string written as Go's `strconv.Quote` and a number as its text after
the normalisation above (`0.5` stays `0.5`, `2e2` stays `2e2`). With no such value the file is the
header and `package gen` only. The same contract always gives the same bytes.

`<Name>` is `Declared` followed by the pointer's segments, without the segments `components`,
`schemas`, `properties`, `paths`, `content` and `schema`, each decoded (`~1`, `~0`), every
character outside `[A-Za-z0-9]` removed, and the first character of the segment and the one after
each removed run upper-cased: `/components/schemas/NewNote/properties/text/maxLength` is
`DeclaredNewNoteTextMaxLength`. A name already given, or declared at the top level of another
file in `gen/`, gets the first free suffix of `_2`, `_3`, and so on. A name follows the contract:
changing the contract can rename a constant.

The checks:

- **DeclaredValuesGenerated** (generate step): `gen/declared.go` is byte for byte the file above
  for the approved contract. It fails naming a missing or differing file.
- **DeclaredValuesNotRepeated** (code step): judges every `.go` file the step's `writable`
  matches, except `_test.go` files. Each integer, floating-point and string literal is compared
  with the declared numbers and strings and with the enum values: a number by its exact value
  (`200`, `0xC8`, `2e2` and `200.0` are equal; a literal under a unary minus is negated), a string
  after unquoting, byte for byte (interpreted or raw); a string never equals a number. A literal
  that equals one fails, naming the file, the line, the value (as `%q`, cut to 64 characters),
  every pointer that declares it (written after `#`, as `#/components/...`) and its constant, and ends by saying to write the literal another
  way if it does not stand for that value. A pass says how many files, literals and declared
  values it examined.

Limits of DeclaredValuesNotRepeated:

- `0`, `1`, `-1` and `""` (in any spelling of the same value) are never judged, so a declared
  value equal to one is not caught. Import paths, struct tags, comments, rune and imaginary
  literals are not judged either.
- A value computed from several literals, or a parenthesised negative (`-(5)`), is not caught.
- Booleans, nulls and unjudged numbers are not judged.
- A literal equal to a declared value that does not stand for it (a collision, such as the base
  `10` of `strconv.ParseInt(s, 10, 64)` when the contract declares `default: 10`) is rejected; the
  code says it another way (`strconv.Atoi`, a named standard-library constant).
- The check service rewrites every reason so that it names no host path: a quoted value that
  starts with `/` (a declared `"/health"`) is shown as `"<host path>"`; the pointers, written after
  `#`, stay as they are.
- The line a reason names is the one Go's `//line` directives give, so such a directive in a judged
  file moves it.

## Configuration

Every value that changes with where the service runs is read once, when it starts, through
`gen/config.go`, which the template declares and whose doc comment is the project's documentation
of its configuration:

| Variable | Read by | Default (unset or empty) | Meaning |
|----------|---------|--------------------------|---------|
| `LISTEN_ADDRESS` | `gen.ListenAddress()` | `:8080` | The address the service listens on |

The code step serves `gen.Handler(app.New())` on `gen.ListenAddress()`. Its own code never reads
the environment.

## Deployment values

**DeploymentValuesConfigured** (code step) judges the same files as DeclaredValuesNotRepeated and
fails once per offence, naming the file, the line and its kind, and saying it belongs in
configuration, read through `gen/config.go`. The reason never quotes the literal, since a URL can
carry a password. A pass says how many files it examined. The forms it recognises:

| Kind | Found when |
|------|------------|
| an address | a string literal that is a URL with a scheme and a host; an IP address; or `host:port` or `:port` whose port is a decimal number from 1 to 65535 and whose host is empty, `localhost`, an IP address or a name holding a letter |
| a credential | a non-empty literal argument of `SetBasicAuth`; the second argument of `Set` or `Add` when the first is the literal `Authorization`, `Proxy-Authorization`, `Cookie` or `X-Api-Key` (any letter case); a literal assigned, declared or keyed to a name matching `(?i)passw(or)?d\|secret\|token\|api_?key\|credential\|private_?key` |
| an environment read | any use of `os.Getenv`, `os.LookupEnv`, `os.Environ` or `os.ExpandEnv` (a call, a function value, an argument) under any import name of `os`, and of their bare names under a dot import of `os` |

A literal argument may be several literals joined by `+`. Limits: a port written as a number,
`":0"`, a credential under another name or in another call, and a value built at run time are not
recognised. Some literals that are not deployment values are rejected: `"1.2.3.4"` is judged an
address, and `const tokenHeader = "X-Api-Key"` and a name such as `secretaryNote` a credential;
under a dot import of `os`, a field, method, key or local named like one of the four functions is
judged an environment read.
Address forms not recognised: a bare host name (`"notes.local"`), a URL without a scheme
(`"//notes.local"`), an address with surrounding spaces, a non-ASCII host in `host:port`, and an
address joined from literals with `+` (each literal is judged alone). Credential forms not
recognised: a literal inside a conversion or call (`[]byte("x")`), a map index key
(`m["password"] = "x"`), a positional struct literal, a returned literal, and `url.UserPassword`.
The line a reason names follows `//line` directives, as for DeclaredValuesNotRepeated.

DeclaredValuesGenerated and DeclaredValuesNotRepeated read the approved contract the engine sends,
not the project's `api/openapi.yaml`; DeploymentValuesConfigured does not use it. Each fails
closed, naming what it could not read: the two declared-value checks an approved contract that is
missing or does not parse, and all three a judged file that does not parse as Go. None of them
runs the project's code.

## Dependencies

Each entry is a Go package pattern: `std` (the standard library), an import path, or a path
ending in `/...` (that path and everything below it), as defined by the
[go command](https://pkg.go.dev/cmd/go#hdr-Package_lists_and_patterns). The check matches every
package the step's own writable files import from outside the project's own module, directly or
not, as `go list -deps` reports it; another step's files are judged by that step's lists. An import must match an `allowed` entry and no `forbidden` entry.
`forbidden` wins over `allowed`, which is how `os/exec` and `syscall` stay out even though `std`
is allowed.

## Budgets

Each budget is reached by whichever of tokens, cost or minutes comes first, and the run is then
handed over. The step budget applies to one attempt, or to one review round. The numbers are
explained in `pattern.yaml`: they come from Claude Haiku's price and the ADR 0026 spike's measured
run.

## Agent digests

The `tests`, `code` and `review` steps carry an all-zero `agent_digest` until T049 writes their
agent definitions in `src/agents/`. No definition hashes to all zeros, so a run fails closed
until then.
