'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const root = path.join(__dirname, '../miniprogram');
const { renderArticleDocument } = require(path.join(root, 'utils/article-document'));

test('article renderer keeps text and repeated media in document order on a narrow screen', () => {
  const body = JSON.stringify({ schema: 1, blocks: [
    { kind: 'heading', level: 2, text: '户型说明', style: { align: 'center', spacing_after: 12 } },
    { kind: 'image', media_id: 'asset-0001', caption: '一层平面图' },
    { kind: 'paragraph', text: '中间的解说' },
    { kind: 'image', media_id: 'asset-0001' },
    { kind: 'video', media_id: 'asset-0002' },
    { kind: 'video', media_id: 'asset-0002' },
  ] });
  const media = [
    { media_id: 'asset-0001', kind: 'image', url: 'https://example.test/image', width: 1200, height: 800 },
    { media_id: 'asset-0002', kind: 'video', url: 'https://example.test/video', width: 1280, height: 720 },
  ];
  const blocks = renderArticleDocument(body, media, 280);
  assert.deepEqual(blocks.map(block => block.kind), ['heading', 'image', 'paragraph', 'image', 'video', 'video']);
  assert.equal(blocks[1].height, 187);
  assert.equal(blocks[3].domId, 'article-media-3');
  assert.equal(blocks[4].domId, 'article-media-4');
  assert.notEqual(blocks[4].domId, blocks[5].domId);
  assert.notEqual(blocks[4].playerKey, blocks[5].playerKey);
  assert.notEqual(blocks[4].errorKey, blocks[5].errorKey);
  assert.equal(blocks[4].media_id, blocks[5].media_id);
  assert.match(blocks[0].className, /(?:^| )article-heading(?: |$)/);
  assert.match(blocks[0].className, /article-align-center/);
  assert.equal(blocks[0].style, 'margin-bottom:12px');
});

test('article renderer rejects missing media instead of silently dropping part of the article', () => {
  const body = JSON.stringify({ schema: 1, blocks: [{ kind: 'image', media_id: 'asset-0001' }] });
  assert.throws(() => renderArticleDocument(body, [], 320), /媒体/);
});

test('reader has an article branch and does not append article media as legacy attachments', () => {
  const wxml = fs.readFileSync(path.join(root, 'pages/contentreader/contentreader.wxml'), 'utf8');
  assert.match(wxml, /body_format === 'article_v2'/);
  assert.match(wxml, /wx:for="\{\{articleBlocks\}\}"/);
  assert.match(wxml, /body_format !== 'article_v2'/);
  assert.match(wxml, /data-player-key="\{\{item\.playerKey\}\}"/);
});
