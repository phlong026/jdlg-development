// 解析前端所有 get/post/put/patch/del 调用，得到 METHOD + 解析后的路径
import fs from 'node:fs';
import path from 'node:path';
import { createRequire } from 'node:module';
const SRC = '/Users/honor.pei/Documents/订单项目/甲地灵光二期/开发版916/JDLG/background/src';
const require = createRequire('/Users/honor.pei/Documents/订单项目/甲地灵光二期/开发版916/JDLG/background/package.json');
const esbuild = require('esbuild');

// 1. 求值 config/api.ts
const cfgSrc = fs.readFileSync(path.join(SRC, 'config/api.ts'), 'utf8');
const js = esbuild.transformSync(cfgSrc, { loader: 'ts', format: 'cjs', define: { 'import.meta.env': '{}' } }).code;
const mod = { exports: {} };
new Function('module', 'exports', js)(mod, mod.exports);
const { API_ENDPOINTS, API_BASE_URL } = mod.exports;

// 2. 收集源文件
const files = [];
(function walk(d) {
  for (const f of fs.readdirSync(d)) {
    const p = path.join(d, f);
    if (fs.statSync(p).isDirectory()) walk(p);
    else if (/\.(ts|tsx)$/.test(f) && !f.endsWith('.d.ts')) files.push(p);
  }
})(SRC);

const METHOD = { get: 'GET', post: 'POST', put: 'PUT', patch: 'PATCH', del: 'DELETE' };
const callRe = /(?<![\w.$])(get|post|put|patch|del)\s*(?:<[^()]*?>)?\s*\(/g;

function firstArg(s, start) {
  let depth = 0, i = start, q = null;
  for (; i < s.length; i++) {
    const c = s[i];
    if (q) { if (c === '\\') { i++; continue; } if (c === q) q = null; continue; }
    if (c === '"' || c === "'" || c === '`') { q = c; continue; }
    if ('([{'.includes(c)) depth++;
    else if (')]}'.includes(c)) { if (depth === 0) return s.slice(start, i); depth--; }
    else if (c === ',' && depth === 0) return s.slice(start, i);
  }
  return null;
}

const proxy = new Proxy({}, {
  has: (_, k) => k !== 'API_ENDPOINTS' && k !== 'API_BASE_URL',
  get: (_, k) => (k in globalThis ? globalThis[k] : 'X'),
});

const calls = [], unresolved = [];
for (const f of files) {
  const src = fs.readFileSync(f, 'utf8');
  if (!/utils\/request/.test(src) && !/^export const (get|post|put|patch|del)\b/m.test(src)) continue;
  let m;
  while ((m = callRe.exec(src))) {
    const expr = firstArg(src, m.index + m[0].length);
    if (!expr || !expr.trim()) continue;
    const line = src.slice(0, m.index).split('\n').length;
    try {
      const fn = new Function('API_ENDPOINTS', 'API_BASE_URL', 'proxy', `with(proxy){return (${expr})}`);
      const val = fn(API_ENDPOINTS, API_BASE_URL, proxy);
      if (typeof val !== 'string') throw new Error('not string');
      calls.push({ method: METHOD[m[1]], path: val.replace(/\/[^/]*X[^/]*(?=\/|$)/g, '/{}'), raw: val, file: path.relative(SRC, f), line });
    } catch (e) {
      unresolved.push({ file: path.relative(SRC, f), line, expr: expr.trim().slice(0, 80) });
    }
  }
}
fs.writeFileSync('/tmp/contract/frontend_calls.json', JSON.stringify({ calls, unresolved }, null, 1));
console.log(`frontend calls: ${calls.length}, unresolved: ${unresolved.length}`);
