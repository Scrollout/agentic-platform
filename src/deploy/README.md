# src/deploy

Packaging: the ap launcher, Compose file, Dockerfiles, the nginx gateway template and the image lock.

## Images

`build.sh <name>` builds `ap-<name>:dev` with `src/` as the context. It reads each base image from
`images.lock` and passes it as a build arg (`GOLANG_IMAGE`, `DOCKER_AGENT_IMAGE`), so no digest is
copied into a Dockerfile. Run `scripts/generate.sh` first: generated code in `api/gen/` is not committed.
The ignore rule for the Go stack is `src/.dockerignore`; it leaves out `*_test.go`, `testdata/`,
`evals/`, `src/tests/` and `src/deploy/`. Every image runs as UID 65532, not root.

- `Dockerfile.agent`: the agent step image, with Docker Agent copied from its pinned image, the Go toolchain, and `TELEMETRY_ENABLED=false`.
- `Dockerfile.check`: the check container, with the Go toolchain only, to run checks on generated code.
- `Dockerfile.engine`: the workflow engine, a static Go binary on `scratch`; expects the main package `src/engine/cmd/engine`.
- `Dockerfile.cli`: the terminal client, a static Go binary on `scratch`; expects `src/cli/cmd/cli`.
- `Dockerfile.page`: the read-only run page, a static Go binary on `scratch`; expects `src/page/cmd/page`.
