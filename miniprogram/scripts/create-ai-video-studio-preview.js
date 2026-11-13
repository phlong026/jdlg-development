#!/usr/bin/env node
'use strict';

const fs = require('node:fs');
const path = require('node:path');
const { execFileSync } = require('node:child_process');

const projectRoot = path.resolve(__dirname, '..');
const sourceRoot = path.join(projectRoot, 'miniprogram');
const outputRoot = '/tmp/jdlg-studio-ui-preview-20261113';
const markerName = '.jdlg-ai-video-studio-preview';
const markerPath = path.join(outputRoot, markerName);
const pageSource = path.join(sourceRoot, 'pages', 'aivideostudio');
const pageOutput = path.join(outputRoot, 'miniprogram', 'pages', 'aivideostudio');
const allowedPageExtensions = new Set(['.js', '.wxml', '.wxss', '.json']);

function mediaOriginFromArguments(argv) {
  const option = argv.find(value => value.startsWith('--media-origin='));
  if (!option) return '';
  if (argv.filter(value => value.startsWith('--media-origin=')).length !== 1) throw new Error('--media-origin can only be provided once');
  const raw = option.slice('--media-origin='.length);
  let parsed;
  try { parsed = new URL(raw); } catch (_) { throw new Error('--media-origin must be a valid loopback HTTP URL'); }
  if (parsed.protocol !== 'http:' || parsed.hostname !== '127.0.0.1' || !parsed.port || parsed.username || parsed.password || parsed.pathname !== '/' || parsed.search || parsed.hash) {
    throw new Error('--media-origin only accepts http://127.0.0.1:<port>');
  }
  const port = Number(parsed.port);
  if (!Number.isSafeInteger(port) || port < 1 || port > 65535) throw new Error('--media-origin port is invalid');
  return `http://127.0.0.1:${port}`;
}

const mediaOrigin = mediaOriginFromArguments(process.argv.slice(2));

function ensureSafeOutput() {
  if (fs.existsSync(outputRoot)) {
    if (!fs.statSync(outputRoot).isDirectory() || !fs.existsSync(markerPath)) {
      throw new Error(`refusing to overwrite unmarked directory: ${outputRoot}`);
    }
  } else {
    fs.mkdirSync(outputRoot, { recursive: true });
  }
  fs.writeFileSync(markerPath, 'JDLG AI video studio offline UI preview\n', 'utf8');
}

function ensureDir(directory) {
  fs.mkdirSync(directory, { recursive: true });
}

function copyFile(source, destination) {
  ensureDir(path.dirname(destination));
  fs.copyFileSync(source, destination);
}

function writeFile(relativePath, content) {
  const destination = path.join(outputRoot, relativePath);
  ensureDir(path.dirname(destination));
  fs.writeFileSync(destination, content, 'utf8');
}

function copyStudioPages() {
  ensureDir(pageOutput);
  for (const entry of fs.readdirSync(pageSource, { withFileTypes: true })) {
    if (!entry.isFile() || !allowedPageExtensions.has(path.extname(entry.name))) continue;
    copyFile(path.join(pageSource, entry.name), path.join(pageOutput, entry.name));
  }
}

function copyMiniIcon() {
  const source = path.join(sourceRoot, 'components', 'mini-icon');
  const destination = path.join(outputRoot, 'miniprogram', 'components', 'mini-icon');
  ensureDir(destination);
  for (const name of ['mini-icon.js', 'mini-icon.json', 'mini-icon.wxml', 'mini-icon.wxss']) {
    copyFile(path.join(source, name), path.join(destination, name));
  }
}

function copyNavigationBar() {
  const source = path.join(sourceRoot, 'components', 'navigation-bar');
  const destination = path.join(outputRoot, 'miniprogram', 'components', 'navigation-bar');
  ensureDir(destination);
  for (const name of ['navigation-bar.js', 'navigation-bar.json', 'navigation-bar.wxml', 'navigation-bar.wxss']) {
    copyFile(path.join(source, name), path.join(destination, name));
  }
}

function writeLoginGuardStub() {
  writeFile('miniprogram/components/login-guard/login-guard.json', JSON.stringify({ component: true }, null, 2) + '\n');
  writeFile('miniprogram/components/login-guard/login-guard.wxml', '<view hidden="true" />\n');
  writeFile('miniprogram/components/login-guard/login-guard.wxss', '/* Offline preview: no authentication overlay. */\n');
  writeFile('miniprogram/components/login-guard/login-guard.js', "Component({ lifetimes: { attached() { this.triggerEvent('authenticated'); } } });\n");
}

function copyRulesAndImage() {
  copyFile(path.join(sourceRoot, 'utils', 'ai-video-studio.js'), path.join(outputRoot, 'miniprogram', 'utils', 'ai-video-studio.js'));
  copyFile(path.join(sourceRoot, 'assets', 'welcome', 'courtyard.jpg'), path.join(outputRoot, 'miniprogram', 'assets', 'welcome', 'courtyard.jpg'));
}

function createMediaFixtures() {
  const mediaDir = path.join(outputRoot, 'miniprogram', 'assets', 'fixtures');
  ensureDir(mediaDir);
  const image = path.join(outputRoot, 'miniprogram', 'assets', 'welcome', 'courtyard.jpg');
  const video = path.join(mediaDir, 'courtyard-preview-5s.mp4');
  const audio = path.join(mediaDir, 'silence-preview-5s.wav');
  try {
    execFileSync('ffmpeg', ['-y', '-loop', '1', '-i', image, '-t', '5', '-vf', 'scale=960:-2', '-r', '25', '-c:v', 'libx264', '-pix_fmt', 'yuv420p', '-movflags', '+faststart', '-an', video], { stdio: 'ignore' });
    execFileSync('ffmpeg', ['-y', '-f', 'lavfi', '-i', 'anullsrc=r=44100:cl=stereo', '-t', '5', '-c:a', 'pcm_s16le', audio], { stdio: 'ignore' });
  } catch (error) {
    throw new Error(`ffmpeg is required to create local 5-second preview fixtures: ${error.message}`);
  }
}

function writeAPIStub() {
  const videoURL = mediaOrigin ? mediaOrigin + '/assets/fixtures/courtyard-preview-5s.mp4' : '/assets/fixtures/courtyard-preview-5s.mp4';
  const audioURL = mediaOrigin ? mediaOrigin + '/assets/fixtures/silence-preview-5s.wav' : '/assets/fixtures/silence-preview-5s.wav';
  const stub = `'use strict';

const OFFLINE_ERROR = '离线UI验收禁止提交生成任务，不会调用供应商或扣费';
const rules = require('./ai-video-studio');
const image = '/assets/welcome/courtyard.jpg';
const video = ${JSON.stringify(videoURL)};
const audio = ${JSON.stringify(audioURL)};
let revision = 7;
const savedDrafts = new Map();

function capabilities() {
  return Promise.resolve({
    enabled: true,
    disabled_reason: '',
    modes: ['text', 'first_frame', 'first_last_frame', 'reference'].map(code => ({ code, enabled: true })),
    ratios: ['16:9', '9:16', '1:1', 'adaptive'],
    resolutions: ['768p', '2k'],
    durations_by_resolution: { '768p': [5, 10, 15], '2k': [5, 10, 15] },
    quantity_max: 3,
    reference_limits: { image_max: 8, video_max: 3, audio_max: 3, total_max: 12, media_duration_min: 2, media_duration_max: 15, video_total_duration_max: 15, audio_total_duration_max: 15 },
  });
}

function frameDraft() {
  return {
    draft_id: 'frame-preview', revision: 7, name: '庭院漫游', mode: 'first_frame',
    prompt: '清晨的现代庭院，镜头从水景缓慢推进到客厅落地窗，柔和自然光。',
    reference_prompt: '参考图1控制庭院材质与暖色光线。',
    first_frame_media_id: 'fixture-courtyard-image', last_frame_media_id: '',
    first_frame: { media_id: 'fixture-courtyard-image', media_type: 'image', name: '庭院首帧', width: 1200, height: 800, preview_url: image, url: image },
    last_frame: {}, references: [], ratio: 'adaptive', resolution: '768p', duration: 5, quantity: 1,
  };
}

function referenceDraft() {
  return {
    draft_id: 'reference-preview', revision: 7, name: '庭院漫游', mode: 'reference',
    prompt: '庭院空间由近及远展开，保持自然、克制的镜头运动。',
    reference_prompt: '图片1控制建筑与庭院风格；视频1参考镜头推进节奏；音频1参考舒缓节拍。',
    first_frame_media_id: '', last_frame_media_id: '', first_frame: {}, last_frame: {}, ratio: '16:9', resolution: '768p', duration: 5, quantity: 1,
    references: [
      { media_id: 'fixture-reference-image', media_type: 'image', mime_type: 'image/jpeg', name: '庭院参考图', purpose: '控制建筑与庭院风格', position: 1, width: 1200, height: 800, preview_url: image, url: image },
      { media_id: 'fixture-reference-video', media_type: 'video', mime_type: 'video/mp4', name: '庭院推进参考', purpose: '参考镜头推进节奏', position: 2, duration_seconds: 5, width: 960, height: 640, poster: image, preview_url: image, url: video },
      { media_id: 'fixture-reference-audio', media_type: 'audio', mime_type: 'audio/wav', name: '舒缓节奏参考', purpose: '参考舒缓节拍', position: 3, duration_seconds: 5, url: audio },
    ],
  };
}

function fixtureMedia() {
  return [
    { media_id: 'fixture-courtyard-image', media_type: 'image', mime_type: 'image/jpeg', name: '庭院首帧', width: 1200, height: 800, preview_url: image, url: image },
    { media_id: 'fixture-reference-image', media_type: 'image', mime_type: 'image/jpeg', name: '庭院参考图', width: 1200, height: 800, preview_url: image, url: image },
    { media_id: 'fixture-reference-video', media_type: 'video', mime_type: 'video/mp4', name: '庭院推进参考', duration_seconds: 5, width: 960, height: 640, poster: image, poster_url: image, preview_url: image, url: video },
    { media_id: 'fixture-reference-audio', media_type: 'audio', mime_type: 'audio/wav', name: '舒缓节奏参考', duration_seconds: 5, url: audio },
  ];
}

function getDraft(id) {
  const draft = id === 'reference-preview' ? referenceDraft() : frameDraft();
  savedDrafts.set(draft.draft_id, draft);
  return Promise.resolve(draft);
}
function saveDraft(input, id) {
  revision += 1;
  const saved = Object.assign({}, input, { draft_id: id || 'offline-preview-draft', revision });
  savedDrafts.set(saved.draft_id, saved);
  return Promise.resolve(saved);
}
function createDraft(input) { return saveDraft(input, 'offline-preview-draft'); }
function updateDraft(id, input) { return saveDraft(input, id); }
function getMedia(id) {
  const item = fixtureMedia().find(media => media.media_id === id);
  if (item) return Promise.resolve(item);
  return Promise.reject(Object.assign(new Error('离线 fixture 不包含该素材'), { statusCode: 404, offlineFixture: true }));
}
function quote(input) {
  const draft = savedDrafts.get(input && input.draft_id) || frameDraft();
  const inputFingerprint = rules.billableFingerprint(draft);
  console.warn('[offline-ui-preview] quote fixture only: no charge, no provider request');
  return Promise.resolve({ quote_id: 'offline-quote-no-charge', input_fingerprint: inputFingerprint, total_stones: 36, billable_units: Number(draft.quantity || 1), expires_at: '2099-11-13T23:59:00+08:00', offline_fixture: true, disclaimer: '离线UI验收示例价，不扣费' });
}
function createBatch() { return Promise.reject(Object.assign(new Error(OFFLINE_ERROR), { statusCode: 503, offlineFixture: true })); }
function idempotencyKey() { return 'offline-preview-never-submitted'; }
function listMedia() { const items = fixtureMedia(); return Promise.resolve({ items, total: items.length }); }
function listPrompts() { return Promise.resolve({ items: [], total: 0 }); }
function listBatches() { return Promise.resolve({ items: [], total: 0 }); }

module.exports = { capabilities, getDraft, createDraft, updateDraft, getMedia, quote, createBatch, idempotencyKey, listMedia, listPrompts, listBatches, OFFLINE_ERROR };
`;
  writeFile('miniprogram/utils/ai-video-studio-api.js', stub);
}

function writeApplicationFiles() {
  const app = `const nativePage = Page;
Page = function offlinePreviewPage(definition) {
  definition.data = Object.assign({}, definition.data || {}, { __profileAccessMounted: true });
  const originalOnLoad = definition.onLoad;
  definition.onLoad = function previewOnLoad(options) {
    this.setData({ __profileAccessMounted: true, notice: '离线UI验收：不会调用供应商或扣费' });
    return originalOnLoad && originalOnLoad.call(this, options || { draft_id: 'frame-preview' });
  };
  return nativePage(definition);
};
App({ onLaunch() { console.warn('[offline-ui-preview] local fixtures only; no token, network, provider, generation or billing'); } });
`;
  writeFile('miniprogram/app.js', app);
  writeFile('miniprogram/app.json', JSON.stringify({
    pages: ['pages/aivideostudio/generate'],
    window: { navigationBarTitleText: 'AI 视频 · 离线UI验收', navigationBarBackgroundColor: '#fbfaf7', navigationBarTextStyle: 'black', backgroundColor: '#fbfaf7' },
    sitemapLocation: 'sitemap.json',
  }, null, 2) + '\n');
  writeFile('miniprogram/app.wxss', 'page { background: #fbfaf7; }\n');
  writeFile('miniprogram/sitemap.json', JSON.stringify({ desc: 'Offline UI preview only', rules: [] }, null, 2) + '\n');

  const sourceConfig = JSON.parse(fs.readFileSync(path.join(projectRoot, 'project.config.json'), 'utf8'));
  writeFile('project.config.json', JSON.stringify({
    description: 'AI视频离线UI验收（无网络、无生成、无扣费）',
    miniprogramRoot: 'miniprogram/', compileType: 'miniprogram', appid: sourceConfig.appid,
    projectname: 'AI视频-离线UI验收', libVersion: sourceConfig.libVersion || '2.32.3',
    setting: { es6: true, enhance: true, minified: false, postcss: false, minifyWXSS: false, minifyWXML: false, urlCheck: false },
    condition: { miniprogram: { list: [
      { name: '首帧图生视频', pathName: 'pages/aivideostudio/generate', query: 'draft_id=frame-preview', scene: null },
      { name: '混合参考视频', pathName: 'pages/aivideostudio/generate', query: 'draft_id=reference-preview&kind=reference', scene: null },
    ] } },
  }, null, 2) + '\n');
}

function writeReadme() {
  writeFile('README-OFFLINE-PREVIEW.md', `# AI视频离线UI验收 fixture

- 页面与样式从实际 \`pages/aivideostudio\` 的 JS/WXML/WXSS/JSON 复制；不复制 TS。
- \`frame-preview\` 展示庭院首帧与普通提示词；\`reference-preview\` 展示图片、5秒本地视频、5秒静音 WAV 和结构化参考提示词。
- Page 包装器只在本 fixture 将 \`__profileAccessMounted\` 设为 true，未读取或写入真实登录 token，也未更改生产登录守卫。
- API 全部为本地 Promise stub。报价为 36 灵石的离线展示值且不扣费；\`createBatch\` 固定拒绝。
- fixture 不包含任何供应商凭据，不访问外部网络，不产生生成费用。可选 loopback 地址仅用于读取本机夹具音视频文件。

重新生成：

\`node ${path.relative(projectRoot, __filename)}\`

原生 video 需要本机静态服务时：

\`node ${path.relative(projectRoot, __filename)} --media-origin=http://127.0.0.1:8769\`
`);
}

function main() {
  ensureSafeOutput();
  copyStudioPages();
  copyMiniIcon();
  copyNavigationBar();
  writeLoginGuardStub();
  copyRulesAndImage();
  createMediaFixtures();
  writeAPIStub();
  writeApplicationFiles();
  writeReadme();
  console.log(`offline preview created at ${outputRoot}`);
}

main();
