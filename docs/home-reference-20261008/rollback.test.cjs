const assert = require('node:assert/strict');
const crypto = require('node:crypto');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { spawnSync } = require('node:child_process');
const test = require('node:test');

const manifest = JSON.parse(fs.readFileSync(path.join(__dirname, 'manifest.json'), 'utf8'));
const hash = (file) => crypto.createHash('sha256').update(fs.readFileSync(file)).digest('hex');

function fixture() {
  const target = fs.mkdtempSync(path.join(os.tmpdir(), 'jdlg-home-rollback-test-'));
  for (const entry of manifest.files) {
    const destination = path.join(target, entry.path);
    fs.mkdirSync(path.dirname(destination), { recursive: true });
    fs.copyFileSync(path.join(__dirname, 'after', entry.path), destination);
  }
  fs.writeFileSync(path.join(target, 'unrelated-file.txt'), '保留其他人的文件');
  return target;
}

function run(target, ...args) {
  return spawnSync(process.execPath, [path.join(__dirname, 'rollback.cjs'), '--target', target, ...args], { encoding: 'utf8' });
}

test('默认预演不修改任何应用源码；应用回退恢复原件并移除清单内新增文件', () => {
  const target = fixture();
  try {
    const dry = run(target);
    assert.equal(dry.status, 0, dry.stderr);
    for (const entry of manifest.files) assert.equal(hash(path.join(target, entry.path)), entry.afterSha256);
    const apply = run(target, '--apply');
    assert.equal(apply.status, 0, apply.stderr);
    for (const entry of manifest.files) {
      const file = path.join(target, entry.path);
      if (entry.role === 'restore') assert.equal(hash(file), entry.beforeSha256);
      if (entry.role === 'remove') assert.equal(fs.existsSync(file), false);
      if (entry.role === 'test-only') assert.equal(hash(file), entry.afterSha256);
    }
    assert.equal(fs.readFileSync(path.join(target, 'unrelated-file.txt'), 'utf8'), '保留其他人的文件');
  } finally {
    fs.rmSync(target, { recursive: true, force: true });
  }
});

test('后续源码被他人修改时整次回退必须在写入前停止', () => {
  const target = fixture();
  try {
    const changed = manifest.files.find((entry) => entry.role === 'restore');
    fs.appendFileSync(path.join(target, changed.path), '\n后续修改\n');
    const output = run(target, '--apply');
    assert.equal(output.status, 1);
    assert.match(output.stderr, /与交付快照不一致/);
    for (const entry of manifest.files) {
      if (entry.path !== changed.path) assert.equal(hash(path.join(target, entry.path)), entry.afterSha256);
    }
    assert.match(fs.readFileSync(path.join(target, changed.path), 'utf8'), /后续修改/);
  } finally {
    fs.rmSync(target, { recursive: true, force: true });
  }
});
