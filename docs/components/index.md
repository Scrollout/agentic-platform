# Component

* [actionlint](actionlint.md) - Lints the GitHub Actions workflows a commit changes; the pre-commit hook keeps only findings on the changed lines.
* [actions/checkout](github-actions-checkout.md) - GitHub's actions/checkout action, pinned at v7.0.1 by commit hash in the CI workflows ([research D41](../../specs/001-prose-to-accepted-slice/research.md)); MIT, OpenSSF Scorecard 5.9 on 2026-11-09.
* [actions/setup-go](github-actions-setup-go.md) - GitHub's actions/setup-go action, pinned at v7.0.0 by commit hash in the CI workflows ([research D41](../../specs/001-prose-to-accepted-slice/research.md)); MIT, OpenSSF Scorecard 6.6 on 2026-11-09.
* [actions/setup-python](github-actions-setup-python.md) - GitHub's actions/setup-python action, pinned at v7.1.0 by commit hash in the CI workflows ([research D41](../../specs/001-prose-to-accepted-slice/research.md)); MIT, OpenSSF Scorecard 6.6 on 2026-11-09.
* [actions/upload-artifact](github-actions-upload-artifact.md) - GitHub's actions/upload-artifact action, pinned at v7.0.2 by commit hash; a failed live eval job uploads its report folder with it, kept 7 days ([research D46](../../specs/001-prose-to-accepted-slice/research.md)); MIT, OpenSSF Scorecard 5.5 on 2026-11-09.
* [cosign](cosign.md) - Sigstore's signing tool; signs every image the platform builds with a local key and verifies the signature before the launcher starts it.
* [cucumber/gherkin and godog](cucumber-gherkin.md) - The Gherkin parser the slice model uses to trace examples now, and the godog runner that will execute them as tests in Story 2.
* [Distribution registry](distribution-registry.md) - The CNCF Distribution image registry, run on 127.0.0.1 to hold the platform's built images, their SBOM and provenance attestations, and their signatures.
* [Docker Agent](docker-agent.md) - The agent runtime the platform is built on; here it runs the analyst as a long-lived serve api service.
* [git](git.md) - The version control CLI the project worker runs to keep each new project's version history, one commit per accepted step.
* [go-arch-lint](go-arch-lint.md) - Checks each platform Go module's imports against the components and allowed dependencies its .go-arch-lint.yml declares; an undeclared import or an unmapped package fails the arch step of scripts/check.sh, run in CI.
* [go-licenses](go-licenses.md) - Google's Go dependency licence checker, pinned at v2.0.1, run over every platform Go module by the deps step of scripts/check.sh with forbidden and unknown licence types disallowed.
* [go-yaml](go-yaml.md) - The YAML library (go.yaml.in/yaml/v3) the slice model and the engine use to read contracts, patterns and rules.
* [godog](godog.md) - The Cucumber test runner for Go, v0.16.0, which runs a slice's Gherkin examples with --strict so that an undefined step fails and every example is proven bound to a test.
* [golang.org/x/mod](x-mod.md) - The Go team's module library; the engine's DependenciesAllowed step check reads a project's go.mod with its modfile package.
* [golangci-lint](golangci-lint.md) - Runs Go linters (with gosec) on the Go packages a commit changes; the pre-commit hook keeps only findings on the changed lines.
* [gotestsum](gotestsum.md) - Runs go test and writes its results as JUnit XML for the CI job summaries ([research D43](../../specs/001-prose-to-accepted-slice/research.md)); Apache-2.0, OpenSSF Scorecard 3.6 on 2026-10-19.
* [govulncheck](govulncheck.md) - The Go team's vulnerability scanner, pinned at v1.9.0, run over every platform Go module by the deps step of scripts/check.sh in the deps CI job.
* [gVisor](gvisor.md) - The application-kernel sandbox (runtime runsc) that every step, check, analyst and contract-lint container runs under.
* [Hadolint](hadolint.md) - Lints the Dockerfiles a commit changes; the pre-commit hook keeps only findings on the changed lines, so older findings stay recorded in feature 002's T037.
* [kin-openapi](kin-openapi.md) - The Go OpenAPI 3 parser that oasdiff uses as a library, and which the engine imports for contract loading.
* [markdownlint-cli2](markdownlint-cli2.md) - The Markdown linter scripts/check.sh and the commit hook run on docs, specs and source Markdown.
* [mikepenz/action-junit-report](github-actions-junit-report.md) - Shows each CI test job's JUnit results in its job summary and as annotations, with a read-only token ([research D43](../../specs/001-prose-to-accepted-slice/research.md)); Apache-2.0, OpenSSF Scorecard 8.3 on 2026-11-16.
* [nginx](nginx.md) - The web server image the model gateway and the run page's forwarder are made of: official image plus config templates, no custom code.
* [oapi-codegen](oapi-codegen.md) - Generates Go server interfaces, types and clients from the platform's OpenAPI contracts, with its runtime library imported by the generated code.
* [oasdiff](oasdiff.md) - The OpenAPI breaking-change checker, run as a pinned container by scripts/contract-diff.sh for the platform's own contracts and as a Go library in the engine for each slice's contract.
* [PostgreSQL](postgresql.md) - The database that holds Temporal's state (persistence and visibility) for the engine's durable execution (feature 002); not used by the engine directly.
* [Redocly CLI](redocly-cli.md) - Bundles the multi-file engine contract into one OpenAPI document and dereferences schemas for code and agent output generation.
* [Ruff](ruff.md) - Lints the Python files a commit changes (rules E, F, W, B and S, E501 off; S101, S603, S607 off under scripts/tests); the pre-commit hook keeps only findings on the changed lines.
* [santhosh-tekuri/jsonschema](jsonschema.md) - The Go JSON Schema validator the slice model and the engine use to check parts, proposals and request bodies against the contract's schemas.
* [Schemathesis](schemathesis.md) - The property-based API tester that checks the running engine against its OpenAPI contract.
* [ShellCheck](shellcheck.md) - Lints the shell scripts a commit changes (`*.sh`, and files without an extension whose first line is a sh or bash shebang); the pre-commit hook keeps only findings on the changed lines.
* [Spectral](spectral.md) - The OpenAPI linter: run from the pinned Node image on the platform's own contracts, and as a small contract-lint service on each slice's contract.
* [Temporal](temporal.md) - The durable-execution engine that runs the engine's runs and operations as workflows (feature 002): the server in one process, its Go SDK in the engine, state in PostgreSQL.
* [WireMock](wiremock.md) - The HTTP stub server used as the recorded model in tests, answering the model gateway's paths only from recordings, and as the owner's recorder in front of the real gateway.
* [zizmor](zizmor.md) - Audits the GitHub Actions workflows and actions a commit changes for security weaknesses, offline; the pre-commit hook keeps only findings on the changed lines.

# Convention

* [Component documentation conventions](conventions.md) - How services, libraries, and modules are documented in components/.
