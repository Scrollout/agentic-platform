---
type: Reference
title: Project template tools
description: What gonew, Copier, Cookiecutter and Go's text/template do for making new projects from a template, and that none derives a template from an existing project, as read on 2026-11-25.
resource: https://copier.readthedocs.io/en/stable/
tags: [references, templates, go]
status: draft
stale_after: 2027-05-25T00:00:00Z
sources:
  - id: gonew
    resource: https://pkg.go.dev/golang.org/x/tools/cmd/gonew
    title: gonew command documentation (x/tools v0.53.0)
  - id: copier
    resource: https://copier.readthedocs.io/en/stable/
    title: Copier documentation
  - id: cookiecutter
    resource: https://cookiecutter.readthedocs.io/en/stable/
    title: Cookiecutter documentation
  - id: texttemplate
    resource: https://pkg.go.dev/text/template
    title: Go text/template package
  - id: research
    resource: ../../specs/011-capture-pattern/research.md
    title: Feature 011 research, S11 to S14 and R4
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-25T23:00:00Z }
---

# Facts

- **gonew** "starts a new Go module by copying a template module", changing only its module
  path. Its page calls it "highly experimental and subject to change". BSD-3-Clause.
- **Copier** renders hand-written templates: Jinja placeholders in folder names, file names and
  contents, questions in `copier.yml`, the answers kept in an answers file. Python 3.10+.
- **Cookiecutter** "creates projects from cookiecutters (project templates)" with Jinja2, in
  Python.
- **text/template** is Go's standard-library template engine with `{{...}}` actions; it assumes
  trusted template authors and does not escape.
- None of the four pages says it can derive a template from an existing project, or replace a
  name in its case forms; each needs the placeholders written by hand.

Sources: [gonew](https://pkg.go.dev/golang.org/x/tools/cmd/gonew), [Copier](https://copier.readthedocs.io/en/stable/), [Cookiecutter](https://cookiecutter.readthedocs.io/en/stable/), [text/template](https://pkg.go.dev/text/template).

# Use in this project

Feature 011 derives a captured pattern's template from an accepted project by named values, which
none of these does ([ADR 0092](/decisions/0092-captured-pattern-fit-is-exact-substitution.md)).

Sources: [Feature 011 research, R4](../../specs/011-capture-pattern/research.md).
