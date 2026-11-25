# Quickstart: The delivered service refuses requests its contract does not allow

How to see the feature work. Partial runs while working; the full test command is the CI run of
the pushed commit.

## 1. The fit conditions and shapes alone (seconds, no Docker)

From `src/engine`:

```sh
go test ./checks/ -run 'Refusal|RequestBodies|RequestShape'
go test ./checksvc/ -run 'RequestShape'
```

Expected: the evals' notes contract fits; one without a 400 body on `POST /notes`, one with
`allOf` in `NewNote`, and one with a `text/plain` body each fail, naming the location; the notes
contract gives one shape (`POST /notes`: object, `text` required, a string); an edited
`gen/request_shapes.go` fails RequestShapesGenerated.

## 2. Generation and the served stub (seconds, needs the pinned oapi-codegen)

`go test ./procedures/` with `AP_OAPI_CODEGEN` set: generating the notes contract gives
`gen/request_shapes.go` and a `gen/api.gen.go` whose registrations call `refuseMalformed`, the
same bytes in two runs. Served with `gen.Handler(gen.Stub{})`: `{"TEXT":"x"}` is answered 400
`{"message":"member \"/text\" is required"}`, `{"text":"x"}` 501 (the stub).

## 3. The clean reference (seconds)

The reference implementation (`src/tests/evals/steps/testdata/ref/impl`) in a generated project:
`{"TEXT":"x"}` 400, `{"text":"a","TEXT":"b"}` 201 with `"a"`, `{"text":"a","text":"b"}` 400,
`{"text":"x"}` 201; the held-out tests pass.

## 4. The acceptance suites (CI)

On push: `TestUS1`, `TestUS2`, `TestPlanted` and `TestPlantedValues` replay the renewed recordings
with no miss; `TestPlantedValues`' contract still fits.

## 5. The live evals (paid, owner's go-ahead)

Dispatch the evals workflow for the analyst and steps suites on the branch: every counted case
passes^3, `review/clean` among them, counted.
