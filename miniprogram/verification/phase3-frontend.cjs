'use strict';
// Runs native WeChat pages against explicit, in-memory fixtures; never publishes or calls a paid provider.
const fs = require('node:fs');
const path = require('node:path');
const { execFileSync } = require('node:child_process');
const assert = require('node:assert/strict');
const project = path.resolve(__dirname, '..');
const output = process.env.PHASE3_UI_EVIDENCE_DIR || path.resolve(project, '../docs/phase3/evidence/frontend');
const cli = process.env.WECHATIDE_CLI || '/Applications/wechatwebdevtools.app/Contents/MacOS/wechatide';
fs.mkdirSync(output, { recursive: true });
const steps = [];
const installedMocks = [];
let storageChanged = false;
let completed = false;
let failure = '';
const cleanupErrors = [];
function call(tool, args = {}) {
  const argv = ['-c', 'Codex', tool, '--project', project];
  for (const [key, value] of Object.entries(args)) argv.push('--' + key, typeof value === 'object' ? JSON.stringify(value) : String(value));
  let raw;
  try { raw = execFileSync(cli, argv, { encoding: 'utf8', timeout: 45000 }); }
  catch (error) { throw new Error(tool + ': ' + (error.stdout ? String(error.stdout).trim() : error.message)); }
  const result = JSON.parse(raw.slice(raw.indexOf('{')));
  if (!result.ok) throw new Error(tool + ': ' + result.message);
  return result.result;
}
const evaluate = fn => call('automation_evaluate', { 'fn-source': fn }).result.result;
const mock = (method, fn) => { call('automation_wx_api', { action: 'mock', method, 'function-declaration': fn }); installedMocks.push(method); };
const navigate = url => call('automation_navigate', { action: 'navigateTo', url });
const patch = value => call('automation_page_action', { action: 'setData', patch: JSON.stringify(value) });
const data = field => call('automation_page_action', { action: 'getData', 'data-path': field });
const tap = selector => call('automation_element_action', { action: 'tap', selector, 'wait-for-selector': selector });
const input = (selector, value) => call('automation_element_action', { action: 'input', selector, value });
const screenshot = name => call('simulator_screenshot', { path: path.join(output, name + '.jpg') });
function passed(name) { steps.push({ name, status: 'passed' }); process.stdout.write('PASS ' + name + '\n'); }
const fixtureFunction = function (options) {
  const app = getApp();
  app.phase3UIFixture = app.phase3UIFixture || { content_id: 'fixture-content-01', title: '庭院空间与归家动线', revision: 1, version: 0, status: 'draft', type: 'article', body_format: 'article_v2', body: JSON.stringify({ schema: 1, blocks: [{ kind: 'paragraph', text: '从入口到庭院，整理方案的空间层次与归家动线。' }] }), owner_id: '界面验证资料', rights: 'own', media: [], source_note: '隔离界面验证资料' };
  const content = app.phase3UIFixture;
  const url = String(options.url || '');
  let result = {};
  if (url.includes('/workbench/context')) result = { enabled: true, can_manage: true, owners: ['界面验证资料'], media_upload_configured: true, publish_enabled: true };
  else if (url.includes('/imports/capabilities')) result = { enabled: true, platforms: [{ platform: 'wechat_article', available: true }, { platform: 'channels', available: true }, { platform: 'xiaohongshu', available: true }] };
  else if (url.endsWith('/imports')) result = options.method === 'POST' ? { import_id: 'fixture-import', status: 'ready', content_id: content.content_id } : { items: [{ import_id: 'fixture-import', platform: 'wechat_article', status: 'ready', content_id: content.content_id }, { import_id: 'fixture-failed', platform: 'xiaohongshu', status: 'failed', error_code: 'invalid_xhs_response', can_retry: true }] };
  else if (url.includes('/workbench/media')) result = { items: [] };
  else if (url.endsWith('/preview')) result = { content: Object.assign({}, content), preview_token: 'fixture-preview' };
  else if (url.endsWith('/publish')) { content.version += 1; content.revision += 1; content.status = 'published'; result = { content: Object.assign({}, content) }; }
  else if (url.includes('/workbench/contents')) {
    if (options.method === 'POST' || options.method === 'PATCH') { Object.assign(content, options.data, { revision: content.revision + 1 }); result = Object.assign({}, content); }
    else if (url.includes(content.content_id)) result = Object.assign({}, content);
    else result = { items: [Object.assign({}, content), Object.assign({}, content, { content_id: 'fixture-content-02', title: '新中式乡墅的庭院与入口', status: 'published' }), Object.assign({}, content, { content_id: 'fixture-content-03', title: '庭院空间视频资料', type: 'article', media: [{ kind: 'video', media_id: 'fixture-video' }], status: 'draft' })] };
  } else if (url.includes('/tracking/config')) result = { enabled: false, content_enabled: true };
  else if (url.includes('/contents/') && url.includes('/versions/')) result = Object.assign({}, content);
  else if (url.includes('/ai/pricing')) result = { prices: { ai_chat_single: 1 } };
  else {
    app.phase3UIUnknownRequests = (app.phase3UIUnknownRequests || []).concat(url);
    return { statusCode: 501, data: { code: 501, msg: '未配置此界面验收请求' }, header: {} };
  }
  return { statusCode: 200, data: { code: 0, data: result }, header: {} };
};
// Paste this source into the selected native console if the IDE automation bridge
// targets a different runtime. The restore function keeps the override reversible.
if (process.argv.includes('--console-source')) {
  process.stdout.write(`(function(){
    const app=getApp();
    if(app.phase3UINativeRestore) return 'fixtures already active';
    const request=wx.request, clipboard=wx.getClipboardData, token=wx.getStorageSync('token');
    const respond=${fixtureFunction.toString()};
    wx.request=function(options){
      const result=respond(options);
      Promise.resolve().then(function(){if(options.success)options.success(result);if(options.complete)options.complete(result);});
      return {abort:function(){}};
    };
    wx.getClipboardData=function(options){const result={data:'https://mp.weixin.qq.com/s/fixture'};if(options.success)options.success(result);if(options.complete)options.complete(result);};
    wx.setStorageSync('token','phase3-ui-fixture');
    app.phase3UINativeRestore=function(){
      wx.request=request;wx.getClipboardData=clipboard;
      if(token)wx.setStorageSync('token',token);else wx.removeStorageSync('token');
      delete app.phase3UIFixture;delete app.phase3UIUnknownRequests;delete app.phase3UINativeRestore;
      return 'native fixtures restored';
    };
    return 'isolated native fixtures active';
  })();\n`);
  process.exit(0);
}
try {
  assert.equal(evaluate('function(){return typeof wx.getStorageSync;}'), 'function', 'native storage API must be ready');
  mock('request', fixtureFunction.toString());
  mock('getClipboardData', 'function(){return {data:"https://mp.weixin.qq.com/s/fixture"};}');
  evaluate('function(){getApp().phase3UIOriginalToken=wx.getStorageSync("token");wx.setStorageSync("token","phase3-ui-fixture");return true;}');
  storageChanged = true;
  call('automation_navigate', { action: 'switchTab', url: '/pages/my/my' });
  patch({ hasLoginToken: true, userProfile: { nickname: '界面验证账号', identityType: '设计师' }, certStatus: 'approved', vipLevelText: '普通', stonesDisplay: '--' });
  screenshot('01-my-top');
  call('automation_viewport_action', { action: 'pageScrollTo', 'scroll-top': 650 });
  screenshot('02-my-services');
  passed('my page renders account, workbench and services');
  navigate('/pages/contentworkbench/contentworkbench');
  call('automation_page_action', { action: 'querySelectorAll', selector: '.content-row', 'wait-for-selector': '.content-row' });
  assert.equal(call('automation_page_action', {action: 'querySelectorAll', selector: '.empty-state'}).elements.length, 0, 'loaded rows must not show the empty state');
  screenshot('03-content-studio');
  tap('.creation-tool:nth-child(2)');
  call('automation_runtime_info', { action: 'currentPage' });
  tap('.paste-button');
  const pasted = data('importURL');
  assert.ok(JSON.stringify(pasted).includes('mp.weixin.qq.com/s/fixture'));
  screenshot('04-content-import');
  passed('native import entry and clipboard button');
  navigate('/pages/contentedit/contentedit');
  input('input[data-field="title"]', '原生交互验证图文');
  input('textarea[data-index="0"]', '这段正文通过原生输入事件写入，仅用于隔离界面验证。');
  tap('.editor-actions .secondary');
  assert.ok(JSON.stringify(data('id')).includes('fixture-content-01'));
  tap('.editor-actions .primary');
  assert.ok(JSON.stringify(data('preview')).includes('fixture-preview'));
  screenshot('05-content-editor');
  tap('.publish-button');
  assert.ok(JSON.stringify(data('status')).includes('published'));
  tap('.published-card button');
  call('automation_page_action', { action: 'querySelector', selector: '.reader', 'wait-for-selector': '.reader' });
  screenshot('06-published-reader');
  passed('native input, save, preview, publish and reader navigation');
  navigate('/pages/contentwriting/contentwriting');
  input('.facts-panel input', '新中式庭院设计说明');
  input('.facts-panel textarea', '设计风格为新中式，重点说明从门厅到庭院的空间层次。');
  patch({ result: '方案以门厅与庭院的空间层次为线索，组织归家的动线与视线。' });
  screenshot('07-ai-writing');
  tap('.writing-footer .primary');
  assert.ok(JSON.stringify(data('title')).includes('新中式庭院设计说明'));
  passed('AI writing result reaches new editor through native EventChannel');
  completed = true;
} catch (error) {
  failure = error.message;
  throw error;
} finally {
  if (storageChanged) {
    try { evaluate('function(){var app=getApp();if(app.phase3UIOriginalToken){wx.setStorageSync("token",app.phase3UIOriginalToken);}else{wx.removeStorageSync("token");}delete app.phase3UIOriginalToken;delete app.phase3UIFixture;delete app.phase3UIUnknownRequests;return true;}'); }
    catch (error) { cleanupErrors.push('Storage restoration failed: ' + error.message); process.exitCode = 1; }
  }
  for (const method of installedMocks.reverse()) {
    try { call('automation_wx_api', { action: 'restore', method }); } catch (error) { cleanupErrors.push('Restore failed: ' + method + ': ' + error.message); process.exitCode = 1; }
  }
  cleanupErrors.forEach(message => process.stderr.write(message + '\n'));
  fs.writeFileSync(path.join(output, 'native-verification.json'), JSON.stringify({ status: completed && !cleanupErrors.length ? 'passed' : 'incomplete', cleanupErrors, environment: 'WeChat DevTools, isolated in-memory fixtures', production: false, modelCalled: false, steps, failure }, null, 2) + '\n');
}
