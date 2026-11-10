#!/usr/bin/env python3
"""Build a release package directory from a clean checkout.

Package layout (consumed by runner.py / web_release.py):
  applinux                 Linux amd64 backend binary
  migrations/*.sql         audited migrations (names must match runner.MIGRATIONS)
  admin/**                 built admin assets (optional; index.html required)
  manifest.json            {"files":[{"path":..,"sha256":..}]}

Usage:
  python3 ops/phase3-release/package_release.py \
    --out /tmp/jdlg-release \
    --binary /tmp/jdlg-build/applinux-vcs \
    --sql-dir service/sql \
    --admin-dist background/dist
"""
from __future__ import annotations

import argparse
import hashlib
import json
import os
import shutil
import stat
import sys
from pathlib import Path

MIGRATIONS = (
    "phase3_notifications_up.sql",
    "phase3_content_import_up.sql",
    "phase3_official_bindings_up.sql",
    "phase3_share_actor_index_up.sql",
)


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def build_package(out: Path, binary: Path, sql_dir: Path, admin_dist: Path | None) -> dict:
    if out.exists() and any(out.iterdir()):
        raise ValueError(f"output directory is not empty: {out}")
    if not binary.is_file():
        raise ValueError(f"binary is missing: {binary}")
    out.mkdir(parents=True, exist_ok=True)
    candidate = out / "applinux"
    shutil.copyfile(binary, candidate)
    candidate.chmod(stat.S_IRWXU | stat.S_IRGRP | stat.S_IXGRP | stat.S_IROTH | stat.S_IXOTH)

    migrations_dir = out / "migrations"
    migrations_dir.mkdir()
    for name in MIGRATIONS:
        source = sql_dir / name
        if not source.is_file():
            raise ValueError(f"audited migration is missing: {source}")
        shutil.copyfile(source, migrations_dir / name)

    if admin_dist is not None:
        if not (admin_dist / "index.html").is_file():
            raise ValueError("admin dist must contain index.html")
        shutil.copytree(admin_dist, out / "admin")

    if (out / ".env.local").exists():
        raise ValueError("package must not contain .env.local")

    files = []
    for path in sorted(out.rglob("*")):
        if path.is_file() and path.name != "manifest.json":
            files.append({"path": path.relative_to(out).as_posix(), "sha256": sha256_file(path)})
    manifest = {"files": files}
    (out / "manifest.json").write_text(json.dumps(manifest, ensure_ascii=False, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    return manifest


def main() -> int:
    parser = argparse.ArgumentParser(description="Build a release package directory")
    parser.add_argument("--out", required=True, type=Path)
    parser.add_argument("--binary", required=True, type=Path)
    parser.add_argument("--sql-dir", required=True, type=Path)
    parser.add_argument("--admin-dist", type=Path, default=None)
    args = parser.parse_args()
    try:
        manifest = build_package(args.out, args.binary, args.sql_dir, args.admin_dist)
    except (OSError, ValueError) as error:
        print(f"package error: {error}", file=sys.stderr)
        return 1
    print(json.dumps({"out": str(args.out), "files": len(manifest["files"])}, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
