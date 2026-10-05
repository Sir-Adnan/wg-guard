#!/usr/bin/env python3
"""Build one canonical, precompiled-manager runtime and its offline image asset."""
import argparse
import gzip
import hashlib
import json
from pathlib import Path
import shutil
import signal
import subprocess
import tempfile


def run(*args):
    return subprocess.check_output(args, text=True, timeout=900).strip()


def digest(file):
    with file.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--assets", type=Path, required=True)
    parser.add_argument("--core", default="recommended")
    args = parser.parse_args()
    assets = args.assets.resolve(strict=True)
    metadata = json.loads((assets / "release-metadata.json").read_text())
    binary = assets / "wg-guard_linux_amd64"
    if digest(binary) != metadata["binary_sha256"] or metadata["platform"] != "linux/amd64":
        raise SystemExit("manager identity mismatch")
    info = json.loads(run(str(binary), "runtime-recipe", args.core))
    core, contract = info["core"], info["contract"]
    labels = {
        "org.opencontainers.image.source": "https://github.com/Sir-Adnan/wg-guard",
        "org.opencontainers.image.version": metadata["version"],
        "org.opencontainers.image.revision": metadata["commit"],
        "io.wg-guard.binary.sha256": metadata["binary_sha256"],
        "io.wg-guard.core.bundle": core["id"],
        "io.wg-guard.runtime.recipe.sha256": info["recipe_sha256"],
        "io.wg-guard.notices.sha256": info["notices_sha256"],
        "io.wg-guard.data.contract": contract["data_contract"],
        "io.wg-guard.deployment.schema": str(contract["deployment_schema"]),
        "io.wg-guard.maintenance.protocol": str(contract["maintenance_protocol"]),
        "io.wg-guard.awg-tools.commit": core["tools_commit"],
        "io.wg-guard.awg-userspace.commit": core["userspace_commit"],
    }
    with tempfile.TemporaryDirectory(prefix="wg-guard-image-") as directory:
        context = Path(directory)
        (context / "Dockerfile").write_text(info["dockerfile"])
        shutil.copy2(binary, context / "wg-guard")
        for name, content in info["notices"].items():
            relative = Path(name)
            if relative.is_absolute() or ".." in relative.parts:
                raise SystemExit("unsafe embedded notice path")
            target = context / "notices" / relative
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_text(content)
        build = ["docker", "build", "--platform", "linux/amd64", "--iidfile", str(context / "image-id")]
        for name, value in labels.items():
            build.extend(["--label", name + "=" + value])
        subprocess.run(build + [str(context)], check=True, timeout=900)
        image = (context / "image-id").read_text().strip()
        observed = json.loads(run("docker", "image", "inspect", image))[0]
        if observed["Architecture"] != "amd64" or observed["Os"] != "linux" or any(
            observed["Config"]["Labels"].get(key) != value for key, value in labels.items()
        ):
            raise SystemExit("runtime provenance mismatch")
        embedded = run("docker", "run", "--rm", "--network", "none", "--entrypoint", "sha256sum", image, "/usr/local/bin/wg-guard").split()[0]
        if embedded != metadata["binary_sha256"]:
            raise SystemExit("runtime binary differs from verified manager")
        if json.loads(run("docker", "run", "--rm", "--network", "none", "--entrypoint", "/usr/local/bin/wg-guard", image, "installer-contract")) != contract:
            raise SystemExit("runtime contract mismatch")
        if core["tools_version"] not in run("docker", "run", "--rm", "--network", "none", "--entrypoint", "/usr/local/bin/awg", image, "--version"):
            raise SystemExit("tools version mismatch")
        # The pinned daemon's --version text is stale. Verify embedded VCS build
        # provenance through the manager, never identify the engine from that text.
        run("docker", "run", "--rm", "--network", "none", "--entrypoint", "/usr/local/bin/amneziawg-go", image, "--version")
        if run("docker", "run", "--rm", "--network", "none", "--entrypoint", "/usr/local/bin/wg-guard", image, "userspace-check") != "verified":
            raise SystemExit("userspace build provenance mismatch")
        run("docker", "run", "--rm", "--network", "none", "--entrypoint", "test", image, "-s", "/usr/share/doc/wg-guard/THIRD_PARTY.md")
        archive = assets / "runtime_linux_amd64.tar.gz"
        # Keep image layers out of memory; enforce an uncompressed disk/stream budget.
        with subprocess.Popen(["docker", "save", image], stdout=subprocess.PIPE) as process:
            try:
                def timed_out(signum, frame):
                    raise TimeoutError("runtime export deadline exceeded")
                previous_alarm = signal.signal(signal.SIGALRM, timed_out)
                signal.alarm(900)
                with archive.open("xb") as destination, gzip.GzipFile(fileobj=destination, mode="wb", mtime=0) as zipped:
                    size = 0
                    for chunk in iter(lambda: process.stdout.read(1 << 16), b""):
                        size += len(chunk)
                        if size > 4 << 30:
                            raise SystemExit("runtime export exceeds raw size limit")
                        zipped.write(chunk)
                if process.wait(timeout=120) != 0:
                    raise SystemExit("runtime image export failed")
            except BaseException:
                process.kill()
                archive.unlink(missing_ok=True)
                raise
            finally:
                signal.alarm(0)
                signal.signal(signal.SIGALRM, previous_alarm)
        if archive.stat().st_size > 1 << 30:
            raise SystemExit("runtime archive exceeds acquisition limit")
    manifest = {
        "schema": 1, "version": metadata["version"], "commit": metadata["commit"], "platform": "linux/amd64",
        "binary_sha256": metadata["binary_sha256"], "image_id": image,
        "recipe_sha256": info["recipe_sha256"], "notices_sha256": info["notices_sha256"],
        "archive": archive.name, "archive_sha256": digest(archive), "archive_size": archive.stat().st_size,
        "data_contract": contract["data_contract"], "deployment_schema": contract["deployment_schema"],
        "maintenance_protocol": contract["maintenance_protocol"], "tools_version": core["tools_version"],
        "tools_commit": core["tools_commit"], "userspace_version": core["userspace_version"], "userspace_commit": core["userspace_commit"],
        "kernels": [{"id": b["id"], "version": b["kernel_version"], "commit": b["kernel_commit"]} for b in info["kernels"]],
        "sbom_sha256": digest(assets / "sbom.spdx.json"),
    }
    (assets / "runtime-metadata.json").write_text(json.dumps(manifest, sort_keys=True, indent=2) + "\n")
    with (assets / "checksums.txt").open("a") as checks:
        for file in (archive, assets / "runtime-metadata.json"):
            checks.write(f"{digest(file)}  {file.name}\n")
    print(f"Verified runtime image: {image}")


if __name__ == "__main__":
    main()
