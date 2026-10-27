(function (root, factory) {
  const api = factory();
  if (typeof module === 'object' && module.exports) module.exports = api;
  else root.JDLGShareReader = api;
})(typeof globalThis !== 'undefined' ? globalThis : this, function () {
  function secureID() {
    if (typeof crypto === 'undefined' || !crypto.getRandomValues) return null;
    const bytes = new Uint8Array(16);
    crypto.getRandomValues(bytes);
    return Array.from(bytes, byte => byte.toString(16).padStart(2, '0')).join('');
  }

  function jsonUTF8Bytes(value) {
    let bytes = 0;
    for (const character of JSON.stringify(value)) {
      const code = character.codePointAt(0);
      bytes += code <= 0x7f ? 1 : (code <= 0x7ff ? 2 : (code <= 0xffff ? 3 : 4));
    }
    return bytes;
  }

  class Tracker {
    constructor(options) {
      this.send = options.send;
      this.now = options.now || Date.now;
      this.makeID = options.makeID || secureID;
    }
    async event(type, fields) {
      const eventID = this.makeID();
      if (!eventID) return false;
      const event = Object.assign({ event_id: eventID, event_type: type, occurred_at: new Date(this.now()).toISOString() }, fields || {});
      try { await this.send(event); } catch (_) { await this.send(event); }
      return true;
    }
    async pageView(consented, visible) {
      if (!consented || !visible) return false;
      return this.event('page_view');
    }
  }

  class ReaderTracking {
    constructor(options) {
      this.enter = options.enter;
      this.monotonic = options.monotonic;
      this.now = options.now || Date.now;
      this.makeID = options.makeID || secureID;
      this.setInterval = options.setInterval;
      this.clearInterval = options.clearInterval;
      this.isVisible = options.isVisible;
      this.isOnline = options.isOnline || (() => true);
      this.send = options.send;
      this.sendKeepalive = options.sendKeepalive || ((token, events) => Promise.all(events.map(event => this.send(token, event, true))));
      this.schedule = options.setTimeout || globalThis.setTimeout.bind(globalThis);
      this.cancelSchedule = options.clearTimeout || globalThis.clearTimeout.bind(globalThis);
      this.onDiagnostic = options.onDiagnostic || (() => {});
      this.maxOutbox = options.maxOutbox || 50;
      this.entry = null;
      this.enabling = null;
      this.interval = null;
      this.closed = false;
      this.origin = 0;
      this.visibleSince = null;
      this.sequence = 0;
      this.viewID = null;
      this.pageViewEventID = null;
      this.pageViewAccepted = false;
      this.handoffToken = null;
      this.outbox = [];
      this.pumping = null;
      this.retryTimer = null;
      this.retryDelay = 1000;
      this.overflowNotified = false;
    }

    enable() {
      if (this.closed) return Promise.resolve(false);
      if (this.entry) return Promise.resolve(true);
      if (this.enabling) return this.enabling;
      const clientID = this.makeID();
      const viewID = this.makeID();
      if (!clientID || !viewID) return Promise.resolve(false);
      this.enabling = (async () => {
        const entry = await this.enter(clientID);
        if (this.closed) return false;
        this.entry = entry;
        this.viewID = viewID;
        this.origin = this.monotonic();
        this.visibleSince = this.isVisible() ? this.origin : null;
        if (!this.emit('page_view', {}, this.origin)) {
          this.close();
          return false;
        }
        if (this.closed) return false;
        this.interval = this.setInterval(() => { this.tick().catch(() => {}); }, 10000);
        return true;
      })().finally(() => { this.enabling = null; });
      return this.enabling;
    }

    emit(type, fields, at) {
      if (this.closed || !this.entry) return false;
      const eventID = this.makeID();
      if (!eventID) return false;
      const event = Object.assign({
        event_id: eventID,
        event_type: type,
        occurred_at: new Date(this.now()).toISOString(),
        schema_version: 1,
        sdk_version: 'share-web-v1',
        session_id: this.entry.session_id,
        page_view_id: this.viewID,
        route_key: this.entry.route_key,
        content_id: this.entry.content_id || '',
        content_version: this.entry.content_version || 0,
        share_id: this.entry.share_id || '',
        client_monotonic_ms: Math.max(0, Math.round((at === undefined ? this.monotonic() : at) - this.origin)),
        app_state: 'foreground',
        page_state: 'visible',
        quality_flags: []
      }, fields);
      if (this.outbox.length >= this.maxOutbox) {
        if (!this.overflowNotified) {
          this.overflowNotified = true;
          try { this.onDiagnostic({ type: 'outbox_overflow', limit: this.maxOutbox, dropped_event_id: eventID }); } catch (_) {}
        }
        return false;
      }
      this.outbox.push(event);
      if (type === 'page_view') this.pageViewEventID = eventID;
      this.pump();
      return true;
    }

    pump() {
      if (this.closed || !this.entry || !this.outbox.length || !this.isOnline() || this.retryTimer !== null) return Promise.resolve();
      if (this.pumping) return this.pumping;
      const token = this.entry.entry_token;
      this.pumping = (async () => {
        while (!this.closed && this.outbox.length && this.isOnline()) {
          const event = this.outbox[0];
          try {
            const response = await this.send(token, event);
            if (event.event_id === this.pageViewEventID && response && Array.isArray(response.results) && response.results.some(result => result.event_id === event.event_id && ['accepted', 'duplicate'].includes(result.status))) this.pageViewAccepted = true;
          } catch (_) {
            this.scheduleRetry();
            return;
          }
          if (this.outbox[0] === event) this.outbox.shift();
          this.retryDelay = 1000;
          this.overflowNotified = false;
        }
      })().finally(() => {
        this.pumping = null;
        if (!this.closed && this.retryTimer === null && this.outbox.length && this.isOnline()) this.pump();
      });
      return this.pumping;
    }

    scheduleRetry() {
      if (this.closed || this.retryTimer !== null || !this.outbox.length) return;
      const delay = this.retryDelay;
      this.retryDelay = Math.min(30000, delay * 2);
      this.retryTimer = this.schedule(() => {
        this.retryTimer = null;
        this.pump();
      }, delay);
    }

    online() {
      if (this.closed) return;
      if (this.retryTimer !== null) this.cancelSchedule(this.retryTimer);
      this.retryTimer = null;
      this.retryDelay = 1000;
      this.pump();
    }

    drain() {
      return this.pumping || Promise.resolve();
    }

    async handoffProof() {
      if (this.closed || !this.entry) return {};
      let timer;
      try {
        await Promise.race([this.drain(), new Promise(resolve => { timer = this.schedule(resolve, 1000); })]);
      } finally { if (timer !== undefined) this.cancelSchedule(timer); }
      if (this.closed || !this.entry || !this.pageViewAccepted || !this.pageViewEventID) return {};
      if (!this.handoffToken) this.handoffToken = this.makeID();
      if (!this.handoffToken) return {};
      return { source_entry_token: this.entry.entry_token, source_page_view_event_id: this.pageViewEventID, handoff_token: this.handoffToken };
    }

    outboxSize() {
      return this.outbox.length;
    }

    flushKeepalive() {
      if (this.closed || !this.entry || !this.outbox.length) return false;
      const token = this.entry.entry_token;
      const byteLimit = 60 * 1024;
      let count = 0;
      let bytes = jsonUTF8Bytes({ entry_token: token, events: [] });
      for (const event of this.outbox) {
        const nextBytes = bytes + jsonUTF8Bytes(event) + (count ? 1 : 0);
        if (nextBytes > byteLimit) break;
        bytes = nextBytes;
        count++;
      }
      if (!count) {
        try { this.onDiagnostic({ type: 'keepalive_oversize', unsent_count: this.outbox.length, byte_limit: byteLimit }); } catch (_) {}
        return false;
      }
      const events = this.outbox.slice(0, count);
      let operation;
      try {
        operation = this.sendKeepalive(token, events);
      } catch (_) {
        try { this.onDiagnostic({ type: 'keepalive_failed', event_count: events.length }); } catch (_) {}
        return false;
      }
      this.outbox.splice(0, count);
      if (this.retryTimer !== null) this.cancelSchedule(this.retryTimer);
      this.retryTimer = null;
      this.overflowNotified = false;
      if (this.outbox.length) {
        try { this.onDiagnostic({ type: 'keepalive_truncated', sent_count: count, unsent_count: this.outbox.length, byte_limit: byteLimit }); } catch (_) {}
      }
      Promise.resolve(operation).catch(() => {
        try { this.onDiagnostic({ type: 'keepalive_failed', event_count: events.length }); } catch (_) {}
      });
      return true;
    }

    async flush(at, continueVisible) {
      if (this.closed || !this.entry || this.visibleSince === null) return;
      const delta = Math.min(15000, Math.max(0, Math.round(at - this.visibleSince)));
      this.visibleSince = continueVisible ? at : null;
      if (!delta) return;
      const segmentID = this.makeID();
      if (!segmentID) {
        this.close();
        return;
      }
      this.sequence++;
      this.emit('visible_segment', { segment_id: segmentID, seq: this.sequence, visible_delta_ms: delta }, at);
    }

    async tick() {
      if (this.closed || !this.entry) return;
      const at = this.monotonic();
      if (!this.isVisible()) {
        await this.flush(at, false);
        return;
      }
      if (this.visibleSince === null) {
        this.visibleSince = at;
        return;
      }
      await this.flush(at, true);
    }

    async visibilityChanged(visible) {
      if (this.closed || !this.entry) return;
      const at = this.monotonic();
      if (!visible) {
        await this.flush(at, false);
      } else {
        this.visibleSince = at;
      }
    }

    close() {
      this.closed = true;
      if (this.interval !== null) this.clearInterval(this.interval);
      if (this.retryTimer !== null) this.cancelSchedule(this.retryTimer);
      this.interval = null;
      this.retryTimer = null;
      this.entry = null;
      this.pageViewAccepted = false;
      this.handoffToken = null;
      this.visibleSince = null;
      this.outbox.length = 0;
    }
  }

  class VideoTracking {
    constructor(options) {
      this.mediaID = options.mediaID;
      this.playerID = options.playerID;
      this.monotonic = options.monotonic;
      this.reportEveryMS = options.reportEveryMS || 5000;
      this.session = null;
      this.visible = false;
      this.reset();
    }

    reset() {
      this.stateValue = 'paused';
      this.rateValue = 1;
      this.position = 0;
      this.duration = 0;
      this.lastSample = null;
      this.watch = 0;
      this.ranges = [];
      this.sequence = 0;
      this.pendingSeek = false;
      this.pendingState = false;
    }

    setSession(session) {
      if (this.session === session) return;
      this.session = session || null;
      this.reset();
    }

    setVisible(visible) {
      this.visible = !!visible;
      this.lastSample = null;
    }

    rate(value) {
      const next = Number(value);
      if (Number.isFinite(next) && next >= 0 && next <= 4) this.rateValue = next;
    }

    normalize(seconds, durationSeconds) {
      const duration = Math.max(0, Math.round(Number(durationSeconds) * 1000) || 0);
      const rawPosition = Math.max(0, Math.round(Number(seconds) * 1000) || 0);
      return { position: duration > 0 ? Math.min(rawPosition, duration) : rawPosition, duration };
    }

    mergeRange(start, end) {
      if (end <= start) return;
      const sorted = this.ranges.concat([[start, end]]).sort((a, b) => a[0] - b[0]);
      const merged = [];
      sorted.forEach(range => {
        const last = merged[merged.length - 1];
        if (last && range[0] <= last[1]) last[1] = Math.max(last[1], range[1]);
        else if (merged.length < 256) merged.push(range.slice());
      });
      this.ranges = merged;
    }

    uniquePlayed() {
      return this.ranges.reduce((total, range) => total + range[1] - range[0], 0);
    }

    fields(type, flags) {
      return {
        media_id: this.mediaID,
        player_instance_id: this.playerID,
        seq: ++this.sequence,
        current_time_ms: this.position,
        video_duration_ms: this.duration,
        playback_rate: this.rateValue,
        play_state: this.stateValue,
        video_watch_ms: type === 'video_progress' ? Math.floor(this.watch) : 0,
        unique_played_ms: this.uniquePlayed(),
        played_ranges: this.ranges.map(range => range.slice()),
        quality_flags: flags || []
      };
    }

    send(type, flags) {
      if (!this.session || !this.visible || this.duration <= 0) return false;
      const at = this.monotonic();
      const accepted = this.session.emit(type, this.fields(type, flags), at) !== false;
      if (accepted && type === 'video_progress') this.watch = 0;
      return accepted;
    }

    sync(seconds, durationSeconds, force) {
      const at = this.monotonic();
      const next = this.normalize(seconds, durationSeconds);
      if (next.duration > 0) this.duration = next.duration;
      const delta = this.lastSample === null ? 0 : at - this.lastSample;
      const advance = next.position - this.position;
      const playing = !!this.session && this.visible && this.stateValue === 'playing' && delta > 0 && delta <= 2000;
      const inferredSeek = this.pendingSeek || (playing && (advance < -100 || advance > delta * this.rateValue + 1000));
      if (playing && !inferredSeek && advance > 0) {
        this.watch = Math.min(15000, this.watch + delta);
        this.mergeRange(this.position, Math.min(next.position, this.duration || next.position));
      }
      this.position = next.position;
      this.lastSample = this.session && this.visible ? at : null;
      this.pendingSeek = false;
      if (this.pendingState && this.send('video_state')) this.pendingState = false;
      if (inferredSeek) this.send('video_progress', ['inferred_seek']);
      else if ((force && this.watch > 0) || this.watch >= this.reportEveryMS) this.send('video_progress');
    }

    progress(seconds, durationSeconds) {
      this.sync(seconds, durationSeconds, false);
    }

    flush(seconds, durationSeconds) {
      this.sync(seconds, durationSeconds, true);
    }

    seeking(seconds, durationSeconds) {
      const next = this.normalize(seconds, durationSeconds);
      if (next.duration > 0) this.duration = next.duration;
      this.position = next.position;
      this.lastSample = null;
      this.pendingSeek = true;
    }

    state(value, seconds, durationSeconds) {
      const nextState = value === 'playing' ? 'playing' : (value === 'ended' ? 'ended' : 'paused');
      if (this.stateValue === nextState) {
        if (nextState === 'playing') this.sync(seconds, durationSeconds, false);
        else {
          const next = this.normalize(seconds, durationSeconds);
          if (next.duration > 0) this.duration = next.duration;
          this.position = next.position;
        }
        return;
      }
      if (this.stateValue === 'playing' && nextState !== 'playing') {
        this.sync(seconds, durationSeconds, true);
        // Overflow is already diagnosed by the shared outbox. Do not carry
        // playing time into a later paused event that the server must reject.
        if (this.watch > 0) this.watch = 0;
      }
      else {
        const next = this.normalize(seconds, durationSeconds);
        if (next.duration > 0) this.duration = next.duration;
        this.position = next.position;
      }
      this.stateValue = nextState;
      this.lastSample = nextState === 'playing' && this.session && this.visible ? this.monotonic() : null;
      this.pendingState = !this.send('video_state') && !!this.session && this.visible && this.duration <= 0;
    }

    drain() {
      return this.session && this.session.drain ? this.session.drain() : Promise.resolve();
    }
  }

  const videoBindings = new WeakMap();
  let nextVideoPlayer = 0;

  function bindVideos(elements, options) {
    const settings = options || {};
    const monotonic = settings.monotonic || (() => performance.now());
    const isVisible = settings.isVisible || (() => true);
    const bindings = [];
    Array.from(elements || []).forEach(video => {
      const mediaID = video.dataset && video.dataset.mediaId;
      if (!mediaID) return;
      let binding = videoBindings.get(video);
      if (!binding) {
        const tracker = new VideoTracking({ mediaID, playerID: `share-video-${++nextVideoPlayer}`, monotonic });
        const values = () => [video.currentTime || 0, Number.isFinite(video.duration) ? video.duration : 0];
        video.addEventListener('play', () => tracker.state('playing', ...values()));
        video.addEventListener('pause', () => tracker.state(video.ended ? 'ended' : 'paused', ...values()));
        video.addEventListener('ended', () => tracker.state('ended', ...values()));
        video.addEventListener('ratechange', () => tracker.rate(video.playbackRate));
        video.addEventListener('timeupdate', () => tracker.progress(...values()));
        video.addEventListener('loadedmetadata', () => tracker.progress(...values()));
        video.addEventListener('seeking', () => tracker.seeking(...values()));
        video.addEventListener('seeked', () => tracker.progress(...values()));
        binding = { video, tracker };
        videoBindings.set(video, binding);
      }
      bindings.push(binding);
    });
    return {
      setSession(session) {
        bindings.forEach(({ video, tracker }) => {
          tracker.setSession(session);
          tracker.setVisible(isVisible());
          tracker.rate(video.playbackRate);
          tracker.progress(video.currentTime || 0, Number.isFinite(video.duration) ? video.duration : 0);
          if (session && video.paused === false) tracker.state('playing', video.currentTime || 0, video.duration);
        });
      },
      flush() { bindings.forEach(({ video, tracker }) => tracker.flush(video.currentTime || 0, Number.isFinite(video.duration) ? video.duration : 0)); },
      setVisible(visible) { bindings.forEach(({ tracker }) => tracker.setVisible(visible)); }
    };
  }

  function createPost(fetchFn, options) {
    const settings = options || {};
    const timeoutMS = settings.timeoutMS || 10000;
    const schedule = settings.setTimeout || globalThis.setTimeout.bind(globalThis);
    const cancel = settings.clearTimeout || globalThis.clearTimeout.bind(globalThis);
    const AbortType = Object.prototype.hasOwnProperty.call(settings, 'AbortController')
      ? settings.AbortController
      : (typeof AbortController === 'undefined' ? null : AbortController);
    return async function post(path, payload, requestOptions) {
      const request = requestOptions || {};
      const controller = !request.keepalive && AbortType ? new AbortType() : null;
      let timer;
      const timeout = new Promise((_, reject) => {
        timer = schedule(() => {
          if (controller) controller.abort();
          reject(new Error('request timeout'));
        }, timeoutMS);
      });
      try {
        const operation = (async () => {
          const response = await fetchFn(path, {
            method: 'POST',
            credentials: 'same-origin',
            headers: { 'Content-Type': 'application/json', 'X-Share-Web': '1' },
            body: JSON.stringify(payload),
            ...(request.keepalive ? { keepalive: true } : {}),
            ...(controller ? { signal: controller.signal } : {})
          });
          if (!response.ok) throw new Error('request failed');
          const value = await response.json();
          return value.data;
        })();
        return await Promise.race([operation, timeout]);
      } finally {
        cancel(timer);
      }
    };
  }

  async function boot() {
    if (typeof document === 'undefined') return;
    const body = document.body, code = body.dataset.code;
    const consent = document.getElementById('consent'), state = document.getElementById('tracking-state');
    const post = createPost(fetch);
    let tracking = null;
    const videos = bindVideos(document.querySelectorAll('video[data-media-id]'), {
      monotonic: () => performance.now(),
      isVisible: () => document.visibilityState === 'visible'
    });
    const buildTracking = () => new ReaderTracking({
      enter: client => post('/api/v1/share-web/entry', { code, client_instance: client, consent_version: 'v1' }),
      send: (token, event) => post('/api/v1/share-web/events', { entry_token: token, events: [event] }),
      sendKeepalive: (token, events) => post('/api/v1/share-web/events', { entry_token: token, events }, { keepalive: true }),
      monotonic: () => performance.now(),
      isVisible: () => document.visibilityState === 'visible',
      isOnline: () => navigator.onLine !== false,
      setInterval: (fn, ms) => window.setInterval(fn, ms),
      clearInterval: value => window.clearInterval(value),
      setTimeout: (fn, ms) => window.setTimeout(fn, ms),
      clearTimeout: value => window.clearTimeout(value),
      onDiagnostic: diagnostic => {
        if (diagnostic.type === 'outbox_overflow') state.textContent = ' 网络拥堵，部分记录未保存';
      }
    });
    if (consent) consent.addEventListener('click', async () => {
      if (tracking && tracking.entry) {
        videos.setSession(null);
        tracking.close();
        tracking = null;
        consent.textContent = '重新开启阅读记录';
        state.textContent = ' 已关闭阅读记录';
        return;
      }
      consent.disabled = true;
      tracking = buildTracking();
      try {
        if (!(await tracking.enable())) {
          tracking.close();
          tracking = null;
          state.textContent = ' 当前浏览器不支持安全统计';
          return;
        }
        consent.textContent = '关闭阅读记录';
        state.textContent = ' 已开启阅读记录';
        videos.setSession(tracking);
      } catch (_) {
        videos.setSession(null);
        if (tracking) tracking.close();
        tracking = null;
        state.textContent = ' 阅读记录暂不可用';
      } finally {
        consent.disabled = false;
      }
    });
    const onVisibility = () => {
      const visible = document.visibilityState === 'visible';
      if (!visible) videos.flush();
      if (tracking) tracking.visibilityChanged(visible).catch(() => {});
      videos.setVisible(visible);
    };
    let cleaned = false;
    const cleanup = () => {
      if (cleaned) return;
      cleaned = true;
      videos.flush();
      if (tracking) {
        tracking.visibilityChanged(false).catch(() => {});
        tracking.flushKeepalive();
        tracking.close();
      }
      videos.setSession(null);
      tracking = null;
    };
    document.addEventListener('visibilitychange', onVisibility);
    window.addEventListener('online', () => { if (tracking) tracking.online(); });
    window.addEventListener('pagehide', cleanup, { once: true });
    window.addEventListener('unload', cleanup, { once: true });
    const open = document.getElementById('open-mini');
    if (open) open.addEventListener('click', async () => {
      if (open.disabled) return;
      open.disabled = true;
      const source = tracking;
      try {
        const proof = source ? await source.handoffProof() : {};
        const out = await post('/api/v1/share-web/open-miniprogram', { code, ...(source === tracking && source && !source.closed ? proof : {}) });
        location.href = out.url_link;
      }
      catch (_) { open.textContent = '暂时无法打开小程序'; }
      finally { open.disabled = false; }
    });
  }
  if (typeof window !== 'undefined') window.addEventListener('DOMContentLoaded', boot);
  return { Tracker, ReaderTracking, VideoTracking, bindVideos, secureID, createPost, boot };
});
