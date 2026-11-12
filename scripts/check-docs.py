#!/usr/bin/env python3
"""Checks the mechanical parts of the docs: a title on the first line, no em dashes, balanced code fences,
and relative links (with their #anchors) that point at something real. Run with `make docs-check`."""
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
FILES = [ROOT / "README.md", ROOT / "CONTRIBUTING.md", ROOT / "AGENTS.md"] + sorted((ROOT / "docs").glob("*.md")) + sorted((ROOT / "examples").rglob("*.md"))

LINK = re.compile(r"(?<!!)\[[^\]]*\]\(([^)\s]+)(?:\s+\"[^\"]*\")?\)")
HEADING = re.compile(r"^(#{1,6})\s+(.*?)\s*#*\s*$")


def slug(text: str) -> str:
    """GitHub's heading anchor: lowercase, drop punctuation except hyphens and spaces, spaces to hyphens."""
    text = re.sub(r"`", "", text.strip().lower())
    text = re.sub(r"[^\w\- ]", "", text)
    return text.replace(" ", "-")


def anchors(path: Path) -> set:
    out, fence = set(), False
    for line in path.read_text(encoding="utf-8").splitlines():
        if line.lstrip().startswith("```"):
            fence = not fence
            continue
        m = None if fence else HEADING.match(line)
        if m:
            out.add(slug(m.group(2)))
    return out


problems = []
cache = {}
for f in FILES:
    rel = f.relative_to(ROOT)
    text = f.read_text(encoding="utf-8")
    lines = text.splitlines()
    first = next((l for l in lines if l.strip()), "")
    if not first.startswith("# "):
        problems.append(f"{rel}: the first line must be a '# ' title")
    if "—" in text:
        n = text.index("—")
        problems.append(f"{rel}:{text[:n].count(chr(10)) + 1}: em dash (use a comma, colon or full stop)")
    if sum(1 for l in lines if l.lstrip().startswith("```")) % 2:
        problems.append(f"{rel}: a code fence is never closed")
    fence = False
    for i, line in enumerate(lines, 1):
        if line.lstrip().startswith("```"):
            fence = not fence
        if fence:
            continue
        for target in LINK.findall(line):
            if re.match(r"^(https?:|mailto:|#)", target) and not target.startswith("#"):
                continue
            path_part, _, frag = target.partition("#")
            dest = f if not path_part else (f.parent / path_part).resolve()
            if not dest.exists():
                problems.append(f"{rel}:{i}: link to a missing file: {target}")
                continue
            if frag and dest.suffix == ".md":
                if dest not in cache:
                    cache[dest] = anchors(dest)
                if frag.lower() not in cache[dest]:
                    problems.append(f"{rel}:{i}: link to a missing heading: {target}")

if problems:
    print("\n".join(problems))
    print(f"\n{len(problems)} problem(s) in the docs")
    sys.exit(1)
print(f"docs ok ({len(FILES)} files)")
