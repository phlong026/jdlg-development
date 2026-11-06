'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const { fitNoteMedia, fitNoteBlocks, groupNoteImages, noteVideoMetadata } = require('../miniprogram/utils/project-note-media');

test('only consecutive images become a swipe gallery and text or video preserves document boundaries', () => {
  const images = [{ media_id: 'a', kind: 'image', width: 1600, height: 900 }, { media_id: 'b', kind: 'image', width: 900, height: 1600 }];
  const blocks = [images[0], images[1], { kind: 'paragraph', text: '一层说明' }, images[0], { kind: 'video', media_id: 'v' }, images[1]];
  const grouped = groupNoteImages(blocks, images, { windowWidth: 375, windowHeight: 667 });
  assert.deepEqual(grouped.map(item => item.kind), ['gallery', 'paragraph', 'image', 'video', 'image']);
  assert.deepEqual(grouped[0].items.map(item => item.media_id), ['a', 'b']);
  assert.ok(grouped[0].frameHeight <= Math.floor(667 * 0.62));
  assert.equal(blocks.length, 6);
});

test('gallery regrouping after resize retains every image and its caption', () => {
  const media = fitNoteMedia([{ media_id: 'a', kind: 'image', width: 1600, height: 900 }, { media_id: 'b', kind: 'image', width: 900, height: 1600 }]);
  const source = [{ kind: 'image', media_id: 'a', caption: '入口' }, { kind: 'image', media_id: 'b', caption: '庭院' }];
  const portrait = fitNoteBlocks(source, media, { windowWidth: 375, windowHeight: 667 });
  const landscape = fitNoteBlocks(portrait, media, { windowWidth: 667, windowHeight: 375 });
  assert.equal(landscape.length, 1);
  assert.deepEqual(landscape[0].items.map(item => item.caption), ['入口', '庭院']);
  assert.ok(landscape[0].frameHeight < portrait[0].frameHeight);
});

test('standalone media and galleries have stable unique rendering keys after resize', () => {
  const media = [{ media_id: 'a', kind: 'image' }, { media_id: 'v', kind: 'video' }, { media_id: 'b', kind: 'image' }, { media_id: 'c', kind: 'image' }, { media_id: 'file', kind: 'document' }];
  const first = groupNoteImages(media, media, { windowWidth: 375, windowHeight: 667 });
  const resized = groupNoteImages(media, media, { windowWidth: 667, windowHeight: 375 });
  assert.ok(fitNoteMedia(media).every(item => item.key));
  assert.ok(first.every(item => item.key));
  assert.equal(new Set(first.map(item => item.key)).size, first.length);
  assert.deepEqual(first.map(item => item.key), resized.map(item => item.key));
});

test('wide and portrait videos retain their aspect ratio inside the document and viewport', () => {
  const source = [
    { media_id: 'wide', kind: 'video', width: 1920, height: 1080 },
    { media_id: 'portrait', kind: 'video', width: 1080, height: 1920 },
    { media_id: 'square', kind: 'video', width: 1080, height: 1080 },
  ];
  for (const window of [{ windowWidth: 375, windowHeight: 667 }, { windowWidth: 320, windowHeight: 568 }, { windowWidth: 844, windowHeight: 390 }]) {
    const frames = fitNoteMedia(source, window);
    for (let index = 0; index < frames.length; index++) {
      const frame = frames[index], original = source[index];
      assert.ok(frame.frameWidth <= Math.floor(window.windowWidth * 636 / 750));
      assert.ok(frame.frameHeight <= Math.floor(window.windowHeight * 0.62));
      assert.ok(Math.abs(frame.frameWidth / frame.frameHeight - original.width / original.height) < 0.02);
      assert.equal(frame.width, original.width);
      assert.equal(frame.height, original.height);
    }
  }
  assert.equal(source[0].frameWidth, undefined);
});

test('images retain their original dimensions and very wide frames have no artificial minimum height', () => {
  const frames = fitNoteMedia([
    { media_id: 'drawing', kind: 'image', width: 800, height: 5000 },
    { media_id: 'wide-video', kind: 'video', width: 4000, height: 400 },
  ], { windowWidth: 375, windowHeight: 667 });
  assert.equal(frames[0].width, 800);
  assert.equal(frames[0].height, 5000);
  assert.ok(frames[1].frameHeight < 120);
});

test('missing dimensions use a bounded landscape frame and metadata corrects a portrait video', () => {
  const window = { windowWidth: 375, windowHeight: 667 };
  const fallback = fitNoteMedia([{ media_id: 'video-one', kind: 'video' }], window);
  assert.ok(Math.abs(fallback[0].frameWidth / fallback[0].frameHeight - 16 / 9) < 0.02);
  const media = noteVideoMetadata(fallback, { currentTarget: { dataset: { id: 'video-one' } }, detail: { width: 1080, height: 1920 } }, window);
  assert.ok(media[0].frameHeight > media[0].frameWidth);
  assert.ok(media[0].frameHeight <= Math.floor(667 * 0.62));
  const blocks = fitNoteBlocks([{ kind: 'heading', text: '方案' }, { kind: 'video', media_id: 'video-one', height: 200 }], media);
  assert.equal(blocks[1].frameHeight, media[0].frameHeight);
  assert.equal(blocks[0].text, '方案');
});

test('invalid metadata cannot replace stored dimensions or affect an unrelated video', () => {
  const media = [{ media_id: 'video-one', kind: 'video', width: 1920, height: 1080 }];
  for (const [id, width, height] of [['video-one', -1, 0], ['video-one', Infinity, 300], ['other', 1080, 1920]]) {
    const result = noteVideoMetadata(media, { currentTarget: { dataset: { id } }, detail: { width, height } }, { windowWidth: 375, windowHeight: 667 });
    assert.equal(result[0].width, 1920);
    assert.equal(result[0].height, 1080);
  }
});
