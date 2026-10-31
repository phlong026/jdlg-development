import hashlib
import importlib.util
import json
import sys
import tempfile
import unittest
from pathlib import Path

# 同目录 inspect.py 是生产探针，不能遮蔽 unittest.mock 使用的标准库模块。
TEST_DIRECTORY = str(Path(__file__).resolve().parent)
sys.path = [entry for entry in sys.path if str(Path(entry or ".").resolve()) != TEST_DIRECTORY]
from unittest import mock

SPEC = importlib.util.spec_from_file_location("phase3_web_release", Path(__file__).with_name("web_release.py"))
release = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(release)


def write(path, content):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(content)
    return path


class WebReleaseTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name).resolve()
        self.package = self.root / "package"
        self.backup = self.root / "backup"
        self.admin = self.root / "live-admin"
        self.target = self.root / "extension/phase3-share.conf"
        self.target.parent.mkdir()
        self.main = write(self.root / "main.conf", ("include " + str(self.target.parent / "*.conf") + ";\n").encode())
        for name, value in {"ADMIN_ROOT": self.admin, "NGINX_TARGET": self.target, "NGINX_MAIN": self.main,
                            "NGINX_INCLUDE": str(self.target.parent / "*.conf")}.items():
            patch = mock.patch.object(release, name, value)
            patch.start()
            self.addCleanup(patch.stop)
        self.files = {"phase3-share.conf": b"location /share/ { return 200; }\n",
                      "admin/index.html": b"new index", "admin/assets/app.js": b"new script",
                      "admin/assets/new.js": b"new asset", "applinux": b"backend candidate"}
        for name, content in self.files.items():
            write(self.package / name, content)
        write(self.package / "manifest.json", json.dumps({"files": [
            {"path": name, "sha256": hashlib.sha256(content).hexdigest()} for name, content in self.files.items()
        ]}).encode())
        write(self.admin / "index.html", b"old index")
        write(self.admin / "assets/app.js", b"old script")
        write(self.admin / "assets/old.js", b"old cached asset")
        write(self.admin / "user-file.txt", b"keep user file")

    def test_nginx_failed_test_removes_only_the_new_candidate(self):
        with mock.patch.object(release, "run_nginx", side_effect=RuntimeError("test failed")) as run:
            with self.assertRaisesRegex(RuntimeError, "restored"):
                release.release_nginx(self.package, self.backup)
        self.assertFalse(self.target.exists())
        self.assertTrue((self.backup / "nginx/target-absent").is_file())
        self.assertEqual(run.call_args_list, [mock.call("-t")])
        self.assertEqual((self.backup / "nginx").stat().st_mode & 0o777, 0o700)

    def test_nginx_reload_failure_restores_and_reloads_the_original(self):
        write(self.target, b"original nginx config")
        with mock.patch.object(release, "run_nginx", side_effect=[None, RuntimeError("reload failed"), None, None]) as run:
            with self.assertRaisesRegex(RuntimeError, "restored"):
                release.release_nginx(self.package, self.backup)
        self.assertEqual(self.target.read_bytes(), b"original nginx config")
        self.assertEqual(run.call_args_list, [mock.call("-t"), mock.call("-s", "reload"), mock.call("-t"), mock.call("-s", "reload")])

    def test_nginx_does_not_remove_a_concurrently_changed_file(self):
        def changed(*args):
            self.target.write_bytes(b"another operator config")
            raise RuntimeError("test failed")
        with mock.patch.object(release, "run_nginx", side_effect=changed):
            with self.assertRaisesRegex(RuntimeError, "changed"):
                release.release_nginx(self.package, self.backup)
        self.assertEqual(self.target.read_bytes(), b"another operator config")

    def test_nginx_requires_the_active_include_before_installing(self):
        self.main.write_text("# include " + release.NGINX_INCLUDE + ";\n")
        with mock.patch.object(release, "run_nginx") as run:
            with self.assertRaisesRegex(ValueError, "include"):
                release.release_nginx(self.package, self.backup)
        run.assert_not_called()
        self.assertFalse(self.target.exists())
        self.assertFalse(self.backup.exists())

    def test_admin_backs_up_everything_and_installs_index_last(self):
        installed = []
        original_copy = release.atomic_copy
        def tracked(source, destination, **kwargs):
            if self.package in source.parents:
                installed.append(destination.relative_to(self.admin).as_posix())
            return original_copy(source, destination, **kwargs)
        with mock.patch.object(release, "atomic_copy", side_effect=tracked):
            release.release_admin(self.package, self.backup)
        self.assertEqual(installed[-1], "index.html")
        self.assertEqual(self.admin.joinpath("index.html").read_bytes(), b"new index")
        self.assertEqual(self.admin.joinpath("assets/old.js").read_bytes(), b"old cached asset")
        self.assertEqual(self.admin.joinpath("user-file.txt").read_bytes(), b"keep user file")
        self.assertEqual(self.backup.joinpath("admin/original/user-file.txt").read_bytes(), b"keep user file")
        self.assertEqual(self.backup.joinpath("admin/original/index.html").read_bytes(), b"old index")

    def test_admin_rejects_a_mismatch_anywhere_in_the_manifest_before_backup(self):
        self.package.joinpath("applinux").write_bytes(b"changed unrelated candidate")
        with self.assertRaisesRegex(ValueError, "checksum"):
            release.release_admin(self.package, self.backup)
        self.assertFalse(self.backup.exists())
        self.assertEqual(self.admin.joinpath("index.html").read_bytes(), b"old index")

    def test_admin_failure_restores_originals_without_deleting_other_files(self):
        original_copy = release.atomic_copy
        def failing(source, destination, **kwargs):
            if source == self.package / "admin/index.html":
                raise OSError("index install failed")
            return original_copy(source, destination, **kwargs)
        with mock.patch.object(release, "atomic_copy", side_effect=failing):
            with self.assertRaisesRegex(RuntimeError, "restored"):
                release.release_admin(self.package, self.backup)
        self.assertEqual(self.admin.joinpath("index.html").read_bytes(), b"old index")
        self.assertEqual(self.admin.joinpath("assets/app.js").read_bytes(), b"old script")
        self.assertEqual(self.admin.joinpath("assets/new.js").read_bytes(), b"new asset")
        self.assertEqual(self.admin.joinpath("user-file.txt").read_bytes(), b"keep user file")


if __name__ == "__main__":
    unittest.main()
