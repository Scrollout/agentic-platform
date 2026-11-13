# Component

* [cosign](cosign.md) - Sigstore's signing tool; signs every image the platform builds with a local key and verifies the signature before the launcher starts it.
* [cucumber/gherkin and godog](cucumber-gherkin.md) - The Gherkin parser the slice model uses to trace examples now, and the godog runner that will execute them as tests in Story 2.
* [Distribution registry](distribution-registry.md) - The CNCF Distribution image registry, run on 127.0.0.1 to hold the platform's built images, their SBOM and provenance attestations, and their signatures.
* [Docker Agent](docker-agent.md) - The agent runtime the platform is built on; here it runs the analyst as a long-lived serve api service.
* [go-yaml](go-yaml.md) - The YAML library (go.yaml.in/yaml/v3) the slice model and the engine use to read contracts, patterns and rules.
* [golang.org/x/mod](x-mod.md) - The Go team's module library; the engine's DependenciesAllowed step check reads a project's go.mod with its modfile package.
* [gVisor](gvisor.md) - The application-kernel sandbox (runtime runsc) that every step, check, analyst and contract-lint container runs under.
* [kin-openapi](kin-openapi.md) - The Go OpenAPI 3 parser that oasdiff uses as a library, and which the engine imports for contract loading.
* [markdownlint-cli2](markdownlint-cli2.md) - The Markdown linter scripts/check.sh and the commit hook run on docs, specs and source Markdown.
* [nginx](nginx.md) - The web server image the model gateway and the run page's forwarder are made of: official image plus config templates, no custom code.
* [oapi-codegen](oapi-codegen.md) - Generates Go server interfaces, types and clients from the platform's OpenAPI contracts, with its runtime library imported by the generated code.
* [oasdiff](oasdiff.md) - The OpenAPI breaking-change checker, run as a pinned container by scripts/contract-diff.sh for the platform's own contracts and as a Go library in the engine for each slice's contract.
* [Redocly CLI](redocly-cli.md) - Bundles the multi-file engine contract into one OpenAPI document and dereferences schemas for code and agent output generation.
* [santhosh-tekuri/jsonschema](jsonschema.md) - The Go JSON Schema validator the slice model and the engine use to check parts, proposals and request bodies against the contract's schemas.
* [Schemathesis](schemathesis.md) - The property-based API tester that checks the running engine against its OpenAPI contract.
* [Spectral](spectral.md) - The OpenAPI linter: run from the pinned Node image on the platform's own contracts, and as a small contract-lint service on each slice's contract.
* [WireMock](wiremock.md) - The HTTP stub server used as the recorded model in tests, answering the model gateway's paths only from recordings, and as the owner's recorder in front of the real gateway.

# Convention

* [Component documentation conventions](conventions.md) - How services, libraries, and modules are documented in components/.
