#!/usr/bin/env python
"""VPS command runner for WG-Guard Phase 7 verification.

Credentials come from the environment (set by the operator session), never
from files inside the repository. The file itself is git-ignored.
"""
import os
import sys

import paramiko

HOST = "178.105.13.15"


def client():
    c = paramiko.SSHClient()
    c.set_missing_host_key_policy(paramiko.AutoAddPolicy())
    c.connect(
        HOST,
        username=os.environ["VPS_USER"],
        password=os.environ["VPS_PASS"],
        look_for_keys=False,
        allow_agent=False,
        timeout=15,
    )
    return c


def run(cmd, timeout=300, show=True):
    c = client()
    try:
        stdin, stdout, stderr = c.exec_command(cmd, timeout=timeout)
        out = stdout.read().decode("utf-8", "replace")
        err = stderr.read().decode("utf-8", "replace")
        code = stdout.channel.recv_exit_status()
    finally:
        c.close()
    if show:
        if out.strip():
            print(out.rstrip())
        if err.strip():
            print("[stderr]", err.rstrip(), file=sys.stderr)
        print(f"[exit {code}]")
    return code, out, err


if __name__ == "__main__":
    cmd = sys.argv[1] if len(sys.argv) > 1 else "hostname"
    timeout = int(sys.argv[2]) if len(sys.argv) > 2 else 300
    code, _, _ = run(cmd, timeout=timeout)
    sys.exit(code)
