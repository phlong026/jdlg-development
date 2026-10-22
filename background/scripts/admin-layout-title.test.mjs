import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import test from 'node:test';
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
import vm from 'node:vm';
import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import ts from 'typescript';

const require = createRequire(import.meta.url);
const src = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../src');
const cache = new Map();
const route = { path: '/share-tracking', title: '分享访问记录' };
const session = { user: { username: 'platform', scope: 'platform_admin', crmEnabled: true } };

function loadComponent(filename) {
  const resolved = path.resolve(src, filename);
  if (cache.has(resolved)) return cache.get(resolved).exports;
  const source = fs.readFileSync(resolved, 'utf8');
  const code = ts.transpileModule(source, {
    compilerOptions: {
      target: ts.ScriptTarget.ES2020,
      module: ts.ModuleKind.CommonJS,
      jsx: ts.JsxEmit.ReactJSX,
      esModuleInterop: true,
    },
  }).outputText;
  const module = { exports: {} };
  cache.set(resolved, module);
  const localRequire = name => {
    if (name === 'react' || name === 'react/jsx-runtime') return require(name);
    if (name === 'react-router-dom') return {
      Outlet: () => React.createElement(loadComponent('component/PageTitle.tsx').default, { title: route.title }),
      useLocation: () => ({ pathname: route.path }),
      useNavigate: () => () => {},
    };
    if (name === 'react-icons/fi') return new Proxy({}, { get: () => props => React.createElement('svg', props) });
    if (name === './topbar') return loadComponent('component/topbar.tsx');
    if (name === './PageTitle') return loadComponent('component/PageTitle.tsx');
    if (name === './ManagementSession') return { useManagementSession: () => session };
    if (name === '../api/auth') return { logout: async () => {} };
    if (name === '../routes') return { routes: [route] };
    if (name === './navMode') return { readNavMode: () => 'new', saveNavMode() {} };
    if (name === './navConfig') return { locate: () => undefined, searchablePages: () => [], visibleGroups: () => [] };
    if (name === '../utils/pageLoadError') return { updatePageLoadFailures: current => current };
    if (name === './adminDialogKeyboard') return { installAdminDialogKeyboard: () => undefined };
    if (['./sidebar', './SubNav', './SubNavCards', './CommandPalette'].includes(name)) {
      return { __esModule: true, default: () => null };
    }
    if (name.endsWith('.scss')) return {};
    throw new Error(`Unexpected dependency from ${filename}: ${name}`);
  };
  vm.runInNewContext(code, {
    module,
    exports: module.exports,
    require: localRequire,
    console,
    URLSearchParams,
    window: {
      location: { pathname: route.path, search: '' },
      matchMedia: () => ({ matches: false }),
    },
  }, { filename: resolved });
  return module.exports;
}

test('AdminLayout 在内容页头已显示标题时只渲染一个主标题', () => {
  const AdminLayout = loadComponent('component/layout.tsx').default;
  const html = renderToStaticMarkup(React.createElement(AdminLayout));

  assert.equal((html.match(/<h1\b/g) || []).length, 1, html);
  assert.equal((html.match(/分享访问记录/g) || []).length, 1, html);
  assert.match(html, /aria-label="收起导航"/);
  assert.match(html, /aria-label="搜索页面（Ctrl\/⌘ K）"/);
});
