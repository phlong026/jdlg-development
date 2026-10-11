// Real component handlers, deterministic hooks and delayed fake APIs. No browser/server writes.
import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import ts from 'typescript';
import React from 'react';
const flush = () => new Promise(resolve => setImmediate(resolve));
const clone = value => JSON.parse(JSON.stringify(value));
function deferred() { let resolve, reject; const promise = new Promise((yes, no) => { resolve = yes; reject = no; }); return { promise, resolve, reject }; }
const item = (id, extra = {}) => ({ id, title: `灵感${id}`, description: 'original draft', images: [`image-${id}`], status: 'pending', topic: 'inspiration', sort_order: 0, view_count: 0, like_count: 0, created_at: '', updated_at: '', ...extra });
function nodes(tree, predicate) {
  const found = [];
  function walk(node) {
    if (Array.isArray(node)) return node.forEach(walk);
    if (!node || typeof node !== 'object') return;
    if (predicate(node)) found.push(node);
    walk(node.props?.children);
  }
  walk(tree); return found;
}
function textOf(node) { if (node == null || typeof node === 'boolean') return ''; if (Array.isArray(node)) return node.map(textOf).join(''); if (typeof node !== 'object') return String(node); return textOf(node.props?.children); }
function one(page, predicate, label) { const result = nodes(page.tree, predicate); assert.equal(result.length, 1, label); return result[0]; }
function button(page, label) { return one(page, node => node.type === 'button' && textOf(node) === label, `button ${label}`); }
function named(page, label) { return one(page, node => node.props?.['aria-label'] === label, label); }
function field(page, placeholder) { return one(page, node => node.props?.placeholder === placeholder, placeholder); }
function click(page, label) { const node = button(page, label); assert.notEqual(node.props.disabled, true, `${label} enabled`); const result = node.props.onClick(); page.render(); return result; }
function fill(page, placeholder, value) { field(page, placeholder).props.onChange({ target: { value } }); page.render(); }
function filter(page, label, value) { named(page, label).props.onChange({ target: { value } }); page.render(); page.effects(); page.render(); }
function upload(page, files) { const input = named(page, '上传灵感图片'); const target = { files: files.map(name => ({ name })), value: 'selected' }; input.props.onChange({ target }); assert.equal(target.value, ''); page.render(); }
function images(page) { return nodes(page.tree, node => node.props?.className === 'image-preview-thumb').map(node => node.props.src); }
async function settle(page) { await flush(); await flush(); page.render(); }
function harness() {
  const page = { slots: [], cursor: 0, pending: [], tree: null, updates: 0 };
  const lists = [], details = [], uploads = [], writes = [], failures = [], recoveries = [], confirms = [];
  const settings = { confirm: () => true };
  const fakeReact = {
    ...React,
    useState(initial) { const i = page.cursor++; if (!(i in page.slots)) page.slots[i] = typeof initial === 'function' ? initial() : initial; return [page.slots[i], value => { page.updates++; page.slots[i] = typeof value === 'function' ? value(page.slots[i]) : value; }]; },
    useRef(initial) { const i = page.cursor++; if (!(i in page.slots)) page.slots[i] = { current: initial }; return page.slots[i]; },
    useMemo(factory) { return factory(); },
    useEffect(effect, deps) { const i = page.cursor++, previous = page.slots[i]; if (!previous || deps.some((value, n) => !Object.is(value, previous.deps[n]))) { page.slots[i] = { deps, cleanup: previous?.cleanup }; page.pending.push(() => { page.slots[i].cleanup?.(); page.slots[i].cleanup = effect(); }); } },
  };
  fakeReact.default = fakeReact; fakeReact.__esModule = true;
  const api = {
    getInspirationList: params => { const request = { ...deferred(), params: clone(params) }; lists.push(request); return request.promise; },
    getInspirationDetail: id => { const request = { ...deferred(), id }; details.push(request); return request.promise; },
  };
  for (const method of ['createInspiration', 'updateInspiration', 'updateInspirationStatus', 'deleteInspiration']) api[method] = (...args) => { const request = { ...deferred(), method, args: clone(args) }; writes.push(request); return request.promise; };
  const module = { exports: {} };
  const context = vm.createContext({
    module, exports: module.exports, console, Error,
    FormData: class { constructor() { this.entries = []; } append(name, value) { this.entries.push([name, value]); } },
    window: { confirm: text => { confirms.push(text); return settings.confirm(text); } },
    fetch: (url, options) => { const request = { ...deferred(), url, options }; uploads.push(request); return request.promise; },
    require(name) {
      if (name === 'react') return fakeReact;
      if (name.endsWith('.scss')) return {};
      if (name === 'react-icons/fi') return new Proxy({}, { get: (_, name) => name });
      if (name === '../utils/pageLoadError') return { reportPageLoadError: (error, source) => failures.push({ error, source }), reportPageLoadRecovered: source => recoveries.push(source) };
      if (name === '../utils/token') return { tokenUtils: { getToken: () => 'fixture-session' } };
      if (name === '../config/api') return { API_BASE_URL: 'https://fixture.invalid', API_ENDPOINTS: { OSS: { UPLOAD: '/oss/upload' } } };
      if (name === '../api/inspirations') return api;
      throw new Error(`Unexpected import ${name}`);
    },
  });
  const source = fs.readFileSync(new URL('../src/pages/inspirations.tsx', import.meta.url), 'utf8');
  vm.runInContext(ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, jsx: ts.JsxEmit.React, target: ts.ScriptTarget.ES2020, esModuleInterop: true } }).outputText, context);
  page.render = () => { page.cursor = 0; page.tree = module.exports.default({}); };
  page.effects = () => page.pending.splice(0).forEach(effect => effect());
  page.unmount = () => page.slots.forEach(slot => { if (slot && typeof slot.cleanup === 'function') slot.cleanup(); });
  page.render(); page.effects(); page.render();
  return { page, lists, details, uploads, writes, failures, recoveries, confirms, settings };
}
async function list(h, index = h.lists.length - 1, ids = [1, 2], total = 42) { h.lists[index].resolve({ list: ids.map(id => item(id)), total }); await settle(h.page); }
async function edit(h, id = 1, extra = {}) { named(h.page, `编辑灵感 ${id}`).props.onClick(); h.page.render(); const request = h.details.at(-1); request.resolve(item(id, extra)); await settle(h.page); }
async function uploaded(h, index, url) { h.uploads[index].resolve({ ok: true, json: async () => ({ code: 0, data: { url } }) }); await settle(h.page); }

test('inspiration upload only updates its original editor and never starts remaining files after cancel', async () => {
  const h = harness(); await list(h); await edit(h, 1);
  upload(h.page, ['first', 'second']); assert.equal(h.uploads.length, 1);
  click(h.page, '取消'); click(h.page, '新增灵感'); fill(h.page, '灵感标题', 'new draft');
  upload(h.page, ['new-editor']); assert.equal(h.uploads.length, 2);
  await uploaded(h, 0, 'late-old');
  assert.deepEqual(images(h.page), []); assert.equal(h.uploads.length, 2); assert.match(textOf(h.page.tree), /图片上传中/);
  await uploaded(h, 1, 'new-image'); assert.deepEqual(images(h.page), ['new-image']); assert.equal(field(h.page, '灵感标题').props.value, 'new draft');
  assert.doesNotMatch(textOf(h.page.tree), /图片上传中/);
});

test('inspiration same-ID reopen, late upload failure and stale image/delete/save handlers cannot touch a newer draft', async () => {
  const h = harness(); await list(h); await edit(h, 1);
  const oldRemove = named(h.page, '移除图片 1').props.onClick, oldSave = button(h.page, '保存').props.onClick;
  upload(h.page, ['old']); click(h.page, '取消'); await edit(h, 1, { title: 'reopened', images: ['fresh'] });
  oldRemove(); oldSave(); h.page.render();
  h.uploads[0].reject(new Error('old upload failed')); await settle(h.page);
  assert.equal(field(h.page, '灵感标题').props.value, 'reopened'); assert.deepEqual(images(h.page), ['fresh']); assert.equal(h.writes.length, 0); assert.doesNotMatch(textOf(h.page.tree), /old upload failed/);
});

test('inspiration uploads enforce nine-image cap before network and keep same-frame duplicate submissions single-flight', async () => {
  const h = harness(); await list(h); await edit(h, 1, { images: Array.from({ length: 8 }, (_, i) => `existing-${i}`) });
  const savedUpload = named(h.page, '上传灵感图片').props.onChange;
  upload(h.page, ['one', 'two', 'three']); savedUpload({ target: { files: [{ name: 'duplicate' }], value: 'x' } });
  assert.equal(h.uploads.length, 1); assert.match(textOf(h.page.tree), /仅上传前 1 张/);
  const oldSave = button(h.page, '保存').props.onClick; oldSave(); assert.equal(h.writes.length, 0);
  await uploaded(h, 0, 'ninth'); assert.equal(images(h.page).length, 9); assert.equal(named(h.page, '上传灵感图片').props.disabled, true);
  upload(h.page, ['tenth']); assert.equal(h.uploads.length, 1); assert.match(textOf(h.page.tree), /最多 9 张图片/);
  click(h.page, '保存'); assert.equal(h.writes.length, 1); assert.equal(h.writes[0].args[1].images.length, 9);
});

test('inspiration upload merges into the latest draft; removed images stay removed and partial failures preserve completed uploads', async () => {
  const h = harness(); await list(h); await edit(h, 1, { images: ['keep', 'remove'] });
  upload(h.page, ['success', 'failure', 'not-started']);
  named(h.page, '移除图片 2').props.onClick(); h.page.render(); fill(h.page, '灵感标题', 'edited while uploading');
  await uploaded(h, 0, 'uploaded'); assert.deepEqual(images(h.page), ['keep', 'uploaded']); assert.equal(h.uploads.length, 2);
  h.uploads[1].reject(new Error('network failure')); await settle(h.page);
  assert.equal(h.uploads.length, 2); assert.deepEqual(images(h.page), ['keep', 'uploaded']); assert.equal(field(h.page, '灵感标题').props.value, 'edited while uploading');
  assert.match(textOf(h.page.tree), /本次已成功 1 张/); assert.equal(button(h.page, '保存').props.disabled, false);
  upload(h.page, ['failure']); await uploaded(h, 2, 'retried'); assert.deepEqual(images(h.page), ['keep', 'uploaded', 'retried']);
});

test('inspiration edit detail is cancelable, identity checked and isolated from slow A/new B or add dialog', async () => {
  const h = harness(); await list(h);
  named(h.page, '编辑灵感 1').props.onClick(); h.page.render(); assert.match(textOf(h.page.tree), /素材详情加载中/); assert.equal(button(h.page, '保存').props.disabled, true);
  click(h.page, '取消'); await edit(h, 2); h.details[0].resolve(item(1)); await settle(h.page); assert.equal(field(h.page, '灵感标题').props.value, '灵感2');
  click(h.page, '取消'); named(h.page, '编辑灵感 1').props.onClick(); h.page.render(); h.details.at(-1).resolve(item(2)); await settle(h.page);
  assert.match(textOf(h.page.tree), /详情与所选素材不一致/); button(h.page, '保存').props.onClick(); assert.equal(h.writes.length, 0);
  click(h.page, '重新加载详情'); h.details.at(-1).reject(new Error('detail offline')); await settle(h.page); assert.match(textOf(h.page.tree), /detail offline/);
  click(h.page, '重新加载详情'); h.details.at(-1).resolve(item(1)); await settle(h.page); assert.equal(field(h.page, '灵感标题').props.value, '灵感1');
  click(h.page, '取消'); named(h.page, '编辑灵感 1').props.onClick(); h.page.render(); click(h.page, '取消'); click(h.page, '新增灵感');
  h.details.at(-1).reject(new Error('late canceled detail')); await settle(h.page); assert.equal(field(h.page, '灵感标题').props.value, ''); assert.doesNotMatch(textOf(h.page.tree), /late canceled detail/);
});

test('inspiration save freezes target/payload, suppresses duplicates, survives cancel/reopen and keeps failed drafts retryable', async () => {
  const h = harness(); await list(h); await edit(h, 1);
  fill(h.page, '灵感标题', ' saved title '); const oldSave = button(h.page, '保存').props.onClick;
  oldSave(); oldSave(); assert.equal(h.writes.length, 1); assert.equal(h.writes[0].method, 'updateInspiration'); assert.equal(h.writes[0].args[0], '1'); assert.equal(h.writes[0].args[1].title, 'saved title');
  h.page.render(); click(h.page, '取消'); await edit(h, 2); assert.equal(button(h.page, '保存中...').props.disabled, true);
  h.writes[0].resolve(item(1)); await settle(h.page); assert.equal(field(h.page, '灵感标题').props.value, '灵感2'); assert.equal(button(h.page, '保存').props.disabled, false);
  const failed = click(h.page, '保存'); h.writes[1].reject(new Error('save offline')); await failed; h.page.render();
  assert.match(textOf(h.page.tree), /保存失败.*草稿已保留/); assert.equal(field(h.page, '灵感标题').props.value, '灵感2'); assert.deepEqual(images(h.page), ['image-2']);
  click(h.page, '保存'); assert.equal(h.writes.length, 3); assert.equal(h.writes[2].args[0], '2'); assert.deepEqual(h.writes[1].args, h.writes[2].args);
});

test('inspiration new drafts retain existing default status and first-image cover ordering', async () => {
  const h = harness(); await list(h); click(h.page, '新增灵感'); fill(h.page, '灵感标题', 'new');
  upload(h.page, ['one', 'two']); await uploaded(h, 0, 'cover'); await uploaded(h, 1, 'second');
  click(h.page, '保存'); assert.equal(h.writes[0].method, 'createInspiration'); assert.equal(h.writes[0].args[0].status, 'draft'); assert.deepEqual(h.writes[0].args[0].images, ['cover', 'second']);
});

test('inspiration approve/reject/delete preserve existing protocols and are mutually exclusive before rerender', async () => {
  const h = harness(); await list(h, 0, [1]);
  const approve = button(h.page, '通过').props.onClick, reject = button(h.page, '驳回').props.onClick, remove = named(h.page, '删除灵感 1').props.onClick;
  const pending = approve(); approve(); reject(); remove(); assert.equal(h.writes.length, 1); assert.deepEqual(h.writes[0].args, ['1', 'published']);
  h.writes[0].reject(new Error('status offline')); await pending; h.page.render(); assert.match(textOf(h.page.tree), /灵感1.*操作失败/);
  const retry = reject(); assert.deepEqual(h.writes[1].args, ['1', 'draft']); h.writes[1].resolve(item(1)); await retry; await settle(h.page); await list(h, 1, [1]);
  h.settings.confirm = () => false; await named(h.page, '删除灵感 1').props.onClick(); assert.equal(h.writes.length, 2);
  h.settings.confirm = () => true; const deleteHandler = named(h.page, '删除灵感 1').props.onClick; deleteHandler(); deleteHandler(); assert.equal(h.writes.length, 3); assert.equal(h.writes[2].method, 'deleteInspiration'); assert.deepEqual(h.writes[2].args, ['1']);
});

test('inspiration server pagination preserves filters, resets on filter changes and ignores old scope success/failure', async () => {
  const h = harness(); await list(h, 0, [1, 2], 42);
  assert.equal(h.lists[0].params.page, 1); assert.equal(h.lists[0].params.page_size, 20);
  const oldDelete = named(h.page, '删除灵感 1').props.onClick;
  button(h.page, '下一页').props.onClick(); oldDelete(); assert.equal(h.writes.length, 0); assert.equal(h.confirms.length, 0);
  h.page.render(); h.page.effects(); h.page.render(); assert.equal(h.lists[1].params.page, 2);
  filter(h.page, '搜索灵感', 'target'); assert.equal(h.lists[2].params.page, 1); assert.equal(h.lists[2].params.keyword, 'target');
  await list(h, 2, [7], 21); h.lists[1].resolve({ list: [item(1)], total: 42 }); await settle(h.page); assert.match(textOf(h.page.tree), /灵感7/); assert.doesNotMatch(textOf(h.page.tree), /灵感1/);
  click(h.page, '下一页'); h.page.effects(); h.page.render(); assert.equal(h.lists[3].params.keyword, 'target');
  filter(h.page, '灵感状态', 'published'); await list(h, 4, [8], 1); h.lists[3].reject(new Error('late failure')); await settle(h.page);
  assert.match(textOf(h.page.tree), /灵感8/); assert.equal(h.failures.length, 0); assert.equal(h.lists[4].params.page, 1); assert.equal(h.lists[4].params.status, 'published');
});

test('inspiration list failure is unknown, retry recovers, and empty last page moves to valid server page', async () => {
  const h = harness(); h.lists[0].reject(new Error('offline')); await settle(h.page);
  assert.match(textOf(h.page.tree), /灵感列表加载失败/); assert.doesNotMatch(textOf(h.page.tree), /暂无灵感素材/); assert.deepEqual(nodes(h.page.tree, node => node.props?.className === 'stat-value').map(textOf), ['—', '—', '—']);
  click(h.page, '重新加载列表'); await list(h, 1, [], 0); assert.match(textOf(h.page.tree), /暂无灵感素材/); assert.equal(h.failures[0].source, h.recoveries.at(-1));
  click(h.page, '刷新列表'); await list(h, 2, [1], 21); click(h.page, '下一页'); h.page.effects(); h.page.render();
  await list(h, 3, [], 20); h.page.effects(); h.page.render(); assert.equal(h.lists[4].params.page, 1); await list(h, 4, [1], 20); assert.match(textOf(h.page.tree), /第 1 页/);
});

test('inspiration old mutation completion refreshes current filters, never its captured filters', async () => {
  const h = harness(); await list(h, 0, [1]); const action = click(h.page, '通过');
  filter(h.page, '搜索灵感', 'new-scope'); await list(h, 1, [2], 1);
  h.writes[0].resolve(item(1)); await action; await settle(h.page);
  assert.equal(h.lists.at(-1).params.keyword, 'new-scope'); await list(h, 2, [2], 1); assert.doesNotMatch(textOf(h.page.tree), /灵感1/);
});

test('inspiration unmount drops late list/detail/upload/write results without UI updates or follow-up reads', async () => {
  for (const phase of ['list', 'detail', 'upload', 'write']) {
    const h = harness();
    if (phase !== 'list') await list(h, 0, [1]);
    if (phase === 'detail') { named(h.page, '编辑灵感 1').props.onClick(); h.page.render(); }
    if (phase === 'upload') { await edit(h, 1); upload(h.page, ['first', 'second']); }
    if (phase === 'write') { await edit(h, 1); click(h.page, '保存'); }
    h.page.unmount(); const updates = h.page.updates, reads = h.lists.length;
    if (phase === 'list') h.lists[0].resolve({ list: [item(1)], total: 1 });
    if (phase === 'detail') h.details[0].resolve(item(1));
    if (phase === 'upload') h.uploads[0].resolve({ ok: true, json: async () => ({ code: 0, data: { url: 'late' } }) });
    if (phase === 'write') h.writes[0].resolve(item(1));
    await flush(); await flush(); assert.equal(h.page.updates, updates, phase); assert.equal(h.lists.length, reads, phase); assert.equal(h.uploads.length, phase === 'upload' ? 1 : 0, phase);
  }
});

test('inspiration native picker button invokes only the hidden input and respects the upload lock before rerender', async () => {
  const h = harness(); await list(h); await edit(h, 1);
  let picks = 0; named(h.page, '上传灵感图片').ref.current = { click() { picks++; } };
  const trigger = button(h.page, '选择图片并上传'); assert.equal(trigger.props.type, 'button');
  trigger.props.onClick(); trigger.props.onClick(); assert.equal(picks, 2); assert.equal(h.uploads.length, 0); assert.equal(h.writes.length, 0);
  named(h.page, '上传灵感图片').props.onChange({ target: { files: [], value: '' } }); assert.equal(h.uploads.length, 0);
  upload(h.page, ['one']); trigger.props.onClick(); assert.equal(picks, 2); assert.equal(h.uploads.length, 1);
  assert.equal(button(h.page, '选择图片并上传').props.disabled, true);
  h.uploads[0].reject(new Error('retry picker')); await settle(h.page);
  assert.equal(button(h.page, '选择图片并上传').props.disabled, false); click(h.page, '选择图片并上传'); assert.equal(picks, 3);
  assert.equal(h.uploads.length, 1, 'picker recovery itself never posts a file');
});

test('inspiration picker cannot bypass canceled sessions, detail failures, nine-image limit or pending save', async () => {
  const h = harness(); await list(h); await edit(h, 1);
  let picks = 0; named(h.page, '上传灵感图片').ref.current = { click() { picks++; } };
  const oldPick = button(h.page, '选择图片并上传').props.onClick;
  click(h.page, '取消'); click(h.page, '新增灵感'); oldPick(); assert.equal(picks, 0);
  click(h.page, '取消'); named(h.page, '编辑灵感 1').props.onClick(); h.page.render();
  assert.equal(button(h.page, '选择图片并上传').props.disabled, true); button(h.page, '选择图片并上传').props.onClick(); assert.equal(picks, 0);
  h.details.at(-1).reject(new Error('detail unavailable')); await settle(h.page); button(h.page, '选择图片并上传').props.onClick(); assert.equal(picks, 0);
  click(h.page, '重新加载详情'); h.details.at(-1).resolve(item(1, { images: Array.from({ length: 9 }, (_, i) => `image-${i}`) })); await settle(h.page);
  assert.equal(button(h.page, '选择图片并上传').props.disabled, true); button(h.page, '选择图片并上传').props.onClick(); assert.equal(picks, 0);
  named(h.page, '移除图片 9').props.onClick(); h.page.render(); const pick = button(h.page, '选择图片并上传').props.onClick;
  button(h.page, '保存').props.onClick(); pick(); assert.equal(picks, 0); assert.equal(h.writes.length, 1); assert.equal(h.uploads.length, 0);
  h.writes[0].reject(new Error('save failed')); await settle(h.page); pick(); assert.equal(picks, 1);
  h.page.unmount(); pick(); assert.equal(picks, 1);
});
