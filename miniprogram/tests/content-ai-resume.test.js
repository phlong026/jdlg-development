'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const pagesRoot = path.join(__dirname, '../miniprogram/pages');

function loadPage(name) {
  let definition;
  let token = 'session-one';
  const navigations = [];
  const toasts = [];
  const listeners = {};
  const emitted = [];
  const backs = [];
  const channel = {
    on(event, handler) { listeners[event] = handler; },
    emit(event, value) { emitted.push({ event, value }); },
  };
  const wx = {
    getStorageSync: key => key === 'token' ? token : '',
    hideShareMenu() {},
    navigateTo(options) { navigations.push(options); },
    navigateBack() { backs.push({}); },
    showToast(options) { toasts.push(options); },
    getWindowInfo: () => ({ windowWidth: 375 }),
    createVideoContext: () => ({ pause() {} }),
  };
  vm.runInNewContext(fs.readFileSync(path.join(pagesRoot, name, `${name}.js`), 'utf8'), {
    Page(value) { definition = value; },
    wx,
    Promise,
    JSON,
    Set,
    Map,
    require(id) {
      if (id.includes('phase3-api')) return { writingPrompts: async () => { throw new Error('offline'); } };
      if (id.includes('ai-stream')) return { startAIStream: () => ({ abort() {} }) };
      if (id.includes('article-document')) return { preserveLayoutBody: (_layout, fallback) => fallback, renderArticleDocument: () => [] };
      throw new Error(`unexpected require ${id}`);
    },
  });
  const page = {
    ...definition,
    data: JSON.parse(JSON.stringify(definition.data)),
    setData(values) { Object.assign(this.data, values); },
    getOpenerEventChannel: () => channel,
  };
  return { page, navigations, toasts, listeners, emitted, channel, backs, token(value) { token = value; } };
}

test('a saved draft sends only existing text facts to AI and appends the return without losing media blocks', () => {
  const h = loadPage('contentedit');
  h.page.setData({
    id: 'content-42',
    revision: 7,
    title: '已保存的庭院方案',
    blocks: [
      { kind: 'paragraph', text: '入口朝南' },
      { kind: 'image', media_id: 'private-image', caption: '保留现有大树' },
      { kind: 'list', items: ['不改建围墙', '保留老井'] },
    ],
  });

  h.page.openAIWriting();
  assert.equal(h.navigations.length, 1, '已保存草稿也应能继续 AI 编辑');
  h.navigations[0].success({ eventChannel: h.channel });
  const context = h.emitted.find(item => item.event === 'writingContext').value;
  assert.equal(context.source, 'editor');
  assert.equal(context.content_id, 'content-42');
  assert.equal(context.revision, 7);
  assert.equal(context.title, '已保存的庭院方案');
  assert.match(context.facts, /入口朝南/);
  assert.match(context.facts, /保留现有大树/);
  assert.match(context.facts, /不改建围墙/);
  assert.doesNotMatch(context.facts, /private-image/);

  const returned = h.navigations[0].events.prefillContent;
  returned({ title: '不覆盖原标题', text: 'AI 补充的设计说明' });
  assert.equal(h.page.data.title, '已保存的庭院方案');
  assert.deepEqual(h.page.data.blocks[1], { kind: 'image', media_id: 'private-image', caption: '保留现有大树' });
  assert.equal(h.page.data.blocks.at(-1).text, 'AI 补充的设计说明');
  returned({ title: '不覆盖原标题', text: 'AI 补充的设计说明' });
  assert.equal(h.page.data.blocks.filter(block => block.text === 'AI 补充的设计说明').length, 1);
});

test('saved-draft AI return keeps account, revision, busy and original-layout guards', () => {
  const changed = loadPage('contentedit');
  changed.page.setData({ id: 'content-42', revision: 7, title: '标题', blocks: [{ kind: 'paragraph', text: '原文' }] });
  changed.page.openAIWriting();
  changed.page.setData({ revision: 8 });
  changed.navigations[0].events.prefillContent({ text: '不应回填' });
  assert.equal(changed.page.data.blocks.length, 1);
  assert.match(changed.page.data.notice, /变化|版本/);

  const busy = loadPage('contentedit');
  busy.page.setData({ id: 'content-42', revision: 7, title: '标题', blocks: [{ kind: 'paragraph', text: '原文' }] });
  busy.page.openAIWriting();
  busy.page.setData({ busy: true });
  busy.navigations[0].events.prefillContent({ text: '不应回填' });
  assert.equal(busy.page.data.blocks.length, 1);
  assert.match(busy.page.data.notice, /保存|处理/);

  const readOnly = loadPage('contentedit');
  readOnly.page.setData({ id: 'content-layout', layoutReadOnly: true });
  readOnly.page.openAIWriting();
  assert.equal(readOnly.navigations.length, 0);

  const switched = loadPage('contentedit');
  switched.page.setData({ id: 'content-42', revision: 7, title: '标题', blocks: [{ kind: 'paragraph', text: '原文' }] });
  switched.page.openAIWriting();
  switched.token('session-two');
  switched.navigations[0].events.prefillContent({ text: '不应回填' });
  assert.equal(switched.page.data.blocks.length, 1);
  assert.match(switched.page.data.notice, /账号/);
});

test('writing page receives editor context through EventChannel and returns to the same draft', () => {
  const h = loadPage('contentwriting');
  h.page.onLoad({ source: 'editor' });
  h.listeners.writingContext({ source: 'editor', content_id: 'content-42', revision: 7, title: '原标题', facts: '只有这些已知事实' });
  assert.equal(h.page.data.title, '原标题');
  assert.equal(h.page.data.facts, '只有这些已知事实');
  h.page.setData({ result: '用户核对后的文案' });
  h.page.saveToDraft();
  const returned = h.emitted.find(item => item.event === 'prefillContent').value;
  assert.equal(returned.text, '用户核对后的文案');
  assert.equal(returned.content_id, 'content-42');
  assert.equal(returned.revision, 7);
});

test('writing page returns explicit project-note identity through the same EventChannel', () => {
  const h = loadPage('contentwriting');
  h.page.onLoad({ source: 'project_note' });
  assert.equal(typeof h.listeners.writingContext, 'function');
  h.listeners.writingContext({
    source: 'project_note',
    project_id: 'project-8',
    note_id: 'note-12',
    note_revision: 4,
    title: '业主沟通纪要',
    facts: '业主明确要求保留东侧窗洞。',
  });
  assert.equal(h.page.data.title, '业主沟通纪要');
  assert.equal(h.page.data.facts, '业主明确要求保留东侧窗洞。');
  assert.match(h.page.data.returnLabel, /项目笔记/);

  h.page.editResult({ detail: { value: '经人工核对的沟通文案' } });
  h.page.saveToDraft();
  assert.equal(h.navigations.length, 0, '采纳项目笔记文案不应打开第二个内容编辑器');
  const returned = h.emitted.find(item => item.event === 'prefillContent').value;
  assert.equal(returned.source, 'project_note');
  assert.equal(returned.project_id, 'project-8');
  assert.equal(returned.note_id, 'note-12');
  assert.equal(returned.note_revision, 4);
  assert.equal(returned.title, '业主沟通纪要');
  assert.equal(returned.text, '经人工核对的沟通文案');
});

test('saved editable drafts expose AI writing while original-layout content stays read-only', () => {
  const wxml = fs.readFileSync(path.join(pagesRoot, 'contentedit/contentedit.wxml'), 'utf8');
  assert.match(wxml, /class="text-action ai-action" bindtap="openAIWriting"/);
  assert.doesNotMatch(wxml, /wx:if="\{\{!id\}\}" class="text-action ai-action"/);
  assert.match(wxml, /<block wx:if="\{\{!layoutReadOnly\}\}">/);
});

test('AI writing navigation failure keeps the draft and reports a retryable error', () => {
  const h = loadPage('contentedit');
  h.page.setData({ id: 'content-42', revision: 7, title: '标题', blocks: [{ kind: 'paragraph', text: '原文' }] });
  h.page.openAIWriting();
  h.navigations[0].fail();
  assert.equal(h.page.data.blocks.length, 1);
  assert.equal(h.page.data.blocks[0].text, '原文');
  assert.match(h.page.data.notice, /无法打开|重试/);
});

test('video editor requests one-click bundle copy and preserves omitted returned fields', () => {
  const h = loadPage('contentedit');
  h.page.setData({ presentation: 'video', title: '新中式乡村别墅', shareCopy: '保留的转发文案', momentsCopy: '', blocks: [{ kind: 'video', media_id: 'video-1', caption: '' }] });
  h.page.openAIWriting();
  h.navigations[0].success({ eventChannel: h.channel });
  const context = h.emitted.find(item => item.event === 'writingContext').value;
  assert.equal(context.target, 'video_bundle');
  assert.equal(context.auto_generate, true);
  assert.equal(context.share_copy, '保留的转发文案');
  h.navigations[0].events.prefillContent({ video_copy: { title: 'AI 新标题', moments_copy: 'AI 朋友圈文案' } });
  assert.equal(h.page.data.title, 'AI 新标题');
  assert.equal(h.page.data.shareCopy, '保留的转发文案');
  assert.equal(h.page.data.momentsCopy, 'AI 朋友圈文案');
});

// F1 / T1.1：AI 页必须在原页面确认后才关闭；暂时拒绝不关闭、不丢文案；回放不重复追加。
test('AI return closes only after the opener acknowledges, and a busy rejection keeps the copy retryable', async () => {
  const h = loadPage('contentwriting');
  h.page.onLoad({ source: 'project_note' });
  h.listeners.writingContext({ source: 'project_note', project_id: 'project-8', note_id: 'note-12', note_revision: 4, title: '沟通纪要', facts: '业主保留东侧窗洞。' });
  h.page.editResult({ detail: { value: '经人工核对的沟通文案' } });
  h.page.saveToDraft();
  assert.equal(h.backs.length, 0, '未收到原页面确认前不得关闭');
  assert.equal(h.page.data.navigating, true, '等待确认期间不得重复发起');
  const first = h.emitted.filter(item => item.event === 'prefillContent');
  assert.equal(first.length, 1);
  assert.ok(first[0].value.request > 0 && first[0].value.operation, '回填请求必须带请求 ID 与操作 ID');

  h.listeners.writingAck({ state: 'retryable', message: '笔记正在保存，稍后可再次采纳', request: first[0].value.request, operation: first[0].value.operation });
  assert.equal(h.backs.length, 0, '暂时拒绝不得关闭 AI 页');
  assert.equal(h.page.data.navigating, false);
  assert.equal(h.page.data.result, '经人工核对的沟通文案', '文案必须保留可复制');
  assert.match(h.page.data.notice, /稍后/);

  h.page.saveToDraft();
  const second = h.emitted.filter(item => item.event === 'prefillContent');
  assert.equal(second.length, 2, '解除后应可再次采纳');
  assert.equal(second[1].value.operation, first[0].value.operation, '同一次采纳的 operation 保持稳定');
  assert.notEqual(second[1].value.request, first[0].value.request, '每次发送使用新的请求 ID');
  // 旧请求的迟到回执不得关闭本次等待，也不得解除按钮锁
  h.listeners.writingAck({ state: 'applied', message: '旧回执', request: first[0].value.request, operation: first[0].value.operation });
  assert.equal(h.backs.length, 0, '错配请求 ID 的旧回执必须忽略');
  assert.equal(h.page.data.navigating, true);
  h.listeners.writingAck({ state: 'applied', message: 'AI 文案已带回此笔记', request: second[1].value.request, operation: second[1].value.operation });
  await Promise.resolve(); await Promise.resolve();
  assert.equal(h.backs.length, 1, '确认成功后才关闭');
});

test('video copy return uses the same acknowledgement and fills only selected fields', async () => {
  const h = loadPage('contentwriting');
  h.page.onLoad({ source: 'editor' });
  h.listeners.writingContext({ source: 'editor', content_id: 'content-3', revision: 2, target: 'video_bundle', title: '新中式乡村别墅', facts: '两层', auto_generate: false, share_copy: '', moments_copy: '' });
  h.page.setData({ videoCopy: { title: '视频标题', share_copy: '转发文案', moments_copy: '朋友圈文案' } });
  h.page.selectVideoFields({ detail: { value: ['title', 'share_copy'] } });
  h.page.saveToDraft();
  const payload = h.emitted.find(item => item.event === 'prefillContent').value;
  assert.deepEqual(Object.keys(payload.video_copy).sort(), ['share_copy', 'title'], '只提交勾选字段');
  assert.equal(h.backs.length, 0);
  h.listeners.writingAck({ state: 'invalid', message: '草稿版本已变化，请重新发起', request: payload.request, operation: payload.operation });
  assert.equal(h.backs.length, 0, '目标失效不得关闭 AI 页');
  assert.equal(h.page.data.navigating, false);
  assert.equal(h.page.data.videoCopy.title, '视频标题', '失效时视频文案仍保留可复制');
});

test('an applied return replays its success ack instead of appending a second time', () => {
  const h = loadPage('contentedit');
  h.page.setData({ id: 'content-42', revision: 7, title: '标题', blocks: [{ kind: 'paragraph', text: '原有正文' }] });
  h.page.openAIWriting();
  h.navigations[0].success({ eventChannel: h.channel });
  const returned = h.navigations[0].events.prefillContent;
  returned({ title: '标题', text: 'AI 新正文' });
  const first = h.emitted.filter(item => item.event === 'writingAck').map(item => item.value);
  assert.equal(first.length, 1);
  assert.equal(first[0].state, 'applied');
  returned({ title: '标题', text: 'AI 新正文' });
  const second = h.emitted.filter(item => item.event === 'writingAck').map(item => item.value);
  assert.equal(second.length, 2, '重复请求必须回执');
  assert.equal(second[1].state, 'applied');
  assert.equal(second[1].replayed, true);
  assert.equal(h.page.data.blocks.filter(block => block.text === 'AI 新正文').length, 1, '不得重复追加');
});

// F1 / T1.1：编辑器侧已应用后的重放同样必须重新核对目标与载荷。
test('an applied editor AI return turns invalid once the draft or the returned copy changes', () => {
  const prepare = () => {
    const h = loadPage('contentedit');
    h.page.setData({ id: 'content-42', revision: 7, title: '标题', blocks: [{ kind: 'paragraph', text: '原有正文' }] });
    h.page.openAIWriting();
    h.navigations[0].success({ eventChannel: h.channel });
    const returned = h.navigations[0].events.prefillContent;
    returned({ title: '标题', text: 'AI 新正文', request: 1, operation: 'op-1' });
    const acks = () => h.emitted.filter(item => item.event === 'writingAck').map(item => item.value);
    assert.equal(acks()[0].state, 'applied');
    assert.equal(acks()[0].request, 1);
    assert.equal(h.page.data.blocks.filter(block => block.text === 'AI 新正文').length, 1);
    return { h, returned, acks };
  };

  const same = prepare();
  same.returned({ title: '标题', text: 'AI 新正文', request: 2, operation: 'op-1' });
  assert.equal(same.acks()[1].state, 'applied');
  assert.equal(same.acks()[1].replayed, true);

  const changedCopy = prepare();
  changedCopy.returned({ title: '标题', text: 'AI 改过的正文', request: 2, operation: 'op-1' });
  assert.equal(changedCopy.acks()[1].state, 'invalid', '载荷变化不得重放旧成功');
  assert.equal(changedCopy.h.page.data.blocks.some(block => block.text === 'AI 改过的正文'), false);

  const switched = prepare();
  switched.h.token('session-two');
  switched.returned({ title: '标题', text: 'AI 新正文', request: 2, operation: 'op-1' });
  assert.equal(switched.acks()[1].state, 'invalid', '换账号后不得重放旧成功');
});

// F1 / T1.1：回执必须同时匹配 request 与 operation；账号变化后的迟到回执不得关闭页面。
test('a return ack must match both request and operation ids', async () => {
  const h = loadPage('contentwriting');
  h.page.onLoad({ source: 'editor' });
  h.listeners.writingContext({ source: 'editor', content_id: 'c-1', revision: 1, title: '标题', facts: '资料' });
  h.page.setData({ result: '待采纳文案' });
  h.page.saveToDraft();
  const sent = h.emitted.find(item => item.event === 'prefillContent').value;

  h.listeners.writingAck({ state: 'applied', request: sent.request });
  assert.equal(h.backs.length, 0, '缺少 operation 的回执必须忽略');
  assert.equal(h.page.data.navigating, true);
  h.listeners.writingAck({ state: 'applied', request: sent.request, operation: sent.operation + '-other' });
  assert.equal(h.backs.length, 0, 'operation 不匹配的回执必须忽略');
  h.listeners.writingAck({ state: 'applied', request: sent.request + 99, operation: sent.operation });
  assert.equal(h.backs.length, 0, 'request 不匹配的回执必须忽略');
  h.listeners.writingAck({ state: 'applied', request: sent.request, operation: sent.operation });
  await Promise.resolve(); await Promise.resolve();
  assert.equal(h.backs.length, 1, '双匹配后才退回原页面');
});

test('a late applied ack cannot close the page after the account changed', async () => {
  const h = loadPage('contentwriting');
  h.page.onLoad({ source: 'editor' });
  h.listeners.writingContext({ source: 'editor', content_id: 'c-1', revision: 1, title: '标题', facts: '资料' });
  h.page.setData({ result: '待采纳文案' });
  h.page.saveToDraft();
  const sent = h.emitted.find(item => item.event === 'prefillContent').value;
  // 账号已变化但 onShow 尚未执行：旧回执不得关闭页面
  h.token('session-two');
  h.listeners.writingAck({ state: 'applied', request: sent.request, operation: sent.operation });
  await Promise.resolve(); await Promise.resolve();
  assert.equal(h.backs.length, 0, '账号变化后旧回执不得关闭页面');
  assert.match(h.page.data.notice, /账号/);
});

// F1 / T1.1：延后带入不得让已失效的操作复活。
test('a deferred prefill cannot resurrect an invalidated return', () => {
  const h = loadPage('contentedit');
  h.page.setData({ id: '', revision: 0, title: '', blocks: [{ kind: 'paragraph', text: '' }] });
  h.page.openAIWriting();
  h.navigations[0].success({ eventChannel: h.channel });
  const returned = h.navigations[0].events.prefillContent;
  returned({ title: 'AI 标题', text: 'AI 正文', request: 1, operation: 'op-1' });
  let acks = h.emitted.filter(item => item.event === 'writingAck').map(item => item.value);
  assert.equal(acks.at(-1).state, 'retryable', '编辑器未载入时先延后');
  h.token('session-two');
  returned({ title: 'AI 标题', text: 'AI 正文', request: 2, operation: 'op-1' });
  acks = h.emitted.filter(item => item.event === 'writingAck').map(item => item.value);
  assert.equal(acks.at(-1).state, 'invalid', '账号变化后操作失效');
  h.page._loaded = true;
  h.page.setData({ context: { owners: [] } });
  assert.equal(h.page.applyPendingPrefill(), 'dropped', '失效操作不得被延后带入');
  assert.equal(h.page.data.title, '');
  assert.equal(h.page.data.blocks.some(block => block.text === 'AI 正文'), false);
});
