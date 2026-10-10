const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const { test } = require('node:test');
const ts = require('./report-runtime')('typescript');

function flow() {
  const file = path.join(__dirname, '../miniprogram/pages/report/flow.ts');
  const exports = {};
  vm.runInNewContext(ts.transpileModule(fs.readFileSync(file, 'utf8'), {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 },
  }).outputText, { exports });
  return exports;
}

test('five steps collect decisions, not fabricated site facts', () => {
  const f = flow();
  const input = f.emptyInput();
  assert.ok(f.validateStep(input, 0));
  input.site = { region: '福建省 泉州市', width_m: 12, depth_m: 10, road_side: '待确认' };
  assert.equal(f.validateStep(input, 0), '');
  input.site.width_m = Infinity;
  assert.ok(f.validateStep(input, 0));
  input.preference.priorities = ['老人起居', '老人起居', '预算控制'];
  assert.ok(f.validateStep(input, 3));
});

test('a phone reading has four ordered sections, not fourteen squeezed PDF sheets', () => {
  const f = flow();
  const snapshot = { facts: { region: '泉州', family: '三代同堂' }, pages: [
    { number: 1, kind: 'cover', title: '家的方向' },
    { number: 2, kind: 'evidence', title: '判断依据' },
    { number: 3, kind: 'profile', title: '家庭画像' },
    { number: 4, kind: 'priorities', title: '先解决什么' },
  ] };
  assert.equal(f.previewSections(snapshot).length, 4);
  assert.throws(() => f.previewSections({ ...snapshot, pages: snapshot.pages.slice(0, 3) }));
  assert.throws(() => f.previewSections({ ...snapshot, pages: [...snapshot.pages].reverse() }));
});

test('payment callback never grants PDF access: server paid, bound and ready all required', () => {
  const f = flow();
  assert.equal(f.canOpenFull({ paid: false, bound: true, status: 'ready' }), false);
  assert.equal(f.canOpenFull({ paid: true, bound: false, status: 'ready' }), false);
  assert.equal(f.canOpenFull({ paid: true, bound: true, status: 'full_generating' }), false);
  assert.equal(f.canOpenFull({ paid: true, bound: true, status: 'ready' }), true);
  assert.equal(f.shouldPoll('preview_ready'), false);
  assert.equal(f.shouldPoll('failed'), false);
  assert.equal(f.shouldPoll('full_queued'), true);
});

test('house form builds server input without losing priorities, rooms or unknown budget', () => {
 const f = flow(); const input = f.emptyInput();
 Object.assign(input.site, { region: '福建泉州', width_m: 12, depth_m: 10 });
 Object.assign(input.household, { family_type: '返乡度假', residents: '父母常住，子女节假日回来' });
 Object.assign(input.space, { floors: 2, bedrooms: 4 });
 Object.assign(input.preference, { style: '新闽派', priorities: ['老人起居', '预算控制'], goal: '父母方便起居' });
 const house = f.emptyHouseDetails();
 Object.assign(house, { permanent_count: 2, returning_count: 4, return_frequency: 'holidays', elder_room: 'yes', courtyard: 'yes', parking: 'no', flexible_use: 'yes' });
 house.rooms = [{ id: 'study', name: '书房', count: 1, preferred_floor: 2, required: true }];
 assert.equal(f.validateHouseStep(input, house, 1), '');
 const data = f.houseRequestInput(input, house);
 assert.equal(data.permanent_count, 2); assert.equal(data.returning_count, 4);
 assert.equal(data.house.first_floor_elder_room, true); assert.equal(data.house.rooms[0].preferred_floor, 2);
 assert.deepEqual(Array.from(data.priorities), ['老人起居', '预算控制']);
 assert.equal(data.budget_wan, undefined); assert.equal(data.house.courtyard, 'yes');
 house.return_frequency = 'none'; assert.ok(f.validateHouseStep(input, house, 1));
 house.returning_count = 0; assert.equal(f.validateHouseStep(input, house, 1), '');
 house.rooms[0].preferred_floor = 3; assert.ok(f.validateHouseStep(input, house, 2));
});

test('modern reports have four matching preview blocks and optional enterprise binding', () => {
 const f = flow();
 const snapshot = { meta: { protocol_version: 'report-house-v1' }, facts: {}, pages: ['house_profile','house_constraints','house_goals','house_confirmation'].map((kind,i)=>({number:i+1,kind})) };
 assert.equal(f.previewSections(snapshot).length, 4);
 for (const protocol_version of ['report-house-v1','report-decision-v5']) {
  assert.equal(f.canOpenFull({ protocol_version, paid:true, bound:false, status:'ready' }), true);
  assert.equal(f.canOpenFull({ protocol_version, paid:false, bound:false, status:'ready' }), false);
 }
 assert.equal(f.canOpenFull({ paid:true,bound:false,status:'ready' }),false);
});

test('house optional perspective text matches server minimum length without losing user input', () => {
 const f=flow(),input=f.emptyInput(),house=f.emptyHouseDetails();
 input.household.residents='父'; assert.match(f.validateHouseStep(input,house,1),/人数|至少/);
 house.permanent_count=2;house.returning_count=0;
 assert.match(f.validateHouseStep(input,house,1),/至少/);
 input.household.residents='父母';assert.equal(f.validateHouseStep(input,house,1),'');
 input.preference.style='新中式';input.preference.priorities=['老人起居'];input.preference.goal='静';
 assert.match(f.validateHouseStep(input,house,3),/至少/);
});
