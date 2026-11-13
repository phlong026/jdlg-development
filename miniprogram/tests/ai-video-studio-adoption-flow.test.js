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
