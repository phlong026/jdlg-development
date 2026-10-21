const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const root = path.resolve(__dirname, '../miniprogram');
const markup = fs.readFileSync(path.join(root, 'pages/index/index.wxml'), 'utf8');
const styles = fs.readFileSync(path.join(root, 'pages/index/index.scss'), 'utf8');
const rules = new Map([...styles.matchAll(/([^{}\n]+)\s*\{([^{}]*)\}/g)].map(match => [match[1].trim(), match[2].trim()]));
const value = (selector, property) => {
  const match = rules.get(selector).match(new RegExp(`${property}:\\s*([\\d.]+)rpx;`));
  assert.ok(match, `${selector} 缺少 ${property}`);
  return Number(match[1]);
};

for (const width of [320, 375, 390, 430]) {
  const scale = width / 750;
  const cardBottom = (value('.all-card', 'top') + value('.service-card', 'height')) * scale;
  const heroBottom = value('.home-canvas', 'height') * scale;
  assert.ok(cardBottom + 24 * scale <= heroBottom, '四张卡片下方保留间距');
  const formBottom = (value('.estimate-note', 'top') + value('.estimate-note', 'height')) * scale;
  assert.ok(formBottom < value('.cost-estimator', 'height') * scale, '隐私说明不得覆盖精选案例');
}
assert.match(styles, /env\(safe-area-inset-bottom\)/);
assert.doesNotMatch(styles, /filter:\s*blur|image-rendering:\s*pixelated/);
assert.match(markup, /aria-label="别墅工程"/);
assert.match(markup, /aria-label="室内装修"/);
assert.match(markup, /点击按钮视为同意/);
assert.match(markup, /data-url="\/pages\/privacy\/privacy\?type=privacy"/);
assert.doesNotMatch(markup, /估算结果仅供参考/);
console.log('Restored cards, estimator and cases do not overlap, with safe bottom padding and privacy entry');
