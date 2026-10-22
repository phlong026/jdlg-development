// 全站排版一次收口：字号只用总控台那套档位（标题 20 / 分区 16 / 正文 14 / 辅助 12 / 大数字 24），
// 按钮、卡片圆角与主色走全局变量；一级页统一由布局层渲染同一种页面标题。
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import test from 'node:test';
import { fileURLToPath } from 'node:url';

const root = fileURLToPath(new URL('../src/', import.meta.url));
const walk = dir => fs.readdirSync(dir, { withFileTypes: true }).flatMap(entry => {
    const full = path.join(dir, entry.name);
    return entry.isDirectory() ? walk(full) : [full];
});
const files = walk(root);
const styles = files.filter(file => file.endsWith('.scss'));
const sources = files.filter(file => /\.tsx?$/.test(file));
const rel = file => path.relative(root, file);
const ALLOWED_PX = new Set([12, 14, 16, 20, 24]);

test('全局排版变量：标题 20、分区 16、正文 14、辅助 12、大数字 24；按钮 40（手机 44）、圆角 12、卡片 16、主色 139,94,60', () => {
    const app = fs.readFileSync(path.join(root, 'App.scss'), 'utf8');
    const rootBlock = app.slice(app.indexOf(':root'), app.indexOf('}', app.indexOf(':root')));
    const expected = {
        '--admin-font-title': '20px', '--admin-font-section': '16px', '--admin-font-body': '14px',
        '--admin-font-caption': '12px', '--admin-font-stat': '24px', '--admin-control-height': '40px',
        '--admin-radius-control': '12px', '--admin-radius-card': '16px', '--admin-accent': '#8b5e3c',
    };
    for (const [name, value] of Object.entries(expected)) assert.match(rootBlock, new RegExp(`${name}:\\s*${value};`), name);
    assert.match(app, /@media \(max-width: 768px\)[\s\S]*--admin-control-height:\s*44px/);
});

test('样式里的字号只用排版档位：不小于 12px、没有半像素', () => {
    const bad = [];
    for (const file of styles) {
        const text = fs.readFileSync(file, 'utf8');
        for (const match of text.matchAll(/font-size:\s*([^;}\n]+)/g)) {
            const value = match[1].trim().replace(/\s*!important$/, '');
            if (/^var\(--admin-font-(title|section|body|caption|stat)\)$/.test(value) || value === 'inherit' || /^\d+(\.\d+)?em$/.test(value)) continue;
            const px = /^(\d+(?:\.\d+)?)px$/.exec(value);
            if (px && ALLOWED_PX.has(Number(px[1]))) continue;
            bad.push(`${rel(file)}: ${value}`);
        }
    }
    assert.deepEqual(bad, []);
});

test('组件内联样式也不写小于 12px 或半像素的字号', () => {
    const bad = [];
    for (const file of sources) {
        const text = fs.readFileSync(file, 'utf8');
        for (const match of text.matchAll(/fontSize:\s*['"]?(\d+(?:\.\d+)?)(px)?/g)) {
            const size = Number(match[1]);
            if (!ALLOWED_PX.has(size)) bad.push(`${rel(file)}: ${match[0]}`);
        }
    }
    assert.deepEqual(bad, []);
});

test('分享访问记录的主色与全站一致', () => {
    const share = fs.readFileSync(path.join(root, 'pages/shareTracking.scss'), 'utf8');
    assert.match(share, /--st-accent:\s*var\(--admin-accent\)/);
    assert.doesNotMatch(share, /#94582f|148,\s*88,\s*47/i);
});

test('按钮与卡片的高度、圆角、主色由全局规则统一', () => {
    const app = fs.readFileSync(path.join(root, 'App.scss'), 'utf8');
    assert.match(app, /\.btn-primary[\s\S]*?min-height:\s*var\(--admin-control-height\)/);
    assert.match(app, /border-radius:\s*var\(--admin-radius-control\)/);
    assert.match(app, /background:\s*var\(--admin-accent\)/);
    assert.match(app, /\.section-card[\s\S]*?border-radius:\s*var\(--admin-radius-card\)/);
});

test('一级页标题统一由 PageTitle 渲染：布局层兜底，自带页头的页面也用同一个组件', () => {
    const layout = fs.readFileSync(path.join(root, 'component/layout.tsx'), 'utf8');
    assert.match(layout, /<PageTitle /);
    const title = fs.readFileSync(path.join(root, 'component/PageTitle.tsx'), 'utf8');
    assert.match(title, /className="admin-page-title"/);
    for (const page of ['dashboard', 'salesOverview', 'customerLeadsPage', 'customerSettings', 'aiOverview', 'aiPricing', 'aiContentAudits', 'shareTracking']) {
        const source = fs.readFileSync(path.join(root, `pages/${page}.tsx`), 'utf8');
        assert.match(source, /<PageTitle /, `${page} 用 PageTitle`);
        assert.doesNotMatch(source, /<h1[ >]/, `${page} 不再自写 h1`);
    }
});
