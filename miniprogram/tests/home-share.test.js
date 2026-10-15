const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const { test } = require('node:test');
const ts = require('./report-runtime')('typescript');

const root = path.resolve(__dirname, '..');
const title = '甲第灵光 · AI建房助手';

function loadPage(extension) {
  let definition;
  const menus = [];
  const warnings = [];
  const source = fs.readFileSync(path.join(root, `miniprogram/pages/index/index.${extension}`), 'utf8');
  const code = extension === 'ts'
    ? ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 } }).outputText
    : source;
  vm.runInNewContext(code, {
    exports: {},
    require: name => name.endsWith('costRegions')
      ? require('../miniprogram/utils/costRegions.js')
      : { resolveAssetPath: value => value },
    Page: value => { definition = value; },
    wx: { showShareMenu: options => menus.push(options) },
    console: { warn: (...args) => warnings.push(args) },
  });
  return { definition, menus, warnings };
}

for (const extension of ['ts', 'js']) {
  test(`${extension}: 首页分享使用建房助手标题、专用封面和首页路径`, () => {
    const { definition } = loadPage(extension);
    const share = definition.onShareAppMessage();
    assert.equal(share.title, title);
    assert.equal(share.path, '/pages/index/index');
    assert.equal(share.imageUrl, '/assets/share/home-chat.jpg');
  });

  test(`${extension}: 朋友圈使用方形封面并留在当前首页`, () => {
    const { definition } = loadPage(extension);
    const share = definition.onShareTimeline();
    assert.equal(share.title, title);
    assert.equal(share.imageUrl, '/assets/share/home-timeline.jpg');
    assert.equal(share.query, '');
    assert.equal(share.path, undefined);
  });

  test(`${extension}: 首页加载开启两种分享并保留原有初始化`, () => {
    const { definition, menus, warnings } = loadPage(extension);
    let safeAreaCalls = 0;
    let introCalls = 0;
    definition.onLoad.call({
      syncBrandSafeArea() { safeAreaCalls += 1; },
      showHomeIntroPopupOnce() { introCalls += 1; },
    });
    assert.equal(safeAreaCalls, 1);
    assert.equal(introCalls, 1);
    assert.equal(menus.length, 1);
    assert.deepEqual(Array.from(menus[0].menus), ['shareAppMessage', 'shareTimeline']);
    menus[0].fail({ errMsg: 'showShareMenu:fail' });
    assert.equal(warnings.length, 1);
    assert.equal(warnings[0][0], '首页分享菜单开启失败');
  });
}

test('两张分享封面在上传配置中显式包含，避免按未使用资源删除', () => {
  const config = JSON.parse(fs.readFileSync(path.join(root, 'project.config.json'), 'utf8'));
  assert.ok(config.packOptions.include.some(item => item.type === 'folder' && item.value === 'assets/share'));
  for (const name of ['home-chat.jpg', 'home-timeline.jpg']) {
    const bytes = fs.readFileSync(path.join(root, 'miniprogram/assets/share', name));
    assert.equal(bytes.readUInt16BE(0), 0xffd8);
    assert.ok(bytes.length < 300 * 1024, '封面应控制体积，避免占用过多代码包空间');
  }
});
