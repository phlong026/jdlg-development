'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const { createTracker } = require('../miniprogram/utils/tracking/core');
const tick = () => new Promise(resolve => setImmediate(resolve));
function runtime({ failFirst = false } = {}) {
  let serial = 0, clock = 0, account = '';
  const requests = [];
  const sdk = createTracker({
    wall: () => 1700000000000 + clock, mono: () => clock, id: () => 'event-' + (++serial),
    random: () => .5, account: () => account, load: () => [], save() {},
    entry: async body => { requests.push(body); if (failFirst && requests.length === 1) throw new Error('offline'); return { entry_token: 'entry-0001', session_id: 'session-0001', content_accessible: true }; },
    send: async body => ({ results: body.events.map(event => ({ event_id: event.event_id, status: 'accepted' })) }),
  });
  sdk.appShow(); sdk.configure(true, 'v1'); sdk.register('page', 'pages/index/index', {}, 'share-0001', 'handoff-0001'); sdk.show('page'); sdk.ready('page', true);
  return { sdk, requests, advance: value => { clock += value; }, account: value => { account = value; } };
}
test('handoff accompanies only its first successful attributed entry', async () => {
  const { sdk, requests } = runtime(); await tick();
  assert.equal(requests[0].handoff_token, 'handoff-0001');
  assert.equal(sdk.get('page').handoff, '');
  sdk.hide('page', false); sdk.show('page'); await tick();
  assert.equal(requests.length, 1);
});
test('handoff retry keeps the proof until success, leaving clears it', async () => {
  const { sdk, requests } = runtime({ failFirst: true }); await tick(); sdk.tick(); await tick();
  assert.deepEqual(requests.map(body => body.handoff_token), ['handoff-0001', 'handoff-0001']);
  const other = runtime({ failFirst: true }); await tick(); other.sdk.hide('page', false); other.sdk.show('page'); await tick();
  assert.equal(other.requests[1].handoff_token, undefined);
});
test('identity and withdrawn consent never carry an old handoff', async () => {
  const t = runtime({ failFirst: true }); await tick(); t.account('account-b'); t.sdk.tick(); await tick();
  assert.equal(t.requests[1].handoff_token, undefined);
  const other = runtime({ failFirst: true }); await tick(); other.sdk.configure(true, ''); other.sdk.configure(true, 'v1'); await tick();
  assert.equal(other.requests[1].handoff_token, undefined);
});
test('new shares never forward a previous cross-device proof', () => {
  const { appendToken } = require('../miniprogram/utils/tracking/index');
  assert.equal(appendToken('/pages/contentreader/contentreader?content_id=content-0001&ht=old-proof&st=old-share', 'new-share'), '/pages/contentreader/contentreader?content_id=content-0001&st=new-share');
});
