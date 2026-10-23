import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import ts from 'typescript';
import { createRequire } from 'node:module';

const require = createRequire(import.meta.url);
const React = require('react');
const { renderToStaticMarkup } = require('react-dom/server');
const root = new URL('../', import.meta.url);
function load(file, overrides = {}) {
  const module = { exports: {} };
  const code = ts.transpileModule(fs.readFileSync(new URL(file, root), 'utf8'), { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020, jsx: ts.JsxEmit.ReactJSX, esModuleInterop: true } }).outputText;
  vm.runInNewContext(code, { module, exports: module.exports, require: name => name in overrides ? overrides[name] : require(name), Date, URL, Intl, Error, Math, AbortController, AbortSignal, setTimeout, clearTimeout, JSON, window: { location: { origin: 'https://example.test', protocol: 'https:' } } });
  return module.exports;
}
const model = load('src/pages/shareTracking/model.ts');
const normalized = value => JSON.parse(JSON.stringify(value));

test('identified accounts without nicknames have a stable account label, anonymous visitors stay anonymous', () => {
  assert.equal(model.visitorName({ visitor_key: 'u:293', identity_state: 'identified', nickname: '' }), '用户 #293 · 未设置昵称');
  assert.equal(model.visitorName({ visitor_key: 'u:293', identity_state: 'identified', nickname: ' 林间访客 ' }), '林间访客');
  assert.match(model.visitorName({ visitor_key: 'a:random', identity_state: 'anonymous', nickname: '不可使用的账号昵称' }), /^匿名访客/);
  assert.equal(model.visitorName({ visitor_key: 'u:293', identity_state: 'deleted', nickname: '旧昵称' }), '已删除账号');
});

test('customer timeline query sends source channel and drops list-only sort', () => {
  const filters = { ...model.defaultFilters, range: 'all', intent: 'high', channel: 'timeline', q: ' 雪峰 ', sort: 'visible_ms', direction: 'asc' };
  assert.deepEqual(normalized(model.queryFor(filters)), { identity: '', intent: 'high', q: '雪峰', channel: 'timeline', owner: '', limit: 20 });
  const contents = normalized(model.queryFor(filters, new Date(), 'contents'));
  assert.equal(contents.channel, undefined); assert.equal(contents.sort, 'visible_ms');
});

test('default range is the last 30 days and channel defaults to all', () => {
  assert.equal(model.defaultFilters.range, '30');
  assert.equal(model.defaultFilters.channel, '');
});

test('query range feedback renders the recovery action instead of a blind retry', () => {
  const components = load('src/pages/shareTracking/components.tsx', { react: React, './model': model, './evidence': { IntentEvidenceDetails: () => null } });
  const html = renderToStaticMarkup(React.createElement(components.Feedback, {
    error: '查询范围过大，请缩小时间或归属、素材、访客范围',
    status: 422,
    retry() {},
    rangeRecovery: { label: '改看近7天', onClick() {} },
  }));
  assert.match(html, /改看近7天/);
  assert.doesNotMatch(html, /重试，保留筛选/);
});

test('source labels follow the timeline rules', () => {
  assert.equal(model.sourceLabel({ kind: 'share', channel: 'app_message' }), '私发');
  assert.equal(model.sourceLabel({ kind: 'share', channel: 'timeline' }), '朋友圈');
  assert.equal(model.sourceLabel({ kind: 'share' }), '未记录');
  assert.equal(model.sourceLabel({ kind: 'code_link' }), '扫码·链接');
  assert.equal(model.sourceLabel({ kind: 'direct' }), '直接访问');
  assert.equal(model.sourceLabel({ kind: 'unknown', reason: 'unattributed' }), '来源未确认');
  assert.equal(model.sourceLabel({ kind: 'unknown', reason: 'invalid_share' }), '分享链接无效');
  assert.equal(model.sourceLabel({ kind: 'unknown', reason: 'expired_share' }), '分享链接已过期');
  assert.equal(model.sourceLabel({ kind: 'unknown', reason: 'revoked_share' }), '分享链接已停用');
  assert.equal(model.sourceLabel({ kind: 'unknown', reason: 'mismatched_share' }), '分享链接与页面不符');
  assert.ok(model.sourceOptions.some(([value, label]) => value === 'unknown' && label === '来源未确认'));
  assert.equal(model.queryFor({ ...model.defaultFilters, range: 'all', channel: 'unknown' }).channel, 'unknown');
  assert.equal(model.forwarderText({ kind: 'share', forwarder: { visitor_key: 'u:3', nickname: '心随我动', identity_state: 'identified' } }), '（心随我动转发）');
  assert.equal(model.forwarderText({ kind: 'share', forwarder: { visitor_key: 'a:x', nickname: '匿名访客 abcdef', identity_state: 'anonymous' } }), '（匿名访客转发）');
  assert.equal(model.forwarderText({ kind: 'share' }), '');
});

test('progress text: capped video with replays, article depth, nothing without data', () => {
  assert.equal(model.progressText({ kind: 'video', percent: 100, plays: 3 }), '进度100% · 看了3遍');
  assert.equal(model.progressText({ kind: 'video', percent: 80 }), '进度80%');
  assert.equal(model.progressText({ kind: 'article', percent: 80 }), '读到80%');
  assert.equal(model.progressText(undefined), '');
});

test('visit time is Beijing month-day within the year and adds the year otherwise', () => {
  const now = new Date('2026-10-21T00:00:00Z');
  assert.equal(model.visitTime('2026-10-20T22:53:00Z', now), '10-21 06:53');
  // UTC new year's eve is already Beijing 2026, the current year.
  assert.equal(model.visitTime('2025-12-31T17:00:00Z', now), '01-01 01:00');
  assert.equal(model.visitTime('2025-06-01T02:05:00Z', now), '2025-06-01 10:05');
  assert.equal(model.visitTime('bad', now), '时间不可用');
  assert.equal(model.visitRange('2026-10-20T22:53:00Z', '2026-10-20T23:02:00Z', now), '10-21 06:53–07:02');
  assert.equal(model.visitRange('2026-10-20T15:00:00Z', '2026-10-21T01:00:00Z', now), '10-20 23:00–10-21 09:00');
  assert.equal(model.visitRange('2026-10-20T22:53:00Z', undefined, now), '10-21 06:53');
  assert.equal(model.intentShortLabels.high, '高意向'); assert.equal(model.intentShortLabels.low, '低关注');
});

function renderCard(visit, props = {}) {
  const components = { MediaPreview: ({ title }) => React.createElement('span', { className: 'media' }, title) };
  const card = load('src/pages/shareTracking/visitCard.tsx', {
    react: React, './model': model, './components': components,
    './convertLead': { ConvertLeadButton: ({ visitor, canCRM }) => canCRM && visitor.identity_type === '乡墅业主' ? React.createElement('button', null, '转为销售线索') : null },
  });
  return renderToStaticMarkup(React.createElement(card.VisitCard, { visit, onOpen() {}, canCRM: true, now: new Date('2026-10-21T00:00:00Z'), ...props }));
}
const baseVisit = {
  visit_id: 's1:content1:1', started_at: '2026-10-20T22:53:00Z', visible_ms: 50000, content_page_views: 2, engaged: false,
  visitor: { visitor_key: 'u:2', nickname: '雪峰行者', avatar_url: 'https://example.test/a.png', identity_state: 'identified', identity_type: '乡墅业主', risk_tags: ['同设备多账号'], last_visit: '', page_views: 2, share_count: 0, visible_ms: 0, session_count: 1, intent: { level: 'high', reasons: [], rule_version: 'r', window_start: '', window_end: '' } },
  content: { content_id: 'content1', content_version: 1, title: '南安石堤寺规划，山林溪流间的禅意布局', type: 'video', status: 'published', content_accessible: true },
  source: { kind: 'share', channel: 'timeline', forwarder: { visitor_key: 'u:3', nickname: '心随我动', identity_state: 'identified' } },
  progress: { kind: 'video', percent: 80 },
};

test('visit card shows the five reference modules', () => {
  const html = renderCard(baseVisit);
  for (const want of ['雪峰行者', '（心随我动转发）', '高意向', '南安石堤寺规划', '来源:朋友圈', '10-21 06:53', '本次观看50秒', '进度80%', '累计浏览2次', '乡墅业主', '同设备多账号', '转为销售线索', 'https://example.test/a.png']) {
    assert.ok(html.includes(want), `card missing ${want}: ${html}`);
  }
  assert.ok(!html.includes('看得认真'));
  assert.ok(html.includes('aria-label="复制昵称"'));
});

test('anonymous visit card falls back without copy, forwarder or lead button', () => {
  const html = renderCard({ ...baseVisit, engaged: true, progress: undefined, source: { kind: 'direct' }, visitor: { ...baseVisit.visitor, visitor_key: 'a:x', nickname: '匿名访客 abcdef', avatar_url: undefined, identity_state: 'anonymous', identity_type: undefined, risk_tags: undefined, intent: { ...baseVisit.visitor.intent, level: 'unknown' } } });
  for (const want of ['匿名访客', '待判断', '来源:直接访问', '看得认真']) assert.ok(html.includes(want), `card missing ${want}: ${html}`);
  for (const absent of ['复制昵称', '转发）', '转为销售线索', '进度', 'abcdef']) assert.ok(!html.includes(absent), `card should not show ${absent}`);
});

test('B-side visitor shows channel badge and no lead conversion', () => {
  const html = renderCard({ ...baseVisit, visitor: { ...baseVisit.visitor, identity_type: '设计师', risk_tags: undefined } });
  assert.ok(html.includes('B端 · 设计师'));
  assert.ok(!html.includes('转为销售线索'));
});

test('revoked material keeps the card but shows the safe title', () => {
  const html = renderCard({ ...baseVisit, content: { ...baseVisit.content, title: '素材已撤回', status: 'revoked', content_accessible: false } });
  assert.ok(html.includes('素材已撤回'));
});

test('batch card merges multiple materials and shows visit count', () => {
  const html = renderCard({
    ...baseVisit,
    visit_id: 'batch:u:2:s1:content1',
    ended_at: '2026-10-20T22:58:00Z',
    visible_ms: 90000,
    content_page_views: 5,
    visit_count: 2,
    group: 'batch',
    materials: [
      { content: baseVisit.content, started_at: baseVisit.started_at, ended_at: '2026-10-20T22:53:50Z', visible_ms: 50000, page_views: 3, progress: { kind: 'video', percent: 80 }, engaged: false },
      { content: { content_id: 'content2', content_version: 1, title: '闽南古厝改造', type: 'rich_text', status: 'published', content_accessible: true }, started_at: '2026-10-20T22:57:00Z', ended_at: '2026-10-20T22:58:00Z', visible_ms: 40000, page_views: 2, engaged: true },
    ],
  });
  for (const want of ['2 个素材', '2 次访问', '本次共1分30秒', '闽南古厝改造', '累计3次', '累计2次', '10-21 06:53–06:58', '看得认真']) {
    assert.ok(html.includes(want), `batch card missing ${want}: ${html}`);
  }
  assert.ok(html.includes('累计浏览5次'));
  assert.ok(html.includes('进度80%'));
});

test('visit card discloses an oversized progress fallback instead of looking like no progress events', () => {
  const html = renderCard({ ...baseVisit, progress: undefined, progress_omitted: true });
  assert.match(html, /进度因数据量过大未加载/);
});

test('plain public page cards show the route title and page presentation', () => {
  const html = renderCard({ ...baseVisit,
    content: { content_id: '', content_version: 0, route_key: 'pages/index/index', title: '首页', type: 'page', status: 'published', content_accessible: true },
    progress: undefined, materials: undefined,
  });
  assert.match(html, /首页/);
  assert.match(html, /页面/);
  assert.match(html, /本次停留/);
  assert.doesNotMatch(html, /素材暂不可访问|历史素材信息不可用/);
});

test('day card shows the day badge and cross-session visit count', () => {
  const html = renderCard({
    ...baseVisit,
    group: 'day',
    date: '2026-10-21',
    visit_count: 3,
    materials: [{ content: baseVisit.content, started_at: baseVisit.started_at, ended_at: baseVisit.started_at, visible_ms: 50000, page_views: 2, engaged: false }],
  });
  for (const want of ['按天汇总', '3 次访问', '累计浏览2次']) assert.ok(html.includes(want), `day card missing ${want}: ${html}`);
});

test('API calls the visit timeline endpoint', async () => {
  const calls = [];
  const api = load('src/api/shareTracking.ts', { '../utils/request': { request: async (...args) => { calls.push(args); return { data: { items: [] } }; } }, '../pages/shareTracking/access': { captureTrackingAccessBoundary: () => ({}), notifyTrackingAccessDenied() {} }, '../config/api': { API_BASE_URL: '' }, '../utils/session': { expireSession() {} } });
  await api.tracking.visits({ channel: 'timeline' });
  assert.equal(calls[0][0], '/api/v1/management/tracking/visits');
  assert.equal(calls[0][1].params.channel, 'timeline');
});

test('detail opened from a visit card never sends a bare content_version to shares or materials', () => {
  const seen = {};
  const capture = name => props => { seen[name] = props.query; return null; };
  const real = load('src/pages/shareTracking/components.tsx', { react: React, './model': model, './evidence': { IntentEvidenceDetails: () => null } });
  const details = load('src/pages/shareTracking/details.tsx', {
    react: React, './model': model, '../../api/shareTracking': { tracking: { sessions: () => new Promise(() => {}) } },
    './components': { ...real, Feedback: () => null, Pager: () => null },
    './crmTarget': { CRMTargetButton: () => null }, './convertLead': { ConvertLeadButton: () => null }, './governance': { VisitorGovernance: () => null },
    './visitorShares': { VisitorSharesPanel: capture('shares') }, './visitorContents': { VisitorContentPanel: capture('materials') },
  });
  renderToStaticMarkup(React.createElement(details.VisitorDetails, { visitor: baseVisit.visitor, contentId: 'content1', query: { owner: 'one', as_of: '2026-10-21T00:00:00Z', content_version: 3 }, context: { can_crm: false }, changed() {}, onBusyChange() {}, onDirtyChange() {} }));
  assert.equal(seen.shares.content_version, undefined);
  assert.equal(seen.materials.content_version, undefined);
  assert.equal(seen.shares.owner, 'one'); assert.equal(seen.shares.as_of, '2026-10-21T00:00:00Z');
});

test('restored source filter outside the allowed list falls back to all sources', () => {
  const access = load('src/pages/shareTracking/access.ts', { react: React, './model': model });
  assert.equal(access.normalizeTrackingView({ filters: { channel: 'fax' }, view: 'visitors' }, []).filters.channel, '');
  assert.equal(access.normalizeTrackingView({ filters: { channel: 'timeline' }, view: 'visitors' }, []).filters.channel, 'timeline');
});
