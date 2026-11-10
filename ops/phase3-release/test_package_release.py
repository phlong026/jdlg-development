import json
import os
import tempfile
import unittest
from pathlib import Path

import package_release


def make_inputs(root: Path) -> tuple[Path, Path, Path]:
    binary = root / "applinux-src"
    binary.write_bytes(b"\x7fELF-fake-binary")
    binary.chmod(0o755)
    sql_dir = root / "sql"
    sql_dir.mkdir()
    for name in package_release.MIGRATIONS:
        (sql_dir / name).write_text("-- migration\n", encoding="utf-8")
    admin = root / "dist"
    (admin / "assets").mkdir(parents=True)
    (admin / "index.html").write_text("<html></html>", encoding="utf-8")
    (admin / "assets" / "app.js").write_text("console.log(1)", encoding="utf-8")
    return binary, sql_dir, admin


class PackageReleaseTest(unittest.TestCase):
    def test_builds_manifest_with_binary_migrations_and_admin(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            binary, sql_dir, admin = make_inputs(root)
            out = root / "release"
            manifest = package_release.build_package(out, binary, sql_dir, admin)
            paths = {entry["path"] for entry in manifest["files"]}
            self.assertIn("applinux", paths)
            self.assertIn("admin/index.html", paths)
            self.assertIn("admin/assets/app.js", paths)
            for name in package_release.MIGRATIONS:
                self.assertIn(f"migrations/{name}", paths)
            self.assertTrue(os.access(out / "applinux", os.X_OK))
            written = json.loads((out / "manifest.json").read_text(encoding="utf-8"))
            self.assertEqual(written["files"], manifest["files"])

    def test_rejects_missing_audited_migration(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            binary, sql_dir, admin = make_inputs(root)
            (sql_dir / package_release.MIGRATIONS[0]).unlink()
            with self.assertRaises(ValueError):
                package_release.build_package(root / "release", binary, sql_dir, admin)

    def test_rejects_admin_without_index(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            binary, sql_dir, admin = make_inputs(root)
            (admin / "index.html").unlink()
            with self.assertRaises(ValueError):
                package_release.build_package(root / "release", binary, sql_dir, admin)

    def test_rejects_non_empty_output(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            binary, sql_dir, admin = make_inputs(root)
            out = root / "release"
            out.mkdir()
            (out / "stale").write_text("x", encoding="utf-8")
            with self.assertRaises(ValueError):
                package_release.build_package(out, binary, sql_dir, admin)

    def test_rejects_env_local_in_package(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            binary, sql_dir, admin = make_inputs(root)
            out = root / "release"
            out.mkdir()
            (out / ".env.local").write_text("SECRET=1", encoding="utf-8")
            with self.assertRaises(ValueError):
                package_release.build_package(out, binary, sql_dir, admin)


if __name__ == "__main__":
    unittest.main()
