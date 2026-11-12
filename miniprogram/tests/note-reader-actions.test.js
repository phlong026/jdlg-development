'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const vm = require('node:vm');
const fs = require('node:fs');
const path = require('node:path');
const file = path.join(__dirname, '../miniprogram/utils/note-reader-actions.js');
function harness(overrides = {}) {
  const storage = { token: 'one' }, navigations = [], saves = [], clipboard = [];
  const wx = { setClipboardData: value => clipboard.push(value.data), getStorageSync: key => storage[key] || '', navigateTo: value => navigations.push(value.url), showToast() {}, showModal: value => value.success({ confirm: true }) };
  const api = { saveContent: async value => { saves.push(value); return { content_id: 'copied-note' }; }, ...overrides };
  const module = { exports: {} };
  vm.runInNewContext(fs.readFileSync(file, 'utf8'), { module, exports: module.exports, wx, require: () => api, console, Promise, String, Number, Object, Array, Date, Set, encodeURIComponent });
  const page = { ...module.exports.methods, data: { ...module.exports.data, contentReady: true, posterAvailable: true, content: { content_id: 'note-one', version: 2, title: '原作品', body: '固定正文', body_format: 'plain', owner_id: 'owner-one', rights: 'own', media: [] }, presentationView: { kind: 'gallery', title: '原作品', bodyText: '固定正文', media: [] } }, options: { content_id: 'note-one', content_version: '2' }, loadSequence: 1, pageVisible: true, posterAccessCurrent: () => true, setData(value) { Object.assign(this.data, value); } };
  return { page, storage, saves, navigations, clipboard, wx };
}
test('note menu grants editing only with current management proof', () => {
  const { page } = harness();
  page.openNoteMore();
  assert.ok(page.data.noteMenu.some(item => item.key === 'edit'));
  assert.ok(page.data.noteMenu.some(item => item.key === 'delete'));
  assert.ok(page.data.noteMenu.some(item => item.key === 'collection'));
  page.posterAccessCurrent = () => false;
  page.openNoteMore();
  assert.ok(!page.data.noteMenu.some(item => ['edit', 'copy', 'collection', 'delete', 'word'].includes(item.key)));
});
test('document archive content never offers a generic copy path', () => {
  const { page } = harness();
  page.data.content.body_format = 'document_v1';
  page.openNoteMore();
  assert.ok(!page.data.noteMenu.some(item => item.key === 'copy'));
  assert.ok(page.data.noteMenu.some(item => item.key === 'collection'));
});
test('copy retries reuse one create key and clear it only after known success', async () => {
  let calls = 0, sequence = 0;
  const keys = [];
  const { page, navigations } = harness({
    mutationKey: () => 'copy-key-' + (++sequence),
    saveContent: async value => { keys.push(value.idempotency_key); if (++calls === 1) throw new Error('响应丢失'); return { content_id: 'copied-note' }; },
  });
  await page.copyNote();
  await page.copyNote();
  assert.deepEqual(keys, ['copy-key-1', 'copy-key-1']);
  assert.match(navigations[0], /copied-note/);
  assert.equal(page._noteCopyIntent, null);
});
test('copy key and delayed navigation cannot cross account or source version', async () => {
  let resolve, sequence = 0;
  const { page, storage, navigations } = harness({ mutationKey: () => 'copy-key-' + (++sequence), saveContent: () => new Promise(done => { resolve = done; }) });
  const pending = page.copyNote();
  storage.token = 'two';
  page.options.content_version = '3';
  resolve({ content_id: 'old-account-copy' });
  await pending;
  assert.equal(navigations.length, 0);
  page.data.noteActionBusy = false;
  await Promise.race([page.copyNote(), Promise.resolve()]);
  assert.equal(page._noteCopyIntent.key, 'copy-key-2');
});
test('article copy retains headings and lists as readable text instead of HTML markup',()=>{
 const {page,clipboard}=harness();
 page.data.presentationView={kind:'article',title:'文章标题',bodyText:'',blocks:[{kind:'heading',text:'方案要点'},{kind:'list',items:['客餐厅','院落']},{kind:'layout',html:'<p>采光 &amp; 通风</p>'}]};
 page.copyNoteText();
 assert.match(clipboard[0],/方案要点/);assert.match(clipboard[0],/客餐厅/);assert.match(clipboard[0],/采光 & 通风/);assert.doesNotMatch(clipboard[0],/<p>/);
});

test('new collection retry reuses one key and binds the viewed fixed version', async()=>{
 let calls=0,sequence=0;const saved=[];
 const {page}=harness({mutationKey:()=> 'collection-key-'+(++sequence),saveCollection:async input=>{saved.push(input);if(++calls===1)throw new Error('响应丢失');return {collection_id:'collection-new'};}});
 page.data.noteCollectionTitle='方案合集';
 const event={currentTarget:{dataset:{}}};
 await page.addNoteCollection(event);
 await page.addNoteCollection(event);
 assert.equal(saved.length,2);assert.equal(saved[0].idempotency_key,saved[1].idempotency_key);
 assert.equal(JSON.stringify(saved[1].items),JSON.stringify([{content_id:'note-one',content_version:2}]));
 assert.equal(page.data.collectionsOpen,false);
});

test('existing collection uses current revision and exposes a 409 refresh conflict', async()=>{
 let saved;
 const conflict=Object.assign(new Error('版本冲突'),{statusCode:409});
 const {page}=harness({getCollection:async()=>({collection_id:'collection-one',title:'已有合集',description:'',revision:7,items:[{content_id:'other-note',content_version:1}]}),saveCollection:async(input,id)=>{saved={input,id};throw conflict;}});
 page.data.collectionsOpen=true;page.data.noteCollections=[{collection_id:'collection-one'}];
 await page.addNoteCollection({currentTarget:{dataset:{id:'collection-one'}}});
 assert.equal(saved.id,'collection-one');assert.equal(saved.input.expected_revision,7);
 assert.equal(JSON.stringify(saved.input.items),JSON.stringify([{content_id:'other-note',content_version:1},{content_id:'note-one',content_version:2}]));
 assert.match(page.data.noteCollectionError,/重新打开/);
 assert.equal(page.data.collectionsOpen,true);
});

test('late collection response cannot populate a switched account or version', async()=>{
 let resolve;
 const {page,storage}=harness({listCollections:()=>new Promise(done=>{resolve=done;})});
 const pending=page.openNoteCollections();
 storage.token='two';page.options.content_version='3';
 resolve({items:[{collection_id:'old-collection',owner_id:'owner-one',status:'draft'}],next_cursor:'old-cursor'});
 await pending;
 assert.equal(page.data.noteCollections.length,0);
 assert.equal(page.data.noteCollectionCursor,'');
});

test('note more preserves the existing authorized poster entry', () => {
  const { page } = harness();
  let calls = 0;
  page.generatePoster = () => { calls += 1; };
  page.openNoteMore();
  assert.ok(page.data.noteMenu.some(item => item.key === 'poster'));
  page.selectNoteMenu({ currentTarget: { dataset: { key: 'poster' } } });
  assert.equal(calls, 1);
  page.posterAccessCurrent = () => false;
  page.openNoteMore();
  assert.ok(!page.data.noteMenu.some(item => item.key === 'poster'));
});

test('save images reuses resolved local media and downloads remote media before album access', async () => {
  let resolutions = 0;
  const { page, wx } = harness({ resolveProtectedMediaURL: async url => { resolutions += 1; if (!url.startsWith('https://')) throw new Error('素材地址无效'); return url; } });
  const saved = [], downloaded = [];
  wx.downloadFile = options => { downloaded.push(options.url); options.success({ statusCode: 200, tempFilePath: 'wxfile://downloaded' }); };
  wx.saveImageToPhotosAlbum = options => { saved.push(options.filePath); options.success(); };
  page.data.media = [{ kind: 'image', url: 'wxfile://already-resolved' }, { kind: 'image', url: 'https://example.test/photo.jpg' }];
  await page.saveNoteImages();
  assert.deepEqual(saved, ['wxfile://already-resolved', 'wxfile://downloaded']);
  assert.equal(resolutions, 1);
  assert.equal(downloaded.length, 1);
});

test('failed remote image download never sends a URL to the album API', async () => {
  const { page, wx } = harness({ resolveProtectedMediaURL: async url => url });
  let saved = false;
  wx.downloadFile = options => options.success({ statusCode: 503 });
  wx.saveImageToPhotosAlbum = () => { saved = true; };
  page.data.media = [{ kind: 'image', url: 'https://example.test/photo.jpg' }];
  await page.saveNoteImages();
  assert.equal(saved, false);
  assert.equal(page.data.noteActionBusy, false);
});
