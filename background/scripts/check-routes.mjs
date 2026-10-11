#!/usr/bin/env node
// 路由一致性检查：不依赖任何第三方包，改动路由 / 导航 / 样式作用域后必须通过。
//
// 拦截三类问题：
//   1. 页面里写死的跳转目标不在路由表里（死链 → 点击后整页空白）；
//   2. scss 的 data-admin-page 作用域值不是任何路由（网址变了样式整页静默失效）；
//   3. 侧栏条目指向不存在的路由。
//
// 局限：只能检查"字面量"路径（navigate('/x')、path: '/x'、to="/x"）；
// navigate(item.path) 这类动态跳转，其来源数组里的 path: 字面量会被检查到。
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const SRC = path.join(ROOT, 'src');

const read = (p) => fs.readFileSync(p, 'utf8');
const lineOf = (text, index) => text.slice(0, index).split('\n').length;
const walk = (dir, exts, out = []) => {
  for (const name of fs.readdirSync(dir)) {
    const p = path.join(dir, name);
    if (fs.statSync(p).isDirectory()) walk(p, exts, out);
    else if (exts.some((e) => name.endsWith(e))) out.push(p);
  }
  return out;
};
const rel = (p) => path.relative(ROOT, p);

// 1. 路由表
const routesFile = path.join(SRC, 'routes/index.tsx');
const routeSrc = read(routesFile);
const routes = new Set([...routeSrc.matchAll(/\bpath:\s*'(\/[^']*)'/g)].map((m) => m[1]));
if (routes.size === 0) {
  console.error('未能从 routes/index.tsx 解析出任何路由，脚本需要更新');
  process.exit(2);
}

const problems = [];
const add = (kind, file, text, index, detail) =>
  problems.push(`[${kind}] ${rel(file)}:${lineOf(text, index)}  ${detail}`);

// 2. 页面 / 组件里的跳转目标（字面量）
const tsFiles = walk(SRC, ['.tsx', '.ts']).filter((f) => f !== routesFile && !f.endsWith('.d.ts'));
const targetPatterns = [
  /navigate\(\s*[`'"](\/[^`'"?#$]*)/g,                 // navigate('/x') / navigate(`/x?y=${}`)
  /<Navigate\s+to=\{?[`'"](\/[^`'"?#$]*)/g,            // <Navigate to="/x" />
  /\bto=\{?[`'"](\/[^`'"?#$]*)/g,                      // <NavLink to="/x" />
  /\bpath:\s*[`'"](\/[^`'"?#$]*)/g,                    // { label, path: '/x' } 数据数组
];
let checked = 0;
for (const file of tsFiles) {
  const text = read(file);
  for (const re of targetPatterns) {
    for (const m of text.matchAll(re)) {
      const target = m[1].replace(/\/$/, '') || '/';
      checked += 1;
      if (target === '/') continue;                    // 根路径由 App.tsx 重定向
      if (!routes.has(target)) add('死链', file, text, m.index, `跳转目标 ${target} 不在路由表中`);
    }
  }
}

// 3. scss 的路径作用域
const scssFiles = walk(SRC, ['.scss']);
let scoped = 0;
for (const file of scssFiles) {
  const text = read(file);
  for (const m of text.matchAll(/data-admin-page=["']([^"']+)["']/g)) {
    scoped += 1;
    if (!routes.has(m[1])) add('样式作用域', file, text, m.index, `data-admin-page="${m[1]}" 不是任何路由，该页面样式将不生效`);
  }
}

// 4. 导航条目（单一数据源 navConfig.ts；兼容仍把菜单写在 sidebar.tsx 的旧写法）
let navEntries = 0;
for (const rel of ['component/navConfig.ts', 'component/sidebar.tsx']) {
  const navFile = path.join(SRC, rel);
  if (!fs.existsSync(navFile)) continue;
  const text = read(navFile);
  for (const m of text.matchAll(/\bpath:\s*'(\/[^']*)'/g)) {
    navEntries += 1;
    if (!routes.has(m[1])) add('导航', navFile, text, m.index, `导航条目 ${m[1]} 不在路由表中`);
  }
}

// 5. 新导航覆盖：除登录页外，每个路由都必须归属某个分组，否则页面会从导航里消失
const navFileText = fs.existsSync(path.join(SRC, 'component/navConfig.ts')) ? read(path.join(SRC, 'component/navConfig.ts')) : '';
const groupsBlock = navFileText.slice(navFileText.indexOf('export const navGroups'), navFileText.indexOf('export function visibleGroups'));
if (groupsBlock.length > 0) {
  const grouped = new Set([...groupsBlock.matchAll(/\bpath:\s*'(\/[^']*)'/g)].map((m) => m[1]));
  for (const route of routes) {
    if (route !== '/login' && !grouped.has(route)) {
      problems.push(`[导航覆盖] 路由 ${route} 不属于 navConfig.ts 的任何分组，页面将无法从导航进入`);
    }
  }
}

console.log(`路由 ${routes.size} 条；已检查跳转字面量 ${checked} 处；样式作用域 ${scoped} 处；导航条目 ${navEntries} 个`);
if (problems.length) {
  console.error(`\n发现 ${problems.length} 个问题：`);
  for (const p of problems) console.error('  ' + p);
  process.exit(1);
}
console.log('路由一致性检查通过');
