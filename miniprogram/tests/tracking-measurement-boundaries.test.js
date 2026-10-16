'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const { createTracker, SESSION_IDLE } = require('../miniprogram/utils/tracking/core');
const root = path.join(__dirname, '../miniprogram');
const settle = () => new Promise(resolve => setImmediate(resolve));
const readerRoute = 'pages/contentreader/contentreader';

// Only WeChat, the clock, and transport are substituted. These tests execute
// the production reader callbacks, adapter/modal, core, and video tracker.
function readerRuntime({ accepted = false, deferConfig = false } = {}) {
  let pageDefinition, appDefinition, mono = 1000, paused = 0;
  const storage = accepted ? { share_tracking_consent_v1: { version: 'v1' } } : {};
  const entries = [], sent = [], modals = [], credentials = new Map();
  let pendingConfig = null;
  const configuration = { enabled: true, consent_version: 'v1', privacy_notice: 'Optional analytics; withdrawal leaves playback available.' };
  const api = {
    resolveReaderOptions: async options => options,
    mediaURL: value => value,
    async request(suffix, method, body) {
      if (suffix === '/config') {
        if (deferConfig) return new Promise(resolve => { pendingConfig = resolve; });
        return configuration;
      }
      if (suffix === '/entry') {
        entries.push(body);
        const old = credentials.get(body.previous_entry_token);
        const response = { entry_token: 'credential_' + entries.length, session_id: old && old.account === storage.token ? old.session_id : 'session_' + entries.length, content_version: 1, content_accessible: true };
        credentials.set(response.entry_token, { ...response, account: storage.token }); return response;
      }
      if (suffix === '/events/batch') {
        sent.push(...body.events);
        return { results: body.events.map(event => ({ event_id: event.event_id, status: 'accepted' })) };
      }
      if (suffix.startsWith('/contents/')) return { title: 'Readable video', media: [{ kind: 'video', media_id: 'video_asset', url: 'https://media.example/video.mp4' }] };
      throw new Error('Unexpected request ' + suffix);
    }
  };
  const wx = {
    getPerformance: () => ({ now: () => mono }), getStorageSync: key => storage[key],
    setStorageSync: (key, value) => { storage[key] = value; }, removeStorageSync: key => { delete storage[key]; },
    getRandomValues: ({ success }) => success({ randomValues: new Uint8Array(24).fill(7).buffer }),
    getWindowInfo: () => ({ windowWidth: 400, windowHeight: 800 }),
    createVideoContext: () => ({ pause() { paused++; } }), showModal: options => modals.push(options)
  };
  const sandbox = {
    module: { exports: {} }, wx, Date, Math, Promise, Uint8Array, setTimeout, clearTimeout, setInterval: () => 1,
    require: name => name === './api' ? api : require(path.join(root, 'utils/tracking', name.slice(2))),
    Page: definition => { pageDefinition = definition; }, App: definition => { appDefinition = definition; }
  };
  vm.runInNewContext(fs.readFileSync(path.join(root, 'utils/tracking/index.js'), 'utf8'), sandbox);
  const adapter = sandbox.module.exports;
  adapter.install(); sandbox.App({}); appDefinition.onShow({});
  vm.runInNewContext(fs.readFileSync(path.join(root, readerRoute + '.js'), 'utf8'), {
    Page: sandbox.Page, wx, Date, Math, Promise,
    require: name => name.endsWith('/index') ? adapter : name.endsWith('/api') ? api : require(path.join(root, 'utils/tracking/video'))
  });
  const page = { ...pageDefinition, route: readerRoute, data: JSON.parse(JSON.stringify(pageDefinition.data)),
    setData(value, callback) { Object.assign(this.data, value); if (callback) callback.call(this); },
    createIntersectionObserver() { return { relativeToViewport() { return this; }, observe(selector, callback) { callback({ intersectionRatio: 1 }); }, disconnect() {} }; }
  };
  const event = (type, detail = {}) => ({ type, detail, currentTarget: { dataset: { mediaId: 'video_asset' } } });
  return {
    page, adapter, entries, sent, modals, storage, resolveConfig() { pendingConfig(configuration); },
    async mount() { page.onLoad({ content_id: 'material_id', content_version: 1 }); page.onShow(); page.onReady(); await settle(); },
    progress(position, elapsed = 1000) { mono += elapsed; page.videoProgress(event('timeupdate', { currentTime: position, duration: 100 })); },
    play() { page.videoState(event('play')); },
    videos() { return [...sent, ...adapter.getSDK().queueForTest().map(row => row.event)].filter(event => event.event_type.startsWith('video_')); },
    player() { return page.players.video_asset; }, paused: () => paused
  };
}

test('real reader excludes denied video coverage until consent is granted via the reader button', async () => {
  {
    const t = readerRuntime(); await t.mount();
    assert.equal(t.page.data.contentReady, true);
    t.page.declineTracking();
    t.play(); t.progress(0, 0);
    for (let second = 1; second <= 60; second++) t.progress(second);
    assert.equal(t.player().snapshot().unique_played_ms, 0, 'denied playback never accumulates analytical coverage');
    assert.equal(t.videos().length, 0);
    const pauses = t.paused();
    t.page.acceptTracking();
    await settle(); t.progress(61); t.progress(62);
    const observed = t.videos();
    assert.equal(observed.length, 2);
    assert.deepEqual(observed[0].played_ranges, []);
    assert.equal(observed[0].video_watch_ms, 0);
    assert.deepEqual(observed[1].played_ranges, [[61000, 62000]]);
    assert.equal(observed[1].video_watch_ms, 1000);
    assert.deepEqual(observed.map(event => event.seq), [1, 2]);
    assert.equal(t.paused(), pauses, 'consent changes must not pause business video playback');
  }
});

test('withdrawal and regrant reset observations immediately without restarting playback', async () => {
  const t = readerRuntime({ accepted: true }); await t.mount();
  t.play(); t.progress(0, 0); t.progress(1); await t.adapter.getSDK().flush();
  const first = t.videos().at(-1), count = t.videos().length, pauses = t.paused();
  t.adapter.setConsent(false);
  assert.equal(t.player().snapshot().unique_played_ms, 0);
  t.progress(2); t.progress(3); assert.equal(t.videos().length, count);
  t.adapter.setConsent(true); await settle(); t.progress(4); t.progress(5);
  const next = t.videos().at(-1);
  assert.notEqual(next.player_instance_id, first.player_instance_id);
  assert.notEqual(next.page_view_id, first.page_view_id);
  assert.deepEqual(next.played_ranges, [[4000, 5000]]);
  assert.equal(next.seq, 2); assert.equal(t.paused(), pauses);
});

test('account change detected by a media callback cannot bridge the old observation', async () => {
  const t = readerRuntime({ accepted: true }); await t.mount();
  t.play(); t.progress(0, 0); t.progress(1); const first = t.videos().at(-1);
  t.storage.token = 'different-account'; t.progress(2);
  t.adapter.getSDK().tick(); await settle(); t.progress(3); t.progress(4);
  const next = t.videos().at(-1);
  assert.notEqual(next.player_instance_id, first.player_instance_id);
  assert.notEqual(next.page_view_id, first.page_view_id);
  assert.deepEqual(next.played_ranges, [[3000, 4000]]);
  assert.equal(next.seq, 2);
});

test('cached reader new page view resets video observation but short background only resets sampling', async () => {
  const t = readerRuntime({ accepted: true }); await t.mount();
  const sdk = t.adapter.getSDK(); t.play(); t.progress(0, 0); t.progress(1); await t.adapter.getSDK().flush();
  const first = t.videos().at(-1), initialContext = sdk.get(t.page).context;
  sdk.appHide(); t.progress(2); sdk.appShow(); t.progress(3); t.progress(4);
  assert.equal(t.videos().at(-1).player_instance_id, first.player_instance_id);
  assert.deepEqual(t.videos().at(-1).played_ranges, [[0, 1000], [3000, 4000]]);
  sdk.register('next', 'pages/index/index', {}); sdk.show('next'); sdk.ready('next', true); await settle();
  sdk.hide('next', true); sdk.show(t.page); await settle(); t.progress(5); t.progress(6);
  const next = t.videos().at(-1);
  assert.equal(sdk.get(t.page).context, initialContext, 'cached same-session entry is retained while its PV changes');
  assert.notEqual(next.page_view_id, first.page_view_id);
  assert.notEqual(next.player_instance_id, first.player_instance_id);
  assert.deepEqual(next.played_ranges, [[5000, 6000]]);
});

test('configuration suspension resets media samples without discarding the current page view or offline queue', async () => {
  const t = readerRuntime({ accepted: true }); await t.mount();
  const sdk = t.adapter.getSDK(); t.play(); t.progress(0, 0); t.progress(1);
  const first = t.videos().at(-1), pauses = t.paused();
  sdk.suspend(); const saved = sdk.queueForTest();
  assert.equal(t.player().snapshot().unique_played_ms, 0);
  t.progress(2); t.progress(3); assert.deepEqual(sdk.queueForTest(), saved);
  sdk.configure(true, 'v1'); t.progress(4); t.progress(5);
  const next = t.videos().at(-1);
  assert.equal(next.page_view_id, first.page_view_id);
  assert.notEqual(next.player_instance_id, first.player_instance_id);
  assert.deepEqual(next.played_ranges, [[4000, 5000]]);
  assert.equal(next.seq, 2); assert.equal(t.paused(), pauses);
});

test('a changed consent version while hidden invalidates the cached media observation', async () => {
  const t = readerRuntime({ accepted: true }); await t.mount();
  const sdk = t.adapter.getSDK(); t.play(); t.progress(0, 0); t.progress(1);
  const first = t.videos().at(-1);
  sdk.hide(t.page, false); sdk.configure(true, 'v2');
  assert.equal(t.player().snapshot().unique_played_ms, 0);
  sdk.show(t.page); await settle(); t.progress(2); t.progress(3);
  const next = t.videos().at(-1);
  assert.notEqual(next.page_view_id, first.page_view_id);
  assert.notEqual(next.player_instance_id, first.player_instance_id);
  assert.deepEqual(next.played_ranges, [[2000, 3000]]);
});

test('a readiness gap cannot add video coverage even if the underlying video keeps playing', async () => {
  const t = readerRuntime({ accepted: true }); await t.mount();
  const sdk = t.adapter.getSDK(); t.play(); t.progress(0, 0); t.progress(1);
  const first = t.videos().at(-1);
  sdk.ready(t.page, false); t.progress(2); t.progress(3);
  sdk.ready(t.page, true); t.progress(4); t.progress(5);
  const next = t.videos().at(-1);
  assert.equal(next.page_view_id, first.page_view_id);
  assert.equal(next.player_instance_id, first.player_instance_id);
  assert.deepEqual(next.played_ranges, [[0, 1000], [4000, 5000]]);
});

function coreRuntime({ failFirst = false, deferFirst = false, forceSession = false, online = false } = {}) {
  let now = 0, sequence = 0, account = '', resolveFirst;
  const requests = [], credentials = new Map(), views = new Map(), batches = [], boundaries = [];
  const sdk = createTracker({
    wall: () => 1700000000000 + now, mono: () => now, id: () => 'event_' + (++sequence),
    random: () => .5, account: () => account, load: () => [], save() {}, onBoundary: reason => boundaries.push(reason),
    async entry(body) {
      requests.push({ ...body }); const number = requests.length;
      if (number === 1 && failFirst) throw new Error('transient unavailable');
      const old = credentials.get(body.previous_entry_token);
      const entry = { entry_token: 'credential_' + number, session_id: !forceSession && old && now - old.active < SESSION_IDLE ? old.session_id : 'session_' + number,
        share_id: body.share_token ? 'share:' + body.share_token : undefined, content_accessible: true, active: now };
      credentials.set(entry.entry_token, entry);
      if (number === 1 && deferFirst) await new Promise(resolve => { resolveFirst = resolve; });
      return entry;
    },
    async send(body, credential) {
      if (!online) throw new Error('offline');
      const entry = credentials.get(credential);
      const results = body.events.map(event => {
        const invalid = event.previous_page_view_id && views.get(event.previous_page_view_id) !== entry.session_id;
        if (!invalid && event.event_type === 'page_view') views.set(event.page_view_id, entry.session_id);
        return { event_id: event.event_id, status: invalid ? 'rejected' : 'accepted', reason: invalid ? 'previous_page_view_invalid' : undefined };
      });
      entry.active = now; batches.push({ credential, events: body.events, results }); return { results };
    }
  });
  sdk.configure(true, 'v1'); sdk.appShow();
  return { sdk, requests, credentials, views, batches, boundaries, advance: ms => { now += ms; }, setAccount: value => { account = value; },
    resolveFirst: () => resolveFirst(),
    async mount(key = 'a', route = 'pages/index/index', share) { sdk.register(key, route, {}, share); sdk.show(key); sdk.ready(key, true); await settle(); },
    events: () => [...batches.flatMap(batch => batch.events), ...sdk.queueForTest().map(row => row.event)]
  };
}

test('transient first entry failure retries the same pending share once without a second boundary', async () => {
  const t = coreRuntime({ failFirst: true }); await t.mount('a', 'pages/index/index', 'incoming_share');
  t.advance(10000); t.sdk.tick(); await settle();
  assert.deepEqual(t.requests.map(request => request.share_token), ['incoming_share', 'incoming_share']);
  assert.equal(t.boundaries.filter(reason => reason === 'external_entry').length, 1);
  assert.equal(t.events().find(event => event.event_type === 'page_view').share_id, 'share:incoming_share');
  t.sdk.tick(); t.sdk.show('a'); await settle(); assert.equal(t.requests.length, 2);
  assert.equal(t.sdk.get('a').share, '');
});

for (const change of ['withdrawal', 'account', 'hide', 'unload', 'navigate', 'replacement', 'content']) {
  for (const pending of ['failed', 'in-flight']) {
    test('pending ' + pending + ' share cannot revive after ' + change, async () => {
      const t = coreRuntime({ failFirst: pending === 'failed', deferFirst: pending === 'in-flight' });
      await t.mount('a', 'pages/index/index', 'old_share');
      if (change === 'withdrawal') { t.sdk.configure(true, ''); t.sdk.configure(true, 'v1'); }
      if (change === 'account') { t.setAccount('other-account'); t.sdk.tick(); }
      if (change === 'hide') { t.sdk.hide('a', false); t.sdk.show('a'); }
      if (change === 'unload') { t.sdk.hide('a', true); await t.mount('a'); }
      if (change === 'navigate') { await t.mount('b', 'pages/aicost/aicost'); t.sdk.hide('b', true); t.sdk.show('a'); }
      if (change === 'content') t.sdk.ready('a', true, { id: 'replacement_content', version: 2 });
      if (change === 'replacement') t.sdk.appShow({ path: 'pages/index/index', scene: 1007, query: { st: 'new_share' } });
      if (pending === 'in-flight') t.resolveFirst();
      await settle(); t.advance(10000); t.sdk.tick(); await settle();
      const latest = t.events().filter(event => event.event_type === 'page_view').at(-1);
      assert.ok(latest);
      assert.equal(latest.share_id, change === 'replacement' ? 'share:new_share' : undefined);
      assert.ok(t.requests.slice(1).every(request => request.share_token !== 'old_share'));
    });
  }
}

test('A to B to cached A restores the actual active context before 31 minutes of heartbeats and C', async () => {
  const t = coreRuntime({ online: true }); await t.mount();
  const initial = t.sdk.get('a').context;
  t.sdk.hide('a', false); await t.mount('b', 'pages/aicost/aicost');
  t.sdk.hide('b', true); t.sdk.show('a');
  assert.equal(t.sdk.context(), t.sdk.get('a').context);
  for (let i = 0; i < 186; i++) { t.advance(10000); t.sdk.tick(); await settle(); }
  t.sdk.hide('a', false); await t.mount('c', 'pages/aitools/aitools'); await t.sdk.flush();
  assert.equal(t.requests.at(-1).previous_entry_token, initial.entry_token);
  assert.equal(t.sdk.get('c').context.session_id, initial.session_id);
  assert.ok(t.batches.flatMap(batch => batch.results).every(result => result.status === 'accepted'));
});

test('server-created new sessions never reference previous views from another session, including cached return', async () => {
  const t = coreRuntime({ online: true, forceSession: true }); await t.mount();
  const first = t.sdk.get('a').context;
  t.sdk.hide('a', false); await t.mount('b', 'pages/aicost/aicost');
  t.advance(10000); t.sdk.tick(); await settle();
  t.sdk.hide('b', true); t.sdk.show('a'); await settle();
  t.advance(10000); t.sdk.tick(); await settle(); await t.sdk.flush();
  const events = t.events();
  assert.notEqual(t.sdk.context().session_id, first.session_id, 'a cached page cannot resurrect an invalidated server session');
  assert.ok(events.filter(event => event.session_id !== first.session_id).every(event => event.previous_page_view_id === undefined));
  assert.ok(t.batches.flatMap(batch => batch.results).every(result => result.status === 'accepted'));
});
