'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const vm = require('node:vm');
const { spawnSync } = require('node:child_process');

const compiler = process.env.WXML_COMPILER || '/Applications/wechatwebdevtools.app/Contents/Resources/app.asar.unpacked/node_modules/wcc-exec/wcc';
const available = fs.existsSync(compiler);
const root = path.resolve(__dirname, '../miniprogram');
function compile(file) {
  const shared = './components/project-note-document/note-document.wxml';
  const files = /pages\/(?:projectnotes|projectnoteview|projectpackageview)\//.test(file) ? [file, shared] : /pages\/(?:contentedit|contentreader)\//.test(file) ? [file, './components/content-presentation/content-presentation.wxml'] : [file];
  const result = spawnSync(compiler, files, { cwd: root, encoding: 'utf8', timeout: 10000, maxBuffer: 4 * 1024 * 1024 });
  if (result.error) throw result.error;
  return result;
}

test('project draft preview and both recipient readers render the same ordered document', { skip: !available }, () => {
  const blocks = [
    { key: 'heading', kind: 'heading', text: '空间布局', className: 'article-heading article-heading-2' },
    { key: 'quote', kind: 'quote', text: '保留原有庭院', className: 'article-quote' },
    { key: 'list', kind: 'list', ordered: true, items: ['客厅朝南', '老人房靠近入口'], className: 'article-list' },
    { key: 'image', kind: 'image', media_id: 'image-one', local_path: '/tmp/plan.png', height: 320, caption: '一层平面图' },
  ];
  for (const page of ['projectnotes', 'projectnoteview', 'projectpackageview']) {
    const result = compile(`./pages/${page}/${page}.wxml`);
    assert.equal(result.status, 0, result.stderr || result.stdout);
    const context = { window: {}, console };
    vm.createContext(context);
    vm.runInContext(result.stdout, context);
    const version = { title: '庭院方案', revision: 2, body_format: 'article_v2', body: '{}' };
    const data = {
      __profileAccessMounted: true, loading: false, selectedProject: { name: '示例项目' }, pageView: page === 'projectnotes' ? 'editor' : 'version',
      editorMode: 'preview', noteTitle: version.title, previewBodyFormat: version.body_format, previewBody: version.body, previewBlocks: blocks, previewMedia: [],
      view: { project: { name: '示例项目' }, version }, versionView: { version }, directory: { project: { name: '示例项目' } }, articleBlocks: blocks, media: [],
    };
    const rendered = JSON.stringify(context.$gwx(`./pages/${page}/${page}.wxml`)(data));
    for (const text of ['空间布局', '保留原有庭院', '客厅朝南', '老人房靠近入口', '一层平面图']) assert.ok(rendered.includes(text), page + ': ' + text);
    assert.ok(rendered.indexOf('空间布局') < rendered.indexOf('保留原有庭院'), page + ': block order');
    assert.match(rendered, /article-heading/);
    assert.match(rendered, /article-quote/);
  }
});

test('compiled multi-image preview presents the selected caption and native swiper', { skip: !available }, () => {
  const result = compile('./pages/projectnotes/projectnotes.wxml');
  assert.equal(result.status, 0, result.stderr || result.stdout);
  const context = { window: {}, console };
  vm.createContext(context); vm.runInContext(result.stdout, context);
  const tree = context.$gwx('./pages/projectnotes/projectnotes.wxml')({
    __profileAccessMounted: true, pageView: 'editor', editorMode: 'preview', selectedProject: { name: '示例项目' }, noteTitle: '庭院方案',
    previewBodyFormat: 'article_v2', galleryPositions: { 'note-gallery-0': 1 }, previewBlocks: [{ kind: 'gallery', key: 'note-gallery-0', frameHeight: 400, items: [
      { media_id: 'image-one', galleryItemKey: 'image-0', local_path: '/tmp/one.png', caption: '第一张的说明' },
      { media_id: 'image-two', galleryItemKey: 'image-1', local_path: '/tmp/two.png', caption: '第二张的说明' },
    ] }],
  });
  const rendered = JSON.stringify(tree);
  assert.match(rendered, /note-gallery-swiper/);
  assert.match(rendered, /第二张的说明/);
  assert.doesNotMatch(rendered, /第一张的说明/);
  assert.match(rendered, /aspectFit/);
});

test('native WXML compiler reproduces the escaped-operator runtime error', { skip: !available }, () => {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'phase3-wxml-'));
  try {
    const file = path.join(directory, 'conditional.wxml');
    fs.writeFileSync(file, '<view wx:if="{{ready &amp;&amp; enabled}}">内容</view>');
    assert.notEqual(compile(file).status, 0);
    fs.writeFileSync(file, '<view wx:if="{{ready && enabled}}">内容</view>');
    assert.equal(compile(file).status, 0);
  } finally { fs.rmSync(directory, { recursive: true, force: true }); }
});

for (const page of ['my', 'chat', 'contentworkbench', 'contentedit', 'contentreader', 'contentwriting', 'nativereferences', 'sharevisits', 'visitdetail', 'messagecenter', 'projectnotes', 'projectnoteview', 'projectcollections', 'collectionreader', 'projectpackage', 'projectpackageview', 'projectinvite']) {
  test('native WXML compiler accepts ' + page, { skip: !available }, () => {
    const result = compile(`./pages/${page}/${page}.wxml`);
    assert.equal(result.status, 0, result.stderr || result.stdout);
  });
}

test('content workbench keeps filters mounted while only the authorized list reloads', { skip: !available }, () => {
  const result = compile('./pages/contentworkbench/contentworkbench.wxml');
  assert.equal(result.status, 0, result.stderr || result.stdout);
  const context = { window: {}, console };
  vm.createContext(context);
  vm.runInContext(result.stdout, context);
  const tree = context.$gwx('./pages/contentworkbench/contentworkbench.wxml')({
    __profileAccessMounted: true, view: 'studio', loginRequired: false, denied: false, disabled: false,
    loading: true, loadingMore: false, error: '', context: { owner_choices: [
      { owner_id: 'company', display_name: '公司空间' }, { owner_id: 'personal', display_name: '个人空间' },
    ], stones_balance: -1, month_generations: -1 },
    activeProjects: [], hasShareVisitAccess: true, ownerFilter: 'personal', keyword: '庭院',
    categories: [{ key: 'all', label: '全部' }], category: 'all', searchTerm: '', next_cursor: '',
    visibleItems: [{ content_id: 'old-owner-row', title: '旧归属内容', kind: 'article', kind_label: '图文', status: 'draft' }],
  });
  const rendered = JSON.stringify(tree);
  assert.match(rendered, /owner-tabs/);
  assert.match(rendered, /search-row/);
  assert.match(rendered, /content-list-loading/);
  assert.doesNotMatch(rendered, /旧归属内容/);
  assert.doesNotMatch(rendered, /暂无内容|未找到相关内容|暂无这类内容/);
});

for (const kind of ['gallery', 'video', 'article']) {
 test('compiled editor preview and reader share ' + kind + ' content', {skip: !available}, () => {
  const {buildContentPresentation} = require('../miniprogram/utils/content-presentation');
  const blocks = kind === 'article' ? [{kind:'heading',text:'空间布局',level:2},{kind:'quote',text:'保留庭院'},{kind:'paragraph',text:'完整发布文案'}] : [{kind:'paragraph',text:'完整发布文案'}, {kind:kind === 'gallery' ? 'image' : 'video',media_id:'media-one'}];
  if(kind === 'gallery') blocks.push({kind:'image',media_id:'media-two'});
  const content = {title:'原生排版验证',body_format:'article_v2',body:JSON.stringify({schema:1,presentation:kind,blocks}),cover:{media_id:'media-one'}};
  const media = ['media-one','media-two'].map(media_id=>({media_id,kind:kind === 'video' ? 'video':'image',url:'/tmp/'+media_id,width:1600,height:900}));
  const presentationView = buildContentPresentation(content,media,{windowWidth:390,windowHeight:844});
  for(const page of ['contentedit','contentreader']) {
   const result = compile(`./pages/${page}/${page}.wxml`);
   assert.equal(result.status,0,result.stderr);
   const context={window:{},console};vm.createContext(context);vm.runInContext(result.stdout,context);
   const data={__profileAccessMounted:true,loading:false,preview:{},contentReady:true,presentationView,mediaErrors:{},presentationMediaErrors:{},content,context:{owner_choices:[]}};
   const rendered=JSON.stringify(context.$gwx(`./pages/${page}/${page}.wxml`)(data));
   assert.ok(rendered.includes('原生排版验证'),page);
   assert.equal(rendered.split('完整发布文案').length-1,1,page+' body once');
   if(kind === 'gallery') {assert.match(rendered,/presentation-gallery/);assert.match(rendered,/media-one/);assert.match(rendered,/media-two/);}
   if(kind === 'video') {assert.match(rendered,/presentation-video/);assert.match(rendered,/player-key/);}
   if(kind === 'article') {assert.match(rendered,/空间布局/);assert.match(rendered,/保留庭院/);}
  }
 });
}

test('editor more actions open beside the status bar before the long form', { skip: !available }, () => {
  const result = compile('./pages/contentedit/contentedit.wxml');
  assert.equal(result.status, 0, result.stderr);
  const context = { window: {}, console };
  vm.createContext(context); vm.runInContext(result.stdout, context);
  const render = context.$gwx('./pages/contentedit/contentedit.wxml');
  const data = { __profileAccessMounted: true, loading: false, id: 'fixture-draft', status: 'draft', version: 0, moreExpanded: true, presentation: 'gallery', context: { owner_choices: [] }, imageBlocks: [], videoBlocks: [], media: [] };
  const expanded = JSON.stringify(render(data));
  assert.match(expanded, /editor-more/);
  assert.ok(expanded.indexOf('more-menu') < expanded.indexOf('mode-tabs'), 'opened actions must be reachable above the long editor');
  assert.ok(expanded.indexOf('删除草稿') < expanded.indexOf('mode-tabs'));
  assert.doesNotMatch(JSON.stringify(render({ ...data, moreExpanded: false })), /删除草稿/);
  for (const patch of [{ preview: {} }, { deleted: true }, { documentReadOnly: true }, { legacyReadOnly: true }]) {
    const hidden = JSON.stringify(render({ ...data, ...patch }));
    assert.doesNotMatch(hidden, /editor-more|more-menu/);
  }
});
