import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import test from 'node:test';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import ts from 'typescript';
import * as sass from 'sass';
import postcss from 'postcss';

// Compiled CSS/source-structure and actual controller checks, NOT browser rendering.
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const read = file => fs.readFileSync(path.join(root, file), 'utf8');
const compile = file => postcss.parse(sass.compile(path.join(root, file), { logger: sass.Logger.silent }).css);
const css = compile('src/responsive-remaining.scss');
const pages = { templates: '/templates', utilityTools: '/utility-tools', inspirations: '/inspirations', designerCenter: '/designer-center', supportTickets: '/support-tickets', riskControl: '/risk-control', distribution: '/distribution', contentAnalytics: '/content-analytics', reportCenter: '/report-center', oss: '/oss', logs: '/logs', opportunities: '/opportunities' };
const widths = [320, 375, 390, 768, 769, 800, 900, 1024, 1440];
const find = (node, predicate, out = []) => { if (predicate(node)) out.push(node); ts.forEachChild(node, child => { find(child, predicate, out); }); return out; };
const parse = file => ts.createSourceFile(file, read(file), ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
function active(rule, width, height = 800, touch = false) {
  for (let parent = rule.parent; parent; parent = parent.parent) {
    if (parent.type !== 'atrule' || parent.name !== 'media') continue;
    for (const [, kind, dimension, value] of parent.params.matchAll(/(max|min)-(width|height):\s*([\d.]+)px/g)) {
      const actual = dimension === 'width' ? width : height;
      if (kind === 'max' ? actual > Number(value) : actual < Number(value)) return false;
    }
    if (parent.params.includes('hover: none') && !touch) return false;
  }
  return true;
}
function declarations(route, leaf, width = 1440, height = 800, touch = false) {
  const selector = `.layout-content[data-admin-page="${route}"] ${leaf}`, found = {};
  css.walkRules(rule => {
    if (!active(rule, width, height, touch) || !rule.selectors.includes(selector)) return;
    rule.walkDecls(decl => { if (!found[decl.prop]?.important || decl.important) found[decl.prop] = decl; });
  });
  return Object.fromEntries(Object.entries(found).map(([prop, decl]) => [prop, decl.value]));
}

test('remaining CSS is imported once after prior responsive rules and keeps an explicit non-CRM route allowlist', () => {
  const imports = find(parse('src/App.tsx'), ts.isImportDeclaration).map(node => node.moduleSpecifier.text);
  assert.equal(imports.filter(value => value === './responsive-remaining.scss').length, 1);
  assert.ok(imports.indexOf('./responsive-remaining.scss') > imports.indexOf('./responsive-priority.scss'));
  css.walkRules(rule => { for (const selector of rule.selectors) {
    const match = selector.match(/^\.layout-content\[data-admin-page="([^"]+)"\] /);
    assert.ok(match && Object.values(pages).includes(match[1]), selector); assert.doesNotMatch(selector, /customer|crm/);
  } });
});
for (const [page, route] of Object.entries(pages)) test(`${route}: lazy styles are isolated; table scroll parents exclude headings and actions`, () => {
  compile(`src/pages/${page}.scss`).walkRules(rule => {
    for (let parent = rule.parent; parent; parent = parent.parent) if (parent.type === 'atrule' && /keyframes$/.test(parent.name)) return;
    for (const selector of rule.selectors) assert.ok(selector.includes(`data-admin-page="${route}"`), selector);
  });
  for (const table of find(parse(`src/pages/${page}.tsx`), node => ts.isJsxElement(node) && node.openingElement.tagName.getText() === 'table')) {
    assert.ok(ts.isJsxElement(table.parent));
    assert.equal(table.parent.children.filter(node => !ts.isJsxText(node) || node.text.trim()).length, 1, `${page}: scroller has non-table content`);
    assert.equal(table.parent.openingElement.tagName.getText(), 'div');
  }
});

test('width matrix keeps long statistics shrinkable and primary/inline/upload controls at least 44px', () => {
  for (const width of widths) for (const route of Object.values(pages)) {
    assert.equal(declarations(route, '.stat-card', width)['min-width'], '0');
    for (const leaf of ['.stat-value', '.designer-summary-value', '.summary-chip span']) assert.equal(declarations(route, leaf, width)['overflow-wrap'], 'anywhere');
    for (const leaf of ['button', '.upload-trigger', '.upload-label', '.btn-text-link', '.btn-link']) {
      assert.equal(declarations(route, leaf, width)['min-height'], '44px', `${width} ${route} ${leaf}`);
      assert.equal(declarations(route, leaf, width)['min-width'], '44px');
    }
    for (const leaf of ['input:not([type=checkbox]):not([type=radio])', 'select', 'textarea']) {
      const value = declarations(route, leaf, width); assert.equal(value['min-width'], '0', `${width} ${route} ${leaf}`); assert.equal(value['min-height'], '44px');
    }
  }
});

test('filters cover 769/800/900 as well as mobile; fixed-content cards and inputs have shrink paths', () => {
  for (const width of widths) for (const route of Object.values(pages)) {
    const controls = declarations(route, '.management-search-panel .management-search-panel__controls', width);
    if (width <= 960) {
      assert.equal(controls.display, 'grid'); assert.equal(controls['grid-template-columns'], width <= 560 ? 'minmax(0, 1fr)' : 'repeat(2, minmax(0, 1fr))');
      assert.equal(declarations(route, '.management-search-panel__controls > *', width)['min-width'], '0');
      assert.equal(declarations(route, '.management-search-searchbox', width)['grid-column'], '1/-1');
    } else assert.equal(controls.display, undefined);
  }
  assert.equal(declarations('/designer-center', '.designer-search-box input')['min-width'], '0');
  assert.equal(declarations('/templates', '.case-item')['min-width'], '0');
  assert.equal(declarations('/templates', '.templates-grid')['grid-template-columns'], 'repeat(auto-fill, minmax(min(100%, 300px), 1fr))');
});

test('dedicated table regions scroll before observer initialization and do not make whole panels scroll', () => {
  const tables = { '/support-tickets': 'support', '/risk-control': 'risk', '/distribution': 'distribution', '/content-analytics': 'analytics', '/report-center': 'report', '/oss': 'oss', '/logs': 'logs' };
  for (const width of widths) for (const [route, prefix] of Object.entries(tables)) {
    const value = declarations(route, `.${prefix}-table-container`, width); assert.equal(value['overflow-x'], 'auto'); assert.equal(value['max-width'], '100%'); assert.equal(value['min-width'], '0');
  }
  for (const route of ['/oss', '/logs']) assert.equal(declarations(route, `${route}-table`.replace('/', '.'))['min-width'], '980px');
  css.walkDecls('overflow-x', decl => assert.ok(decl.parent.selectors.every(selector => selector.endsWith('-table-container'))));
});

test('sticky applies only to verified risk/support/OSS action columns and never hides any data column', () => {
  for (const [route, page, prefix] of [['/risk-control', 'riskControl', 'risk'], ['/support-tickets', 'supportTickets', 'support'], ['/oss', 'oss', 'oss']]) {
    for (const table of find(parse(`src/pages/${page}.tsx`), node => ts.isJsxElement(node) && node.openingElement.tagName.getText() === 'table')) {
      const headers = find(table, node => ts.isJsxElement(node) && node.openingElement.tagName.getText() === 'th'); assert.match(headers.at(-1).getText(), /操作|建议动作/);
    }
    for (const width of widths) {
      const cell = declarations(route, `.${prefix}-table td:last-child:not([colspan])`, width); assert.equal(cell.position, width <= 768 ? 'sticky' : undefined);
      if (width <= 768) { assert.equal(cell['min-width'], '128px'); assert.equal(cell.background, 'var(--admin-surface)'); }
    }
  }
  css.walkDecls('position', decl => { if (decl.value === 'sticky') for (const selector of decl.parent.selectors) assert.match(selector, /data-admin-page="\/(risk-control|support-tickets|oss)"/); });
  css.walkDecls('display', decl => assert.notEqual(decl.value, 'none')); css.walkDecls('visibility', decl => assert.notEqual(decl.value, 'hidden'));
});

test('short-screen modal rules keep header/footer outside body scroll and explicitly override the old mobile !important rule', () => {
  let previous;
  compile('src/App.scss').walkRules(rule => { if (rule.selectors.includes('.layout-content .modal-content')) rule.walkDecls('overflow', decl => { previous = decl; }); });
  assert.equal(previous?.value, 'auto'); assert.equal(previous?.important, true);
  for (const route of ['/templates', '/utility-tools', '/inspirations', '/support-tickets', '/oss']) {
    for (const width of widths) for (const height of [320, 480, 800]) {
      const dialog = declarations(route, '.modal-content', width, height); assert.equal(dialog.display, 'flex'); assert.equal(dialog['flex-direction'], 'column'); assert.equal(dialog.overflow, 'hidden'); assert.match(dialog['max-height'], /100dvh/);
      if (width <= 768) assert.match(dialog['max-height'], /safe-area-inset-top/);
      const body = declarations(route, '.modal-body', width, height); assert.equal(body['min-height'], '0'); assert.equal(body['overflow-y'], 'auto'); assert.equal(body.flex, '1 1 auto');
      for (const leaf of ['.modal-header', '.modal-footer']) assert.equal(declarations(route, leaf, width, height)['flex-shrink'], '0');
      assert.match(declarations(route, '.modal-footer', width, height)['padding-bottom'], /safe-area-inset-bottom/);
    }
    let own;
    css.walkRules(rule => { if (rule.selectors.includes(`.layout-content[data-admin-page="${route}"] .modal-content`)) rule.walkDecls('overflow', decl => { own = decl; }); });
    assert.equal(own.important, true);
    // route + attribute + class specificity (0,3,0) beats shared mobile (0,2,0).
    assert.ok(own.parent.selector.includes('[data-admin-page='));
  }
});

test('template preview controls remain discoverable by touch and keyboard focus without hover', () => {
  assert.equal(declarations('/templates', '.template-thumbnail:focus-within .template-overlay').opacity, '1');
  assert.equal(declarations('/templates', '.template-overlay', 390, 800, true).opacity, '1');
  assert.equal(declarations('/templates', '.template-overlay', 1440, 800, false).opacity, undefined);
});

test('production table observer handles width matrix, resize, route replacement and cleanup', () => {
  const effect = find(parse('src/component/layout.tsx'), node => ts.isCallExpression(node) && node.expression.getText() === 'useEffect' && node.arguments[0]?.getText().includes('const syncTables ='))[0].arguments[0];
  assert.ok(effect, 'execute actual production effect, not a copy');
  const region = (clientWidth, scrollWidth) => ({ clientWidth, scrollWidth, dataset: {}, attributes: {}, setAttribute(key, value) { this.attributes[key] = value; } });
  let tables = []; const heading = { dataset: {} };
  const rootElement = { querySelectorAll: selector => { assert.equal(selector, 'table'); return tables; }, contains: element => tables.some(table => table === element || table.parentElement === element) };
  const observers = [], mutations = [], frames = new Map(); let sequence = 0;
  const context = vm.createContext({ contentRef: { current: rootElement },
    ResizeObserver: class { constructor(callback) { this.callback = callback; this.observed = new Set(); observers.push(this); } observe(node) { this.observed.add(node); } unobserve(node) { this.observed.delete(node); } disconnect() { this.disconnected = true; } },
    MutationObserver: class { constructor(callback) { this.callback = callback; mutations.push(this); } observe() {} disconnect() { this.disconnected = true; } },
    requestAnimationFrame: callback => { frames.set(++sequence, callback); return sequence; }, cancelAnimationFrame: id => frames.delete(id),
  });
  const flush = () => { const callbacks = [...frames.values()]; frames.clear(); callbacks.forEach(callback => callback()); };
  vm.runInContext(ts.transpileModule(`globalThis.setup = ${effect.getText()}`, { compilerOptions: { target: ts.ScriptTarget.ES2020 } }).outputText, context);
  const cleanup = context.setup();
  for (const width of widths) {
    const parent = region(width - 24, 980); tables = [{ parentElement: parent }]; mutations[0].callback(); flush();
    const overflowing = parent.scrollWidth > parent.clientWidth + 1;
    assert.equal(parent.dataset.adminTableScroll, overflowing ? 'overflow' : 'fit'); assert.equal(parent.tabIndex, overflowing ? 0 : -1); assert.equal(parent.attributes.role, 'region');
    assert.equal(parent.attributes['aria-label'].includes('左右滚动'), overflowing); assert.equal(observers[0].observed.size, 2);
    parent.scrollWidth = parent.clientWidth; observers[0].callback(); observers[0].callback(); assert.equal(frames.size, 1); flush(); assert.equal(parent.dataset.adminTableScroll, 'fit'); assert.equal(parent.tabIndex, -1);
  }
  assert.deepEqual(heading.dataset, {}); mutations[0].callback(); cleanup(); assert.equal(frames.size, 0); assert.equal(observers[0].disconnected, true); assert.equal(mutations[0].disconnected, true);
});

test('inspiration and OSS file pickers expose native keyboard buttons rather than unfocusable upload labels', () => {
  for (const [page, label] of [['inspirations', '选择图片并上传'], ['oss', '选择文件']]) {
    const tree = parse(`src/pages/${page}.tsx`);
    const buttons = find(tree, node => ts.isJsxElement(node) && node.openingElement.tagName.getText() === 'button' && node.children.some(child => child.getText().includes(label)));
    assert.equal(buttons.length, 1);
    const attrs = buttons[0].openingElement.attributes.properties;
    assert.ok(attrs.some(attr => attr.name?.getText() === 'type' && attr.initializer?.text === 'button'));
    assert.ok(attrs.some(attr => attr.name?.getText() === 'onClick')); assert.ok(attrs.some(attr => attr.name?.getText() === 'disabled'));
    assert.equal(attrs.some(attr => attr.name?.getText() === 'onKeyDown'), false, 'native Enter/Space must not double-fire a custom keyboard handler');
    const files = find(tree, node => ts.isJsxSelfClosingElement(node) && node.tagName.getText() === 'input' && node.attributes.properties.some(attr => attr.name?.getText() === 'type' && attr.initializer?.text === 'file'));
    assert.equal(files.length, 1); assert.ok(files[0].attributes.properties.some(attr => attr.name?.getText() === 'ref'));
    assert.equal(find(buttons[0], node => files.includes(node)).length, 0, 'file input stays separate from the button');
  }
});

test('utility titles and previews have their own shrink/wrap rules without hiding publish status', () => {
  const tree = parse('src/pages/utilityTools.tsx');
  const classNode = className => find(tree, node => ts.isJsxElement(node) && node.openingElement.attributes.properties.some(attr => attr.name?.getText() === 'className' && attr.initializer?.text === className));
  const header = classNode('tool-header'); assert.equal(header.length, 1);
  assert.equal(find(header[0], node => ts.isJsxElement(node) && node.openingElement.tagName.getText() === 'h4').length, 1);
  assert.equal(classNode('tool-preview').length, 1);
  for (const width of widths) {
    for (const leaf of ['.tool-header h4', '.tool-preview']) {
      const value = declarations('/utility-tools', leaf, width);
      assert.equal(value['min-width'], '0'); assert.equal(value['max-width'], '100%'); assert.equal(value['overflow-wrap'], 'anywhere');
      assert.notEqual(value.display, 'none');
    }
    assert.equal(declarations('/utility-tools', '.tool-header h4', width).flex, '1 1 auto', 'mobile column layout must not reserve a 120px title height');
    assert.equal(declarations('/utility-tools', '.tool-header', width)['flex-wrap'], 'wrap');
    assert.equal(declarations('/utility-tools', '.tool-header .tool-status', width)['flex-shrink'], '0');
  }
});

test('template and utility editors scroll through a div body; the fieldset only carries the disabled state', () => {
  for (const file of ['templates', 'utilityTools']) {
    const tree = parse(`src/pages/${file}.tsx`);
    const withClass = (tag, name) => find(tree, node => ts.isJsxElement(node) && node.openingElement.tagName.getText() === tag
      && node.openingElement.attributes.properties.some(attr => attr.name?.getText() === 'className' && attr.initializer?.text === name));
    assert.equal(withClass('fieldset', 'modal-body').length, 0, `${file}: a fieldset cannot be the scroll container`);
    const fields = withClass('fieldset', 'modal-fields'); assert.ok(fields.length > 0);
    for (const fieldset of fields) {
      const parent = fieldset.parent;
      assert.ok(ts.isJsxElement(parent) && parent.openingElement.tagName.getText() === 'div' && parent.openingElement.attributes.properties.some(attr => attr.name?.getText() === 'className' && attr.initializer?.text === 'modal-body'));
    }
  }
  for (const route of ['/templates', '/utility-tools']) {
    const fields = declarations(route, 'fieldset.modal-fields', 390, 480);
    assert.equal(fields['min-width'], '0'); assert.equal(fields.border, '0');
  }
  assert.match(read('src/pages/utilityTools.scss'), /\.modal-body\[hidden\]\s*\{\s*display:\s*none/);
});

test('inspirations toolbar stacks on phones so search, filters and actions each get a full row', () => {
  for (const width of widths) {
    const toolbar = declarations('/inspirations', '.inspirations-toolbar', width);
    assert.equal(toolbar['flex-direction'], width <= 768 ? 'column' : undefined);
    const search = declarations('/inspirations', '.search-box', width);
    if (width <= 768) { assert.equal(search.width, '100%'); assert.equal(search['max-width'], 'none'); assert.equal(search.flex, '0 0 auto'); }
  }
});

test('narrow-screen fixes for pricing header, AI pricing notice, CRM tabs and long-text table columns', () => {
  const rulesFor = (file, selector) => { const found = []; compile(file).walkRules(rule => { if (rule.selectors.some(item => item.endsWith(selector))) found.push(rule); }); return found; };
  const decl = (rule, prop) => { let value; rule.walkDecls(prop, item => { value = item.value; }); return value; };
  // F02: the header stacks inside a phone media query and the status badge owns its own row.
  const toolbar = rulesFor('src/pages/pricingConfig.scss', '.pricing-toolbar').filter(rule => rule.parent.type === 'atrule');
  assert.ok(toolbar.some(rule => decl(rule, 'flex-direction') === 'column'));
  assert.ok(rulesFor('src/pages/pricingConfig.scss', '.pricing-toolbar-actions').some(rule => decl(rule, 'flex-wrap') === 'wrap'));
  assert.ok(rulesFor('src/pages/pricingConfig.scss', '.source-badge').some(rule => decl(rule, 'flex') === '1 0 100%'));
  // F03: prose with inline <b> must not be a flex row, otherwise every fragment becomes a column.
  const notices = rulesFor('src/pages/aiPricing.scss', '.aip-notice').filter(rule => rule.selectors.some(item => item.endsWith('.aip-notice')));
  assert.ok(notices.length > 0); assert.ok(notices.every(rule => decl(rule, 'display') !== 'flex'));
  assert.equal(decl(notices[0], 'display'), 'block');
  // F05: the CRM tab strip scrolls its active tab into view on load and on every switch.
  const source = read('src/pages/customerManagement.tsx');
  assert.match(source, /ref=\{tabsRef\}/); assert.match(source, /querySelector<HTMLElement>\('\.active'\)/); assert.match(source, /track\.scrollLeft\+=/);
  assert.ok(source.indexOf('tabsRef=useRef') < source.indexOf('if(session.crmLoading)return'), 'hook must run before the loading early return');
  // F06: long-text cells keep a readable minimum width in every affected table.
  const priority = compile('src/responsive-priority.scss');
  const minWidth = (route, leaf) => { let value; priority.walkRules(rule => { if (rule.selectors.includes(`.layout-content[data-admin-page='${route}'] ${leaf}`) || rule.selectors.includes(`.layout-content[data-admin-page="${route}"] ${leaf}`)) rule.walkDecls('min-width', item => { value = item.value; }); }); return value; };
  for (const [route, table, cells] of [['/ai-tasks', '.task-table', ['.text-cell', '.user-cell']], ['/recharge', '.order-center-table', ['.order-title-cell', '.order-user-cell']], ['/membership-operations', '.membership-table', ['.order-cell', '.user-cell', '.time-cell', '.plan-cell']], ['/support-tickets', '.support-table', ['.identity-cell']]]) {
    for (const cell of cells) assert.ok(parseInt(minWidth(route, `${table} ${cell}`), 10) >= 120, `${route} ${cell}`);
  }
  assert.ok(parseInt(minWidth('/certification', '.cert-subject-cell'), 10) >= 160);
  assert.match(read('src/pages/certification.tsx'), /<td className="cert-subject-cell">/);
});

test('list cells clamp long free text with the full value in title, CRM tabs show an edge cue, share dialog header stays sticky', () => {
  for (const file of ['orderCenter', 'aiTaskCenter', 'supportTickets']) assert.match(read(`src/pages/${file}.tsx`), /className="cell-clamp" title=\{/, `${file} clamps long text and exposes the full value`);
  const clamp = declarations('x', 'x'); assert.deepEqual(clamp, {});
  let lines;
  compile('src/responsive-priority.scss').walkRules(rule => { if (rule.selectors.includes('.layout-content .cell-clamp')) rule.walkDecls('-webkit-line-clamp', decl => { lines = decl.value; }); });
  assert.equal(lines, '3');
  const crm = read('src/pages/customerManagement.tsx');
  assert.match(crm, /dataset\.fade=/); assert.match(crm, /addEventListener\('scroll',fade/); assert.match(crm, /removeEventListener\('scroll',fade\)/);
  assert.match(read('src/pages/customerResponsive.scss'), /\.crm-tabs\[data-fade='right'\][^}]*mask-image/);
  // The share dialog's pinned header now lives in main's rewritten shareTracking.scss (PR #4); the browser audit covers it.
  assert.match(read('src/pages/shareTracking.scss'), /\.st-dialog > header \{ position: sticky;/);
});
