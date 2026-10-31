'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const ts = require('./report-runtime')('typescript');

const root = path.resolve(__dirname, '../miniprogram');
const reportRoute = 'pages/aicostshare/aicostshare';
const readerRoute = 'pages/contentreader/contentreader';
const firstScene = 'a'.repeat(32), nextScene = 'b'.repeat(32);
const reportID = 'AbcdEFghIJklMNopQRstUVwxYZ012345';
const nextReportID = 'BcdeFGhiJKlmNOpqRStuVWxyZA123456';
const avatar = 'https://cdn.example.test/profile/uploaded.png';
const completeProfile = { id: 41, nickname: '分享访客', avatar };
const result = {
  version: 4, estimatedTotalCost: '58.36万元', estimatedUnitCost: '1823.75元/㎡',
  totalLabel: '参考总造价', unitLabel: '参考单价',
  basicInfo: Array.from({ length: 14 }, (_, i) => ({ label: i === 0 ? '所在地区' : i === 1 ? '总建筑面积' : `公开项目${i}`, value: i === 0 ? '福建 / 泉州' : i === 1 ? '320㎡' : '普通配置' })),
  costBreakdown: Array.from({ length: 6 }, (_, i) => ({ name: `费用${i}`, amount: 100, amountText: '0.01万元', percent: 10, note: '公开参考', estimable: true })),
  dynamicNotes: [],
};
const settle = async () => { for (let i = 0; i < 16; i++) await new Promise(resolve => setImmediate(resolve)); };

// Real installers, page, components, profile validation, cost API and tracking
// modules run together. Only native WeChat I/O, signature generation and images
// are replaced; no request can reach a network transport from this harness.
function journey({ extension = 'js', token = '', profile = completeProfile, cachedID = profile.id, profileMode = 'ok', enabled = true, declined = false, defaultShare = false, reader = false } = {}) {
  const storage = { token, userInfo: token ? { id: cachedID, staleBusinessField: 'must-not-cross-account' } : {}, device_fingerprint: 'test-device', share_tracking_instance_v1: 'test-installation' };
  if (declined) storage.share_tracking_consent_choice_v1 = { accepted: false, explicit: true, version: 'v1' };
  const server = { profile: { ...profile }, profileMode, holdProfile: false, persistNickname: true };
  const requests = [], events = [], errors = [], pendingProfile = [], modules = new Map();
  let pageDefinition, componentDefinition, app, page, sequence = 0;
  const reply = (options, data, statusCode = 200) => queueMicrotask(() => options.success({ statusCode, data: { code: statusCode === 200 ? 0 : statusCode, data } }));
  const wx = {
    getStorageSync: key => storage[key], setStorageSync: (key, value) => { storage[key] = value; }, removeStorageSync: key => { delete storage[key]; },
    getPerformance: () => ({ now: () => 1000 }), getLaunchOptionsSync: () => ({ scene: 1007 }),
    getWindowInfo: () => ({ windowWidth: 400, windowHeight: 800 }),
    onNetworkStatusChange() {}, showShareMenu() {}, hideShareMenu() {}, showToast() {}, navigateTo() {},
    login: options => queueMicrotask(() => options.success({ code: 'test-wechat-code' })),
    uploadFile(options) {
      requests.push({ path: new URL(options.url).pathname, method: 'UPLOAD', header: { ...options.header } });
      queueMicrotask(() => options.success({ statusCode: 200, data: JSON.stringify({ code: 0, data: { url: avatar } }) }));
    },
    request(options) {
      const requestPath = new URL(options.url).pathname;
      requests.push({ path: requestPath, method: options.method || 'GET', body: options.data, header: { ...options.header } });
      if (requestPath === '/api/v1/miniprogram/login') return reply(options, { id: 41, token: 'session-A', is_new_user: !server.profile.nickname });
      if (requestPath === '/api/v1/miniprogram/profile') {
        const complete = () => {
          if (server.profileMode === 'offline') return queueMicrotask(() => options.fail(new Error('profile offline')));
          if (options.header.token !== 'session-A') return reply(options, {}, 401);
          if (server.profileMode === 'error') return reply(options, {}, 500);
          reply(options, { ...server.profile });
        };
        if (server.holdProfile) pendingProfile.push(complete); else complete();
        return;
      }
      if (requestPath === '/api/v1/miniprogram/profile/avatar') { server.profile.avatar = options.data.avatar; return reply(options, {}); }
      if (requestPath === '/api/v1/miniprogram/profile/nickname') {
        if (server.persistNickname) server.profile.nickname = options.data.nickname;
        return reply(options, {});
      }
      if ([reportID, nextReportID].some(id => requestPath === `/api/v1/miniprogram/ai/cost/shares/${id}`)) return reply(options, { report_id: requestPath.split('/').at(-1), expires_at: '2026-11-22T00:00:00Z', result });
      if (requestPath.endsWith('/tracking/config')) return reply(options, { enabled, consent_version: 'v1', privacy_notice: 'Public visit analytics.' });
      if (requestPath.endsWith('/tracking/share-entry')) {
        assert.ok([firstScene, nextScene].includes(options.data.scene));
        const next = options.data.scene === nextScene;
        return reply(options, { route_key: readerRoute, content_id: next ? 'content_two' : 'content_one', content_version: next ? 2 : 1, share_token: next ? 'next-share' : 'incoming-token' });
      }
      if (requestPath.includes('/tracking/contents/')) return reply(options, { title: '公开正文', media: [] });
      if (requestPath.endsWith('/tracking/entry')) return reply(options, {
        entry_token: `entry_${++sequence}`, session_id: `session_${sequence}`, content_version: 5123,
        content_accessible: true, visitor_key: options.header.token === 'session-A' ? 'u:41' : 'anonymous',
        share_id: options.data.share_token === 'incoming-token' ? 'upstream-share' : options.data.share_token ? `share:${options.data.share_token}` : undefined,
      });
      if (requestPath.endsWith('/tracking/shares')) return reply(options, { share_token: `prepared_${++sequence}`, share_id: `share_${sequence}`, expires_at: new Date(Date.now() + 60000).toISOString() });
      if (requestPath.endsWith('/tracking/events/batch')) {
        events.push(...options.data.events);
        return reply(options, { results: options.data.events.map(event => ({ event_id: event.event_id, status: 'accepted' })) });
      }
      throw new Error(`Unexpected mock request: ${requestPath}`);
    },
  };
  const context = vm.createContext({
    wx, Promise, Math, Date, Uint8Array, URL, setTimeout, clearTimeout, setInterval: () => 1,
    console: { error: (...values) => errors.push(values), warn: (...values) => errors.push(values) },
    Page: definition => { pageDefinition = definition; }, App: definition => { app = definition; },
    Component: definition => { componentDefinition = definition; }, getCurrentPages: () => page ? [page] : [],
  });
  const mocks = {
    'utils/parameter': { generateRequestParams: session => ({ token: session }), paramsToHeaders: value => value },
    'utils/deviceFingerprint': { getCachedDeviceFingerprint: () => 'test-device', generateDeviceFingerprint: async () => 'test-device', cacheDeviceFingerprint() {} },
    'utils/promptReview': { blockUnsafeText: () => false },
    'utils/costEstimateShareImage': { prepareCostEstimateShareImage: async () => 'local:chat', prepareCostEstimateTimelineImage: async () => 'local:timeline' },
  };
  function load(relative) {
    if (mocks[relative]) return mocks[relative];
    if (modules.has(relative)) return modules.get(relative).exports;
    const module = { exports: {} };
    modules.set(relative, module);
    let source = fs.readFileSync(path.join(root, `${relative}.${extension}`), 'utf8');
    if (extension === 'ts') source = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 } }).outputText;
    const run = vm.runInContext(`(function(module, exports, require) {\n${source}\n})`, context, { filename: `${relative}.${extension}` });
    run(module, module.exports, name => {
      if (!name.startsWith('.')) throw new Error(`Unexpected dependency: ${name}`);
      return load(path.posix.normalize(path.posix.join(path.posix.dirname(relative), name)));
    });
    return module.exports;
  }
  load('utils/profile-access').install();
  const tracking = load('utils/tracking/index');
  tracking.install();
  context.App({});
  app.onLaunch({});
  if (defaultShare) context.Page({ data: {} }); else load(reader ? readerRoute : reportRoute);
  const instantiate = definition => ({ ...definition, ...definition.methods, data: { ...definition.data }, setData(value, done) { Object.assign(this.data, value); if (done) done.call(this); } });
  load('components/nickname-guard/nickname-guard');
  const editor = instantiate(componentDefinition);
  load('components/login-guard/login-guard');
  const gate = instantiate(componentDefinition);
  gate.selectComponent = id => id === '#nicknameGuard' ? editor : null;
  editor.triggerEvent = (name) => { if (name === 'completed') gate.onProfileCompleted(); if (name === 'authrequired') gate.onProfileAuthRequired(); };
  gate.triggerEvent = (name, detail) => {
    if (name === 'gated') page.onProfileAccessBlocked({ detail });
    if (name === 'authenticated') page.onProfileAccessGranted({ detail });
  };
  const route = defaultShare ? 'pages/aicost/aicost' : reader ? readerRoute : reportRoute;
  const h = {
    storage, server, requests, events, errors, editor, gate, tracking,
    get page() { return page; },
    entries: () => requests.filter(request => request.path.endsWith('/tracking/entry')),
    shares: () => requests.filter(request => request.path.endsWith('/tracking/shares')),
    reports: () => requests.filter(request => request.path.includes('/ai/cost/shares/')),
    async mount(initialOptions) {
      const options = initialOptions || (defaultShare ? { st: 'incoming-token' } : reader ? { scene: firstScene } : { report_id: reportID, st: 'incoming-token' });
      app.onShow({ path: route, scene: 1007, query: options });
      page = { ...instantiate(pageDefinition), route, selectComponent: id => id === '#profileAccessGuard' ? gate : null };
      page.onLoad(options); page.onShow(); page.onReady();
      await settle();
    },
    async login() { gate.onTogglePrivacy(); await gate.onWxLogin(); await settle(); },
    async chooseAvatar() { editor.onChooseAvatar({ detail: { avatarUrl: 'wxfile://test-avatar' } }); await settle(); },
    async saveNickname(value = '分享访客') { await editor.onConfirm({ detail: { value: { nickname: value } } }); await settle(); },
    releaseProfile() { server.holdProfile = false; pendingProfile.splice(0).forEach(complete => complete()); },
    background() { page.onHide(); app.onHide(); },
    async resume(options = { report_id: reportID, st: 'incoming-token' }) {
      app.onShow({ path: route, scene: 1007, query: options });
      page.onShow();
      await settle();
    },
    close() { if (page) page.onUnload(); if (gate.lifetimes?.detached) gate.lifetimes.detached.call(gate); },
  };
  return h;
}

function assertBlocked(h) {
  assert.equal(h.page.data.__profileAccessReady, false);
  assert.equal(h.page.data.contentReady, false);
  assert.equal(h.reports().length, 0, 'no report fetch before verified profile');
  assert.equal(h.entries().length, 0, 'no anonymous entry while profile is required');
  assert.equal(h.events.length, 0);
  assert.equal(Object.keys(h.page.onShareAppMessage()).length, 0);
}

async function assertAccessibleAndAttributed(h) {
  assert.equal(h.page.data.__profileAccessReady, true);
  assert.equal(h.page.data.contentReady, true);
  assert.equal(h.reports().length, 1);
  assert.equal(h.entries().length, 1);
  assert.equal(h.entries()[0].header.token, 'session-A');
  assert.equal(h.entries()[0].body.share_token, 'incoming-token');
  assert.equal(h.entries()[0].body.content_id, `costsnapshot:${reportID}`);
  assert.equal(h.tracking.getSDK().context().visitor_key, 'u:41');
  assert.equal(h.events.filter(event => event.event_type === 'page_view').length, 1);
  const shared = await h.page.onShareAppMessage().promise;
  assert.match(shared.path, /[?&]st=prepared_/);
  assert.ok(h.shares().length > 0);
  assert.ok(h.shares().every(request => request.body.parent_share_id === 'upstream-share'));
}

for (const extension of ['ts', 'js']) {
  test(`${extension}: new account must save avatar and nickname before report/PV; original share and re-share parent survive`, async t => {
    const h = journey({ extension, profile: { id: 41, nickname: '', avatar: '' } }); t.after(() => h.close());
    await h.mount(); assertBlocked(h);
    await h.login(); assertBlocked(h);
    assert.equal(h.editor.data.required, true);
    await h.saveNickname(); assertBlocked(h);
    assert.equal(h.requests.filter(request => request.path.endsWith('/profile/nickname')).length, 0, 'nickname alone cannot satisfy required avatar');
    await h.chooseAvatar(); assertBlocked(h);
    await h.saveNickname();
    await assertAccessibleAndAttributed(h);
    assert.equal(h.storage.userInfo.nickname, '分享访客');
    assert.equal(h.storage.userInfo.avatar, avatar);
  });

  for (const missing of ['nickname', 'avatar']) test(`${extension}: returning account missing ${missing} completes server profile before access`, async t => {
    const h = journey({ extension, token: 'session-A', profile: { ...completeProfile, [missing]: '' } }); t.after(() => h.close());
    await h.mount(); assertBlocked(h);
    assert.equal(h.editor.data.nickname, missing === 'nickname' ? '' : completeProfile.nickname);
    if (missing === 'avatar') await h.chooseAvatar();
    await h.saveNickname();
    await assertAccessibleAndAttributed(h);
  });

  test(`${extension}: stale cached id is replaced before the authoritative account completes a missing avatar`, async t => {
    const h = journey({ extension, token: 'session-A', cachedID: 99, profile: { ...completeProfile, avatar: '' } }); t.after(() => h.close());
    await h.mount(); assertBlocked(h);
    assert.equal(h.storage.userInfo.id, 41);
    assert.equal(h.storage.userInfo.staleBusinessField, undefined, 'cross-account cache fields must be discarded');
    assert.equal(h.editor.data.promptUserID, 41);
    await h.chooseAvatar(); await h.saveNickname();
    await assertAccessibleAndAttributed(h);
    assert.equal(h.storage.userInfo.avatar, avatar);
  });

  test(`${extension}: complete returning account still waits for authoritative profile read`, async t => {
    const h = journey({ extension, token: 'session-A' }); t.after(() => h.close());
    h.server.holdProfile = true;
    await h.mount(); assertBlocked(h);
    h.releaseProfile(); await settle();
    await assertAccessibleAndAttributed(h);
    assert.equal(h.requests.filter(request => request.method === 'PUT' || request.method === 'UPLOAD').length, 0);
  });

  test(`${extension}: expired stored token stays blocked until a fresh login is verified`, async t => {
    const h = journey({ extension, token: 'expired-session' }); t.after(() => h.close());
    await h.mount(); assertBlocked(h);
    assert.ok(!h.storage.token); assert.equal(h.gate.data.needsLogin, true);
    await h.login(); await assertAccessibleAndAttributed(h);
  });

  test(`${extension}: profile network failure remains blocked and retry recovers same share`, async t => {
    const h = journey({ extension, token: 'session-A', profileMode: 'offline' }); t.after(() => h.close());
    await h.mount(); assertBlocked(h); assert.ok(h.gate.data.error);
    h.server.profileMode = 'ok'; await h.gate.onRetry(); await settle();
    await assertAccessibleAndAttributed(h);
  });

  test(`${extension}: successful nickname write must be read back before unlocking`, async t => {
    const h = journey({ extension, token: 'session-A', profile: { ...completeProfile, nickname: '' } }); t.after(() => h.close());
    await h.mount(); assertBlocked(h);
    h.server.holdProfile = true;
    const saving = h.editor.onConfirm({ detail: { value: { nickname: '新昵称' } } });
    await settle();
    assert.equal(h.server.profile.nickname, '新昵称'); assertBlocked(h);
    h.releaseProfile(); await saving; await settle();
    await assertAccessibleAndAttributed(h);
  });

  test(`${extension}: acknowledged write with mismatched profile readback stays blocked`, async t => {
    const h = journey({ extension, token: 'session-A', profile: { ...completeProfile, nickname: '' } }); t.after(() => h.close());
    await h.mount(); h.server.persistNickname = false;
    await h.saveNickname(); assertBlocked(h);
    assert.equal(h.editor.data.visible, true);
  });

  test(`${extension}: declining analytics still requires complete profile but produces no tracking events`, async t => {
    const h = journey({ extension, declined: true, profile: { id: 41, nickname: '', avatar: '' } }); t.after(() => h.close());
    await h.mount(); assertBlocked(h);
    await h.login(); assertBlocked(h);
    await h.chooseAvatar(); await h.saveNickname();
    assert.equal(h.page.data.__profileAccessReady, true); assert.equal(h.page.data.contentReady, true);
    assert.equal(h.reports().length, 1); assert.equal(h.entries().length, 0); assert.equal(h.shares().length, 0); assert.equal(h.events.length, 0);
    assert.equal(h.page.onShareAppMessage().path, `/${reportRoute}?report_id=${reportID}`);
  });

  test(`${extension}: public page without a custom app-message handler still gets a tracked share`, async t => {
    const h = journey({ extension, token: 'session-A', defaultShare: true }); t.after(() => h.close());
    await h.mount();
    assert.equal(h.page.data.__profileAccessReady, true);
    const share = h.page.onShareAppMessage();
    assert.ok(share.promise, 'default share must use prepareShare');
    const value = await share.promise;
    assert.match(value.path, /^\/pages\/aicost\/aicost\?st=prepared_/);
    assert.equal(h.shares().at(-1).body.parent_share_id, 'upstream-share');
  });

  test(`${extension}: ordinary background return rechecks profile without replaying the same share visit`, async t => {
    const h = journey({ extension, token: 'session-A' }); t.after(() => h.close());
    await h.mount(); await assertAccessibleAndAttributed(h);
    h.background(); h.server.holdProfile = true;
    await h.resume();
    assert.equal(h.page.data.__profileAccessReady, false, '热返回必须等待服务端再次确认');
    assert.ok(h.requests.some(request => String(request.url || request.path || '').endsWith('/profile')), 'warm return must still re-verify the profile');
    assert.equal(h.reports().length, 1); assert.equal(h.entries().length, 1);
    h.releaseProfile(); await settle();
    assert.equal(h.page.data.__profileAccessReady, true);
    assert.equal(h.reports().length, 2, '核验通过后才重新确认报告可用性');
    assert.equal(h.entries().length, 1, 'unchanged warm query is not another external share');
    assert.equal(h.events.filter(event => event.event_type === 'page_view').length, 1);
  });

  test(`${extension}: warm different report waits for profile, then binds only the new report and share`, async t => {
    const h = journey({ extension, token: 'session-A' }); t.after(() => h.close());
    await h.mount(); await assertAccessibleAndAttributed(h);
    h.background(); h.server.holdProfile = true;
    await h.resume({ report_id: nextReportID, st: 'next-share' });
    assert.equal(h.page.data.__profileAccessReady, false);
    assert.equal(h.reports().length, 1);
    assert.equal(h.entries().length, 1);
    h.releaseProfile(); await settle();
    assert.equal(h.page.data.contentReady, true);
    assert.equal(h.page.data.reportId, nextReportID);
    assert.equal(h.entries().length, 2);
    assert.equal(h.entries()[1].body.content_id, `costsnapshot:${nextReportID}`);
    assert.equal(h.entries()[1].body.share_token, 'next-share');
    assert.equal(h.entries()[1].body.previous_entry_token, undefined);
    const share = await h.page.onShareAppMessage().promise;
    assert.ok(share.path.includes(`report_id=${nextReportID}&st=`));
    assert.equal(h.shares().at(-1).body.parent_share_id, 'share:next-share');
  });

  test(`${extension}: profile failure after background return never resumes an old tracked credential`, async t => {
    const h = journey({ extension, token: 'session-A' }); t.after(() => h.close());
    await h.mount(); await assertAccessibleAndAttributed(h);
    h.background(); h.server.profileMode = 'offline';
    await h.resume();
    assert.equal(h.reports().length, 1, '权威资料查询失败时不重新读取内容');
    const eventCount = h.events.length;
    h.tracking.getSDK().tick(); await settle();
    assert.equal(h.page.data.__profileAccessReady, false);
    assert.equal(h.entries().length, 1);
    assert.equal(h.events.length, eventCount);
    assert.equal(Object.keys(h.page.onShareAppMessage()).length, 0);
  });

  test(`${extension}: second reader scene keeps its own share after warm profile verification and reload`, async t => {
    const h = journey({ extension, token: 'session-A', reader: true }); t.after(() => h.close());
    await h.mount();
    assert.equal(h.page.data.contentReady, true);
    assert.equal(h.entries().length, 1);
    assert.equal(h.entries()[0].body.content_id, 'content_one');
    assert.equal(h.entries()[0].body.share_token, 'incoming-token');
    const firstGeneration = h.page.boundEntryGeneration;
    h.background(); h.server.holdProfile = true;
    await h.resume({ scene: nextScene });
    assert.equal(h.page.data.__profileAccessReady, false);
    assert.equal(h.entries().length, 1);
    assert.equal(h.requests.filter(request => request.path.endsWith('/tracking/share-entry')).length, 1);
    h.releaseProfile(); await settle();
    assert.equal(h.page.data.contentReady, true);
    assert.equal(h.entries().length, 2);
    assert.equal(h.entries()[1].body.content_id, 'content_two');
    assert.equal(h.entries()[1].body.content_version, 2);
    assert.equal(h.entries()[1].body.share_token, 'next-share');
    assert.ok(h.page.boundEntryGeneration > firstGeneration, 'warm navigation must not reuse the previous bound generation');
    assert.equal(h.entries()[1].body.previous_entry_token, undefined);
    assert.equal(h.events.filter(event => event.event_type === 'page_view').at(-1).share_id, 'share:next-share');
    const share = await h.page.onShareAppMessage().promise;
    assert.ok(share.path.includes('content_id=content_two&content_version=2&st='));
    assert.equal(h.shares().at(-1).body.parent_share_id, 'share:next-share');
  });

  for (const mode of ['disabled', 'declined']) test(`${extension}: ${mode} analytics still opens a different warm report without st`, async t => {
    const h = journey({ extension, token: 'session-A', enabled: mode !== 'disabled', declined: mode === 'declined' }); t.after(() => h.close());
    await h.mount({ report_id: reportID });
    assert.equal(h.page.data.contentReady, true); assert.equal(h.page.data.reportId, reportID);
    assert.equal(h.entries().length, 0);
    h.background(); h.server.holdProfile = true;
    await h.resume({ report_id: nextReportID });
    assert.equal(h.page.data.__profileAccessReady, false);
    assert.equal(h.reports().length, 1, '新报告在资料核验前不可读取');
    h.releaseProfile(); await settle();
    assert.equal(h.page.data.__profileAccessReady, true);
    assert.equal(h.page.data.contentReady, true);
    assert.equal(h.page.data.reportId, nextReportID);
    assert.equal(h.reports().length, 2);
    assert.equal(h.reports()[1].path, `/api/v1/miniprogram/ai/cost/shares/${nextReportID}`);
    assert.equal(h.page.onShareAppMessage().path, `/${reportRoute}?report_id=${nextReportID}`);
    assert.equal(h.entries().length, 0); assert.equal(h.shares().length, 0); assert.equal(h.events.length, 0);
  });
}
