// Deterministic tests of the production component with controlled hooks and promises.
// No browser, network, real refunds, or backend writes. A source path can be passed
// to verify the same regressions against a saved baseline.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import vm from 'node:vm';
import test from 'node:test';
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';

const require = createRequire(import.meta.url);
const React = require('react');
const ts = require('typescript');
const root = fileURLToPath(new URL('..', import.meta.url));
const sourcePath = process.argv[2] || path.join(root, 'src/pages/orderCenter.tsx');
const source = fs.readFileSync(sourcePath, 'utf8');
const javascript = ts.transpileModule(source, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, jsx: ts.JsxEmit.React, target: ts.ScriptTarget.ES2020, esModuleInterop: true },
}).outputText;
const flush = () => new Promise(resolve => setImmediate(resolve));
const orders = [101, 202].map(id => ({
  id, user_id: id, username: `fixture-user-${id}`, order_no: `fixture-order-${id}`,
  type: 'recharge', order_category: 'recharge', status: 'success',
  pay_channel: 'virtualpay', pay_mode: 'short_series_coin',
  amount: 50, amount_fen: 500, wechat_coin_mirror: 80,
}));
const preview = (id, sufficient = true) => ({
  order_id: id, order_no: `fixture-order-${id}`, user_id: id,
  stones_to_deduct: 50, current_stones: 80, amount_fen: 500, sufficient,
  membership: { plan_title: 'fixture membership', recover_detail: 'fixture recovery plan' },
});
const detail = id => ({
  order: orders.find(order => order.id === id),
  fund_view: { wechat_mirror: 80, local_stones: 60, consumed_net: 20 },
  wechat_evidence: { sett_state: 2, platform_fee_fen: 30, amount_match: 'match' },
});
function deferred() {
  let resolve, reject;
  const promise = new Promise((res, rej) => { resolve = res; reject = rej; });
  return { promise, resolve, reject };
}
function nodes(tree, predicate) {
  const found = [];
  const walk = node => {
    if (Array.isArray(node)) return node.forEach(walk);
    if (!node || typeof node !== 'object') return;
    if (predicate(node)) found.push(node);
    walk(node.props?.children);
  };
  walk(tree);
  return found;
}
function textOf(node) {
  if (node == null || typeof node === 'boolean') return '';
  if (Array.isArray(node)) return node.map(textOf).join('');
  return typeof node === 'object' ? textOf(node.props?.children) : String(node);
}
function byClass(tree, name) {
  return nodes(tree, node => String(node.props?.className || '').split(/\s+/).includes(name));
}
function button(tree, label) {
  const found = nodes(tree, node => node.type === 'button' && textOf(node) === label);
  assert.equal(found.length, 1, `Expected one ${label} button`);
  return found[0];
}
function modal(page) { return byClass(page.tree, 'refund-preview-modal')[0]; }
function row(page, id) { return nodes(page.tree, node => node.type === 'tr' && node.key === String(id))[0]; }
function click(page, node) {
  assert.ok(node, 'Click target exists');
  assert.notEqual(node.props.disabled, true, 'Click target is enabled');
  node.props.onClick();
  page.commit();
}
function open(page, id) { click(page, button(row(page, id), '退款')); }
function dismiss(page, method) {
  const dialog = modal(page);
  const target = method === 'backdrop' ? byClass(page.tree, 'modal-overlay')[0]
    : method === 'close' ? byClass(dialog, 'modal-close')[0]
      : button(dialog, '取消');
  click(page, target);
}
function mount({ deferDetails = false } = {}) {
  const page = { slots: [], cursor: 0, pending: [], tree: null, dirty: false, mounted: true, afterUnmountWrites: 0 };
  const requests = [];
  const detailRequests = [];
  const refunds = [];
  const alerts = [];
  const prompts = [];
  const refundResult = deferred();
  const refundResults = [refundResult];
  let promptResult = null;
  let searchParams = new URLSearchParams();
  const stateChanged = () => {
    if (!page.mounted) page.afterUnmountWrites += 1;
    page.dirty = true;
  };
  const react = {
    ...React,
    useState(initial) {
      const index = page.cursor++;
      if (!(index in page.slots)) page.slots[index] = typeof initial === 'function' ? initial() : initial;
      return [page.slots[index], value => {
        const next = typeof value === 'function' ? value(page.slots[index]) : value;
        if (!Object.is(next, page.slots[index])) { page.slots[index] = next; stateChanged(); }
      }];
    },
    useRef(initial) {
      const index = page.cursor++;
      if (!(index in page.slots)) page.slots[index] = { current: initial };
      return page.slots[index];
    },
    useEffect(effect, deps) {
      const index = page.cursor++;
      const previous = page.slots[index];
      if (!previous || deps.some((value, i) => !Object.is(value, previous.deps[i]))) {
        page.slots[index] = { deps, cleanup: previous?.cleanup };
        page.pending.push(() => {
          page.slots[index].cleanup?.();
          page.slots[index].cleanup = effect();
        });
      }
    },
  };
  react.default = react;
  react.__esModule = true;
  const api = {
    getOrderList: async () => ({ list: orders, summary: { total_count: 2 }, total: 2, page: 1 }),
    getOrderFundStats: async () => ({ wechat_mirror_total: 160, local_stones_total: 120 }),
    getOrderDetail: id => {
      if (!deferDetails) return Promise.resolve(detail(Number(id)));
      const request = { id, ...deferred() };
      detailRequests.push(request);
      return request.promise;
    },
    getRefundPreview: id => {
      const request = { id, ...deferred() };
      requests.push(request);
      return request.promise;
    },
    refundVirtualPayOrder: (...args) => {
      const result = refundResults[refunds.length] || deferred();
      refundResults[refunds.length] = result;
      refunds.push(args);
      return result.promise;
    },
  };
  const module = { exports: {} };
  const context = vm.createContext({
    module, exports: module.exports, URLSearchParams,
    console: { ...console, error() {} },
    alert: message => alerts.push(message),
    window: { prompt: (...args) => { prompts.push(args); return promptResult; } },
    require(name) {
      if (name === 'react') return react;
      if (name === 'react-router-dom') return {
        useNavigate: () => () => {},
        useSearchParams: () => [searchParams, next => { searchParams = next; stateChanged(); }],
      };
      if (name === 'react-icons/fi') return new Proxy({}, { get: (_, key) => key });
      if (name === '../api/orders') return api;
      if (name === '../api/virtualPay') return { getXPayLogs: async () => ({ list: [] }) };
      if (name === '../utils/pageLoadError') return { reportPageLoadError() {} };
      if (name === '../config/api') return { API_BASE_URL: '/fixture-only' };
      if (name.endsWith('.scss')) return {};
      throw new Error(`Unexpected import: ${name}`);
    },
  });
  vm.runInContext(javascript, context, { filename: sourcePath });
  page.commit = () => {
    let cycles = 0;
    do {
      assert.ok(cycles++ < 20, 'Component settles without an effect loop');
      page.dirty = false;
      page.cursor = 0;
      page.tree = module.exports.default();
      page.pending.splice(0).forEach(effect => effect());
    } while (page.dirty);
  };
  page.unmount = () => {
    page.slots.forEach(slot => { if (typeof slot?.cleanup === 'function') slot.cleanup(); });
    page.mounted = false;
  };
  page.setSearchParams = value => { searchParams = new URLSearchParams(value); page.commit(); };
  page.setPrompt = value => { promptResult = value; };
  Object.assign(page, { requests, detailRequests, refunds, alerts, prompts, refundResult, refundResults });
  page.commit();
  return page;
}
async function boot(options) {
  const page = mount(options);
  await flush(); page.commit();
  return page;
}
async function settle(page, request, value, reject = false) {
  request[reject ? 'reject' : 'resolve'](value);
  await flush(); page.commit();
}
function assertReady(page, id) {
  assert.ok(modal(page));
  assert.match(textOf(modal(page)), new RegExp(`退款确认 · fixture-order-${id}`));
  assert.match(textOf(modal(page)), /扣回灵石：50/);
  assert.equal(button(modal(page), '确认发起退款').props.disabled, false);
}

test('all preview dismissal controls close immediately and ignore late success or failure without a refund', async () => {
  for (const method of ['cancel', 'close', 'backdrop']) {
    for (const reject of [false, true]) {
      const page = await boot();
      open(page, 101);
      assert.match(textOf(modal(page)), /正在获取退款影响清单/);
      dismiss(page, method);
      assert.equal(modal(page), undefined, `${method} dismisses during loading`);
      await settle(page, page.requests[0], reject ? new Error('late failure') : preview(101), reject);
      assert.equal(modal(page), undefined, `${method} cannot be resurrected by a late response`);
      assert.equal(page.alerts.length, 0);
      assert.equal(page.refunds.length, 0);
    }
  }
});

test('loaded preview cancellation is read-only, and reopening starts a fresh preview', async () => {
  const page = await boot();
  open(page, 101);
  await settle(page, page.requests[0], preview(101));
  assertReady(page, 101);
  dismiss(page, 'cancel');
  assert.equal(modal(page), undefined);
  assert.equal(page.refunds.length, 0);
  open(page, 101);
  assert.equal(page.requests.length, 2);
  assert.equal(button(modal(page), '确认发起退款').props.disabled, true);
  assert.doesNotMatch(textOf(modal(page)), /扣回灵石/);
  await settle(page, page.requests[1], preview(101));
  assertReady(page, 101);
});

test('latest A/B preview wins; stale finally cannot clear the newer loading state', async () => {
  for (const reject of [false, true]) {
    const page = await boot();
    open(page, 101); open(page, 202);
    await settle(page, page.requests[0], reject ? new Error('stale A') : preview(101), reject);
    assert.match(textOf(modal(page)), /正在获取退款影响清单/);
    assert.match(textOf(modal(page)), /退款确认 · fixture-order-202/);
    assert.equal(button(modal(page), '确认发起退款').props.disabled, true);
    await settle(page, page.requests[1], preview(202));
    assertReady(page, 202);
    assert.equal(page.alerts.length, 0);
    assert.equal(page.refunds.length, 0);
  }
  for (const reject of [false, true]) {
    const page = await boot();
    open(page, 101); open(page, 202);
    await settle(page, page.requests[1], preview(202));
    assertReady(page, 202);
    await settle(page, page.requests[0], reject ? new Error('stale A') : preview(101), reject);
    assertReady(page, 202);
    assert.equal(page.alerts.length, 0);
  }
});

test('current preview failure is local, non-actionable, retryable for the same order, and dismissible', async () => {
  const page = await boot();
  open(page, 202);
  await settle(page, page.requests[0], new Error('fixture preview unavailable'), true);
  assert.match(textOf(modal(page)), /获取退款影响预览失败：fixture preview unavailable/);
  assert.equal(nodes(modal(page), node => node.props?.role === 'alert').length, 1);
  assert.equal(button(modal(page), '确认发起退款').props.disabled, true);
  assert.equal(page.alerts.length, 0);
  click(page, button(modal(page), '重新加载'));
  assert.equal(page.requests[1].id, '202');
  assert.doesNotMatch(textOf(modal(page)), /fixture preview unavailable/);
  await settle(page, page.requests[1], preview(202));
  assertReady(page, 202);
  dismiss(page, 'close');
  assert.equal(modal(page), undefined);
  assert.equal(page.refunds.length, 0);
});

test('changing or closing order detail invalidates pending preview responses', async () => {
  for (const transition of ['open detail', 'close detail', 'URL detail']) {
    const page = await boot();
    if (transition === 'close detail') {
      click(page, byClass(row(page, 101), 'btn-view')[0]);
      await flush(); page.commit();
    }
    open(page, 101);
    if (transition === 'open detail') click(page, byClass(row(page, 202), 'btn-view')[0]);
    if (transition === 'close detail') click(page, byClass(page.tree, 'drawer-close')[0]);
    if (transition === 'URL detail') page.setSearchParams('orderId=202');
    assert.equal(modal(page), undefined, `${transition} dismisses old context immediately`);
    await settle(page, page.requests[0], preview(101));
    assert.equal(modal(page), undefined, `${transition} ignores the old response`);
    assert.equal(page.refunds.length, 0);
  }
});

test('unmount prevents pending preview success, failure, and finally from writing state', async () => {
  for (const reject of [false, true]) {
    const page = await boot();
    open(page, 101); page.unmount();
    page.requests[0][reject ? 'reject' : 'resolve'](reject ? new Error('after unmount') : preview(101));
    await flush();
    assert.equal(page.afterUnmountWrites, 0);
    assert.equal(page.alerts.length, 0);
    assert.equal(page.refunds.length, 0);
  }
});

test('existing balance eligibility, reason prompt, membership impact, and submission lock remain intact', async () => {
  const page = await boot();
  open(page, 101);
  await settle(page, page.requests[0], preview(101, false));
  assert.equal(button(modal(page), '确认发起退款').props.disabled, true);
  assert.match(textOf(modal(page)), /余额不足，无法发起/);
  dismiss(page, 'cancel');
  open(page, 202);
  await settle(page, page.requests[1], preview(202));
  assert.match(textOf(modal(page)), /fixture membership（fixture recovery plan）/);
  assert.match(textOf(modal(page)), /先扣回灵石 → 发起微信退款 → 微信确认后订单落"已退款"并自动回收会员/);
  click(page, button(modal(page), '确认发起退款'));
  assert.equal(page.refunds.length, 0, 'Canceling the existing reason prompt sends nothing');
  assert.deepEqual(page.prompts[0], ['请填写退款原因（记入审计日志）', '运营后台退款']);
  page.setPrompt('fixture operator reason');
  click(page, button(modal(page), '确认发起退款'));
  assert.deepEqual(page.refunds, [['202', 'fixture operator reason']]);
  assert.equal(button(modal(page), '退款提交中...').props.disabled, true);
  assert.equal(button(modal(page), '取消').props.disabled, true);
  assert.equal(byClass(modal(page), 'modal-close')[0].props.disabled, true);
  // Even direct invocation of backdrop/cancel handlers retains the existing lock.
  byClass(page.tree, 'modal-overlay')[0].props.onClick();
  button(modal(page), '取消').props.onClick(); page.commit();
  assert.ok(modal(page));
  await settle(page, page.refundResult, { order_no: 'fixture-order-202', refund_amount: 50, old_stones: 80, new_stones: 30 });
  assert.equal(modal(page), undefined);
  assert.equal(page.refunds.length, 1);
});

test('financial explanations are native touch/keyboard disclosures with original formulas and fee context', async () => {
  const page = await boot();
  click(page, byClass(row(page, 101), 'btn-view')[0]);
  await flush(); page.commit();
  const explanations = [
    '全站用户的微信虚拟支付代币余额合计 = 累计成功充值 − 已退款（本地镜像口径）',
    '已本地消费 = 微信侧在账 − 本地可用，为净差额：含邀请/签到/手工调整等非虚拟支付来源灵石的净影响',
    '微信侧实际扣款金额（query_order 回读取证），与「支付金额」逐单对平',
    '该用户当前在微信虚拟支付账本中的代币余额（本地镜像 = 累计成功充值 − 已退款）',
    '尚未回读微信实付（旧通道订单或支付确认中）',
    '该用户微信侧代币余额（本地镜像）',
    '微信侧在账 = 本地镜像（累计成功充值 − 已退款）；本地可用 = 用户当前灵石余额；已消费 = 两账净差额，含邀请/签到/手工调整等非虚拟支付来源灵石的净影响',
  ];
  for (const explanation of explanations) {
    const matches = nodes(page.tree, node => node.type === 'details' && textOf(node).includes(explanation));
    assert.ok(matches.length > 0, explanation);
    for (const disclosure of matches) {
      assert.equal(nodes(disclosure, node => node.type === 'summary').length, 1);
      assert.equal(disclosure.props.open, undefined, 'Native disclosure is initially collapsed');
    }
  }
  assert.match(textOf(page.tree), /技术服务费 ¥0.30/);
  assert.doesNotMatch(textOf(page.tree), /悬停看口径/);
});

test('a preview response for another order is rejected locally before it can become refundable', async () => {
  const page = await boot();
  open(page, 101);
  await settle(page, page.requests[0], preview(202));
  assert.match(textOf(modal(page)), /返回的退款预览与当前订单不一致/);
  assert.equal(button(modal(page), '确认发起退款').props.disabled, true);
  // Invoke the production handler too: disabled markup alone is not a write guard.
  button(modal(page), '确认发起退款').props.onClick(); await flush();
  assert.equal(page.prompts.length, 0);
  assert.equal(page.refunds.length, 0);
  click(page, button(modal(page), '重新加载'));
  assert.equal(page.requests[1].id, '101');
  await settle(page, page.requests[1], preview(101));
  assertReady(page, 101);
});

test('the final production refund handler independently rejects a mismatched current preview', async () => {
  const page = await boot();
  const response = preview(101);
  open(page, 101); await settle(page, page.requests[0], response);
  const confirm = button(modal(page), '确认发起退款').props.onClick;
  // Deliberately corrupt already-loaded fixture state to exercise the final
  // handler's own identity check independently from response validation.
  response.order_id = 202;
  page.setPrompt('must never be submitted');
  confirm(); page.commit(); await flush();
  assert.equal(page.prompts.length, 0);
  assert.equal(page.refunds.length, 0);
  assert.match(textOf(modal(page)), /退款预览与当前订单不一致/);
  assert.equal(button(modal(page), '确认发起退款').props.disabled, true);
});

test('saved refund handlers cannot submit after switching orders or reopening the same order', async () => {
  for (const nextId of [101, 202]) {
    const page = await boot();
    open(page, 101); await settle(page, page.requests[0], preview(101));
    const staleConfirm = button(modal(page), '确认发起退款').props.onClick;
    dismiss(page, 'cancel'); open(page, nextId);
    page.setPrompt('must never be submitted');
    staleConfirm(); page.commit(); await flush();
    assert.equal(page.prompts.length, 0);
    assert.equal(page.refunds.length, 0);
    assert.match(textOf(modal(page)), /正在获取退款影响清单/);
    await settle(page, page.requests[1], preview(nextId));
    assertReady(page, nextId);
    staleConfirm(); page.commit(); await flush();
    assert.equal(page.refunds.length, 0);
    assertReady(page, nextId);
  }
});

test('two calls to the same refund handler before any render submit exactly once and cannot dismiss it', async () => {
  const page = await boot();
  open(page, 101); await settle(page, page.requests[0], preview(101));
  page.setPrompt('fixture single-flight reason');
  const confirm = button(modal(page), '确认发起退款').props.onClick;
  const cancel = button(modal(page), '取消').props.onClick;
  const backdrop = byClass(page.tree, 'modal-overlay')[0].props.onClick;
  confirm(); confirm(); cancel(); backdrop();
  // Deliberately no commit between invocations: useState alone cannot guard this.
  assert.equal(page.refunds.length, 1);
  assert.equal(page.prompts.length, 1);
  assert.deepEqual(page.refunds[0], ['101', 'fixture single-flight reason']);
  page.commit();
  assert.ok(modal(page));
  assert.equal(button(modal(page), '退款提交中...').props.disabled, true);
  await settle(page, page.refundResult, { order_no: 'fixture-order-101', refund_amount: 50, old_stones: 80, new_stones: 30 });
  assert.equal(modal(page), undefined);
});

function drawer(page) { return byClass(page.tree, 'order-drawer')[0]; }
function viewDetail(page, id, entry) {
  if (entry === 'URL') page.setSearchParams(`orderId=${id}`);
  else click(page, byClass(row(page, id), 'btn-view')[0]);
}
function assertDetail(page, id) {
  assert.ok(drawer(page));
  assert.match(textOf(drawer(page)), new RegExp(`fixture-order-${id}`));
  const other = id === 101 ? 202 : 101;
  assert.doesNotMatch(textOf(drawer(page)), new RegExp(`fixture-order-${other}`));
  assert.equal(nodes(drawer(page), node => node.type === 'button' && textOf(node) === '虚拟支付退款').length, 1);
}

test('list and URL detail loads share latest-request ownership across success, failure, and entry changes', async () => {
  for (const [firstEntry, secondEntry] of [['list', 'list'], ['URL', 'URL'], ['list', 'URL'], ['URL', 'list']]) {
    for (const staleReject of [false, true]) {
      for (const staleFirst of [false, true]) {
        const page = await boot({ deferDetails: true });
        viewDetail(page, 101, firstEntry); viewDetail(page, 202, secondEntry);
        assert.deepEqual(page.detailRequests.map(request => request.id), ['101', '202']);
        assert.match(textOf(drawer(page)), /正在加载订单详情/);
        assert.equal(nodes(drawer(page), node => node.type === 'button' && textOf(node) === '虚拟支付退款').length, 0);
        if (staleFirst) {
          await settle(page, page.detailRequests[0], staleReject ? new Error('stale detail A') : detail(101), staleReject);
          assert.match(textOf(drawer(page)), /正在加载订单详情/);
          assert.doesNotMatch(textOf(drawer(page)), /stale detail A|fixture-order-101/);
        }
        await settle(page, page.detailRequests[1], detail(202));
        assertDetail(page, 202);
        if (!staleFirst) await settle(page, page.detailRequests[0], staleReject ? new Error('stale detail A') : detail(101), staleReject);
        assertDetail(page, 202);
        assert.equal(page.refunds.length, 0);
      }
    }
  }
});

test('closing a loading detail through every control or URL removal prevents late response resurrection', async () => {
  for (const entry of ['list', 'URL']) {
    for (const method of ['close', 'footer', 'backdrop', 'URL removal']) {
      for (const reject of [false, true]) {
        const page = await boot({ deferDetails: true });
        viewDetail(page, 101, entry);
        if (method === 'close') click(page, byClass(drawer(page), 'drawer-close')[0]);
        if (method === 'footer') click(page, button(drawer(page), '关闭'));
        if (method === 'backdrop') click(page, byClass(page.tree, 'drawer-overlay')[0]);
        if (method === 'URL removal') page.setSearchParams('');
        assert.equal(drawer(page), undefined);
        await settle(page, page.detailRequests[0], reject ? new Error('late detail error') : detail(101), reject);
        assert.equal(drawer(page), undefined);
        assert.equal(page.alerts.length, 0);
        assert.equal(page.refunds.length, 0);
      }
    }
  }
});

test('a failed or wrong-ID detail clears old actions and retries only the currently requested order', async () => {
  for (const entry of ['list', 'URL']) {
    for (const mismatch of [false, true]) {
      const page = await boot({ deferDetails: true });
      viewDetail(page, 101, entry);
      await settle(page, page.detailRequests[0], detail(101)); assertDetail(page, 101);
      viewDetail(page, 202, entry);
      assert.doesNotMatch(textOf(drawer(page)), /fixture-order-101|虚拟支付退款/);
      await settle(page, page.detailRequests[1], mismatch ? detail(101) : new Error('fixture detail unavailable'), !mismatch);
      assert.match(textOf(drawer(page)), mismatch ? /返回的订单详情与当前订单不一致/ : /fixture detail unavailable/);
      assert.doesNotMatch(textOf(drawer(page)), /fixture-order-101|虚拟支付退款/);
      click(page, button(drawer(page), '重新加载'));
      assert.equal(page.detailRequests[2].id, '202');
      await settle(page, page.detailRequests[2], detail(202));
      assertDetail(page, 202);
    }
  }
});

test('unmount invalidates both list and URL detail requests without state writes', async () => {
  for (const entry of ['list', 'URL']) {
    for (const reject of [false, true]) {
      const page = await boot({ deferDetails: true });
      viewDetail(page, 101, entry); page.unmount();
      page.detailRequests[0][reject ? 'reject' : 'resolve'](reject ? new Error('unmounted detail') : detail(101));
      await flush();
      assert.equal(page.afterUnmountWrites, 0);
      assert.equal(page.alerts.length, 0);
    }
  }
});

test('finishing refund A cannot refresh A over a newly selected detail B', async () => {
  const page = await boot({ deferDetails: true });
  viewDetail(page, 101, 'list'); await settle(page, page.detailRequests[0], detail(101));
  click(page, button(drawer(page), '虚拟支付退款'));
  await settle(page, page.requests[0], preview(101));
  page.setPrompt('fixture refund reason'); click(page, button(modal(page), '确认发起退款'));
  viewDetail(page, 202, 'URL'); await settle(page, page.detailRequests[1], detail(202));
  assertDetail(page, 202);
  await settle(page, page.refundResult, { order_no: 'fixture-order-101', refund_amount: 50, old_stones: 80, new_stones: 30 });
  assertDetail(page, 202);
  assert.deepEqual(page.detailRequests.map(request => request.id), ['101', '202']);
  assert.deepEqual(page.refunds, [['101', 'fixture refund reason']]);
});

test('slow detail A cannot replace fast detail B after both production handlers have completed', async () => {
  const page = await boot({ deferDetails: true });
  // This end-state regression intentionally does not depend on the new loading UI.
  viewDetail(page, 101, 'list'); viewDetail(page, 202, 'list');
  await settle(page, page.detailRequests[1], detail(202));
  assertDetail(page, 202);
  await settle(page, page.detailRequests[0], detail(101));
  assertDetail(page, 202);
});

test('a failed refund releases the synchronous lock and permits one deliberate retry', async () => {
  const page = await boot();
  open(page, 101); await settle(page, page.requests[0], preview(101));
  page.setPrompt('fixture first reason');
  click(page, button(modal(page), '确认发起退款'));
  await settle(page, page.refundResults[0], new Error('fixture refund failed'), true);
  assertReady(page, 101);
  assert.deepEqual(page.alerts, ['fixture refund failed']);
  page.setPrompt('fixture retry reason');
  const retry = button(modal(page), '确认发起退款').props.onClick;
  retry(); retry(); page.commit();
  assert.deepEqual(page.refunds, [['101', 'fixture first reason'], ['101', 'fixture retry reason']]);
  assert.equal(page.prompts.length, 2);
  await settle(page, page.refundResults[1], { order_no: 'fixture-order-101', refund_amount: 50, old_stones: 80, new_stones: 30 });
  assert.equal(modal(page), undefined);
});
