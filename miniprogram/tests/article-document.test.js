'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const root = path.join(__dirname, '../miniprogram');
const { renderArticleDocument, preserveLayoutBody } = require(path.join(root, 'utils/article-document'));

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

test('article renderer keeps sanitized layout and splits nested video into a native segment', () => {
  const body = JSON.stringify({ schema: 1, layout_html: '<section style="padding:12px"><p>前文</p><img data-media-id="asset-0001"><table style="width:100%"><tbody><tr><td style="background-color:#fff"><blockquote style="border-left:2px solid #ddd">引用<jd-video data-media-id="asset-0002" style="margin:8px"></jd-video>后文</blockquote></td></tr></tbody></table></section>', blocks: [{ kind: 'paragraph', text: '兼容正文' }] });
  const media = [
    { media_id: 'asset-0001', kind: 'image', url: 'https://example.test/image?a=1&b=2', width: 1200, height: 800 },
    { media_id: 'asset-0002', kind: 'video', url: 'https://example.test/video', width: 1280, height: 720 },
  ];
  const blocks = renderArticleDocument(body, media, 280);
  assert.deepEqual(blocks.map(block => block.kind), ['layout', 'video', 'layout']);
  assert.match(blocks[0].html, /<img src="https:\/\/example\.test\/image\?a=1&amp;b=2">/);
  assert.match(blocks[0].html, /前文/);
  assert.equal(blocks[1].media_id, 'asset-0002');
  assert.match(blocks[1].style, /padding:12px/);
  assert.match(blocks[1].style, /background-color:#fff/);
  assert.match(blocks[1].style, /border-left:2px solid #ddd/);
  assert.match(blocks[1].style, /margin:8px/);
  for (const tag of ['section', 'table', 'tbody', 'tr', 'td', 'blockquote']) assert.match(blocks[1].className, new RegExp(`article-layout-native-${tag}`));
  assert.match(blocks[2].html, /后文/);
  assert.doesNotMatch(blocks.map(block => block.html || '').join(''), /data-media-id|jd-video/);
  const balanced = html => {
    const stack = [];
    for (const token of html.match(/<[^>]*>/g) || []) {
      const match = /^<\/?([a-z0-9-]+)/.exec(token); if (!match || ['img', 'br', 'hr'].includes(match[1])) continue;
      if (token.startsWith('</')) assert.equal(stack.pop(), match[1], `unbalanced fragment: ${html}`); else stack.push(match[1]);
    }
    assert.deepEqual(stack, [], `unclosed fragment: ${html}`);
  };
  blocks.filter(block => block.kind === 'layout').forEach(block => balanced(block.html));
  assert.match(blocks[0].html, /<section[^>]*>.*<blockquote[^>]*>引用<\/blockquote><\/td><\/tr><\/tbody><\/table><\/section>/);
  assert.match(blocks[2].html, /^<section[^>]*><table[^>]*><tbody><tr><td[^>]*><blockquote[^>]*>后文<\/blockquote>/);
});

test('article renderer rejects unsafe or incomplete layout at the client boundary', () => {
  const media = [{ media_id: 'asset-0001', kind: 'image', url: 'https://example.test/image' }];
  for (const layout of ['<script>alert(1)</script>', '<img src="https://evil.test/x">', '<img data-media-id="asset-missing">']) {
    const body = JSON.stringify({ schema: 1, layout_html: layout, blocks: [{ kind: 'paragraph', text: '兼容正文' }] });
    assert.throws(() => renderArticleDocument(body, media, 320), /排版|媒体/);
  }
});

test('article editor save keeps the complete imported layout body byte-for-byte', () => {
  const original = JSON.stringify({ schema: 1, layout_html: '<p>原排版</p>', blocks: [{ kind: 'paragraph', text: '兼容正文' }] });
  const rebuilt = JSON.stringify({ schema: 1, blocks: [{ kind: 'paragraph', text: '被扁平化' }] });
  assert.equal(preserveLayoutBody(original, rebuilt), original);
  assert.equal(preserveLayoutBody(rebuilt, rebuilt), rebuilt);
});

test('reader delegates all presentation kinds to the shared template', () => {
  const wxml = fs.readFileSync(path.join(root, 'pages/contentreader/contentreader.wxml'), 'utf8');
  const presentation = fs.readFileSync(path.join(root, 'components/content-presentation/content-presentation.wxml'), 'utf8');
  assert.match(wxml, /template is="content-presentation"/);
  assert.match(presentation, /view\.kind === 'gallery'/);
  assert.match(presentation, /data-player-key="\{\{item\.playerKey\}\}"/);
  assert.match(presentation, /item\.kind === 'layout'/);
	const editor = fs.readFileSync(path.join(root, 'pages/contentedit/contentedit.js'), 'utf8');
	const editorView = fs.readFileSync(path.join(root, 'pages/contentedit/contentedit.wxml'), 'utf8');
	assert.match(editor, /preserveLayoutBody\(this\.data\.layoutBody/);
	assert.match(editor, /layoutMediaIDs/);
	assert.match(editorView, /正文保留原排版/);
});
