import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
import vm from 'node:vm';
import test from 'node:test';
import ts from 'typescript';
import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';

const require = createRequire(import.meta.url);
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const settle = () => new Promise((resolve) => setImmediate(resolve));
const deferred = () => {
  let resolve;
  let reject;
  const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
};
const allNodes = (node) => {
  if (Array.isArray(node)) return node.flatMap(allNodes);
  if (!node || typeof node !== 'object') return [];
  return [node, ...allNodes(node.props?.children)];
};
const textOf = (node) => {
  if (Array.isArray(node)) return node.map(textOf).join('');
  if (node == null || typeof node === 'boolean') return '';
  return typeof node === 'object' ? textOf(node.props?.children) : String(node);
};

// Execute the page's real event handlers. Only React hook storage and API I/O are
// substituted; TSX rendering and save/load decisions come from production code.
function page(filename, api) {
  const state = [];
  const effects = [];
  const cleanups = [];
  const mountedEffects = [];
  const alerts = [];
  const reports = [];
  let cursor = 0;
  let tree;
  const react = {
    ...React,
    useState(initial) {
      const index = cursor++;
      if (!(index in state)) state[index] = typeof initial === 'function' ? initial() : initial;
      return [state[index], (next) => { state[index] = typeof next === 'function' ? next(state[index]) : next; }];
    },
    useRef(initial) {
      const index = cursor++;
      if (!(index in state)) state[index] = { current: initial };
      return state[index];
    },
    useEffect(effect, deps) {
      const index = cursor++;
      if (!(index in state) || deps.some((dep, i) => dep !== state[index][i])) {
        state[index] = deps;
        effects.push(effect);
      }
    },
  };
  const module = { exports: {} };
  const code = ts.transpileModule(fs.readFileSync(path.join(root, 'src/pages', filename), 'utf8'), {
    compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.CommonJS, jsx: ts.JsxEmit.ReactJSX, esModuleInterop: true },
  }).outputText;
  vm.runInNewContext(code, {
    module, exports: module.exports,
    require(name) {
      if (name === 'react') return react;
      if (name === 'react/jsx-runtime') return require(name);
      if (name === 'react-icons/fi') return new Proxy({}, { get: () => () => null });
      if (name.startsWith('../api/')) return api;
      if (name === '../utils/pageLoadError') return { reportPageLoadError: (...args) => reports.push(args), reportPageLoadRecovered() {} };
      if (name.includes('useIsMobile')) return {useIsMobile:()=>false};
  if (name.endsWith('.scss')) return {};
      throw new Error(`Unexpected dependency: ${name}`);
    },
    alert: (message) => alerts.push(message),
    window: { confirm: () => true, location: { pathname: '/fixture' } },
    console: { error() {} },
  }, { filename });
  const harness = {
    alerts, reports,
    render() { cursor = 0; tree = module.exports.default(); return tree; },
    nodes() { return allNodes(tree); },
    find(predicate) { const found = harness.nodes().find(predicate); assert.ok(found, 'Expected UI element'); return found; },
    button(label) { return harness.find((node) => node.type === 'button' && textOf(node).trim() === label); },
    edit(id) {
      const row = harness.find((node) => node.key === String(id) && node.props.className.includes('config-item'));
      return allNodes(row).find((node) => node.type === 'button' && node.props.className === 'btn-edit');
    },
    remove(id) {
      const row = harness.find((node) => node.key === String(id) && node.props.className.includes('config-item'));
      return allNodes(row).find((node) => node.type === 'button' && node.props.className === 'btn-delete');
    },
    input(placeholder) { return harness.find((node) => node.type === 'input' && node.props.placeholder === placeholder); },
    markup() { return renderToStaticMarkup(tree); },
    selectMode(mode) { return harness.find((node) => node.type === 'select').props.onChange({ target: { value: mode } }); },
    async mount() {
      harness.render();
      for (const effect of effects.splice(0)) {
        mountedEffects.push(effect);
        cleanups.push(effect());
      }
      await settle();
      harness.render();
      return harness;
    },
    async replayEffects() {
      for (const cleanup of cleanups.splice(0)) cleanup?.();
      for (const effect of mountedEffects) cleanups.push(effect());
      await settle();
      harness.render();
    },
    unmount() { for (const cleanup of cleanups) cleanup?.(); },
  };
  return harness;
}

const config = (mode, data = {}, enabled = true, id = 1) => ({ id, payment_mode: mode, config_data: data, is_enabled: enabled });
function rechargeApi(overrides = {}) {
  const saved = [];
  const api = {
    getRechargeConfigList: async () => [],
    getRechargeConfig: async (mode) => config(mode),
    createOrUpdateRechargeConfig: async (payload) => { saved.push(payload); },
    deleteRechargeConfig: async () => {},
    ...overrides,
  };
  return { api, saved };
}
const plan = (id, overrides = {}) => ({
  id, plan_code: `plan_${id}`, title: `Plan ${id}`, description: '', badge_text: '',
  recharge_amount_fen: 5000, duration_days: 30, template_download_enabled: true,
  is_enabled: true, sort_order: 0, download_validity_days: 30,
  max_total_downloads: 0, daily_download_limit: 0, rate_limit_per_minute: 0,
  ...overrides,
});
function membershipApi(overrides = {}) {
  const saved = [];
  const api = {
    getMembershipPlanList: async () => [plan(1), plan(2)],
    getMembershipPlan: async (id) => plan(Number(id)),
    createOrUpdateMembershipPlan: async (payload) => { saved.push(payload); },
    deleteMembershipPlan: async () => {},
    ...overrides,
  };
  return { api, saved };
}

test('recharge: initial unverified form cannot overwrite a default payment mode', async () => {
  const { api, saved } = rechargeApi();
  const h = await page('rechargeConfig.tsx', api).mount();
  assert.equal(h.button('保存').props.disabled, true);
  await h.button('保存').props.onClick();
  assert.equal(saved.length, 0);
  assert.equal(h.find((node) => node.type === 'select').props.value, '');
});

test('recharge: list/network/permission errors never become empty writable configurations', async () => {
  for (const message of ['网络断开', '无权限', 'HTTP 500']) {
    const { api, saved } = rechargeApi();
    const h = await page('rechargeConfig.tsx', api).mount();
    api.getRechargeConfigList = async () => { throw new Error(message); };
    await h.selectMode('wechat_only');
    h.render();
    assert.match(h.markup(), /读取失败，暂不能保存/);
    assert.match(h.markup(), new RegExp(message));
    assert.equal(h.button('保存').props.disabled, true);
    await h.button('保存').props.onClick();
    assert.equal(saved.length, 0);
    assert.equal(h.find((node) => node.type === 'fieldset').props.disabled, true);
    h.button('重试');
  }
});

test('recharge: existing-record detail failure stays blocked; retry preserves enabled state and account data', async () => {
  const current = config('wechat_only', { wechat_account: 'existing-account', note: 'keep-note' }, true);
  const { api, saved } = rechargeApi({
    getRechargeConfigList: async () => [current],
    getRechargeConfig: async () => { throw new Error('读取失败'); },
  });
  const h = await page('rechargeConfig.tsx', api).mount();
  await h.selectMode('wechat_only');
  h.render();
  await h.button('保存').props.onClick();
  assert.equal(saved.length, 0);
  api.getRechargeConfig = async () => current;
  await h.button('重试').props.onClick();
  h.render();
  assert.equal(h.input('微信号或手机号').props.value, 'existing-account');
  assert.equal(h.find((node) => node.type === 'input' && node.props.type === 'checkbox').props.checked, true);
  await h.button('保存').props.onClick();
  assert.equal(saved.length, 1);
  assert.equal(saved[0].payment_mode, 'wechat_only');
  assert.deepEqual(saved[0].config_data, current.config_data);
  assert.equal(saved[0].is_enabled, true);
});

test('recharge: only a successful current absence check enables creating a disabled configuration', async () => {
  let detailReads = 0;
  const { api, saved } = rechargeApi({ getRechargeConfig: async () => { detailReads++; throw new Error('not found'); } });
  const h = await page('rechargeConfig.tsx', api).mount();
  await h.selectMode('alipay_only');
  h.render();
  assert.equal(h.button('保存').props.disabled, false);
  h.input('支付宝账号或手机号').props.onChange({ target: { value: 'new-account' } });
  h.render();
  await h.button('保存').props.onClick();
  assert.equal(detailReads, 0);
  assert.equal(saved[0].payment_mode, 'alipay_only');
  assert.equal(saved[0].config_data.alipay_account, 'new-account');
  assert.equal(saved[0].is_enabled, false);
});

test('recharge: a detail returned for another payment mode is blocked', async () => {
  const { api, saved } = rechargeApi({
    getRechargeConfigList: async () => [config('wechat_only')],
    getRechargeConfig: async () => config('alipay_only'),
  });
  const h = await page('rechargeConfig.tsx', api).mount();
  await h.selectMode('wechat_only');
  h.render();
  assert.match(h.markup(), /与所选支付方式不一致/);
  await h.button('保存').props.onClick();
  assert.equal(saved.length, 0);
});

test('recharge: A/B out-of-order detail completions cannot change B or save A under B', async () => {
  const a = deferred();
  const b = deferred();
  const { api, saved } = rechargeApi({
    getRechargeConfigList: async () => [config('wechat_only'), config('alipay_only', {}, true, 2)],
    getRechargeConfig: (mode) => mode === 'wechat_only' ? a.promise : b.promise,
  });
  const h = await page('rechargeConfig.tsx', api).mount();
  const first = h.selectMode('wechat_only');
  await settle();
  h.render();
  const second = h.selectMode('alipay_only');
  await settle();
  h.render();
  assert.equal(h.button('保存').props.disabled, true);
  await h.button('保存').props.onClick();
  b.resolve(config('alipay_only', { alipay_account: 'B' }, true, 2));
  await second;
  a.resolve(config('wechat_only', { wechat_account: 'A' }));
  await first;
  h.render();
  assert.equal(h.input('支付宝账号或手机号').props.value, 'B');
  await h.button('保存').props.onClick();
  assert.equal(saved.length, 1);
  assert.equal(saved[0].payment_mode, 'alipay_only');
  assert.equal(saved[0].config_data.alipay_account, 'B');
  assert.equal(saved[0].config_data.wechat_account, undefined);
});

test('recharge: stale list failure cannot replace a newer successful selection', async () => {
  const a = deferred();
  const { api } = rechargeApi();
  const h = await page('rechargeConfig.tsx', api).mount();
  api.getRechargeConfigList = () => a.promise;
  const first = h.selectMode('wechat_only');
  h.render();
  api.getRechargeConfigList = async () => [];
  await h.selectMode('alipay_only');
  a.reject(new Error('old failure'));
  await first;
  h.render();
  assert.equal(h.button('保存').props.disabled, false);
  assert.doesNotMatch(h.markup(), /old failure/);
  assert.equal(h.find((node) => node.type === 'select').props.value, 'alipay_only');
});

test('recharge: cancel invalidates a pending detail and keeps save disabled', async () => {
  const result = deferred();
  const { api, saved } = rechargeApi({
    getRechargeConfigList: async () => [config('wechat_only')],
    getRechargeConfig: () => result.promise,
  });
  const h = await page('rechargeConfig.tsx', api).mount();
  const loading = h.selectMode('wechat_only');
  await settle();
  h.render();
  h.button('取消').props.onClick();
  result.resolve(config('wechat_only', { wechat_account: 'late' }));
  await loading;
  h.render();
  assert.equal(h.find((node) => node.type === 'select').props.value, '');
  assert.doesNotMatch(h.markup(), /value="late"/);
  await h.button('保存').props.onClick();
  assert.equal(saved.length, 0);
});

test('recharge: repeated save is single-flight and a failed save retains editable configuration', async () => {
  const result = deferred();
  let writes = 0;
  const { api } = rechargeApi({ createOrUpdateRechargeConfig: () => { writes++; return result.promise; } });
  const h = await page('rechargeConfig.tsx', api).mount();
  await h.selectMode('wechat_only');
  h.render();
  h.input('微信号或手机号').props.onChange({ target: { value: 'keep-on-failure' } });
  h.render();
  const save = h.button('保存').props.onClick;
  const first = save();
  await save();
  assert.equal(writes, 1);
  result.reject(new Error('保存失败'));
  await first;
  h.render();
  assert.equal(h.input('微信号或手机号').props.value, 'keep-on-failure');
  assert.equal(h.button('保存').props.disabled, false);
});

test('membership: selecting B immediately clears A; B failure cannot submit old A data', async () => {
  const failure = deferred();
  const { api, saved } = membershipApi({ getMembershipPlan: (id) => id === '2' ? failure.promise : Promise.resolve(plan(1)) });
  const h = await page('membershipPlans.tsx', api).mount();
  await h.edit(1).props.onClick();
  h.render();
  const oldSave = h.button('保存计划').props.onClick;
  const second = h.edit(2).props.onClick();
  h.render();
  assert.equal(h.input('例如 download_monthly').props.value, '');
  assert.equal(h.input('例如 模板下载月卡').props.value, '');
  assert.equal(h.button('保存计划').props.disabled, true);
  await oldSave();
  failure.reject(new Error('无权限'));
  await second;
  h.render();
  assert.match(h.markup(), /读取失败，暂不能保存：无权限/);
  await h.button('保存计划').props.onClick();
  assert.equal(saved.length, 0);
  assert.equal(h.find((node) => node.type === 'fieldset').props.disabled, true);
});

test('membership: retry saves the selected ID and preserves payment, benefit and limit semantics', async () => {
  const item = plan(2, {
    plan_code: '  year  ', title: '  Year  ', description: '  description  ', badge_text: '  badge  ',
    recharge_amount_fen: 8899, duration_days: 365, template_download_enabled: false,
    is_enabled: false, sort_order: 7, download_validity_days: 0,
    max_total_downloads: 0, daily_download_limit: 13, rate_limit_per_minute: 0,
  });
  const { api, saved } = membershipApi({ getMembershipPlan: async () => { throw new Error('server failure'); } });
  const h = await page('membershipPlans.tsx', api).mount();
  await h.edit(2).props.onClick();
  h.render();
  api.getMembershipPlan = async () => item;
  await h.button('重试').props.onClick();
  h.render();
  await h.button('保存计划').props.onClick();
  assert.deepEqual(JSON.parse(JSON.stringify(saved[0])), { ...item, plan_code: 'year', title: 'Year', description: 'description', badge_text: 'badge' });
});

test('membership: A/B out-of-order responses leave B visible and save only B', async () => {
  const a = deferred();
  const b = deferred();
  const { api, saved } = membershipApi({ getMembershipPlan: (id) => id === '1' ? a.promise : b.promise });
  const h = await page('membershipPlans.tsx', api).mount();
  const first = h.edit(1).props.onClick();
  h.render();
  const second = h.edit(2).props.onClick();
  b.resolve(plan(2));
  await second;
  a.resolve(plan(1));
  await first;
  h.render();
  assert.equal(h.input('例如 模板下载月卡').props.value, 'Plan 2');
  await h.button('保存计划').props.onClick();
  assert.equal(saved.length, 1);
  assert.equal(saved[0].id, 2);
  assert.equal(saved[0].plan_code, 'plan_2');
});

test('membership: stale errors cannot block B, and mismatched response identity cannot enable save', async () => {
  const a = deferred();
  const { api, saved } = membershipApi({ getMembershipPlan: (id) => id === '1' ? a.promise : Promise.resolve(plan(2)) });
  const h = await page('membershipPlans.tsx', api).mount();
  const first = h.edit(1).props.onClick();
  h.render();
  await h.edit(2).props.onClick();
  a.reject(new Error('old failure'));
  await first;
  h.render();
  assert.equal(h.button('保存计划').props.disabled, false);
  assert.doesNotMatch(h.markup(), /old failure/);
  api.getMembershipPlan = async () => plan(1);
  await h.edit(2).props.onClick();
  h.render();
  assert.match(h.markup(), /与所选记录不一致/);
  await h.button('保存计划').props.onClick();
  assert.equal(saved.length, 0);
});

test('membership: reset invalidates in-flight detail and new plan still uses original validation/defaults', async () => {
  const result = deferred();
  const { api, saved } = membershipApi({ getMembershipPlan: () => result.promise });
  const h = await page('membershipPlans.tsx', api).mount();
  const loading = h.edit(1).props.onClick();
  h.render();
  h.button('重置').props.onClick();
  result.resolve(plan(1));
  await loading;
  h.render();
  assert.equal(h.input('例如 download_monthly').props.value, '');
  await h.button('保存计划').props.onClick();
  assert.equal(saved.length, 0);
  assert.equal(h.alerts.at(-1), '计划编码和计划名称不能为空');
  h.input('例如 download_monthly').props.onChange({ target: { value: ' new_plan ' } });
  h.input('例如 模板下载月卡').props.onChange({ target: { value: ' New Plan ' } });
  h.render();
  await h.button('保存计划').props.onClick();
  assert.equal(saved[0].id, 0);
  assert.equal(saved[0].plan_code, 'new_plan');
  assert.equal(saved[0].recharge_amount_fen, 5000);
  assert.equal(saved[0].duration_days, 30);
  assert.equal(saved[0].template_download_enabled, true);
});

test('membership: amount/duration validation and backend deletion rejection remain intact', async () => {
  const { api, saved } = membershipApi({
    getMembershipPlan: async () => plan(1, { recharge_amount_fen: 0 }),
    deleteMembershipPlan: async () => { throw new Error('该会员计划已有引用，不能删除'); },
  });
  const h = await page('membershipPlans.tsx', api).mount();
  await h.edit(1).props.onClick();
  h.render();
  await h.button('保存计划').props.onClick();
  assert.equal(saved.length, 0);
  assert.equal(h.alerts.at(-1), '充值金额和有效天数必须大于 0');
  await h.remove(1).props.onClick();
  h.render();
  assert.equal(h.alerts.at(-1), '该会员计划已有引用，不能删除');
  assert.equal(h.input('例如 模板下载月卡').props.value, 'Plan 1');
});

test('membership: repeated save is single-flight and selection/reset are blocked while saving', async () => {
  const result = deferred();
  let writes = 0;
  const { api } = membershipApi({ createOrUpdateMembershipPlan: () => { writes++; return result.promise; } });
  const h = await page('membershipPlans.tsx', api).mount();
  await h.edit(1).props.onClick();
  h.render();
  const save = h.button('保存计划').props.onClick;
  const selectB = h.edit(2).props.onClick;
  const reset = h.button('重置').props.onClick;
  const first = save();
  await save();
  await selectB();
  reset();
  h.render();
  assert.equal(writes, 1);
  assert.equal(h.input('例如 模板下载月卡').props.value, 'Plan 1');
  assert.equal(h.button('重置').props.disabled, true);
  result.reject(new Error('保存失败'));
  await first;
  h.render();
  assert.equal(h.button('保存计划').props.disabled, false);
  assert.equal(h.input('例如 模板下载月卡').props.value, 'Plan 1');
});

test('both configuration pages ignore detail responses after unmount', async () => {
  const rechargeResult = deferred();
  const membershipResult = deferred();
  const { api: recharge } = rechargeApi({ getRechargeConfigList: async () => [config('wechat_only')], getRechargeConfig: () => rechargeResult.promise });
  const { api: membership } = membershipApi({ getMembershipPlan: () => membershipResult.promise });
  const h1 = await page('rechargeConfig.tsx', recharge).mount();
  const h2 = await page('membershipPlans.tsx', membership).mount();
  const load1 = h1.selectMode('wechat_only');
  const load2 = h2.edit(1).props.onClick();
  await settle();
  h1.render();
  h2.render();
  h1.unmount();
  h2.unmount();
  rechargeResult.resolve(config('wechat_only', { wechat_account: 'late' }));
  membershipResult.resolve(plan(1, { title: 'late' }));
  await Promise.all([load1, load2]);
  h1.render();
  h2.render();
  assert.equal(h1.input('微信号或手机号').props.value, '');
  assert.equal(h2.input('例如 模板下载月卡').props.value, '');
});


test('membership: StrictMode effect replay keeps the initial new-plan form writable', async () => {
  const { api, saved } = membershipApi();
  const h = await page('membershipPlans.tsx', api).mount();
  await h.replayEffects();
  h.input('例如 download_monthly').props.onChange({ target: { value: 'new_plan' } });
  h.input('例如 模板下载月卡').props.onChange({ target: { value: 'New Plan' } });
  h.render();
  await h.button('保存计划').props.onClick();
  assert.equal(saved.length, 1);
  assert.equal(saved[0].id, 0);
});

test('configuration delete locks same-frame save/delete/selection and preserves a rejected record', async () => {
  for (const kind of ['membership', 'recharge']) {
    const deletion = deferred(); const deleted = [];
    const fixture = kind === 'membership'
      ? membershipApi({ deleteMembershipPlan: id => { deleted.push(id); return deletion.promise; } })
      : rechargeApi({ getRechargeConfigList: async () => [config('wechat_only')], deleteRechargeConfig: id => { deleted.push(id); return deletion.promise; } });
    const h = await page(kind === 'membership' ? 'membershipPlans.tsx' : 'rechargeConfig.tsx', fixture.api).mount();
    if (kind === 'membership') await h.edit(1).props.onClick(); else await h.selectMode('wechat_only');
    h.render();
    const save = h.button(kind === 'membership' ? '保存计划' : '保存').props.onClick;
    const remove = h.remove(1).props.onClick;
    const first = remove();
    await remove(); await save();
    h.render();
    assert.equal(deleted.length, 1, 'repeated delete is synchronous single-flight');
    assert.equal(fixture.saved.length, 0, 'a previously captured save cannot race delete');
    assert.equal(h.button('删除中...').props.disabled, true);
    deletion.reject(new Error('记录被引用，无法删除')); await first; h.render();
    assert.match(h.markup(), kind === 'membership' ? /Plan 1/ : /wechat_only/);
    assert.equal(h.button(kind === 'membership' ? '保存计划' : '保存').props.disabled, false);
  }
});

test('failed configuration lists are unknown rather than confirmed empty and can retry', async () => {
  for (const kind of ['membership', 'recharge']) {
    let fail = true;
    const read = async () => { if (fail) throw new Error('fixture unavailable'); return []; };
    const fixture = kind === 'membership' ? membershipApi({ getMembershipPlanList: read }) : rechargeApi({ getRechargeConfigList: read });
    const h = await page(kind === 'membership' ? 'membershipPlans.tsx' : 'rechargeConfig.tsx', fixture.api).mount();
    assert.match(h.markup(), /未能加载，当前列表可能不完整/);
    assert.doesNotMatch(h.markup(), /暂无会员计划|暂无配置/);
    fail = false; await h.button('重新加载列表').props.onClick(); await settle(); h.render();
    assert.doesNotMatch(h.markup(), /未能加载，当前列表可能不完整/);
    assert.match(h.markup(), kind === 'membership' ? /暂无会员计划/ : /暂无配置/);
  }
});
