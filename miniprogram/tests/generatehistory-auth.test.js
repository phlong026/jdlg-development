const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const ts = require('./report-runtime')('typescript');
const root = path.join(__dirname, '../miniprogram');

function harness(extension, expired = false, initialStorage = {}) {
  const storage = {
    token: 'valid-session',
    userInfo: { id: 40 },
    device_fingerprint: 'bound-device',
    ...initialStorage,
  };
  const requests = [], navigation = [], storageReads = [], storageRemovals = [];
  let definition;
  const auth = {
    generateRequestParams(token, _body, apiPath, device) {
      return { token, 'token-signature': 'signature', sin: 'sin', 'md5-signature': apiPath, pass: device || '', tm: 'timestamp' };
    },
    paramsToHeaders: params => params,
  };
  const wx = {
    getStorageSync: key => {
      storageReads.push(key);
      return storage[key];
    },
    removeStorageSync: key => {
      storageRemovals.push(key);
      delete storage[key];
    },
    showModal(options) { options.success?.({ confirm: true, cancel: false }); },
    showToast() {},
    stopPullDownRefresh() {},
    navigateTo: options => navigation.push(options.url),
    request(options) {
      const apiPath = options.url.replace('https://api.jiadilingguang.com', '');
      const valid = !expired && options.header['md5-signature'] === apiPath && options.header.pass === storage.device_fingerprint;
      const result = valid ? { statusCode: 200, data: { code: 0, data: { tasks: [], total: 0 } } }
        : { statusCode: 401, data: { code: 401, msg: '参数不合法' } };
      requests.push({ apiPath, method: options.method, status: result.statusCode });
      options.success?.(result);
      options.complete?.(result);
      return {};
    },
  };
  const guardModule = { exports: {} };
  vm.runInNewContext(fs.readFileSync(path.join(root, 'utils/profile-request-guard.js'), 'utf8'), {
    module: guardModule, exports: guardModule.exports, wx, require: () => auth,
    getCurrentPages: () => [{ route: 'pages/generatehistory/generatehistory' }],
  });
  guardModule.exports.install();
  let code = fs.readFileSync(path.join(root, `pages/generatehistory/generatehistory.${extension}`), 'utf8');
  if (extension === 'ts') code = ts.transpileModule(code, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 } }).outputText;
  vm.runInNewContext(code, {
    exports: {}, wx, console: { error() {} },
    require: name => name.endsWith('/parameter') ? auth : require(path.join(root, 'pages/generatehistory/currentPageSelection.js')),
    Page: value => { definition = value; },
  });
  const page = { ...definition, data: JSON.parse(JSON.stringify(definition.data)), setData(patch, callback) { Object.assign(this.data, patch); callback?.(); } };
  page.syncManageSelectionAfterListChange = () => {};
  return { page, storage, requests, navigation, storageReads, storageRemovals };
}

for (const extension of ['ts', 'js']) {
  for (const scenario of [
    { name: 'first page', run: page => page.loadHistory(), expected: '?page=1&page_size=10' },
    { name: 'next page', run: page => page.loadMore(), expected: '?page=2&page_size=10' },
    { name: 'filtered page', run: page => { page.data.selectedToolId = '17'; return page.loadHistory(); }, expected: '?page=1&page_size=10&tool_id=17' },
    { name: 'filtered next page', run: page => { page.data.selectedToolId = '17'; return page.loadMore(); }, expected: '?page=2&page_size=10&tool_id=17' },
    { name: 'refresh', run: page => { page.data.page = 3; page.onPullDownRefresh(); }, expected: '?page=1&page_size=10' },
  ]) {
    test(`${extension}: ${scenario.name} keeps a valid session through the installed request guard`, async () => {
      const h = harness(extension);
      await scenario.run(h.page);
      await new Promise(resolve => setImmediate(resolve));
      assert.equal(h.requests.length, 1);
      assert.equal(h.requests[0].apiPath, '/api/v1/miniprogram/user/tasks' + scenario.expected);
      assert.equal(h.requests[0].status, 200);
      assert.equal(h.storage.token, 'valid-session');
      assert.equal(h.navigation.length, 0);
    });
  }
  test(`${extension}: tools and deletion carry the current device binding`, async () => {
    const h = harness(extension);
    await h.page.loadAITools();
    await h.page.doDeleteByTaskNo('test-task');
    assert.deepEqual(h.requests.map(x => x.status), [200, 200]);
    assert.equal(h.storage.token, 'valid-session');
    assert.equal(h.navigation.length, 0);
  });
  test(`${extension}: genuinely expired session still opens the login gate`, async () => {
    const h = harness(extension, true);
    await assert.rejects(h.page.loadHistory());
    assert.equal(h.storage.token, undefined);
    assert.equal(h.navigation[0], '/pages/login/login?from=profile-gate');
  });

  test(`${extension}: local draft is isolated to the current account`, () => {
    const accountADraft = { promptText: '账号 A 的草稿' };
    const accountBDraft = { promptText: '账号 B 的草稿' };
    const h = harness(extension, false, {
      'jdlg_ai_generate_local_draft_v1:40': accountADraft,
      'jdlg_ai_generate_local_draft_v1:41': accountBDraft,
    });

    h.page.loadLocalDraft();

    assert.equal(h.page.data.hasLocalDraft, true);
    assert.equal(h.page.data.localDraft.promptText, accountADraft.promptText);
    assert.equal(h.storageReads.includes('jdlg_ai_generate_local_draft_v1:40'), true);
    assert.equal(h.storageReads.includes('jdlg_ai_generate_local_draft_v1:41'), false);
    assert.equal(h.storageReads.includes('jdlg_ai_generate_local_draft_v1'), false);
  });

  test(`${extension}: unauthenticated and mismatched sessions never read a draft`, () => {
    for (const initialStorage of [
      {
        token: '',
        userInfo: { id: 40 },
        'jdlg_ai_generate_local_draft_v1:40': { promptText: '不应读取' },
      },
      {
        token: 'current-session',
        userInfo: { id: 40, token: 'another-session' },
        'jdlg_ai_generate_local_draft_v1:40': { promptText: '不应读取' },
      },
      {
        token: 'current-session',
        userInfo: { id: 0, token: 'current-session' },
        'jdlg_ai_generate_local_draft_v1:0': { promptText: '不应读取' },
      },
      {
        token: 'current-session',
        userInfo: { id: 40.5, token: 'current-session' },
        'jdlg_ai_generate_local_draft_v1:40.5': { promptText: '不应读取' },
      },
    ]) {
      const h = harness(extension, false, initialStorage);
      h.page.loadLocalDraft();
      assert.equal(h.page.data.hasLocalDraft, false);
      assert.equal(h.storageReads.includes('jdlg_ai_generate_local_draft_v1:40'), false);
      assert.equal(h.storageReads.includes('jdlg_ai_generate_local_draft_v1'), false);
    }
  });

  test(`${extension}: legacy global draft is neither read nor migrated`, () => {
    const legacyDraft = { promptText: '旧全局草稿' };
    const h = harness(extension, false, {
      jdlg_ai_generate_local_draft_v1: legacyDraft,
    });

    h.page.loadLocalDraft();

    assert.equal(h.page.data.hasLocalDraft, false);
    assert.equal(h.storageReads.includes('jdlg_ai_generate_local_draft_v1'), false);
    assert.equal(h.storage.jdlg_ai_generate_local_draft_v1, legacyDraft);
    assert.equal(h.storage['jdlg_ai_generate_local_draft_v1:40'], undefined);
  });

  test(`${extension}: continue and discard only act on the current account draft`, () => {
    const h = harness(extension, false, {
      'jdlg_ai_generate_local_draft_v1:40': { promptText: '账号 A 的草稿' },
      'jdlg_ai_generate_local_draft_v1:41': { promptText: '账号 B 的草稿' },
    });

    h.page.loadLocalDraft();
    h.page.onContinueLocalDraft();
    h.page.onDiscardLocalDraft();

    assert.equal(h.navigation.at(-1), '/pages/aigenerate/aigenerate?draft=1');
    assert.deepEqual(h.storageRemovals, ['jdlg_ai_generate_local_draft_v1:40']);
    assert.equal(h.storage['jdlg_ai_generate_local_draft_v1:40'], undefined);
    assert.deepEqual(h.storage['jdlg_ai_generate_local_draft_v1:41'], { promptText: '账号 B 的草稿' });
  });

  test(`${extension}: switching accounts invalidates a previously loaded draft before continuing`, () => {
    const h = harness(extension, false, {
      'jdlg_ai_generate_local_draft_v1:40': { promptText: '账号 A 的草稿' },
    });

    h.page.loadLocalDraft();
    h.storage.token = 'account-b-session';
    h.storage.userInfo = { id: 41, token: 'account-b-session' };
    h.page.onContinueLocalDraft();

    assert.equal(h.page.data.hasLocalDraft, false);
    assert.equal(h.navigation.length, 0);
    assert.deepEqual(h.storage['jdlg_ai_generate_local_draft_v1:40'], { promptText: '账号 A 的草稿' });
  });
}
