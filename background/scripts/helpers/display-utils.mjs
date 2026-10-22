// 显示名与内部代码翻译是纯函数，各页面测试直接加载真实实现，而不是各写一份桩，
// 这样测试断言的就是生产代码的翻译结果。
import fs from 'node:fs';
import path from 'node:path';
import vm from 'node:vm';
import ts from 'typescript';
import React from 'react';
import { fileURLToPath } from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..');
const cache = new Map();

function loadUtil(file) {
    if (cache.has(file)) return cache.get(file);
    const source = fs.readFileSync(path.join(root, 'src/utils', file), 'utf8');
    const code = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 } }).outputText;
    const module = { exports: {} };
    vm.runInNewContext(code, { module, exports: module.exports, require(name) { throw new Error(`显示工具不应依赖其他模块：${name}`); } });
    cache.set(file, module.exports);
    return module.exports;
}

const files = { displayName: 'displayName.ts', displayLabels: 'displayLabels.ts' };

// 统一页面标题组件：结构与 src/component/PageTitle.tsx 一致（header > h1.admin-page-title + 附加内容）
const PageTitle = ({ title, children, className }) => React.createElement('header', { className: ['admin-page-head', className].filter(Boolean).join(' ') },
    React.createElement('h1', { className: 'admin-page-title' }, title), children);

/** 页面里共用显示件的 require：`../utils/displayName`、`../utils/displayLabels`、`../component/PageTitle`；其他返回 undefined */
export function requireDisplayUtil(name) {
    if (name === '../component/PageTitle') return { __esModule: true, default: PageTitle };
    const match = /^\.\.\/utils\/(displayName|displayLabels)$/.exec(name);
    return match ? loadUtil(files[match[1]]) : undefined;
}

export const displayName = () => loadUtil(files.displayName);
export const displayLabels = () => loadUtil(files.displayLabels);
