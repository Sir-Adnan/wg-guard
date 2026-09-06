#!/usr/bin/env python3
"""Owned PTY probes; never confirms a mutation or uses real credentials."""
import errno
import fcntl
import hashlib
import json
import os
from pathlib import Path
import pty
import re
import select
import signal
import sqlite3
import struct
import subprocess
import sys
import termios
import time
import tomllib
import unicodedata

os.umask(0o077)
ROOT = Path(__file__).resolve().parent
BIN = ROOT / "wg-guard_linux_amd64"
EXPECTED_SHA256 = "50cc6590ae405f09b00569b65f57c7227901fb988afe60601695c827b33f857b"
MARKER = b"WGG_SYNTHETIC_TTY_MARKER_no_real_credentials"
ANSI = re.compile(rb"\x1b\[[0-?]*[ -/]*[@-~]")


def snapshot():
    config = tomllib.loads(Path("/etc/wg-guard/wg-guard.toml").read_text())
    db_path = config.get("database_path") or str(Path(config["data_dir"]) / "wg-guard.db")
    with sqlite3.connect(Path(db_path).as_uri() + "?mode=ro", uri=True) as conn:
        conn.execute("PRAGMA query_only=ON")
        settings = conn.execute("SELECT key,value,updated_at FROM settings ORDER BY key").fetchall()
    paths = ["/etc/wg-guard/install-state.json", "/etc/wg-guard/lifecycle.json",
             "/etc/wg-guard/wg-guard.toml", "/etc/wg-guard/compose.yaml", "/usr/local/bin/wg-guard"]
    files = {}
    for name in paths:
        path = Path(name)
        files[name] = hashlib.file_digest(path.open("rb"), "sha256").hexdigest() if path.exists() else None
    row = json.loads(subprocess.check_output(["docker", "inspect", "wg-guard"]))[0]
    return {"settings": settings, "files": files, "container_id": row["Id"],
            "image": row["Image"], "started": row["State"]["StartedAt"], "running": row["State"]["Running"]}


class Session:
    def __init__(self, name, cols, locale, term="xterm-256color", no_color=False):
        self.name, self.cols = name, cols
        self.master, self.slave = pty.openpty()
        fcntl.ioctl(self.slave, termios.TIOCSWINSZ, struct.pack("HHHH", 40, cols, 0, 0))
        self.initial = termios.tcgetattr(self.slave)
        env = dict(os.environ, TERM=term, LANG="C.UTF-8")
        env.pop("NO_COLOR", None)
        if no_color:
            env["NO_COLOR"] = "1"
        def child_setup():
            os.setsid()
            fcntl.ioctl(0, termios.TIOCSCTTY, 0)
        self.child = subprocess.Popen([str(BIN), "manage", "--lang", locale], stdin=self.slave,
                                      stdout=self.slave, stderr=self.slave, env=env, preexec_fn=child_setup)
        self.output = bytearray()
        self.cursor = 0
        self.deadline = time.monotonic() + 45

    def read(self, seconds=0.2):
        if len(self.output) > 256 * 1024 or time.monotonic() > self.deadline:
            raise RuntimeError("bounded probe exceeded")
        if select.select([self.master], [], [], seconds)[0]:
            try:
                self.output.extend(os.read(self.master, 16384))
            except OSError as error:
                if error.errno != errno.EIO:
                    raise

    def prompt(self):
        deadline = time.monotonic() + 12
        while time.monotonic() < deadline:
            position = self.output.find(b"\n> ", self.cursor)
            if position >= 0:
                self.cursor = position + 3
                return
            if self.child.poll() is not None:
                raise RuntimeError("child exited before expected prompt")
            self.read()
        raise RuntimeError("prompt deadline exceeded")

    def send(self, value):
        os.write(self.master, value)

    def choose(self, value):
        self.prompt()
        self.send(value + b"\n")

    def finish(self, expected):
        while self.child.poll() is None:
            self.read()
        for _ in range(3):
            self.read(0.03)
        actual = self.child.returncode
        if actual != expected:
            raise RuntimeError("unexpected exit status " + str(actual))
        current = termios.tcgetattr(self.slave)
        restored = current == self.initial
        if not restored:
            raise RuntimeError("terminal attributes not restored")
        if MARKER in self.output:
            raise RuntimeError("synthetic secret was echoed")
        (ROOT / (self.name + ".capture")).write_bytes(self.output)
        plain = ANSI.sub(b"", self.output).decode("utf-8", errors="strict")
        def cells(line):
            return sum(0 if unicodedata.combining(c) or unicodedata.category(c) == "Cf"
                       else 2 if unicodedata.east_asian_width(c) in ("W", "F") else 1 for c in line)
        widest = max((cells(line) for line in plain.replace("\r", "").splitlines()), default=0)
        if widest > self.cols:
            raise RuntimeError("wrapped text exceeds configured terminal columns")
        return {"case": self.name, "columns": self.cols, "exit": actual,
                "terminal_restored": restored, "max_text_cells": widest,
                "ansi_present": b"\x1b" in self.output, "synthetic_secret_absent": True}

    def close(self):
        if self.child.poll() is None:
            self.child.kill()
            self.child.wait(timeout=5)
        os.close(self.master)
        os.close(self.slave)


def run_case(name, cols, locale, action, expected=0, term="xterm-256color", no_color=False):
    s = Session(name, cols, locale, term, no_color)
    try:
        action(s)
        result = s.finish(expected)
        if (term == "dumb" or no_color) and result["ansi_present"]:
            raise RuntimeError("ANSI emitted when disabled")
        return result
    finally:
        s.close()


def menu_back(s):
    s.choose(b"2")  # Read-only operations submenu; execute no action.
    s.choose(b"0")
    s.choose(b"0")


def invalid_then_back(s):
    s.choose(b"99")
    s.choose("۲".encode())
    s.choose(b"0")
    s.choose(b"0")


def rollback_decline(s):
    s.choose(b"1")
    s.choose(b"3")
    s.choose(b"")  # Default no; no update/rollback invocation.
    s.choose(b"0")
    s.choose(b"0")


def enter_secret(s):
    s.choose(b"3")
    s.choose(b"5")
    s.choose(b"2")
    s.prompt()
    if termios.tcgetattr(s.slave)[3] & termios.ECHO:
        raise RuntimeError("secret prompt has echo enabled")


def secret_cancel(s):
    enter_secret(s)
    s.send(MARKER + b"\n")
    s.prompt()
    if not termios.tcgetattr(s.slave)[3] & termios.ECHO:
        raise RuntimeError("normal input echo was not restored")
    s.send(b"q\n")  # Cancel at chat prompt, before settings writes.


def secret_interrupt(s, key=False):
    enter_secret(s)
    s.send(MARKER)
    if key:
        s.send(b"\x03")
    else:
        os.kill(s.child.pid, signal.SIGINT)


def normal_interrupt(s):
    s.prompt()
    os.kill(s.child.pid, signal.SIGINT)


def eof(s):
    s.prompt()
    s.send(b"\x04")


results = []
try:
    if hashlib.file_digest(BIN.open("rb"), "sha256").hexdigest() != EXPECTED_SHA256:
        raise RuntimeError("fixture requires its exact reviewed M4 candidate; menu layouts may change")
    before = snapshot()
    for locale in ("en", "fa"):
        for width in (48, 80, 120):
            results.append(run_case(f"menu-{locale}-{width}", width, locale, menu_back))
        results.append(run_case(f"dumb-{locale}", 80, locale, menu_back, term="dumb"))
        results.append(run_case(f"no-color-{locale}", 80, locale, menu_back, no_color=True))
    results.append(run_case("invalid-persian-digits", 48, "fa", invalid_then_back))
    results.append(run_case("rollback-blank-declined", 48, "en", rollback_decline))
    results.append(run_case("secret-cancel", 48, "en", secret_cancel, expected=1))
    results.append(run_case("secret-sigint", 80, "en", secret_interrupt, expected=1))
    results.append(run_case("secret-ctrl-c", 80, "fa", lambda s: secret_interrupt(s, True), expected=1))
    results.append(run_case("normal-sigint", 80, "en", normal_interrupt))
    results.append(run_case("normal-eof", 80, "en", eof))
    for args, expected in (([], 2), (["manage", "--lang", "fa"], 1), (["help"], 0)):
        p = subprocess.run([str(BIN)] + args, stdin=subprocess.DEVNULL, capture_output=True, timeout=15)
        if p.returncode != expected or b"\x1b" in p.stdout + p.stderr:
            raise RuntimeError("nonTTY behavior mismatch")
    after = snapshot()
    if before != after:
        raise RuntimeError("original settings/deployment changed during readonly probes")
    report = {"ok": True, "candidate_sha256": hashlib.file_digest(BIN.open("rb"), "sha256").hexdigest(),
              "cases": results, "non_tty_cases": 3, "settings_and_deployment_unchanged": True,
              "limits": "Text cell measurements do not certify Persian visual shaping on every SSH client."}
except Exception as error:
    report = {"ok": False, "completed_cases": results, "error_type": type(error).__name__,
              "error": str(error)}
(ROOT / "result.json").write_text(json.dumps(report, indent=2))
print(json.dumps(report))
sys.exit(0 if report["ok"] else 1)
