const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const crypto = require('node:crypto');
const root = path.resolve(__dirname, '..');
const runtimeRoot = path.join(root, 'miniprogram');
const manifest = require('../assets-cloud-manifest.json');
const { resolveAssetPath } = require('../miniprogram/utils/asset');

function isIgnored(config, name) {
  return config.packOptions.ignore.some(rule => rule.type === 'folder'
    ? name.startsWith(rule.value + '/') : name === rule.value);
}

test('云端发布清单锁定所有图片原件，运行时解析到相同版本', () => {
  assert.equal(manifest.assets.length, 45);
  for (const item of manifest.assets) {
    const bytes = fs.readFileSync(path.join(runtimeRoot, item.path));
    assert.equal(bytes.length, item.bytes);
    assert.equal(crypto.createHash('sha256').update(bytes).digest('hex'), item.sha256);
    assert.equal(resolveAssetPath('/' + item.path), item.url);
    assert.ok(item.url.startsWith(manifest.baseUrl + '/'));
  }
});

test('两个项目入口都只保留微信要求的五张本地导航图，不打入其他云图', () => {
  const app = require('../miniprogram/app.json');
  const nativeIcons = new Set(app.tabBar.list.flatMap(item => [item.iconPath, item.selectedIconPath]));
  assert.equal(nativeIcons.size, 5);
  for (const file of ['project.config.json', 'miniprogram/project.config.json']) {
    const config = JSON.parse(fs.readFileSync(path.join(root, file)));
    assert.deepEqual(config.packOptions.include, []);
    for (const item of manifest.assets) {
      assert.equal(isIgnored(config, item.path), !nativeIcons.has(item.path), `${file}: ${item.path}`);
    }
  }
});

test('聊天页和邀请海报通过云端地址加载图片，不遗留被排除的包内路径', () => {
  for (const page of ['customerservice', 'specadvisor']) {
    const markup = fs.readFileSync(path.join(runtimeRoot, 'pages', page, page + '.wxml'), 'utf8');
    assert.doesNotMatch(markup, /src=["']\/?assets\//);
  }
  const invite = fs.readFileSync(path.join(runtimeRoot, 'pages/Invitefriends/Invitefriends.ts'), 'utf8');
  assert.match(invite, /INVITE_POSTER_LOGO\s*=\s*resolveAssetPath\(/);
});
