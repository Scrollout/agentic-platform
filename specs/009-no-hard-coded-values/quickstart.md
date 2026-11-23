# Quickstart: Code that hard-codes a declared value is rejected

How to see the feature work. Partial runs while working; the full test command is the CI run of
the pushed commit.

## 1. The checks alone (seconds, no Docker)

From `src/engine`:

```sh
go test ./checks/ -run 'Declared|Deployment'   # DeclaredValues, RenderDeclared, the three checks
go test ./procedures/ -run 'Declared'
go test ./checksvc/ -run 'Declared|Deployment'
```

Expected: a project whose `internal/` compares a text with `200`, where the contract declares
`maxLength: 200`, fails DeclaredValuesNotRepeated naming the file, line,
`/components/schemas/.../maxLength` and `gen.Declared...`; the same code using the constant
passes; `":8080"`, `"http://notes.local"`, `SetBasicAuth("a", "b")` and `os.Getenv("PORT")` in
`cmd/` each fail DeploymentValuesConfigured; `gen.ListenAddress()` passes. A planted file that
does not parse fails both checks.

## 2. Generation (seconds, needs the pinned oapi-codegen)

`go test ./procedures/` with `AP_OAPI_CODEGEN` set: a contract with a declared limit gives
`gen/declared.go` with its constant, the same bytes in two runs; `gen/config.go` is in the output;
GenerationRepeatable and DeclaredValuesGenerated pass, and an edited `gen/declared.go` fails
DeclaredValuesGenerated.

## 3. The step evals' graders (minutes, no model)

From `src/tests`: `go test ./evals/steps/ -run 'Grade'`: the reference implementation passes
every code grader, and each planted hard-coded value fails its grader.

## 4. The planted failures and the build path (CI)

The acceptance suite (`TestPlanted` and the new `TestPlantedValues`, recorded model, Docker)
runs in CI on push: `hard-coded-declared-value` and `hard-coded-listen-address` are each stopped
by their check in 3 of 3 trials; `TestUS2` still reaches done with the renewed recordings.

## 5. The code agent's live evals (paid, owner's go-ahead)

Dispatch the evals workflow for the steps suite on the branch: `code/inject-hard-code` passes^3,
and every earlier case passes as before (SC-004).
