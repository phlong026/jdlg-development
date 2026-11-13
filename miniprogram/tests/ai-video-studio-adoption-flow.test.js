'use strict';
const test = require('node:test'); const assert = require('node:assert/strict');
const fs = require('node:fs'); const path = require('node:path'); const vm = require('node:vm');
function detail(api, phase3) {
 let definition, token = 'account-a'; const navigations = [];
 vm.runInNewContext(fs.readFileSync(path.join(__dirname, '../miniprogram/pages/aivideostudio/detail.js'), 'utf8'), {
  Page: value => { definition = value; }, require: id => id.includes('ai-video-studio-api') ? api : phase3,
  wx: { getStorageSync: key => key === 'token' ? token : '', navigateTo: value => navigations.push(value.url) },
  setTimeout, clearTimeout, console,
 });
 const page = Object.assign({}, definition); page.data = JSON.parse(JSON.stringify(definition.data));
 page.setData = function(patch) { Object.assign(this.data, patch); }; page._token = token; page._visible = true; page.id = 'batch-1';
 page.data.results = [{ result_id: 'result-a', stable_media_id: 'stable-video-a', retained: true }];
 return { page, navigations, setToken: value => { token = value; } };
}
const event = id => ({ currentTarget: { dataset: { id, index: 0 } } });
test('user chooses project note and insertion point before one adoption, then returns to its editor', async () => {
 const writes = [];
 const { page, navigations } = detail({
  pendingAdoption: () => null,
  getProjectNoteAdoptionTarget: async () => ({ project_id: 'p1', note_id: 'n1', title: '原方案', revision: 3, blocks: [{ kind: 'paragraph', text: '原正文' }, { kind: 'image', media_id: 'image-a' }] }),
  adoptProjectNote: async (mediaID, input) => { writes.push({ mediaID, input }); return { note_id: 'n1', project_id: 'p1', revision: 4 }; },
 }, {
  listProjectNoteProjects: async () => ({ items: [{ project_id: 'p1', name: '本人项目' }] }),
  getProjectNoteHistory: async () => ({ items: [{ note_id: 'n1', title: '原方案', revision: 3 }] }),
 });
 await page.startNoteAdoption(event()); assert.equal(page.data.adoptionProjects[0].name, '本人项目');
 await page.selectAdoptionProject(event('p1')); await page.selectAdoptionNote(event('n1'));
 assert.equal(page.data.adoptionPositions.length, 3); assert.equal(writes.length, 0);
 page.setAdoptionPosition({ detail: { value: '1' } }); await page.confirmNoteAdoption();
 assert.equal(writes.length, 1); assert.equal(writes[0].input.insert_at, 1); assert.equal(writes[0].input.expected_revision, 3);
 assert.equal(navigations[0], '/pages/projectnotes/projectnotes?project_id=p1&note_id=n1&view=editor');
});
test('cancelled or old-account project picker response cannot revive the adoption target', async () => {
 let release; const { page, setToken } = detail({ pendingAdoption: () => null }, { listProjectNoteProjects: () => new Promise(resolve => { release = resolve; }) });
 const pending = page.startNoteAdoption(event()); page.closeNoteAdoption(); setToken('account-b');
 release({ items: [{ project_id: 'old-private-project' }] }); await pending;
 assert.equal(page.data.noteAdoptionOpen, false); assert.equal(page.data.adoptionProjects.length, 0);
});

test('changing accounts clears protected task and adoption data before the next load', async () => {
  let release;
  const { page, setToken } = detail({ getBatch: () => new Promise(resolve => { release = resolve; }) }, {});
  page.data.batch = { batch_id: 'private-old-batch' }; page.data.noteAdoptionOpen = true;
  setToken('account-b'); const pending = page.onShow();
  assert.equal(page.data.batch, null); assert.equal(page.data.results.length, 0); assert.equal(page.data.noteAdoptionOpen, false);
  release({ batch_id: 'batch-1', status: 'completed', results: [] }); await pending;
});

test('content recovery replays the original owner before a revoked grant can release its pending attempt', async () => {
  const original = { draft_instance_id: 'old-stable-key', owner_id: 'owner-revoked', title: '原题' }, calls = [];
  let pending = original, contexts = 0;
  const { page } = detail({
    pendingAdoption: () => pending, idempotencyKey: () => 'new-key',
    adoptContent: async (_mediaID, input) => { calls.push(input); if (pending) { pending = null; throw Object.assign(new Error('原归属权限已撤销'), { statusCode: 403 }); } return { content_id: 'new-draft' }; },
  }, { workbenchContext: async () => { contexts++; return { owner_choices: [{ owner_id: 'owner-current', display_name: '当前归属' }] }; } });
  await page.adoptContent(event());
  assert.equal(contexts, 0); assert.equal(calls[0].owner_id, original.owner_id); assert.equal(calls[0].draft_instance_id, original.draft_instance_id);
  await page.adoptContent(event());
  assert.equal(contexts, 1); assert.equal(calls[1].owner_id, 'owner-current');
});

test('a late old-account content context neither submits nor releases the new-account adoption lock', async () => {
  let releaseA, releaseB; const requests = []; let contexts = 0;
  const { page, setToken } = detail({
    pendingAdoption: () => null, idempotencyKey: () => 'key',
    getBatch: async () => ({ batch_id: 'batch-b', status: 'completed', results: [{ result_id: 'result-b', stable_media_id: 'media-b', retained: true }] }),
    adoptContent: (mediaID, input) => { requests.push({ mediaID, input }); if (mediaID !== 'media-b') return Promise.reject(Object.assign(new Error('not found'), { statusCode: 404 })); return new Promise(resolve => { releaseB = resolve; }); },
  }, { workbenchContext: () => ++contexts === 1 ? new Promise(resolve => { releaseA = resolve; }) : Promise.resolve({ owner_choices: [{ owner_id: 'owner-b', display_name: 'B归属' }] }) });
  const first = page.adoptContent(event()); setToken('account-b'); await page.onShow();
  const second = page.adoptContent(event()); await Promise.resolve(); await Promise.resolve();
  releaseA({ owner_choices: [{ owner_id: 'owner-a', display_name: 'A归属' }] }); await first;
  assert.equal(requests.length, 1); assert.equal(requests[0].mediaID, 'media-b'); assert.equal(page.data.adoptingResultID, 'result-b');
  releaseB({ content_id: 'content-b' }); await second;
});

test('project and note retry buttons reload the first page instead of pretending to append', async () => {
  let projects = 0, notes = 0;
  const { page } = detail({ pendingAdoption: () => null }, {
    listProjectNoteProjects: async () => { if (++projects === 1) throw new Error('temporary'); return { items: [{ project_id: 'p', name: '项目' }] }; },
    getProjectNoteHistory: async () => { if (++notes === 1) throw new Error('temporary'); return { items: [{ note_id: 'n', title: '笔记' }] }; },
  });
  await page.startNoteAdoption(event()); await page.loadAdoptionProjects({ currentTarget: { dataset: {} } });
  assert.equal(projects, 2); assert.equal(page.data.adoptionProjects.length, 1);
  await page.selectAdoptionProject(event('p')); await page.loadAdoptionNotes({ currentTarget: { dataset: {} } });
  assert.equal(notes, 2); assert.equal(page.data.adoptionNotes.length, 1);
});
