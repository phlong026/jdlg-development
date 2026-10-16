// Execute the production TSX, search panel and API validators with controlled
// hooks and deferred GETs. No browser, server, or production requests are used.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import vm from 'node:vm';
import { createRequire } from 'node:module';
import test from 'node:test';
import React from 'react';
import ts from 'typescript';

const require = createRequire(import.meta.url);
const root = new URL('../src/', import.meta.url);
const settle = () => new Promise(resolve => setImmediate(resolve));
const nodes = node => Array.isArray(node) ? node.flatMap(nodes) : node && typeof node === 'object' ? [node, ...nodes(node.props?.children)] : [];
const textOf = node => Array.isArray(node) ? node.map(textOf).join('') : node == null || typeof node === 'boolean' ? '' : typeof node === 'object' ? textOf(node.props?.children) : String(node);
const endpoints = {
  OVERVIEW: '/overview', DOWNLOAD_RANKING: '/download-ranking', ENGAGEMENT_RANKING: '/engagement-ranking',
  NEW_TEMPLATES: '/new-templates', LOW_CONVERSION: '/low-conversion', FEATURED_CASES: '/featured-cases',
};
const list = items => ({ list: items, total: items.length, page: 1, page_size: 10 });
const template = (overrides = {}) => ({
  id: 31, name: 'Same template name', category: 'villa', creator: 'Designer', source_type: 'upload',
  is_free: true, price: 0, download_count: 12, like_count: 2, comment_count: 1, share_count: 3,
  unlock_count: 0, engagement_score: 6, created_at: '2026-10-08T10:00:00Z', ...overrides,
});
const featured = (overrides = {}) => ({
  id: 999, name: 'Group name', display_mode: 'comparison', combined_download_count: 20,
  combined_engagement_score: 8, updated_at: '2026-10-08T10:00:00Z',
  case1: { id: 201, name: 'Same template name', download_count: 12, engagement_score: 5 },
  case2: { id: 202, name: 'Same template name', download_count: 8, engagement_score: 3 }, ...overrides,
});
const fixture = (overrides = {}) => ({
  '/overview': { total_templates: 50, published_templates: 25, total_downloads: 120, total_unlocks: 2,
    total_interactions: 80, week_new_templates: 7, featured_case_group_count: 1 },
  '/download-ranking': list([template()]), '/engagement-ranking': list([template()]),
  '/new-templates': list([template()]), '/low-conversion': list([template({ conversion_type: 'download', conversion_count: 1, conversion_rate: 0.1 })]),
  '/featured-cases': list([featured()]), ...overrides,
});

function mount() {
  const slots = [], effects = [], requests = [], navigations = [], reports = [], recoveries = [];
  let cursor = 0, dirty = false, tree, unmounted = false, updatesAfterUnmount = 0;
  const hooks = {
    ...React,
    useState(initial) {
      const index = cursor++;
      if (!(index in slots)) slots[index] = typeof initial === 'function' ? initial() : initial;
      return [slots[index], update => {
        if (unmounted) updatesAfterUnmount++;
        const next = typeof update === 'function' ? update(slots[index]) : update;
        if (!Object.is(slots[index], next)) { slots[index] = next; dirty = true; }
      }];
    },
    useRef(initial) { const index = cursor++; if (!(index in slots)) slots[index] = { current: initial }; return slots[index]; },
    useEffect(effect, deps) {
      const index = cursor++, previous = slots[index];
      if (!previous || deps.some((value, n) => !Object.is(value, previous.deps[n]))) {
        slots[index] = { deps, cleanup: previous?.cleanup };
        effects.push(() => { slots[index].cleanup?.(); slots[index].cleanup = effect(); });
      }
    },
  };
  const cache = {};
  function load(filename) {
    if (cache[filename]) return cache[filename];
    const code = ts.transpileModule(fs.readFileSync(new URL(filename, root), 'utf8'), {
      fileName: filename,
      compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.CommonJS, jsx: ts.JsxEmit.ReactJSX, esModuleInterop: true },
    }).outputText;
    const module = { exports: {} };
    vm.runInNewContext(code, {
      module, exports: module.exports, AbortController, console: { error() {} },
      require(name) {
        if (name === 'react') return hooks;
        if (name === 'react/jsx-runtime') return require(name);
        if (name === 'react-router-dom') return { useNavigate: () => target => navigations.push(target) };
        if (name === 'react-icons/fi') return new Proxy({}, { get: () => 'icon' });
        if (name === '../utils/request') return { request: (endpoint, config) => new Promise((resolve, reject) => requests.push({ endpoint, ...config, resolve, reject })) };
        if (name === '../config/api') return { API_ENDPOINTS: { CONTENT_ANALYTICS: endpoints } };
        if (name === '../utils/pageLoadError') return { reportPageLoadError: (...args) => reports.push(args), reportPageLoadRecovered: (...args) => recoveries.push(args) };
        if (name.includes('useIsMobile')) return {useIsMobile:()=>false};
  if (name.endsWith('.scss')) return {};
        if (name.startsWith('.')) return load(path.posix.normalize(path.posix.join(path.posix.dirname(filename), name)) + (name.includes('/api/') ? '.ts' : '.tsx'));
        throw new Error(`Unexpected dependency: ${name}`);
      },
    }, { filename });
    cache[filename] = module.exports;
    return module.exports;
  }
  const Component = load('pages/contentAnalytics.tsx').default;
  function expand(node) {
    if (Array.isArray(node)) return node.map(expand);
    if (!node || typeof node !== 'object') return node;
    if (typeof node.type === 'function') return expand(node.type(node.props));
    return { ...node, props: { ...node.props, children: expand(node.props?.children) } };
  }
  const page = {
    requests, navigations, reports, recoveries,
    get updatesAfterUnmount() { return updatesAfterUnmount; },
    render() {
      let cycles = 0;
      do {
        assert.ok(cycles++ < 20, 'Render must settle');
        cursor = 0; dirty = false; tree = expand(Component()); effects.splice(0).forEach(effect => effect());
      } while (dirty);
      return tree;
    },
    all: predicate => nodes(tree).filter(predicate),
    text: () => textOf(tree),
    button(label) { const button = page.all(n => n.type === 'button' && textOf(n).trim() === label)[0]; assert.ok(button, `Missing button: ${label}`); return button; },
    click(label) { const button = page.button(label); assert.ok(!button.props.disabled); button.props.onClick(); page.render(); },
    enter(value) { const input = page.all(n => n.type === 'input')[0]; input.props.onChange({ target: { value } }); page.render(); page.all(n => n.type === 'input')[0].props.onKeyDown({ key: 'Enter' }); page.render(); },
    batch(index = 0) { const batch = requests.slice(index * 6, (index + 1) * 6); assert.equal(batch.length, 6); return batch; },
    async resolve(index = 0, data = fixture()) { page.batch(index).forEach(request => request.resolve({ code: 0, data: data[request.endpoint] })); await settle(); page.render(); },
    async reject(index = 0, endpoint = '/overview') { page.batch(index).find(request => request.endpoint === endpoint).reject(new Error('Fixture read failure')); await settle(); page.render(); },
    unmount() { slots.forEach(slot => slot?.cleanup?.()); unmounted = true; },
  };
  page.render();
  return page;
}

// These tests deliberately call the actual button callbacks (including disabled
// callbacks in hostile-input cases) rather than reimplementing link logic.
test('analytics links use exact template IDs, even when names match and group ID differs', async () => {
  const page = mount();
  await page.resolve(0, fixture({ '/download-ranking': list([template({ id: 31 }), template({ id: 32 })]) }));
  page.all(n => n.type === 'button' && textOf(n) === '查看模板详情').forEach(n => n.props.onClick());
  page.click('查看主案例'); page.click('查看对比案例');
  assert.deepEqual(page.navigations, ['/templates?templateId=31', '/templates?templateId=32', '/templates?templateId=31', '/templates?templateId=31', '/templates?templateId=31', '/templates?templateId=201', '/templates?templateId=202']);
  assert.ok(page.all(n => n.type === 'button' && textOf(n) === '查看模板详情').every(n => n.props.title.includes('只读查看模板')));
});

test('analytics rejects invalid and unsafe IDs without name, group, or broad-list fallbacks', async () => {
  const invalid = [0, -1, 1.5, '31', '', null, undefined, NaN, Infinity, Number.MAX_SAFE_INTEGER + 1];
  const page = mount();
  await page.resolve(0, fixture({
    '/download-ranking': list(invalid.map((id, index) => template({ id, name: `invalid ${index}` })).concat(template({ id: Number.MAX_SAFE_INTEGER }))),
    '/engagement-ranking': list([]), '/new-templates': list([]), '/low-conversion': list([]),
    '/featured-cases': list([featured({ case1: undefined, case2: { id: 0, name: 'Invalid comparison' } })]),
  }));
  const links = page.all(n => n.type === 'button' && textOf(n) === '查看模板详情');
  for (const button of links.slice(0, invalid.length)) {
    assert.equal(button.props.disabled, true); assert.match(button.props.title, /ID 缺失或无效/); button.props.onClick();
  }
  for (const label of ['查看主案例', '查看对比案例']) { const button = page.button(label); assert.equal(button.props.disabled, true); button.props.onClick(); }
  assert.equal(page.navigations.length, 0);
  assert.equal(links.at(-1).props.disabled, false); links.at(-1).props.onClick();
  assert.deepEqual(page.navigations, [`/templates?templateId=${Number.MAX_SAFE_INTEGER}`]);
});

test('featured case links remain exact when template names are absent', async () => {
  const page = mount();
  await page.resolve(0, fixture({ '/featured-cases': list([featured({ case1: { id: 7, name: '' }, case2: { id: 8, name: '' } })]) }));
  page.click('查看主案例'); page.click('查看对比案例');
  assert.deepEqual(page.navigations, ['/templates?templateId=7', '/templates?templateId=8']);
});

test('creator is explicitly same-name search and missing/system values do not imply designer identity', async () => {
  const page = mount();
  await page.resolve(0, fixture({
    '/download-ranking': list(['  A & B / 甲  ', '', '   ', '系统', ' 系统 ', null, undefined, 123].map((creator, index) => template({ id: index + 1, creator }))),
    '/engagement-ranking': list([]), '/new-templates': list([]), '/low-conversion': list([]),
  }));
  const buttons = page.all(n => n.type === 'button' && textOf(n) === '按创建者同名搜索');
  assert.equal(buttons.length, 8); assert.match(buttons[0].props.title, /不能确认是同一位设计师/);
  buttons.forEach(button => button.props.onClick());
  assert.deepEqual(page.navigations, [`/designer-center?keyword=${encodeURIComponent('A & B / 甲')}`]);
  assert.ok(buttons.slice(1).every(button => button.props.disabled));
  assert.match(page.text(), /同名搜索需自行核对设计师身份/);
});

test('analytics copy matches creation-time scopes and does not alter backend query filters', async () => {
  const page = mount(); await page.resolve();
  assert.match(page.text(), /每个排行及模板观察区最多显示 10 条，精选案例最多显示 10 组，均非完整清单/);
  assert.match(page.text(), /近 7 天新创建模板/); assert.match(page.text(), /全部状态的模板，含未发布/);
  assert.match(page.text(), /近 14 天新创建模板表现/); assert.match(page.text(), /仅展示已发布且创建于近 14 天/);
  assert.equal(page.all(n => n.type === 'th' && textOf(n) === '创建时间').length, 4);
  assert.equal(page.all(n => n.type === 'th' && textOf(n).includes('上架')).length, 0);
  page.enter('  villa  ');
  for (const request of page.batch(1)) {
    assert.equal(request.method, 'GET');
    if (request.endpoint === '/overview') assert.equal(request.params, undefined);
    else assert.deepEqual(JSON.parse(JSON.stringify(request.params)), { page: 1, page_size: 10, keyword: 'villa' });
  }
});

test('initial and failed reads never look like zero metrics or empty rankings, and retry recovers', async () => {
  const page = mount();
  assert.match(page.text(), /正在加载内容运营分析/);
  assert.equal(page.all(n => n.props?.className === 'stat-value' || n.type === 'table').length, 0);
  await page.reject(0, '/new-templates');
  assert.match(page.text(), /读取失败/); assert.doesNotMatch(page.text(), /暂无数据|暂无精选案例数据/);
  assert.equal(page.all(n => n.props?.className === 'stat-value' || n.type === 'table').length, 0);
  assert.ok(page.batch(0).every(request => request.signal.aborted), 'failure cancels unfinished sibling reads');
  assert.equal(page.reports.length, 1);
  page.click('重新加载'); await page.resolve(1);
  assert.match(page.text(), /全部模板库存 50/); assert.equal(page.recoveries.length, 1);
  assert.equal(page.reports[0][1], page.recoveries[0][0]);
});

test('refresh hides old rows, coalesces same-render retries, and failed refresh stays unavailable', async () => {
  const page = mount(); await page.resolve();
  const refresh = page.button('刷新').props.onClick;
  refresh(); refresh(); page.render();
  assert.equal(page.requests.length, 12); assert.equal(page.button('刷新中...').props.disabled, true);
  assert.doesNotMatch(page.text(), /Same template name|暂无数据/);
  await page.reject(1); assert.match(page.text(), /读取失败/); assert.doesNotMatch(page.text(), /Same template name/);
  page.enter(''); assert.equal(page.requests.length, 18, 'same-query search retries after failure');
  await page.resolve(2); assert.match(page.text(), /Same template name/);
});

test('older query success and failure cannot overwrite newer rows, loading state, or feedback', async () => {
  const page = mount(); page.enter('new');
  assert.ok(page.batch(0).every(request => request.signal.aborted));
  await page.resolve(0); assert.match(page.text(), /正在加载内容运营分析/); assert.doesNotMatch(page.text(), /Same template name/);
  assert.equal(page.recoveries.length, 0);
  await page.resolve(1, fixture({ '/download-ranking': list([template({ id: 77, name: 'Newest result' })]) }));
  assert.match(page.text(), /Newest result/);
  page.enter('older'); page.enter('latest');
  await page.resolve(3, fixture({ '/download-ranking': list([template({ id: 88, name: 'Latest result' })]) }));
  await page.reject(2);
  assert.match(page.text(), /Latest result/); assert.doesNotMatch(page.text(), /读取失败|正在加载内容运营分析/);
  assert.equal(page.reports.length, 0); assert.equal(page.recoveries.length, 2);
});

test('batched search then reset starts the reset query even when the final keyword equals initial state', async () => {
  const page = mount();
  const input = page.all(n => n.type === 'input')[0]; input.props.onChange({ target: { value: 'transient' } }); page.render();
  const search = page.button('搜索内容').props.onClick, reset = page.button('重置筛选').props.onClick;
  search(); reset(); page.render();
  assert.equal(page.requests.length, 18);
  assert.ok(page.batch(0).every(request => request.signal.aborted)); assert.ok(page.batch(1).every(request => request.signal.aborted));
  assert.ok(page.batch(2).filter(request => request.params).every(request => request.params.keyword === ''));
  await page.resolve(2); await page.resolve(1); await page.resolve(0);
  assert.doesNotMatch(page.text(), /关键词：transient|正在加载内容运营分析/); assert.equal(page.recoveries.length, 1);
});

test('unmount aborts all analytics reads and suppresses late success and failure feedback', async () => {
  for (const outcome of ['success', 'failure']) {
    const page = mount(); page.unmount();
    assert.ok(page.batch().every(request => request.signal.aborted));
    if (outcome === 'success') page.batch().forEach(request => request.resolve({ code: 0, data: fixture()[request.endpoint] }));
    else page.batch()[0].reject(new Error('Late failure'));
    await settle();
    assert.equal(page.updatesAfterUnmount, 0); assert.equal(page.reports.length, 0); assert.equal(page.recoveries.length, 0);
  }
});

test('real API validators reject incomplete overview and malformed list payloads instead of substituting zeros/empty', async () => {
  for (const [endpoint, payload] of [
    ['/overview', {}], ['/overview', { ...fixture()['/overview'], week_new_templates: null }],
    ['/download-ranking', { total: 0, page: 1, page_size: 10 }], ['/engagement-ranking', { ...list([]), list: null }],
    ['/featured-cases', { ...list([]), list: [null] }], ['/new-templates', { ...list([]), total: -1 }],
    ['/low-conversion', { ...list([]), page_size: 0 }],
  ]) {
    const page = mount(); await page.resolve(0, fixture({ [endpoint]: payload }));
    assert.match(page.text(), /读取失败/); assert.doesNotMatch(page.text(), /暂无数据|暂无精选案例数据/);
    assert.equal(page.all(n => n.props?.className === 'stat-value').length, 0); assert.equal(page.reports.length, 1);
  }
});

test('successful empty lists and known zero overview remain legitimate data', async () => {
  const page = mount();
  const data = Object.fromEntries(Object.values(endpoints).map(endpoint => [endpoint, endpoint === '/overview' ? Object.fromEntries(Object.keys(fixture()['/overview']).map(key => [key, 0])) : list([])]));
  await page.resolve(0, data);
  assert.match(page.text(), /全部模板库存 0/); assert.match(page.text(), /暂无精选案例数据/);
  assert.equal(page.all(n => n.type === 'table').length, 5); assert.equal(page.reports.length, 0);
});

test('unmounted analytics callbacks cannot restart GETs or navigate', async () => {
  const page = mount(); await page.resolve();
  const refresh = page.button('刷新').props.onClick;
  const search = page.button('搜索内容').props.onClick;
  const reset = page.button('重置筛选').props.onClick;
  const templateLink = page.button('查看模板详情').props.onClick;
  const creatorLink = page.button('按创建者同名搜索').props.onClick;
  page.unmount(); refresh(); search(); reset(); templateLink(); creatorLink();
  assert.equal(page.requests.length, 6);
  assert.equal(page.updatesAfterUnmount, 0);
  assert.deepEqual(page.navigations, []);
});
