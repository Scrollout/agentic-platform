# Component

* [cosign](cosign.md) - Sigstore's signing tool; signs every image the platform builds with a local key and verifies the signature before the launcher starts it.
* [Distribution registry](distribution-registry.md) - The CNCF Distribution image registry, run on 127.0.0.1 to hold the platform's built images, their SBOM and provenance attestations, and their signatures.
* [WireMock](wiremock.md) - The HTTP stub server used as the recorded model in tests, answering the model gateway's paths only from recordings, and as the owner's recorder in front of the real gateway.

# Convention

* [Component documentation conventions](conventions.md) - How services, libraries, and modules are documented in components/.
