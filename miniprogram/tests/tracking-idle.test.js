'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const { createTracker, SESSION_IDLE, TTL } = require('../miniprogram/utils/tracking/core');

// Exercise the production state machine and its retained page objects. The fake
// entry service deliberately reuses sessions and shares when passed an old token,
// so clearing only the local PV cannot conceal a stale-attribution regression.
function runtime({ start = 1000, initialQueue = [], validatePrevious = false } = {}) {
  let mono = start, wall = 1700000000000, sequence = 0, account = '', online = false;
  const requests = [], credentials = new Map(), boundaries = [], batches = [], knownViews = new Set();
  const sdk = createTracker({
    mono: () => mono, wall: () => wall, id: () => `event_${++sequence}`,
    random: () => 0.5, account: () => account, load: () => initialQueue.slice(), save() {},
    onBoundary: reason => boundaries.push(reason),
    async entry(body) {
      requests.push({ ...body });
      const prior = credentials.get(body.previous_entry_token);
      const number = requests.length;
      const sameAccount = prior && prior.account === account;
      const response = {
        entry_token: `credential_${number}`,
        session_id: sameAccount ? prior.session_id : `session_${number}`,
        share_id: body.share_token ? `share:${body.share_token}` : sameAccount ? prior.share_id : undefined,
        content_accessible: true
      };
      credentials.set(response.entry_token, { ...response, account });
      return response;
    },
    async send(body, credential) {
      if (!online) throw new Error('offline');
      const results = body.events.map(event => {
        const missingPrevious = validatePrevious && event.event_type === 'page_view' &&
          event.previous_page_view_id && !knownViews.has(event.previous_page_view_id);
        if (event.event_type === 'page_view' && !missingPrevious) knownViews.add(event.page_view_id);
        return { event_id: event.event_id, status: missingPrevious ? 'rejected' : 'accepted' };
      });
      batches.push({ credential, events: body.events, results });
      return { results };
    }
  });
  return {
    sdk, requests, credentials, boundaries, batches,
    advance(ms) { mono += ms; wall += ms; },
    setAccount(value) { account = value; }, setOnline() { online = true; },
    events(type) { return sdk.queueForTest().map(row => row.event).filter(event => !type || event.event_type === type); }
  };
}
const settle = () => new Promise(resolve => setImmediate(resolve));
const launch = { path: 'pages/index/index', scene: 1007, query: { st: 'original_share' } };
async function mount(t) {
  t.sdk.configure(true, 'notice-v1');
  t.sdk.appShow(launch);
  t.sdk.register('page', launch.path, {}, launch.query.st);
  t.sdk.show('page');
  t.sdk.ready('page', true);
  await settle();
  assert.equal(t.events('page_view').length, 1);
  return t.sdk.get('page');
}
function hide(t) { t.sdk.appHide(); t.sdk.hide('page', false); }
function resume(t) { t.sdk.appShow(launch); t.sdk.show('page'); }
function assertFresh(t, first) {
  const views = t.events('page_view');
  assert.equal(views.length, 2);
  assert.notEqual(views[1].session_id, first.session_id);
  assert.equal(views[1].share_id, undefined);
  assert.equal(views[1].previous_page_view_id, undefined);
  assert.equal(t.requests.at(-1).previous_entry_token, undefined);
  assert.equal(t.requests.at(-1).share_token, undefined);
  assert.equal(t.boundaries.filter(reason => reason === 'idle').length, 1);
}

test('retained entry-ready page resumes after 31 minutes with a new session and no old share', async () => {
  const t = runtime(), page = await mount(t), first = t.events('page_view')[0];
  page.shareCache.cached = { query: 'st=old_outgoing_share' };
  t.advance(1000); hide(t); t.advance(31 * 60 * 1000);
  assert.equal(page.entryReady, true, 'the original page and ready entry stay in memory');
  resume(t); await settle();
  assert.equal(t.sdk.get('page'), page, 'no reload or new tracker hides the warm-resume bug');
  assertFresh(t, first);
  assert.deepEqual(page.shareCache, {});
  assert.equal(t.requests.length, 2, 'App.onShow and Page.onShow share one pending entry');
  t.advance(1000); t.sdk.tick();
  assert.equal(t.events('visible_segment').reduce((sum, event) => sum + event.visible_delta_ms, 0), 2000);
});

test('App.onShow renews an idle context even when no Page.onHide/onShow callbacks run', async () => {
  const t = runtime(); await mount(t); const first = t.events('page_view')[0];
  t.sdk.appHide(); t.advance(SESSION_IDLE + 1); t.sdk.appShow(launch); await settle();
  assertFresh(t, first);
});

test('Page.onShow alone cannot revive an entry-ready idle session', async () => {
  const t = runtime(); await mount(t); const first = t.events('page_view')[0];
  t.sdk.hide('page', false); t.advance(SESSION_IDLE + 1); t.sdk.show('page'); await settle();
  assertFresh(t, first);
});

test('short background periods and exactly 30 minutes preserve the same PV, session and share', async () => {
  const t = runtime(); await mount(t); const first = t.events('page_view')[0];
  for (const elapsed of [60000, SESSION_IDLE, 60000]) {
    hide(t); t.advance(elapsed); resume(t); await settle();
    assert.deepEqual(t.events('page_view'), [first]);
    assert.equal(t.sdk.context().session_id, first.session_id);
    assert.equal(t.sdk.context().share_id, first.share_id);
  }
  assert.equal(t.requests.length, 1);
  assert.equal(t.boundaries.includes('idle'), false);
});

test('a session started at monotonic zero still expires after inactivity', async () => {
  const t = runtime({ start: 0 }); await mount(t); const first = t.events('page_view')[0];
  hide(t); t.advance(SESSION_IDLE + 1); resume(t); await settle(); assertFresh(t, first);
});

test('regular foreground heartbeat activity for over 30 minutes does not split navigation', async () => {
  const t = runtime(); await mount(t); const first = t.events('page_view')[0];
  for (let i = 0; i < 240; i++) { t.advance(10000); t.sdk.tick(); }
  t.sdk.hide('page', false); t.sdk.register('next', 'pages/aicost/aicost', {});
  t.sdk.show('next'); t.sdk.ready('next', true); await settle();
  const next = t.events('page_view').at(-1);
  assert.equal(next.session_id, first.session_id);
  assert.equal(next.share_id, first.share_id);
  assert.equal(next.previous_page_view_id, first.page_view_id);
  assert.equal(t.boundaries.includes('idle'), false);
});

test('continuous visible user actions refresh activity even when the heartbeat is delayed', async () => {
  const t = runtime(); await mount(t); const first = t.events('page_view')[0];
  for (let i = 0; i < 40; i++) {
    t.advance(60000); t.sdk.emit('page', 'scroll_depth', { scroll_depth: 0.5 });
  }
  t.sdk.hide('page', false); t.sdk.register('next', 'pages/aicost/aicost', {});
  t.sdk.show('next'); t.sdk.ready('next', true); await settle();
  assert.equal(t.events('page_view').at(-1).session_id, first.session_id);
  assert.equal(t.boundaries.includes('idle'), false);
});

test('background events do not keep an inactive session alive', async () => {
  const t = runtime(); await mount(t); const first = t.events('page_view')[0]; hide(t);
  for (let i = 0; i < 31; i++) { t.advance(60000); t.sdk.emit('page', 'scroll_depth', { scroll_depth: 0.5 }); }
  resume(t); await settle(); assertFresh(t, first);
});

test('idle boundary keeps old queued events under their original credentials', async () => {
  const t = runtime(); await mount(t); const first = t.events('page_view')[0];
  const oldRows = t.sdk.queueForTest().map(row => ({ id: row.event.event_id, credential: row.credential }));
  hide(t); t.advance(SESSION_IDLE + 1); resume(t); await settle(); assertFresh(t, first);
  const rows = t.sdk.queueForTest();
  for (const old of oldRows) assert.equal(rows.find(row => row.event.event_id === old.id).credential, old.credential);
  assert.ok(rows.some(row => row.credential === 'credential_1'));
  assert.ok(rows.some(row => row.credential === 'credential_2'));
  t.setOnline(); t.advance(5000); await t.sdk.flush();
  assert.equal(t.sdk.diagnostics().queued, 0);
  assert.equal(t.batches.length, 2);
  for (const batch of t.batches) assert.ok(batch.events.every(event => event.session_id === t.credentials.get(batch.credential).session_id));
});

test('short-idle login linkage remains allowed, but an expired anonymous session cannot link on login', async () => {
  for (const expired of [false, true]) {
    const t = runtime(); await mount(t); hide(t);
    t.advance(expired ? SESSION_IDLE + 1 : 60000); t.setAccount('logged-in-account');
    resume(t); await settle();
    assert.equal(t.requests.at(-1).previous_entry_token, expired ? undefined : 'credential_1');
    assert.equal(t.events('page_view').length, 2);
    assert.equal(t.events('page_view')[1].share_id, undefined);
    t.setAccount(''); t.sdk.tick(); await settle();
    assert.equal(t.requests.at(-1).previous_entry_token, undefined, 'logout never carries the previous account entry');
  }
});

test('offline A to B to cached A uploads credential runs in navigation order', async () => {
  const t = runtime({ validatePrevious: true }); await mount(t);
  t.advance(1000); t.sdk.hide('page', false);
  t.sdk.register('next', 'pages/aicost/aicost', {}); t.sdk.show('next'); t.sdk.ready('next', true); await settle();
  t.advance(1000); t.sdk.hide('next', true); t.sdk.show('page');
  const views = t.events('page_view');
  assert.equal(views.length, 3);
  assert.equal(views[1].previous_page_view_id, views[0].page_view_id);
  assert.equal(views[2].previous_page_view_id, views[1].page_view_id);
  t.setOnline(); t.advance(5000); await t.sdk.flush();
  assert.deepEqual(t.batches.map(batch => batch.credential), ['credential_1', 'credential_2', 'credential_1']);
  assert.deepEqual(t.batches.flatMap(batch => batch.events.filter(event => event.event_type === 'page_view')), views);
  assert.ok(t.batches.flatMap(batch => batch.results).every(result => result.status === 'accepted'), 'no predecessor is uploaded after its referring page');
  assert.equal(t.sdk.diagnostics().dropped.rejected, 0);
  assert.equal(t.sdk.diagnostics().queued, 0);
});

test('temporary config suspension preserves queued events and pauses sampling and upload', async () => {
  const t = runtime(); await mount(t); const first = t.events('page_view')[0];
  t.advance(1000); t.sdk.suspend(); const saved = t.sdk.queueForTest();
  assert.equal(t.sdk.diagnostics().enabled, false);
  t.setOnline(); t.advance(60000); t.sdk.show('page'); t.sdk.ready('page', true); t.sdk.tick();
  t.sdk.emit('page', 'scroll_depth', { scroll_depth: 0.5 }); await t.sdk.flush();
  assert.deepEqual(t.sdk.queueForTest(), saved);
  assert.equal(t.batches.length, 0);
  t.sdk.configure(true, 'notice-v1');
  assert.equal(t.sdk.diagnostics().enabled, true);
  assert.deepEqual(t.events('page_view'), [first], 'a short config outage is not a new visit');
  t.advance(1000); t.sdk.tick(); await settle(); await t.sdk.flush();
  assert.equal(t.sdk.diagnostics().queued, 0);
  assert.equal(t.batches.flatMap(batch => batch.events).filter(event => event.event_type === 'visible_segment')
    .reduce((sum, event) => sum + event.visible_delta_ms, 0), 2000, 'configuration outage time is not recorded as visible');
});

test('long config suspension resumes with a clean session and attribution', async () => {
  const t = runtime(); await mount(t); const first = t.events('page_view')[0];
  t.sdk.suspend(); t.advance(SESSION_IDLE + 1); t.sdk.configure(true, 'notice-v1'); await settle();
  assertFresh(t, first);
});

function restoredQueue() {
  return [{ credential: 'restored_credential', queued_at: 1700000000000,
    event: { event_id: 'restored_event', event_type: 'page_view', page_view_id: 'restored_view', session_id: 'restored_session' } }];
}

test('cold-start suspension retains restored queue until a confirmed enabled config returns', async () => {
  const saved = restoredQueue(), t = runtime({ initialQueue: saved });
  t.sdk.suspend(); t.setOnline(); t.advance(60000); t.sdk.tick(); await t.sdk.flush();
  assert.deepEqual(t.sdk.queueForTest(), saved);
  assert.equal(t.batches.length, 0);
  t.sdk.configure(true, 'notice-v1'); await t.sdk.flush();
  assert.equal(t.batches[0].credential, 'restored_credential');
  assert.equal(t.batches[0].events[0].event_id, 'restored_event');
  assert.equal(t.sdk.diagnostics().queued, 0);
});

test('resuming a configuration outage still expires queue entries older than 72 hours', async () => {
  const t = runtime({ initialQueue: restoredQueue() });
  t.sdk.suspend(); t.advance(TTL + 1); t.setOnline();
  t.sdk.configure(true, 'notice-v1'); await t.sdk.flush();
  assert.equal(t.batches.length, 0);
  assert.equal(t.sdk.diagnostics().queued, 0);
  assert.equal(t.sdk.diagnostics().dropped.expired, 1);
});

test('explicit disabled config or revoked consent still clears restored and suspended queues', async () => {
  for (const [flag, version] of [[false, ''], [false, 'notice-v1'], [true, '']]) {
    for (const suspended of [false, true]) {
      const t = runtime({ initialQueue: restoredQueue() });
      if (suspended) t.sdk.suspend();
      t.sdk.configure(flag, version);
      assert.equal(t.sdk.diagnostics().queued, 0);
      assert.equal(t.sdk.diagnostics().enabled, false);
    }
    const t = runtime(); await mount(t); t.sdk.suspend(); t.sdk.configure(flag, version);
    assert.equal(t.sdk.diagnostics().queued, 0);
  }
});

test('actual adapter config-fetch failure preserves cold-start queue, and success resumes delivery', async () => {
  const fs = require('node:fs'), path = require('node:path'), vm = require('node:vm');
  const saved = restoredQueue(), sent = [], storage = {
    share_tracking_queue_v1: saved,
    share_tracking_consent_v1: { version: 'notice-v1' }
  };
  let configFails = true;
  const api = { async request(suffix, method, body) {
    if (suffix === '/config') {
      if (configFails) throw new Error('offline config fetch');
      return { enabled: true, consent_version: 'notice-v1', privacy_notice: 'purpose and categories' };
    }
    if (suffix === '/events/batch') {
      sent.push(...body.events);
      return { results: body.events.map(event => ({ event_id: event.event_id, status: 'accepted' })) };
    }
    throw new Error(`Unexpected request ${suffix}`);
  } };
  const sandbox = {
    module: { exports: {} },
    require: name => name === './api' ? api : require('../miniprogram/utils/tracking/' + name.slice(2)),
    wx: { getPerformance: () => ({ now: () => 1000 }), getStorageSync: key => storage[key],
      setStorageSync: (key, value) => { storage[key] = value; } },
    Date: { now: () => 1700000000000 }, Math, Promise
  };
  vm.runInNewContext(fs.readFileSync(path.join(__dirname, '../miniprogram/utils/tracking/index.js'), 'utf8'), sandbox);
  const adapter = sandbox.module.exports, sdk = adapter.getSDK();
  await adapter.refreshConfig();
  assert.deepEqual(sdk.queueForTest(), saved);
  assert.deepEqual(storage.share_tracking_queue_v1, saved);
  assert.equal(sdk.diagnostics().enabled, false);
  await sdk.flush(); assert.equal(sent.length, 0);
  configFails = false; await adapter.refreshConfig(); await sdk.flush();
  assert.equal(sent.length, 1);
  assert.equal(sent[0].event_id, 'restored_event');
  assert.equal(sdk.diagnostics().queued, 0);
});

for (const oldFirst of [true, false]) {
  for (const oldFails of [true, false]) {
    test(`content switch invalidates pending entry (${oldFirst ? 'old' : 'new'} finishes first; old ${oldFails ? 'rejects' : 'resolves'})`, async () => {
      let sequence = 0; const pending = [];
      const sdk = createTracker({
        mono: () => 1000, wall: () => 1700000000000, id: () => `content_event_${++sequence}`,
        random: () => 0.5, account: () => '', load: () => [], save() {},
        entry: body => new Promise((resolve, reject) => pending.push({ body, resolve, reject })),
        send: async () => { throw new Error('offline'); }
      });
      sdk.configure(true, 'notice-v1'); sdk.appShow();
      sdk.register('reader', 'pages/contentreader/contentreader', { id: 'content_one', version: 1 });
      sdk.show('reader'); sdk.ready('reader', true);
      assert.equal(pending.length, 1);
      sdk.ready('reader', true, { id: 'content_two', version: 2 });
      assert.equal(pending.length, 2, 'the new content starts its own entry without waiting for the obsolete request');
      assert.equal(pending[1].body.content_id, 'content_two');
      assert.equal(pending[1].body.content_version, 2);
      const finishOld = async () => {
        if (oldFails) pending[0].reject(new Error('late failed request for content_one'));
        else pending[0].resolve({ entry_token: 'entry_one', session_id: 'session_one', content_version: 1, content_accessible: true });
        await settle();
      };
      const finishNew = async () => {
        pending[1].resolve({ entry_token: 'entry_two', session_id: 'session_two', content_version: 2, content_accessible: true });
        await settle();
      };
      if (oldFirst) {
        await finishOld();
        assert.equal(sdk.get('reader').entering, true, 'old finally cannot unlock the newer in-flight request');
        assert.equal(sdk.get('reader').context, null);
        assert.equal(sdk.queueForTest().length, 0);
        sdk.tick(); sdk.show('reader'); sdk.ready('reader', true);
        assert.equal(pending.length, 2, 'repeat lifecycle hooks do not duplicate the current request');
        await finishNew();
      } else {
        await finishNew(); await finishOld();
      }
      assert.equal(sdk.get('reader').entryReady, true, 'old catch cannot invalidate the successful current entry');
      assert.equal(sdk.get('reader').context.entry_token, 'entry_two');
      assert.equal(sdk.get('reader').entering, false);
      sdk.tick();
      assert.equal(pending.length, 2);
      const views = sdk.queueForTest().filter(row => row.event.event_type === 'page_view');
      assert.equal(views.length, 1);
      assert.equal(views[0].credential, 'entry_two');
      assert.equal(views[0].event.content_id, 'content_two');
      assert.equal(views[0].event.content_version, 2);
      assert.equal(views[0].event.session_id, 'session_two');
    });
  }
}

function pendingConsent(t) {
  t.sdk.appShow(launch);
  t.sdk.register('page', launch.path, {}, launch.query.st);
  t.sdk.show('page'); t.sdk.ready('page', true);
  t.sdk.configure(true, '', { awaitConsent: true });
  assert.equal(t.requests.length, 0);
  assert.equal(t.events().length, 0);
}

test('config awaiting first consent retains only the current unused share until an explicit grant', async () => {
  const t = runtime(); pendingConsent(t);
  t.sdk.configure(true, '', { awaitConsent: true });
  assert.equal(t.sdk.get('page').share, 'original_share');
  assert.equal(t.requests.length, 0);
  t.sdk.configure(true, 'notice-v1'); await settle();
  assert.equal(t.requests.length, 1);
  assert.equal(t.requests[0].share_token, 'original_share');
  assert.equal(t.events('page_view')[0].share_id, 'share:original_share');
});

test('an explicit denial or withdrawal cannot restore the old share on subsequent consent', async () => {
  for (const previouslyGranted of [false, true]) {
    const t = runtime(); pendingConsent(t);
    if (previouslyGranted) { t.sdk.configure(true, 'notice-v1'); await settle(); }
    t.sdk.configure(true, '');
    assert.equal(t.sdk.get('page').share, '');
    t.sdk.configure(true, '', { awaitConsent: true });
    t.sdk.configure(true, 'notice-v1'); await settle();
    assert.equal(t.requests.at(-1).share_token, undefined);
    assert.equal(t.events('page_view').at(-1).share_id, undefined);
  }
});

test('an account switch before consent discards the pending page share', async () => {
  const t = runtime(); pendingConsent(t); t.setAccount('different-account');
  t.sdk.configure(true, 'notice-v1'); await settle();
  assert.equal(t.requests.length, 1);
  assert.equal(t.requests[0].share_token, undefined);
  assert.equal(t.events('page_view')[0].share_id, undefined);
});

test('granting consent away from the pending page neither enters it while hidden nor revives its share', async () => {
  const t = runtime(); pendingConsent(t); t.sdk.hide('page', false);
  t.sdk.configure(true, 'notice-v1'); await settle();
  assert.equal(t.requests.length, 0, 'grant from a private page does not collect the prior hidden public page');
  t.sdk.show('page'); await settle();
  assert.equal(t.requests.length, 1);
  assert.equal(t.requests[0].share_token, undefined);
});

test('switching pages before consent or leaving the app discards pending entry attribution', async () => {
  const navigated = runtime(); pendingConsent(navigated);
  navigated.sdk.register('next', 'pages/aicost/aicost', {});
  navigated.sdk.show('next'); navigated.sdk.ready('next', true);
  navigated.sdk.configure(true, 'notice-v1'); await settle();
  assert.equal(navigated.sdk.get('page').share, '');
  assert.equal(navigated.requests[0].share_token, undefined);
  const backgrounded = runtime(); pendingConsent(backgrounded);
  backgrounded.sdk.appHide(); backgrounded.advance(SESSION_IDLE + 1); backgrounded.sdk.appShow(launch);
  backgrounded.sdk.configure(true, 'notice-v1'); await settle();
  assert.equal(backgrounded.requests[0].share_token, undefined);
});
