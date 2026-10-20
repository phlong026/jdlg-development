const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { test } = require('node:test');

const root = path.resolve(__dirname, '..');
const wxml = fs.readFileSync(path.join(root, 'miniprogram/pages/templatesquaredetails/templatesquaredetails.wxml'), 'utf8');

// 契约测试：详情页最强的两个转化动作（收藏、手机号授权）必须接入可确认的链路。
for (const extension of ['ts', 'js']) {
  const source = fs.readFileSync(path.join(root, `miniprogram/pages/templatesquaredetails/templatesquaredetails.${extension}`), 'utf8');
  const onLike = source.slice(source.indexOf('onLike()'), source.indexOf('onDesignerTap()'));

  test(`${extension}: 收藏走 /favorites（会产生服务端收藏回执），不再调用 /like`, () => {
    assert.ok(onLike.length > 0, '找到 onLike');
    assert.match(onLike, /toggleFavorite\)?\(\s*['"]template['"]/);
    assert.doesNotMatch(onLike, /\/like/);
  });

  test(`${extension}: 手机号授权按钮绑定的方法在页面中有定义`, () => {
    const bound = /bindgetphonenumber="(\w+)"/.exec(wxml);
    assert.ok(bound, 'wxml 里有手机号授权按钮');
    assert.match(source, new RegExp(`\\b${bound[1]}\\s*\\(`));
    assert.match(source, /verifyEnterpriseWechatPhoneCode\s*\(/);
    assert.match(source, /\/user\/ai\/wecom\/verify-phone/);
  });
}
