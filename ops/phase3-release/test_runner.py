import importlib.util
import json
import os
import sys
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace


TEST_DIRECTORY = str(Path(__file__).resolve().parent)
ORIGINAL_SYS_PATH = sys.path[:]
sys.path = [entry for entry in sys.path if str(Path(entry or ".").resolve()) != TEST_DIRECTORY]
from unittest import mock
sys.path = ORIGINAL_SYS_PATH

SPEC = importlib.util.spec_from_file_location("phase3_release_runner", Path(__file__).with_name("runner.py"))
runner = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(runner)


def write(path: Path, content: bytes, mode: int = 0o600) -> Path:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(content)
    os.chmod(path, mode)
    return path


class ReleaseRunnerTests(unittest.TestCase):
    def test_flags_are_main_only_and_use_real_names(self):
        environment = runner.environment_with_flags({"MYSQL_DSN": "dsn", "SESSION_SECRET": "live"})
        self.assertEqual(environment["SESSION_SECRET"], "live")
        self.assertEqual(environment["BROWSE_NOTIFICATIONS_ENABLED"], "true")
        self.assertEqual(environment["CONTENT_MEDIA_STORAGE"], "cos")
        self.assertEqual(environment["PUBLIC_SHARE_ORIGIN"], "https://www.jiadilingguang.com")
        self.assertNotIn("BROWSE_TRACKING_ENABLED", environment)
        self.assertNotIn("MEDIA_STORAGE", environment)
        self.assertFalse(any(key.startswith("CRM_") for key in environment))

    def test_missing_database_backup_refuses_activation_before_stop(self):
        with tempfile.TemporaryDirectory() as temporary:
            state = self.make_state(Path(temporary), migrated=True)
            backup = Path(temporary) / "backup"
            (backup / "database.sql").unlink()
            with mock.patch.object(runner, "stop_process") as stop:
                with self.assertRaisesRegex(RuntimeError, "database backup is missing"):
                    runner.activate_release(backup, Path(temporary) / "pointer.json")
                stop.assert_not_called()

    def test_prepare_privately_backs_up_runtime_and_both_environment_files(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            package = self.make_package(root)
            binary = write(root / "applinux", b"old-binary", 0o755)
            base = write(root / ".env", b"SERVER_PORT=18087\n")
            local = write(root / ".env.local", b"COS_SECRET_ID=stale\n")
            backup = root / "backup"
            identity = {"pid": 111, "start_time": "1", "exe": str(binary), "cwd": str(root)}

            def dump(command, stdout, **_kwargs):
                self.assertNotIn("secret", " ".join(command))
                stdout.write(b"database-backup\n")
                return SimpleNamespace(returncode=0)

            with mock.patch.object(runner, "find_process", return_value=identity), \
                    mock.patch.object(runner, "same_process", return_value=True), \
                    mock.patch.object(runner, "process_environment", return_value=self.runtime()), \
                    mock.patch.object(runner.subprocess, "run", side_effect=dump):
                state = runner.prepare_release(package, package / "manifest.json", backup,
                                               binary, base, local)
            self.assertEqual(backup.stat().st_mode & 0o777, 0o700)
            self.assertEqual((backup / runner.RUNTIME_ENV_NAME).stat().st_mode & 0o777, 0o600)
            self.assertEqual((backup / "service.env.previous").read_bytes(), base.read_bytes())
            self.assertEqual((backup / "service.env.local.previous").read_bytes(), local.read_bytes())
            self.assertEqual(runner.load_runtime_environment(backup, state["runtime_env_sha256"]),
                             self.runtime())

    def test_prepare_rejects_crm_port_before_creating_backup(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            package = self.make_package(root)
            binary = write(root / "applinux", b"old-binary", 0o755)
            base = write(root / ".env", b"SERVER_PORT=18087\n")
            local = write(root / ".env.local", b"COS_SECRET_ID=stale\n")
            runtime = dict(self.runtime(), SERVER_PORT="18087")
            with mock.patch.object(runner, "find_process",
                                   return_value={"pid": 111, "cwd": str(root), "exe": str(binary)}), \
                    mock.patch.object(runner, "process_environment", return_value=runtime), \
                    mock.patch.object(runner.subprocess, "run") as run:
                with self.assertRaisesRegex(RuntimeError, "SERVER_PORT"):
                    runner.prepare_release(package, package / "manifest.json", root / "backup",
                                           binary, base, local)
            self.assertFalse((root / "backup").exists())
            run.assert_not_called()

    def test_missing_local_env_backup_refuses_migration(self):
        with tempfile.TemporaryDirectory() as temporary:
            self.make_state(Path(temporary))
            backup = Path(temporary) / "backup"
            (backup / "service.env.local.previous").unlink()
            with self.assertRaisesRegex(RuntimeError, "service.env.local.previous"):
                runner.migrate_release(backup)

    def test_runtime_snapshot_tamper_refuses_migration_before_candidate(self):
        with tempfile.TemporaryDirectory() as temporary:
            self.make_state(Path(temporary))
            backup = Path(temporary) / "backup"
            (backup / runner.RUNTIME_ENV_NAME).write_text('{"MYSQL_DSN":"tampered"}\n')
            with mock.patch.object(runner, "run_private") as run_private:
                with self.assertRaisesRegex(RuntimeError, "runtime-env.json|runtime environment snapshot"):
                    runner.migrate_release(backup)
                run_private.assert_not_called()

    def test_migrations_receive_exact_captured_runtime_without_crm(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            state = self.make_state(root)
            backup = root / "backup"
            runtime = self.runtime()
            with mock.patch.object(runner, "same_process", return_value=True), \
                    mock.patch.object(runner, "process_environment", return_value=runtime), \
                    mock.patch.object(runner, "run_private") as run_private, \
                    mock.patch.object(runner, "verify_schema") as verify_schema:
                runner.migrate_release(backup)
            self.assertEqual(run_private.call_count, 4)
            for call in run_private.call_args_list:
                self.assertEqual(call.args[1], runtime)
                self.assertFalse(any(key.startswith("CRM_") for key in call.args[1]))
                self.assertEqual(call.kwargs["cwd"], backup)
            verify_schema.assert_called_once()
            self.assertTrue(json.loads((backup / runner.STATE_NAME).read_text())["migrated"])

    def test_runtime_drift_refuses_migration(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            self.make_state(root)
            drifted = dict(self.runtime(), SERVER_PORT="18087")
            with mock.patch.object(runner, "same_process", return_value=True), \
                    mock.patch.object(runner, "process_environment", return_value=drifted), \
                    mock.patch.object(runner, "run_private") as run_private:
                with self.assertRaisesRegex(RuntimeError, "runtime environment changed"):
                    runner.migrate_release(root / "backup")
                run_private.assert_not_called()

    def test_local_env_drift_refuses_activation_without_mutation(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            self.make_state(root, migrated=True)
            (root / ".env.local").write_text("COS_SECRET_ID=changed\n")
            with mock.patch.object(runner, "same_process", return_value=True), \
                    mock.patch.object(runner, "stop_process") as stop:
                with self.assertRaisesRegex(RuntimeError, "production inputs changed"):
                    runner.activate_release(root / "backup", root / "pointer.json")
                stop.assert_not_called()

    def test_activation_starts_directly_with_snapshot_and_flags_without_editing_env_files(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            state = self.make_state(root, migrated=True)
            backup = root / "backup"
            base_before = (root / ".env").read_bytes()
            local_before = (root / ".env.local").read_bytes()
            started = SimpleNamespace(pid=222)
            new_identity = {"pid": 222, "start_time": "2", "exe": state["binary"], "cwd": str(root)}
            with mock.patch.object(runner, "same_process", return_value=True), \
                    mock.patch.object(runner, "process_environment", return_value=self.runtime()), \
                    mock.patch.object(runner, "stop_process"), \
                    mock.patch.object(runner, "start_application", return_value=started) as start, \
                    mock.patch.object(runner, "wait_healthy", return_value=new_identity):
                runner.activate_release(backup, root / "pointer.json")
            launched = start.call_args.args[1]
            self.assertEqual(launched["SESSION_SECRET"], "live-session")
            self.assertEqual(launched["CONTENT_WORKBENCH_ENABLED"], "true")
            self.assertNotIn("SERVER_PORT", launched)
            self.assertFalse(any(key.startswith("CRM_") for key in launched))
            self.assertEqual((root / ".env").read_bytes(), base_before)
            self.assertEqual((root / ".env.local").read_bytes(), local_before)

    def test_failed_health_restores_old_binary_with_exact_runtime_snapshot(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            state = self.make_state(root, migrated=True)
            backup = root / "backup"
            started = SimpleNamespace(pid=222)
            new_identity = {"pid": 222, "start_time": "2", "exe": state["binary"], "cwd": str(root)}
            environments = []

            def capture_start(_binary, environment, _log):
                environments.append(dict(environment))
                return started

            with mock.patch.object(runner, "same_process", return_value=True), \
                    mock.patch.object(runner, "process_environment", return_value=self.runtime()), \
                    mock.patch.object(runner, "stop_process") as stop, \
                    mock.patch.object(runner, "start_application", side_effect=capture_start), \
                    mock.patch.object(runner, "process_identity", return_value=new_identity), \
                    mock.patch.object(runner, "wait_healthy",
                                      side_effect=[RuntimeError("unhealthy"), state["process"]]):
                with self.assertRaisesRegex(RuntimeError, "unhealthy"):
                    runner.activate_release(backup, root / "pointer.json")
            self.assertEqual((root / "applinux").read_bytes(), b"old-binary")
            self.assertEqual(environments[1], self.runtime())
            self.assertNotIn("CONTENT_WORKBENCH_ENABLED", environments[1])
            self.assertEqual(stop.call_count, 2)

    def test_manifest_mismatch_is_rejected_before_process_lookup(self):
        with tempfile.TemporaryDirectory() as temporary:
            package = self.make_package(Path(temporary), bad_binary_hash=True)
            with mock.patch.object(runner, "find_process") as find_process:
                with self.assertRaisesRegex(ValueError, "checksum mismatch"):
                    runner.prepare_release(package, package / "manifest.json", package / "backup")
                find_process.assert_not_called()

    def test_schema_verification_counts_composite_index_once(self):
        with tempfile.TemporaryDirectory() as temporary:
            package = self.make_package(Path(temporary))
            table_count = len(set(runner.REQUIRED_TABLES + runner.official_tables(
                package / "migrations/phase3_official_bindings_up.sql"
            )))
            result = SimpleNamespace(returncode=0, stdout=f"{table_count}\n1\n0\n")
            with mock.patch.object(runner.subprocess, "run", return_value=result) as run:
                runner.verify_schema(self.runtime(), package, package / "schema.log")
            query = run.call_args.args[0][-2]
            self.assertIn("COUNT(DISTINCT index_name)", query)

    def test_start_application_executes_binary_in_its_own_directory(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            binary = write(root / "applinux", b"binary", 0o755)
            process = SimpleNamespace(pid=1)
            with mock.patch.object(runner.subprocess, "Popen", return_value=process) as popen:
                runner.start_application(binary, {"A": "1"}, root / "start.log")
            self.assertEqual(popen.call_args.args[0], [str(binary)])
            self.assertEqual(popen.call_args.kwargs["cwd"], root)
            self.assertEqual(popen.call_args.kwargs["env"], {"A": "1"})

    def test_start_current_does_not_duplicate_live_process(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            state = self.make_state(root, migrated=True, activated=True)
            identity = {"pid": 333, "start_time": "3", "exe": state["binary"],
                        "cwd": str(root.resolve())}
            with mock.patch.object(runner, "find_processes", return_value=[identity]), \
                    mock.patch.object(runner, "wait_healthy", return_value=identity), \
                    mock.patch.object(runner, "start_application") as start:
                actual = runner.start_current(root / "backup", root / "pointer.json")
            self.assertEqual(actual, identity)
            start.assert_not_called()

    def test_start_current_uses_snapshot_and_flags_when_process_is_absent(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            state = self.make_state(root, migrated=True, activated=True)
            identity = {"pid": 444, "start_time": "4", "exe": state["binary"], "cwd": str(root)}
            with mock.patch.object(runner, "find_processes", return_value=[]), \
                    mock.patch.object(runner, "start_application", return_value=SimpleNamespace(pid=444)) as start, \
                    mock.patch.object(runner, "wait_healthy", return_value=identity):
                runner.start_current(root / "backup", root / "pointer.json")
            environment = start.call_args.args[1]
            self.assertEqual(environment["SESSION_SECRET"], "live-session")
            self.assertEqual(environment["CONTENT_WORKBENCH_ENABLED"], "true")
            self.assertFalse(any(key.startswith("CRM_") for key in environment))

    def test_rollback_restores_old_binary_and_exact_runtime_then_updates_state(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            state = self.make_state(root, migrated=True, activated=True)
            backup = root / "backup"
            old_identity = {"pid": 555, "start_time": "5", "exe": state["binary"],
                            "cwd": str(root.resolve())}
            started = SimpleNamespace(pid=555)
            environments = []

            def capture_start(_binary, environment, _log):
                environments.append(dict(environment))
                return started

            with mock.patch.object(runner, "same_process", return_value=True), \
                    mock.patch.object(runner, "process_environment",
                                      return_value=runner.environment_with_flags(self.runtime())), \
                    mock.patch.object(runner, "stop_process"), \
                    mock.patch.object(runner, "start_application", side_effect=capture_start), \
                    mock.patch.object(runner, "wait_healthy", return_value=old_identity):
                result = runner.rollback_release(backup, root / "pointer.json")
            self.assertFalse(result["activated"])
            self.assertTrue(result["rolled_back"])
            self.assertEqual(result["process"], old_identity)
            self.assertEqual((root / "applinux").read_bytes(), b"old-binary")
            self.assertEqual(environments, [self.runtime()])
            self.assertNotIn("CONTENT_WORKBENCH_ENABLED", environments[0])

    def test_rollback_rejects_changed_new_process_before_stop(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            self.make_state(root, migrated=True, activated=True)
            with mock.patch.object(runner, "same_process", return_value=False), \
                    mock.patch.object(runner, "stop_process") as stop:
                with self.assertRaisesRegex(RuntimeError, "activated process changed"):
                    runner.rollback_release(root / "backup", root / "pointer.json")
                stop.assert_not_called()

    def test_failed_manual_rollback_recovers_activated_binary_and_environment(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            state = self.make_state(root, migrated=True, activated=True)
            backup = root / "backup"
            started = SimpleNamespace(pid=555)
            old_identity = {"pid": 555, "start_time": "5", "exe": state["binary"],
                            "cwd": str(root.resolve())}
            new_identity = {"pid": 666, "start_time": "6", "exe": state["binary"],
                            "cwd": str(root.resolve())}
            environments = []

            def capture_start(_binary, environment, _log):
                environments.append(dict(environment))
                return started

            with mock.patch.object(runner, "same_process", return_value=True), \
                    mock.patch.object(runner, "process_environment",
                                      return_value=runner.environment_with_flags(self.runtime())), \
                    mock.patch.object(runner, "stop_process") as stop, \
                    mock.patch.object(runner, "start_application", side_effect=capture_start), \
                    mock.patch.object(runner, "process_identity", return_value=old_identity), \
                    mock.patch.object(runner, "wait_healthy",
                                      side_effect=[RuntimeError("old unhealthy"), new_identity]):
                with self.assertRaisesRegex(RuntimeError, "old unhealthy"):
                    runner.rollback_release(backup, root / "pointer.json")
            self.assertEqual((root / "applinux").read_bytes(), b"new-binary")
            self.assertEqual(environments[0], self.runtime())
            self.assertEqual(environments[1], runner.environment_with_flags(self.runtime()))
            self.assertEqual(stop.call_count, 2)
            self.assertTrue(json.loads((backup / runner.STATE_NAME).read_text())["activated"])

    def test_current_pointer_contains_only_the_private_backup_path(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            pointer = root / "current.json"
            runner.write_pointer(pointer, root / "backup")
            value = json.loads(pointer.read_text(encoding="utf-8"))
            self.assertEqual(value, {"backup": str((root / "backup").resolve())})
            rendered = pointer.read_text(encoding="utf-8")
            self.assertNotIn("SESSION_SECRET", rendered)
            self.assertNotIn("MYSQL_DSN", rendered)
            self.assertEqual(pointer.stat().st_mode & 0o777, 0o600)

    @staticmethod
    def runtime() -> dict[str, str]:
        return {
            "MYSQL_DSN": "release:secret@tcp(db:3306)/app",
            "SESSION_SECRET": "live-session",
            "COS_SECRET_ID": "live-cos-id",
            "COS_SECRET_KEY": "live-cos-key",
            "PATH": "/usr/bin:/bin",
        }

    def make_package(self, root: Path, bad_binary_hash: bool = False) -> Path:
        package = root / "package"
        package.mkdir(parents=True, exist_ok=True)
        binary = write(package / "applinux", b"new-binary", 0o755)
        files = [{"path": "applinux", "sha256": "0" * 64 if bad_binary_hash else runner.sha256(binary)}]
        for migration in runner.MIGRATIONS:
            sql = write(package / "migrations" / migration,
                        ("CREATE TABLE IF NOT EXISTS official_%s(id INT);\n" % migration[:8]).encode())
            files.append({"path": f"migrations/{migration}", "sha256": runner.sha256(sql)})
        write(package / "manifest.json", json.dumps({"files": files}).encode())
        return package

    def make_state(self, root: Path, migrated: bool = False, activated: bool = False) -> dict:
        backup = root / "backup"
        backup.mkdir(mode=0o700)
        package = self.make_package(root)
        binary_content = b"new-binary" if activated else b"old-binary"
        binary = write(root / "applinux", binary_content, 0o755)
        base = write(root / ".env", b"SERVER_PORT=18087\nCRM_BRIDGE_URL=https://wrong.example\n")
        local = write(root / ".env.local", b"COS_SECRET_ID=stale\nCOS_SECRET_KEY=stale\n")
        manifest = package / "manifest.json"
        write(backup / "applinux.previous", b"old-binary", 0o755)
        write(backup / "service.env.previous", base.read_bytes())
        write(backup / "service.env.local.previous", local.read_bytes())
        write(backup / "manifest.json", manifest.read_bytes())
        write(backup / "database.sql", b"database-backup\n")
        runtime = self.runtime()
        runner.atomic_write_bytes(backup / runner.RUNTIME_ENV_NAME, runner.environment_bytes(runtime))
        process = {"pid": 111, "start_time": "1", "exe": str(binary), "cwd": str(root)}
        state = {
            "prepared": True,
            "migrated": migrated,
            "activated": activated,
            "process": process,
            "binary": str(binary),
            "base_env": str(base),
            "local_env": str(local),
            "candidate": str((package / "applinux").resolve()),
            "package": str(package.resolve()),
            "binary_sha256": runner.sha256(backup / "applinux.previous"),
            "base_env_sha256": runner.sha256(backup / "service.env.previous"),
            "local_env_sha256": runner.sha256(backup / "service.env.local.previous"),
            "manifest_sha256": runner.sha256(backup / "manifest.json"),
            "runtime_env_sha256": runner.sha256(backup / runner.RUNTIME_ENV_NAME),
            "candidate_sha256": runner.sha256(package / "applinux"),
        }
        if activated:
            state["new_binary_sha256"] = runner.sha256(binary)
            state["new_process"] = process
        runner.save_state(backup, state)
        return state


if __name__ == "__main__":
    unittest.main()
