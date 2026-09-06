#!/usr/bin/env python3
"""Behavioral tests for the isolated Phase 8.1 backup acceptance helper."""

from __future__ import annotations

import hashlib
import importlib.util
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import unittest


sys.dont_write_bytecode = True

ROOT = Path(__file__).resolve().parents[1]
HELPER = ROOT / "docs/integrations/fixtures/verify-phase8.1-synthetic-backup.py"


def load_helper():
    spec = importlib.util.spec_from_file_location("phase81_synthetic_backup", HELPER)
    if spec is None or spec.loader is None:
        raise RuntimeError("cannot load helper")
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


class SyntheticBackupHelperTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.helper = load_helper()

    def setUp(self):
        if os.name != "posix":
            self.skipTest("the acceptance helper intentionally targets Linux")
        self.temp = tempfile.TemporaryDirectory(prefix="wgg-helper-test-")
        self.root = Path(self.temp.name)

    def tearDown(self):
        self.temp.cleanup()

    def candidate(self, mode: int = 0o700) -> tuple[Path, str]:
        path = self.root / "candidate"
        path.write_bytes(b"synthetic candidate\n")
        path.chmod(mode)
        return path, hashlib.sha256(path.read_bytes()).hexdigest()

    def options(self, candidate: Path, digest: str, *extra: str):
        return self.helper.build_parser().parse_args(
            ["--candidate", str(candidate), "--expected-sha256", digest, *extra]
        )

    def test_prepare_rejects_mismatch_before_creating_requested_workspace(self):
        candidate, _ = self.candidate()
        work = self.root / "must-not-exist"
        options = self.options(candidate, "0" * 64, "--work-dir", str(work))

        with self.assertRaisesRegex(self.helper.PreflightError, "candidate SHA-256 mismatch"):
            self.helper.prepare(options)

        self.assertFalse(work.exists())

    def test_prepare_rejects_symlink_and_group_writable_candidate(self):
        candidate, digest = self.candidate(0o720)
        options = self.options(candidate, digest)
        with self.assertRaisesRegex(self.helper.PreflightError, "candidate permissions"):
            self.helper.prepare(options)

        candidate.chmod(0o700)
        link = self.root / "candidate-link"
        link.symlink_to(candidate)
        options = self.options(link, digest)
        with self.assertRaisesRegex(self.helper.PreflightError, "regular non-symlink"):
            self.helper.prepare(options)

    def test_telegram_credentials_require_opt_in_and_private_regular_file(self):
        candidate, digest = self.candidate()
        credentials = self.root / "telegram.json"
        credentials.write_text('{"bot_token":"123:synthetic-token","chat_id":"-12345"}')
        credentials.chmod(0o600)

        with self.assertRaisesRegex(self.helper.PreflightError, "requires --real-telegram"):
            self.helper.prepare(
                self.options(candidate, digest, "--telegram-credentials-file", str(credentials))
            )
        with self.assertRaisesRegex(self.helper.PreflightError, "requires --telegram-credentials-file"):
            self.helper.prepare(self.options(candidate, digest, "--real-telegram"))

        credentials.chmod(0o640)
        with self.assertRaisesRegex(self.helper.PreflightError, "credential permissions"):
            self.helper.prepare(
                self.options(
                    candidate,
                    digest,
                    "--real-telegram",
                    "--telegram-credentials-file",
                    str(credentials),
                )
            )

    def test_prepare_loads_credentials_without_putting_values_in_repr(self):
        candidate, digest = self.candidate()
        credentials = self.root / "telegram.json"
        credentials.write_text('{"bot_token":"123:synthetic-token","chat_id":"-12345"}')
        credentials.chmod(0o600)
        prepared = self.helper.prepare(
            self.options(
                candidate,
                digest,
                "--real-telegram",
                "--telegram-credentials-file",
                str(credentials),
            )
        )

        self.assertEqual(prepared.credentials.token, "123:synthetic-token")
        self.assertNotIn("synthetic-token", repr(prepared))
        self.assertNotIn("-12345", repr(prepared))

    def test_workspace_cleanup_removes_only_new_owned_directory(self):
        outside = self.root / "outside"
        outside.write_text("preserve")
        work = self.root / "owned"
        workspace = self.helper.OwnedWorkspace.create(work)
        (workspace.path / "private").write_text("owned")

        workspace.cleanup()

        self.assertFalse(work.exists())
        self.assertEqual(outside.read_text(), "preserve")
        with self.assertRaisesRegex(self.helper.PreflightError, "already exists"):
            self.helper.OwnedWorkspace.create(outside)

    def test_owned_child_stop_does_not_signal_unrelated_process(self):
        unrelated = subprocess.Popen(
            [sys.executable, "-c", "import time; time.sleep(30)"],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )
        child = self.helper.OwnedChild.start(
            [sys.executable, "-c", "import time; time.sleep(30)"],
            env={"PATH": os.environ.get("PATH", "")},
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )
        try:
            child.stop(timeout=2)
            self.assertIsNotNone(child.process.poll())
            self.assertIsNone(unrelated.poll())
        finally:
            if child.process.poll() is None:
                child.stop(timeout=1)
            unrelated.terminate()
            unrelated.wait(timeout=3)

    def test_secret_scan_fails_without_echoing_secret(self):
        secret = "123:synthetic-secret"
        with self.assertRaises(self.helper.SecretLeakError) as raised:
            self.helper.assert_secrets_absent([b"prefix 123:synthetic-secret suffix"], [secret])

        self.assertNotIn(secret, str(raised.exception))

    def test_secret_scan_failure_still_removes_owned_workspace(self):
        candidate, digest = self.candidate()
        prepared = self.helper.prepare(self.options(candidate, digest))
        run = self.helper.AcceptanceRun(prepared)
        work = self.root / "owned-leak"
        run.workspace = self.helper.OwnedWorkspace.create(work)
        run.log = work / "absent.log"
        run.captures.append(run.password.encode())

        with self.assertRaises(self.helper.SecretLeakError):
            run.cleanup()

        self.assertFalse(work.exists())

    def test_each_run_generates_a_distinct_private_archive_password(self):
        candidate, digest = self.candidate()
        prepared = self.helper.prepare(self.options(candidate, digest))

        first = self.helper.AcceptanceRun(prepared).password
        second = self.helper.AcceptanceRun(prepared).password

        self.assertGreaterEqual(len(first), 32)
        self.assertNotEqual(first, second)
        self.assertNotIn(digest[:16], first)


if __name__ == "__main__":
    unittest.main(verbosity=2)
