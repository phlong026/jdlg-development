const fs = require('node:fs');
const path = require('node:path');
const assert = require('node:assert/strict');
const { test } = require('node:test');

const root = path.resolve(__dirname, '..');
const sourceRoot = path.join(root, 'miniprogram');

test('欢迎场景素材随小程序打包，首次登录不依赖远程图片', () => {
  const template = fs.readFileSync(path.join(sourceRoot, 'templates/auth-welcome.wxml'), 'utf8');
  const assets = [...template.matchAll(/src="(\/assets\/welcome\/[^"{}]+)"/g)].map(match => match[1]);
  assert.equal(assets.length, 3, '庭院、原版品牌 Logo 和头像示例均使用本地素材');
  const config = JSON.parse(fs.readFileSync(path.join(root, 'project.config.json'), 'utf8'));
  let total = 0;
  for (const asset of assets) {
    const relative = asset.slice(1);
    total += fs.statSync(path.join(sourceRoot, relative)).size;
    assert.ok(!config.packOptions.ignore.some(rule => rule.value === relative || (rule.type === 'folder' && relative.startsWith(rule.value + '/'))), `${relative} 不能被打包排除`);
  }
  assert.ok(total < 300 * 1024, '欢迎素材合计小于 300 KB，控制主包增长');
  assert.doesNotMatch(template, /好友|赞了|\d+\s*人/);
});

test('品牌沿用原 Logo，欢迎页不引入另一套字体', () => {
  const template = fs.readFileSync(path.join(sourceRoot, 'templates/auth-welcome.wxml'), 'utf8');
  assert.match(template, /class="aw-brand-logo"/);
  for (const file of ['styles/_auth-welcome.scss', 'components/login-guard/login-guard.scss', 'components/nickname-guard/nickname-guard.scss']) {
    assert.doesNotMatch(fs.readFileSync(path.join(sourceRoot, file), 'utf8'), /font-family|@font-face/);
  }
});

test('资料填写继续使用原生头像选择、昵称输入和表单提交', () => {
  const template = fs.readFileSync(path.join(sourceRoot, 'components/nickname-guard/nickname-guard.wxml'), 'utf8');
  assert.match(template, /open-type="chooseAvatar"/);
  assert.match(template, /type="nickname"/);
  assert.match(template, /bindsubmit="onConfirm"/);
  assert.match(template, /scroll-y/);
});
