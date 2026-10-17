import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import ts from 'typescript';
import { createRequire } from 'node:module';
import { environment, lead, nodes, textOf } from './helpers/crm-harness.mjs';
const require = createRequire(import.meta.url), React = require('react');
const flush = () => new Promise(r => setImmediate(r));

// 线索按账号身份分流：乡墅业主=销售线索，未选身份=身份待确认（仍进销售），B 端=合作线索。
const rows = [
    { ...lead, id: 1, name: '业主', identity_type: '乡墅业主', lead_type: 'sales' },
    { ...lead, id: 2, name: '设计师', identity_type: '设计师', lead_type: 'partner' },
    { ...lead, id: 3, name: '未选身份', identity_type: '', lead_type: 'identity_pending' },
    // 旧接口未返回 lead_type 时按身份推导
    { ...lead, id: 4, name: '施工队', identity_type: '施工队' },
];
const tags = h => h.all(n => typeof n.type === 'function' && n.type.name === 'IdentityTag').map(n => { const span = n.type(n.props); return { id: n.props.item.id, className: span.props.className, text: textOf(span) }; });

test('lead list labels every lead by identity and keeps B-side as partner leads', async () => {
    const env = environment(); env.leadApi.getCustomerLeadList = async () => ({ list: rows, total: rows.length });
    const h = await env.mount('customerLeads.tsx');
    const byId = Object.fromEntries(tags(h).map(tag => [tag.id, tag]));
    assert.match(byId[1].className, /identity-sales/); assert.equal(byId[1].text, '乡墅业主');
    assert.match(byId[2].className, /identity-partner/); assert.equal(byId[2].text, '合作线索 · 设计师');
    assert.match(byId[3].className, /identity-pending/); assert.equal(byId[3].text, '身份待确认');
    assert.match(byId[4].className, /identity-partner/); assert.equal(byId[4].text, '合作线索 · 施工队');
});

test('lead type filter is sent to the list API and resets paging', async () => {
    const env = environment(); const calls = []; env.leadApi.getCustomerLeadList = async params => { calls.push(params); return { list: rows, total: rows.length }; };
    const h = await env.mount('customerLeads.tsx');
    assert.equal(calls.at(-1).lead_type, undefined);
    const select = h.find(n => n.type === 'select' && n.props['aria-label'] === '线索类型');
    assert.deepEqual(nodes(select).filter(n => n.type === 'option').map(n => n.props.value), ['all', 'sales', 'identity_pending', 'partner']);
    select.props.onChange({ target: { value: 'partner' } }); await h.flush();
    assert.equal(calls.at(-1).lead_type, 'partner'); assert.equal(calls.at(-1).page, 1);
});

test('detail advice routes partner leads to admins and asks to confirm unknown identity', async () => {
    for (const [row, advice] of [[rows[1], /合作线索：由管理员确认合作方式后统一处理/], [rows[2], /联系时确认是否为建房业主/]]) {
        const env = environment(); env.leadApi.getCustomerLeadList = async () => ({ list: [row], total: 1 }); env.leadApi.getCustomerLeadDetail = async () => ({ lead: row, events: [], followups: [] });
        const h = await env.mount('customerLeads.tsx'); await h.button('详情').props.onClick(); await h.flush();
        assert.match(h.text(), advice);
    }
});

function convertHarness(visitor, result = { lead_id: 9, existed: false }) {
    let index = 0, slots = [], pending = [], tree; const calls = [], navigation = [];
    const react = { ...React, useState(init) { const i = index++; if (!(i in slots)) slots[i] = typeof init === 'function' ? init() : init; return [slots[i], v => slots[i] = typeof v === 'function' ? v(slots[i]) : v]; }, useRef(init) { const i = index++; if (!(i in slots)) slots[i] = { current: init }; return slots[i]; }, useEffect(fn, deps) { const i = index++; const old = slots[i]; if (!old || deps.some((v, j) => v !== old.deps[j])) { slots[i] = { deps, cleanup: old?.cleanup }; pending.push(() => { slots[i].cleanup?.(); slots[i].cleanup = fn(); }); } } };
    const transpile = file => ts.transpileModule(fs.readFileSync(new URL(file, import.meta.url), 'utf8'), { compilerOptions: { module: ts.ModuleKind.CommonJS, jsx: ts.JsxEmit.ReactJSX, esModuleInterop: true, target: ts.ScriptTarget.ES2020 } }).outputText;
    const model = { exports: {} }; vm.runInNewContext(transpile('../src/pages/shareTracking/model.ts'), { module: model, exports: model.exports, require });
    const module = { exports: {} };
    vm.runInNewContext(transpile('../src/pages/shareTracking/convertLead.tsx'), { module, exports: module.exports, AbortController, Error, require(name) { if (name === 'react') return react; if (name === 'react-router-dom') return { useNavigate: () => path => navigation.push(path) }; if (name === '../../api/shareTracking') return { tracking: { convertLead: async key => { calls.push(key); return result; } } }; if (name === './model') return model.exports; return require(name); } });
    const h = { calls, navigation, render() { index = 0; tree = module.exports.ConvertLeadButton({ visitor, canCRM: true }); pending.splice(0).forEach(fn => fn()); return tree; }, text: () => textOf(tree), buttons: () => nodes(tree).filter(n => n.type === 'button') };
    h.render(); h.render(); return h;
}

test('convert button: homeowner, unselected identity and B-side are all convertible with matching wording', async () => {
    const cases = [
        ['乡墅业主', '转为销售线索', /访问动机转为销售线索/, '已创建销售线索 #9', '/customer-leads'],
        ['', '转为销售线索', /标记身份待确认/, '已创建销售线索（身份待确认） #9', '/customer-leads'],
        ['设计师', '转为合作线索', /该访客为设计师，将转为合作线索/, '已创建合作线索 #9', '/customer-leads?queue=partner'],
    ];
    for (const [identity, label, confirm, done, destination] of cases) {
        const h = convertHarness({ visitor_key: 'u:42', identity_state: 'identified', identity_type: identity });
        assert.equal(textOf(h.buttons()[0]), label, identity);
        h.buttons()[0].props.onClick(); h.render(); assert.match(h.text(), confirm);
        await h.buttons().find(b => textOf(b) === '确认转化').props.onClick(); await flush(); h.render();
        assert.deepEqual(h.calls, ['u:42']); assert.match(h.text(), new RegExp(done.replace(/[()#]/g, '\\$&')));
        h.buttons().find(b => textOf(b) === '前往线索工作台').props.onClick(); assert.deepEqual(h.navigation, [destination]);
    }
});

test('convert button stays hidden for anonymous visitors and unknown identities', () => {
    for (const visitor of [{ visitor_key: 'a:1', identity_state: 'anonymous', identity_type: '' }, { visitor_key: 'u:7', identity_state: 'identified', identity_type: '魔法师' }]) {
        assert.equal(convertHarness(visitor).render(), null);
    }
});

test('stage ring counts only sales leads and notes partner leads separately', async () => {
    const env = environment();
    const overview = { total_count: 12, new_count: 4, contacted_count: 3, converted_count: 2, invalid_count: 1, partner_count: 2 };
    const h = await env.mount('customerLeadHub.tsx', 'default', { overview, loading: false, updatedAt: null, onRefresh() {} });
    assert.match(h.text(), /销售线索阶段 · 共 10 条 · 合作线索 2 条另计/);
    assert.match(h.text(), /新线索4\(40%\)/);
    assert.match(h.text(), /合作线索另计2 条/);
    const none = await environment().mount('customerLeadHub.tsx', 'default', { overview: { ...overview, partner_count: 0 }, loading: false, updatedAt: null, onRefresh() {} });
    assert.doesNotMatch(none.text(), /合作线索/);
});
