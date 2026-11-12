# src/slice

Shared Go package: the slice model, its schemas, traceability checks and EARS derivation.

It holds no copy of the schemas. `LoadSchemas(dir)` compiles them from a contract version folder,
`LoadSchemasFS(fsys)` from a file system such as the engine's embedded contract.

Values with a rule of their own in `slice-defs.schema.json` are types that hold only valid values:
`ID` (`$defs/slice_id`), `RunID` (`$defs/run_id`) and `Hash` (`$defs/hash`). They are made only by
`Schemas.ParseID`, `ParseRunID` and `ParseHash`, which apply the compiled definition itself, so the
patterns live in the contract alone.
