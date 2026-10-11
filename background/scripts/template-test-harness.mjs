import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
import vm from 'node:vm';
import ts from 'typescript';
import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';

const require = createRequire(import.meta.url);
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const settle = () => new Promise(resolve => setImmediate(resolve));
const deferred = () => {
  let resolve, reject;
  const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
};
const nodes = node => Array.isArray(node) ? node.flatMap(nodes) : node && typeof node === 'object' ? [node, ...nodes(node.props?.children)] : [];
const textOf = node => Array.isArray(node) ? node.map(textOf).join('') : node == null || typeof node === 'boolean' ? '' : typeof node === 'object' ? textOf(node.props?.children) : String(node);
const plain = value => JSON.parse(JSON.stringify(value));
const template = (overrides = {}) => ({
  id: 1, name: 'Existing', category: 'villa', description: 'Description',
  main_tab: 'main', sub_tab: 'sub', third_tab: 'third',
  thumbnail: '/derived-thumbnail.jpg', preview_url: '/derived-preview.jpg', images: '[{"image":"/original.jpg","caption":"keep"}]',
  image_width: 800, image_height: 1000, price: 10, is_free: true,
  status: 'rejected', publish_scope: 'homepage_only', reject_reason: 'Needs revision', source_type: 'album_upload',
  download_count: 12, creator: 'Creator', created_at: '', updated_at: '', ...overrides,
});

// Render production TSX and call its actual event handlers. Only hook storage,
// DOM file-input refs and network/API boundaries are replaced.
async function page(overrides = {}) {
  const state = [], effects = [], alerts = [], saves = [], creates = [], uploads = [];
  const cleanups = new Map(), timers = new Map();
  let timerId = 0, dirty = false;
  const location = { search: overrides.search || '' };
  let cursor = 0, tree;
  const current = template(overrides.template);
  const api = {
    getTemplateList: async () => ({ list: [current], total: 1, page: 1, page_size: 20 }),
    getFeaturedCaseGroups: async () => ({ list: [], total: 0, page: 1, page_size: 20 }),
    getTemplateDetail: async () => current,
    getTemplateCategories: async () => [{ id: 'villa', name: 'Villa' }],
    getTemplateTabConfig: async () => ({ main_tabs: [{ label: 'Main', value: 'main' }], sub_tabs: [{ label: 'Sub', value: 'sub', parent: 'main' }], third_tabs: [{ label: 'Third', value: 'third', parent: 'sub' }] }),
    updateTemplate: async (id, patch) => { saves.push({ id, patch: plain(patch) }); return { ...current, ...patch }; },
    createTemplate: async patch => { creates.push(plain(patch)); return { ...current, ...patch }; },
    ...overrides.api,
  };
  const react = {
    ...React,
    useState(initial) {
      const index = cursor++;
      if (!(index in state)) state[index] = typeof initial === 'function' ? initial() : initial;
      return [state[index], next => { const value = typeof next === 'function' ? next(state[index]) : next; if (!Object.is(value, state[index])) dirty = true; state[index] = value; }];
    },
    useRef(initial) {
      const index = cursor++;
      if (!(index in state)) state[index] = { current: initial };
      return state[index];
    },
    useCallback(callback, deps) {
      const index = cursor++;
      if (!(index in state) || deps.some((dep, i) => dep !== state[index].deps[i])) state[index] = { callback, deps };
      return state[index].callback;
    },
    useEffect(effect, deps) {
      const index = cursor++;
      if (!(index in state) || deps.some((dep, i) => dep !== state[index][i])) { state[index] = deps; effects.push({ index, effect }); }
    },
  };
  const module = { exports: {} };
  const code = ts.transpileModule(fs.readFileSync(path.join(root, 'src/pages/templates.tsx'), 'utf8'), {
    compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.CommonJS, jsx: ts.JsxEmit.ReactJSX, esModuleInterop: true },
  }).outputText;
  vm.runInNewContext(code, {
    module, exports: module.exports,
    require(name) {
      if (name === 'react') return react;
      if (name === 'react/jsx-runtime') return require(name);
      if (name === 'react-router-dom') return { useLocation: () => location };
      if (name === 'react-icons/fi') return new Proxy({}, { get: () => () => null });
      if (name === '../api/templates') return api;
      if (name === '../config/api') return { API_BASE_URL: '/fake-api', API_ENDPOINTS: { OSS: { UPLOAD: '/upload' } } };
      if (name === '../utils/pageLoadError') return { reportPageLoadError: error => alerts.push(error.message) };
      if (name.endsWith('.scss')) return {};
      throw new Error(`Unexpected dependency: ${name}`);
    },
    URLSearchParams, FormData,
    fetch: async (...args) => { uploads.push(args); return overrides.upload ? overrides.upload(...args) : { ok: true, json: async () => ({ code: 0, data: { url: '/uploaded.jpg' } }) }; },
    alert: message => alerts.push(message),
    window: { location, setTimeout(callback) { const id = ++timerId; timers.set(id, callback); return id; }, clearTimeout(id) { timers.delete(id); }, confirm: () => true, prompt: () => '', open() {} },
    console: { error() {} },
  });
  const h = {
    alerts, saves, creates, uploads,
    render() {
      cursor = 0;
      dirty = false;
      tree = module.exports.default();
      for (const node of nodes(tree)) if (node.type === 'input' && node.props.type === 'file' && node.ref) {
        node.ref.current = { clicks: 0, click() { this.clicks++; } };
      }
      return tree;
    },
    nodes: () => nodes(tree),
    find(predicate) { const node = h.nodes().find(predicate); assert.ok(node, 'Expected UI element'); return node; },
    button(label) { return h.find(node => node.type === 'button' && textOf(node).trim() === label); },
    modal() { return h.find(node => node.props?.className === 'modal-content modal-form'); },
    field(label) {
      const group = nodes(h.modal()).find(node => node.props?.className?.startsWith('form-group') && nodes(node).some(child => child.type === 'label' && textOf(child) === label));
      assert.ok(group, `Expected field ${label}`);
      return nodes(group).find(node => ['input', 'select', 'textarea'].includes(node.type));
    },
    async edit() { await h.find(node => node.type === 'button' && node.props.title === '编辑').props.onClick(); h.render(); },
    add() { h.button('添加模板').props.onClick(); h.render(); },
    cancel() { h.button('取消').props.onClick(); h.render(); },
    change(label, value, checked) { h.field(label).props.onChange({ target: { value, checked } }); h.render(); },
    file() { return nodes(h.modal()).find(node => node.type === 'input' && node.props.type === 'file'); },
    upload() { const input = { files: [new Blob(['fake'])], value: 'fixture.jpg' }; return h.file().props.onChange({ currentTarget: input, target: input }); },
    markup() { return renderToStaticMarkup(tree); },
    async flush() {
      for (let pass = 0; pass < 30; pass++) {
        h.render();
        for (const { index, effect } of effects.splice(0)) { cleanups.get(index)?.(); cleanups.set(index, effect()); }
        await settle();
        if (!dirty && effects.length === 0) return h;
      }
      throw new Error('Hook fixture did not settle');
    },
    async runTimers() { const pending = [...timers.values()]; timers.clear(); pending.forEach(callback => callback()); await h.flush(); },
    setSearch(search) { location.search = search; h.render(); },
    setSearchWithoutRender(search) { location.search = search; },
    unmount() { for (const cleanup of cleanups.values()) cleanup?.(); },
  };
  await h.flush();
  return h;
}


export { page, template, deferred, settle, nodes, textOf, plain };
