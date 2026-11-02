#!/usr/bin/env python3
"""Conservative release runner for the standalone JDLG main process."""
from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import shutil
import signal
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request
from pathlib import Path


DEFAULT_BINARY = Path("/www/wwwroot/jiadilingguang/service/bin/applinux")
DEFAULT_BASE_ENV = DEFAULT_BINARY.parent / ".env"
DEFAULT_LOCAL_ENV = DEFAULT_BINARY.parent / ".env.local"
DEFAULT_POINTER = Path("/root/jdlg-release-current.json")
STATE_NAME = "release-state.json"
RUNTIME_ENV_NAME = "runtime-env.json"
MIGRATIONS = (
    "phase3_notifications_up.sql",
    "phase3_content_import_up.sql",
    "phase3_official_bindings_up.sql",
    "phase3_share_actor_index_up.sql",
)
MIGRATION_PATHS = tuple(f"migrations/{name}" for name in MIGRATIONS)
REQUIRED_TABLES = (
    "mini_notification_event_jobs",
    "mini_notifications",
    "mini_notification_deliveries",
    "mini_notification_provider_receipts",
    "content_import_jobs",
)
FLAGS = {
    "CONTENT_WORKBENCH_ENABLED": "true",
    "PROJECT_NOTES_ENABLED": "true",
    "NOTIFICATIONS_ENABLED": "true",
    "BROWSE_NOTIFICATIONS_ENABLED": "true",
    "CONTENT_MEDIA_STORAGE": "cos",
    "CONTENT_MEDIA_COS_PREFIX": "jd-content-private",
    "CONTENT_IMPORT_ENABLED": "false",
    "OFFICIAL_ACCOUNT_ENABLED": "false",
    "OFFICIAL_ACCOUNT_DELIVERY_ENABLED": "false",
    "SHARE_WEB_ENABLED": "true",
    "PUBLIC_SHARE_ORIGIN": "https://www.jiadilingguang.com",
}


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def environment_bytes(environment: dict[str, str]) -> bytes:
    normalized = {str(key): str(value) for key, value in environment.items()}
    return (json.dumps(normalized, ensure_ascii=False, sort_keys=True,
                       separators=(",", ":")) + "\n").encode("utf-8")


def environment_sha256(environment: dict[str, str]) -> str:
    return hashlib.sha256(environment_bytes(environment)).hexdigest()


def atomic_write_bytes(path: Path, content: bytes, mode: int = 0o600) -> None:
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    descriptor, temporary = tempfile.mkstemp(prefix=f".{path.name}.", dir=path.parent)
    try:
        os.fchmod(descriptor, mode)
        with os.fdopen(descriptor, "wb") as stream:
            stream.write(content)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, path)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def atomic_write_json(path: Path, value: dict, mode: int = 0o600) -> None:
    content = (json.dumps(value, ensure_ascii=False, sort_keys=True, indent=2) + "\n").encode("utf-8")
    atomic_write_bytes(path, content, mode)


def read_manifest(path: Path) -> dict:
    manifest = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(manifest, dict) or not isinstance(manifest.get("files"), list):
        raise ValueError("manifest must contain files[]")
    return manifest


def verify_manifest(manifest: dict, package: Path) -> Path:
    if not manifest["files"]:
        raise ValueError("manifest files[] must not be empty")
    seen = set()
    root = package.resolve()
    for item in manifest["files"]:
        if not isinstance(item, dict):
            raise ValueError("manifest entry must be an object")
        relative = item.get("path")
        expected = item.get("sha256")
        if not isinstance(relative, str) or not re.fullmatch(r"[0-9a-f]{64}", str(expected)):
            raise ValueError("manifest entry is incomplete")
        target = (root / relative).resolve()
        if target != root and root not in target.parents:
            raise ValueError("manifest path escapes package root")
        if relative in seen or not target.is_file() or sha256(target) != expected:
            raise ValueError(f"package checksum mismatch: {relative}")
        seen.add(relative)
    if "applinux" not in seen:
        raise ValueError("manifest must include root applinux")
    missing = set(MIGRATION_PATHS) - seen
    if missing:
        raise ValueError("manifest is missing audited migrations: " + ", ".join(sorted(missing)))
    candidate = root / "applinux"
    if not os.access(candidate, os.X_OK):
        raise ValueError("candidate applinux is not executable")
    if (root / ".env.local").exists():
        raise ValueError("candidate package must not provide an implicit .env.local")
    return candidate


def process_environment(pid: int) -> dict[str, str]:
    raw = Path(f"/proc/{pid}/environ").read_bytes().decode("utf-8")
    return dict(part.split("=", 1) for part in raw.split("\0") if "=" in part)


def process_identity(pid: int) -> dict:
    proc = Path(f"/proc/{pid}")
    if not proc.exists():
        raise RuntimeError("application process disappeared")
    fields = (proc / "stat").read_text(encoding="utf-8").split()
    return {
        "pid": pid,
        "start_time": fields[21],
        "exe": str(Path(os.readlink(proc / "exe")).resolve()),
        "cwd": str(Path(os.readlink(proc / "cwd")).resolve()),
    }


def find_processes(expected_binary: Path = DEFAULT_BINARY) -> list[dict]:
    matches = []
    for entry in Path("/proc").iterdir():
        if not entry.name.isdigit():
            continue
        try:
            if Path(os.readlink(entry / "exe")).resolve() == expected_binary.resolve():
                matches.append(process_identity(int(entry.name)))
        except (FileNotFoundError, PermissionError, ProcessLookupError):
            continue
    return matches


def find_process(expected_binary: Path = DEFAULT_BINARY) -> dict:
    matches = find_processes(expected_binary)
    if len(matches) != 1:
        raise RuntimeError(f"expected exactly one application process, found {len(matches)}")
    if matches[0]["cwd"] != str(expected_binary.resolve().parent):
        raise RuntimeError("application working directory mismatch")
    return matches[0]


def same_process(expected: dict) -> bool:
    try:
        actual = process_identity(int(expected["pid"]))
    except (OSError, RuntimeError, ValueError):
        return False
    return all(actual[key] == expected[key] for key in ("pid", "start_time", "exe", "cwd"))


def save_state(backup: Path, state: dict) -> None:
    atomic_write_json(backup / STATE_NAME, state)


def load_runtime_environment(backup: Path, expected_hash: str) -> dict[str, str]:
    path = backup / RUNTIME_ENV_NAME
    if not path.is_file() or sha256(path) != expected_hash:
        raise RuntimeError("prepared runtime environment snapshot is invalid")
    value = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(value, dict) or not all(isinstance(key, str) and isinstance(item, str)
                                              for key, item in value.items()):
        raise RuntimeError("prepared runtime environment snapshot has invalid data")
    if environment_sha256(value) != expected_hash:
        raise RuntimeError("prepared runtime environment snapshot is not canonical")
    return value


def load_state(backup: Path) -> dict:
    path = backup / STATE_NAME
    if not path.is_file():
        raise RuntimeError("release was not prepared")
    state = json.loads(path.read_text(encoding="utf-8"))
    if not state.get("prepared"):
        raise RuntimeError("release backup is incomplete")
    required = {
        "applinux.previous": state.get("binary_sha256"),
        "service.env.previous": state.get("base_env_sha256"),
        "service.env.local.previous": state.get("local_env_sha256"),
        "manifest.json": state.get("manifest_sha256"),
        RUNTIME_ENV_NAME: state.get("runtime_env_sha256"),
    }
    for name, expected in required.items():
        candidate = backup / name
        if not expected or not candidate.is_file() or sha256(candidate) != expected:
            raise RuntimeError(f"prepared backup is invalid: {name}")
    dump = backup / "database.sql"
    if not dump.is_file() or dump.stat().st_size == 0:
        raise RuntimeError("prepared database backup is missing")
    load_runtime_environment(backup, state["runtime_env_sha256"])
    return state


def parse_mysql_dsn(dsn: str, executable: str) -> tuple[list[str], dict[str, str]]:
    match = re.fullmatch(r"([^:]+):(.*?)@tcp\(([^:()]+):(\d+)\)/([^?]+)(?:\?.*)?", dsn)
    if not match:
        raise RuntimeError("MYSQL_DSN has unsupported format")
    user, password, host, port, database = match.groups()
    command = [executable, "--host", host, "--port", port, "--user", user, database]
    environment = os.environ.copy()
    environment["MYSQL_PWD"] = password
    return command, environment


def verify_live_prepared(state: dict, backup: Path) -> dict[str, str]:
    if not same_process(state["process"]):
        raise RuntimeError("application process changed after prepare")
    current_files = (
        (Path(state["binary"]), state["binary_sha256"]),
        (Path(state["base_env"]), state["base_env_sha256"]),
        (Path(state["local_env"]), state["local_env_sha256"]),
    )
    if any(not path.is_file() or sha256(path) != expected for path, expected in current_files):
        raise RuntimeError("production inputs changed after prepare")
    snapshot = load_runtime_environment(backup, state["runtime_env_sha256"])
    running = process_environment(int(state["process"]["pid"]))
    if environment_sha256(running) != state["runtime_env_sha256"] or running != snapshot:
        raise RuntimeError("application runtime environment changed after prepare")
    return snapshot


def prepare_release(package: Path, manifest_path: Path, backup: Path,
                    binary: Path = DEFAULT_BINARY, base_env: Path = DEFAULT_BASE_ENV,
                    local_env: Path = DEFAULT_LOCAL_ENV) -> dict:
    candidate = verify_manifest(read_manifest(manifest_path), package)
    current = find_process(binary)
    runtime = process_environment(current["pid"])
    if "MYSQL_DSN" not in runtime:
        raise RuntimeError("running application has no MYSQL_DSN")
    if "SERVER_PORT" in runtime and runtime["SERVER_PORT"] != "8080":
        raise RuntimeError("running application SERVER_PORT is not 8080")
    if backup.exists() and any(backup.iterdir()):
        raise RuntimeError("backup directory is not empty")
    backup.mkdir(parents=True, mode=0o700)
    os.chmod(backup, 0o700)
    backup_sources = (
        (binary, "applinux.previous"),
        (base_env, "service.env.previous"),
        (local_env, "service.env.local.previous"),
        (manifest_path, "manifest.json"),
    )
    for source, name in backup_sources:
        if not source.is_file():
            raise RuntimeError(f"required release input is missing: {source}")
        shutil.copy2(source, backup / name)
    atomic_write_bytes(backup / RUNTIME_ENV_NAME, environment_bytes(runtime), 0o600)
    dump_command, dump_environment = parse_mysql_dsn(runtime["MYSQL_DSN"], "mysqldump")
    dump_command[1:1] = ["--single-transaction", "--routines", "--events", "--triggers", "--hex-blob"]
    database_dump = backup / "database.sql"
    with database_dump.open("wb") as output:
        result = subprocess.run(dump_command, stdout=output, stderr=subprocess.DEVNULL,
                                env=dump_environment, timeout=1800, check=False)
    os.chmod(database_dump, 0o600)
    if result.returncode or database_dump.stat().st_size == 0:
        raise RuntimeError("database backup failed")
    if not same_process(current):
        raise RuntimeError("application process changed during prepare")
    for source, name in backup_sources:
        if sha256(source) != sha256(backup / name):
            raise RuntimeError(f"release input changed during prepare: {source}")
    running_after = process_environment(current["pid"])
    if running_after != runtime:
        raise RuntimeError("application runtime environment changed during prepare")
    candidate = verify_manifest(read_manifest(manifest_path), package)
    state = {
        "prepared": True,
        "migrated": False,
        "activated": False,
        "process": current,
        "binary": str(binary),
        "base_env": str(base_env),
        "local_env": str(local_env),
        "candidate": str(candidate),
        "package": str(package.resolve()),
        "binary_sha256": sha256(backup / "applinux.previous"),
        "base_env_sha256": sha256(backup / "service.env.previous"),
        "local_env_sha256": sha256(backup / "service.env.local.previous"),
        "manifest_sha256": sha256(backup / "manifest.json"),
        "runtime_env_sha256": sha256(backup / RUNTIME_ENV_NAME),
        "candidate_sha256": sha256(candidate),
    }
    save_state(backup, state)
    return state


def run_private(command: list[str], environment: dict[str, str], log: Path,
                timeout: int = 300, cwd: Path | None = None) -> None:
    with log.open("ab") as output:
        result = subprocess.run(command, stdout=output, stderr=output, env=environment,
                                cwd=cwd, timeout=timeout, check=False)
    if result.returncode:
        raise RuntimeError(f"release command failed; see private log {log}")


def official_tables(path: Path) -> tuple[str, ...]:
    sql = path.read_text(encoding="utf-8")
    names = tuple(re.findall(r"(?i)CREATE\s+TABLE\s+IF\s+NOT\s+EXISTS\s+`?([a-z0-9_]+)`?", sql))
    if not names:
        raise RuntimeError("official-account migration contains no tables")
    return names


def verify_schema(environment: dict[str, str], package: Path, log: Path) -> None:
    tables = REQUIRED_TABLES + official_tables(package / "migrations/phase3_official_bindings_up.sql")
    quoted = ",".join("'" + table + "'" for table in tables)
    query = (
        "SELECT COUNT(DISTINCT table_name) FROM information_schema.tables "
        f"WHERE table_schema=DATABASE() AND table_name IN ({quoted});"
        "SELECT COUNT(DISTINCT index_name) FROM information_schema.statistics "
        "WHERE table_schema=DATABASE() AND table_name='tracking_records' "
        "AND index_name='tracking_share_actor_page';"
        "SELECT COUNT(*) FROM tracking_records WHERE kind='share' AND visitor_key='';"
    )
    command, mysql_environment = parse_mysql_dsn(environment.get("MYSQL_DSN", ""), "mysql")
    command[1:1] = ["--batch", "--skip-column-names"]
    command[-1:-1] = ["--execute", query]
    with log.open("ab") as errors:
        result = subprocess.run(command, stdout=subprocess.PIPE, stderr=errors,
                                env=mysql_environment, text=True, timeout=120, check=False)
    if result.returncode:
        raise RuntimeError("post-migration schema verification failed; see private log")
    try:
        values = [int(value) for value in result.stdout.splitlines()]
    except ValueError:
        raise RuntimeError("post-migration schema verification returned invalid output") from None
    if values != [len(set(tables)), 1, 0]:
        raise RuntimeError("post-migration schema verification did not satisfy release invariants")


def migrate_release(backup: Path) -> None:
    state = load_state(backup)
    if state.get("activated"):
        raise RuntimeError("cannot migrate an activated release")
    runtime = verify_live_prepared(state, backup)
    package = Path(state["package"])
    candidate = verify_manifest(read_manifest(backup / "manifest.json"), package)
    if candidate != Path(state["candidate"]) or sha256(candidate) != state["candidate_sha256"]:
        raise RuntimeError("candidate binary changed after prepare")
    for migration in MIGRATIONS:
        run_private([str(candidate), "migrate", migration], runtime, backup / "migration.log",
                    cwd=backup)
    verify_schema(runtime, package, backup / "schema-verification.log")
    state["migrated"] = True
    save_state(backup, state)


def stop_process(identity: dict, timeout: float = 20.0) -> None:
    if not same_process(identity):
        raise RuntimeError("application process changed after prepare")
    os.kill(int(identity["pid"]), signal.SIGTERM)
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if not same_process(identity):
            return
        time.sleep(0.2)
    raise RuntimeError("application did not stop after SIGTERM; no SIGKILL was sent")


def install_binary(candidate: Path, target: Path) -> None:
    temporary = target.with_name(f".{target.name}.phase3")
    shutil.copy2(candidate, temporary)
    os.chmod(temporary, 0o755)
    with temporary.open("rb") as stream:
        os.fsync(stream.fileno())
    os.replace(temporary, target)


def start_application(binary: Path, environment: dict[str, str], log: Path) -> subprocess.Popen:
    output = log.open("ab")
    try:
        return subprocess.Popen([str(binary)], cwd=binary.parent, env=environment,
                                stdin=subprocess.DEVNULL, stdout=output, stderr=output,
                                start_new_session=True, close_fds=True)
    finally:
        output.close()


def health_check(url: str, expected_status: int, timeout: float = 3.0) -> bool:
    try:
        with urllib.request.urlopen(url, timeout=timeout) as response:
            return response.status == expected_status
    except urllib.error.HTTPError as error:
        return error.code == expected_status
    except (OSError, urllib.error.URLError):
        return False


def wait_healthy(binary: Path = DEFAULT_BINARY, timeout: float = 30.0) -> dict:
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        try:
            identity = find_process(binary)
        except RuntimeError:
            time.sleep(0.5)
            continue
        tracking_ok = health_check(
            "http://127.0.0.1:8080/api/v1/miniprogram/tracking/config", 200
        )
        workbench_ok = health_check(
            "http://127.0.0.1:8080/api/v1/miniprogram/workbench/context", 401
        )
        if tracking_ok and workbench_ok:
            return identity
        time.sleep(0.5)
    raise RuntimeError("application health checks did not pass")


def environment_with_flags(runtime: dict[str, str]) -> dict[str, str]:
    environment = dict(runtime)
    environment.update(FLAGS)
    return environment


def restore_release(backup: Path, state: dict, new_process: dict | None = None) -> None:
    if new_process and same_process(new_process):
        stop_process(new_process)
    binary = Path(state["binary"])
    install_binary(backup / "applinux.previous", binary)
    runtime = load_runtime_environment(backup, state["runtime_env_sha256"])
    start_application(binary, runtime, backup / "rollback-start.log")
    wait_healthy(binary)


def write_pointer(pointer: Path, backup: Path) -> None:
    atomic_write_json(pointer, {"backup": str(backup.resolve())}, 0o600)


def activate_release(backup: Path, pointer: Path = DEFAULT_POINTER) -> dict:
    state = load_state(backup)
    if not state.get("migrated"):
        raise RuntimeError("release migrations are not complete")
    if state.get("activated"):
        raise RuntimeError("release is already activated")
    runtime = verify_live_prepared(state, backup)
    candidate = Path(state["candidate"])
    if sha256(candidate) != state["candidate_sha256"]:
        raise RuntimeError("candidate binary changed after prepare")
    binary = Path(state["binary"])
    stopped = False
    started = None
    new_identity = None
    try:
        stop_process(state["process"])
        stopped = True
        install_binary(candidate, binary)
        started = start_application(binary, environment_with_flags(runtime), backup / "release-start.log")
        new_identity = wait_healthy(binary)
    except Exception:
        if stopped:
            if new_identity is None and started is not None:
                try:
                    new_identity = process_identity(started.pid)
                except (OSError, RuntimeError):
                    pass
            restore_release(backup, state, new_identity)
        raise
    state.update({
        "activated": True,
        "new_process": new_identity,
        "new_binary_sha256": sha256(binary),
    })
    save_state(backup, state)
    write_pointer(pointer, backup)
    return state


def backup_from_pointer(pointer: Path) -> Path:
    value = json.loads(pointer.read_text(encoding="utf-8"))
    if not isinstance(value, dict) or not isinstance(value.get("backup"), str):
        raise RuntimeError("current release pointer is invalid")
    return Path(value["backup"]).resolve()


def start_current(backup: Path, pointer: Path = DEFAULT_POINTER) -> dict:
    state = load_state(backup)
    if not state.get("activated"):
        raise RuntimeError("current release is not activated")
    binary = Path(state["binary"])
    if not binary.is_file() or sha256(binary) != state.get("new_binary_sha256"):
        raise RuntimeError("current release binary does not match activated state")
    processes = find_processes(binary)
    if len(processes) > 1:
        raise RuntimeError(f"expected at most one application process, found {len(processes)}")
    if processes:
        if processes[0]["cwd"] != str(binary.resolve().parent):
            raise RuntimeError("application working directory mismatch")
        return wait_healthy(binary)
    runtime = load_runtime_environment(backup, state["runtime_env_sha256"])
    started = start_application(binary, environment_with_flags(runtime), backup / "start-current.log")
    try:
        identity = wait_healthy(binary)
    except Exception:
        try:
            identity = process_identity(started.pid)
            if same_process(identity):
                stop_process(identity)
        except (OSError, RuntimeError):
            pass
        raise
    state["new_process"] = identity
    save_state(backup, state)
    write_pointer(pointer, backup)
    return identity


def verify_activated_release(state: dict, backup: Path) -> dict[str, str]:
    if not state.get("activated"):
        raise RuntimeError("release is not activated")
    new_process = state.get("new_process")
    if not isinstance(new_process, dict) or not same_process(new_process):
        raise RuntimeError("activated process changed after release")
    current_files = (
        (Path(state["binary"]), state.get("new_binary_sha256")),
        (Path(state["base_env"]), state["base_env_sha256"]),
        (Path(state["local_env"]), state["local_env_sha256"]),
        (Path(state["candidate"]), state["candidate_sha256"]),
    )
    if any(not expected or not path.is_file() or sha256(path) != expected
           for path, expected in current_files):
        raise RuntimeError("activated release inputs changed")
    runtime = load_runtime_environment(backup, state["runtime_env_sha256"])
    running = process_environment(int(new_process["pid"]))
    if running != environment_with_flags(runtime):
        raise RuntimeError("activated process environment changed")
    return runtime


def rollback_release(backup: Path, pointer: Path = DEFAULT_POINTER) -> dict:
    state = load_state(backup)
    runtime = verify_activated_release(state, backup)
    binary = Path(state["binary"])
    candidate = Path(state["candidate"])
    stopped = False
    started = None
    old_identity = None
    try:
        stop_process(state["new_process"])
        stopped = True
        install_binary(backup / "applinux.previous", binary)
        started = start_application(binary, runtime, backup / "rollback-manual-start.log")
        old_identity = wait_healthy(binary)
    except Exception:
        if stopped:
            if old_identity is None and started is not None:
                try:
                    old_identity = process_identity(started.pid)
                except (OSError, RuntimeError):
                    pass
            if old_identity and same_process(old_identity):
                stop_process(old_identity)
            install_binary(candidate, binary)
            start_application(binary, environment_with_flags(runtime),
                              backup / "rollback-recovery-start.log")
            wait_healthy(binary)
        raise
    state.update({
        "activated": False,
        "rolled_back": True,
        "rollback_process": old_identity,
        "process": old_identity,
    })
    save_state(backup, state)
    write_pointer(pointer, backup)
    return state


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Standalone main-process release runner; start-current can use a private pointer."
    )
    sub = parser.add_subparsers(dest="phase", required=True)
    prepare = sub.add_parser("prepare")
    prepare.add_argument("--package", required=True, type=Path)
    prepare.add_argument("--manifest", required=True, type=Path)
    prepare.add_argument("--backup", required=True, type=Path)
    migrate = sub.add_parser("migrate")
    migrate.add_argument("--backup", required=True, type=Path)
    activate = sub.add_parser("activate")
    activate.add_argument("--backup", required=True, type=Path)
    activate.add_argument("--pointer", type=Path, default=DEFAULT_POINTER)
    rollback = sub.add_parser(
        "rollback",
        help="Explicitly restore the prepared old binary and its exact captured runtime environment.",
    )
    rollback.add_argument("--backup", required=True, type=Path)
    rollback.add_argument("--pointer", type=Path, default=DEFAULT_POINTER)
    start = sub.add_parser(
        "start-current",
        help="Explicitly start the activated binary from its private runtime snapshot; never duplicates a live process.",
    )
    start.add_argument("--backup", type=Path)
    start.add_argument("--pointer", type=Path, default=DEFAULT_POINTER)
    args = parser.parse_args()
    if args.phase == "prepare":
        prepare_release(args.package.resolve(), args.manifest.resolve(), args.backup.resolve())
        print("prepare completed: live process, runtime snapshot and private backups verified")
    elif args.phase == "migrate":
        migrate_release(args.backup.resolve())
        print("migrate completed: additive migrations applied with captured runtime environment")
    elif args.phase == "activate":
        activate_release(args.backup.resolve(), args.pointer.resolve())
        print("activate completed: direct main-process health checks passed")
    elif args.phase == "rollback":
        rollback_release(args.backup.resolve(), args.pointer.resolve())
        print("rollback completed: original main process is healthy")
    else:
        backup = args.backup.resolve() if args.backup else backup_from_pointer(args.pointer.resolve())
        start_current(backup, args.pointer.resolve())
        print("start-current completed: activated main process is healthy")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (OSError, ValueError, RuntimeError, subprocess.SubprocessError) as error:
        print(f"phase3 release failed: {error}", file=sys.stderr)
        raise SystemExit(1)
