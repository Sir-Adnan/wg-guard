#!/usr/bin/env python3
"""Run an owned, fake-backend backup/Telegram/scheduler acceptance drill.

This helper is intentionally Linux-only.  It starts one candidate child with
private synthetic state, uses only that child's SQLite database, and never
installs packages, services, modules, containers, tunnels, or firewall rules.
"""

from __future__ import annotations

import argparse
from dataclasses import dataclass, field
from datetime import datetime, timedelta, timezone
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import signal
import secrets
import socket
import sqlite3
import stat
import subprocess
import sys
import tempfile
import threading
import time
from typing import BinaryIO, Sequence
from urllib.error import URLError
from urllib.request import urlopen


os.umask(0o077)
CAPTURE_LIMIT = 256 * 1024
LOG_LIMIT = 1024 * 1024
COMMAND_TIMEOUT = 135
READY_TIMEOUT = 30
SCHEDULER_TIMEOUT = 90
CREDENTIAL_LIMIT = 16 * 1024
SHA256_RE = re.compile(r"^[0-9a-f]{64}$")
CHAT_RE = re.compile(r"^-?[1-9][0-9]*$")
SCHEDULE_ID_RE = re.compile(r"(?m)^([0-9a-f]{16})\s+\|")


class PreflightError(RuntimeError):
    """A safe refusal before the candidate or any remote action starts."""


class AcceptanceError(RuntimeError):
    """A sanitized acceptance failure."""


class SecretLeakError(AcceptanceError):
    """A capture contained one of the private values (value never echoed)."""

    def __init__(self):
        super().__init__("private value detected in a bounded capture")


@dataclass(frozen=True)
class TelegramCredentials:
    token: str = field(repr=False)
    chat_id: str = field(repr=False)


@dataclass(frozen=True)
class Prepared:
    candidate: Path
    candidate_sha256: str
    work_dir: Path | None
    work_parent: Path | None
    result: Path | None
    listen_port: int
    real_telegram: bool
    credentials_file: Path | None
    credentials: TelegramCredentials | None = field(repr=False)


def _validate_work_ancestry(parent: Path) -> None:
    """Reject locations a different non-root user can replace underneath us."""
    parent = parent.absolute()
    current_uid = os.geteuid()
    for component in reversed((parent, *parent.parents)):
        try:
            info = component.lstat()
        except OSError as error:
            raise PreflightError("work location ancestor is not accessible") from error
        if not stat.S_ISDIR(info.st_mode) or component.is_symlink():
            raise PreflightError("work location ancestor must be a non-symlink directory")
        if info.st_uid not in (0, current_uid):
            raise PreflightError("work location ancestor has an untrusted owner")
        mode = stat.S_IMODE(info.st_mode)
        sticky_boundary = bool(mode & stat.S_ISVTX) and info.st_uid in (0, current_uid)
        if mode & 0o022 and not sticky_boundary:
            raise PreflightError("work location has an unsafe writable ancestor")


class OwnedWorkspace:
    """One collision-checked directory whose exact path this run may remove."""

    def __init__(self, path: Path, info: os.stat_result):
        self.path = path
        self._identity = (info.st_dev, info.st_ino)
        self._created = True

    @classmethod
    def create(cls, path: Path) -> "OwnedWorkspace":
        path = Path(path)
        _validate_work_ancestry(path.parent)
        if path.exists() or path.is_symlink():
            raise PreflightError("requested work directory already exists")
        try:
            path.mkdir(mode=0o700, parents=False)
        except FileExistsError as error:
            raise PreflightError("requested work directory already exists") from error
        info = path.lstat()
        if stat.S_IMODE(info.st_mode) & 0o077:
            path.rmdir()
            raise PreflightError("work directory is not private")
        return cls(path, info)

    @classmethod
    def temporary(cls, parent: Path | None) -> "OwnedWorkspace":
        target_parent = Path(parent) if parent else Path(tempfile.gettempdir())
        _validate_work_ancestry(target_parent)
        value = tempfile.mkdtemp(prefix="wg-guard-phase81-synthetic-", dir=target_parent)
        path = Path(value)
        path.chmod(0o700)
        return cls(path, path.lstat())

    def cleanup(self) -> None:
        if not self._created:
            return
        # The exact path was atomically created above; never derive or glob a
        # cleanup target and never follow an attacker-substituted directory.
        try:
            current = self.path.lstat()
        except FileNotFoundError:
            self._created = False
            return
        if (
            not stat.S_ISDIR(current.st_mode)
            or self.path.is_symlink()
            or (current.st_dev, current.st_ino) != self._identity
        ):
            raise AcceptanceError("owned work directory identity changed before cleanup")
        shutil.rmtree(self.path)
        self._created = False


class OwnedChild:
    """A child in its own session; stop signals only that owned process group."""

    def __init__(self, process: subprocess.Popen[bytes]):
        self.process = process

    @classmethod
    def start(cls, argv: Sequence[str], **kwargs) -> "OwnedChild":
        process = subprocess.Popen(list(argv), start_new_session=True, **kwargs)
        return cls(process)

    def stop(self, timeout: float = 10) -> None:
        if self.process.poll() is not None:
            return
        try:
            os.killpg(self.process.pid, signal.SIGTERM)
            self.process.wait(timeout=timeout)
        except subprocess.TimeoutExpired:
            os.killpg(self.process.pid, signal.SIGKILL)
            self.process.wait(timeout=5)
        except ProcessLookupError:
            self.process.wait(timeout=5)

    def stop_for_overflow(self) -> None:
        """Bound a noisy child without ever targeting a foreign process."""
        self.stop(timeout=1)


@dataclass
class CommandResult:
    stdout: bytes
    stderr: bytes
    returncode: int


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="isolated synthetic backup/Telegram/scheduler acceptance"
    )
    parser.add_argument("--candidate", required=True, type=Path)
    parser.add_argument("--expected-sha256", required=True)
    parser.add_argument("--telegram-credentials-file", type=Path)
    parser.add_argument("--real-telegram", action="store_true")
    parser.add_argument("--work-dir", type=Path)
    parser.add_argument("--work-parent", type=Path)
    parser.add_argument("--result", type=Path)
    parser.add_argument("--listen-port", type=int, default=0)
    return parser


def _open_regular(path: Path, label: str, executable: bool = False) -> tuple[int, os.stat_result]:
    flags = os.O_RDONLY | os.O_NONBLOCK
    if hasattr(os, "O_CLOEXEC"):
        flags |= os.O_CLOEXEC
    if hasattr(os, "O_NOFOLLOW"):
        flags |= os.O_NOFOLLOW
    try:
        descriptor = os.open(path, flags)
    except OSError as error:
        raise PreflightError(f"{label} is not an accessible regular non-symlink file") from error
    info = os.fstat(descriptor)
    if not stat.S_ISREG(info.st_mode):
        os.close(descriptor)
        raise PreflightError(f"{label} must be a regular non-symlink file")
    if info.st_uid != os.geteuid():
        os.close(descriptor)
        raise PreflightError(f"{label} must be owned by the current user")
    mode = stat.S_IMODE(info.st_mode)
    if mode & 0o022 or executable and not mode & 0o100:
        os.close(descriptor)
        raise PreflightError(f"{label} permissions are unsafe")
    return descriptor, info


def _hash_descriptor(descriptor: int) -> str:
    digest = hashlib.sha256()
    while True:
        chunk = os.read(descriptor, 1024 * 1024)
        if not chunk:
            return digest.hexdigest()
        digest.update(chunk)


def _hash_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def _load_credentials(path: Path) -> TelegramCredentials:
    descriptor, info = _open_regular(path, "credential")
    if stat.S_IMODE(info.st_mode) & 0o077:
        os.close(descriptor)
        raise PreflightError("credential permissions must be 0600 or stricter")
    if info.st_size > CREDENTIAL_LIMIT:
        os.close(descriptor)
        raise PreflightError("credential file exceeds 16 KiB")
    try:
        raw = os.read(descriptor, CREDENTIAL_LIMIT + 1)
    finally:
        os.close(descriptor)
    if len(raw) > CREDENTIAL_LIMIT:
        raise PreflightError("credential file exceeds 16 KiB")
    try:
        value = json.loads(raw)
    except (UnicodeDecodeError, json.JSONDecodeError) as error:
        raise PreflightError("credential file must be valid UTF-8 JSON") from error
    if not isinstance(value, dict) or set(value) != {"bot_token", "chat_id"}:
        raise PreflightError("credential JSON must contain only bot_token and chat_id")
    token, chat = value["bot_token"], value["chat_id"]
    if (
        not isinstance(token, str)
        or not token
        or len(token) > 512
        or ":" not in token
        or any(char.isspace() or ord(char) < 0x20 for char in token)
    ):
        raise PreflightError("credential bot_token has an invalid shape")
    if not isinstance(chat, str) or not CHAT_RE.fullmatch(chat):
        raise PreflightError("credential chat_id must be a nonzero signed decimal string")
    return TelegramCredentials(token, chat)


def prepare(options: argparse.Namespace) -> Prepared:
    if os.name != "posix" or not sys.platform.startswith("linux"):
        raise PreflightError("this acceptance helper requires Linux")
    candidate = Path(options.candidate).absolute()
    expected = str(options.expected_sha256).lower()
    if not SHA256_RE.fullmatch(expected):
        raise PreflightError("expected SHA-256 must be 64 lowercase hexadecimal characters")
    descriptor, _ = _open_regular(candidate, "candidate", executable=True)
    try:
        actual = _hash_descriptor(descriptor)
    finally:
        os.close(descriptor)
    if actual != expected:
        raise PreflightError("candidate SHA-256 mismatch")

    credentials_file = None
    credentials = None
    if options.telegram_credentials_file and not options.real_telegram:
        raise PreflightError("--telegram-credentials-file requires --real-telegram")
    if options.real_telegram and not options.telegram_credentials_file:
        raise PreflightError("--real-telegram requires --telegram-credentials-file")
    if options.real_telegram:
        credentials_file = Path(options.telegram_credentials_file).absolute()
        credentials = _load_credentials(credentials_file)

    work_dir = Path(options.work_dir).absolute() if options.work_dir else None
    work_parent = Path(options.work_parent).absolute() if options.work_parent else None
    if work_dir and work_parent:
        raise PreflightError("use only one of --work-dir and --work-parent")
    if work_dir:
        _validate_work_ancestry(work_dir.parent)
        if work_dir.exists() or work_dir.is_symlink():
            raise PreflightError("requested work directory already exists")
        if not work_dir.parent.is_dir():
            raise PreflightError("work directory parent does not exist")
    if work_parent:
        _validate_work_ancestry(work_parent)

    result = Path(options.result).absolute() if options.result else None
    if result:
        if result.exists() or result.is_symlink():
            raise PreflightError("result path already exists")
        if not result.parent.is_dir() or result.parent.is_symlink():
            raise PreflightError("result parent must be an existing non-symlink directory")
    if options.listen_port < 0 or options.listen_port > 65535:
        raise PreflightError("listen port must be 0..65535")

    return Prepared(
        candidate,
        actual,
        work_dir,
        work_parent,
        result,
        options.listen_port,
        options.real_telegram,
        credentials_file,
        credentials,
    )


def assert_secrets_absent(captures: Sequence[bytes], secrets: Sequence[str]) -> None:
    needles = [secret.encode() for secret in secrets if secret]
    for capture in captures:
        if any(needle in capture for needle in needles):
            raise SecretLeakError()


def _drain(
    pipe: BinaryIO,
    target: bytearray,
    limit: int,
    overflow: threading.Event,
    on_overflow=None,
) -> None:
    try:
        while True:
            chunk = pipe.read(8192)
            if not chunk:
                return
            room = limit - len(target)
            if room > 0:
                target.extend(chunk[:room])
            if len(chunk) > room and not overflow.is_set():
                overflow.set()
                if on_overflow is not None:
                    try:
                        on_overflow()
                    except Exception:
                        # The main cleanup path retries the owned-child stop;
                        # never emit a thread traceback with captured data.
                        pass
    finally:
        pipe.close()


def run_bounded(
    argv: Sequence[str],
    *,
    env: dict[str, str],
    input_bytes: bytes = b"",
    timeout: int = COMMAND_TIMEOUT,
) -> CommandResult:
    process = subprocess.Popen(
        list(argv),
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        env=env,
        start_new_session=True,
    )
    assert process.stdin is not None and process.stdout is not None and process.stderr is not None
    stdout, stderr = bytearray(), bytearray()
    overflow = threading.Event()
    threads = [
        threading.Thread(
            target=_drain, args=(process.stdout, stdout, CAPTURE_LIMIT, overflow), daemon=True
        ),
        threading.Thread(
            target=_drain, args=(process.stderr, stderr, CAPTURE_LIMIT, overflow), daemon=True
        ),
    ]
    for thread in threads:
        thread.start()
    try:
        process.stdin.write(input_bytes)
        process.stdin.close()
        process.wait(timeout=timeout)
    except (BrokenPipeError, subprocess.TimeoutExpired) as error:
        try:
            os.killpg(process.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        process.wait(timeout=5)
        if isinstance(error, subprocess.TimeoutExpired):
            raise AcceptanceError("bounded candidate command timed out") from error
    finally:
        for thread in threads:
            thread.join(timeout=3)
    if overflow.is_set():
        raise AcceptanceError("candidate command capture exceeded 256 KiB")
    return CommandResult(bytes(stdout), bytes(stderr), process.returncode)


def _reserve_port(requested: int) -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 0)
        try:
            sock.bind(("127.0.0.1", requested))
        except OSError as error:
            raise PreflightError("requested loopback listen port is unavailable") from error
        return int(sock.getsockname()[1])


def _write_private(path: Path, data: bytes) -> None:
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    try:
        os.write(descriptor, data)
        os.fsync(descriptor)
    finally:
        os.close(descriptor)


class AcceptanceRun:
    def __init__(self, prepared: Prepared):
        self.prepared = prepared
        self.workspace: OwnedWorkspace | None = None
        self.child: OwnedChild | None = None
        self.captures: list[bytes] = []
        self.log_capture = bytearray()
        self.log_overflow = threading.Event()
        self.log_thread: threading.Thread | None = None
        self.log_limit = LOG_LIMIT
        self.schedule_id = ""
        self.candidate = Path()
        self.config = Path()
        self.database = Path()
        self.env: dict[str, str] = {}
        self.password = secrets.token_urlsafe(32)
        self.summary: dict[str, object] = {
            "ok": False,
            "candidate_sha256": prepared.candidate_sha256,
            "isolation": {
                "backend": "fake",
                "host_install_changes": False,
                "original_deployment_read": False,
            },
            "telegram": {
                "requested": prepared.real_telegram,
                "verified": False,
                "encrypted_archive_sends": 0,
            },
        }

    def _make_workspace(self) -> None:
        if self.prepared.work_dir:
            self.workspace = OwnedWorkspace.create(self.prepared.work_dir)
        else:
            self.workspace = OwnedWorkspace.temporary(self.prepared.work_parent)
        root = self.workspace.path
        for name in ("data", "tmp", "home"):
            (root / name).mkdir(mode=0o700)
        self._pin_candidate(root / "candidate")
        self.config = root / "wg-guard.toml"
        self.database = root / "data/wg-guard.db"
        port = _reserve_port(self.prepared.listen_port)
        config = "\n".join(
            [
                f"data_dir = {json.dumps(str(root / 'data'))}",
                f"database_path = {json.dumps(str(self.database))}",
                f"master_key_file = {json.dumps(str(root / 'data/master.key'))}",
                f"http_listen = {json.dumps('127.0.0.1:' + str(port))}",
                "",
                "[tls]",
                'mode = "dev"',
                "",
                "[log]",
                'level = "warn"',
                'format = "json"',
                "",
                "[metrics]",
                "enabled = false",
                "",
            ]
        )
        _write_private(self.config, config.encode())
        inherited_path = os.environ.get("PATH", "/usr/bin:/bin")
        self.env = {
            "PATH": inherited_path,
            "HOME": str(root / "home"),
            "TMPDIR": str(root / "tmp"),
            "LANG": "C.UTF-8",
            "LC_ALL": "C.UTF-8",
            "WGG_IN_CONTAINER": "1",
        }
        self.summary["listen"] = "loopback-ephemeral" if self.prepared.listen_port == 0 else "loopback-explicit"

    def _pin_candidate(self, destination: Path) -> None:
        source, _ = _open_regular(self.prepared.candidate, "candidate", executable=True)
        output = os.open(destination, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o700)
        digest = hashlib.sha256()
        try:
            while True:
                chunk = os.read(source, 1024 * 1024)
                if not chunk:
                    break
                digest.update(chunk)
                view = memoryview(chunk)
                while view:
                    written = os.write(output, view)
                    view = view[written:]
            os.fchmod(output, 0o700)
            os.fsync(output)
        finally:
            os.close(source)
            os.close(output)
        if digest.hexdigest() != self.prepared.candidate_sha256:
            destination.unlink()
            raise PreflightError("candidate SHA-256 mismatch while pinning private copy")
        self.candidate = destination

    def _command(self, argv: Sequence[str], label: str, input_text: str = "") -> CommandResult:
        result = run_bounded(argv, env=self.env, input_bytes=input_text.encode())
        self.captures.extend([result.stdout, result.stderr])
        if result.returncode != 0:
            raise AcceptanceError(f"{label} failed with exit status {result.returncode}")
        return result

    def _candidate_command(
        self, args: Sequence[str], label: str, input_text: str = ""
    ) -> CommandResult:
        return self._command([str(self.candidate), *args], label, input_text)

    def _backup(self, command: str, *args: str, label: str) -> CommandResult:
        return self._candidate_command(
            [
                "backup",
                command,
                *args,
                "--config",
                str(self.config),
            ],
            label,
        )

    def _setting(self, key: str, value: str) -> None:
        self._candidate_command(
            [
                "settings",
                "set",
                key,
                "-stdin",
                "--config",
                str(self.config),
            ],
            "settings update",
            value + "\n",
        )

    def _start_service_process(self, argv: Sequence[str]) -> None:
        self.child = OwnedChild.start(
            argv,
            env=self.env,
            stdin=subprocess.DEVNULL,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
        )
        assert self.child.process.stdout is not None
        self.log_thread = threading.Thread(
            target=_drain,
            args=(
                self.child.process.stdout,
                self.log_capture,
                self.log_limit,
                self.log_overflow,
                self.child.stop_for_overflow,
            ),
            daemon=True,
        )
        self.log_thread.start()

    def _start_node(self) -> None:
        self._start_service_process(
            [
                str(self.candidate),
                "serve",
                "--config",
                str(self.config),
                "--backend",
                "fake",
            ]
        )
        deadline = time.monotonic() + READY_TIMEOUT
        listen = _config_listen(self.config)
        while time.monotonic() < deadline:
            if self.child.process.poll() is not None:
                raise AcceptanceError("owned candidate child exited before readiness")
            try:
                with urlopen(f"http://{listen}/readyz", timeout=2) as response:
                    if response.status == 200:
                        return
            except (OSError, URLError):
                pass
            time.sleep(0.25)
        raise AcceptanceError("owned candidate child did not become ready within 30 seconds")

    def _verify_archive(self, path: Path) -> dict[str, object]:
        info = path.lstat()
        if not stat.S_ISREG(info.st_mode) or path.is_symlink():
            raise AcceptanceError("archive is not a regular non-symlink file")
        if stat.S_IMODE(info.st_mode) & 0o077:
            raise AcceptanceError("archive is not private")
        with path.open("rb") as source:
            if source.read(6) != b"age-en":
                raise AcceptanceError("archive is not age-encrypted")
        return {
            "name": path.name,
            "sha256": _hash_file(path),
            "size": info.st_size,
            "mode": format(stat.S_IMODE(info.st_mode), "04o"),
            "encrypted": True,
        }

    def _create_manual_archives(self) -> Path:
        backups = self.workspace.path / "data/backups"  # type: ignore[union-attr]
        names: list[str] = []
        for index in range(3):
            result = self._backup(
                "create", "--reason", f"phase81-synthetic-{index}", label="encrypted backup create"
            )
            match = re.search(rb"(?m)^created ([^\s]+\.wgg) ", result.stdout)
            if not match:
                raise AcceptanceError("backup create did not return its stable archive name")
            names.append(match.group(1).decode("ascii"))
        paths = sorted(backups.glob("wg-guard-*.wgg"))
        if len(paths) != 2 or (backups / names[0]).exists():
            raise AcceptanceError("retention did not keep exactly the two newest archives")
        details = [self._verify_archive(path) for path in paths]
        listed = self._backup("list", label="backup list")
        text = listed.stdout.decode("utf-8", errors="strict")
        if any(path.name not in text for path in paths) or text.count("age=true") < 2:
            raise AcceptanceError("backup list did not report both encrypted retained archives")
        self.summary["manual_backups"] = {
            "created": 3,
            "retained": len(paths),
            "retention_count": 2,
            "archives": details,
        }
        return backups / names[-1]

    def _exercise_telegram(self, selected: Path) -> None:
        if not self.prepared.real_telegram:
            return
        assert self.prepared.credentials is not None
        self._setting("backup.telegram_token", self.prepared.credentials.token)
        self._setting("backup.telegram_chat", self.prepared.credentials.chat_id)
        probe = self._backup("telegram-test", label="Telegram test delivery")
        if b"Telegram" not in probe.stdout and b"telegram" not in probe.stdout:
            raise AcceptanceError("Telegram test delivery lacked confirmation")
        sent = self._backup("send", "--archive", str(selected), label="selected archive delivery")
        if b"telegram" not in sent.stdout.lower():
            raise AcceptanceError("selected archive delivery lacked Telegram confirmation")
        telegram = self.summary["telegram"]
        assert isinstance(telegram, dict)
        telegram["probe_delivered"] = True
        telegram["selected_archive_delivered"] = True
        telegram["encrypted_archive_sends"] = 1

    def _schedule_row(self) -> sqlite3.Row:
        connection = sqlite3.connect(self.database, timeout=5)
        connection.row_factory = sqlite3.Row
        try:
            row = connection.execute(
                "SELECT * FROM backup_schedules WHERE id = ?", (self.schedule_id,)
            ).fetchone()
        finally:
            connection.close()
        if row is None:
            raise AcceptanceError("owned schedule row is missing")
        return row

    def _exercise_schedule(self) -> None:
        name = "phase81-synthetic-" + self.workspace.path.name[-8:]  # type: ignore[union-attr]
        created = self._backup(
            "schedule-add",
            "--name",
            name,
            "--hours",
            "1",
            "--retention",
            "1",
            label="schedule create",
        )
        match = SCHEDULE_ID_RE.search(created.stdout.decode("utf-8", errors="strict"))
        if not match:
            raise AcceptanceError("schedule create did not return an owned identifier")
        self.schedule_id = match.group(1)
        row = self._schedule_row()
        if row["name"] != name or row["kind"] != "interval" or row["interval_hours"] != 1:
            raise AcceptanceError("created schedule definition differs from requested values")
        _require_utc(row["next_run_at"])

        self._backup(
            "schedule-update",
            "--id",
            self.schedule_id,
            "--name",
            name,
            "--days",
            "2",
            "--retention",
            "1",
            label="schedule update",
        )
        row = self._schedule_row()
        if row["kind"] != "interval" or row["interval_hours"] != 48 or row["retention_count"] != 1:
            raise AcceptanceError("updated schedule lost days or retention values")

        self._backup("schedule-disable", "--id", self.schedule_id, label="schedule disable")
        if self._schedule_row()["enabled"] != 0:
            raise AcceptanceError("schedule disable was not persisted")
        self._backup("schedule-enable", "--id", self.schedule_id, label="schedule enable")
        if self._schedule_row()["enabled"] != 1:
            raise AcceptanceError("schedule enable was not persisted")
        listed = self._backup("schedule-list", label="schedule list")
        listing = listed.stdout.decode("utf-8", errors="strict")
        if self.schedule_id not in listing or "UTC" not in listing:
            raise AcceptanceError("schedule listing omitted identifier or UTC time")

        due = (datetime.now(timezone.utc) - timedelta(minutes=1)).isoformat().replace("+00:00", "Z")
        connection = sqlite3.connect(self.database, timeout=5)
        try:
            changed = connection.execute(
                "UPDATE backup_schedules SET next_run_at = ? WHERE id = ? AND name = ?",
                (due, self.schedule_id, name),
            ).rowcount
            connection.commit()
        finally:
            connection.close()
        if changed != 1:
            raise AcceptanceError("refused to accelerate a non-owned schedule row")

        audit: tuple[str, str] | None = None
        deadline = time.monotonic() + SCHEDULER_TIMEOUT
        while time.monotonic() < deadline:
            self._check_child_and_log()
            connection = sqlite3.connect(self.database, timeout=5)
            try:
                row = connection.execute(
                    "SELECT last_run_at,last_status,next_run_at FROM backup_schedules WHERE id=?",
                    (self.schedule_id,),
                ).fetchone()
                records = connection.execute(
                    "SELECT target,metadata FROM audit_log WHERE action='backup.created' ORDER BY id DESC"
                ).fetchall()
            finally:
                connection.close()
            if row and row[0] and row[1] in ("ok", "failed"):
                for target, metadata in records:
                    try:
                        parsed = json.loads(metadata)
                    except json.JSONDecodeError:
                        continue
                    if parsed.get("reason") == "schedule:" + self.schedule_id:
                        audit = (target, metadata)
                        break
                if audit:
                    break
            time.sleep(1)
        if not audit or not row or row[1] != "ok":
            raise AcceptanceError("production scheduler did not complete the accelerated due run")
        _require_utc(row[0])
        _require_utc(row[2])
        metadata = json.loads(audit[1])
        archive = self.workspace.path / "data/backups" / audit[0]  # type: ignore[union-attr]
        details = self._verify_archive(archive)
        retained = list((self.workspace.path / "data/backups").glob("wg-guard-*.wgg"))  # type: ignore[union-attr]
        if len(retained) != 1:
            raise AcceptanceError("scheduled retention did not keep exactly one archive")
        telegram = self.summary["telegram"]
        assert isinstance(telegram, dict)
        delivered = metadata.get("delivered", [])
        if self.prepared.real_telegram:
            if "telegram" not in delivered:
                raise AcceptanceError("scheduled archive lacks successful Telegram delivery evidence")
            telegram["scheduled_archive_delivered"] = True
            telegram["encrypted_archive_sends"] = 2
            telegram["verified"] = True
        self.summary["schedule"] = {
            "crud": True,
            "utc_display_and_storage": True,
            "accelerated_due_execution": True,
            "elapsed_hours_claimed": False,
            "last_status": row[1],
            "retention_count": 1,
            "archive": details,
        }

        self._backup("schedule-disable", "--id", self.schedule_id, label="final schedule disable")
        self._backup("schedule-delete", "--id", self.schedule_id, label="schedule delete")
        self.schedule_id = ""

    def _check_child_and_log(self) -> None:
        if self.log_overflow.is_set():
            raise AcceptanceError("owned candidate log exceeded 1 MiB")
        if self.child is None or self.child.process.poll() is not None:
            raise AcceptanceError("owned candidate child exited during acceptance")

    def execute(self) -> dict[str, object]:
        self._make_workspace()
        version = self._candidate_command(["version"], "candidate version")
        self.summary["candidate_version"] = version.stdout.decode("utf-8", errors="strict").strip()
        self._start_node()
        self._setting("backup.password", self.password)
        self._setting("backup.retention_count", "2")
        selected = self._create_manual_archives()
        self._exercise_telegram(selected)
        self._exercise_schedule()
        self.summary["ok"] = True
        return self.summary

    def cleanup(self) -> None:
        cleanup_errors: list[AcceptanceError] = []
        if self.schedule_id and self.child and self.child.process.poll() is None:
            for command in ("schedule-disable", "schedule-delete"):
                try:
                    self._backup(command, "--id", self.schedule_id, label="owned schedule cleanup")
                except Exception:
                    pass
            self.schedule_id = ""
        if self.child:
            try:
                self.child.stop()
            except (OSError, subprocess.SubprocessError):
                cleanup_errors.append(AcceptanceError("could not stop owned candidate child"))
        if self.log_thread:
            self.log_thread.join(timeout=3)
            if self.log_thread.is_alive():
                cleanup_errors.append(AcceptanceError("owned candidate log drain did not stop"))
        self.captures.append(bytes(self.log_capture))
        if self.log_overflow.is_set():
            cleanup_errors.append(AcceptanceError("owned candidate log exceeded 1 MiB"))
        private_values = [self.password]
        if self.prepared.credentials:
            private_values.extend([self.prepared.credentials.token, self.prepared.credentials.chat_id])
        leak: SecretLeakError | None = None
        try:
            assert_secrets_absent(self.captures, private_values)
        except SecretLeakError as error:
            leak = error
        if self.workspace:
            try:
                self.workspace.cleanup()
            except (AcceptanceError, OSError) as error:
                cleanup_errors.append(
                    error
                    if isinstance(error, AcceptanceError)
                    else AcceptanceError("could not remove owned work directory")
                )
        self.summary["cleanup"] = {
            "owned_child_stopped": self.child is None or self.child.process.poll() is not None,
            "owned_workspace_removed": self.workspace is None or not self.workspace.path.exists(),
            "credential_file_preserved": (
                self.prepared.credentials_file is None or self.prepared.credentials_file.exists()
            ),
        }
        if leak:
            raise leak
        if cleanup_errors:
            raise cleanup_errors[0]

def _config_listen(path: Path) -> str:
    for line in path.read_text().splitlines():
        if line.startswith("http_listen = "):
            return json.loads(line.split("=", 1)[1].strip())
    raise AcceptanceError("private config omitted loopback listener")


def _require_utc(value: str) -> None:
    try:
        parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
    except (AttributeError, ValueError) as error:
        raise AcceptanceError("schedule timestamp is not RFC3339") from error
    if parsed.utcoffset() != timedelta(0):
        raise AcceptanceError("schedule timestamp is not UTC")


def _write_result(path: Path, report: dict[str, object]) -> None:
    payload = (json.dumps(report, indent=2, sort_keys=True) + "\n").encode()
    _write_private(path, payload)


def main(argv: Sequence[str] | None = None) -> int:
    report: dict[str, object] = {"ok": False}
    prepared: Prepared | None = None
    run: AcceptanceRun | None = None
    try:
        prepared = prepare(build_parser().parse_args(argv))
        run = AcceptanceRun(prepared)
        report = run.execute()
    except (PreflightError, AcceptanceError, OSError, sqlite3.Error, UnicodeError) as error:
        report = {
            "ok": False,
            "error_type": type(error).__name__,
            "error": str(error),
        }
        if prepared:
            report["candidate_sha256"] = prepared.candidate_sha256
            report["telegram_requested"] = prepared.real_telegram
    finally:
        if run:
            try:
                run.cleanup()
                report["cleanup"] = run.summary.get("cleanup", {})
            except (AcceptanceError, OSError) as cleanup_error:
                report = {
                    "ok": False,
                    "error_type": type(cleanup_error).__name__,
                    "error": str(cleanup_error),
                    "candidate_sha256": prepared.candidate_sha256 if prepared else "",
                }
    if prepared and prepared.result:
        try:
            _write_result(prepared.result, report)
        except OSError:
            report = {
                "ok": False,
                "error_type": "ResultWriteError",
                "error": "could not write the collision-checked private result file",
                "candidate_sha256": prepared.candidate_sha256,
            }
    print(json.dumps(report, sort_keys=True))
    return 0 if report.get("ok") else 1


if __name__ == "__main__":
    raise SystemExit(main())
