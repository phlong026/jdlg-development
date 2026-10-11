#!/usr/bin/env node
const crypto = require('node:crypto');
const fs = require('node:fs');
const path = require('node:path');

const hash = bytes => crypto.createHash('sha256').update(bytes).digest('hex');
function checkedPath(root, relative) {
  const parts = relative.split('/');
  if (!['miniprogram', 'tests', 'project.config.json'].includes(parts[0]) ||
      parts.some(part => !part || part === '.' || part === '..' || part.includes('\\'))) {
    throw new Error('回退路径不合法：' + relative);
  }
  let current = root;
  for (const part of parts) {
    current = path.join(current, part);
    if (fs.lstatSync(current).isSymbolicLink()) throw new Error('拒绝回退符号链接：' + relative);
  }
  if (!fs.statSync(current).isFile()) throw new Error('回退目标不是文件：' + relative);
  return current;
}
function main() {
  let target = path.resolve(__dirname, '../../miniprogram');
  let apply = false;
  const args = process.argv.slice(2);
  for (let i = 0; i < args.length; i++) {
    if (args[i] === '--apply') apply = true;
    else if (args[i] === '--target' && args[i + 1] && !args[i + 1].startsWith('--')) target = path.resolve(args[++i]);
    else throw new Error('用法：node rollback.cjs [--target 小程序目录] [--apply]');
  }
  if (!fs.statSync(target).isDirectory() || fs.lstatSync(target).isSymbolicLink()) throw new Error('目标目录无效');
  const manifest = JSON.parse(fs.readFileSync(path.join(__dirname, 'rollback-manifest.json'), 'utf8'));
  const seen = new Set();
  const plan = [];
  // Validate the whole release before writing so later edits cannot be overwritten silently.
  for (const entry of manifest.files) {
    if (seen.has(entry.path)) throw new Error('清单路径重复：' + entry.path);
    seen.add(entry.path);
    const current = checkedPath(target, entry.path);
    if (hash(fs.readFileSync(current)) !== entry.afterSha256) throw new Error('文件已变更，停止回退：' + entry.path);
    let original;
    if (entry.role === 'restore') {
      original = fs.readFileSync(checkedPath(path.join(__dirname, 'before'), entry.path));
      if (hash(original) !== entry.beforeSha256) throw new Error('原版备份校验失败：' + entry.path);
    } else if (entry.role !== 'remove') throw new Error('未知回退动作：' + entry.role);
    plan.push({ entry, current, original });
  }
  process.stdout.write((apply ? '执行' : '预演') + '回退：' + target + '\n');
  for (const item of plan) process.stdout.write(item.entry.role + ' ' + item.entry.path + '\n');
  if (!apply) {
    process.stdout.write('全部哈希校验通过；未修改文件。添加 --apply 才执行回退。\n');
    return;
  }
  for (const item of plan) {
    if (item.entry.role === 'restore') fs.writeFileSync(item.current, item.original);
    else fs.unlinkSync(item.current);
  }
  for (const item of plan) {
    if (item.entry.role === 'restore' && hash(fs.readFileSync(item.current)) !== item.entry.beforeSha256) throw new Error('恢复后校验失败：' + item.entry.path);
    if (item.entry.role === 'remove' && fs.existsSync(item.current)) throw new Error('新增文件仍存在：' + item.entry.path);
  }
  process.stdout.write('旧首页已恢复；云端资源保留，其他页面未修改。\n');
}
try { main(); }
catch (error) { process.stderr.write('回退失败：' + error.message + '\n'); process.exitCode = 1; }
