'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const root = path.join(__dirname, '../miniprogram');
const { inferContentPresentation, buildContentPresentation } = require(path.join(root, 'utils/content-presentation'));

const image = (id, caption = '') => ({ media_id: id, kind: 'image', url: `https://example.test/${id}.jpg`, width: 900, height: 1200, caption });
const video = (id, width = 1920, height = 1080) => ({ media_id: id, kind: 'video', url: `https://example.test/${id}.mp4`, width, height });
const article = blocks => ({ title: '乡墅日记', body_format: 'article_v2', body: JSON.stringify({ schema: 1, blocks }) });

test('presentation inference keeps rich layouts and interleaved article blocks as articles', () => {
  assert.equal(inferContentPresentation({ ...article([{ kind: 'heading', level: 2, text: '户型' }, { kind: 'image', media_id: 'image-a' }]) }), 'article');
  assert.equal(inferContentPresentation(article([{ kind: 'paragraph', text: '居中文案', style: { align: 'center' } }, { kind: 'image', media_id: 'image-a' }])), 'article');
  assert.equal(inferContentPresentation({ ...article([{ kind: 'image', media_id: 'image-a' }, { kind: 'paragraph', text: '图间说明' }, { kind: 'image', media_id: 'image-b' }]) }), 'article');
  assert.equal(inferContentPresentation({ title: '原排版', body_format: 'article_v2', body: JSON.stringify({ schema: 1, presentation: 'gallery', layout_html: '<p>原排版</p>', blocks: [{ kind: 'paragraph', text: '兼容' }] }) }), 'article');
  assert.equal(inferContentPresentation({ title: '指定文章', body_format: 'article_v2', body: JSON.stringify({ schema: 1, presentation: 'article', blocks: [{ kind: 'image', media_id: 'image-a' }] }) }), 'article');
  assert.equal(inferContentPresentation({ title: '冲突呈现', presentation: 'gallery', body_format: 'article_v2', body: JSON.stringify({ schema: 1, presentation: 'article', blocks: [{ kind: 'image', media_id: 'image-a' }] }) }), 'article');
});

test('presentation inference recognizes explicit and safe legacy gallery or video notes', () => {
  assert.equal(inferContentPresentation(article([{ kind: 'paragraph', text: '整套方案' }, { kind: 'image', media_id: 'image-a' }, { kind: 'image', media_id: 'image-b' }])), 'gallery');
  assert.equal(inferContentPresentation(article([{ kind: 'paragraph', text: '建造过程' }, { kind: 'video', media_id: 'video-a' }])), 'video');
  assert.equal(inferContentPresentation(article([{ kind: 'image', media_id: 'image-a' }, { kind: 'paragraph', text: '图间说明' }, { kind: 'video', media_id: 'video-a' }])), 'article');
  assert.equal(inferContentPresentation({ ...article([{ kind: 'image', media_id: 'image-a' }]), presentation: 'video' }), 'video');
});

test('gallery view shows one note body and only keeps genuinely distinct image captions', () => {
  const content = article([
    { kind: 'paragraph', text: '村里第一栋现代别墅。' },
    { kind: 'image', media_id: 'image-a', caption: '全屋实景' },
    { kind: 'image', media_id: 'image-b', caption: '全屋实景' },
  ]);
  const source = [image('image-a', '全屋实景'), image('image-b', '全屋实景')];
  const before = JSON.stringify({ content, source });
  const view = buildContentPresentation(content, source, { windowWidth: 390, windowHeight: 844 });
  assert.equal(view.kind, 'gallery');
  assert.equal(view.bodyText, '村里第一栋现代别墅。\n\n全屋实景');
  assert.deepEqual(view.media.map(item => item.caption), ['', '']);
  assert.equal(view.media.length, 2);
  assert.deepEqual(buildContentPresentation(content, source.slice().reverse(), { windowWidth: 390, windowHeight: 844 }).media.map(item => item.media_id), ['image-a', 'image-b'], '图集顺序必须跟随正文而不是素材接口');
  assert.equal(view.galleryIndex, 0);
  assert.equal(JSON.stringify({ content, source }), before, 'builder must not mutate saved content or authenticated media');

  const distinct = buildContentPresentation(article([
    { kind: 'paragraph', text: '方案细节' },
    { kind: 'image', media_id: 'image-a', caption: '南立面' },
    { kind: 'image', media_id: 'image-b', caption: '室内' },
  ]), [image('image-a'), image('image-b')], { windowWidth: 390, windowHeight: 844 });
  assert.deepEqual(distinct.media.map(item => item.caption), ['南立面', '室内']);
  const mixedRatios = buildContentPresentation(article([{ kind: 'image', media_id: 'wide' }, { kind: 'image', media_id: 'tall' }]), [image('wide'), { ...image('tall'), width: 900, height: 1600 }], { windowWidth: 390, windowHeight: 844 });
  assert.equal(mixedRatios.galleryHeight, Math.max(...mixedRatios.media.map(item => item.height)), '滑动切图时舞台高度保持稳定');
});

test('video view preserves native player identity and caps portrait and landscape media to the viewport', () => {
  const content = { ...article([{ kind: 'paragraph', text: '建造全过程' }, { kind: 'video', media_id: 'video-a' }]), cover: { url: 'wxfile://cover.jpg' } };
  const landscape = buildContentPresentation(content, [video('video-a')], { windowWidth: 390, windowHeight: 844 }, { prefix: 'reader' });
  assert.equal(landscape.kind, 'video');
  assert.equal(landscape.videoLayout, 'channels');
  assert.equal(landscape.poster, 'wxfile://cover.jpg');
  assert.equal(landscape.nativePoster, '', 'wxfile cover must not be passed to the native video poster attribute');
  assert.equal(landscape.media[0].height, 199);
  assert.equal(landscape.media[0].domId, 'reader-media-0');
  assert.equal(landscape.media[0].playerKey, 'reader-player-0');
  assert.equal(landscape.media[0].errorKey, 'readerMedia0');

  const portrait = buildContentPresentation(content, [video('video-a', 1080, 1920)], { windowWidth: 390, windowHeight: 600 }, { prefix: 'reader' });
  assert.ok(portrait.media[0].height <= 360, 'portrait video must reserve navigation, copy and dock space instead of using a blanket viewport percentage');
  assert.ok(portrait.media[0].width < portrait.frameWidth, 'portrait video should be centered without stretching to full width');
  const square = buildContentPresentation(content, [video('video-a', 1080, 1080)], { windowWidth: 390, windowHeight: 844 });
  assert.equal(square.media[0].width, square.media[0].height, 'square video remains square without stretching');
  const remote = buildContentPresentation({ ...content, cover: { url: 'https://example.test/cover.jpg' } }, [video('video-a')], { windowWidth: 390, windowHeight: 844 });
  assert.equal(remote.nativePoster, 'https://example.test/cover.jpg');
});

test('video document supplies layout and channel-specific share copy without changing article or gallery defaults', () => {
  const content = {
    title: '三层新中式乡村别墅',
    body_format: 'article_v2',
    body: JSON.stringify({
      schema: 1,
      presentation: 'video',
      video_layout: 'douyin',
      share_copy: '哥/姐，给你看这套方案。',
      moments_copy: '今天分享一套好房子。',
      blocks: [{ kind: 'paragraph', text: '竖屏看房更沉浸' }, { kind: 'video', media_id: 'video-a' }],
    }),
  };
  const view = buildContentPresentation(content, [video('video-a', 1080, 1920)], { windowWidth: 390, windowHeight: 844 });
  assert.equal(view.videoLayout, 'douyin');
  assert.ok(view.media[0].height <= 844 - 150 - 160, 'dark video must reserve visible title and description space above the fixed action bar');
  assert.equal(view.shareCopy, '哥/姐，给你看这套方案。');
  assert.equal(view.momentsCopy, '今天分享一套好房子。');
  assert.equal(view.bodyExpandable, false);
  const expanded = buildContentPresentation({ ...content, body: JSON.stringify({ schema: 1, presentation: 'video', blocks: [{ kind: 'paragraph', text: '长文案'.repeat(50) }, { kind: 'video', media_id: 'video-a' }] }) }, [video('video-a')], { windowWidth: 390, windowHeight: 844 }, { descriptionExpanded: true });
  assert.equal(expanded.bodyExpandable, true);
  assert.equal(expanded.descriptionExpanded, true);
  assert.equal(buildContentPresentation(article([{ kind: 'image', media_id: 'image-a' }]), [image('image-a')], { windowWidth: 390, windowHeight: 844 }).videoLayout, 'channels');
});

test('article view preserves original mixed layout and excludes an unrelated cover', () => {
  const content = { ...article([{ kind: 'image', media_id: 'image-a' }, { kind: 'paragraph', text: '图间说明' }, { kind: 'image', media_id: 'image-b' }]), cover: { media_id: 'cover-a', kind: 'image' } };
  const view = buildContentPresentation(content, [image('cover-a'), image('image-a'), image('image-b')], { windowWidth: 390, windowHeight: 844 });
  assert.equal(view.kind, 'article');
  assert.deepEqual(view.blocks.map(block => block.kind), ['image', 'paragraph', 'image']);
  assert.deepEqual(view.media.map(item => item.media_id), ['image-a', 'image-b']);
  assert.ok(view.media.every(item => item.height <= Math.round(844 * 0.68)));

  const sharedCover = { ...article([{ kind: 'image', media_id: 'image-a' }]), cover: { media_id: 'image-a', kind: 'image' } };
  assert.deepEqual(buildContentPresentation(sharedCover, [image('image-a')], { windowWidth: 390, windowHeight: 844 }).media.map(item => item.media_id), ['image-a'], '被设为封面的正文图仍属于正文');
});

test('shared presentation template keeps swiper, native video controls, tracking hooks and profile gate placement', () => {
  const template = fs.readFileSync(path.join(root, 'components/content-presentation/content-presentation.wxml'), 'utf8');
  assert.match(template, /<template name="content-presentation">/);
  assert.match(template, /<swiper[^>]*bindchange="onContentGalleryChange"/);
  assert.match(template, /\{\{view\.galleryIndex \+ 1\}\}\/\{\{view\.media\.length\}\}/);
  assert.match(template, /<video wx:else[^>]*autoplay="\{\{videoStarted\[view\.media\[0\]\.playerKey\]\}\}"[^>]*controls[^>]*bindplay="videoState"[^>]*bindfullscreenchange="videoFullscreen"/s);
  assert.match(template, /poster="\{\{view\.nativePoster\}\}"/);
  assert.match(template, /wx:if="\{\{view\.poster && !videoStarted\[view\.media\[0\]\.playerKey\]\}\}"[^>]*bindtap="playContentVideo"[\s\S]*<video wx:else/);
  assert.match(template, /<image[^>]*src="\{\{view\.poster\}\}"[^>]*mode="aspectFit"/s);
  assert.match(template, /content-video-\{\{view\.videoLayout\}\}/);
  assert.match(template, /bindtap="toggleContentDescription"/);
  assert.match(template, /<block wx:if="\{\{view\.kind === 'article'\}\}">/);
  assert.match(template, /data-tracking="\{\{tracking\}\}"/);

  const reader = fs.readFileSync(path.join(root, 'pages/contentreader/contentreader.wxml'), 'utf8');
  assert.match(reader.split(/\r?\n/)[0], /^<import src=/);
  assert.match(reader, /<block wx:if="{{__profileAccessMounted}}">/);
  assert.match(reader, /<import src="\.\.\/\.\.\/components\/content-presentation\/content-presentation\.wxml"\/>/);
  assert.match(reader, /<template is="content-presentation" data="\{\{view: presentationView, tracking: true, mediaErrors: mediaErrors, videoStarted: videoStarted\}\}"\/>/);
});

 test('video keeps legacy full captions without duplicating body copy', () => {
 const build = (text, caption) => buildContentPresentation(article([...(text ? [{kind:'paragraph',text}] : []), {kind:'video',media_id:'video-a',caption}]), [video('video-a')], {windowWidth:390,windowHeight:844});
 assert.equal(build('', '完整旧视频文案').bodyText, '完整旧视频文案');
 assert.equal(build('完整文案', '完整文案').bodyText, '完整文案');
 assert.equal(build('完整文案', '补充说明').bodyText, '完整文案\n\n补充说明');
 });
