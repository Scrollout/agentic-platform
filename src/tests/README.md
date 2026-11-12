# src/tests

Contract, acceptance, planted-failure and eval suites; excluded from every built image.

Tests that need the model run against the recorded model in [`recorded/`](recorded/README.md)
(research D19, D26): a replay server on an internal network stands in for the gateway as
`gateway:8080` and answers only from the recordings there. A request without a recording fails
with 404 and is never forwarded, so these tests need no key and cost nothing. Only the project
owner records from the provider, by hand, with `recorded/record.sh`.
