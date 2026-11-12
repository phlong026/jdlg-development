const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const dir = path.join(__dirname, '../miniprogram/components/navigation-bar');

test('运行时导航样式覆盖实际WXML类名并约束安全区总高度', () => {
  const wxml = fs.readFileSync(path.join(dir, 'navigation-bar.wxml'), 'utf8');
  const css = fs.readFileSync(path.join(dir, 'navigation-bar.wxss'), 'utf8');
  for (const name of ['uni-nav', 'uni-nav-inner', 'uni-nav-side', 'uni-nav-back', 'uni-nav-chevron', 'uni-nav-title', 'uni-nav-title-main']) {
    assert.ok(wxml.includes(`class="${name}`), name);
    assert.ok(css.includes(`.${name} {`), `${name}必须有运行时样式，避免标题左移和返回按钮消失`);
  }
  assert.match(css, /\.uni-nav\s*\{[^}]*box-sizing:\s*border-box;/s);
  assert.match(css, /\.uni-nav-inner\s*\{[^}]*display:\s*flex;/s);
  assert.match(css, /\.uni-nav-title\s*\{[^}]*align-items:\s*center;/s);
});
