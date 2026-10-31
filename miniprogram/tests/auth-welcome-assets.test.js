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

test('登录与资料确认使用同一场景，编辑器持续挂载以保留选择结果', () => {
  const login = fs.readFileSync(path.join(sourceRoot, 'components/login-guard/login-guard.wxml'), 'utf8');
  const rootTag = login.match(/<scroll-view[^>]+>/)[0];
  assert.doesNotMatch(rootTag, /wx:if=/, '检查期间不能销毁正在上传或编辑资料的组件');
  const hidden = rootTag.match(/hidden="{{([^}]+)}}"/)[1];
  const hiddenFor = data => Function(...Object.keys(data), `return (${hidden});`)(...Object.values(data));
  const initial = { visible: true, checking: true, loggingIn: false, completingProfile: false };
  assert.equal(hiddenFor(initial), false, '核验期间保留同一品牌场景和加载态');
  assert.equal(hiddenFor({ ...initial, checking: false }), false);
  assert.equal(hiddenFor({ ...initial, visible: false, loggingIn: true }), false, '登录后核验继续使用同一背景');
  assert.equal(hiddenFor({ ...initial, visible: false, checking: false, completingProfile: true }), false);
  assert.equal(hiddenFor({ ...initial, visible: false, checking: false }), true, '资料完整后关闭场景');
  assert.match(login, /<nickname-guard[^>]+embedded="{{true}}"/);
  const page = fs.readFileSync(path.join(sourceRoot, 'pages/login/login.wxml'), 'utf8');
  assert.match(page, /<nickname-guard[^>]+embedded="{{true}}"/);
});
