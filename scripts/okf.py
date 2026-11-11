#!/usr/bin/env python3
"""Tooling for the OKF v0.2 bundle in docs/.

  python3 scripts/okf.py index [BUNDLE]   regenerate every index.md
  python3 scripts/okf.py check [BUNDLE]   validate the bundle (exit 1 on failure)

`index` mirrors the reference implementation
(GoogleCloudPlatform/open-knowledge-format, src/reference_agent/bundle/index.py):
- one `# <type>` section per concept type, entries sorted by title;
- `# Subdirectories` for child directories, linked as `<dir>/index.md`;
- each entry is `* [Title](relative-link) - description`, with the description
  taken from the concept's frontmatter;
- a directory's description is the one already written in its parent index. If
  there is none, it falls back to the directory's single entry's description.
  (The reference agent synthesizes one with an LLM instead.)
Unlike the reference agent, it preserves `okf_version` on the root index and
never lists the reserved `log.md`.

`check` enforces SPEC.md §11 conformance as errors:
- every non-reserved .md file has a YAML frontmatter mapping with a non-empty `type`;
- index.md carries no frontmatter except `okf_version` at the root;
- log.md date headings are `YYYY-MM-DD`, newest first.

It also reports SHOULD-level guidance from §5 to §9 as errors:
- timestamps are ISO 8601 with a UTC offset;
- actors follow §7;
- `status` is `draft`, `stable` or `deprecated`;
- every `sources` entry has a `resource`;
- footnotes match a `sources[].id`;
- indexes are up to date.

Broken links are only warnings, because §6.1 says they may be not-yet-written knowledge.
"""
from __future__ import annotations

import re
import sys
from collections import defaultdict
from datetime import datetime
from pathlib import Path

import yaml

ROOT_DEFAULT = Path(__file__).resolve().parent.parent / "docs"
INDEX, LOG = "index.md", "log.md"
RESERVED = {INDEX, LOG}
STATUSES = {"draft", "stable", "deprecated"}
ACTOR_RE = re.compile(r"^(human|process):\S+$|^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$")
SEGMENT_RE = re.compile(r"[A-Za-z0-9_][A-Za-z0-9_.\-]*")  # concept id segment, as in paths.py
CODE_RE = re.compile(r"```.*?```|`[^`\n]*`", re.S)
LINK_RE = re.compile(r"\[[^\]]*\]\(([^)\s]+)\)")
ENTRY_RE = re.compile(r"^\* \[[^\]]+\]\(([^)]+)\)(?: - (.*))?$", re.M)


class _Loader(yaml.SafeLoader):
    """Keep timestamps as written (same approach as the reference document.py)."""


_Loader.yaml_implicit_resolvers = {
    ch: [(t, r) for t, r in rs if t != "tag:yaml.org,2002:timestamp"]
    for ch, rs in yaml.SafeLoader.yaml_implicit_resolvers.items()
}


def split(text: str):
    """Return (frontmatter or None, body). Raises ValueError on bad frontmatter."""
    lines = text.splitlines()
    if not lines or lines[0].strip() != "---":
        return None, text
    for i in range(1, len(lines)):
        if lines[i].strip() == "---":
            fm = yaml.load("\n".join(lines[1:i]), Loader=_Loader) or {}
            if not isinstance(fm, dict):
                raise ValueError("frontmatter must be a YAML mapping")
            return fm, "\n".join(lines[i + 1:])
    raise ValueError("unterminated frontmatter block")


def concepts(root: Path):
    for p in sorted(root.rglob("*.md")):
        if p.name not in RESERVED:
            yield p


def is_ts(v) -> bool:
    s = str(v or "")
    if "T" not in s:
        return False
    try:
        return datetime.fromisoformat(s.replace("Z", "+00:00")).tzinfo is not None
    except ValueError:
        return False


# ----------------------------------------------------------------- index


def build_index(directory: Path, root: Path, dir_desc: dict[Path, str]) -> str:
    grouped: dict[str, list[tuple[str, str, str]]] = defaultdict(list)
    for child in sorted(directory.iterdir()):
        if child.name.startswith(".") or child.name in RESERVED:
            continue
        if child.is_file() and child.suffix == ".md":
            fm, _ = split(child.read_text(encoding="utf-8"))
            fm = fm or {}
            grouped[str(fm.get("type") or "Other")].append(
                (str(fm.get("title") or child.stem), child.name, str(fm.get("description") or "")))
        elif child.is_dir() and any(child.rglob("*.md")):
            grouped["Subdirectories"].append((child.name, f"{child.name}/{INDEX}", dir_desc.get(child, "")))
    sections = []
    # Concept types first, then subdirectories, so a reader meets content before navigation.
    for typ in sorted(grouped, key=lambda t: (t == "Subdirectories", t.lower())):
        rows = [f"* [{t}]({l})" + (f" - {d}" if d else "")
                for t, l, d in sorted(grouped[typ], key=lambda e: e[0].lower())]
        sections.append("\n".join([f"# {typ}", "", *rows]))
    head = ""
    if directory == root:
        old = directory / INDEX
        fm = split(old.read_text(encoding="utf-8"))[0] if old.exists() else None
        version = (fm or {}).get("okf_version", "0.2")
        head = f'---\nokf_version: "{version}"\n---\n\n'
    return head + "\n\n".join(sections) + "\n"


def existing_desc(parent_index: Path, child: Path) -> str:
    if not parent_index.exists():
        return ""
    for link, desc in ENTRY_RE.findall(parent_index.read_text(encoding="utf-8")):
        if (parent_index.parent / link).resolve() == (child / INDEX).resolve():
            return desc or ""
    return ""


def render_indexes(root: Path) -> dict[Path, str]:
    # Every directory holding a .md file, plus its ancestors up to the root; deepest first so
    # each child's description is known before its parent's index is built.
    leaves = {p.parent for p in root.rglob("*.md")} | {root}
    dirs = sorted({a for d in leaves for a in [d, *d.parents] if a == root or root in a.parents},
                  key=lambda d: (-len(d.relative_to(root).parts), str(d)))
    out, dir_desc = {}, {}
    for d in dirs:
        out[d / INDEX] = build_index(d, root, dir_desc)
        if d == root:
            continue
        entries = ENTRY_RE.findall(out[d / INDEX])
        single = entries[0][1] if len(entries) == 1 and entries[0][1] else ""
        dir_desc[d] = existing_desc(d.parent / INDEX, d) or single
    return out


def cmd_index(root: Path) -> int:
    for path, text in render_indexes(root).items():
        if not path.exists() or path.read_text(encoding="utf-8") != text:
            path.write_text(text, encoding="utf-8")
            print(f"wrote {path.relative_to(root.parent)}")
    return 0


# ----------------------------------------------------------------- check


def cmd_check(root: Path) -> int:
    errors, warnings, n = [], [], 0

    def e(p, m): errors.append(f"ERROR {p.relative_to(root.parent)}: {m}")
    def w(p, m): warnings.append(f"WARN  {p.relative_to(root.parent)}: {m}")

    for p in sorted(root.rglob("*.md")):
        text = p.read_text(encoding="utf-8")
        try:
            fm, body = split(text)
        except (ValueError, yaml.YAMLError) as exc:
            e(p, f"invalid frontmatter: {str(exc).splitlines()[0]}")
            continue
        prose = CODE_RE.sub("", text)  # links/footnotes inside code are examples, not references
        for target in LINK_RE.findall(prose):
            if re.match(r"^[a-z][a-z0-9+.-]*:|^#", target, re.I):
                continue
            t = target.split("#", 1)[0]
            dest = root / t.lstrip("/") if t.startswith("/") else p.parent / t
            if not dest.exists():
                w(p, f"link to not-yet-written concept: {target}")
        if p.name == INDEX:
            if fm is not None and (p.parent != root or set(fm) - {"okf_version"}):
                e(p, "index.md may carry frontmatter only at the bundle root, and only `okf_version`")
            continue
        if p.name == LOG:
            if fm is not None:
                e(p, "log.md must not carry frontmatter")
            dates = re.findall(r"^## (.+)$", body, re.M)
            bad = [d for d in dates if not re.fullmatch(r"\d{4}-\d{2}-\d{2}", d.strip())]
            if bad:
                e(p, f"date headings must be YYYY-MM-DD: {bad}")
            if dates != sorted(dates, reverse=True):
                e(p, "date headings must be newest first")
            continue
        n += 1
        rel = p.relative_to(root).with_suffix("")
        if not all(SEGMENT_RE.fullmatch(s) for s in rel.parts):
            e(p, "concept id segments must match [A-Za-z0-9_][A-Za-z0-9_.-]*")
        if fm is None:
            e(p, "missing YAML frontmatter")
            continue
        if not str(fm.get("type") or "").strip():
            e(p, "missing non-empty `type`")
        for k in ("title", "description"):
            if not fm.get(k):
                w(p, f"recommended `{k}` is missing")
        if "status" in fm and fm["status"] not in STATUSES:
            e(p, f"`status` must be one of {sorted(STATUSES)}")
        if "stale_after" in fm and not is_ts(fm["stale_after"]):
            e(p, "`stale_after` must be an ISO 8601 datetime with UTC offset")
        events = [("generated", fm["generated"])] if "generated" in fm else []
        v = fm.get("verified")
        events += [("verified", x) for x in (v if isinstance(v, list) else [v] if v else [])]
        for field, ev in events:
            if not isinstance(ev, dict) or not ACTOR_RE.match(str(ev.get("by") or "")):
                e(p, f"`{field}.by` must be an actor: <producer>/<version>, human:<id>, process:<id>")
            elif "at" in ev and not is_ts(ev["at"]):
                e(p, f"`{field}.at` must be an ISO 8601 datetime with UTC offset")
        ids = set()
        for s in fm.get("sources") or []:
            if not isinstance(s, dict) or not s.get("resource"):
                e(p, "every `sources` entry needs a `resource`")
            elif s.get("id"):
                ids.add(str(s["id"]))
            if isinstance(s, dict) and "last_modified" in s and not is_ts(s["last_modified"]):
                e(p, "`sources[].last_modified` must be an ISO 8601 datetime with UTC offset")
        for label in sorted(set(re.findall(r"\[\^([^\]]+)\]", CODE_RE.sub("", body)))):
            if label not in ids:
                e(p, f"footnote [^{label}] has no matching `sources[].id`")

    for path, text in render_indexes(root).items():
        if not path.exists() or path.read_text(encoding="utf-8") != text:
            e(path, "index is out of date; run `python3 scripts/okf.py index`")

    print("\n".join(errors + warnings) or "", end="\n" if errors or warnings else "")
    print(f"okf check: {n} concepts, {len(errors)} errors, {len(warnings)} warnings"
          f" -> {'FAIL' if errors else 'OK'}")
    return 1 if errors else 0


def main(argv: list[str]) -> int:
    if not argv or argv[0] not in {"index", "check"}:
        print(__doc__)
        return 2
    root = Path(argv[1]).resolve() if len(argv) > 1 else ROOT_DEFAULT
    if not root.is_dir():
        print(f"okf: {root} is not a directory", file=sys.stderr)
        return 2
    return cmd_index(root) if argv[0] == "index" else cmd_check(root)


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
