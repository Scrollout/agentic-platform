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
no scenario tagged `@R-<n>` under its `Rule: R-<n>` block, an example whose `Rule:` block or
`@R-<n>` tag names a rule that is not in `rules` (a `rules` finding naming the example's `E-<n>`
and the rule), an operation with no `operationId` (named by its method and path) or with a missing
or empty `x-rules`, and an operation whose `x-rules` names a rule that does not exist. It returns
nil when every link holds. Shape is mostly the schemas' concern, so run `CheckSlice` or
`CheckProposal` first, but Trace fails closed on a contract shape it cannot follow: a root,
`paths`, path item or operation that is not a mapping, an `x-rules` that is not a list or holds a
non-scalar entry, and any YAML alias or merge key are each a `contract` finding. Every finding on a
part has a non-empty ID. Examples that are not valid Gherkin are a `rules` finding with ID
`examples`, a contract that YAML cannot parse a `contract` finding with ID `contract`; both have
`Syntax` set, since the links inside were not followed. A document that is not JSON gives one
finding with an empty part.

`Schemas.MaxAnswerText()` and `MaxQuestions()` return the free answer's `maxLength`
(`slice-defs.schema.json` `$defs/answer`) and the slice's `questions` `maxItems`
(`slice.schema.json`), read from the compiled schemas, or 0 when a schema sets none.

`ExampleTags(text)` parses Gherkin examples with the same parser and returns a `rules` finding for
each scenario without exactly one `@R-<n>` tag matching its `Rule: R-<n>` block, or without exactly
one `@E-<n>` tag no other scenario carries; each reason names the scenario. An error means the text
is not Gherkin. The engine runs it as the `ExampleTagged` check. `ExampleScenarios(text)` maps
each `E-<n>` ID to a digest of its scenario (rule, name, steps; not tags or layout), which the
engine keeps to refuse an ID a revision dropped coming back on another scenario. `ExampleRules(text)` gives each example ID of Gherkin examples with the rule it sits under: each
`E-<n>` a scenario under a `Rule: R-<n>` block carries as an `@E-<n>` tag, in document order, each
once (an ID carried twice keeps its first rule); unlike `ExampleScenarios`, scenarios outside such
a block are not listed; empty text gives none; an error means the text is not Gherkin. The engine
uses it to refuse a revision that drops an example or moves it under another rule.
