const test = require('node:test');
const assert = require('node:assert/strict');
const { Tracker, ReaderTracking, VideoTracking, bindVideos, createPost } = require('./reader.js');

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
    setTimeout: () => 1,
    clearTimeout: () => {},
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

test('keepalive post starts a fetch without an abort signal', async () => {
  let request;
  const post = createPost(async (_path, options) => {
    request = options;
    return { ok: true, json: async () => ({ data: true }) };
  });
  await post('/events', { events: [] }, { keepalive: true });
  assert.equal(request.keepalive, true);
  assert.equal(Object.hasOwn(request, 'signal'), false);
});

function videoFixture() {
  let mono = 0;
  const sent = [];
  const session = { emit: async (type, fields, at) => sent.push({ type, fields, at }) };
  const video = new VideoTracking({
    mediaID: 'media-video-0001',
    playerID: 'share-video-1',
    monotonic: () => mono,
    reportEveryMS: 5000
  });
  video.setSession(session);
  video.setVisible(true);
  return { video, sent, setMono: value => { mono = value; } };
}

test('video progress counts only continuous visible playback and is rate limited', async () => {
  const f = videoFixture();
  f.video.state('playing', 0, 60);
  for (let second = 1; second <= 12; second++) {
    f.setMono(second * 1000);
    f.video.progress(second, 60);
  }
  await f.video.drain();

  const progress = f.sent.filter(item => item.type === 'video_progress');
  assert.equal(progress.length, 2);
  assert.deepEqual(progress.map(item => item.fields.video_watch_ms), [5000, 5000]);
  assert.deepEqual(progress[1].fields.played_ranges, [[0, 10000]]);
  assert.equal(progress[1].fields.unique_played_ms, 10000);
});

test('duplicate video state does not emit duplicate events', async () => {
  const f = videoFixture();
  f.video.state('playing', 0, 60);
  f.video.state('playing', 0, 60);
  await f.video.drain();
  assert.equal(f.sent.filter(item => item.type === 'video_state').length, 1);
});

test('video state waits for metadata instead of disappearing', async () => {
  const f = videoFixture();
  f.video.state('playing', 0, 0);
  assert.equal(f.sent.length, 0);
  f.setMono(500);
  f.video.progress(0.5, 60);
  await f.video.drain();
  assert.equal(f.sent.filter(item => item.type === 'video_state').length, 1);
  assert.equal(f.sent[0].fields.video_duration_ms, 60000);
});

test('video seek is reported but jump distance is never counted as watch time', async () => {
  const f = videoFixture();
  f.video.state('playing', 0, 60);
  f.setMono(1000);
  f.video.progress(1, 60);
  f.setMono(2000);
  f.video.progress(2, 60);
  f.setMono(2500);
  f.video.seeking(50, 60);
  f.setMono(3000);
  f.video.progress(50.5, 60);
  f.video.state('paused', 50.5, 60);
  await f.video.drain();

  const progress = f.sent.filter(item => item.type === 'video_progress');
  assert.equal(progress.length, 1);
  assert.equal(progress[0].fields.video_watch_ms, 2000);
  assert.deepEqual(progress[0].fields.played_ranges, [[0, 2000]]);
  assert.deepEqual(progress[0].fields.quality_flags, ['inferred_seek']);
});

test('hidden playback and consent reset do not leak into the next tracking session', async () => {
  const f = videoFixture();
  f.video.state('playing', 0, 60);
  f.setMono(1000);
  f.video.progress(1, 60);
  f.setMono(2000);
  f.video.progress(2, 60);
  f.video.flush(2, 60);
  f.video.setVisible(false);
  f.setMono(10000);
  f.video.progress(10, 60);
  f.video.setVisible(true);
  f.setMono(11000);
  f.video.progress(11, 60);
  f.setMono(12000);
  f.video.progress(12, 60);
  f.video.state('paused', 12, 60);
  await f.video.drain();

  const firstSession = f.sent.filter(item => item.type === 'video_progress');
  assert.equal(firstSession.length, 2);
  assert.deepEqual(firstSession.map(item => item.fields.video_watch_ms), [2000, 1000]);

  f.video.setSession(null);
  f.video.state('playing', 12, 60);
  f.setMono(13000);
  f.video.progress(13, 60);
  f.video.setSession({ emit: async (type, fields, at) => f.sent.push({ type, fields, at }) });
  f.video.setVisible(true);
  f.video.state('playing', 13, 60);
  f.setMono(14000);
  f.video.progress(14, 60);
  f.setMono(15000);
  f.video.progress(15, 60);
  f.video.state('paused', 15, 60);
  await f.video.drain();

  const allProgress = f.sent.filter(item => item.type === 'video_progress');
  assert.equal(allProgress.length, 3);
  assert.equal(allProgress[2].fields.video_watch_ms, 2000);
  assert.deepEqual(allProgress[2].fields.played_ranges, [[13000, 15000]]);
  assert.equal(allProgress[2].fields.seq, 2);
});

test('binding the same video twice does not duplicate DOM listeners', () => {
  const listeners = new Map();
  const element = {
    dataset: { mediaId: 'media-video-0001' },
    addEventListener(type, listener) {
      const entries = listeners.get(type) || [];
      entries.push(listener);
      listeners.set(type, entries);
    }
  };
  bindVideos([element], { monotonic: () => 0 });
  bindVideos([element], { monotonic: () => 0 });
  for (const entries of listeners.values()) assert.equal(entries.length, 1);
});

test('video events reuse reader retry without changing event identity', async () => {
  let mono = 0;
  const videoAttempts = [];
  let retry;
  const session = new ReaderTracking({
    enter: async () => ({ entry_token: 'entry-token-0001', session_id: 'session-0001', route_key: 'pages/contentreader/contentreader', content_id: 'content-0001', content_version: 1 }),
    send: async (_token, event) => {
      if (event.event_type !== 'video_state') return;
      videoAttempts.push(event);
      if (videoAttempts.length === 1) throw new Error('offline');
    },
    monotonic: () => mono,
    now: () => 1000 + mono,
    makeID,
    isVisible: () => true,
    setInterval: () => 1,
    clearInterval: () => {},
    setTimeout: callback => { retry = callback; return 1; },
    clearTimeout: () => {}
  });
  await session.enable();
  const video = new VideoTracking({ mediaID: 'media-video-0001', playerID: 'share-video-retry', monotonic: () => mono });
  video.setSession(session);
  video.setVisible(true);
  mono = 1000;
  video.state('playing', 0, 60);
  await video.drain();
  retry();
  await video.drain();

  assert.equal(videoAttempts.length, 2);
  assert.equal(videoAttempts[0].event_id, videoAttempts[1].event_id);
  assert.deepEqual(videoAttempts[0], videoAttempts[1]);
});

test('reader outbox survives sustained offline and resumes with stable event ids', async () => {
  let networkAvailable = false;
  let retry;
  const attempts = [];
  const delivered = [];
  const delays = [];
  const f = sessionFixture({
    send: async (_token, event) => {
      attempts.push(event.event_id);
      if (!networkAvailable) throw new Error('offline');
      delivered.push(event);
    },
    setTimeout: (callback, delay) => { retry = callback; delays.push(delay); return delays.length; },
    clearTimeout: () => {}
  });
  await f.session.enable();
  await f.session.drain();
  retry();
  await f.session.drain();
  assert.deepEqual(delays, [1000, 2000]);
  f.session.emit('content_click', { target_content_id: 'target-0001' }, 10);
  assert.equal(f.session.outboxSize(), 2);

  networkAvailable = true;
  f.session.online();
  await f.session.drain();
  assert.equal(f.session.outboxSize(), 0);
  assert.deepEqual(delivered.map(event => event.event_type), ['page_view', 'content_click']);
  assert.equal(attempts.length, 4);
  assert.equal(attempts[0], attempts[1]);
  assert.equal(attempts[1], attempts[2]);
  assert.equal(new Set(attempts).size, 2);
});

test('slow network keeps one pump and a bounded outbox with one overflow diagnostic', async () => {
  let release;
  let sends = 0;
  const diagnostics = [];
  const f = sessionFixture({
    send: () => { sends++; return new Promise(resolve => { release = resolve; }); },
    onDiagnostic: item => diagnostics.push(item)
  });
  await f.session.enable();
  for (let index = 0; index < 80; index++) {
    f.session.emit('content_click', { target_content_id: `target-${String(index).padStart(4, '0')}` }, index + 1);
  }
  assert.equal(sends, 1);
  assert.equal(f.session.outboxSize(), 50);
  assert.deepEqual(diagnostics.map(item => item.type), ['outbox_overflow']);
  f.setMono(1000);
  await f.session.tick();
  assert.equal(f.session.outboxSize(), 50);
  assert.ok(f.session.entry);
  assert.deepEqual(diagnostics.map(item => item.type), ['outbox_overflow']);
  f.session.close();
  assert.equal(f.session.outboxSize(), 0);
  release();
});

test('pagehide force flushes a two second watch through one keepalive batch', async () => {
  let mono = 0;
  let keepalive = [];
  const f = sessionFixture({
    monotonic: () => mono,
    sendKeepalive: async (_token, events) => { keepalive = events; }
  });
  await f.session.enable();
  const video = new VideoTracking({ mediaID: 'media-video-0001', playerID: 'share-video-pagehide', monotonic: () => mono });
  video.setSession(f.session);
  video.setVisible(true);
  video.state('playing', 0, 60);
  mono = 2000;
  video.flush(2, 60);
  f.session.flushKeepalive();
  f.session.close();

  const progress = keepalive.find(event => event.event_type === 'video_progress');
  assert.equal(progress.video_watch_ms, 2000);
  assert.deepEqual(progress.played_ranges, [[0, 2000]]);
  assert.equal(f.session.outboxSize(), 0);
});

test('keepalive batch respects UTF-8 body budget with large ranges and non-ASCII fields', async () => {
  let release;
  let payload;
  const diagnostics = [];
  const f = sessionFixture({
    send: () => new Promise(resolve => { release = resolve; }),
    sendKeepalive: async (token, events) => { payload = { entry_token: token, events }; },
    onDiagnostic: item => diagnostics.push(item)
  });
  await f.session.enable();
  const ranges = Array.from({ length: 256 }, (_, index) => [index * 20, index * 20 + 10]);
  for (let index = 0; index < 49; index++) {
    f.session.emit('video_progress', {
      media_id: 'media-video-0001',
      player_instance_id: 'share-video-budget',
      seq: index + 1,
      current_time_ms: 6000,
      video_duration_ms: 10000,
      playback_rate: 1,
      play_state: 'playing',
      video_watch_ms: 1000,
      played_ranges: ranges,
      component_key: '视频组件甲乙丙丁戊己庚辛'
    }, index + 1);
  }
  assert.equal(f.session.outboxSize(), 50);
  assert.equal(f.session.flushKeepalive(), true);

  const bytes = new TextEncoder().encode(JSON.stringify(payload)).length;
  assert.ok(bytes <= 60 * 1024, `keepalive body used ${bytes} bytes`);
  assert.ok(payload.events.length > 0 && payload.events.length < 50);
  assert.equal(f.session.outboxSize(), 50 - payload.events.length);
  assert.deepEqual(diagnostics, [{ type: 'keepalive_truncated', sent_count: payload.events.length, unsent_count: 50 - payload.events.length, byte_limit: 60 * 1024 }]);
  f.session.close();
  release();
});

test('small real gaps remain separate played ranges', async () => {
  const f = videoFixture();
  f.video.state('playing', 0, 60);
  f.setMono(1000);
  f.video.progress(1, 60);
  f.video.seeking(1.1, 60);
  f.video.progress(1.1, 60);
  f.setMono(2000);
  f.video.progress(2, 60);
  f.video.flush(2, 60);
  await f.video.drain();

  const progress = f.sent.filter(item => item.type === 'video_progress').at(-1);
  assert.deepEqual(progress.fields.played_ranges, [[0, 1000], [1100, 2000]]);
});
