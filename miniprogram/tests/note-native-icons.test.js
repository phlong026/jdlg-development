'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const root = path.resolve(__dirname, '../miniprogram');
const read = file => fs.readFileSync(path.join(root, file), 'utf8');

test('笔记阅读页使用本地原生图标并保留交互语义', () => {
  const config = JSON.parse(read('pages/contentreader/contentreader.json'));
  const wxml = read('pages/contentreader/contentreader.wxml');
  assert.equal(config.usingComponents['mini-icon'], '/components/mini-icon/mini-icon');
  for (const name of ['share-up', 'more', 'edit', 'heart', 'heart-filled', 'star', 'star-filled', 'comment', 'copy']) {
    assert.match(wxml, new RegExp(`name=(?:"${name}"|"\\{\\{[^"}]*${name})`), `阅读页应使用 ${name} 图标`);
  }
  assert.match(wxml, /open-type="share"[^>]*aria-label="分享笔记"/);
  assert.match(wxml, /bindtap="toggleNoteLike"[^>]*disabled="\{\{interactionBusy \|\| !interactionAvailable\}\}"/);
  assert.match(wxml, /bindtap="toggleNoteFavorite"[^>]*disabled="\{\{interactionBusy \|\| !interactionAvailable\}\}"/);
  assert.match(wxml, /bindtap="scrollToNoteComments"[^>]*aria-label="查看评论"/);
  assert.doesNotMatch(wxml, /[↗♡♥☆★✎⌘]|>•••<|>···</);
});

test('mini-icon 声明属性并提供线性与选中实心图标', () => {
  const script = read('components/mini-icon/mini-icon.js');
  const template = read('components/mini-icon/mini-icon.wxml');
  const styles = read('components/mini-icon/mini-icon.wxss');
  for (const property of ['name', 'size', 'color']) assert.match(script, new RegExp(`${property}:\\s*\\{`));
  assert.match(template, /color:\{\{color\}\}/);
  for (const name of ['share-up', 'more', 'edit', 'copy', 'image', 'word', 'link', 'settings', 'delete', 'heart', 'heart-filled', 'star', 'star-filled', 'comment']) {
    assert.match(styles, new RegExp(`\\.mi-${name}\\b`), `mini-icon 缺少 ${name}`);
  }
  assert.match(styles, /mask-image:\s*url\("data:image\/svg\+xml/);
  assert.doesNotMatch(styles, /url\(["']?https?:\/\//);
});

test('离线原生预览复制 mini-icon 完整组件', () => {
  const preview = fs.readFileSync(path.resolve(__dirname, '../scripts/preview-note-layout.js'), 'utf8');
  for (const ext of ['js', 'json', 'wxml', 'wxss']) {
    assert.ok(preview.includes(`components/mini-icon/mini-icon.${ext}`), `预览夹具缺少 mini-icon.${ext}`);
  }
});
