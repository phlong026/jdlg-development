import assert from 'node:assert/strict';
import test from 'node:test';
import { page, template, deferred, settle, nodes, textOf } from './template-test-harness.mjs';

test('first add renders without a selection and owns a working upload input', async () => {
  const h = await page();
  h.add();
  assert.equal(h.field('名称 *').props.value, '');
  assert.doesNotMatch(textOf(h.modal()), /运营信息|Creator|Needs revision/);
  h.button('选择图片并上传').props.onClick();
  assert.equal(h.file().ref.current.clicks, 1);
  await h.upload(); h.render();
  assert.equal(h.uploads.length, 1);
  assert.ok(nodes(h.modal()).some(node => node.type === 'img' && node.props.src === '/uploaded.jpg'));
  h.change('名称 *', 'Created');
  await h.button('保存').props.onClick(); h.render();
  assert.equal(h.creates.length, 1);
  assert.equal(h.creates[0].name, 'Created');
  assert.deepEqual(JSON.parse(h.creates[0].images), ['/uploaded.jpg']);
});

test('edit-only upload input is mounted, clickable, and saves uploaded images', async () => {
  const h = await page(); await h.edit();
  assert.match(textOf(h.modal()), /运营信息.*Creator/);
  h.button('选择图片并上传').props.onClick();
  assert.equal(h.file().ref.current.clicks, 1);
  await h.upload(); h.render();
  await h.button('保存').props.onClick();
  assert.deepEqual(JSON.parse(h.saves[0].patch.images), ['/original.jpg', '/uploaded.jpg']);
  assert.deepEqual(Object.keys(h.saves[0].patch).sort(), ['images', 'preview_url', 'thumbnail']);
});

test('name-only edit sends no old publication, moderation, pricing, tab or image fields', async () => {
  const h = await page(); await h.edit();
  h.change('名称 *', 'Renamed');
  await h.button('保存').props.onClick();
  assert.deepEqual(h.saves, [{ id: '1', patch: { name: 'Renamed' } }]);
});

test('explicit empty tab/description, zero price and false free flag are retained', async () => {
  const h = await page(); await h.edit();
  h.change('一级标签（可选）', '');
  h.change('描述', '');
  h.change('价格（灵石）', '0');
  h.field('免费').props.onChange({ target: { checked: false } }); h.render();
  await h.button('保存').props.onClick();
  assert.deepEqual(h.saves[0].patch, { description: '', main_tab: '', sub_tab: '', third_tab: '', price: 0, is_free: false });
});

test('unchanged edit makes no write, including structured image metadata', async () => {
  const h = await page(); await h.edit();
  await h.button('保存').props.onClick(); h.render();
  assert.equal(h.saves.length, 0);
  assert.equal(h.nodes().filter(node => node.props?.className === 'modal-overlay').length, 0);
});

test('closing edit and adding starts fresh; closing add and reopening edit reloads detail', async () => {
  const h = await page(); await h.edit();
  h.change('名称 *', 'Unsaved edit'); h.cancel(); h.add();
  assert.equal(h.field('名称 *').props.value, '');
  assert.equal(nodes(h.modal()).filter(node => node.type === 'img').length, 0);
  h.change('名称 *', 'Unsaved add'); h.cancel(); await h.edit();
  assert.equal(h.field('名称 *').props.value, 'Existing');
});

test('late uploads cannot append to another editor, and upload blocks saving', async () => {
  const pending = deferred();
  const h = await page({ upload: () => pending.promise });
  await h.edit();
  const upload = h.upload(); h.render();
  assert.equal(h.button('保存').props.disabled, true);
  await h.button('保存').props.onClick();
  assert.equal(h.saves.length, 0);
  h.cancel(); h.add();
  pending.resolve({ ok: true, json: async () => ({ code: 0, data: { url: '/late.jpg' } }) });
  await upload; h.render();
  assert.equal(nodes(h.modal()).filter(node => node.type === 'img').length, 0);
  assert.equal(h.button('保存').props.disabled, false);
});

test('a late detail request cannot replace an add editor', async () => {
  const pending = deferred();
  const h = await page({ api: { getTemplateDetail: () => pending.promise } });
  const edit = h.edit(); h.add();
  h.change('名称 *', 'New draft');
  pending.resolve(template()); await edit; h.render();
  assert.equal(h.field('名称 *').props.value, 'New draft');
  assert.match(textOf(h.modal()), /添加模板/);
});

test('save is synchronous single-flight and a failed save retains the editable draft', async () => {
  const pending = deferred(); let count = 0;
  const h = await page({ api: { updateTemplate: () => { count++; return pending.promise; } } });
  await h.edit(); h.change('名称 *', 'Retry me');
  const save = h.button('保存').props.onClick;
  const first = save(); await save();
  assert.equal(count, 1);
  pending.reject(new Error('Conflict: reload')); await first; h.render();
  assert.equal(h.field('名称 *').props.value, 'Retry me');
  assert.deepEqual(h.alerts, ['Conflict: reload']);
  assert.equal(h.button('保存').props.disabled, false);
});

test('committed save with unavailable read-back closes editor and asks for refresh without resubmitting', async () => {
  let count = 0;
  const h = await page({ api: { updateTemplate: async () => { count++; return null; } } });
  await h.edit(); h.change('名称 *', 'Saved');
  await h.button('保存').props.onClick(); await settle(); h.render();
  assert.equal(count, 1);
  assert.equal(h.nodes().filter(node => node.props?.className === 'modal-overlay').length, 0);
  assert.deepEqual(h.alerts, ['修改已保存，但最新数据暂时无法读取，请刷新列表核实']);
});

test('removing the last edit image sends explicit empty image fields', async () => {
  const h = await page(); await h.edit();
  h.button('删除').props.onClick(); h.render();
  await h.button('保存').props.onClick();
  assert.deepEqual(h.saves[0].patch, { thumbnail: '', preview_url: '', images: '' });
});
