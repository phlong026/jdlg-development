from pathlib import Path
import difflib
import hashlib
import json
import shutil
import zipfile


delivery = Path(__file__).resolve().parent
project = delivery.parents[1] / 'miniprogram'
backup = delivery.parents[2] / '需求文档/首页视觉方案/20261008-用户确认复刻/原首页备份/源码'
after = delivery / 'after'
before = delivery / 'before'


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


files = []
diff_parts = []
source_paths = sorted(path.relative_to(backup) for path in backup.rglob('*') if path.is_file())
new_paths = [Path('utils/costRegions.ts'), Path('utils/costRegions.js')]
new_paths += sorted(path.relative_to(project / 'miniprogram') for path in (project / 'miniprogram/assets/home-reference/hd').rglob('*') if path.is_file())

for relative in source_paths + new_paths:
    app_relative = Path('miniprogram') / relative
    current = project / app_relative
    target = after / app_relative
    target.parent.mkdir(parents=True, exist_ok=True)
    shutil.copy2(current, target)
    entry = {'path': app_relative.as_posix(), 'role': 'restore' if relative in source_paths else 'remove', 'afterSha256': digest(target), 'afterSize': target.stat().st_size}
    original = backup / relative
    if original.is_file():
        old_target = before / app_relative
        old_target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(original, old_target)
        entry['beforeSha256'] = digest(old_target)
        entry['beforeSize'] = old_target.stat().st_size
    files.append(entry)
    if current.suffix in {'.ts', '.js', '.wxml', '.scss', '.wxss', '.json'}:
        old_lines = original.read_text().splitlines(keepends=True) if original.is_file() else []
        for line in difflib.unified_diff(old_lines, current.read_text().splitlines(keepends=True), fromfile='a/' + app_relative.as_posix(), tofile='b/' + app_relative.as_posix()):
            diff_parts.append(line if line.endswith('\n') else line + '\n\\ No newline at end of file\n')

test_paths = sorted(set(project.glob('tests/home-*.test.js')) | set(project.glob('tests/aicost-*.test.js')) | {project / 'tests/cost-estimate-share-image.test.js'})
for source in test_paths:
    relative = source.relative_to(project)
    target = after / relative
    target.parent.mkdir(parents=True, exist_ok=True)
    shutil.copy2(source, target)
    files.append({'path': relative.as_posix(), 'role': 'test-only', 'afterSha256': digest(target), 'afterSize': target.stat().st_size})

manifest = {'version': 1, 'projectRelativePath': '../../miniprogram', 'sourceSnapshot': '2026-10-09 首页精修', 'testRollback': False, 'files': files}
(delivery / 'manifest.json').write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + '\n')
(delivery / 'source.diff').write_text(''.join(diff_parts))

archive = delivery / 'home-reference-delivery.zip'
with zipfile.ZipFile(archive, 'w', compression=zipfile.ZIP_DEFLATED, compresslevel=9) as output:
    for folder in [after, before]:
        for path in sorted(folder.rglob('*')):
            if path.is_file():
                output.write(path, path.relative_to(delivery))
    for name in ['manifest.json', 'source.diff', 'rollback.cjs', 'README.md']:
        path = delivery / name
        if path.is_file():
            output.write(path, name)

print(json.dumps({'files': len(files), 'restore': sum(e['role'] == 'restore' for e in files), 'remove': sum(e['role'] == 'remove' for e in files), 'tests': sum(e['role'] == 'test-only' for e in files), 'zipBytes': archive.stat().st_size}, ensure_ascii=False))
