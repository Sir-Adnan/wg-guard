#!/usr/bin/env python3
"""Living-documentation integrity check (stdlib only, no network).

Fails when a relative Markdown link points at a missing file or heading, or
when a living document names a repository path in backticks that no longer
exists. Historical records (phase evidence, archive, CHANGELOG) keep
their recorded paths and are link-checked only.
"""
import os
import re
import sys

ROOT = os.path.normpath(os.path.join(os.path.dirname(os.path.abspath(__file__)), ".."))
SKIP_DIRS = {".git", "node_modules"}
# Paths these records name are evidence of their time, not current claims.
HISTORICAL = re.compile(r"^(CHANGELOG\.md|docs/archive/|docs/development/phase[^/]*\.md$)")
PATH_PREFIXES = ("internal/", "web/", "scripts/", "migrations/", "cmd/", "deploy/", "docs/", ".github/")
LINK = re.compile(r"\]\(([^)\s]+)(?:\s+\"[^\"]*\")?\)")
TICKED = re.compile(r"`([^`\s]+)`")
FENCE = re.compile(r"^(```|~~~).*?^\1", re.S | re.M)


def slug(heading):
    text = re.sub(r"\[([^\]]*)\]\([^)]*\)", r"\1", heading.strip().lower())
    text = re.sub(r"[`*_~]", "", text)
    return "".join(c for c in text if c.isalnum() or c in "-_ ").replace(" ", "-")


def anchors(text):
    found, seen = set(), {}
    for line in FENCE.sub("", text).splitlines():
        m = re.match(r"#{1,6}\s+(.*)", line)
        if m:
            base = slug(m.group(1))
            n = seen.get(base, 0)
            seen[base] = n + 1
            found.add(base if n == 0 else f"{base}-{n}")
        found.update(re.findall(r'<a (?:name|id)="([^"]+)"', line))
    return found


def markdown_files():
    for top, dirs, files in os.walk(ROOT):
        dirs[:] = sorted(d for d in dirs if d not in SKIP_DIRS)
        for name in sorted(files):
            if name.endswith(".md"):
                yield os.path.relpath(os.path.join(top, name), ROOT)


def repo_path_exists(token):
    path = token.rstrip(".,;:)").split("#")[0].split(":")[0]
    if not path.startswith(PATH_PREFIXES) or any(c in path for c in "*{}<>$…"):
        return True
    if os.path.exists(os.path.join(ROOT, path)):
        return True
    # A Go symbol such as internal/tunnel.Backend names its package directory.
    pkg, dot, _ = path.rpartition(".")
    return bool(dot) and "/" in pkg and os.path.isdir(os.path.join(ROOT, pkg))


def main():
    files = list(markdown_files())
    texts = {f: open(os.path.join(ROOT, f), encoding="utf-8").read() for f in files}
    heads = {f: anchors(t) for f, t in texts.items()}
    problems = []
    for f, text in texts.items():
        body = FENCE.sub("", text)
        for m in LINK.finditer(body):
            target = m.group(1)
            if re.match(r"[a-z]+:", target):
                continue
            path, _, anchor = target.partition("#")
            resolved = os.path.normpath(os.path.join(os.path.dirname(f), path)) if path else f
            if not os.path.exists(os.path.join(ROOT, resolved)):
                problems.append(f"{f}: link to missing file {target}")
            elif anchor and resolved.endswith(".md") and anchor not in heads.get(resolved, set()):
                problems.append(f"{f}: link to missing heading {target}")
        if HISTORICAL.match(f.replace(os.sep, "/")):
            continue
        for m in TICKED.finditer(body):
            if not repo_path_exists(m.group(1)):
                problems.append(f"{f}: names missing path `{m.group(1)}`")
    for p in problems:
        print(p)
    print(f"docs: {len(files)} Markdown files, {len(problems)} problems")
    return 1 if problems else 0


if __name__ == "__main__":
    sys.exit(main())
