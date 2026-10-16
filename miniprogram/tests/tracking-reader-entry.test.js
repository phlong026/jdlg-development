'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

// Exercise the real reader, SDK adapter, core, routes, and option resolver
// together. Core-only tests cannot detect a reader rehydrating a cleared st.
const root = path.join(__dirname, '../miniprogram');
const route = 'pages/contentreader/contentreader';
const notice = '可选访问分析；可随时撤回，不影响阅读。';
const sceneA = 'a'.repeat(32);
const settle = () => new Promise(resolve => setImmediate(resolve));
const untilReady = async (page, tries = 20) => { for (let i = 0; i < tries && !page.data.contentReady; i++) await settle(); };
const direct = { content_id: 'content_one', content_version: '1', st: 'original_share' };

function runtime({ accepted = false, deferScene = false, deferConfig = false } = {}) {
  let pageDefinition, appDefinition, currentPage, mono = 1000;
  const storage = accepted ? { share_tracking_consent_v1: { version: 'v1' } } : {};
  const entries = [], events = [], pendingScenes = [], pendingConfigs = [], modals = [];
  const configuration = { enabled: true, consent_version: 'v1', privacy_notice: notice };
  const ok = (request, data) => request.success({ statusCode: 200, data: { code: 0, data } });
  const sceneResult = { route_key: route, content_id: 'content_one', content_version: 1, share_token: 'original_share' };
  const wx = {
    getPerformance: () => ({ now: () => mono }),
    getStorageSync: key => storage[key],
    setStorageSync: (key, value) => { storage[key] = value; },
    removeStorageSync: key => { delete storage[key]; },
    getRandomValues: ({ success }) => success({ randomValues: new Uint8Array(24).fill(7).buffer }),
    getWindowInfo: () => ({ windowWidth: 400, windowHeight: 800 }),
    showModal: options => modals.push(options),
    request(request) {
      const suffix = request.url.split('/api/v1/miniprogram/tracking')[1];
      if (suffix === '/config') {
        if (deferConfig) pendingConfigs.push(request);
        else ok(request, configuration);
        return;
      }
      if (suffix === '/share-entry') {
        if (deferScene) pendingScenes.push(request);
        else ok(request, sceneResult);
        return;
      }
      if (suffix === '/entry') {
        entries.push({ ...request.data });
        return ok(request, {
          entry_token: 'credential_' + entries.length, session_id: 'session_' + entries.length,
          content_accessible: true, content_version: request.data.content_version || 1,
          share_id: request.data.share_token ? 'share:' + request.data.share_token : undefined
        });
      }
      if (suffix === '/events/batch') {
        events.push(...request.data.events);
        return ok(request, { results: request.data.events.map(event => ({ event_id: event.event_id, status: 'accepted' })) });
      }
      if (suffix.startsWith('/contents/')) return ok(request, { title: request.data.title || '可读内容', media: [] });
      throw new Error('Unexpected request ' + suffix);
    }
  };
  const apiSandbox = { module: { exports: {} }, wx, Promise,
    require: () => ({ generateRequestParams: () => ({}), paramsToHeaders: () => ({}) }) };
  vm.runInNewContext(fs.readFileSync(path.join(root, 'utils/tracking/api.js'), 'utf8'), apiSandbox);
  const api = apiSandbox.module.exports;
  const sandbox = {
    module: { exports: {} }, wx, Date, Math, Promise, Uint8Array, setTimeout, clearTimeout, setInterval: () => 1,
    require: name => name === './api' ? api : require(path.join(root, 'utils/tracking', name.slice(2))),
    Page: definition => { pageDefinition = definition; },
    App: definition => { appDefinition = definition; },
    getCurrentPages: () => currentPage ? [currentPage] : []
  };
  vm.runInNewContext(fs.readFileSync(path.join(root, 'utils/tracking/index.js'), 'utf8'), sandbox);
  const adapter = sandbox.module.exports;
  adapter.install(); sandbox.App({}); appDefinition.onShow({});
  vm.runInNewContext(fs.readFileSync(path.join(root, route + '.js'), 'utf8'), {
    Page: sandbox.Page, exports: {}, wx, Date, Math, Promise,
    require: name => name.endsWith('/tracking/index') ? adapter : name.endsWith('/tracking/api') ? api : require(path.join(root, 'utils/tracking/video'))
  });
  const page = {
    ...pageDefinition, route, data: JSON.parse(JSON.stringify(pageDefinition.data)),
    setData(value, callback) { Object.assign(this.data, value); if (callback) callback.call(this); }
  };
  currentPage = page;
  return {
    adapter, page, entries, events, storage, modals, pendingScenes, pendingConfigs,
    // 阅读器就绪流程会重发 /config，解析全部待定请求
    resolveConfig() { while (pendingConfigs.length) ok(pendingConfigs.shift(), configuration); },
    async mount(options = direct) { page.onLoad({ ...options }); page.onShow(); page.onReady(); await settle(); },
    resolveScene(index = 0, value = sceneResult) { ok(pendingScenes[index], value); },
    grant() { assert.equal(adapter.setConsent(true), true); },
    withdraw() { adapter.setConsent(false); },
    hide() { page.onHide(); },
    async show() { page.onShow(); await settle(); },
    async switchAccount() { storage.token = 'different-account'; adapter.getSDK().tick(); await settle(); },
    background() { appDefinition.onHide(); page.onHide(); },
    async foregroundAfter(ms) { mono += ms; appDefinition.onShow({}); page.onShow(); await settle(); },
    async external(options) { appDefinition.onShow({ path: route, scene: 1007, query: { ...options } }); await settle(); },
    views() {
      const observed = [...events, ...adapter.getSDK().queueForTest().map(row => row.event)];
      return [...new Map(observed.map(event => [event.event_id, event])).values()].filter(event => event.event_type === 'page_view');
    }
  };
}

for (const options of [direct, { scene: sceneA }]) {
  test('real reader holds the first unused ' + (options.scene ? 'scene' : 'st') + ' until silent consent arrives with the config', async () => {
    const t = runtime({ deferConfig: true }); await t.mount(options);
    assert.equal(t.entries.length, 0, 'nothing collects before the config arrives');
    t.resolveConfig(); await settle();
    assert.equal(t.page.data.contentReady, true);
    assert.equal(t.entries.at(-1).share_token, 'original_share');
    assert.equal(t.views().at(-1).share_id, 'share:original_share');
  });
}

test('silent consent arriving while the scene is still resolving preserves the legitimate initial share', async () => {
  const t = runtime({ deferScene: true }); await t.mount({ scene: sceneA });
  t.resolveScene(); await settle();
  assert.equal(t.entries.at(-1).content_id, 'content_one');
  assert.equal(t.entries.at(-1).share_token, 'original_share');
  assert.equal(t.views().at(-1).share_id, 'share:original_share');
});

test('same-page content retry before consent keeps its unconsumed valid share', async () => {
  const t = runtime({ deferConfig: true }); await t.mount();
  t.page.retryMedia(); await settle();
  assert.equal(t.entries.length, 0, 'nothing collects before the config arrives');
  t.resolveConfig(); await settle(); await untilReady(t.page); await settle();
  assert.equal(t.entries.at(-1).share_token, 'original_share');
  assert.equal(t.views().at(-1).share_id, 'share:original_share');
});

test('real reader return cannot restore a share cleared by withdrawal and hidden re-consent', async () => {
  const t = runtime(); await t.mount(); t.grant(); await settle();
  assert.equal(t.entries.at(-1).share_token, 'original_share');
  t.hide(); t.withdraw(); t.grant(); await settle();
  const before = t.entries.length;
  await t.show();
  assert.ok(t.entries.length > before);
  assert.equal(t.entries.at(-1).share_token, undefined);
  assert.equal(t.views().at(-1).share_id, undefined);
});

test('real reader hide before consent permanently discards that pending share', async () => {
  const t = runtime({ deferConfig: true }); await t.mount(); t.hide(); t.resolveConfig(); await t.show();
  await settle(); await untilReady(t.page); await settle();
  assert.equal(t.entries.at(-1).share_token, undefined, 'hidden-before-consent re-entry is unattributed');
  assert.ok(t.views().every(v => v.share_id === undefined), 'the discarded share never reappears on any view');
});

test('real reader return after account switch cannot restore a previous-account share', async () => {
  const t = runtime({ accepted: true }); await t.mount(); t.hide(); await t.switchAccount(); await t.show();
  assert.equal(t.entries.at(-1).share_token, undefined);
  assert.equal(t.views().at(-1).share_id, undefined);
});

test('real reader long-background reload cannot restore the expired visit share', async () => {
  const t = runtime({ accepted: true }); await t.mount(); const old = t.views().at(-1);
  t.background(); await t.foregroundAfter(31 * 60 * 1000);
  assert.notEqual(t.views().at(-1).session_id, old.session_id);
  assert.equal(t.entries.at(-1).share_token, undefined);
  assert.equal(t.views().at(-1).share_id, undefined);
});

for (const boundary of ['withdrawal', 'account switch', 'hide']) {
  test('late scene resolution cannot restore attribution after ' + boundary, async () => {
    const t = runtime({ deferScene: true }); await t.mount({ scene: sceneA });
    assert.equal(t.pendingScenes.length, 1);
    if (boundary === 'withdrawal') t.withdraw();
    if (boundary === 'account switch') await t.switchAccount();
    if (boundary === 'hide') t.hide();
    t.resolveScene(); await settle();
    t.grant();
    if (boundary === 'hide') await t.show();
    await settle();
    assert.ok(t.entries.length > 0);
    assert.equal(t.entries.at(-1).share_token, undefined);
    assert.equal(t.views().at(-1).share_id, undefined);
  });
}

test('a genuine later external entry can still apply its new share', async () => {
  const t = runtime({ accepted: true }); await t.mount();
  await t.external({ content_id: 'content_two', content_version: '2', st: 'new_share' });
  assert.equal(t.entries.at(-1).content_id, 'content_two');
  assert.equal(t.entries.at(-1).share_token, 'new_share');
  assert.equal(t.views().at(-1).content_id, 'content_two');
  assert.equal(t.views().at(-1).share_id, 'share:new_share');
});

test('obsolete scene completion cannot replace newer reader options or its share path', async () => {
  const t = runtime({ deferScene: true }); await t.mount({ scene: sceneA });
  await t.external({ content_id: 'content_two', content_version: '2', st: 'new_share' });
  assert.equal(t.page.options.content_id, 'content_two');
  t.resolveScene(); await settle();
  assert.equal(t.page.options.content_id, 'content_two');
  assert.equal(String(t.page.options.content_version), '2');
  assert.match(t.page.onShareAppMessage().path, /content_id=content_two&content_version=2/);
  t.grant(); await settle();
  assert.equal(t.entries.at(-1).content_id, 'content_two');
  assert.equal(t.entries.at(-1).share_token, 'new_share');
});
