// 手机端紧凑：筛选 / 操作区默认收起，点开才显示；桌面端原样显示全部筛选。
import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { createRequire } from 'node:module';
import ts from 'typescript';
import React from 'react';

const require = createRequire(import.meta.url);

/** 收集元素树里的全部文字（只走 children，图标桩渲染为空） */
const text = node => Array.isArray(node) ? node.map(text).join('')
    : node == null || typeof node === 'boolean' ? ''
    : typeof node !== 'object' ? String(node)
    : text(node.props?.children);

/** 在元素树里找第一个满足条件的元素 */
const find = (node, match) => {
    if (Array.isArray(node)) { for (const child of node) { const hit = find(child, match); if (hit) return hit; } return null; }
    if (!node || typeof node !== 'object') return null;
    return match(node) ? node : find(node.props?.children, match);
};

/** 载入组件源码；useState 由测试接管，isMobile 由参数控制 */
function load(file, mobile) {
    const h = { slots: [], cursor: 0 };
    const react = { ...React, useState(initial) {
        const i = h.cursor++;
        if (!(i in h.slots)) h.slots[i] = typeof initial === 'function' ? initial() : initial;
        return [h.slots[i], value => { h.slots[i] = typeof value === 'function' ? value(h.slots[i]) : value; }];
    } };
    const mod = { exports: {} };
    const source = fs.readFileSync(new URL(`../src/component/${file}`, import.meta.url), 'utf8');
    const code = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020, jsx: ts.JsxEmit.ReactJSX, esModuleInterop: true } }).outputText;
    vm.runInNewContext(code, { module: mod, exports: mod.exports, require(name) {
        if (name === 'react') return react;
        if (name === 'react/jsx-runtime') return require(name);
        if (name === 'react-icons/fi') return new Proxy({}, { get: () => () => null });
        if (name === './useIsMobile') return { useIsMobile: () => mobile };
        if (name.endsWith('.scss')) return {};
        throw new Error(`Unexpected dependency: ${name}`);
    } });
    const Component = mod.exports.default;
    return props => { h.cursor = 0; return Component(props); };
}

const filters = React.createElement('select', null, React.createElement('option', null, '全部状态'));

test('MobileFold 桌面端原样渲染筛选，不加折叠按钮', () => {
    const render = load('MobileFold.tsx', false);
    const tree = render({ label: '筛选', children: filters });
    assert.equal(text(tree), '全部状态');
    assert.equal(find(tree, n => n.type === 'button'), null);
});

test('MobileFold 手机端默认收起，点开显示，按钮上提示已生效的筛选数', () => {
    const render = load('MobileFold.tsx', true);
    let tree = render({ label: '筛选', activeCount: 2, children: filters });
    assert.doesNotMatch(text(tree), /全部状态/);
    assert.match(text(tree), /筛选/);
    assert.match(text(tree), /2/);

    find(tree, n => n.type === 'button').props.onClick();
    tree = render({ label: '筛选', activeCount: 2, children: filters });
    assert.match(text(tree), /全部状态/);
    assert.match(text(tree), /收起/);
});

test('MobileFold disabled 时手机端也直接显示（例如还没选用户时的搜索框）', () => {
    const render = load('MobileFold.tsx', true);
    const tree = render({ label: '切换用户', disabled: true, children: filters });
    assert.equal(text(tree), '全部状态');
});

const panelProps = {
    title: '设计师检索与运营处理',
    description: '先按认证状态找到设计师',
    actions: React.createElement('button', null, '刷新'),
    controls: filters,
    summary: '共 23 位',
};

test('ManagementSearchPanel 手机端只留一行开关：标题、说明、动作、筛选都收起', () => {
    const render = load('managementSearchPanel.tsx', true);
    let tree = render(panelProps);
    const collapsed = text(tree);
    for (const hidden of ['设计师检索与运营处理', '先按认证状态找到设计师', '刷新', '全部状态', '共 23 位']) {
        assert.ok(!collapsed.includes(hidden), `收起态不应出现「${hidden}」`);
    }
    assert.match(collapsed, /筛选与操作/);

    find(tree, n => n.type === 'button').props.onClick();
    tree = render(panelProps);
    const expanded = text(tree);
    for (const shown of ['刷新', '全部状态', '共 23 位']) assert.ok(expanded.includes(shown), `展开后应出现「${shown}」`);
    assert.ok(!expanded.includes('设计师检索与运营处理'), '手机端不重复页名标题');
});

test('ManagementSearchPanel 桌面端显示全部：标题、说明、动作、筛选与摘要', () => {
    const render = load('managementSearchPanel.tsx', false);
    const all = text(render(panelProps));
    for (const shown of ['设计师检索与运营处理', '先按认证状态找到设计师', '刷新', '全部状态', '共 23 位']) {
        assert.ok(all.includes(shown), `桌面端应出现「${shown}」`);
    }
    assert.ok(!all.includes('筛选与操作'));
});
