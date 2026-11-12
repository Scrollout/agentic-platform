# src/slice

Shared Go package: the slice model, its schemas, traceability checks and EARS derivation.

It holds no copy of the schemas. `LoadSchemas(dir)` compiles them from a contract version folder,
`LoadSchemasFS(fsys)` from a file system such as the engine's embedded contract.

Values with a rule of their own in `slice-defs.schema.json` are types that hold only valid values:
`ID` (`$defs/slice_id`), `RunID` (`$defs/run_id`) and `Hash` (`$defs/hash`). They are made only by
`Schemas.ParseID`, `ParseRunID` and `ParseHash`, which apply the compiled definition itself, so the
patterns live in the contract alone.

`EARS(rule)` returns a rule's one EARS sentence, and `RuleText(doc)` the sentence of every rule in a
slice document by rule ID, which the slice stores as `rule_text`. A sentence is always derived from
the rule's fields, never read from input: `Rule` has no text field and `RuleText` ignores any
existing `rule_text`. Both fail with an error naming the rule when its pattern has no EARS template
or a field the sentence needs is empty. Which fields a pattern requires is the schema's rule; parse
the slice against it first.

`Trace(doc)` takes the JSON of a slice or slice proposal and returns every broken link between its
IDs, each as a `Finding` naming the part to fix (`goals`, `rules` or `contract`), the offending ID
and a reason: a goal no rule serves, a rule naming a goal or term that does not exist, a rule with
no scenario tagged `@R-<n>` under its `Rule: R-<n>` block, and an operation whose `x-rules` names a
rule that does not exist. It returns nil when every link holds. Shape is not its concern: the
schemas decide that, so run `CheckSlice` or `CheckProposal` first. Examples that are not valid
Gherkin are a `rules` finding, a contract that cannot be read a `contract` finding, and a document
that is not JSON one finding with an empty part.
