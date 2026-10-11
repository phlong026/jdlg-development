import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
import vm from 'node:vm';
import test from 'node:test';
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
  let cursor = 0, tree;
  const current = template(overrides.template);
  const api = {
    getTemplateList: async () => ({ list: [current] }),
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
      return [state[index], next => { state[index] = typeof next === 'function' ? next(state[index]) : next; }];
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
      if (!(index in state) || deps.some((dep, i) => dep !== state[index][i])) { state[index] = deps; effects.push(effect); }
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
    window: { location: { search: '' }, setTimeout, clearTimeout, confirm: () => true, open() {} },
    console: { error() {} },
  });
  const h = {
    alerts, saves, creates, uploads,
    render() {
      cursor = 0;
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
  };
  h.render();
  for (const effect of effects.splice(0)) effect();
  await settle();
  h.render();
  return h;
}

test('first add renders without a selection and owns a working upload input', async () => {
  const h = await page();
  h.add();
  assert.equal(h.field('名称 *').props.value, '');
  assert.doesNotMatch(textOf(h.modal()), /运营信息|Creator|Needs revision/);
  h.button('选择图片并上传').props.onClick();
  assert.equal(h.file().ref.current.clicks, 1);
  await h.upload(); h.render();
  assert.equal(h.uploads.length, 1);
  assert.ok(nodes(h.modal()).some(node => node.type === 'img' && node.props.src === '/uploaded.jpg'));
  h.change('名称 *', 'Created');
  await h.button('保存').props.onClick(); h.render();
  assert.equal(h.creates.length, 1);
  assert.equal(h.creates[0].name, 'Created');
  assert.deepEqual(JSON.parse(h.creates[0].images), ['/uploaded.jpg']);
});

test('edit-only upload input is mounted, clickable, and saves uploaded images', async () => {
  const h = await page(); await h.edit();
  assert.match(textOf(h.modal()), /运营信息.*Creator/);
  h.button('选择图片并上传').props.onClick();
  assert.equal(h.file().ref.current.clicks, 1);
  await h.upload(); h.render();
  await h.button('保存').props.onClick();
  assert.deepEqual(JSON.parse(h.saves[0].patch.images), ['/original.jpg', '/uploaded.jpg']);
  assert.deepEqual(Object.keys(h.saves[0].patch).sort(), ['images', 'preview_url', 'thumbnail']);
});

test('name-only edit sends no old publication, moderation, pricing, tab or image fields', async () => {
  const h = await page(); await h.edit();
  h.change('名称 *', 'Renamed');
  await h.button('保存').props.onClick();
  assert.deepEqual(h.saves, [{ id: '1', patch: { name: 'Renamed' } }]);
});

test('explicit empty tab/description, zero price and false free flag are retained', async () => {
  const h = await page(); await h.edit();
  h.change('一级标签（可选）', '');
  h.change('描述', '');
  h.change('价格（灵石）', '0');
  h.field('免费').props.onChange({ target: { checked: false } }); h.render();
  await h.button('保存').props.onClick();
  assert.deepEqual(h.saves[0].patch, { description: '', main_tab: '', sub_tab: '', third_tab: '', price: 0, is_free: false });
});

test('unchanged edit makes no write, including structured image metadata', async () => {
  const h = await page(); await h.edit();
  await h.button('保存').props.onClick(); h.render();
  assert.equal(h.saves.length, 0);
  assert.equal(h.nodes().filter(node => node.props?.className === 'modal-overlay').length, 0);
});

test('closing edit and adding starts fresh; closing add and reopening edit reloads detail', async () => {
  const h = await page(); await h.edit();
  h.change('名称 *', 'Unsaved edit'); h.cancel(); h.add();
  assert.equal(h.field('名称 *').props.value, '');
  assert.equal(nodes(h.modal()).filter(node => node.type === 'img').length, 0);
  h.change('名称 *', 'Unsaved add'); h.cancel(); await h.edit();
  assert.equal(h.field('名称 *').props.value, 'Existing');
});

test('late uploads cannot append to another editor, and upload blocks saving', async () => {
  const pending = deferred();
  const h = await page({ upload: () => pending.promise });
  await h.edit();
  const upload = h.upload(); h.render();
  assert.equal(h.button('保存').props.disabled, true);
  await h.button('保存').props.onClick();
  assert.equal(h.saves.length, 0);
  h.cancel(); h.add();
  pending.resolve({ ok: true, json: async () => ({ code: 0, data: { url: '/late.jpg' } }) });
  await upload; h.render();
  assert.equal(nodes(h.modal()).filter(node => node.type === 'img').length, 0);
  assert.equal(h.button('保存').props.disabled, false);
});

test('a late detail request cannot replace an add editor', async () => {
  const pending = deferred();
  const h = await page({ api: { getTemplateDetail: () => pending.promise } });
  const edit = h.edit(); h.add();
  h.change('名称 *', 'New draft');
  pending.resolve(template()); await edit; h.render();
  assert.equal(h.field('名称 *').props.value, 'New draft');
  assert.match(textOf(h.modal()), /添加模板/);
});

test('save is synchronous single-flight and a failed save retains the editable draft', async () => {
  const pending = deferred(); let count = 0;
  const h = await page({ api: { updateTemplate: () => { count++; return pending.promise; } } });
  await h.edit(); h.change('名称 *', 'Retry me');
  const save = h.button('保存').props.onClick;
  const first = save(); await save();
  assert.equal(count, 1);
  pending.reject(new Error('Conflict: reload')); await first; h.render();
  assert.equal(h.field('名称 *').props.value, 'Retry me');
  assert.deepEqual(h.alerts, ['Conflict: reload']);
  assert.equal(h.button('保存').props.disabled, false);
});

test('committed save with unavailable read-back closes editor and asks for refresh without resubmitting', async () => {
  let count = 0;
  const h = await page({ api: { updateTemplate: async () => { count++; return null; } } });
  await h.edit(); h.change('名称 *', 'Saved');
  await h.button('保存').props.onClick(); await settle(); h.render();
  assert.equal(count, 1);
  assert.equal(h.nodes().filter(node => node.props?.className === 'modal-overlay').length, 0);
  assert.deepEqual(h.alerts, ['修改已保存，但最新数据暂时无法读取，请刷新列表核实']);
});

test('removing the last edit image sends explicit empty image fields', async () => {
  const h = await page(); await h.edit();
  h.button('删除').props.onClick(); h.render();
  await h.button('保存').props.onClick();
  assert.deepEqual(h.saves[0].patch, { thumbnail: '', preview_url: '', images: '' });
});
