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
  const result = spawnSync(compiler, [file], { cwd: root, encoding: 'utf8', timeout: 10000, maxBuffer: 4 * 1024 * 1024 });
  if (result.error) throw result.error;
  return result;
}

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
