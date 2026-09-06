#!/usr/bin/env python3
"""Run the documented GitHub entry in a real PTY; choose exit, never a mutation."""
import fcntl
import hashlib
import json
import os
from pathlib import Path
import pty
import select
import signal
import sqlite3
import struct
import subprocess
import termios
import time
import tomllib

os.umask(0o077)
root = Path(__file__).resolve().parent
ref = "53f55e261f0cdeadfa128eb232fb8731e4719bc7"
recipe = '''set -euo pipefail; umask 077; ref="$1"; script=$(mktemp /tmp/wg-guard-bootstrap.XXXXXXXX); trap 'rm -f -- "$script"' EXIT; curl --proto "=https" --tlsv1.2 -fsS --connect-timeout 15 --max-time 120 -o "$script" "https://raw.githubusercontent.com/Sir-Adnan/wg-guard/$ref/install.sh"; bash "$script" --commit "$ref" -- --lang en'''

def snapshot():
    cfg = tomllib.loads(Path("/etc/wg-guard/wg-guard.toml").read_text())
    db = cfg.get("database_path") or str(Path(cfg["data_dir"]) / "wg-guard.db")
    with sqlite3.connect(Path(db).as_uri() + "?mode=ro", uri=True) as conn:
        conn.execute("PRAGMA query_only=ON")
        settings = conn.execute("SELECT key,value,updated_at FROM settings ORDER BY key").fetchall()
    files = {}
    for name in ("/etc/wg-guard/install-state.json", "/etc/wg-guard/lifecycle.json",
                 "/etc/wg-guard/wg-guard.toml", "/etc/wg-guard/compose.yaml", "/usr/local/bin/wg-guard"):
        p = Path(name)
        if p.exists():
            with p.open("rb") as f:
                files[name] = hashlib.file_digest(f, "sha256").hexdigest()
        else:
            files[name] = None
    row = json.loads(subprocess.check_output(["docker", "inspect", "wg-guard"], timeout=15))[0]
    return settings, files, row["Id"], row["Image"], row["State"]["StartedAt"]

def child_setup():
    os.setsid()
    fcntl.ioctl(0, termios.TIOCSCTTY, 0)

def main():
    before = snapshot()
    master, slave = pty.openpty()
    initial = termios.tcgetattr(slave)
    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 80, 0, 0))
    env = {k:v for k,v in os.environ.items() if not k.startswith("WGG_")}
    env.update(TERM="xterm-256color", LANG="C.UTF-8", NO_COLOR="1")
    output = bytearray()
    child = subprocess.Popen(["bash", "-c", recipe, "--", ref], stdin=slave,
        stdout=slave, stderr=slave, env=env, preexec_fn=child_setup)
    start = time.monotonic()
    answered = False
    try:
        while child.poll() is None:
            if time.monotonic() - start > 1100 or len(output) > 1024*1024:
                raise RuntimeError("probe-bound")
            if select.select([master], [], [], 0.25)[0]:
                output.extend(os.read(master, 16384))
            if not answered and b"\n> " in output:
                os.write(master, b"0\n")
                answered = True
        for _ in range(3):
            if select.select([master], [], [], 0.05)[0]:
                output.extend(os.read(master, 16384))
        result = {"ok": child.returncode == 0 and answered and before == snapshot(),
                  "commit": ref, "exit": child.returncode, "manager_exit_selected": answered,
                  "original_settings_deployment_unchanged": before == snapshot(),
                  "terminal_restored": initial == termios.tcgetattr(slave),
                  "elapsed_seconds": round(time.monotonic() - start, 1),
                  "limits": "Real exact one-command installed-node rerun; not fresh installation."}
        result["ok"] = result["ok"] and result["terminal_restored"] and ref.encode() in output
        (root / "one-command.capture").write_bytes(output)
        return result
    finally:
        if child.poll() is None:
            os.killpg(child.pid, signal.SIGTERM)
            try:
                child.wait(timeout=5)
            except subprocess.TimeoutExpired:
                os.killpg(child.pid, signal.SIGKILL)
                child.wait(timeout=5)
        os.close(master)
        os.close(slave)

try:
    report = main()
except Exception as error:
    report = {"ok": False, "error_type": type(error).__name__}
(root / "result.json").write_text(json.dumps(report, indent=2))
print(json.dumps(report))
raise SystemExit(0 if report["ok"] else 1)
