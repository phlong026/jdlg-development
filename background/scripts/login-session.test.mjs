import assert from 'node:assert/strict';
import fs from 'node:fs';
import test from 'node:test';
import vm from 'node:vm';
import { createRequire } from 'node:module';
import ts from 'typescript';

const require = createRequire(import.meta.url);
const source = fs.readFileSync(new URL('../src/pages/login.tsx', import.meta.url), 'utf8');
const javascript = ts.transpileModule(source, {
  compilerOptions: {
    target: ts.ScriptTarget.ES2020,
    module: ts.ModuleKind.CommonJS,
    jsx: ts.JsxEmit.ReactJSX,
    esModuleInterop: true,
  },
}).outputText;

const settle = () => new Promise(resolve => setImmediate(resolve));
const descendants = node => Array.isArray(node)
  ? node.flatMap(descendants)
  : node && typeof node === 'object'
    ? [node, ...descendants(node.props?.children)]
    : [];
const textOf = node => Array.isArray(node)
  ? node.map(textOf).join('')
  : node == null || typeof node === 'boolean'
    ? ''
    : typeof node === 'object'
      ? textOf(node.props?.children)
      : String(node);

async function loginPage(overrides = {}) {
  const state = [];
  const effects = [];
  const cleanups = new Map();
  const navigations = [];
  const loginCalls = [];
  const removed = [];
  const warnings = [];
  let cursor = 0;
  let tree;
  let dirty = false;
  const session = overrides.session || { loading: false, error: '', user: null, refresh() {} };
  const storage = overrides.storage || {
    removeItem(key) { removed.push(key); },
  };
  const react = {
    useState(initial) {
      const index = cursor++;
      if (!(index in state)) state[index] = typeof initial === 'function' ? initial() : initial;
      return [state[index], next => {
        const value = typeof next === 'function' ? next(state[index]) : next;
        dirty ||= !Object.is(value, state[index]);
        state[index] = value;
      }];
    },
    useEffect(effect, deps) {
      const index = cursor++;
      if (!(index in state) || deps.some((dep, i) => dep !== state[index][i])) {
        state[index] = deps;
        effects.push({ index, effect });
      }
    },
  };
  const module = { exports: {} };
  vm.runInNewContext(javascript, {
    module,
    exports: module.exports,
    require(name) {
      if (name === 'react') return react;
      if (name === 'react/jsx-runtime') return require(name);
      if (name === 'react-icons/fi') return new Proxy({}, { get: () => () => null });
      if (name === 'react-router-dom') return {
        Navigate: props => { navigations.push(props); return null; },
        useNavigate: () => (to, options) => navigations.push({ to, ...options }),
      };
      if (name === '../api/auth') return {
        login: async payload => {
          loginCalls.push(JSON.parse(JSON.stringify(payload)));
          return overrides.loginResult || { id: 1, username: 'admin', scope: 'platform_admin' };
        },
      };
      if (name === '../component/ManagementSession') return { useManagementSession: () => session };
      if (name === '../config/api') return { API_BASE_URL: '/api' };
      if (name.endsWith('.scss')) return {};
      throw new Error(`Unexpected dependency: ${name}`);
    },
    AbortController,
    URLSearchParams,
    fetch: async () => ({ json: async () => ({ code: 0, data: { enabled: false } }) }),
    console: { warn: (...args) => warnings.push(args) },
    window: {
      localStorage: storage,
      location: { search: '', pathname: '/admin/login' },
      history: { replaceState() {} },
    },
  });
  const page = {
    navigations,
    loginCalls,
    removed,
    warnings,
    render() {
      cursor = 0;
      dirty = false;
      tree = module.exports.default();
      return tree;
    },
    nodes: () => descendants(tree),
    find(predicate, message = 'Expected UI element') {
      const found = page.nodes().find(predicate);
      assert.ok(found, message);
      return found;
    },
    input(id) { return page.find(node => node.type === 'input' && node.props.id === id, `Expected input ${id}`); },
    checkbox() { return page.find(node => node.type === 'input' && node.props.type === 'checkbox', 'Expected remember checkbox'); },
    form() { return page.find(node => node.type === 'form', 'Expected login form'); },
    text: () => textOf(tree),
    async flush() {
      for (let pass = 0; pass < 20; pass++) {
        page.render();
        for (const { index, effect } of effects.splice(0)) {
          cleanups.get(index)?.();
          cleanups.set(index, effect());
        }
        await settle();
        if (!dirty && effects.length === 0) return page;
      }
      throw new Error('Login fixture did not settle');
    },
  };
  await page.flush();
  return page;
}

test('remember option asks the server for a persistent session without storing credentials', async () => {
  const page = await loginPage();
  assert.match(page.text(), /28 天内保持登录/);
  page.input('login-account').props.onChange({ target: { value: ' admin ' } });
  page.input('login-password').props.onChange({ target: { value: 'secret' } });
  page.checkbox().props.onChange({ target: { checked: true } });
  page.render();
  await page.form().props.onSubmit({ preventDefault() {} });
  assert.deepEqual(page.loginCalls, [{ username: 'admin', password: 'secret', remember_me: true }]);
  assert.deepEqual(page.navigations.at(-1), { to: '/dashboard', replace: true });
});

test('unchecked login requests the ordinary session lifetime', async () => {
  const page = await loginPage({ loginResult: { id: 2, username: 'sales', scope: 'crm_only' } });
  page.input('login-account').props.onChange({ target: { value: 'sales' } });
  page.input('login-password').props.onChange({ target: { value: 'secret' } });
  page.render();
  await page.form().props.onSubmit({ preventDefault() {} });
  assert.deepEqual(page.loginCalls, [{ username: 'sales', password: 'secret', remember_me: false }]);
  assert.deepEqual(page.navigations.at(-1), { to: '/customer-management', replace: true });
});

test('existing sessions skip the login form and preserve account scope', async () => {
  const admin = await loginPage({ session: { loading: false, error: '', user: { id: 1, scope: 'platform_admin' }, refresh() {} } });
  assert.deepEqual(admin.nodes()[0].props, { to: '/dashboard', replace: true });
  assert.doesNotMatch(admin.text(), /欢迎登录/);
  const crm = await loginPage({ session: { loading: false, error: '', user: { id: 2, scope: 'crm_only' }, refresh() {} } });
  assert.deepEqual(crm.nodes()[0].props, { to: '/customer-management', replace: true });
});

test('session checks show progress, while network errors keep retry and password login available', async () => {
  const checking = await loginPage({ session: { loading: true, error: '', user: null, refresh() {} } });
  assert.match(checking.text(), /正在核对登录状态/);
  assert.doesNotMatch(checking.text(), /欢迎登录/);
  let retries = 0;
  const failed = await loginPage({ session: { loading: false, error: '会话检查失败', user: null, refresh() { retries++; } } });
  assert.match(failed.text(), /会话检查失败/);
  assert.match(failed.text(), /欢迎登录/);
  failed.find(node => node.type === 'button' && textOf(node) === '重新检查').props.onClick();
  assert.equal(retries, 1);
});

test('legacy plaintext credentials are removed and storage failures do not block login', async () => {
  const normal = await loginPage();
  assert.deepEqual(normal.removed, ['background_remembered_login']);
  const failedStorage = await loginPage({ storage: { removeItem() { throw new Error('blocked'); } } });
  assert.match(failedStorage.text(), /欢迎登录/);
  assert.equal(failedStorage.warnings.length, 1);
});
