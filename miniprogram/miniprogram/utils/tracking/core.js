'use strict';
// No WeChat globals: deterministic state machine, also used by contract tests.
const TTL = 72 * 60 * 60 * 1000;
const MAX_QUEUE = 500;
const HEARTBEAT = 10000;
const SESSION_IDLE = 30 * 60 * 1000;
function createTracker(io) {
  let enabled = false, consent = '', suspended = false, foreground = false, active = null;
  let anonymousLink = null;
  let context = null, account = '', epoch = 0, observationEpoch = 0, previous = '', previousSession = '', lastActivity = null;
  let lastEntrySignature = '';
  let opening = null, busy = false, retryAt = 0, attempts = 0;
  let queue = [], dropped = { expired: 0, capacity: 0, rejected: 0, storage: 0 };
  try { queue = io.load() || []; if (!Array.isArray(queue)) queue = []; } catch (_) { dropped.storage++; }
  const pages = new Map();
  try { if (io.loadDiagnostics) dropped = Object.assign(dropped,io.loadDiagnostics()||{}); } catch (_) {}
  function persist() { try { io.save(queue); if (io.saveDiagnostics) io.saveDiagnostics(dropped); } catch (_) { dropped.storage++; } }
  function prune() {
    const before = queue.length;
    queue = queue.filter(x => x && x.event && x.credential && io.wall() - x.queued_at <= TTL && io.wall() >= x.queued_at);
    dropped.expired += before - queue.length;
    while (queue.length > MAX_QUEUE) {
      let index = queue.findIndex(x => !['page_view', 'page_end', 'identity_boundary', 'business_action_confirmed'].includes(x.event.event_type));
      queue.splice(index < 0 ? 0 : index, 1); dropped.capacity++;
    }
  }
  function enqueue(event, ctx) {
    if (!enabled || !consent || suspended || !ctx || !ctx.entry_token) return;
    queue.push({ event, credential: ctx.entry_token, queued_at: io.wall() }); prune(); persist();
  }
  function event(p, type, extra = {}) {
    if (!p || !p.context) return;
    const ctx = p.context;
    enqueue(Object.assign({ schema_version: 1, sdk_version: '1.0.0', event_id: io.id(), event_type: type,
      occurred_at: new Date(io.wall()).toISOString(), client_monotonic_ms: Math.floor(io.mono()), session_id: ctx.session_id,
      page_view_id: p.view || undefined, route_key: p.route, previous_page_view_id: p.previous || undefined,
      share_id: ctx.share_id || undefined, content_id: p.content.id || undefined,
      content_version: p.content.version || ctx.content_version || undefined, app_state: foreground ? 'foreground' : 'background',
      page_state: p.visible ? 'visible' : 'hidden' }, extra), ctx);
  }
  function sample(p) {
    if (!p || p.start == null) return;
    const now = io.mono(), raw = Math.max(0, now - p.start);
    // A suspended timer is not proof of visibility; never recover an unknown long interval.
    p.start = now;
    if (raw > 15000) { event(p, 'visible_segment', { segment_id: p.segment, seq: ++p.seq, visible_delta_ms: 0, quality_flags: ['sampling_gap'] }); return; }
    if (raw) event(p, 'visible_segment', { segment_id: p.segment, seq: ++p.seq, visible_delta_ms: Math.floor(raw) });
    lastActivity = now;
  }
  function observation(p) {
    return {
      generation: enabled && consent && !suspended && p && p.entryReady && p.view ? epoch + ':' + observationEpoch + ':' + p.view : '',
      active: !!(enabled && consent && !suspended && foreground && p && active === p && p.visible && p.ready && !p.blocked && p.start != null)
    };
  }
  function notifyObservation(p) {
    if (p && p.observers) for (const listener of p.observers) {
      try { listener(observation(p)); } catch (_) { /* Analytics cannot block playback or page lifecycle. */ }
    }
  }
  function discardPending(p) {
    p.share = ''; p.pendingEntry = null;
    // Leaving a page invalidates an unresolved request as well as future retries.
    if (p.entering) { p.request = (p.request || 0) + 1; p.entering = false; }
  }
  function stop(p, end) {
    if (!p) return;
    sample(p); p.start = null;
    if (end && p.view) { event(p, 'page_end'); previous = p.view; previousSession = p.context.session_id; p.view = ''; }
    notifyObservation(p);
  }
  function begin(p) {
    if (!enabled || !consent || suspended || !foreground || active !== p || !p.visible || !p.ready || p.blocked || !p.context || !p.entryReady) return;
    // Cached pages own their credential too; business adapters and the next entry
    // must see the same context that is currently receiving visible heartbeats.
    context = p.context;
    if (!p.view) { p.view = io.id(); p.previous = previousSession === p.context.session_id ? previous : ''; event(p, 'page_view'); }
    if (p.start == null) { p.segment = io.id(); p.seq = 0; p.start = io.mono(); }
    lastActivity = io.mono(); notifyObservation(p);
  }
  function boundary(reason) {
    stop(active, true); epoch++; anonymousLink = null; context = null; previous = ''; previousSession = ''; opening = null; lastActivity = null;
    for (const p of pages.values()) { p.view = ''; p.start = null; p.entryReady = false; p.context = null; discardPending(p); p.shareCache = {}; notifyObservation(p); }
    if (io.onBoundary) io.onBoundary(reason);
  }
  function checkAccount() {
    // Check before begin/sample can refresh activity, including warm app/page
    // recovery with an already-ready entry. Zero is a valid monotonic timestamp.
    if (lastActivity !== null && io.mono() - lastActivity > SESSION_IDLE) boundary('idle');
    const current = io.account();
    if (current !== account) { const prior=!account && current ? context : null; boundary('identity_change'); anonymousLink=prior; account = current; }
  }
  async function enter(p, share, fresh) {
    if (!enabled || !consent || suspended || !p) return;
    checkAccount();
    if (fresh) {
      boundary('external_entry');
      // The launch input is consumed only on success, not by the boundary that
      // starts its first request. Retries belong to this exact entry generation.
      p.share = share || '';
    }
    const version = epoch;
    if (!p.pendingEntry || p.pendingEntry.epoch !== epoch) p.pendingEntry = { epoch, share: p.share || '', shareRevision: p.shareRevision, previous: context || anonymousLink };
    const pending = p.pendingEntry, old = pending.previous;
    const request = (p.request || 0) + 1; p.request = request; p.entering = true;
    try {
      const response = await io.entry({ route_key: p.route, content_id: p.content.id || undefined,
        content_version: p.content.version || undefined, share_token: pending.share || undefined,
        previous_entry_token: old ? old.entry_token : undefined, consent_version: consent });
      if (request !== p.request || version !== epoch || account !== io.account() || !enabled || !consent || suspended) return;
      if (!response || !response.entry_token || !response.session_id || response.content_accessible === false) { p.entryReady = false; return; }
      // The server can split a session (owner change, idle credential, etc.)
      // even with recent local activity. Invalidate retained contexts and never
      // attach another session's predecessor to this new observation.
      if (old && old.session_id !== response.session_id) boundary('server_session');
      anonymousLink = null; if (active === p) context = response; p.context = response; p.share = ''; p.pendingEntry = null; p.entryReady = true; begin(p); flush();
      return response;
    } catch (_) {
      if (request === p.request && version === epoch) p.entryReady = false;
    } finally {
      // A late completion must not release or invalidate a newer content entry.
      if (request === p.request) p.entering = false;
    }
  }
  function resume(p) {
    if (!p || active !== p || !foreground || !p.visible || !enabled || !consent || suspended) return;
    checkAccount();
    if (!p.entryReady && p.ready && !p.entering) enter(p, p.share, !p.pendingEntry && !!p.share);
    else begin(p);
  }
  async function flush() {
    if (!enabled || !consent || suspended || busy || io.wall() < retryAt) return;
    prune(); persist(); if (!queue.length) return;
    const credential = queue[0].credential;
    const batch = [];
    for (const row of queue) {
      // Preserve A -> B -> A navigation order across credential batches: a
      // later page may reference a PV that has not yet reached the server.
      if (row.credential !== credential) break;
      if (batch.length >= 50 || JSON.stringify(batch.concat(row.event)).length > 48000) break;
      batch.push(row.event);
    }
    if (!batch.length) { queue.shift(); dropped.rejected++; persist(); return; }
    busy = true; let acknowledged = 0;
    try {
      const result = await io.send({ batch_id: io.id(), events: batch }, credential);
      const results = result.results || [];
      const gone = new Set(results.filter(x => ['accepted', 'duplicate', 'rejected'].includes(x.status)).map(x => x.event_id));
      acknowledged = gone.size;
      dropped.rejected += results.filter(x => x.status === 'rejected').length;
      queue = queue.filter(x => !gone.has(x.event.event_id)); persist(); attempts = 0; retryAt = 0;
    } catch (error) {
      if ([400, 401, 403, 404, 410, 413, 422].includes(error.statusCode)) {
        const ids = new Set(batch.map(x => x.event_id)); queue = queue.filter(x => !ids.has(x.event.event_id));
        dropped.rejected += batch.length; persist();
      } else { attempts++; retryAt = io.wall() + Math.min(300000, 1000 * 2 ** Math.min(attempts, 8)) * (0.75 + io.random() * 0.5); }
    } finally { busy = false; }
    if (acknowledged > 0 && queue.length) return flush();
  }
  return {
    configure(flag, version, options = {}) {
      checkAccount();
      const next = flag === true && !!version;
      if (next && enabled === (flag === true) && consent === (version || '')) { suspended = false; checkAccount(); resume(active); return; }
      // A changed consent generation also invalidates cached hidden pages;
      // there may be no visible page on which enter() can do that immediately.
      if (next && consent && consent !== version && !(active && foreground && active.visible)) boundary('consent_change');
      if (!next) {
        // Only a config read awaiting the first explicit choice may retain the
        // current page's unconsumed launch token. Explicit denial/withdrawal,
        // account changes, and leaving this page must discard it permanently.
        const pendingShare = options.awaitConsent === true && flag === true && !consent &&
          active && foreground && active.visible && !active.context && !active.entering ? active.share : '';
        stop(active, false); boundary('disabled'); queue = []; persist();
        if (pendingShare) active.share = pendingShare;
      }
      enabled = flag === true; consent = version || ''; suspended = false; account = io.account();
      if (next && active && foreground && active.visible) enter(active, active.share, true);
    },
    // A failed config fetch is not a server kill switch or withdrawn consent.
    // Pause collection/retries, retaining the bounded, expiring offline queue.
    suspend() { if (!suspended) { stop(active, false); suspended = true; observationEpoch++; for (const p of pages.values()) notifyObservation(p); } },
    register(key, route, content, share) {
      const p = { key, route, content: content || {}, share: share || '', visible: false, ready: false, view: '', start: null, entryReady: false, shareCache: {}, shareRevision: 0, pendingEntry: null, observers: new Set() };
      pages.set(key, p); return p;
    },
    ready(key, available, content) {
      const p = pages.get(key); if (!p) return;
      if (content && (p.content.id !== content.id || p.content.version !== content.version)) {
        stop(p, true);
        // Resolved reader options may carry a newly observed external share.
        // Preserve that newer input, but never rebind an old pending share to
        // a replacement content/version merely because its request failed.
        if (p.pendingEntry && p.shareRevision === p.pendingEntry.shareRevision) p.share = '';
        p.pendingEntry = null; p.content = content; p.entryReady = false; p.context = null; p.shareCache = {};
        p.request = (p.request || 0) + 1; p.entering = false;
      }
      p.ready = available === true;
      if (!p.ready) stop(p, false);
      else resume(p);
    },
    show(key) {
      const p = pages.get(key); if (!p) return;
      checkAccount();
      if (active && active !== p) { discardPending(active); active.visible = false; stop(active, true); }
      active = p; p.visible = true;
      resume(p);
    },
    hide(key, unload) { const p = pages.get(key); if (!p) return; stop(p, !!unload); p.visible = false; discardPending(p); if (unload) { pages.delete(key); if (active === p) active = null; } },
    appShow(options) {
      checkAccount(); foreground = true;
      // WeChat may repeat the same launch query on ordinary foreground recovery.
      // Changed externally supplied entry context is observable; identical repeated context alone is not proof of a new visit.
      const external = options && options.path && ((options.query || {}).st || (options.query || {}).scene || [1001, 1011, 1012, 1013, 1038, 1047, 1048, 1049, 1089].includes(options.scene));
      const signature = external ? [options.path,options.scene,(options.query||{}).st||'',(options.query||{}).scene||''].join('|') : '';
      const changed = !!signature && signature !== lastEntrySignature;
      if (signature) lastEntrySignature = signature;
      if (changed && active) { active.share = (options.query || {}).st || ''; enter(active, active.share, true); }
      else resume(active);
      flush();
    },
    appHide() { stop(active, false); foreground = false; if (!consent && active) discardPending(active); flush(); },
    tick() { checkAccount(); if (active && !active.entryReady && active.ready && foreground) resume(active); else sample(active); flush(); },
    block(key, blocked) { const p = pages.get(key); if (!p) return; p.blocked = !!blocked; if (blocked) stop(p, false); else resume(p); },
    emit(key, type, extra) {
      checkAccount();
      const p = pages.get(key);
      if (p && p.view && p.ready) {
        event(p, type, extra);
        if (enabled && consent && !suspended && foreground && active === p && p.visible && !p.blocked) lastActivity = io.mono();
      }
    },
    shareIntent(ctx, route, content, shareId, channel) { event({context:ctx,route,content:content||{},visible:false}, 'share_intent', {share_id:shareId,source_channel:channel}); },
    observation(key) { checkAccount(); return observation(pages.get(key)); },
    observe(key, listener) {
      const p = pages.get(key); if (!p) return () => {};
      p.observers.add(listener); listener(observation(p));
      return () => p.observers.delete(listener);
    },
    context() { return context; },
    get(key) { return pages.get(key); },
    flush,
    diagnostics() { return { queued: queue.length, dropped: { ...dropped }, enabled: enabled && !!consent && !suspended }; },
    queueForTest() { return queue.slice(); }
  };
}
module.exports = { createTracker, TTL, MAX_QUEUE, HEARTBEAT, SESSION_IDLE };
