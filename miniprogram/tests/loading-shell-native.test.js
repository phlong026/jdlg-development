'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const { execFileSync } = require('node:child_process');

const root = path.resolve(__dirname, '../miniprogram');
const compiler = process.env.WXML_COMPILER || '/Applications/wechatwebdevtools.app/Contents/Resources/app.asar.unpacked/node_modules/wcc-exec/wcc';
function visibleNodes(node) {
  if (!node || node.attr?.hidden) return [];
  return [node, ...(node.children || []).flatMap(visibleNodes)];
}

test('资料核验只显示业务色加载框架，真实登录需要时仍显示登录界面', { skip: !fs.existsSync(compiler) }, () => {
  const code = execFileSync(compiler, ['./components/login-guard/login-guard.wxml', './templates/auth-welcome.wxml'], { cwd: root, encoding: 'utf8', timeout: 10000 });
  const context = { window: {}, console };
  vm.createContext(context);
  vm.runInContext(code, context);
  const render = context.$gwx('./components/login-guard/login-guard.wxml');
  const checking = visibleNodes(render({ checking: true }));
  assert.ok(checking.some(node => node.tag === 'wx-navigation-bar'), '核验期保留同尺寸导航框架');
  assert.equal(checking.find(node => node.tag === 'wx-navigation-bar').attr.back, false, '等待框架不能新增宿主页原本没有的返回动作');
  assert.ok(checking.includes('正在加载…'));
  assert.ok(!checking.some(node => node.attr?.class === 'login-guard-panel'), '核验不能闪现整页登录品牌画面');
  assert.ok(!checking.some(node => node.tag === 'wx-button'), '未核验时不渲染登录按钮');
  const login = visibleNodes(render({ visible: true, needsLogin: true }));
  assert.ok(login.includes('微信一键登录'));
  assert.ok(login.includes('暂不登录，先逛逛'));
  const approved = visibleNodes(render({}));
  assert.ok(!approved.some(node => node.tag === 'wx-navigation-bar' || node.tag === 'wx-button'), '通过后加载框架不盖住业务页');
});

for (const extension of ['ts', 'js']) {
  test(`${extension}: 导航首帧已使用设备尺寸，挂载时不再先挤压页面`, () => {
    let definition;
    const wx = {
      getSystemInfoSync: () => ({ statusBarHeight: 44, windowWidth: 390 }),
      getMenuButtonBoundingClientRect: () => ({ top: 48, bottom: 80, height: 32, left: 290 }),
    };
    vm.runInNewContext(fs.readFileSync(path.join(root, `components/navigation-bar/navigation-bar.${extension}`), 'utf8'), { Component(value) { definition = value; }, wx });
    assert.equal(definition.data.navSafeTop, 48);
    assert.equal(definition.data.navBarHeight, 92);
    const patches = [];
    const instance = { ...definition.methods, data: { ...definition.data }, setData(patch) { patches.push(patch); Object.assign(this.data, patch); } };
    definition.lifetimes.attached.call(instance);
    assert.equal(patches.length, 0, '相同尺寸不再触发二次布局');
    wx.getMenuButtonBoundingClientRect = () => ({ top: 24, bottom: 56, height: 32, left: 290 });
    instance._initNavLayout();
    assert.equal(instance.data.navBarHeight, 68, '窗口尺寸变化仍能更新');
  });
}
