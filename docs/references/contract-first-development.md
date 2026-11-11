---
type: Reference
title: Contract-first API development
description: Evidence that defining the API contract first, then generating SDKs and server interfaces, then writing tests and code is established practice, with its caveats and the tooling choices it needs.
tags: [references, apis, contracts, openapi, sdk]
status: draft
stale_after: 2027-05-11T00:00:00Z
sources:
  - id: oai-bp
    resource: https://learn.openapis.org/best-practices
    title: OpenAPI Initiative - Best Practices
  - id: aws-models
    resource: https://aws.amazon.com/blogs/aws/introducing-aws-api-models-and-publicly-available-resources-for-aws-api-definitions/
    title: Introducing AWS API models and publicly available resources for AWS API definitions (AWS News Blog, 2025-06-05)
  - id: smithy-ts
    resource: https://aws.amazon.com/blogs/devops/smithy-server-and-client-generator-for-typescript/
    title: Smithy Server and Client Generator for TypeScript (Developer Preview) (AWS DevOps Blog)
  - id: smithy-ssdk
    resource: https://smithy.io/2.0/languages/typescript/ts-ssdk/introduction.html
    title: Introduction to the Smithy Server Generator for TypeScript
  - id: smithy-openapi
    resource: https://smithy.io/2.0/guides/model-translations/converting-to-openapi.html
    title: Converting Smithy to OpenAPI - Smithy 2.0
  - id: aws-rel03
    resource: https://docs.aws.amazon.com/wellarchitected/latest/framework/rel_service_architecture_api_contracts.html
    title: REL03-BP03 Provide service contracts per API (AWS Well-Architected, Reliability Pillar)
  - id: aws-qa-nt7
    resource: https://docs.aws.amazon.com/wellarchitected/latest/devops-guidance/qa.nt.7-verify-service-integrations-through-contract-testing.html
    title: "[QA.NT.7] Verify service integrations through contract testing (AWS DevOps Guidance)"
  - id: aws-apigw-sdk
    resource: https://docs.aws.amazon.com/apigateway/latest/developerguide/how-to-generate-sdk.html
    title: Generate SDKs for REST APIs in API Gateway
  - id: aws-eb-schemas
    resource: https://docs.aws.amazon.com/eventbridge/latest/userguide/eb-schema.html
    title: Amazon EventBridge schemas
  - id: typespec
    resource: https://typespec.io/blog/typespec-1-0-GA-release/
    title: TypeSpec 1.0 GA release
  - id: aip-4210
    resource: https://google.aip.dev/client-libraries/4210
    title: AIP-4210 Client library generators
  - id: aip-181
    resource: https://google.aip.dev/181
    title: "AIP-181: Stability levels"
  - id: ms-playbook
    resource: https://microsoft.github.io/code-with-engineering-playbook/design/design-patterns/rest-api-design-guidance/
    title: Microsoft Engineering Playbook - REST API Design Guidance
  - id: fowler-cdc
    resource: https://martinfowler.com/articles/consumerDrivenContracts.html
    title: "Consumer-Driven Contracts: A Service Evolution Pattern (martinfowler.com)"
  - id: tw-sdd
    resource: https://www.thoughtworks.com/radar/techniques/spec-driven-development
    title: Thoughtworks Technology Radar - Spec-driven development
  - id: oas-releases
    resource: https://github.com/OAI/OpenAPI-Specification/releases
    title: OAI/OpenAPI-Specification releases
  - id: oapi-codegen
    resource: https://github.com/oapi-codegen/oapi-codegen
    title: oapi-codegen
  - id: openapi-ts
    resource: https://github.com/openapi-ts/openapi-typescript
    title: openapi-typescript and openapi-fetch
  - id: schemathesis
    resource: https://github.com/schemathesis/schemathesis
    title: Schemathesis
  - id: oasdiff
    resource: https://github.com/oasdiff/oasdiff
    title: oasdiff
  - id: spectral
    resource: https://github.com/stoplightio/spectral
    title: Spectral
  - id: cloudevents
    resource: https://cloudevents.io/
    title: CloudEvents
  - id: asyncapi
    resource: https://github.com/asyncapi/spec/releases
    title: AsyncAPI specification releases
  - id: scalar-stainless
    resource: https://scalar.com/resources/stainless-wind-down
    title: The Stainless SDK generator wind-down (Scalar)
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T20:40:00Z }
---

# Summary

The proposed order for platform development is:

1. Write the contract.
2. Generate SDKs and server interfaces from it.
3. Write tests.
4. Write the code.

This is established practice. AWS, Google and Microsoft build their own APIs this way, and
AWS and the OpenAPI Initiative recommend it. Two adjustments apply:

- Generate clients and server interfaces, not the implementation.
- Test against the contract directly, as well as through the SDKs.

The rule is the "Contract-first" Engineering Principle in the
[constitution](../../.specify/memory/constitution.md). The tooling choices below are proposed in
[ADR 0004](/decisions/0004-contract-first-toolchain.md).

# Evidence

- **AWS builds every service model-first.** "All AWS services are modeled in Smithy to thoroughly document the API contract", and "since 2018, we have been generating SDK clients and CLI tools using Smithy models."
  - Smithy states that "Smithy services are always written model-first."
  - AWS says Smithy "forces you to define your interface first rather than let your API to become implicitly defined by your implementation choices."
- **AWS tells customers to do the same.**
  - Well-Architected REL03-BP03 says: "First, define machine-readable service contracts for APIs, and then implement an API versioning strategy."
  - It names two anti-patterns: APIs "without strongly typed schemas", and "not using API contracts to develop test cases and mock API implementations".
  - It covers REST, GraphQL "or asynchronous events".
- **AWS recommends contract testing in the pipeline.** DevOps Guidance QA.NT.7 rates it RECOMMENDED: "consumer-driven contract testing is the type we generally recommend", and "embed contract testing into your deployment pipeline."
- **AWS treats events as contracts.** EventBridge keeps event schemas in OpenAPI 3 or JSON Schema draft-04 and generates code bindings from them.
- **The OpenAPI Initiative:** "the importance of using Design-first cannot be stressed strongly enough."
  - Descriptions are first-class source files, checked in CI, kept as "a single source of truth".
- **Google:** APIs are defined in protobuf first, and GAPIC generators produce each language's client library.
  - AIP-181 lets an API iterate in alpha, where breaking changes "are permitted and expected", before it stabilizes.
- **Microsoft and Azure:** TypeSpec generates OpenAPI, client libraries and server scaffolding from one source. The Azure SDK team has long treated API-first as best practice.
- **Contracts evolve:** messages are part of a provider's contract, and contracts change as consumers' needs change.
- **Agent-written code:** spec-driven development, with the spec guiding AI coding agents, is on the Thoughtworks Radar at "Assess".

Sources: [Introducing AWS API models and publicly available resources for AWS API definitions (AWS News Blog, 2025-06-05)](https://aws.amazon.com/blogs/aws/introducing-aws-api-models-and-publicly-available-resources-for-aws-api-definitions/); [Introduction to the Smithy Server Generator for TypeScript](https://smithy.io/2.0/languages/typescript/ts-ssdk/introduction.html); [Smithy Server and Client Generator for TypeScript (Developer Preview) (AWS DevOps Blog)](https://aws.amazon.com/blogs/devops/smithy-server-and-client-generator-for-typescript/); [REL03-BP03 Provide service contracts per API (AWS Well-Architected, Reliability Pillar)](https://docs.aws.amazon.com/wellarchitected/latest/framework/rel_service_architecture_api_contracts.html); [[QA.NT.7] Verify service integrations through contract testing (AWS DevOps Guidance)](https://docs.aws.amazon.com/wellarchitected/latest/devops-guidance/qa.nt.7-verify-service-integrations-through-contract-testing.html); [Amazon EventBridge schemas](https://docs.aws.amazon.com/eventbridge/latest/userguide/eb-schema.html); [OpenAPI Initiative - Best Practices](https://learn.openapis.org/best-practices); [AIP-4210 Client library generators](https://google.aip.dev/client-libraries/4210); [AIP-181: Stability levels](https://google.aip.dev/181); [TypeSpec 1.0 GA release](https://typespec.io/blog/typespec-1-0-GA-release/); [Consumer-Driven Contracts: A Service Evolution Pattern (martinfowler.com)](https://martinfowler.com/articles/consumerDrivenContracts.html); [Thoughtworks Technology Radar - Spec-driven development](https://www.thoughtworks.com/radar/techniques/spec-driven-development).

# Caveats

- **Generated implementations:**
  - Microsoft warns that a generator "might not be able to generate a flexible & maintainable implementation".
  - AWS's own server generators produce validation and stubs, and the logic is written by hand.
- **Early churn:** design-first "may not be a good fit" while requirements change often.
  - The mitigation is a contract that starts as a draft for its first consumer and grows additively, with an alpha-to-stable path like AIP-181's.
- **Testing only through the SDK:** server bugs can hide behind SDK bugs.
  - Add schema-conformance tests against the running server, for example with Schemathesis.
  - Add consumer-driven contract tests where consumers are separate teams.
- **Generator lock-in:**
  - The hosted Stainless SDK generator stopped taking new projects in May 2026, when Stainless joined Anthropic. That account comes from Scalar, a competitor.
  - AWS API Gateway generates SDKs only from an API that is already deployed, so it cannot be the "generate first" step.
- **Lossy translations:** converting Smithy to OpenAPI "is a lossy conversion".

Sources: [Microsoft Engineering Playbook - REST API Design Guidance](https://microsoft.github.io/code-with-engineering-playbook/design/design-patterns/rest-api-design-guidance/); [Introduction to the Smithy Server Generator for TypeScript](https://smithy.io/2.0/languages/typescript/ts-ssdk/introduction.html); [AIP-181: Stability levels](https://google.aip.dev/181); [Schemathesis](https://github.com/schemathesis/schemathesis); [[QA.NT.7] Verify service integrations through contract testing (AWS DevOps Guidance)](https://docs.aws.amazon.com/wellarchitected/latest/devops-guidance/qa.nt.7-verify-service-integrations-through-contract-testing.html); [The Stainless SDK generator wind-down (Scalar)](https://scalar.com/resources/stainless-wind-down); [Generate SDKs for REST APIs in API Gateway](https://docs.aws.amazon.com/apigateway/latest/developerguide/how-to-generate-sdk.html); [Converting Smithy to OpenAPI - Smithy 2.0](https://smithy.io/2.0/guides/model-translations/converting-to-openapi.html).

# Resolved choices

These are researched recommendations. [ADR 0004](/decisions/0004-contract-first-toolchain.md) proposes them for the owner's decision.

- **OpenAPI 3.1.x (3.1.2).**
  - 3.2 was released in 2025, and 3.2.1 in 2026. Validators already handle 3.2, but generators support it only partially or in preview.
  - 3.1 uses JSON Schema 2020-12, the same dialect AHP publishes, so payload schemas can be shared.
- **Authoring:** write OpenAPI 3.1 YAML directly, split into files and bundled with Redocly CLI.
  - TypeSpec defaults to emitting OpenAPI 3.0.0, and its server emitters are alpha.
  - Smithy's OpenAPI output is lossy and needs a Java toolchain.
  - Revisit TypeSpec if several APIs or versioned variants appear.
- **First SDK languages: TypeScript and Go.**
  - TypeScript covers the web console and the VS Code extension. Go covers the CLI, the server stubs and the Docker Agent ecosystem.
  - Python is deferred until automation or notebook users need it.
  - Kotlin and Swift are deferred, because interactive mobile clients use AHP, which has official Kotlin and Swift SDKs.
- **Generators:**
  - **Go server interfaces and client:** oapi-codegen, Apache-2.0. Its strict-server mode generates typed interfaces, and v2.8.0 added initial 3.1 support.
  - **TypeScript client:** openapi-typescript with openapi-fetch, MIT. It supports 3.0 and 3.1 and generates "runtime-free" types.
    - Releases have slowed. The fallback is Hey API, which says it is still "in initial development".
  - **Later stacks:** openapi-python-client for Python, apple/swift-openapi-generator for Swift, and OpenAPI Generator `kotlin` (multiplatform) for Kotlin.
    - The Kotlin generator does not support `oneOf` or `anyOf`, so schemas meant for mobile should avoid them.
- **Events:**
  - Payloads are JSON Schema 2020-12, shared with OpenAPI by `$ref`.
  - Asynchronous domain events and webhooks use the CloudEvents 1.0.2 envelope, a CNCF graduated project.
  - AsyncAPI 3.1 describes non-AHP event channels where one is needed.
    - Its Schema Object is based on JSON Schema draft-07, so 2020-12 payloads need a custom `schemaFormat`, and tool support for that is unverified.
  - AHP's own published schema is its contract and is not re-modelled.
- **Contract tooling:**
  - **Lint:** Spectral, Apache-2.0, supports 3.2.
  - **Bundle and lint:** Redocly CLI, MIT.
  - **Breaking changes:** oasdiff, Apache-2.0, with `breaking` and `changelog`.
  - **Conformance and fuzz testing:** Schemathesis, MIT.
  - **Mocks:** Prism, Apache-2.0.
  - **Drift:** CI regenerates the code and fails on `git diff --exit-code`. oapi-codegen's strict interfaces add a compile-time check.
  - Dredd is archived and must not be used.

Sources: [OAI/OpenAPI-Specification releases](https://github.com/OAI/OpenAPI-Specification/releases); [Converting Smithy to OpenAPI - Smithy 2.0](https://smithy.io/2.0/guides/model-translations/converting-to-openapi.html); [oapi-codegen](https://github.com/oapi-codegen/oapi-codegen); [openapi-typescript and openapi-fetch](https://github.com/openapi-ts/openapi-typescript); [CloudEvents](https://cloudevents.io/); [AsyncAPI specification releases](https://github.com/asyncapi/spec/releases); [Spectral](https://github.com/stoplightio/spectral); [oasdiff](https://github.com/oasdiff/oasdiff); [Schemathesis](https://github.com/schemathesis/schemathesis).

# Unverified

- ogen's OpenAPI 3.1 coverage.
- vacuum's compatibility with Spectral rulesets.
- How complete Kiota's 3.1 support is.
- How well AsyncAPI tools handle JSON Schema 2020-12 payloads.
