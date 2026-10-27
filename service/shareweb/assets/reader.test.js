const test = require('node:test');
const assert = require('node:assert/strict');
const { Tracker, ReaderTracking, createPost } = require('./reader.js');

let nextID = 0;
const makeID = () => `secure-test-id-${String(++nextID).padStart(4, '0')}`;

test('refusal and hidden pages do not send reading events', async () => {
  const sent = [];
  const tracker = new Tracker({ send: async event => sent.push(event), now: () => 1000, makeID });
  await tracker.pageView(false, true);
  await tracker.pageView(true, false);
  assert.deepEqual(sent, []);
});

test('retry keeps the same event id', async () => {
  const ids = [];
  let attempts = 0;
  const tracker = new Tracker({
    now: () => 1000,
    makeID,
    send: async event => {
      ids.push(event.event_id);
      attempts++;
      if (attempts === 1) throw new Error('temporary');
    }
  });
  await tracker.pageView(true, true);
  assert.equal(attempts, 2);
  assert.equal(ids[0], ids[1]);
});

function sessionFixture(overrides = {}) {
  let mono = 0;
  let visible = true;
  let intervalCount = 0;
  const sent = [];
  const session = new ReaderTracking({
    enter: async () => ({ entry_token: 'entry-token-0001', session_id: 'session-0001', route_key: 'pages/contentreader/contentreader', content_id: 'content-0001', content_version: 1, share_id: 'share-0001' }),
    send: async (token, event) => sent.push({ token, event }),
    monotonic: () => mono,
    now: () => 1000 + mono,
    makeID,
    isVisible: () => visible,
    setInterval: () => { intervalCount++; return intervalCount; },
    clearInterval: () => {},
    ...overrides
  });
  return { session, sent, setMono: value => { mono = value; }, setVisible: value => { visible = value; }, intervalCount: () => intervalCount };
}

test('client monotonic time increases while visible', async () => {
  const f = sessionFixture();
  await f.session.enable();
  f.setMono(1000);
  await f.session.tick();
  f.setMono(2000);
  await f.session.tick();
  assert.deepEqual(f.sent.map(item => item.event.client_monotonic_ms), [0, 1000, 2000]);
});

test('background time is never counted as visible', async () => {
  const f = sessionFixture();
  await f.session.enable();
  f.setMono(500);
  await f.session.visibilityChanged(false);
  f.setVisible(false);
  f.setMono(10000);
  await f.session.tick();
  f.setVisible(true);
  f.setMono(20000);
  await f.session.visibilityChanged(true);
  f.setMono(20500);
  await f.session.tick();
  const segments = f.sent.filter(item => item.event.event_type === 'visible_segment');
  assert.deepEqual(segments.map(item => item.event.visible_delta_ms), [500, 500]);
  assert.deepEqual(segments.map(item => item.event.client_monotonic_ms), [500, 20500]);
});

test('closing stops timers and emits no later events', async () => {
  let cleared = 0;
  const f = sessionFixture({ clearInterval: () => { cleared++; } });
  await f.session.enable();
  f.session.close();
  f.setMono(10000);
  await f.session.tick();
  assert.equal(f.sent.length, 1);
  assert.equal(cleared, 1);
});

test('enable is single-flight and creates one interval', async () => {
  let resolveEntry;
  let enters = 0;
  const pending = new Promise(resolve => { resolveEntry = resolve; });
  const f = sessionFixture({ enter: async () => { enters++; return pending; } });
  const first = f.session.enable();
  const second = f.session.enable();
  resolveEntry({ entry_token: 'entry-token-0001', session_id: 'session-0001', route_key: 'page' });
  assert.deepEqual(await Promise.all([first, second]), [true, true]);
  assert.equal(enters, 1);
  assert.equal(f.intervalCount(), 1);
  assert.equal(f.sent.length, 1);
});

test('entry returning after close never enables tracking', async () => {
  let resolveEntry;
  const pending = new Promise(resolve => { resolveEntry = resolve; });
  const f = sessionFixture({ enter: async () => pending });
  const enabling = f.session.enable();
  f.session.close();
  resolveEntry({ entry_token: 'entry-token-0001', session_id: 'session-0001', route_key: 'page' });
  assert.equal(await enabling, false);
  assert.equal(f.sent.length, 0);
  assert.equal(f.intervalCount(), 0);
});

test('missing secure randomness keeps reading available but tracking disabled', async () => {
  let enters = 0;
  const f = sessionFixture({ makeID: () => null, enter: async () => { enters++; } });
  assert.equal(await f.session.enable(), false);
  assert.equal(enters, 0);
  assert.equal(f.sent.length, 0);
});

test('post times out even when AbortController is unavailable', async () => {
  let expire;
  const post = createPost(() => new Promise(() => {}), {
    AbortController: null,
    timeoutMS: 100,
    setTimeout: callback => { expire = callback; return 1; },
    clearTimeout: () => {}
  });
  const request = post('/entry', { code: 'share-code' });
  expire();
  await assert.rejects(request, /timeout/);
});

test('post bounds response body parsing without AbortController', async () => {
 let expire;
 const post=createPost(async()=>({ok:true,json:()=>new Promise(()=>{})}),{AbortController:null,timeoutMS:100,setTimeout:fn=>{expire=fn;return 1;},clearTimeout(){}});
 const pending=post('/entry',{});await new Promise(resolve=>setImmediate(resolve));expire();
 await assert.rejects(pending,/timeout/);
});
