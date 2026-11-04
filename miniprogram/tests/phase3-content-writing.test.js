'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const root = path.join(__dirname, '../miniprogram');

function harness(name) {
  let definition;
  const calls = [], listeners = {}, channel = { on(name, fn) { listeners[name] = fn; }, off() {}, emit(name, value) { calls.push({ event: name, value }); } };
  let token = 'session-one', stream, chunk;
  const apiStub = { writingPrompts: async () => { throw new Error('offline'); } };
  const aiStreamStub = { startAIStream: () => ({ abort() {} }) };
  const wx = {
    hideShareMenu() {}, getStorageSync: () => token, pageScrollTo() {}, showToast(value) { calls.push({ toast: value }); },
    navigateTo(value) { calls.push({ navigate: value }); }, navigateBack(value) { calls.push({ back: value }); },
    request(options) {
      if (options.url.includes('/ai/pricing')) { options.success({ statusCode: 200, data: { code: 0, data: { prices: { ai_chat_single: 12 } } } }); return {}; }
      stream = options; calls.push({ request: options });
      return { onChunkReceived(fn) { chunk = fn; }, abort() {} };
    },
  };
  vm.runInNewContext(fs.readFileSync(path.join(root, `pages/${name}/${name}.js`), 'utf8'), {
    Page(value) { definition = value; }, wx, exports: {}, module: { exports: {} }, console,
    require(id) {
      if (id.endsWith('/ai-stream')) return { startAIStream: options => aiStreamStub.startAIStream(options) };
      if (id.endsWith('/phase3-api')) return apiStub;
      if (id.endsWith('/parameter')) return { generateRequestParams: () => ({}), paramsToHeaders: () => ({}) };
      if (id.endsWith('/deviceFingerprint')) return { getCachedDeviceFingerprint: () => '' };
      if (id.endsWith('/aiError')) return { sanitizeAIGenerationErrorMessage: value => value };
      if (id.endsWith('/promptReview')) return { blockUnsafeText: () => false };
      throw new Error(`unexpected require ${id}`);
    },
    setTimeout() {}, clearTimeout() {}, Promise, JSON, Object, String, Number, Date, Map, Set, Error, Uint8Array, ArrayBuffer,
    encodeURIComponent, decodeURIComponent, escape,
  });
  const page = Object.assign({}, definition, { data: JSON.parse(JSON.stringify(definition.data)), setData(values) { Object.assign(this.data, values); }, getOpenerEventChannel: () => channel });
  return {
    page, calls, listeners, channel, apiStub, aiStreamStub, token(value) { token = value; },
    chunk(text) { const bytes = Uint8Array.from(Buffer.from(text)); chunk({ data: bytes.buffer }); },
    bytes(value) { const bytes = Uint8Array.from(value); chunk({ data: bytes.buffer }); },
    success(statusCode = 200, data = '') { stream.success({ statusCode, data }); },
    fail() { stream.fail({ errMsg: 'network failure' }); },
  };
}
const tick = () => new Promise(resolve => setImmediate(resolve));

test('voice input degrades gracefully without the plugin and appends speech into facts', () => {
  const h = harness('contentwriting'); h.page.onLoad({}); h.page.onShow();
  // VM 环境无 requirePlugin：必须降级提示而不是抛错阻塞创作
  h.page.startVoiceInput();
  assert.match(h.page.data.error, /不可用/);
  // 注入伪识别器并绑定回调，走完整识别链路
  const fake = { start(options) { this.started = options; }, stop() {} };
  h.page.attachVoiceHandlers(fake);
  h.page._voiceManager = fake;
  h.page.setData({ error: '', facts: '占地120平' });
  h.page.startVoiceInput();
  assert.equal(fake.started.duration, 60000);
  assert.equal(fake.started.lang, 'zh_CN');
  fake.onStart();
  assert.equal(h.page.data.voiceActive, true);
  fake.onRecognize({ result: '三层框架' });
  assert.equal(h.page.data.voicePartial, '三层框架');
  fake.onStop({ result: '三层框架结构，带阁楼层。' });
  assert.equal(h.page.data.voiceActive, false);
  assert.equal(h.page.data.facts, '占地120平\n三层框架结构，带阁楼层。');
  // 追加超出输入框上限时截断到 6000
  h.page.setData({ facts: 'a'.repeat(5995) });
  fake.onStop({ result: 'bcd'.repeat(10) });
  assert.equal(h.page.data.facts.length, 6000);
  // 麦克风未授权与普通失败给出可行动文案
  fake.onError({ msg: 'operateWXData:fail permission denied' });
  assert.match(h.page.data.error, /麦克风/);
  fake.onError({ msg: 'network error' });
  assert.match(h.page.data.error, /语音识别失败/);
});

test('app.json declares the WechatSI speech plugin and wxml wires the voice button', () => {
  const appJson = fs.readFileSync(path.join(root, 'app.json'), 'utf8');
  assert.match(appJson, /"WechatSI"/);
  assert.match(appJson, /wx069ba97219f66d99/);
  const writingWxml = fs.readFileSync(path.join(root, 'pages/contentwriting/contentwriting.wxml'), 'utf8');
  assert.match(writingWxml, /bindtouchstart="startVoiceInput"/);
  assert.match(writingWxml, /bindtouchend="stopVoiceInput"/);
});

test('tab examples arrive from the server registry and never feed fabricated facts into inputs', async () => {
  // 兜底不携带业务示例：默认示例卡隐藏
  const offline = harness('contentwriting'); offline.page.onLoad({}); offline.page.onShow();
  assert.equal(offline.page.data.example, '', '本地兜底不得内置业务示例文案');
  assert.ok(offline.page.data.instruction.indexOf('方案设计说明') >= 0, '兜底仍带中性指令保证可用');

  // 服务端注册表下发示例与指令，随 Tab 联动
  const h = harness('contentwriting');
  h.apiStub.writingPrompts = async () => ({ prompts: [
    { key: 'design', label: '设计说明', heading: '方案设计说明', example: '服务端示例：三层框架结构与闽派立面。', instruction: '请撰写方案设计说明。面向专业读者。' },
    { key: 'promotion', label: '推广文案', heading: '作品推广文案', example: '服务端示例：老宅翻新故事。', instruction: '请撰写推广文案。短句为主。' },
  ] });
  h.page.onLoad({}); h.page.onShow();
  await new Promise(resolve => setImmediate(resolve));
  assert.ok(h.page.data.example.indexOf('服务端示例') === 0, '示例来自服务端注册表');
  h.page.selectType({ currentTarget: { dataset: { type: 'promotion' } } });
  assert.equal(h.page.data.exampleLabel, '推广文案');
  assert.ok(h.page.data.instruction.indexOf('短句为主') >= 0);
  assert.equal(h.page.data.facts, '', '示例是虚构事实，绝不能自动进入已知资料');

});

test('first show reuses the prompt request started by onLoad', async () => {
  const h = harness('contentwriting');
  let calls = 0;
  let finish;
  h.apiStub.writingPrompts = () => { calls += 1; return new Promise(resolve => { finish = resolve; }); };
  h.page.onLoad({});
  h.page.onShow();
  assert.equal(calls, 1);
  finish({ prompts: [] });
  await tick();
  h.page.onShow();
  assert.equal(calls, 1);
});

test('prompt registry drives generation while the anti-fabrication clause stays client-side', async () => {
  const h = harness('contentwriting');
  h.apiStub.writingPrompts = async () => ({ prompts: [
    { key: 'design', label: '设计说明', heading: '服务端设计标题', example: '示例', instruction: '服务端定制指令。' },
  ] });
  h.page.onLoad({}); h.page.onShow();
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(h.page.data.heading, '方案设计说明');
  h.page.setData({ title: '庭院方案', facts: '保留现有大树，入口朝南。' });
  h.page.generateWriting();
  const navigation = h.calls.find(call => call.navigate).navigate;
  navigation.success({ eventChannel: h.channel });
  const task = h.calls.find(call => call.event === 'contentWritingTask');
  assert.match(task.value.text, /服务端定制指令/, '生成提示词首句来自服务端注册表');
  assert.match(task.value.text, /不得编造面积/, '防臆造条款必须由客户端常量注入且不可被配置移除');

  // 注册表不可用时静默回退本地中性指令，不报错不打扰
  const offline = harness('contentwriting'); offline.page.onLoad({}); offline.page.onShow();
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(offline.page.data.error, '');
  assert.match(offline.page.data.instruction, /请撰写/);

  // 包内不得残留可运营的完整业务提示词（seed 措辞只存在于服务端默认值与迁移里）
  const pageSource = fs.readFileSync(path.join(root, 'pages/contentwriting/contentwriting.js'), 'utf8');
  for (const seed of ['面向专业读者', '面向业主', '适合朋友圈与社群转发', '新闽派风格，坡屋顶配合浅色真石漆']) {
    assert.ok(pageSource.indexOf(seed) < 0, `业务提示词措辞不得硬编码在小程序包内：${seed}`);
  }
});

test('writing page requires known facts and never triggers model requests or stores private content', () => {
  const h = harness('contentwriting'); h.page.onLoad({}); h.page.onShow();
  h.page.generateWriting();
  assert.equal(h.calls.some(call => call.navigate || call.request), false);
  assert.ok(h.page.data.error);
  h.page.setData({ title: '庭院方案', facts: '保留现有大树，入口朝南。' });
  h.page.generateWriting();
  const navigation = h.calls.find(call => call.navigate).navigate;
  assert.match(navigation.url, /chat\?mode=chat&source=content-writing/);
  navigation.success({ eventChannel: h.channel });
  const task = h.calls.find(call => call.event === 'contentWritingTask');
  assert.match(task.value.text, /保留现有大树/);
  assert.match(task.value.text, /不得编造/);
  assert.equal(h.calls.some(call => call.request), false);
});

test('accepted writing is editable and prefills an unpublished content editor through its event channel', () => {
  const h = harness('contentwriting'); h.page.onLoad({}); h.page.onShow();
  h.page.setData({ title: '庭院方案', facts: '南向入口' }); h.page.generateWriting();
  const chat = h.calls.find(call => call.navigate).navigate;
  chat.events.contentWritingResult({ text: '原生成文案' });
  assert.equal(h.page.data.result, '原生成文案');
  h.page.editResult({ detail: { value: '用户修改后的说明' } });
  h.page.saveToDraft();
  const navigation = h.calls.filter(call => call.navigate).at(-1).navigate;
  assert.equal(navigation.url, '/pages/contentedit/contentedit');
  navigation.success({ eventChannel: h.channel });
  const prefill = h.calls.find(call => call.event === 'prefillContent');
  assert.equal(prefill.value.text, '用户修改后的说明');
  assert.equal(prefill.value.title, '庭院方案');
});

test('writing result from a previous account is rejected', () => {
  const h = harness('contentwriting'); h.page.onLoad({}); h.page.onShow();
  h.page.setData({ facts: '南向入口' }); h.page.generateWriting();
  const chat = h.calls.find(call => call.navigate).navigate;
  h.token('session-two'); chat.events.contentWritingResult({ text: '上一账号的文案' });
  assert.equal(h.page.data.result, '');
  assert.ok(h.page.data.error);
});

test('only exact writing source receives a task; it only prefills user input without spending', async () => {
  const ordinary = harness('chat'); ordinary.page.onLoad({ mode: 'chat', source: 'other' });
  assert.equal(ordinary.listeners.contentWritingTask, undefined);
  const h = harness('chat'); h.page.onLoad({ mode: 'chat', source: 'content-writing' }); await tick();
  h.listeners.contentWritingTask({ text: '请根据真实事实生成设计说明' });
  assert.equal(h.page.data.inputText, '请根据真实事实生成设计说明');
  assert.equal(h.page.data.chatCost, 12);
  assert.equal(h.calls.some(call => call.request), false);
  h.page.adoptWriting();
  assert.equal(h.calls.some(call => call.event === 'contentWritingResult'), false);
});

test('only a successful completed nonempty AI stream can be adopted, even with split SSE chunks', async () => {
  const h = harness('chat'); h.page.onLoad({ mode: 'chat', source: 'content-writing' });
  h.listeners.contentWritingTask({ text: '为南向庭院写说明' });
  await h.page.sendChatMessage('为南向庭院写说明');
  h.chunk('data: {"type":"content","content":"南向'); h.chunk('入口。"}\n\ndata: {"type":"done"}\n\n');
  h.page.adoptWriting();
  assert.equal(h.calls.some(call => call.event === 'contentWritingResult'), false);
  h.success(); h.page.adoptWriting();
  const result = h.calls.find(call => call.event === 'contentWritingResult');
  assert.equal(result.value.text, '南向入口。');
  assert.ok(h.calls.some(call => call.back));
});

test('partial stream, network failure, balance failure and SSE error cannot be adopted', async () => {
  for (const failure of ['partial', 'network', 'balance', 'sse']) {
    const h = harness('chat'); h.page.onLoad({ mode: 'chat', source: 'content-writing' });
    await h.page.sendChatMessage('请生成'); h.chunk('data: {"type":"content","content":"不完整文案"}\n\n');
    if (failure === 'network') h.fail();
    else if (failure === 'balance') h.success(402, { data: { required: 12, current: 0 } });
    else if (failure === 'sse') { h.chunk('data: {"type":"error","error":"生成中断"}\n\ndata: {"type":"done"}\n\n'); h.success(); }
    else h.success();
    h.page.adoptWriting();
    assert.equal(h.calls.some(call => call.event === 'contentWritingResult'), false, failure);
  }
});

test('ordinary chat and a changed writing account cannot export generated content', async () => {
  for (const source of ['other', 'content-writing']) {
    const h = harness('chat'); h.page.onLoad({ mode: 'chat', source });
    await h.page.sendChatMessage('请生成'); h.chunk('data: {"type":"content","content":"真实生成"}\n\ndata: {"type":"done"}\n\n'); h.success();
    if (source === 'content-writing') h.token('session-two');
    h.page.adoptWriting();
    assert.equal(h.calls.some(call => call.event === 'contentWritingResult'), false);
  }
});

test('a multibyte character crossing network chunks is preserved; empty completion cannot be adopted', async () => {
  const h = harness('chat'); h.page.onLoad({ mode: 'chat', source: 'content-writing' });
  await h.page.sendChatMessage('请生成');
  const payload = Buffer.from('data: {"type":"content","content":"庭院"}\n\ndata: {"type":"done"}\n\n');
  const offset = payload.indexOf(Buffer.from('庭')) + 1;
  h.bytes(payload.subarray(0, offset)); h.bytes(payload.subarray(offset)); h.success(); h.page.adoptWriting();
  assert.equal(h.calls.find(call => call.event === 'contentWritingResult').value.text, '庭院');
  const empty = harness('chat'); empty.page.onLoad({ mode: 'chat', source: 'content-writing' });
  await empty.page.sendChatMessage('请生成'); empty.chunk('data: {"type":"done"}\n\n'); empty.success(); empty.page.adoptWriting();
  assert.equal(empty.calls.some(call => call.event === 'contentWritingResult'), false);
});

test('navigation failure and returning from chat preserve user-entered facts and result', () => {
  const h = harness('contentwriting'); h.page.onLoad({}); h.page.onShow();
  h.page.setData({ facts: '实际已知资料', result: '手动编辑的文案' }); h.page.generateWriting();
  const chat = h.calls.find(call => call.navigate).navigate; chat.fail();
  assert.equal(h.page.data.navigating, false);
  h.page.onShow();
  assert.equal(h.page.data.facts, '实际已知资料');
  assert.equal(h.page.data.result, '手动编辑的文案');
});

test('writing opened by the editor returns to that same editor instead of opening a second instance', () => {
  const h = harness('contentwriting'); h.page.onLoad({ source: 'editor' }); h.page.onShow();
  h.page.setData({ title: '庭院方案', result: '用户核对后的设计说明' });
  h.page.saveToDraft();
  assert.equal(h.calls.some(call => call.navigate), false);
  const prefill = h.calls.find(call => call.event === 'prefillContent');
  assert.equal(prefill.value.title, '庭院方案');
  assert.equal(prefill.value.text, '用户核对后的设计说明');
  assert.ok(h.calls.some(call => call.back));
});

test('the actual send button includes the current user message once and preserves a genuine repeated historical question', async () => {
  const h = harness('chat'); h.page.onLoad({ mode: 'chat', source: 'content-writing' });
  h.page.setData({ messages: [
    { id: 'history-user', role: 'user', content: '请写庭院说明', time: '09:01' },
    { id: 'history-ai', role: 'ai', content: '上一轮说明', time: '09:02' },
  ], inputText: '请写庭院说明' });
  h.page.onSendMessage(); await tick();
  const requests = h.calls.filter(call => call.request);
  assert.equal(requests.length, 1);
  const messages = requests[0].request.data.messages;
  assert.equal(messages.length, 3);
  assert.equal(messages.filter(item => item.role === 'user' && item.content === '请写庭院说明').length, 2);
  assert.equal(messages[1].content, '上一轮说明');
  assert.equal(messages[2].content, '请写庭院说明');
});

for (const boundary of ['stop', 'unload', 'account-change', 'storage-change']) {
  test(`quick generation ignores late callbacks after ${boundary}`, () => {
    const h = harness('contentwriting'); let callbacks, aborted = 0;
    h.aiStreamStub.startAIStream = options => { callbacks = options; return { abort() { aborted++; options.onError('abort callback'); } }; };
    h.page.onLoad({}); h.page.onShow(); h.page.setData({ facts: '真实资料' }); h.page.quickGenerate();
    if (boundary === 'stop') h.page.stopStream();
    if (boundary === 'unload') h.page.onUnload();
    if (boundary === 'account-change' || boundary === 'storage-change') h.token('session-two');
    if (boundary === 'account-change') h.page.onShow();
    const before = JSON.stringify(h.page.data);
    callbacks.onChunk('旧片段'); callbacks.onDone('旧账号文案'); callbacks.onError('旧错误');
    assert.equal(JSON.stringify(h.page.data), before);
    if (boundary !== 'storage-change') assert.equal(aborted, 1);
    if (boundary === 'account-change') assert.equal(h.page.data.streaming, false);
  });
}

test('stopped stream cannot overwrite or stop its replacement', () => {
  const h = harness('contentwriting'); const requests = [];
  h.aiStreamStub.startAIStream = options => { requests.push(options); return { abort() { options.onError('aborted'); } }; };
  h.page.onLoad({}); h.page.onShow(); h.page.setData({ facts: '资料' }); h.page.quickGenerate(); h.page.stopStream(); h.page.quickGenerate();
  requests[1].onChunk('新片段'); requests[0].onDone('旧结果'); requests[0].onError('旧错误');
  assert.equal(h.page.data.streaming, true); assert.equal(h.page.data.streamPartial, '新片段'); assert.equal(h.page.data.error, '');
  requests[1].onDone('新结果'); requests[1].onChunk('完成后的片段'); requests[1].onError('完成后的错误');
  assert.equal(h.page.data.result, '新结果'); assert.equal(h.page.data.streaming, false); assert.equal(h.page.data.streamPartial, ''); assert.equal(h.page.data.error, '');
});

test('synchronously completed stream leaves no active task and startup errors recover', () => {
  const h = harness('contentwriting'); let aborted = 0;
  h.aiStreamStub.startAIStream = options => { options.onDone('同步结果'); return { abort() { aborted++; } }; };
  h.page.onLoad({}); h.page.onShow(); h.page.setData({ facts: '资料' }); h.page.quickGenerate();
  assert.equal(h.page.data.result, '同步结果'); assert.equal(h.page.data.streaming, false); assert.ok(!h.page._streamTask);
  h.aiStreamStub.startAIStream = () => { throw new Error('request unavailable'); };
  h.page.quickGenerate(); assert.equal(h.page.data.streaming, false); assert.ok(h.page.data.error); assert.equal(aborted, 0);
});

test('known writing types keep product names while retaining server instructions and custom types', async () => {
  const h = harness('contentwriting');
  h.apiStub.writingPrompts = async () => ({ prompts: [
    ...['design', 'project', 'promotion'].map(key => ({ key, label: 'è®¾', heading: '服务端标题', instruction: key + ' 服务端专用指令' })),
    { key: 'custom', label: '招募文案', heading: '自定义标题', instruction: '自定义指令' },
  ] });
  h.page.onLoad({}); h.page.onShow(); await tick();
  assert.deepEqual(Array.from(h.page.data.writingTypes, item => item.label), ['设计说明', '项目介绍', '推广文案', '招募文案']);
  h.page.setData({ facts: '真实资料' });
  for (const key of ['design', 'project', 'promotion', 'custom']) {
    h.page.selectType({ currentTarget: { dataset: { type: key } } });
    assert.match(h.page.buildPrompt(), new RegExp(key === 'custom' ? '自定义指令' : key + ' 服务端专用指令'));
  }
  assert.equal(h.page.data.heading, '自定义标题');
});

test('native error rendering places complete feedback above footer actions and reserves its height', () => {
  const compiler = process.env.WXML_COMPILER || '/Applications/wechatwebdevtools.app/Contents/Resources/app.asar.unpacked/node_modules/wcc-exec/wcc';
  const file = './pages/contentwriting/contentwriting.wxml';
  const source = require('node:child_process').execFileSync(compiler, [file], { cwd: root, encoding: 'utf8' });
  const context = { window: {}, console: { log: error => { throw error; } } };
  vm.createContext(context); vm.runInContext(source, context);
  const error = '请先填写已知资料和要求，AI 将以这些事实为依据。\n补充项目地点与设计重点后重试。';
  const nodes = tree => typeof tree === 'object' && tree ? [tree, ...(tree.children || []).flatMap(nodes)] : [];
  const text = tree => typeof tree === 'string' ? tree : (tree?.children || []).map(text).join('');
  const tree = context.$gwx(file)({ __profileAccessMounted: true, loginRequired: false, error, result: '', writingTypes: [], introPresets: [] });
  const all = nodes(tree);
  const footer = all.find(node => node.attr?.class === 'writing-footer');
  const alert = nodes(footer).find(node => node.attr?.role === 'alert');
  assert.ok(alert, 'error must be visible in the fixed footer');
  assert.equal(text(alert), error);
  const row = nodes(footer).find(node => node.attr?.class === 'writing-footer-actions');
  assert.ok(nodes(row).some(node => node.attr?.bindtap === 'quickGenerate'));
  const reserve = all.find(node => String(node.attr?.class || '').includes('writing-error-reserve'));
  assert.equal(text(reserve), error, 'reserve the actual wrapped message height without truncating');
});

test('manual writing clears stale generation errors without changing the entered result', () => {
  const h = harness('contentwriting');
  h.page.setData({ error: '请先填写创作要求' });
  h.page.editResult({ detail: { value: '手动填写的文案' } });
  assert.equal(h.page.data.result, '手动填写的文案');
  assert.equal(h.page.data.error, '');
});
