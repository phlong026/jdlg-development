import assert from 'node:assert/strict';
import test from 'node:test';
import { environment, CRMError, baseSession, account, user, deferred, textOf } from './helpers/crm-harness.mjs';

const cases = [
  { name: 'legacy enabled only after explicit global-off session', allow: true },
  { name: 'global CRM takeover with customer access', session: { user: { ...baseSession().user, crmEnabled: true }, crmContext: { account: account(7) } }, link: true },
  { name: 'global CRM takeover despite missing account access', session: { user: { ...baseSession().user, crmEnabled: true }, crmError: '该账号未启用客户访问' } },
  { name: 'global CRM takeover while context is unavailable', session: { user: { ...baseSession().user, crmEnabled: true }, crmError: '服务不可用' } },
  { name: 'context still loading', session: { user: { ...baseSession().user, crmEnabled: true }, crmLoading: true } },
  { name: 'session loading', session: { loading: true } },
  { name: 'session error', session: { error: '检查失败' } },
  { name: 'expired session', session: { user: null } },
  { name: 'unknown global switch', session: { user: { ...baseSession().user, crmEnabled: undefined } } },
  { name: 'CRM-only account', session: { user: { ...baseSession().user, scope: 'crm_only' } } },
  { name: 'explicit read-only remains restrictive', readOnly: true },
];
for (const embedded of [false, true]) {
  for (const fixture of cases) test(`legacy leads ${embedded ? 'embedded' : 'standalone'}: ${fixture.name}`, async () => {
    const env = environment(); env.session = { ...env.session, ...fixture.session };
    const h = await env.mount('customerLeads.tsx', 'default', { embedded, readOnly: fixture.readOnly });
    assert.equal(h.button('跟进').props.disabled, !fixture.allow);
    assert.equal(h.find(n => n.type === 'input' && n.props.type === 'checkbox').props.disabled, !fixture.allow);
    assert.equal(h.all(n => n.type === 'a' && n.props.to === '/customer-management?tab=customers').length > 0, !!fixture.link);
    h.button('跟进').props.onClick(); h.render();
    const editor = h.all(n => n.type === 'textarea');
    assert.equal(editor.length, fixture.allow ? 1 : 0);
    if (fixture.allow) {
      editor[0].props.onChange({ target: { value: '已联系' } }); h.render();
      h.button('保存跟进').props.onClick(); await h.flush();
      assert.equal(env.leadWrites.length, 1); assert.equal(env.leadWrites[0].action, 'status');
    } else assert.equal(env.leadWrites.length, 0);
  });
}

test('legacy batch/detail actions and a saved handler cannot bypass global takeover or session expiration', async () => {
  const env = environment(); const h = await env.mount('customerLeads.tsx');
  h.find(n => n.type === 'input' && n.props.type === 'checkbox').props.onChange(); h.render();
  h.button('批量处理').props.onClick(); h.render();
  h.find(n => n.type === 'textarea').props.onChange({ target: { value: '批量跟进' } }); h.render();
  h.button('保存跟进').props.onClick(); await h.flush();
  assert.equal(env.leadWrites[0].action, 'batch'); assert.deepEqual(Array.from(env.leadWrites[0].args[0]), [1]);
  await h.button('详情').props.onClick(); await h.flush();
  h.button('记录跟进').props.onClick(); h.render();
  h.find(n => n.type === 'textarea').props.onChange({ target: { value: '不应发送' } }); h.render();
  const cachedSave = h.button('保存跟进').props.onClick;
  env.session = { ...env.session, user: { ...env.session.user, crmEnabled: true }, crmError: 'context failed' }; h.render();
  cachedSave(); await h.flush();
  assert.equal(env.leadWrites.length, 1); assert.equal(h.all(n => n.type === 'textarea').length, 0);
  assert.equal(h.button('记录跟进').props.disabled, true);
  env.session = baseSession(); await h.flush();
  h.button('跟进').props.onClick(); h.render();
  h.find(n => n.type === 'textarea').props.onChange({ target: { value: '已失效' } }); h.render();
  const expiredSave = h.button('保存跟进').props.onClick;
  env.session.user = null; h.render(); expiredSave(); await h.flush();
  assert.equal(env.leadWrites.length, 1);
});

test('customer management embeds the same session-gated lead component when CRM context fails', async () => {
  const env = environment(); env.params = new URLSearchParams('tab=leads'); env.session.user.crmEnabled = true; env.session.crmError = '客户服务不可用';
  const h = await env.mount('customerManagement.tsx');
  const child = h.find(n => typeof n.type === 'function' && n.props.embedded === true);
  const leads = await env.mount('customerLeads.tsx', 'default', child.props);
  assert.equal(leads.button('跟进').props.disabled, true);
  assert.match(leads.text(), /客户服务不可用/);
  let refreshes = 0; env.session.refresh = () => refreshes++; leads.render(); leads.button('重新检查会话与客户权限').props.onClick(); assert.equal(refreshes, 1);
});

async function conflictFixture() {
  const env = environment(); const h = await env.mount('customerManagement.tsx', 'AccessPanel'); h.choose(1); h.change('显示名称', 'My draft'); h.change('客户数据角色', 'admin');
  const original = env.crm;
  env.crm = async (url, options = {}) => {
    if (options.method === 'POST') { env.accessWrites.push({ url, body: options.body }); throw new CRMError(409, '版本已变化'); }
    return original(url, options);
  };
  env.access = { ...env.access, users: [user(1, { version: 2, enabled: false, crmEnabled: false }), user(2)], accounts: [account(1, { displayName: 'Latest name', employeeId: 'wx2' }), account(2)] };
  await h.submit(); await h.flush();
  return { env, h, original };
}

test('access: refreshed lists cannot silently rebase an old permission draft after conflict', async () => {
  const { env, h, original } = await conflictFixture();
  assert.equal(env.accessWrites[0].body.version, 1); assert.equal(h.field('显示名称').props.value, 'My draft');
  assert.equal(h.button('保存授权').props.disabled, true); await h.submit(); assert.equal(env.accessWrites.length, 1);
  await h.button('读取最新权限').props.onClick(); await h.flush();
  assert.match(h.text(), /最新权限（版本 2）/); assert.match(h.text(), /后台访问停用/); assert.match(h.text(), /客户访问停用/); assert.match(h.text(), /Latest name/); assert.match(h.text(), /Employee two/);
  assert.equal(h.button('保存授权').props.disabled, true); await h.submit(); assert.equal(env.accessWrites.length, 1);
  h.button('已核对，保留我的输入继续编辑').props.onClick(); h.render();
  assert.equal(h.field('显示名称').props.value, 'My draft'); assert.equal(h.field('客户数据角色').props.value, 'admin');
  env.crm = original; await h.submit(); await h.flush();
  assert.equal(env.accessWrites.length, 2); assert.equal(env.accessWrites[1].body.version, 2); assert.equal(env.accessWrites[1].body.displayName, 'My draft');
});

test('access: explicit reload replaces the draft with the reviewed current permissions', async () => {
  const { env, h, original } = await conflictFixture();
  await h.button('读取最新权限').props.onClick(); await h.flush(); h.button('重新载入最新权限，替换我的输入').props.onClick(); h.render();
  assert.equal(h.field('显示名称').props.value, 'Latest name'); assert.equal(h.field('客户数据角色').props.value, 'staff');
  assert.equal(h.field('启用后台访问').props.checked, false); assert.equal(h.field('启用客户访问').props.checked, false);
  env.crm = original; await h.submit(); await h.flush(); assert.equal(env.accessWrites[1].body.version, 2); assert.equal(env.accessWrites[1].body.enabled, false);
});

test('access: unavailable latest permissions keep the conflict blocked and preserve the draft', async () => {
  const { env, h } = await conflictFixture();
  env.crm = async () => { throw new Error('latest read failed'); };
  await h.button('读取最新权限').props.onClick(); await h.flush();
  assert.match(h.text(), /读取最新权限失败：latest read failed/); assert.equal(h.button('保存授权').props.disabled, true); assert.equal(h.field('显示名称').props.value, 'My draft');
  assert.equal(h.all(n => n.type === 'button' && textOf(n).includes('保留我的输入')).length, 0); await h.submit(); assert.equal(env.accessWrites.length, 1);
});

test('access: non-conflict failures retain the original snapshot even if list reload advances its version', async () => {
  const env = environment(); const h = await env.mount('customerManagement.tsx', 'AccessPanel'); h.choose(1); h.change('显示名称', 'Original draft');
  const original = env.crm;
  env.crm = async (url, options = {}) => { if (options.method === 'POST') { env.accessWrites.push({ url, body: options.body }); throw new CRMError(503, 'service unavailable'); } return original(url, options); };
  env.access = { ...env.access, users: [user(1, { version: 5 }), user(2)] };
  await h.submit(); await h.flush(); await h.submit(); await h.flush();
  assert.deepEqual(env.accessWrites.map(x => x.body.version), [1, 1]); assert.equal(h.field('显示名称').props.value, 'Original draft');
});

for (const failure of [false, true]) test(`access: late ${failure ? 'failure' : 'success'} from A cannot close or modify a newer B editor`, async () => {
  const env = environment(); const pending = deferred(); const original = env.crm;
  env.crm = (url, options = {}) => options.method === 'POST' ? (env.accessWrites.push({ url, body: options.body }), pending.promise) : original(url, options);
  const h = await env.mount('customerManagement.tsx', 'AccessPanel'); h.choose(1); h.change('显示名称', 'A draft');
  const first = h.submit(); const duplicate = h.submit(); h.render(); assert.equal(env.accessWrites.length, 1); await duplicate;
  h.choose(2); h.change('显示名称', 'B draft');
  if (failure) pending.reject(new CRMError(409, 'late conflict')); else pending.resolve();
  await first; await h.flush();
  assert.equal(h.field('后台账号').props.value, 'User 2'); assert.equal(h.field('显示名称').props.value, 'B draft'); assert.equal(h.button('保存授权').props.disabled, false);
  assert.doesNotMatch(h.text(), /late conflict|授权已保存/);
  env.crm = original; await h.submit(); await h.flush(); assert.equal(env.accessWrites[1].body.userId, 2); assert.equal(env.accessWrites[1].body.version, 1);
});

test('access: a late latest-version read cannot replace a newer selected account', async () => {
  const { env, h, original } = await conflictFixture(); const pending = deferred(); env.crm = () => pending.promise;
  const first = h.button('读取最新权限').props.onClick(); h.render(); h.choose(2); h.change('显示名称', 'B stays');
  pending.resolve(env.access); await first; await h.flush();
  assert.equal(h.field('后台账号').props.value, 'User 2'); assert.equal(h.field('显示名称').props.value, 'B stays'); assert.doesNotMatch(h.text(), /最新权限（版本/);
  env.crm = original; await h.submit(); await h.flush(); assert.equal(env.accessWrites[1].body.userId, 2);
});

test('access: changing a platform administrator still requires the existing downgrade confirmation', async () => {
  const env = environment(); env.access.users[0] = user(1, { scope: 'platform_admin' }); env.confirmResult = false;
  const h = await env.mount('customerManagement.tsx', 'AccessPanel'); h.choose(1); h.change('平台访问范围', 'crm_only'); await h.submit();
  assert.equal(env.confirmations.length, 1); assert.equal(env.accessWrites.length, 0);
});

test('access: stale submit handlers cannot save A after selecting B or bypass a detected conflict', async () => {
  const env = environment(); const h = await env.mount('customerManagement.tsx', 'AccessPanel'); h.choose(1);
  const savedHandler = h.find(n => n.type === 'form').props.onSubmit;
  h.choose(2); await savedHandler({ preventDefault() {} }); assert.equal(env.accessWrites.length, 0);
  const pending = deferred(); const original = env.crm;
  env.crm = (url, options = {}) => options.method === 'POST' ? (env.accessWrites.push({ url, body: options.body }), pending.promise) : original(url, options);
  const oldHandler = h.find(n => n.type === 'form').props.onSubmit;
  const first = oldHandler({ preventDefault() {} }); pending.reject(new CRMError(409, 'conflict'));
  await first; await h.flush(); await oldHandler({ preventDefault() {} }); assert.equal(env.accessWrites.length, 1);
});
