# AGENTS.md

## Project

`agentic-platform` is a new project with no code yet. All project knowledge is an
[Open Knowledge Format (OKF) v0.2](https://github.com/GoogleCloudPlatform/open-knowledge-format/blob/ad30107c31c06aec8a7d5636e0d1058118604e6f/SPEC.md)
bundle in `docs/`.

- Start at `docs/index.md`, then read `docs/overview/platform.md`.
- To add or change knowledge, follow `docs/playbooks/maintain-knowledge-bundle.md`.
- Each section's `conventions.md` gives the concept `type`, the required fields and a skeleton.
- Feature work uses [GitHub Spec Kit](https://github.com/github/spec-kit): run `/speckit-constitution`, then
  `/speckit-specify`, `/speckit-plan`, `/speckit-tasks` and `/speckit-implement`. Its state is in `.specify/`.

## Rules

- Every `.md` file in `docs/` other than `index.md` and `log.md` is a concept. Its YAML frontmatter needs `type`, and should also have `title`, `description`, `tags` and `status`.
- Set `generated: { by: claude-code/<model>, at: <ISO 8601 UTC> }` on every meaningful edit. Never write a `human:` entry in `verified`; only a person confirms content.
- Record provenance in `sources`. Attribute claims with footnotes `[^id]` that match a `sources[].id`.
- New content is `status: draft`. Deprecate a concept instead of deleting it.
- Link concepts with bundle-relative paths, such as `/components/x.md`.
- Never hand-edit `index.md`. Run `python3 scripts/okf.py index` instead. The only exception is a section description in `docs/index.md`.
- Log every change in `docs/log.md` under `## YYYY-MM-DD`, newest first.
- Before finishing, `python3 scripts/okf.py check` must report `OK`. It needs PyYAML.
