'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const { createTracker } = require('../miniprogram/utils/tracking/core');

const report = 'pages/aicostshare/aicostshare';
const home = 'pages/index/index';
const settle = () => new Promise(resolve => setImmediate(resolve));

function runtime({ account = '', entry } = {}) {
  let currentAccount = account, sequence = 0, now = 1000;
  const requests = [], events = [];
  const sdk = createTracker({
    wall: () => 1700000000000 + now, mono: () => now,
    id: () => `event_${++sequence}`, random: () => 0.5,
    account: () => currentAccount, load: () => [], save() {},
    async entry(body) {
      requests.push({ ...body, account: currentAccount });
      if (entry) return entry(body, currentAccount);
      return {
        entry_token: `entry_${requests.length}`, session_id: `session_${requests.length}`,
        share_id: body.share_token ? `share:${body.share_token}` : undefined,
        content_accessible: true,
      };
    },
    async send(body) {
      events.push(...body.events);
      return { results: body.events.map(event => ({ event_id: event.event_id, status: 'accepted' })) };
    },
  });
  sdk.configure(true, 'notice-v1');
  sdk.appShow();
  return {
    sdk, requests, events,
    login(value = 'account-A') { currentAccount = value; },
    advance(value) { now += value; },
    open(key = 'report', route = report, share = 'incoming-share') {
      sdk.register(key, route, {}, share);
      sdk.show(key);
    },
    async ready(key = 'report') { sdk.ready(key, true); await settle(); },
  };
}

test('login before first content readiness keeps the same unconsumed share and parent attribution', async () => {
  const t = runtime();
  t.open();
  assert.equal(t.requests.length, 0);
  t.login();
  await t.ready();
  assert.equal(t.requests.length, 1);
  assert.equal(t.requests[0].share_token, 'incoming-share');
  assert.equal(t.requests[0].previous_entry_token, undefined);
  assert.equal(t.sdk.context().share_id, 'share:incoming-share');
  assert.equal(t.events.find(event => event.event_type === 'page_view').share_id, 'share:incoming-share');
});

test('a profile gate may finish login before SDK page registration', async () => {
  const t = runtime();
  t.login();
  t.open();
  await t.ready();
  assert.equal(t.requests[0].share_token, 'incoming-share');
});

test('login between registration and show preserves only the page being shown', async () => {
  const t = runtime();
  t.sdk.register('other', home, {}, 'unrelated-share');
  t.sdk.register('report', report, {}, 'incoming-share');
  t.login();
  t.sdk.show('report');
  await t.ready();
  assert.equal(t.requests[0].share_token, 'incoming-share');
  assert.equal(t.sdk.get('other').share, '');
});

test('late anonymous entry response cannot overwrite login attribution or account credential', async () => {
  const pending = [];
  const t = runtime({ entry: () => new Promise(resolve => pending.push(resolve)) });
  t.open();
  t.sdk.ready('report', true);
  t.login();
  t.sdk.tick();
  assert.equal(pending.length, 2);
  assert.equal(t.requests[1].share_token, 'incoming-share');
  pending[0]({ entry_token: 'anonymous', session_id: 'old', share_id: 'old-share' });
  pending[1]({ entry_token: 'authenticated', session_id: 'new', share_id: 'incoming-share-id' });
  await settle();
  assert.equal(t.sdk.context().entry_token, 'authenticated');
  assert.deepEqual(t.events.filter(event => event.event_type === 'page_view').map(event => event.share_id), ['incoming-share-id']);
});

test('an already consumed anonymous share is linked by credential, never replayed as a new share entry', async () => {
  const t = runtime();
  t.open();
  await t.ready();
  t.login();
  t.sdk.tick();
  await settle();
  assert.equal(t.requests[1].share_token, undefined);
  assert.equal(t.requests[1].previous_entry_token, 'entry_1');
});

for (const nextAccount of ['', 'account-B']) {
  test(`pending attribution is cleared on ${nextAccount ? 'account switch' : 'logout'}`, async () => {
    const t = runtime({ account: 'account-A' });
    t.open();
    t.login(nextAccount);
    await t.ready();
    assert.equal(t.requests[0].share_token, undefined);
    assert.equal(t.requests[0].previous_entry_token, undefined);
  });
}

test('leaving a gated page discards its pending share; navigating back after login cannot resurrect it', async () => {
  const t = runtime();
  t.open();
  t.sdk.hide('report', false);
  t.open('home', home, '');
  t.login();
  await t.ready('home');
  t.sdk.hide('home', true);
  t.sdk.show('report');
  await t.ready();
  assert.ok(t.requests.every(request => request.share_token === undefined));
});

test('warm launch for another route never sends the incoming share to the old active page', async () => {
  const t = runtime({ account: 'account-A' });
  t.open('home', home, '');
  await t.ready('home');
  t.sdk.appHide();
  t.sdk.appShow({ path: report, scene: 1007, query: { st: 'new-report-share' } });
  await settle();
  assert.equal(t.requests.length, 1, 'target page owns the new entry');
  t.open('report', report, 'new-report-share');
  await t.ready();
  assert.equal(t.requests[1].route_key, report);
  assert.equal(t.requests[1].share_token, 'new-report-share');
  assert.equal(t.requests[1].previous_entry_token, undefined);
});

test('new warm share replaces failed pending attribution even while content is loading', async () => {
  let fail = true;
  const t = runtime({ entry: async body => {
    if (fail) throw new Error('offline');
    return { entry_token: 'new-entry', session_id: 'new-session', share_id: body.share_token };
  } });
  t.open();
  await t.ready();
  t.sdk.ready('report', false);
  t.sdk.appShow({ path: report, scene: 1007, query: { st: 'replacement-share' } });
  fail = false;
  t.login();
  t.sdk.show('report');
  await t.ready();
  assert.equal(t.requests.at(-1).share_token, 'replacement-share');
  assert.equal(t.sdk.context().share_id, 'replacement-share');
});

test('same-route warm entry waits for the target content binding and native show before attribution', async () => {
  const t = runtime({ account: 'account-A' });
  t.open();
  t.sdk.ready('report', true, { id: 'old-report' });
  await settle();
  t.sdk.appHide();
  t.sdk.appShow({ path: report, scene: 1007, query: { report_id: 'new-report', st: 'new-share' } });
  t.sdk.tick();
  await settle();
  assert.equal(t.requests.length, 1, 'App.onShow must not attach new share to the old content');
  t.sdk.ready('report', true, { id: 'new-report' });
  await settle();
  assert.equal(t.requests.length, 1, 'profile gate must also wait for the native page show');
  t.sdk.show('report');
  await settle();
  assert.equal(t.requests.length, 2);
  assert.equal(t.requests[1].content_id, 'new-report');
  assert.equal(t.requests[1].share_token, 'new-share');
  assert.equal(t.requests[1].previous_entry_token, undefined);
});

test('the same warm launch query after background recovery does not duplicate a consumed visit', async () => {
  const t = runtime({ account: 'account-A' });
  const launch = { path: report, scene: 1007, query: { st: 'incoming-share' } };
  t.sdk.appShow(launch);
  t.open();
  await t.ready();
  t.sdk.appHide();
  t.sdk.hide('report', false);
  t.advance(1000);
  t.sdk.appShow(launch);
  t.sdk.show('report');
  await settle();
  assert.equal(t.requests.length, 1);
  assert.equal(t.events.filter(event => event.event_type === 'page_view').length, 1);
});
