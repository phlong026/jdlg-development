'use strict';
// T7.8：分享页错误按语义区分（首屏/重验/媒体级失败隔离/联系客服）。
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const root = path.join(__dirname, '../miniprogram');
const pageSource = fs.readFileSync(path.join(root, 'pages/projectnoteview/projectnoteview.js'), 'utf8');

function harness(api) {
  let definition;
  const navigations = [], downloads = [];
  const wx = {
    getStorageSync: key => key === 'token' ? 'session-token' : key === 'userInfo' ? { id: 101 } : '',
    hideShareMenu() {}, showShareMenu() {}, showToast() {}, setClipboardData() {}, openDocument() {}, saveImageToPhotosAlbum() {},
    navigateTo: options => navigations.push(options),
    getWindowInfo: () => ({ windowWidth: 375, windowHeight: 667 }),
    downloadFile(options) {
      downloads.push(options.url);
      const media = api.media || {};
      const id = String(options.url || '').split('/').pop();
      const entry = media[id];
      if (!entry) { options.fail({ errMsg: 'downloadFile:fail' }); return; }
      if (entry.statusCode && entry.statusCode !== 200) { options.success({ statusCode: entry.statusCode, tempFilePath: '' }); return; }
      options.success({ statusCode: 200, tempFilePath: '/tmp/' + id });
    },
  };
  vm.runInNewContext(pageSource, {
    Page: value => { definition = value; }, wx, console, Promise, JSON, Object, String, Number, Date, Map, Set, Array, encodeURIComponent, decodeURIComponent,
    require: id => {
      if (id.includes('phase3-api')) return api;
      if (id.includes('article-document')) return require(path.join(root, 'utils/article-document.js'));
      if (id.includes('project-note-media')) return require(path.join(root, 'utils/project-note-media.js'));
      throw new Error('unexpected require ' + id);
    },
  });
  const page = Object.assign({ data: JSON.parse(JSON.stringify(definition.data)) }, definition, {
    setData(values, done) { Object.assign(this.data, values); if (done) done.call(this); },
    selectComponent: () => null,
  });
  page._visible = true;
  return { page, navigations, downloads };
}
const version = { version_id: 'v1', note_id: 'n1', revision: 1, title: '方案', body: '正文', body_format: 'plain', cover: { media_id: 'cover1', kind: 'image' }, media: [{ media_id: 'cover1', kind: 'image' }, { media_id: 'img2', kind: 'image' }] };
const view = { project: { project_id: 'p', name: '张宅' }, version, share: { share_id: 's1' }, can_confirm: false };
const openPage = (api, token = 'session-token') => {
  const h = harness(api);
  h.page.onLoad({ pns: 'opaque-share-token' });
  h.page._accountToken = token;
  h.page._epoch = 1;
  return h;
};

test('a 403 share error says the account lacks access instead of a generic failure', async () => {
  const h = openPage({ getSharedProjectNote: async () => { throw Object.assign(new Error('仅受邀账号可访问'), { statusCode: 403 }); } });
  await h.page.loadShared();
  assert.equal(h.page.data.error, '当前账号没有该项目的访问权限，请联系设计师。');
  assert.equal(h.page.data.view, null);
});

test('a missing share is reported as invalid, never as expired or revoked', async () => {
  const h = openPage({ getSharedProjectNote: async () => { throw Object.assign(new Error('分享不存在或已失效'), { statusCode: 404 }); } });
  await h.page.loadShared();
  assert.equal(h.page.data.error, '分享不存在或已失效，请让设计师重新分享。');
  assert.doesNotMatch(h.page.data.error, /过期|撤回/);
});

test('a local malformed token is treated as an invalid share', async () => {
  const h = openPage({ getSharedProjectNote: async () => view });
  h.page._shareToken = 'bad token';
  await h.page.loadShared();
  assert.equal(h.page.data.error, '分享不存在或已失效，请让设计师重新分享。');
});

test('a media-level 404 keeps the rest of the share visible with a retry notice', async () => {
  const h = openPage({
    getSharedProjectNote: async () => view,
    downloadSharedProjectNoteMedia: async (token, id) => { if (id === 'img2') throw Object.assign(new Error('媒体不可访问'), { statusCode: 404 }); return '/tmp/' + id; },
  });
  await h.page.loadShared();
  assert.equal(h.page.data.error, '');
  assert.ok(h.page.data.view, '分享本身仍展示');
  assert.match(h.page.data.notice, /部分素材加载失败/);
  assert.ok(h.page.data.media.some(item => item.media_id === 'cover1' && item.local_path), '其它素材正常展示');
  assert.ok(h.page.data.media.some(item => item.media_id === 'img2' && !item.local_path), '失败素材单独标记');
});

test('authorization loss during media download is not shown as a partial media failure', async () => {
  const h = openPage({
    getSharedProjectNote: async () => view,
    downloadSharedProjectNoteMedia: async (token, id) => { if (id === 'img2') throw Object.assign(new Error('仅受邀账号可访问'), { statusCode: 403 }); return '/tmp/' + id; },
  });
  await h.page.loadShared();
  assert.equal(h.page.data.view, null, '按撤权处理而不是部分展示');
  assert.equal(h.page.data.error, '当前账号没有该项目的访问权限，请联系设计师。');
});

test('network failures are retryable copy and a changed share asks to reopen', async () => {
  const network = openPage({ getSharedProjectNote: async () => { throw new Error('网络异常'); } });
  await network.page.loadShared();
  assert.equal(network.page.data.error, '加载失败，请重试。');

  const changed = openPage({ getSharedProjectNote: async () => { throw Object.assign(new Error('分享已变更'), { statusCode: 403 }); } });
  await changed.page.loadShared();
  assert.equal(changed.page.data.error, '分享内容已更新，请重新打开。');
});

test('the share error area offers the platform customer service entry', () => {
  const wxml = fs.readFileSync(path.join(root, 'pages/projectnoteview/projectnoteview.wxml'), 'utf8');
  assert.match(wxml, /bindtap="openCustomerService"/);
  assert.match(wxml, /联系客服/);
  const code = fs.readFileSync(path.join(root, 'pages/projectnoteview/projectnoteview.js'), 'utf8');
  assert.match(code, /pages\/customerservice\/customerservice/);
  assert.doesNotMatch(code, /1[3-9]\d{9}|微信号/, '错误提示不得暴露设计师个人联系方式');
});

// T5.3：原排版（layout_html）里的媒体失败只降级为占位，不拖垮整篇分享。
const { renderArticleDocument } = require(path.join(root, 'utils/article-document.js'));

test('a failed layout image renders a placeholder instead of failing the document', () => {
  const body = JSON.stringify({ schema: 1, blocks: [{ kind: 'paragraph', text: '正文' }], layout_html: '<p>正文</p><img data-media-id="img20001" alt="">' });
  const blocks = renderArticleDocument(body, [{ media_id: 'img20001', kind: 'image', local_path: '' }], 300, 'p');
  assert.ok(blocks.some(block => block.html && /素材加载失败/.test(block.html)), '失败图片必须渲染占位');
});

test('a failed layout video renders a retryable placeholder', () => {
  const body = JSON.stringify({ schema: 1, blocks: [{ kind: 'paragraph', text: '正文' }], layout_html: '<p>前</p><jd-video data-media-id="vid20001"></jd-video><p>后</p>' });
  const blocks = renderArticleDocument(body, [{ media_id: 'vid20001', kind: 'video', local_path: '' }], 300, 'p');
  assert.ok(blocks.some(block => block.html && /视频加载失败/.test(block.html)), '失败视频必须渲染占位');
  assert.ok(blocks.some(block => block.html && /后/.test(block.html)), '其余正文继续渲染');
});

test('an article_v2 share keeps rendering when one layout media fails to download', async () => {
  const articleVersion = {
    version_id: 'v2', note_id: 'n1', revision: 2, title: '排版方案', body_format: 'article_v2',
    body: JSON.stringify({ schema: 1, blocks: [{ kind: 'paragraph', text: '正文' }], layout_html: '<p>正文</p><img data-media-id="img20001" alt="">' }),
    cover: { media_id: 'cover001', kind: 'image' },
    media: [{ media_id: 'img20001', kind: 'image' }],
  };
  const h = openPage({
    getSharedProjectNote: async () => ({ project: { project_id: 'p', name: '张宅' }, version: articleVersion, share: { share_id: 's1' }, can_confirm: false }),
    downloadSharedProjectNoteMedia: async (token, id) => {
      if (id === 'img20001') throw Object.assign(new Error('媒体不可访问'), { statusCode: 404 });
      return '/tmp/' + id;
    },
  });
  await h.page.loadShared();
  assert.equal(h.page.data.error, '', '不得整页失败');
  assert.ok(h.page.data.view, '分享仍展示');
  assert.match(h.page.data.notice, /部分素材加载失败/);
  assert.ok(h.page.data.articleBlocks.length > 0, '原排版正文继续渲染');
});

// 7-B 复评：媒体 404 可能代表分享已失效，必须重新核验后才允许降级。
test('a media 404 that actually means the share died clears protected content', async () => {
  let detailCalls = 0;
  const h = openPage({
    getSharedProjectNote: async () => {
      detailCalls += 1;
      if (detailCalls === 1) return view;
      throw Object.assign(new Error('分享不存在或已失效'), { statusCode: 404 });
    },
    downloadSharedProjectNoteMedia: async (token, id) => {
      if (id === 'img2') throw Object.assign(new Error('媒体不可访问'), { statusCode: 404 });
      return '/tmp/' + id;
    },
  });
  await h.page.loadShared();
  assert.equal(h.page.data.view, null, '分享失效必须清空受保护内容');
  assert.equal(h.page.data.feedbackRows ? h.page.data.feedbackRows.length : 0, 0);
  assert.match(h.page.data.error, /分享不存在或已失效/);
});

test('a media 404 with a still-valid share only degrades that media', async () => {
  const h = openPage({
    getSharedProjectNote: async () => view,
    downloadSharedProjectNoteMedia: async (token, id) => {
      if (id === 'img2') throw Object.assign(new Error('媒体不可访问'), { statusCode: 404 });
      return '/tmp/' + id;
    },
  });
  await h.page.loadShared();
  assert.ok(h.page.data.view, '分享仍有效时保留正文');
  assert.match(h.page.data.notice, /部分素材加载失败/);
});

// T3.3：切后台后操作锁复位，重验成功也不遗留 loading。
test('backgrounding resets the reader operation locks', () => {
  const h = openPage({ getSharedProjectNote: async () => view });
  h.page.setData({ acknowledgingRead: true, activityLoading: true, busy: true });
  h.page.onHide();
  assert.equal(h.page.data.acknowledgingRead, false);
  assert.equal(h.page.data.activityLoading, false);
  assert.equal(h.page.data.busy, false);
});

test('a successful revalidation clears stale operation locks', async () => {
  const h = openPage({ getSharedProjectNote: async () => view });
  h.page.setData({ view, acknowledgingRead: true, activityLoading: true, busy: true });
  await h.page.revalidateShared();
  assert.equal(h.page.data.acknowledgingRead, false);
  assert.equal(h.page.data.activityLoading, false);
  assert.equal(h.page.data.busy, false);
  assert.equal(h.page.data.loading, false);
});

// 7-B 二评：重核验保留真实错误语义；多次媒体 404 只核验一次并安全清空。
test('a media 404 whose recheck fails keeps the real login/permission semantics', async () => {
  let detailCalls = 0;
  const h = openPage({
    getSharedProjectNote: async () => {
      detailCalls += 1;
      if (detailCalls === 1) return view;
      throw Object.assign(new Error('登录已过期'), { statusCode: 401 });
    },
    downloadSharedProjectNoteMedia: async (token, id) => {
      if (id === 'img2') throw Object.assign(new Error('媒体不可访问'), { statusCode: 404 });
      return '/tmp/' + id;
    },
  });
  await h.page.loadShared();
  assert.equal(h.page.data.loginRequired, true, '核验失败是 401 时必须进入登录态，而不是分享失效');
  assert.equal(h.page.data.view, null);
  assert.equal(h.page.data.error, '');
});

test('several media 404s verify the share once and end in a safe state', async () => {
  let goneCalls = 0;
  const gone = openPage({
    getSharedProjectNote: async () => {
      goneCalls += 1;
      if (goneCalls === 1) return view;
      throw Object.assign(new Error('分享不存在或已失效'), { statusCode: 404 });
    },
    downloadSharedProjectNoteMedia: async (token, id) => {
      if (id === 'img2') throw Object.assign(new Error('媒体不可访问'), { statusCode: 404 });
      return '/tmp/' + id;
    },
  });
  await gone.page.loadShared();
  assert.equal(goneCalls, 2, '媒体 404 只额外核验一次分享');
  assert.equal(gone.page.data.view, null);
  assert.equal(gone.page._accessVerified, false);
  assert.equal(gone.page.data.shareReady, false);
  assert.match(gone.page.data.error, /分享不存在或已失效/);

  const twoMedia = Object.assign({}, view, { version: Object.assign({}, version, { media: [{ media_id: 'cover1', kind: 'image' }, { media_id: 'img2', kind: 'image' }, { media_id: 'img3', kind: 'image' }] }) });
  let validCalls = 0;
  const valid = openPage({
    getSharedProjectNote: async () => { validCalls += 1; return twoMedia; },
    downloadSharedProjectNoteMedia: async (token, id) => {
      if (id === 'img2' || id === 'img3') throw Object.assign(new Error('媒体不可访问'), { statusCode: 404 });
      return '/tmp/' + id;
    },
  });
  await valid.page.loadShared();
  assert.equal(validCalls, 2, '两个媒体 404 共用一次核验');
  assert.equal(valid.page.data.view && valid.page.data.view.share.share_id, 's1', '分享仍有效时保留正文');
  assert.match(valid.page.data.notice, /部分素材加载失败/);
});

test('the shared document template renders a placeholder for failed media', () => {
  const wxml = fs.readFileSync(path.join(root, 'components/project-note-document/note-document.wxml'), 'utf8');
  assert.match(wxml, /note-media-missing/);
  assert.match(wxml, /素材加载失败/);
  const wxss = fs.readFileSync(path.join(root, 'components/project-note-document/note-document.wxss'), 'utf8');
  assert.match(wxss, /\.note-media-missing\{/);
});

// D25：重验成功后仍保留失败素材提示，不静默消失。
test('a successful revalidation keeps the partial media failure notice', async () => {
  const h = openPage({
    getSharedProjectNote: async () => view,
    downloadSharedProjectNoteMedia: async (token, id) => {
      if (id === 'img2') throw Object.assign(new Error('媒体不可访问'), { statusCode: 404 });
      return '/tmp/' + id;
    },
  });
  await h.page.loadShared();
  assert.match(h.page.data.notice, /部分素材加载失败/);
  await h.page.revalidateShared();
  assert.match(h.page.data.notice, /部分素材加载失败/, '重验成功后失败素材提示必须保留');
});

// T3.2：素材下载并发上限 4。
test('share media downloads are capped at four concurrent requests', async () => {
  let active = 0, peak = 0;
  const ids = Array.from({ length: 8 }, (_, index) => 'img3000' + index);
  const mediaView = Object.assign({}, view, { version: Object.assign({}, version, { media: ids.map(id => ({ media_id: id, kind: 'image' })) }) });
  const h = openPage({
    getSharedProjectNote: async () => mediaView,
    downloadSharedProjectNoteMedia: async (token, id) => {
      active += 1; peak = Math.max(peak, active);
      await new Promise(resolve => setTimeout(resolve, 5));
      active -= 1;
      return '/tmp/' + id;
    },
  });
  await h.page.loadShared();
  assert.ok(peak <= 4, `并发峰值必须 ≤4，实际 ${peak}`);
  assert.ok(peak >= 2, '应实际并发下载');
});

// T3.5：分享与版本未变化时重验不重建媒体数组。
test('an unchanged revalidation does not rebuild the media array', async () => {
  const h = openPage({
    getSharedProjectNote: async () => view,
    downloadSharedProjectNoteMedia: async (token, id) => '/tmp/' + id,
  });
  await h.page.loadShared();
  const before = h.page.data.media;
  await h.page.revalidateShared();
  assert.equal(h.page.data.media, before, '未变化时不得重建媒体对象');
});

// D16：分享仍有效时对失败素材做定向重试，只重下失败项。
test('retrying failed share media only re-downloads the failures', async () => {
  let fail = true;
  const downloads = [];
  const h = openPage({
    getSharedProjectNote: async () => view,
    downloadSharedProjectNoteMedia: async (token, id) => {
      downloads.push(id);
      if (id === 'img2' && fail) throw Object.assign(new Error('媒体不可访问'), { statusCode: 404 });
      return '/tmp/' + id;
    },
  });
  await h.page.loadShared();
  assert.equal(downloads.filter(id => id === 'img2').length, 1, '初始加载下载一次失败素材');
  downloads.length = 0;
  fail = false;
  await h.page.retryMediaDownloads();
  assert.deepEqual(downloads, ['img2'], '只重下失败素材');
  assert.equal(h.page.data.mediaFailuresCount, 0);
  assert.ok(h.page.data.media.some(item => item.media_id === 'img2' && item.local_path));
});
