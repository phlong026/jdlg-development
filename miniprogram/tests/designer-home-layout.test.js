const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const stylePath = path.join(__dirname, '../miniprogram/pages/designerhome/designerhome.scss');

test('设计师主页拥有独立完整的页面布局样式', () => {
  const style = fs.readFileSync(stylePath, 'utf8');

  assert.doesNotMatch(style, /@import\s+['"]\.\.\/distribution\/distribution\.scss['"]/);
  for (const selector of [
    '.page-content',
    '.content',
    '.designer-panel-card',
    '.designer-panel-tabs',
    '.designer-work-card',
    '.designer-service-row',
    '.designer-review-item',
    '.designer-empty-tip',
  ]) {
    assert.match(style, new RegExp(selector.replace('.', '\\.')));
  }
});

test('设计师主页约束长文本和窄屏操作区', () => {
  const style = fs.readFileSync(stylePath, 'utf8');

  assert.match(style, /\.designer-profile-info\s*\{[^}]*min-width:\s*0/s);
  assert.match(style, /\.designer-service-value\s*\{[^}]*overflow-wrap:\s*anywhere/s);
  assert.match(style, /\.profile-action-button\s*\{[^}]*min-width:/s);
  assert.match(style, /\.designer-work-image\s*\{[^}]*aspect-ratio:/s);
});
