import assert from 'node:assert/strict';
import test from 'node:test';
import { page, template, deferred, settle, nodes, textOf, plain } from './template-test-harness.mjs';

const tabs = () => ({ main_tabs: [{ label: 'Main', value: 'main' }], sub_tabs: [{ label: 'Sub', value: 'sub', parent: 'main' }], third_tabs: [{ label: 'Third', value: 'third', parent: 'sub' }] });
const changeMainLabel = async (h, label) => { h.find(node => node.type === 'input' && node.props.placeholder === '显示名').props.onChange({ target: { value: label } }); await h.flush(); };
const openTabs = async h => { h.button('标签配置').props.onClick(); await h.flush(); };
const group = (overrides = {}) => ({ id: 7, name: 'Group', display_mode: 'normal', case1_id: 88, case2_id: 99, case1_label: 'Original', case2_label: 'AI', sort_order: 3, case1: template({ id: 88, name: 'Selected archived', status: 'archived' }), ...overrides });
const openGroup = async (h, id = 7) => {
  h.button('精选案例组管理').props.onClick(); await h.flush();
  const card = h.find(node => node.key === String(id) && node.props?.className === 'featured-group-item');
  nodes(card).find(node => node.type === 'button' && node.props.title === '编辑').props.onClick();
  await h.flush();
};

test('tab autosave is serialized and coalesces intermediate edits to the latest draft', async () => {
  const first = deferred(), second = deferred(), writes = [];
  const h = await page({ api: { putTemplateTabConfig: payload => { writes.push(plain(payload)); return writes.length === 1 ? first.promise : second.promise; } } });
  await openTabs(h); await changeMainLabel(h, 'A'); await h.runTimers();
  assert.equal(writes.length, 1);
  await changeMainLabel(h, 'B'); await h.runTimers();
  await changeMainLabel(h, 'C'); await h.runTimers();
  assert.equal(writes.length, 1, 'requests must never overlap');
  first.resolve(); await h.flush();
  assert.equal(writes.length, 2);
  assert.equal(writes[1].main_tabs[0].label, 'C');
  assert.doesNotMatch(textOf(h.render()), /已自动保存|最新标签配置已保存/);
  second.resolve(); await h.flush();
  assert.match(textOf(h.render()), /已自动保存/);
  assert.equal(h.find(node => node.type === 'input' && node.props.placeholder === '显示名').props.value, 'C');
});

test('an invalid newer draft is not replaced or marked saved by the old save response', async () => {
  const pending = deferred(), writes = [];
  const h = await page({ api: { putTemplateTabConfig: payload => { writes.push(plain(payload)); return pending.promise; } } });
  await openTabs(h); await changeMainLabel(h, 'Saved older'); await h.runTimers();
  await changeMainLabel(h, ''); pending.resolve(); await h.flush();
  assert.equal(writes.length, 1);
  assert.match(textOf(h.render()), /缺少显示名/);
  assert.doesNotMatch(textOf(h.render()), /已自动保存|保存成功/);
});

test('failed autosave retains newest draft and explicit retry saves that draft', async () => {
  const pending = deferred(), writes = [];
  const h = await page({ api: { putTemplateTabConfig: payload => { writes.push(plain(payload)); return writes.length === 1 ? pending.promise : Promise.resolve(); } } });
  await openTabs(h); await changeMainLabel(h, 'First'); await h.runTimers();
  await changeMainLabel(h, 'Latest'); await h.runTimers();
  pending.reject(new Error('fake save unavailable')); await h.flush();
  assert.match(textOf(h.render()), /fake save unavailable/);
  assert.equal(h.find(node => node.type === 'input' && node.props.placeholder === '显示名').props.value, 'Latest');
  await h.button('保存标签配置').props.onClick(); await h.flush();
  assert.equal(writes.length, 2); assert.equal(writes[1].main_tabs[0].label, 'Latest');
});

test('tab no-op does not lock later saves and reopening does not reload over a draft', async () => {
  let reads = 0; const writes = [];
  const h = await page({ api: { getTemplateTabConfig: async () => { reads++; return tabs(); }, putTemplateTabConfig: async payload => { writes.push(plain(payload)); } } });
  await openTabs(h); await h.button('保存标签配置').props.onClick(); await h.flush();
  await changeMainLabel(h, 'Unsaved');
  h.button('标签配置').props.onClick(); await h.flush(); await openTabs(h);
  assert.equal(reads, 1); assert.equal(h.find(node => node.type === 'input' && node.props.placeholder === '显示名').props.value, 'Unsaved');
  await h.button('保存标签配置').props.onClick(); await h.flush(); assert.equal(writes.length, 1);
});

test('unknown initial tab state cannot be saved and supports a read retry', async () => {
  let fail = true, writes = 0;
  const h = await page({ api: { getTemplateTabConfig: async () => { if (fail) throw new Error('tabs unavailable'); return tabs(); }, putTemplateTabConfig: async () => { writes++; } } });
  await openTabs(h);
  assert.equal(h.button('保存标签配置').props.disabled, true);
  await h.button('保存标签配置').props.onClick(); assert.equal(writes, 0);
  fail = false; await h.button('重新加载标签配置').props.onClick(); await h.flush();
  assert.equal(h.button('保存标签配置').props.disabled, false);
});

test('featured catalog is independent of grid filters and retains both selected IDs', async () => {
  const requests = [], saves = [];
  const h = await page({ api: {
    getTemplateList: async params => { requests.push(plain(params)); return params.page_size === 100 ? { list: [template({ id: 1, name: 'Published option', status: 'published' })], total: 101 } : { list: [template({ id: 2, name: 'Grid draft', status: 'draft' })], total: 1 }; },
    getFeaturedCaseGroups: async () => ({ list: [group()], total: 1 }),
    updateFeaturedCaseGroup: async (id, payload) => { saves.push({ id, payload: plain(payload) }); },
  } });
  h.find(node => node.type === 'select' && node.props.className === 'filter-select' && node.props.children.some(child => child?.props?.value === 'pending')).props.onChange({ target: { value: 'draft' } });
  await h.flush(); await openGroup(h);
  const options = nodes(h.field('第一个案例 *')).filter(node => node.type === 'option');
  assert.deepEqual(options.map(node => node.props.value), ['', '1', '88', '99']);
  assert.match(textOf(h.modal()), /当前案例 #99|普通模式保留现有第二案例 #99/);
  assert.ok(requests.some(params => params.status === 'published' && params.page_size === 100 && params.category === undefined));
  h.change('组名称 *', 'Changed name'); await h.button('保存').props.onClick(); await h.flush();
  assert.equal(saves[0].payload.display_mode, 'normal'); assert.equal(saves[0].payload.case2_id, 99); assert.equal(saves[0].payload.case1_id, 88);
});

test('unknown featured display mode remains visible and needs an explicit supported selection', async () => {
  const saves = [];
  const h = await page({ api: { getFeaturedCaseGroups: async () => ({ list: [group({ display_mode: 'legacy_mode' })], total: 1 }), updateFeaturedCaseGroup: async (_, payload) => { saves.push(plain(payload)); } } });
  await openGroup(h);
  assert.equal(h.field('显示模式 *').props.value, 'legacy_mode'); assert.match(textOf(h.modal()), /未知模式（legacy_mode）/);
  await h.button('保存').props.onClick(); assert.equal(saves.length, 0);
  h.change('显示模式 *', 'normal'); await h.button('保存').props.onClick(); await h.flush();
  assert.equal(saves[0].display_mode, 'normal'); assert.equal(saves[0].case2_id, 99);
});

test('a late catalog response cannot populate a newer featured editor', async () => {
  const first = deferred(), second = deferred(); let reads = 0;
  const h = await page({ api: { getFeaturedCaseGroups: async () => ({ list: [group()], total: 1 }), getTemplateList: async params => params.page_size === 100 ? (++reads === 1 ? first.promise : second.promise) : { list: [], total: 0 } } });
  await openGroup(h); h.cancel();
  h.button('新建案例组').props.onClick(); await h.flush();
  first.resolve({ list: [template({ id: 5, name: 'Old catalog' })], total: 1 }); await h.flush();
  assert.doesNotMatch(textOf(h.modal()), /Old catalog/);
  second.resolve({ list: [template({ id: 6, name: 'New catalog' })], total: 1 }); await h.flush();
  assert.match(textOf(h.modal()), /New catalog/);
});

test('featured save is single-flight and a failed refresh is reported as already saved', async () => {
  const pending = deferred(); let reads = 0, writes = 0;
  const h = await page({ api: { getFeaturedCaseGroups: async () => { if (++reads > 1) throw new Error('refresh down'); return { list: [group()], total: 1 }; }, updateFeaturedCaseGroup: () => { writes++; return pending.promise; } } });
  await openGroup(h); const save = h.button('保存').props.onClick;
  const active = save(); await save(); assert.equal(writes, 1);
  pending.resolve(); await active; await h.flush();
  assert.match(textOf(h.render()), /案例组已保存，但列表刷新失败/);
  assert.equal(h.nodes().filter(node => node.props?.className === 'modal-overlay').length, 0);
  assert.equal(h.alerts.length, 0); assert.match(textOf(h.render()), /Group/);
});

test('template page requests use server totals and disclose page-only keyword search', async () => {
  const requests = [];
  const h = await page({ api: { getTemplateList: async params => { requests.push(plain(params)); return { list: [template({ name: `Page ${params.page}` })], total: 45, page: params.page, page_size: 20 }; } } });
  assert.match(textOf(h.render()), /筛选结果共 45 个模板/);
  h.button('下一页模板').props.onClick(); await h.flush();
  assert.equal(requests.at(-1).page, 2); assert.equal(requests.at(-1).page_size, 20);
  const input = h.find(node => node.type === 'input' && node.props.placeholder === '仅搜索本页模板名称或描述...');
  input.props.onChange({ target: { value: 'not here' } }); h.render(); h.button('搜索本页').props.onClick(); await h.flush();
  assert.equal(requests.length, 2); assert.match(textOf(h.render()), /关键词搜索仅覆盖本页/); assert.match(textOf(h.render()), /本页没有匹配的模板/);
  assert.ok(requests.every(params => params.keyword === undefined));
});

test('stale template reads and failures cannot overwrite the latest filter', async () => {
  const stale = deferred();
  const h = await page({ api: { getTemplateList: async params => params.status === 'published' ? stale.promise : { list: [template({ name: params.status === 'draft' ? 'Current draft' : 'Initial' })], total: 1 } } });
  const status = () => h.find(node => node.type === 'select' && node.props.className === 'filter-select' && node.props.children.some(child => child?.props?.value === 'pending'));
  status().props.onChange({ target: { value: 'published' } }); await h.flush();
  status().props.onChange({ target: { value: 'draft' } }); await h.flush();
  stale.reject(new Error('old query failed')); await h.flush();
  assert.match(textOf(h.render()), /Current draft/); assert.doesNotMatch(textOf(h.render()), /old query failed|尚不能确认列表为空/);
});

test('missing metadata stays unknown and the existing rejected status is represented', async () => {
  const h = await page({ template: { publish_scope: undefined, source_type: undefined, status: 'rejected', reject_reason: 'Explain why' } });
  await h.edit();
  assert.match(textOf(h.modal()), /拒绝原因：Explain why/);
  const status = h.field('状态'); assert.equal(status.props.value, 'rejected');
  assert.ok(nodes(status).some(node => node.type === 'option' && node.props.value === 'rejected'));
  assert.equal(nodes(h.modal()).filter(node => node.props?.className === 'readonly-info-value' && textOf(node) === '未提供').length, 2);
  assert.doesNotMatch(textOf(h.modal()), /后台创建|首页 \+ 模板广场/);
});

test('candidate pagination appends results without losing an unavailable selected ID', async () => {
  const requests = [];
  const h = await page({ api: {
    getFeaturedCaseGroups: async () => ({ list: [group()], total: 1 }),
    getTemplateList: async params => {
      if (params.page_size !== 100) return { list: [], total: 0 };
      requests.push(params.page);
      return { list: [template({ id: params.page, name: `Option ${params.page}`, status: 'published' })], total: 2 };
    },
  } });
  await openGroup(h); await h.button('加载更多案例').props.onClick(); await h.flush();
  assert.deepEqual(requests, [1, 2]);
  const values = nodes(h.field('第一个案例 *')).filter(node => node.type === 'option').map(node => node.props.value);
  assert.deepEqual(values, ['', '1', '2', '88', '99']);
  assert.equal(h.field('第一个案例 *').props.value, '88');
});

test('failed featured catalog preserves current selection and retries without changing mode', async () => {
  let fail = true;
  const h = await page({ api: {
    getFeaturedCaseGroups: async () => ({ list: [group()], total: 1 }),
    getTemplateList: async params => { if (params.page_size === 100 && fail) throw new Error('catalog unavailable'); return { list: [], total: 0 }; },
  } });
  await openGroup(h);
  assert.equal(h.field('第一个案例 *').props.value, '88'); assert.equal(h.field('显示模式 *').props.value, 'normal');
  assert.match(textOf(h.modal()), /catalog unavailable/);
  fail = false; await h.button('重试加载案例').props.onClick(); await h.flush();
  assert.equal(h.field('第一个案例 *').props.value, '88'); assert.doesNotMatch(textOf(h.modal()), /catalog unavailable/);
});

test('a failed initial template read shows unknown totals instead of an empty result', async () => {
  const h = await page({ api: { getTemplateList: async () => { throw new Error('list unavailable'); } } });
  assert.match(textOf(h.render()), /尚不能确认列表为空/);
  assert.doesNotMatch(textOf(h.render()), /当前筛选下暂无模板数据|筛选结果共 0 个模板/);
  const stats = h.find(node => node.props?.className === 'templates-stats');
  assert.equal(nodes(stats).filter(node => node.props?.className === 'stat-value' && textOf(node) === '—').length, 4);
});

test('a page made invalid by a smaller total falls back to the last real page', async () => {
  const requests = [];
  const h = await page({ api: { getTemplateList: async params => { requests.push(params.page); return { list: [template()], total: params.page === 3 || requests.length > 3 ? 21 : 45, page: params.page, page_size: 20 }; } } });
  h.button('下一页模板').props.onClick(); await h.flush(); h.button('下一页模板').props.onClick(); await h.flush();
  assert.deepEqual(requests, [1, 2, 3, 2]);
  assert.match(textOf(h.render()), /第 2 \/ 2 页/);
});

test('template moderation is single-flight and read failure reports the committed result', async () => {
  const pending = deferred(); let reads = 0, writes = 0;
  const h = await page({ template: { status: 'pending' }, api: {
    getTemplateList: async () => { if (++reads > 1) throw new Error('refresh failed'); return { list: [template({ status: 'pending' })], total: 1 }; },
    updateTemplateStatus: () => { writes++; return pending.promise; },
  } });
  const approve = h.find(node => node.type === 'button' && node.props.title === '通过').props.onClick;
  const first = approve(); await approve(); assert.equal(writes, 1);
  pending.resolve(); await first; await h.flush();
  assert.match(textOf(h.render()), /模板已审核通过，但列表刷新失败/);
  assert.equal(h.alerts.length, 0);
  const card = h.find(node => node.props?.className === 'template-card'); assert.match(textOf(card), /已发布/);
});

test('late category move completion cannot close a newer move dialog', async () => {
  const pending = deferred();
  const h = await page({ api: {
    getTemplateList: async () => ({ list: [template({ id: 1, name: 'First' }), template({ id: 2, name: 'Second' })], total: 2 }),
    getTemplateCategories: async () => [{ id: 'villa', name: 'Villa' }, { id: 'urban', name: 'Urban' }],
    updateTemplateCategory: () => pending.promise,
  } });
  const move = id => {
    const card = h.find(node => node.key === id && node.props?.className === 'template-card');
    nodes(card).find(node => node.type === 'button' && node.props.title === '移动分类').props.onClick(); h.render();
  };
  move('1'); const save = h.button('Urban').props.onClick();
  h.find(node => node.type === 'button' && node.props.className === 'modal-close').props.onClick(); h.render();
  move('2'); pending.resolve(); await save; await h.flush();
  assert.match(textOf(h.render()), /移动“Second”到分类/);
});

test('submitted editor fields remain locked while an unrelated row action is pending', async () => {
  const saving = deferred(), moderation = deferred();
  const h = await page({ api: {
    getTemplateList: async () => ({ list: [template({ id: 1 }), template({ id: 2, status: 'pending' })], total: 2 }),
    updateTemplate: () => saving.promise,
    updateTemplateStatus: () => moderation.promise,
  } });
  await h.edit(); h.change('名称 *', 'Submitted');
  const write = h.button('保存').props.onClick(); h.render();
  const card = h.find(node => node.key === '2' && node.props?.className === 'template-card');
  const other = nodes(card).find(node => node.type === 'button' && node.props.title === '通过').props.onClick(); h.render();
  assert.equal(nodes(h.modal()).find(node => node.type === 'fieldset' && node.props.className === 'modal-body').props.disabled, true);
  assert.equal(h.button('保存').props.disabled, true);
  saving.resolve(template({ name: 'Submitted' })); moderation.resolve(); await Promise.all([write, other]); await h.flush();
});

test('URL templateId locates the exact object outside the current page without writing', async () => {
  const reads = [], writes = [];
  const h = await page({ search: '?templateId=42', api: {
    getTemplateDetail: async id => { reads.push(id); return template({ id: Number(id), name: 'Exact object' }); },
    updateTemplate: async (id, patch) => { writes.push({ id, patch }); return template({ id: Number(id), ...patch }); },
  } });
  const target = h.find(node => node.props?.className === 'section-card linked-template');
  assert.match(textOf(target), /指定模板 #42.*Exact object/); assert.deepEqual(reads, ['42']); assert.equal(writes.length, 0);
  await h.button('编辑指定模板').props.onClick(); await h.flush();
  assert.equal(h.field('名称 *').props.value, 'Exact object'); assert.deepEqual(reads, ['42', '42']);
  h.change('名称 *', 'Edited exact object'); await h.button('保存').props.onClick(); await h.flush();
  assert.equal(writes.length, 1); assert.equal(writes[0].id, '42');
});

test('invalid templateId values never become a detail request or a name search', async () => {
  for (const id of ['0', '-1', '1.5', 'name', '01', '9007199254740992', '']) {
    let detailReads = 0;
    const h = await page({ search: `?templateId=${id}`, api: { getTemplateDetail: async () => { detailReads++; return template(); } } });
    assert.equal(detailReads, 0); assert.match(textOf(h.render()), /templateId 必须是可精确表示的正整数/);
    assert.equal(h.nodes().filter(node => node.type === 'button' && textOf(node) === '编辑指定模板').length, 0);
  }
});

test('URL response ID mismatch or read failure never opens an unrelated object', async () => {
  for (const read of [async () => template({ id: 99 }), async () => { throw new Error('not found'); }]) {
    const h = await page({ search: '?templateId=42', api: { getTemplateDetail: read } });
    assert.match(textOf(h.render()), /无法加载指定模板 #42/);
    assert.equal(h.nodes().filter(node => node.props?.className === 'modal-overlay').length, 0);
    assert.equal(h.nodes().filter(node => node.type === 'button' && textOf(node) === '编辑指定模板').length, 0);
  }
});

test('late URL lookup cannot replace a newer ID, interrupt add, or update after unmount', async () => {
  const old = deferred(), current = deferred();
  const h = await page({ search: '?templateId=41', api: { getTemplateDetail: id => id === '41' ? old.promise : current.promise } });
  h.setSearch('?templateId=42'); await h.flush(); h.add();
  old.resolve(template({ id: 41, name: 'Old link' })); await h.flush();
  assert.doesNotMatch(textOf(h.render()), /Old link/); assert.equal(h.field('名称 *').props.value, '');
  current.resolve(template({ id: 42, name: 'Current link' })); await h.flush();
  assert.match(textOf(h.render()), /Current link/); assert.equal(h.field('名称 *').props.value, '');
  const pending = deferred();
  const unmounted = await page({ search: '?templateId=43', api: { getTemplateDetail: () => pending.promise } });
  unmounted.unmount(); pending.resolve(template({ id: 43, name: 'After unmount' })); await settle();
  assert.doesNotMatch(textOf(unmounted.render()), /After unmount/);
});

test('incomplete successful responses do not become empty template or tab state', async () => {
  const h = await page({ api: { getTemplateList: async () => ({ list: [] }), getTemplateTabConfig: async () => undefined } });
  assert.match(textOf(h.render()), /响应缺少有效列表或总数/);
  await openTabs(h);
  assert.equal(h.button('保存标签配置').props.disabled, true);
  assert.match(textOf(h.render()), /标签配置响应不完整/);
});

test('an existing rejected status remains selectable when reverting an unsaved status edit', async () => {
  const h = await page(); await h.edit();
  h.change('状态', 'draft');
  assert.ok(nodes(h.field('状态')).some(node => node.type === 'option' && node.props.value === 'rejected'));
  h.change('状态', 'rejected'); await h.button('保存').props.onClick(); await h.flush();
  assert.equal(h.saves.length, 0);
});

test('closed and same-ID reopened featured editors reject saved callbacks from the old session', async () => {
  const writes = [];
  const h = await page({ api: {
    getFeaturedCaseGroups: async () => ({ list: [group()], total: 1 }),
    updateFeaturedCaseGroup: async (...args) => { writes.push(args); },
  } });
  await openGroup(h);
  const staleSave = h.button('保存').props.onClick;
  const staleChange = h.field('组名称 *').props.onChange;
  const staleClose = h.button('取消').props.onClick;
  staleClose(); await staleSave(); h.render();
  assert.equal(writes.length, 0, 'cancel must invalidate synchronously before another render');
  const card = h.find(node => node.key === '7' && node.props?.className === 'featured-group-item');
  nodes(card).find(node => node.type === 'button' && node.props.title === '编辑').props.onClick(); await h.flush();
  h.change('组名称 *', 'Current draft');
  staleChange({ target: { value: 'Old draft' } }); staleClose(); await staleSave(); await h.flush();
  assert.equal(writes.length, 0); assert.equal(h.field('组名称 *').props.value, 'Current draft');
});

test('a cancelled new featured group cannot be created by its old save callback', async () => {
  let writes = 0;
  const h = await page({ api: { createFeaturedCaseGroup: async () => { writes++; } } });
  h.button('精选案例组管理').props.onClick(); await h.flush();
  h.button('新建案例组').props.onClick(); await h.flush();
  h.change('组名称 *', 'Old create'); h.change('显示模式 *', 'normal'); h.change('第一个案例 *', '1');
  const oldSave = h.button('保存').props.onClick;
  h.cancel(); h.button('新建案例组').props.onClick(); await h.flush();
  await oldSave(); await h.flush();
  assert.equal(writes, 0); assert.equal(h.field('组名称 *').props.value, '');
});

test('add and same-ID edit sessions reject old save, upload, change and close callbacks', async () => {
  for (const mode of ['add', 'edit']) {
    const h = await page();
    if (mode === 'add') h.add(); else await h.edit();
    h.change('名称 *', 'Old draft');
    const save = h.button('保存').props.onClick, close = h.button('取消').props.onClick;
    const change = h.field('名称 *').props.onChange, upload = h.file().props.onChange;
    close(); await save(); assert.equal(h.saves.length + h.creates.length, 0); h.render();
    if (mode === 'add') h.add(); else await h.edit();
    h.change('名称 *', 'Current draft');
    change({ target: { value: 'Stale change' } }); close(); await save();
    const input = { files: [new Blob(['old'])], value: 'old.jpg' }; await upload({ currentTarget: input, target: input }); h.render();
    assert.equal(h.saves.length + h.creates.length, 0); assert.equal(h.uploads.length, 0);
    assert.equal(h.field('名称 *').props.value, 'Current draft');
  }
});

test('a stale move confirmation cannot submit after cancel or same-ID reopening', async () => {
  let writes = 0;
  const h = await page({ api: {
    getTemplateCategories: async () => [{ id: 'villa', name: 'Villa' }, { id: 'urban', name: 'Urban' }],
    updateTemplateCategory: async () => { writes++; },
  } });
  const open = () => { h.find(node => node.type === 'button' && node.props.title === '移动分类').props.onClick(); h.render(); };
  open(); const oldConfirm = h.button('Urban').props.onClick;
  const oldClose = h.find(node => node.type === 'button' && node.props.className === 'modal-close').props.onClick;
  oldClose(); await oldConfirm(); h.render(); open(); oldClose(); await oldConfirm(); h.render();
  assert.equal(writes, 0); assert.match(textOf(h.render()), /移动“Existing”到分类/);
});

test('changing URL ID hides the old linked object before effects and rejects its old entry', async () => {
  const reads = [];
  const h = await page({ search: '?templateId=1', api: { getTemplateDetail: async id => { reads.push(id); return template({ id: Number(id), name: `Linked ${id}` }); } } });
  const staleOpen = h.button('编辑指定模板').props.onClick;
  const staleRefresh = h.button('刷新指定模板').props.onClick;
  h.setSearch('?templateId=2');
  const panel = h.find(node => node.props?.['aria-label'] === '指定模板定位');
  assert.doesNotMatch(textOf(panel), /Linked 1/);
  assert.equal(nodes(panel).filter(node => node.type === 'button' && textOf(node) === '编辑指定模板').length, 0);
  await staleOpen(); await staleRefresh(); assert.deepEqual(reads, ['1']);
  await h.flush(); assert.deepEqual(reads, ['1', '2']); assert.match(textOf(h.render()), /Linked 2/);
});

test('browser URL changes invalidate old linked entry and save even before rendering', async () => {
  const reads = [], writes = [];
  const h = await page({ search: '?templateId=1', api: {
    getTemplateDetail: async id => { reads.push(id); return template({ id: Number(id), name: `Linked ${id}` }); },
    updateTemplate: async (...args) => { writes.push(args); return template(); },
  } });
  const staleOpen = h.button('编辑指定模板').props.onClick;
  await staleOpen(); await h.flush(); h.change('名称 *', 'Old ID draft');
  const staleSave = h.button('保存').props.onClick;
  h.setSearchWithoutRender('?templateId=2');
  await staleOpen(); await staleSave();
  assert.deepEqual(reads, ['1', '1']); assert.equal(writes.length, 0);
  h.render(); assert.equal(h.nodes().filter(node => node.props?.className === 'modal-overlay').length, 0);
  await h.flush(); assert.match(textOf(h.render()), /Linked 2/);
});

test('a linked edit detail response cannot open after the URL target changes', async () => {
  const pending = deferred(); let firstReads = 0;
  const h = await page({ search: '?templateId=1', api: { getTemplateDetail: async id => id === '1' && ++firstReads > 1 ? pending.promise : template({ id: Number(id), name: `Linked ${id}` }) } });
  const edit = h.button('编辑指定模板').props.onClick();
  h.setSearch('?templateId=2'); await h.flush(); pending.resolve(template({ id: 1, name: 'Old editor' })); await edit; await h.flush();
  assert.equal(h.nodes().filter(node => node.props?.className === 'modal-overlay').length, 0);
  assert.doesNotMatch(textOf(h.render()), /Old editor/); assert.match(textOf(h.render()), /Linked 2/);
});
