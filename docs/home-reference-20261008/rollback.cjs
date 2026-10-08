#!/usr/bin/env node
const crypto = require('node:crypto');
const fs = require('node:fs');
const path = require('node:path');

function checksum(bytes) {
  return crypto.createHash('sha256').update(bytes).digest('hex');
}

function checkedPath(root, relative) {
  const parts = relative.split('/');
  if (parts[0] !== 'miniprogram' || parts.some((part) => !part || part === '.' || part === '..' || part.includes('\\'))) {
    throw new Error(`交付清单路径不合法：${relative}`);
  }
  let current = root;
  for (const part of parts) {
    current = path.join(current, part);
    if (fs.lstatSync(current).isSymbolicLink()) throw new Error(`拒绝通过符号链接回退：${current}`);
  }
  return current;
}

function main() {
  let target = path.resolve(__dirname, '../../miniprogram');
  let apply = false;
  const args = process.argv.slice(2);
  for (let index = 0; index < args.length; index += 1) {
    if (args[index] === '--apply') apply = true;
    else if (args[index] === '--target' && args[index + 1] && !args[index + 1].startsWith('--')) target = path.resolve(args[++index]);
    else throw new Error(`未知或缺少参数：${args[index]}；用法：node rollback.cjs [--target 项目目录] [--apply]`);
  }
  if (!fs.statSync(target).isDirectory()) throw new Error(`目标不是目录：${target}`);
  if (fs.lstatSync(target).isSymbolicLink()) throw new Error(`拒绝回退符号链接目录：${target}`);
  const manifest = JSON.parse(fs.readFileSync(path.join(__dirname, 'manifest.json'), 'utf8'));
  const entries = manifest.files.filter((entry) => entry.role === 'restore' || entry.role === 'remove');
  const plan = [];
  const seen = new Set();

  // Complete every content check before writing, so later edits cannot be silently overwritten.
  for (const entry of entries) {
    if (seen.has(entry.path)) throw new Error(`交付清单路径重复：${entry.path}`);
    seen.add(entry.path);
    const current = checkedPath(target, entry.path);
    if (!fs.statSync(current).isFile()) throw new Error(`目标不是普通文件：${current}`);
    if (checksum(fs.readFileSync(current)) !== entry.afterSha256) {
      throw new Error(`当前文件与交付快照不一致，停止整次回退：${entry.path}`);
    }
    let original;
    if (entry.role === 'restore') {
      const originalPath = checkedPath(path.join(__dirname, 'before'), entry.path);
      original = fs.readFileSync(originalPath);
      if (checksum(original) !== entry.beforeSha256) throw new Error(`原始备份校验失败：${entry.path}`);
    }
    plan.push({ entry, current, original });
  }

  process.stdout.write(`${apply ? '执行' : '预演'}回退：${target}\n`);
  for (const item of plan) {
    process.stdout.write(`${item.entry.role === 'restore' ? '恢复' : '删除'} ${item.entry.path}\n`);
  }
  if (!apply) {
    process.stdout.write('全部哈希校验通过；未修改文件。显式添加 --apply 才执行。\n');
    return;
  }
  for (const item of plan) {
    if (item.entry.role === 'restore') fs.writeFileSync(item.current, item.original);
    else fs.unlinkSync(item.current);
  }
  for (const item of plan) {
    if (item.entry.role === 'restore' && checksum(fs.readFileSync(item.current)) !== item.entry.beforeSha256) {
      throw new Error(`恢复后校验失败：${item.entry.path}`);
    }
    if (item.entry.role === 'remove' && fs.existsSync(item.current)) throw new Error(`新增文件删除失败：${item.entry.path}`);
  }
  process.stdout.write(`回退完成：恢复 ${plan.filter((item) => item.entry.role === 'restore').length} 个文件，删除 ${plan.filter((item) => item.entry.role === 'remove').length} 个新增文件。测试和清单之外文件未修改。\n`);
}

try {
  main();
} catch (error) {
  process.stderr.write(`回退校核失败：${error.message}\n`);
  process.exitCode = 1;
}
