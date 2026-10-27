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
      this.send = options.send;
      this.entry = null;
      this.enabling = null;
      this.interval = null;
      this.closed = false;
      this.origin = 0;
      this.visibleSince = null;
      this.sequence = 0;
      this.viewID = null;
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
        if (!(await this.emit('page_view', {}, this.origin))) {
          this.close();
          return false;
        }
        if (this.closed) return false;
        this.interval = this.setInterval(() => { this.tick().catch(() => {}); }, 10000);
        return true;
      })().finally(() => { this.enabling = null; });
      return this.enabling;
    }

    async emit(type, fields, at) {
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
        client_monotonic_ms: Math.max(0, Math.round(at - this.origin)),
        app_state: 'foreground',
        page_state: 'visible',
        quality_flags: []
      }, fields);
      const token = this.entry.entry_token;
      try { await this.send(token, event); } catch (error) {
        if (this.closed) throw error;
        await this.send(token, event);
      }
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
      if (!(await this.emit('visible_segment', { segment_id: segmentID, seq: this.sequence, visible_delta_ms: delta }, at))) this.close();
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
      this.interval = null;
      this.entry = null;
      this.visibleSince = null;
    }
  }

  function createPost(fetchFn, options) {
    const settings = options || {};
    const timeoutMS = settings.timeoutMS || 10000;
    const schedule = settings.setTimeout || globalThis.setTimeout.bind(globalThis);
    const cancel = settings.clearTimeout || globalThis.clearTimeout.bind(globalThis);
    const AbortType = Object.prototype.hasOwnProperty.call(settings, 'AbortController')
      ? settings.AbortController
      : (typeof AbortController === 'undefined' ? null : AbortController);
    return async function post(path, payload) {
      const controller = AbortType ? new AbortType() : null;
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
    const buildTracking = () => new ReaderTracking({
      enter: client => post('/api/v1/share-web/entry', { code, client_instance: client, consent_version: 'v1' }),
      send: (token, event) => post('/api/v1/share-web/events', { entry_token: token, events: [event] }),
      monotonic: () => performance.now(),
      isVisible: () => document.visibilityState === 'visible',
      setInterval: (fn, ms) => window.setInterval(fn, ms),
      clearInterval: value => window.clearInterval(value)
    });
    if (consent) consent.addEventListener('click', async () => {
      if (tracking && tracking.entry) {
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
      } catch (_) {
        if (tracking) tracking.close();
        tracking = null;
        state.textContent = ' 阅读记录暂不可用';
      } finally {
        consent.disabled = false;
      }
    });
    const onVisibility = () => { if (tracking) tracking.visibilityChanged(document.visibilityState === 'visible').catch(() => {}); };
    const cleanup = () => { if (tracking) tracking.close(); tracking = null; };
    document.addEventListener('visibilitychange', onVisibility);
    window.addEventListener('pagehide', cleanup, { once: true });
    window.addEventListener('unload', cleanup, { once: true });
    const open = document.getElementById('open-mini');
    if (open) open.addEventListener('click', async () => {
      try { const out = await post('/api/v1/share-web/open-miniprogram', { code }); location.href = out.url_link; }
      catch (_) { open.textContent = '暂时无法打开小程序'; }
    });
  }
  if (typeof window !== 'undefined') window.addEventListener('DOMContentLoaded', boot);
  return { Tracker, ReaderTracking, secureID, createPost, boot };
});
