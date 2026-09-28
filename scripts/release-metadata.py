#!/usr/bin/env python3
"""Write deterministic candidate metadata, a Go-module SBOM and bundled notices."""

import argparse
import datetime
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess


def command(*args, cwd=None):
    return subprocess.check_output(args, cwd=cwd, text=True)


def json_stream(raw):
    decoder = json.JSONDecoder()
    pos = 0
    while pos < len(raw):
        while pos < len(raw) and raw[pos].isspace():
            pos += 1
        if pos < len(raw):
            value, pos = decoder.raw_decode(raw, pos)
            yield value


def main():
    parser = argparse.ArgumentParser()
    for name in ("version", "commit", "binary", "source", "assets", "bundle"):
        parser.add_argument("--" + name, required=True)
    args = parser.parse_args()
    source, assets, bundle = Path(args.source), Path(args.assets), Path(args.bundle)
    binary = Path(args.binary)
    digest = hashlib.sha256(binary.read_bytes()).hexdigest()
    created = datetime.datetime.fromtimestamp(
        int(os.environ["SOURCE_DATE_EPOCH"]), datetime.timezone.utc
    ).strftime("%Y-%m-%dT%H:%M:%SZ")
    go_version = command("go", "version").strip()
    build_info = command("go", "version", "-m", str(binary))
    linked = {}
    for line in build_info.splitlines():
        fields = line.strip().split("\t")
        if len(fields) >= 3 and fields[0] == "dep":
            linked[fields[1]] = fields[2]
    modules = {
        m["Path"]: m for m in json_stream(command("go", "list", "-m", "-json", "all", cwd=source))
    }
    if not linked or any(path not in modules for path in linked):
        raise SystemExit("linked Go module inventory is incomplete")

    metadata = {
        "version": args.version,
        "commit": args.commit,
        "platform": "linux/amd64",
        "binary": "wg-guard_linux_amd64",
        "binary_sha256": digest,
        "go_toolchain": go_version,
        "source_date": created,
    }
    (assets / "release-metadata.json").write_text(
        json.dumps(metadata, indent=2, sort_keys=True) + "\n", encoding="utf-8"
    )

    packages = [{
        "SPDXID": "SPDXRef-WGGuard",
        "name": "wg-guard",
        "versionInfo": args.version,
        "downloadLocation": "NOASSERTION",
        "filesAnalyzed": False,
        "licenseConcluded": "NOASSERTION",
        "licenseDeclared": "MIT",
        "checksums": [{"algorithm": "SHA256", "checksumValue": digest}],
    }]
    relationships = [{
        "spdxElementId": "SPDXRef-DOCUMENT", "relationshipType": "DESCRIBES",
        "relatedSpdxElement": "SPDXRef-WGGuard",
    }]
    for index, (path, version) in enumerate(sorted(linked.items()), start=1):
        module = modules[path]
        identifier = f"SPDXRef-GoModule-{index}"
        packages.append({
            "SPDXID": identifier,
            "name": path,
            "versionInfo": version,
            "downloadLocation": "NOASSERTION",
            "filesAnalyzed": False,
            "licenseConcluded": "NOASSERTION",
            "licenseDeclared": "NOASSERTION",
            "externalRefs": [{
                "referenceCategory": "PACKAGE-MANAGER",
                "referenceType": "purl",
                "referenceLocator": f"pkg:golang/{path}@{version}",
            }],
        })
        relationships.append({
            "spdxElementId": "SPDXRef-WGGuard", "relationshipType": "DEPENDS_ON",
            "relatedSpdxElement": identifier,
        })
        if not module.get("Dir"):
            raise SystemExit(f"linked Go module has no local source directory: {path}@{version}")
        directory = Path(module["Dir"])
        notice_files = [p for p in directory.iterdir() if p.is_file() and re.match(
            r"^(LICENSE|LICENCE|COPYING|NOTICE)([._-].*)?$", p.name, re.IGNORECASE
        )] if directory.is_dir() else []
        if not notice_files:
            raise SystemExit(f"no root license/notice file for linked Go module {path}@{version}")
        destination = bundle / "licenses" / "go" / f"{index:02d}-{path.replace('/', '_')}"
        destination.mkdir(parents=True, exist_ok=True)
        for notice in sorted(notice_files):
            shutil.copy2(notice, destination / notice.name)
    document = {
        "spdxVersion": "SPDX-2.3",
        "dataLicense": "CC0-1.0",
        "SPDXID": "SPDXRef-DOCUMENT",
        "name": f"wg-guard-{args.version}-linux-amd64",
        "documentNamespace": f"https://github.com/Sir-Adnan/wg-guard/releases/tag/{args.version}#spdx-{args.commit}",
        "creationInfo": {"creators": ["Tool: WG-Guard release-metadata.py"], "created": created},
        "packages": packages,
        "relationships": relationships,
    }
    (assets / "sbom.spdx.json").write_text(
        json.dumps(document, indent=2, sort_keys=True) + "\n", encoding="utf-8"
    )
    shutil.copy2(binary, bundle / binary.name)
    for name in ("LICENSE", "THIRD_PARTY.md"):
        shutil.copy2(source / name, bundle / name)
    shutil.copytree(source / "third_party" / "licenses", bundle / "third_party" / "licenses")
    shutil.copy2(assets / "release-metadata.json", bundle / "release-metadata.json")
    shutil.copy2(assets / "sbom.spdx.json", bundle / "sbom.spdx.json")


if __name__ == "__main__":
    main()
