#!/usr/bin/env python3
"""校验并备份已确认的官网配置和后台目录；不操作 Go 进程。"""
from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

ADMIN_ROOT = Path("/www/wwwroot/jiadilingguang/admin")
NGINX_MAIN = Path("/www/server/panel/vhost/nginx/html_www.jiadilingguang.com.conf")
NGINX_INCLUDE = "/www/server/panel/vhost/nginx/extension/www.jiadilingguang.com/*.conf"
NGINX_TARGET = Path("/www/server/panel/vhost/nginx/extension/www.jiadilingguang.com/phase3-share.conf")
NGINX_BINARY = "/usr/bin/nginx"


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def verify_manifest(package: Path, only: str = "") -> dict:
    root = package.resolve()
    manifest = json.loads((root / "manifest.json").read_text(encoding="utf-8"))
    if not isinstance(manifest, dict) or not isinstance(manifest.get("files"), list) or not manifest["files"]:
        raise ValueError("manifest must contain nonempty files[]")
    files = {}
    for item in manifest["files"]:
        if not isinstance(item, dict):
            raise ValueError("invalid manifest entry")
        name, expected = item.get("path"), item.get("sha256")
        if not isinstance(name, str) or not name or "\\" in name or Path(name).is_absolute() or ".." in Path(name).parts or Path(name).as_posix() != name or name == ".":
            raise ValueError("invalid manifest path")
        if name in files or not re.fullmatch(r"[0-9a-f]{64}", str(expected)):
            raise ValueError("duplicate manifest path or invalid checksum")
        files[name] = expected
        if only and name != only:
            continue
        source = root / name
        if source.is_symlink() or root not in source.resolve().parents or not source.is_file() or sha256(source) != expected:
            raise ValueError("package checksum mismatch: " + name)
    if only and only not in files:
        raise ValueError("manifest is missing: " + only)
    return files


def private_backup(root: Path, phase: str) -> Path:
    if root.is_symlink():
        raise ValueError("backup root must not be a symlink")
    root.mkdir(parents=True, exist_ok=True, mode=0o700)
    if root.stat().st_mode & 0o077:
        raise ValueError("backup root must be private (0700)")
    target = root / phase
    target.mkdir(mode=0o700)
    return target


def atomic_copy(source: Path, target: Path, expected: str = "", mode=None) -> None:
    if target.is_symlink():
        raise ValueError("refusing to replace a symlink: " + str(target))
    target.parent.mkdir(parents=True, exist_ok=True, mode=0o755)
    descriptor, name = tempfile.mkstemp(prefix="." + target.name + ".", dir=target.parent)
    temporary = Path(name)
    try:
        with os.fdopen(descriptor, "wb") as output, source.open("rb") as original:
            shutil.copyfileobj(original, output)
            output.flush()
            os.fsync(output.fileno())
        shutil.copystat(source, temporary)
        if expected and sha256(temporary) != expected:
            raise ValueError("candidate changed during copy: " + str(source))
        if mode is not None:
            temporary.chmod(mode)
        os.replace(temporary, target)
    finally:
        if temporary.exists():
            temporary.unlink()


def run_nginx(*arguments: str) -> None:
    result = subprocess.run([NGINX_BINARY, *arguments], capture_output=True, timeout=20)
    if result.returncode:
        # 配置检查输出可能包含运行环境信息，只向调用者返回阶段与退出码。
        raise RuntimeError("nginx " + " ".join(arguments) + " failed, exit=" + str(result.returncode))


def release_nginx(package: Path, backup: Path) -> dict:
    package = package.resolve()
    files = verify_manifest(package, "phase3-share.conf")
    includes = re.findall(r"^\s*include\s+([^;\n]+)\s*;", NGINX_MAIN.read_text(encoding="utf-8"), re.MULTILINE)
    if not any(value.strip().strip("\"'") == NGINX_INCLUDE for value in includes):
        raise ValueError("active nginx extension include is missing")
    if not NGINX_TARGET.parent.is_dir() or NGINX_TARGET.is_symlink() or NGINX_TARGET.exists() and not NGINX_TARGET.is_file():
        raise ValueError("unexpected nginx target")
    saved = private_backup(backup, "nginx")
    previous = saved / "phase3-share.conf"
    if NGINX_TARGET.exists():
        shutil.copy2(NGINX_TARGET, previous)
    else:
        (saved / "target-absent").write_text(str(NGINX_TARGET) + "\n", encoding="utf-8")
    expected = files["phase3-share.conf"]
    installed, reload_attempted = False, False
    try:
        atomic_copy(package / "phase3-share.conf", NGINX_TARGET, expected=expected, mode=0o644)
        installed = True
        run_nginx("-t")
        reload_attempted = True
        run_nginx("-s", "reload")
    except Exception as failure:
        if installed:
            if NGINX_TARGET.is_symlink() or not NGINX_TARGET.is_file() or sha256(NGINX_TARGET) != expected:
                raise RuntimeError("nginx target changed; preserved it, backup=" + str(saved)) from failure
            try:
                if previous.exists():
                    atomic_copy(previous, NGINX_TARGET)
                else:
                    NGINX_TARGET.unlink()  # 仅删除本次安装且校验仍一致的候选配置。
                if reload_attempted:
                    run_nginx("-t")
                    run_nginx("-s", "reload")
            except Exception as rollback_failure:
                raise RuntimeError("nginx rollback incomplete; backup=" + str(saved)) from rollback_failure
        raise RuntimeError("nginx deployment failed; original config restored, backup=" + str(saved)) from failure
    return {"phase": "nginx", "status": "installed", "target": str(NGINX_TARGET), "backup": str(saved)}


def release_admin(package: Path, backup: Path) -> dict:
    package = package.resolve()
    files = verify_manifest(package)
    entries = {name[len("admin/"):]: digest for name, digest in files.items() if name.startswith("admin/")}
    if "index.html" not in entries:
        raise ValueError("manifest is missing admin/index.html")
    actual = {path.relative_to(package / "admin").as_posix() for path in (package / "admin").rglob("*") if path.is_file() or path.is_symlink()}
    if actual != set(entries):
        raise ValueError("admin files do not match the complete manifest")
    if ADMIN_ROOT.is_symlink() or not (ADMIN_ROOT / "index.html").is_file():
        raise ValueError("confirmed admin root or index is missing")
    if ADMIN_ROOT.resolve() == backup.resolve() or ADMIN_ROOT.resolve() in backup.resolve().parents:
        raise ValueError("backup cannot be inside the admin root")
    for relative in entries:
        target = ADMIN_ROOT / relative
        if any(path.is_symlink() for path in [target, *target.parents] if path == ADMIN_ROOT or ADMIN_ROOT in path.parents):
            raise ValueError("admin destination contains a symlink: " + relative)
        if target.exists() and not target.is_file():
            raise ValueError("admin destination is not a file: " + relative)
    saved = private_backup(backup, "admin")
    original = saved / "original"
    shutil.copytree(ADMIN_ROOT, original, symlinks=True)
    # 保留旧资源供旧页面继续读取，所有新资源就位后才切换入口。
    ordered = sorted(name for name in entries if name != "index.html") + ["index.html"]
    touched = []
    try:
        for relative in ordered:
            touched.append(relative)
            atomic_copy(package / "admin" / relative, ADMIN_ROOT / relative, expected=entries[relative], mode=0o644)
    except Exception as failure:
        errors = []
        for relative in touched:
            source = original / relative
            if source.is_file():
                try:
                    atomic_copy(source, ADMIN_ROOT / relative)
                except Exception:
                    errors.append(relative)
        # 新增资源可以成为无引用文件；回退不删除任何生产文件。
        if errors:
            raise RuntimeError("admin rollback incomplete; backup=" + str(saved)) from failure
        raise RuntimeError("admin deployment failed; originals restored, backup=" + str(saved)) from failure
    return {"phase": "admin", "status": "installed", "target": str(ADMIN_ROOT), "backup": str(saved), "files": len(ordered)}


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("phase", choices=["nginx", "admin"])
    parser.add_argument("--package", required=True, type=Path)
    parser.add_argument("--backup", required=True, type=Path)
    args = parser.parse_args()
    try:
        function = release_nginx if args.phase == "nginx" else release_admin
        print(json.dumps(function(args.package, args.backup), ensure_ascii=False))
        return 0
    except Exception as error:
        print("ERROR: " + str(error), file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
