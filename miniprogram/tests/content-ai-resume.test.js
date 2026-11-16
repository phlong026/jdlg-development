'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const pagesRoot = path.join(__dirname, '../miniprogram/pages');

function loadPage(name, shared) {
  let definition;
  let token = 'session-one';
  const storage = shared && shared.storage ? shared.storage : new Map();
  const navigations = [];
  const toasts = [];
  const listeners = {};
  const emitted = [];
  const backs = [];
  const channel = {
    on(event, handler) { listeners[event] = handler; },
    emit(event, value) { emitted.push({ event, value }); },
  };
  const aiStreamStub = { startAIStream: () => ({ abort() {} }) };
  const wx = {
    getStorageSync: key => key === 'token' ? token : (storage.has(key) ? storage.get(key) : ''),
    setStorageSync: (key, value) => { storage.set(key, value); },
    removeStorageSync: key => { storage.delete(key); },
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
      if (id.includes('ai-stream')) return { startAIStream: options => aiStreamStub.startAIStream(options) };
      if (id.includes('writing-policy')) return require(path.join(__dirname, '../miniprogram/utils/writing-policy.js'));
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
  return { page, navigations, toasts, listeners, emitted, channel, backs, storage, aiStreamStub, token(value) { token = value; } };
}

function legacyEditorLaunch(h) {
  return { token: 'session-one', contentID: String(h.page.data.id || ''), revision: Number(h.page.data.revision || 0), signature: h.page.draftSignature(), state: '', message: '', channel: h.channel };
}

test('a saved article draft sends only existing text facts to inline AI and appends without losing structure', () => {
  const h = loadPage('contentedit');
  let stream;
  h.aiStreamStub.startAIStream = options => { stream = options; return { abort() {} }; };
  h.page.setData({
    id: 'content-42',
    revision: 7,
    presentation: 'article',
    title: '已保存的庭院方案',
    blocks: [
      { kind: 'paragraph', text: '入口朝南' },
      { kind: 'image', media_id: 'private-image', caption: '保留现有大树' },
      { kind: 'list', items: ['不改建围墙', '保留老井'] },
    ],
  });

  h.page.openAIWriting();
  assert.equal(h.navigations.length, 0);
  assert.match(stream.messages[0].content, /入口朝南/);
  assert.match(stream.messages[0].content, /保留现有大树/);
  assert.match(stream.messages[0].content, /不改建围墙/);
  assert.doesNotMatch(stream.messages[0].content, /private-image/);
  stream.onDone('AI 补充的设计说明');
  assert.equal(h.page.data.title, '已保存的庭院方案');
  assert.equal(JSON.stringify(h.page.data.blocks[1]), JSON.stringify({ kind: 'image', media_id: 'private-image', caption: '保留现有大树' }));
  assert.equal(h.page.data.blocks.at(-1).text, 'AI 补充的设计说明');
  stream.onDone('AI 补充的设计说明');
  assert.equal(h.page.data.blocks.filter(block => block.text === 'AI 补充的设计说明').length, 1);
});

test('inline article AI keeps account, revision, busy and original-layout guards', () => {
  const changed = loadPage('contentedit');
  let changedStream;
  changed.aiStreamStub.startAIStream = options => { changedStream = options; return { abort() {} }; };
  changed.page.setData({ id: 'content-42', revision: 7, presentation: 'article', title: '标题', blocks: [{ kind: 'paragraph', text: '原文' }] });
  changed.page.openAIWriting();
  changed.page.setData({ revision: 8 });
  changedStream.onDone('不应回填');
  assert.equal(changed.page.data.blocks.length, 1);

  const busy = loadPage('contentedit');
  let busyCalls = 0;
  busy.aiStreamStub.startAIStream = () => { busyCalls += 1; return { abort() {} }; };
  busy.page.setData({ id: 'content-42', revision: 7, presentation: 'article', title: '标题', blocks: [{ kind: 'paragraph', text: '原文' }], busy: true });
  busy.page.openAIWriting();
  assert.equal(busyCalls, 0);

  const readOnly = loadPage('contentedit');
  readOnly.page.setData({ id: 'content-layout', layoutReadOnly: true });
  readOnly.page.openAIWriting();
  assert.equal(readOnly.navigations.length, 0);

  const switched = loadPage('contentedit');
  let switchedStream;
  switched.aiStreamStub.startAIStream = options => { switchedStream = options; return { abort() {} }; };
  switched.page.setData({ id: 'content-42', revision: 7, presentation: 'article', title: '标题', blocks: [{ kind: 'paragraph', text: '原文' }] });
  switched.page.openAIWriting();
  switched.token('session-two');
  switchedStream.onDone('不应回填');
  assert.equal(switched.page.data.blocks.length, 1);
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

test('content editor AI entry no longer references the independent writing page', () => {
  const source = fs.readFileSync(path.join(pagesRoot, 'contentedit/contentedit.js'), 'utf8');
  assert.doesNotMatch(source, /\/pages\/contentwriting\/contentwriting\?source=editor/);
});

test('gallery and video editors generate introductions inline without navigating', () => {
  for (const presentation of ['gallery', 'video']) {
    const h = loadPage('contentedit');
    let stream;
    h.aiStreamStub.startAIStream = options => { stream = options; return { abort() {} }; };
    const media = presentation === 'gallery'
      ? [{ kind: 'image', media_id: 'image-1', caption: '保留院子里的老树' }]
      : [{ kind: 'video', media_id: 'video-1', caption: '庭院改造前后对比' }];
    h.page.setData({ presentation, title: '新中式乡村别墅', postText: '原介绍', blocks: media.concat({ kind: 'paragraph', text: '原介绍' }) });
    h.page.openAIWriting();
    assert.equal(h.navigations.length, 0, `${presentation} 不应跳出编辑页`);
    assert.ok(stream, `${presentation} 应复用原流式通道`);
    assert.match(stream.messages[0].content, /无法看到图片或视频画面/);
    assert.doesNotMatch(stream.messages[0].content, /image-1|video-1|\/tmp|wxfile:/, '不向文本模型伪装传入私有素材');
    stream.onChunk('AI 生成中');
    assert.equal(h.page.data.postText, 'AI 生成中');
    assert.equal(h.page.data.blocks.at(-1).text, 'AI 生成中');
    stream.onDone('AI 生成完整介绍');
    assert.equal(h.page.data.postText, 'AI 生成完整介绍');
    assert.equal(h.page.data.blocks.at(-1).text, 'AI 生成完整介绍');
    assert.equal(h.page.data.aiInlineCanRestore, true);
    assert.equal(h.page.data.aiInlineStreaming, false);
  }
});

test('article editor generates inline into a paragraph without flattening structured blocks and can restore', () => {
  const h = loadPage('contentedit');
  let stream;
  h.aiStreamStub.startAIStream = options => { stream = options; return { abort() {} }; };
  const original = [
    { kind: 'heading', level: 2, text: '院落改造要点' },
    { kind: 'paragraph', text: '保留东侧老树。' },
    { kind: 'list', ordered: false, items: ['不改建围墙', '保留老井'] },
    { kind: 'image', media_id: 'private-image', caption: '院落现状' },
    { kind: 'quote', text: '业主确认不迁移老树' },
    { kind: 'paragraph', text: '' },
  ];
  h.page.setData({ presentation: 'article', title: '院落改造方案', postText: '保留东侧老树。', blocks: original });
  h.page.openAIWriting();
  assert.equal(h.navigations.length, 0, '文章 AI 不应再打开独立页');
  assert.ok(stream, '文章应复用原流式生成通道');
  assert.doesNotMatch(stream.messages[0].content, /private-image/, '只传入可确认文本');
  stream.onChunk('AI 正文生成中');
  assert.equal(JSON.stringify(h.page.data.blocks.slice(0, 5)), JSON.stringify(original.slice(0, 5)), '标题、列表、媒体和引用必须保留');
  assert.equal(h.page.data.blocks[5].kind, 'paragraph');
  assert.equal(h.page.data.blocks[5].text, 'AI 正文生成中');
  stream.onDone('AI 完整正文');
  assert.equal(h.page.data.blocks[5].text, 'AI 完整正文');
  h.page.restoreInlineAIOriginal();
  assert.equal(JSON.stringify(h.page.data.blocks), JSON.stringify(original), '恢复时应精确还原生成前结构');
});

test('article without an empty paragraph reuses one appended target across chunks, completion and retry', () => {
  const h = loadPage('contentedit');
  const streams = [];
  h.aiStreamStub.startAIStream = options => { streams.push(options); return { abort() {} }; };
  const original = [{ kind: 'heading', level: 2, text: '改造要点' }, { kind: 'paragraph', text: '原有正文' }];
  h.page.setData({ presentation: 'article', title: '庭院方案', postText: '原有正文', blocks: original });
  h.page.openAIWriting();
  streams[0].onChunk('第一个流式片段');
  streams[0].onChunk('第二个流式片段');
  streams[0].onDone('第一次完整正文');
  assert.equal(h.page.data.blocks.length, original.length + 1);
  assert.equal(JSON.stringify(h.page.data.blocks.slice(0, original.length)), JSON.stringify(original));
  assert.equal(h.page.data.blocks[original.length].text, '第一次完整正文');

  h.page.openAIWriting();
  streams[1].onChunk('第二次流式片段');
  streams[1].onDone('第二次完整正文');
  assert.equal(h.page.data.blocks.length, original.length + 1, '重试不得重复追加段落');
  assert.equal(h.page.data.blocks[original.length].text, '第二次完整正文');
});

test('manual edits to a streaming article paragraph abort AI and keep the editable paragraph', () => {
  const h = loadPage('contentedit');
  let stream, aborted = 0;
  h.aiStreamStub.startAIStream = options => { stream = options; return { abort() { aborted += 1; } }; };
  h.page.setData({ presentation: 'article', title: '庭院方案', blocks: [{ kind: 'heading', level: 2, text: '改造要点' }] });
  h.page.openAIWriting();
  stream.onChunk('AI 半截正文');
  h.page.editBlock({ currentTarget: { dataset: { index: 1 } }, detail: { value: '用户手工正文' } });
  stream.onDone('迟到的 AI 正文');
  assert.equal(aborted, 1);
  assert.equal(h.page.data.blocks[1].kind, 'paragraph');
  assert.equal(h.page.data.blocks[1].text, '用户手工正文');
});

test('article stream failure removes its partial appended paragraph and preserves the original structure', () => {
  const h = loadPage('contentedit');
  let stream;
  h.aiStreamStub.startAIStream = options => { stream = options; return { abort() {} }; };
  const original = [{ kind: 'heading', level: 2, text: '改造要点' }, { kind: 'paragraph', text: '保留原正文' }, { kind: 'list', ordered: true, items: ['第一项'] }];
  h.page.setData({ presentation: 'article', title: '庭院方案', postText: '保留原正文', blocks: original });
  h.page.openAIWriting();
  stream.onChunk('AI 半截正文');
  stream.onError('网络异常');
  assert.equal(JSON.stringify(h.page.data.blocks), JSON.stringify(original));
  assert.equal(h.page.data.postText, '保留原正文');
  assert.match(h.page.data.aiInlineError, /网络异常/);
});

test('failed article generation cannot restore an older draft after manual editing and a second failure', () => {
  const h = loadPage('contentedit');
  const streams = [];
  h.aiStreamStub.startAIStream = options => { streams.push(options); return { abort() {} }; };
  h.page.setData({ presentation: 'article', title: '庭院方案', blocks: [{ kind: 'paragraph', text: '第一版人工稿' }] });
  h.page.openAIWriting();
  streams[0].onChunk('第一次 AI 半截');
  streams[0].onError('第一次失败');
  h.page.editBlock({ currentTarget: { dataset: { index: 0 } }, detail: { value: '第二版人工稿' } });
  h.page.openAIWriting();
  streams[1].onChunk('第二次 AI 半截');
  streams[1].onError('第二次失败');
  assert.equal(h.page.data.blocks[0].text, '第二版人工稿');
  assert.equal(streams.length, 2, '每次点击只发起一个请求');
});

test('402 after partial output restores the original and keeps the exact balance message retryable', () => {
  const h = loadPage('contentedit');
  const streams = [];
  h.aiStreamStub.startAIStream = options => { streams.push(options); return { abort() {} }; };
  h.page.setData({ presentation: 'gallery', title: '庭院方案', postText: '人工原介绍', blocks: [{ kind: 'paragraph', text: '人工原介绍' }] });
  h.page.openAIWriting();
  streams[0].onChunk('AI 半截介绍');
  const message = '当前生成需要 5 灵石，你的余额为 2 灵石，请先充值后再试。';
  streams[0].onError(message, 402);
  assert.equal(h.page.data.postText, '人工原介绍');
  assert.equal(h.page.data.aiInlineError, message);
  h.page.openAIWriting();
  assert.equal(streams.length, 2, '重试单击只能新增一个请求');
});

test('a completed inline introduction can restore the exact original blocks', () => {
  const h = loadPage('contentedit');
  let stream;
  h.aiStreamStub.startAIStream = options => { stream = options; return { abort() {} }; };
  const original = [{ kind: 'image', media_id: 'image-1', caption: '庭院入口' }, { kind: 'paragraph', text: '第一段' }, { kind: 'paragraph', text: '第二段' }];
  h.page.setData({ presentation: 'gallery', title: '庭院方案', postText: '第一段\n\n第二段', blocks: original });
  h.page.openAIWriting();
  stream.onDone('AI 介绍');
  h.page.restoreInlineAIOriginal();
  assert.equal(h.page.data.postText, '第一段\n\n第二段');
  assert.equal(JSON.stringify(h.page.data.blocks), JSON.stringify(original));
  assert.equal(h.page.data.aiInlineCanRestore, false);
});

test('inline social AI failure restores the original introduction and remains retryable', () => {
  const h = loadPage('contentedit');
  let stream, calls = 0;
  h.aiStreamStub.startAIStream = options => { calls += 1; stream = options; return { abort() {} }; };
  h.page.setData({ presentation: 'video', title: '新中式乡村别墅', postText: '人工原稿', blocks: [{ kind: 'video', media_id: 'video-1', caption: '' }, { kind: 'paragraph', text: '人工原稿' }] });
  h.page.openAIWriting();
  stream.onChunk('半截 AI 文案');
  stream.onError('网络异常');
  assert.equal(h.page.data.postText, '人工原稿');
  assert.equal(h.page.data.blocks.at(-1).text, '人工原稿');
  assert.match(h.page.data.aiInlineError, /网络异常/);
  h.page.openAIWriting();
  assert.equal(calls, 2, '失败后可原页重试');
});

test('late inline AI callbacks cannot cross account actor or draft changes', () => {
  for (const boundary of ['token', 'actor', 'owner', 'draft']) {
    const storage = new Map([['userInfo', { id: 101 }]]);
    const h = loadPage('contentedit', { storage });
    let stream;
    h.aiStreamStub.startAIStream = options => { stream = options; return { abort() {} }; };
    h.page.setData({ id: 'draft-one', revision: 3, owner_id: 'owner-a', presentation: 'gallery', title: '庭院方案', postText: '原稿', blocks: [{ kind: 'image', media_id: 'image-1', caption: '老树' }, { kind: 'paragraph', text: '原稿' }] });
    h.page.openAIWriting();
    if (boundary === 'token') h.token('session-two');
    if (boundary === 'actor') storage.set('userInfo', { id: 202 });
    if (boundary === 'owner') h.page.setData({ owner_id: 'owner-b' });
    if (boundary === 'draft') h.page.setData({ id: 'draft-two' });
    stream.onDone('迟到的旧文案');
    assert.equal(h.page.data.postText, '原稿', `${boundary} 边界不得写入迟到结果`);
  }
});

test('identity changes after a partial stream restore the original instead of leaving old AI text savable', () => {
  for (const boundary of ['actor', 'owner', 'draft', 'revision']) {
    const storage = new Map([['userInfo', { id: 101 }]]);
    const h = loadPage('contentedit', { storage });
    let stream;
    h.aiStreamStub.startAIStream = options => { stream = options; return { abort() {} }; };
    const original = [{ kind: 'image', media_id: 'image-1', caption: '老树' }, { kind: 'paragraph', text: '人工原稿' }];
    h.page.setData({ id: 'draft-one', revision: 3, owner_id: 'owner-a', presentation: 'gallery', title: '庭院方案', postText: '人工原稿', blocks: original });
    h.page.openAIWriting();
    stream.onChunk('旧身份 AI 半截结果');
    if (boundary === 'actor') storage.set('userInfo', { id: 202 });
    if (boundary === 'owner') h.page.setData({ owner_id: 'owner-b' });
    if (boundary === 'draft') h.page.setData({ id: 'draft-two' });
    if (boundary === 'revision') h.page.setData({ revision: 4 });
    stream.onDone('迟到的完整结果');
    assert.equal(h.page.data.postText, '人工原稿', `${boundary} 变化后必须移除旧 AI 半截内容`);
    assert.equal(JSON.stringify(h.page.data.blocks), JSON.stringify(original));
    assert.equal(h.page.data.aiInlineConflict, false);
  }
});

test('an identity change plus a new draft body blocks saving instead of overwriting the new body', async () => {
  const h = loadPage('contentedit');
  let stream;
  h.aiStreamStub.startAIStream = options => { stream = options; return { abort() {} }; };
  h.page.setData({ id: 'draft-one', revision: 1, owner_id: 'owner-a', presentation: 'article', title: '旧草稿', blocks: [{ kind: 'paragraph', text: '旧正文' }] });
  h.page.openAIWriting();
  stream.onChunk('旧 AI 半截正文');
  h.page.setData({ id: 'draft-two', revision: 2, title: '新草稿', blocks: [{ kind: 'paragraph', text: '新草稿正文' }] });
  stream.onDone('迟到的旧 AI 正文');
  assert.equal(h.page.data.blocks[0].text, '新草稿正文');
  assert.equal(h.page.data.aiInlineConflict, true);
  const before = h.page.data.busy;
  await h.page.saveDraft();
  assert.equal(h.page.data.busy, before);
  assert.match(h.page.data.error, /重新进入|重新加载/);
});

test('manual introduction edits stop inline AI and are never overwritten by late chunks', () => {
  const h = loadPage('contentedit');
  let stream, aborted = 0;
  h.aiStreamStub.startAIStream = options => { stream = options; return { abort() { aborted += 1; } }; };
  h.page.setData({ presentation: 'gallery', title: '庭院方案', postText: '原稿', blocks: [{ kind: 'image', media_id: 'image-1', caption: '老树' }, { kind: 'paragraph', text: '原稿' }] });
  h.page.openAIWriting();
  stream.onChunk('AI 半截结果');
  h.page.editPostText({ detail: { value: '用户手工修改' } });
  stream.onChunk('迟到的 AI 结果');
  stream.onDone('迟到的完整结果');
  assert.equal(aborted, 1);
  assert.equal(h.page.data.postText, '用户手工修改');
  assert.equal(h.page.data.blocks.at(-1).text, '用户手工修改');
});

test('editing non-target fields restores the pre-stream body before applying the human change', () => {
  const title = loadPage('contentedit');
  let titleStream;
  title.aiStreamStub.startAIStream = options => { titleStream = options; return { abort() {} }; };
  title.page.setData({ presentation: 'gallery', title: '原标题', owner_id: 'owner-a', postText: '原介绍', blocks: [{ kind: 'paragraph', text: '原介绍' }] });
  title.page.openAIWriting();
  titleStream.onChunk('AI 半截介绍');
  title.page.setField({ currentTarget: { dataset: { field: 'title' } }, detail: { value: '人工新标题' } });
  assert.equal(title.page.data.title, '人工新标题');
  assert.equal(title.page.data.postText, '原介绍');
  assert.equal(title.page.data.blocks[0].text, '原介绍');
  assert.doesNotMatch(title.page.draftInput().draft.body, /AI 半截/);

  const caption = loadPage('contentedit');
  let captionStream;
  caption.aiStreamStub.startAIStream = options => { captionStream = options; return { abort() {} }; };
  caption.page.setData({ presentation: 'gallery', title: '庭院', postText: '原介绍', blocks: [{ kind: 'image', media_id: 'i1', caption: '旧注释' }, { kind: 'paragraph', text: '原介绍' }] });
  caption.page.openAIWriting();
  captionStream.onChunk('AI 半截介绍');
  caption.page.editCaption({ currentTarget: { dataset: { index: 0 } }, detail: { value: '人工新注释' } });
  assert.equal(caption.page.data.blocks[0].caption, '人工新注释');
  assert.equal(caption.page.data.blocks[1].text, '原介绍');

  const structured = loadPage('contentedit');
  let structuredStream;
  structured.aiStreamStub.startAIStream = options => { structuredStream = options; return { abort() {} }; };
  structured.page.setData({ presentation: 'article', title: '庭院', blocks: [{ kind: 'heading', level: 2, text: '原小标题' }, { kind: 'paragraph', text: '' }] });
  structured.page.openAIWriting();
  structuredStream.onChunk('AI 半截正文');
  structured.page.editBlock({ currentTarget: { dataset: { index: 0 } }, detail: { value: '人工新小标题' } });
  assert.equal(structured.page.data.blocks[0].text, '人工新小标题');
  assert.equal(structured.page.data.blocks[1].text, '', '非 AI 目标块编辑前应撤掉半截正文');

  const list = loadPage('contentedit');
  let listStream;
  list.aiStreamStub.startAIStream = options => { listStream = options; return { abort() {} }; };
  list.page.setData({ presentation: 'article', title: '庭院', blocks: [{ kind: 'list', ordered: false, items: ['原列表'] }, { kind: 'paragraph', text: '' }] });
  list.page.openAIWriting();
  listStream.onChunk('AI 半截正文');
  list.page.editListItem({ currentTarget: { dataset: { index: 0, itemIndex: 0 } }, detail: { value: '人工新列表' } });
  assert.equal(list.page.data.blocks[0].items[0], '人工新列表');
  assert.equal(list.page.data.blocks[1].text, '');
});

test('owner changes after a partial stream keep the human owner choice without retaining AI partial text', async () => {
  const h = loadPage('contentedit');
  let stream;
  h.aiStreamStub.startAIStream = options => { stream = options; return { abort() {} }; };
  h.page._token = 'session-one';
  h.page.setData({ id: '', presentation: 'gallery', title: '庭院', owner_id: 'owner-a', ownerIndex: 0,
    context: { owner_choices: [{ owner_id: 'owner-a', display_name: 'A' }, { owner_id: 'owner-b', display_name: 'B' }] },
    postText: '原介绍', blocks: [{ kind: 'paragraph', text: '原介绍' }] });
  h.page.openAIWriting();
  stream.onChunk('AI 半截介绍');
  h.page.setOwner({ detail: { value: 1 } });
  await Promise.resolve();
  assert.equal(h.page.data.owner_id, 'owner-b');
  assert.equal(h.page.data.blocks[0].text, '原介绍');
  assert.equal(h.page.data.aiInlineStreaming, false);
});

test('structure actions recompute safely after an appended AI paragraph is restored', () => {
  const h = loadPage('contentedit');
  let stream;
  h.aiStreamStub.startAIStream = options => { stream = options; return { abort() {} }; };
  const original = [{ kind: 'heading', level: 2, text: '原小标题' }];
  h.page.setData({ presentation: 'article', title: '庭院', blocks: original });
  h.page.openAIWriting();
  stream.onChunk('AI 追加正文');
  h.page.moveBlock({ currentTarget: { dataset: { index: 1, step: -1 } } });
  assert.equal(JSON.stringify(h.page.data.blocks), JSON.stringify(original));
  assert.equal(h.page.data.blocks.includes(undefined), false);
});

test('backgrounding restores the original and unloading rejects every late stream callback', () => {
  const h = loadPage('contentedit');
  let stream, aborted = 0;
  h.aiStreamStub.startAIStream = options => { stream = options; return { abort() { aborted += 1; } }; };
  h.page.setData({ presentation: 'video', title: '庭院视频', postText: '原介绍', blocks: [{ kind: 'video', media_id: 'v1', caption: '' }, { kind: 'paragraph', text: '原介绍' }] });
  h.page.openAIWriting();
  stream.onChunk('AI 半截');
  h.page.onHide();
  assert.equal(aborted, 1);
  assert.equal(h.page.data.postText, '原介绍');

  h.page.openAIWriting();
  const unloadedStream = stream;
  h.page.onUnload();
  unloadedStream.onChunk('卸载后的迟到内容');
  unloadedStream.onDone('卸载后的迟到完成');
  assert.equal(h.page.data.postText, '原介绍');
});

test('empty social editors ask for a real topic and never pretend to inspect media', () => {
  const h = loadPage('contentedit');
  let calls = 0;
  h.aiStreamStub.startAIStream = () => { calls += 1; return { abort() {} }; };
  h.page.setData({ presentation: 'gallery', title: '', postText: '', blocks: [{ kind: 'image', media_id: 'image-1', caption: '' }] });
  h.page.openAIWriting();
  assert.equal(calls, 0);
  assert.equal(h.navigations.length, 0);
  assert.match(h.page.data.aiInlineError, /标题|介绍|短注释/);
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
  const launch = legacyEditorLaunch(h);
  const returned = payload => h.page.receiveWritingReturn(payload, launch);
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
    const launch = legacyEditorLaunch(h);
    const returned = payload => h.page.receiveWritingReturn(payload, launch);
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
  const launch = legacyEditorLaunch(h);
  const returned = payload => h.page.receiveWritingReturn(payload, launch);
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

// F2 / T1.2：AI 结果轻量暂存——同实例可恢复、request ID 递增、换实例/换账号/过期都不恢复。
const projectNoteContext = (instanceID, extra) => Object.assign({ source: 'project_note', project_id: 'project-9', note_id: '', note_revision: 0, draft_instance_id: instanceID, title: '方案', facts: '已知事实' }, extra || {});

test('a generated project-note result is restored on the same draft instance and request ids keep increasing', () => {
  const shared = { storage: new Map() };
  const first = loadPage('contentwriting', shared);
  first.page.onLoad({ source: 'project_note' });
  first.page.receiveWritingContext(projectNoteContext('draft-a'));
  let captured;
  first.aiStreamStub.startAIStream = options => { captured = options; return { abort() {} }; };
  first.page.quickGenerate();
  captured.onDone('第一次生成但未采纳的文案');
  const record = shared.storage.get('project_note_ai_result_v1');
  assert.equal(record.draft_instance_id, 'draft-a');
  assert.equal(record.project_id, 'project-9');
  assert.equal(record.request_id, 1);
  assert.equal(record.text, '第一次生成但未采纳的文案');

  // 重进同一草稿实例：恢复文案与请求 ID 计数
  const second = loadPage('contentwriting', shared);
  second.page.onLoad({ source: 'project_note' });
  second.page.receiveWritingContext(projectNoteContext('draft-a'));
  assert.equal(second.page.data.result, '第一次生成但未采纳的文案');
  assert.match(second.page.data.notice, /恢复/);
  let capturedAgain;
  second.aiStreamStub.startAIStream = options => { capturedAgain = options; return { abort() {} }; };
  second.page.quickGenerate();
  capturedAgain.onDone('第二次生成的文案');
  const updated = shared.storage.get('project_note_ai_result_v1');
  assert.equal(updated.request_id, 2, '每次 AI 请求使用不同 request ID');
  assert.equal(updated.draft_instance_id, 'draft-a', '同一草稿实例 ID 不变');
});

test('a cached AI result never leaks across draft instances, accounts or expiry', () => {
  const shared = { storage: new Map() };
  const seed = { schema: 1, account: 'session-one', project_id: 'project-9', draft_instance_id: 'draft-a', request_id: 3, title: '方案', text: '旧缓存文案', updated_at: Date.now() };

  // 同项目、同正文的另一个空 ID 新草稿：实例不同不得串入
  shared.storage.set('project_note_ai_result_v1', { ...seed });
  const otherDraft = loadPage('contentwriting', shared);
  otherDraft.page.onLoad({ source: 'project_note' });
  otherDraft.page.receiveWritingContext(projectNoteContext('draft-b'));
  assert.equal(otherDraft.page.data.result, '', '不同草稿实例不得恢复旧缓存');

  // 账号不匹配不得恢复
  shared.storage.set('project_note_ai_result_v1', { ...seed, account: 'another-account' });
  const otherAccount = loadPage('contentwriting', shared);
  otherAccount.page.onLoad({ source: 'project_note' });
  otherAccount.page.receiveWritingContext(projectNoteContext('draft-a'));
  assert.equal(otherAccount.page.data.result, '', '换账号不得串出旧账号文案');

  // 超过有效期自动丢弃并清理
  shared.storage.set('project_note_ai_result_v1', { ...seed, updated_at: Date.now() - 31 * 60 * 1000 });
  const expired = loadPage('contentwriting', shared);
  expired.page.onLoad({ source: 'project_note' });
  expired.page.receiveWritingContext(projectNoteContext('draft-a'));
  assert.equal(expired.page.data.result, '', '过期缓存不得恢复');
  assert.equal(shared.storage.has('project_note_ai_result_v1'), false, '过期缓存应被清理');
});

test('a successful adoption clears the cached AI result and the page does not write it back on unload', async () => {
  const shared = { storage: new Map() };
  const h = loadPage('contentwriting', shared);
  h.page.onLoad({ source: 'project_note' });
  h.page.receiveWritingContext(projectNoteContext('draft-a'));
  let captured;
  h.aiStreamStub.startAIStream = options => { captured = options; return { abort() {} }; };
  h.page.quickGenerate();
  captured.onDone('准备采纳的文案');
  assert.ok(shared.storage.has('project_note_ai_result_v1'));

  h.page.saveToDraft();
  const payload = h.emitted.find(item => item.event === 'prefillContent').value;
  h.listeners.writingAck({ state: 'applied', message: '已采纳', request: payload.request, operation: payload.operation });
  assert.equal(shared.storage.has('project_note_ai_result_v1'), false, '采纳成功后必须清理缓存');
  await Promise.resolve(); await Promise.resolve();
  h.page.onUnload();
  assert.equal(shared.storage.has('project_note_ai_result_v1'), false, '采纳成功后卸载不得重新写回缓存');
});

test('leaving the AI page without adopting keeps the result for the same draft instance', () => {
  const shared = { storage: new Map() };
  const h = loadPage('contentwriting', shared);
  h.page.onLoad({ source: 'project_note' });
  h.page.receiveWritingContext(projectNoteContext('draft-a'));
  h.page.setData({ result: '手动修改后离开的文案' });
  h.page.onUnload();
  const record = shared.storage.get('project_note_ai_result_v1');
  assert.equal(record.text, '手动修改后离开的文案', '未采纳离开时也要暂存当前文案');

  const reopened = loadPage('contentwriting', shared);
  reopened.page.onLoad({ source: 'project_note' });
  reopened.page.receiveWritingContext(projectNoteContext('draft-a'));
  assert.equal(reopened.page.data.result, '手动修改后离开的文案');
});

test('adopting from the editor source never clears the project-note AI cache', async () => {
  const shared = { storage: new Map() };
  shared.storage.set('project_note_ai_result_v1', { schema: 1, account: 'session-one', project_id: 'project-9', draft_instance_id: 'draft-a', request_id: 2, operation: 'ai-result-draft-a-r2', title: '方案', text: '项目笔记文案', updated_at: Date.now() });
  const h = loadPage('contentwriting', shared);
  h.page.onLoad({ source: 'editor' });
  h.listeners.writingContext({ source: 'editor', content_id: 'c-1', revision: 1, title: '标题', facts: '资料' });
  h.page.setData({ result: '编辑器采纳的文案' });
  h.page.saveToDraft();
  const payload = h.emitted.find(item => item.event === 'prefillContent').value;
  h.listeners.writingAck({ state: 'applied', request: payload.request, operation: payload.operation });
  assert.equal(shared.storage.has('project_note_ai_result_v1'), true, '其他来源采纳不得清掉项目笔记缓存');
});

test('request ids never repeat across re-entry even when no result was stored', () => {
  const shared = { storage: new Map() };
  const first = loadPage('contentwriting', shared);
  first.page.onLoad({ source: 'project_note' });
  first.page.receiveWritingContext(projectNoteContext('draft-a'));
  let captured;
  first.aiStreamStub.startAIStream = options => { captured = options; return { abort() {} }; };
  first.page.quickGenerate();
  assert.equal(first.page._aiRequestID, 1);
  captured.onError('网络异常');
  assert.equal(shared.storage.has('project_note_ai_result_v1'), false, '失败请求不产生结果缓存');

  const second = loadPage('contentwriting', shared);
  second.page.onLoad({ source: 'project_note' });
  second.page.receiveWritingContext(projectNoteContext('draft-a'));
  let capturedAgain;
  second.aiStreamStub.startAIStream = options => { capturedAgain = options; return { abort() {} }; };
  second.page.quickGenerate();
  assert.equal(second.page._aiRequestID, 2, '无结果的中断请求也必须让下次请求拿到新 ID');
});

test('an expired cache entry is cleared even when its identity does not match', () => {
  const shared = { storage: new Map() };
  shared.storage.set('project_note_ai_result_v1', { schema: 1, account: 'another-account', project_id: 'project-other', draft_instance_id: 'draft-z', request_id: 5, text: '旧文案', updated_at: Date.now() - 31 * 60 * 1000 });
  const h = loadPage('contentwriting', shared);
  h.page.onLoad({ source: 'project_note' });
  h.page.receiveWritingContext(projectNoteContext('draft-a'));
  assert.equal(h.page.data.result, '');
  assert.equal(shared.storage.has('project_note_ai_result_v1'), false, '身份不匹配的过期记录也要清理');
});

test('a restored result keeps its operation id so re-adoption is recognised as the same operation', () => {
  const shared = { storage: new Map() };
  shared.storage.set('project_note_ai_result_v1', { schema: 1, account: 'session-one', project_id: 'project-9', draft_instance_id: 'draft-a', request_id: 3, operation: 'ai-result-draft-a-r3', title: '方案', text: '恢复的文案', updated_at: Date.now() });
  const h = loadPage('contentwriting', shared);
  h.page.onLoad({ source: 'project_note' });
  h.page.receiveWritingContext(projectNoteContext('draft-a'));
  assert.equal(h.page.data.result, '恢复的文案');
  h.page.saveToDraft();
  const payload = h.emitted.find(item => item.event === 'prefillContent').value;
  assert.equal(payload.operation, 'ai-result-draft-a-r3', '恢复后采纳必须沿用原 operation');
});

test('request ids and operations never wrap around after the result cache expires', () => {
  const shared = { storage: new Map() };
  const first = loadPage('contentwriting', shared);
  first.page.onLoad({ source: 'project_note' });
  first.page.receiveWritingContext(projectNoteContext('draft-a'));
  let captured;
  first.aiStreamStub.startAIStream = options => { captured = options; return { abort() {} }; };
  first.page.quickGenerate();
  captured.onDone('A 文案');
  const recordA = shared.storage.get('project_note_ai_result_v1');
  assert.equal(recordA.request_id, 1);
  const operationA = recordA.operation;

  // 超过有效期：结果缓存与序列都过期，但同一实例的请求计数不得回退
  const expiredAt = Date.now() - 31 * 60 * 1000;
  shared.storage.set('project_note_ai_result_v1', { ...recordA, updated_at: expiredAt });
  shared.storage.set('project_note_ai_request_seq_v1', { ...shared.storage.get('project_note_ai_request_seq_v1'), updated_at: expiredAt });

  const second = loadPage('contentwriting', shared);
  second.page.onLoad({ source: 'project_note' });
  second.page.receiveWritingContext(projectNoteContext('draft-a'));
  assert.equal(second.page.data.result, '', '过期结果不恢复');
  let capturedAgain;
  second.aiStreamStub.startAIStream = options => { capturedAgain = options; return { abort() {} }; };
  second.page.quickGenerate();
  capturedAgain.onDone('B 文案');
  const recordB = shared.storage.get('project_note_ai_result_v1');
  assert.equal(recordB.request_id, 2, '跨 TTL 的新请求不得回绕为 1');
  assert.notEqual(recordB.operation, operationA, '新结果不得复用旧 operation');
});

test('a restored result restores its title so the adopted payload stays identical', () => {
  const shared = { storage: new Map() };
  const first = loadPage('contentwriting', shared);
  first.page.onLoad({ source: 'project_note' });
  first.page.receiveWritingContext(projectNoteContext('draft-a', { title: '原笔记标题' }));
  first.page.setData({ title: 'AI 改过的标题' });
  let captured;
  first.aiStreamStub.startAIStream = options => { captured = options; return { abort() {} }; };
  first.page.quickGenerate();
  captured.onDone('AI 正文');
  const record = shared.storage.get('project_note_ai_result_v1');
  assert.equal(record.title, 'AI 改过的标题');

  const second = loadPage('contentwriting', shared);
  second.page.onLoad({ source: 'project_note' });
  second.page.receiveWritingContext(projectNoteContext('draft-a', { title: '原笔记标题' }));
  assert.equal(second.page.data.title, 'AI 改过的标题', '恢复结果必须同时恢复标题');
  second.page.saveToDraft();
  const payload = second.emitted.find(item => item.event === 'prefillContent').value;
  assert.equal(payload.title, 'AI 改过的标题');
  assert.equal(payload.operation, record.operation, '重放沿用同一 operation');
});

test('chat-based quick adjustments allocate a new request and operation and persist the result', () => {
  const shared = { storage: new Map() };
  const h = loadPage('contentwriting', shared);
  h.page.onLoad({ source: 'project_note' });
  h.page.receiveWritingContext(projectNoteContext('draft-a'));
  h.page.setData({ facts: '已知资料', result: '旧文案' });
  let captured;
  h.aiStreamStub.startAIStream = options => { captured = options; return { abort() {} }; };
  h.page.quickGenerate();
  captured.onDone('旧文案');
  const before = shared.storage.get('project_note_ai_result_v1');

  h.page.generateWriting();
  const chatNav = h.navigations[h.navigations.length - 1];
  assert.ok(chatNav && chatNav.events && chatNav.events.contentWritingResult, '快速调整仍走聊天页通道');
  chatNav.events.contentWritingResult({ text: '扩写后的新文案' });
  const after = shared.storage.get('project_note_ai_result_v1');
  assert.equal(after.text, '扩写后的新文案');
  assert.notEqual(after.operation, before.operation, '聊天生成的新结果必须换 operation');
  assert.ok(after.request_id > before.request_id, '聊天生成也必须递增请求编号');
});

test('an empty AI title keeps the actual adopted title across a lost ack and re-entry', () => {
  const shared = { storage: new Map() };
  const first = loadPage('contentwriting', shared);
  first.page.onLoad({ source: 'project_note' });
  first.page.receiveWritingContext(projectNoteContext('draft-a', { title: '笔记原标题' }));
  first.page.setData({ title: '' });
  let captured;
  first.aiStreamStub.startAIStream = options => { captured = options; return { abort() {} }; };
  first.page.quickGenerate();
  captured.onDone('AI 正文');
  const adoptedTitle = first.page.data.heading;
  const record = shared.storage.get('project_note_ai_result_v1');
  assert.equal(record.title, adoptedTitle, '缓存必须保存实际采纳标题而不是空串');
  first.page.saveToDraft();
  const firstPayload = first.emitted.find(item => item.event === 'prefillContent').value;
  assert.equal(firstPayload.title, adoptedTitle);
  // 模拟回执丢失后系统返回：页面卸载但未结算
  first.page.onUnload();

  const second = loadPage('contentwriting', shared);
  second.page.onLoad({ source: 'project_note' });
  second.page.receiveWritingContext(projectNoteContext('draft-a', { title: '笔记原标题' }));
  second.page.saveToDraft();
  const secondPayload = second.emitted.find(item => item.event === 'prefillContent').value;
  assert.equal(secondPayload.title, firstPayload.title, '重进后采纳标题必须与首次一致');
  assert.equal(secondPayload.operation, firstPayload.operation, '同一次采纳沿用同一 operation');
});

// T4.3：三个「快速调整」就地生成，不再跳 chat 页；多轮对话仍走独立入口。
test('quick adjustments stay on page while the deep chat entry still navigates', () => {
  const wxml = fs.readFileSync(path.join(pagesRoot, 'contentwriting/contentwriting.wxml'), 'utf8');
  const quick = wxml.match(/<button data-action="(?:concise|expand|tone)"[^>]*>/g) || [];
  assert.equal(quick.length, 3, '三个快速调整按钮');
  quick.forEach(tag => assert.match(tag, /bindtap="quickGenerate"/, '快速调整必须就地生成'));
  assert.match(wxml, /class="text-action deep-chat" bindtap="generateWriting"/, '继续对话仍走 chat');
});
