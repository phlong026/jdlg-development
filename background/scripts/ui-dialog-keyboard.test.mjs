// Execute the production DOM controller against controlled elements/events.
// This checks focus/keyboard policy, not browser layout, rendering, or backend actions.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import test from 'node:test';
import { createRequire } from 'node:module';
const require = createRequire(import.meta.url);
const React = require('react');
import ts from 'typescript';
import { environment, deferred } from './helpers/crm-harness.mjs';

const source = fs.readFileSync(new URL('../src/component/adminDialogKeyboard.ts', import.meta.url), 'utf8');
const compiled = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 } }).outputText;

function mount(path = '/user-workbench') {
  const observers = [];
  const listeners = new Map();
  const windowListeners = [];
  const windowCaptureListeners = [];
  const doc = {
    activeElement: null,
    defaultView: { getComputedStyle: element => ({ zIndex: element.zIndex || 'auto', visibility: element.visibility || 'visible' }) },
    addEventListener(name, fn) { if (!listeners.has(name)) listeners.set(name, new Set()); listeners.get(name).add(fn); },
    removeEventListener(name, fn) { listeners.get(name)?.delete(fn); },
    emit(name, event = {}) {
      if (name === 'keydown') {
        for (const fn of windowCaptureListeners) { fn(event); if (event.immediateStopped) break; }
        if (event.propagationStopped) return;
      }
      for (const fn of listeners.get(name) || []) { fn(event); if (event.immediateStopped) break; }
      if (name === 'keydown' && !event.propagationStopped) for (const fn of windowListeners) fn(event);
    },
    querySelector(selector) { return this.body.querySelector(selector); },
  };
  class Element {
    constructor(tag = 'div', className = '', text = '') {
      this.tagName = tag.toUpperCase(); this.className = className; this.textContent = text;
      this.ownerDocument = doc; this.parentElement = null; this.children = []; this.attributes = new Map();
      this.dataset = {}; this.focusCalls = []; this.clicks = 0; this.disabled = false;
    }
    get isConnected() { return this === doc.body || !!this.parentElement?.isConnected; }
    get tabIndex() { return this.hasAttribute('tabindex') ? Number(this.getAttribute('tabindex')) : /^(BUTTON|INPUT|TEXTAREA|SELECT|A)$/.test(this.tagName) ? 0 : -1; }
    append(...children) { for (const child of children) { child.parentElement = this; this.children.push(child); } return children[children.length - 1]; }
    remove() {
      if (this.contains(doc.activeElement)) doc.activeElement = doc.body;
      this.parentElement.children = this.parentElement.children.filter(child => child !== this); this.parentElement = null;
    }
    contains(element) { return element === this || this.children.some(child => child.contains(element)); }
    hasAttribute(name) { return this.attributes.has(name); }
    getAttribute(name) { return this.attributes.get(name) ?? null; }
    setAttribute(name, value) { this.attributes.set(name, String(value)); }
    removeAttribute(name) { this.attributes.delete(name); }
    matches(selector) {
      return selector.split(',').some(part => {
        let value = part.trim();
        if (value === ':disabled') return this.disabled;
        const excluded = [...value.matchAll(/:not\(([^)]+)\)/g)].map(match => match[1]);
        value = value.replace(/:not\([^)]+\)/g, '');
        if (excluded.some(item => this.matches(item))) return false;
        const attributes = [...value.matchAll(/\[([\w-]+)(?:="([^"]*)")?\]/g)];
        if (attributes.some(([, name, expected]) => expected === undefined ? !this.hasAttribute(name) : this.getAttribute(name) !== expected)) return false;
        value = value.replace(/\[[^\]]+\]/g, '');
        const classes = [...value.matchAll(/\.([\w-]+)/g)].map(match => match[1]);
        if (classes.some(name => !this.className.split(' ').includes(name))) return false;
        const tag = value.replace(/\.[\w-]+/g, '').trim();
        return !tag || tag.toUpperCase() === this.tagName;
      });
    }
    closest(selector) { return this.matches(selector) ? this : this.parentElement?.closest(selector) || null; }
    querySelectorAll(selector) {
      return this.children.flatMap(child => [...(child.matches(selector) ? [child] : []), ...child.querySelectorAll(selector)]);
    }
    querySelector(selector) { return this.querySelectorAll(selector)[0] || null; }
    getClientRects() { return this.isConnected && !this.closest('[hidden]') ? [{}] : []; }
    focus(options) { this.focusCalls.push(options); if (this.disabled || !this.isConnected) return; doc.activeElement = this; doc.emit('focusin', { target: this }); }
    click() { if (!this.disabled) { this.clicks += 1; this.onClick?.(); } }
  }
  doc.body = new Element('body'); doc.activeElement = doc.body;
  const container = doc.body.append(new Element('div', 'layout-container'));
  const root = container.append(new Element('div', 'layout-content')); root.dataset.adminPage = path;
  const opener = root.append(new Element('button', '', '打开详情')); opener.focus();
  class MutationObserver {
    constructor(fn) { this.fn = fn; this.active = true; observers.push(this); }
    observe(target, options) { this.target = target; this.options = options; }
    disconnect() { this.active = false; }
  }
  const module = { exports: {} };
  vm.runInNewContext(compiled, { exports: module.exports, module, MutationObserver });
  const cleanup = module.exports.installAdminDialogKeyboard(root);
  const flush = () => observers.filter(observer => observer.active).forEach(observer => observer.fn());
  const key = (name, options = {}) => {
    const event = { key: name, shiftKey: false, defaultPrevented: false, isComposing: false,
      preventDefault() { this.defaultPrevented = true; },
      stopPropagation() { this.propagationStopped = true; },
      stopImmediatePropagation() { this.immediateStopped = this.propagationStopped = true; }, ...options };
    doc.emit('keydown', event); return event;
  };
  const dialog = (kind = 'modal-content', { z = 1400, title = '认证审核', close = true, input = false } = {}) => {
    const overlay = root.append(new Element('div', 'modal-overlay')); overlay.zIndex = String(z);
    const panel = overlay.append(new Element('section', kind));
    panel.append(new Element('h3', '', title));
    const className = ({ 'modal-content': 'modal-close', 'cert-drawer': 'cert-drawer-close', 'order-drawer': 'drawer-close', 'aip-modal': 'aip-modal-close', 'aca-modal': 'aca-modal-close', 'aca-preview': 'aca-btn', 'customer-lead-modal': 'customer-lead-modal-close', 'customer-lead-action-modal': 'customer-lead-modal-close' })[kind];
    const closeButton = close ? panel.append(new Element('button', className, '关闭')) : null;
    if (closeButton) closeButton.onClick = () => overlay.remove();
    const field = input ? panel.append(new Element('textarea')) : null;
    const confirm = panel.append(new Element('button', 'confirm', '确认操作'));
    return { panel, overlay, close: closeButton, field, confirm };
  };
  return { doc, root, container, opener, Element, flush, key, dialog, cleanup, observers, listeners, windowListeners, windowCaptureListeners, api: module.exports };
}

for (const kind of ['modal-content', 'cert-drawer', 'order-drawer', 'aip-modal', 'aca-modal', 'aca-preview']) {
  test(`${kind}: gets a named dialog, neutral focus, and Escape through the original close`, () => {
    const page = mount(); const dialog = page.dialog(kind); page.flush();
    assert.equal(dialog.panel.getAttribute('role'), 'dialog');
    assert.equal(dialog.panel.getAttribute('aria-label'), '认证审核');
    assert.equal(dialog.panel.getAttribute('aria-modal'), 'true');
    assert.equal(page.doc.activeElement, dialog.close);
    assert.equal(dialog.confirm.focusCalls.length, 0);
    const event = page.key('Escape'); page.flush();
    assert.equal(event.defaultPrevented, true); assert.equal(dialog.close.clicks, 1);
    assert.equal(page.doc.activeElement, page.opener);
    assert.ok(page.opener.focusCalls.at(-1).preventScroll);
  });
}

test('two equal-z certification dialogs close only the last layer and restore its trigger', () => {
  const page = mount(); const detail = page.dialog(); page.flush(); detail.confirm.focus();
  const review = page.dialog(); page.flush();
  assert.equal(detail.panel.getAttribute('aria-modal'), 'false');
  page.key('Escape'); page.flush();
  assert.equal(review.close.clicks, 1); assert.equal(detail.close.clicks, 0);
  assert.equal(page.doc.activeElement, detail.confirm);
  assert.equal(detail.panel.getAttribute('aria-modal'), 'true');
  page.key('Escape'); page.flush(); assert.equal(page.doc.activeElement, page.opener);
});

test('a refund modal wins over a later-in-DOM order drawer by z-index', () => {
  const page = mount('/recharge'); const refund = page.dialog(); const drawer = page.dialog('order-drawer', { z: 1301 }); page.flush();
  assert.equal(page.doc.activeElement, refund.close);
  page.key('Escape'); page.flush(); assert.equal(refund.close.clicks, 1); assert.equal(drawer.close.clicks, 0);
});

test('disabled or aria-disabled close consumes Escape without closing a lower layer', () => {
  for (const mode of ['disabled', 'aria-disabled']) {
    const page = mount('/recharge'); const drawer = page.dialog('order-drawer', { z: 1301 }); page.flush(); drawer.confirm.focus();
    const refund = page.dialog(); page.flush();
    if (mode === 'disabled') refund.close.disabled = true; else refund.close.setAttribute('aria-disabled', 'true');
    let lowerHandler = 0; page.windowListeners.push(() => { lowerHandler += 1; });
    const event = page.key('Escape'); page.flush();
    assert.equal(event.propagationStopped, true); assert.equal(refund.close.clicks, 0); assert.equal(drawer.close.clicks, 0); assert.equal(lowerHandler, 0);
    assert.equal(refund.confirm.focusCalls.length, 0);
  }
});

test('an enabled close with a submission guard retains the original callback semantics', () => {
  const page = mount(); const dialog = page.dialog(); let saving = true;
  dialog.close.onClick = () => { if (!saving) dialog.overlay.remove(); }; page.flush();
  page.key('Escape'); page.flush(); assert.equal(dialog.panel.isConnected, true);
  saving = false; page.key('Escape'); page.flush(); assert.equal(dialog.panel.isConnected, false);
});

test('Tab cycles within the top dialog and skips hidden, disabled, and negative-tabindex controls', () => {
  const page = mount(); const dialog = page.dialog('modal-content', { input: true });
  const disabled = dialog.panel.append(new page.Element('button')); disabled.disabled = true;
  const hidden = dialog.panel.append(new page.Element('button')); hidden.setAttribute('hidden', '');
  const negative = dialog.panel.append(new page.Element('button')); negative.setAttribute('tabindex', '-1');
  page.flush();
  page.key('Tab', { shiftKey: true }); assert.equal(page.doc.activeElement, dialog.confirm);
  page.key('Tab'); assert.equal(page.doc.activeElement, dialog.close);
  dialog.field.focus(); const ordinary = page.key('Tab'); assert.equal(ordinary.defaultPrevented, false);
  page.opener.focus(); assert.equal(page.doc.activeElement, dialog.close, 'background focus is redirected');
});

test('an empty or confirmation-only dialog starts on its container, not the destructive action', () => {
  const page = mount(); const dialog = page.dialog('modal-content', { close: false }); page.flush();
  assert.equal(page.doc.activeElement, dialog.panel); assert.equal(dialog.confirm.focusCalls.length, 0);
  dialog.confirm.disabled = true; page.flush(); page.key('Tab'); assert.equal(page.doc.activeElement, dialog.panel);
  page.key('Escape'); assert.equal(dialog.panel.isConnected, true);
});

test('autofocus is preserved and focus returns to the actual opener', () => {
  const page = mount(); const dialog = page.dialog('aca-modal', { input: true });
  dialog.field.focus(); // React autoFocus happens before the mutation observer.
  page.flush(); assert.equal(page.doc.activeElement, dialog.field);
  page.key('Escape'); page.flush(); assert.equal(page.doc.activeElement, page.opener);
});

test('expanding details inside an existing dialog preserves current focus and return target', () => {
  const page = mount(); const dialog = page.dialog('modal-content', { input: true }); page.flush(); dialog.field.focus();
  const details = dialog.panel.append(new page.Element('details')); details.append(new page.Element('button'));
  page.flush(); assert.equal(page.doc.activeElement, dialog.field);
  page.key('Escape'); page.flush(); assert.equal(page.doc.activeElement, page.opener);
});

test('removing both dialog layers restores the connected original opener', () => {
  const page = mount(); const detail = page.dialog(); page.flush(); detail.confirm.focus(); const review = page.dialog(); page.flush();
  detail.overlay.remove(); review.overlay.remove(); page.flush(); assert.equal(page.doc.activeElement, page.opener);
});

// Mount the real CommandPalette effect/handler around controlled DOM nodes.
function openPalette(page) {
  const overlay = page.container.append(new page.Element('div', 'cmdk-overlay')); overlay.zIndex = '1500';
  const panel = overlay.append(new page.Element('section', 'cmdk-panel'));
  const search = panel.append(new page.Element('input'));
  const first = panel.append(new page.Element('button', 'cmdk-item'));
  const last = panel.append(new page.Element('button', 'cmdk-item'));
  const effects = []; let cleanup, closes = 0;
  const react = { ...React,
    useRef: initial => ({ current: initial === null ? search : initial }),
    useState: initial => [initial, () => {}], useMemo: factory => factory(), useEffect: effect => effects.push(effect),
  };
  react.default = react; react.__esModule = true;
  const module = { exports: {} };
  const code = ts.transpileModule(fs.readFileSync(new URL('../src/component/CommandPalette.tsx', import.meta.url), 'utf8'), {
    compilerOptions: { module: ts.ModuleKind.CommonJS, jsx: ts.JsxEmit.React, target: ts.ScriptTarget.ES2020, esModuleInterop: true },
  }).outputText;
  vm.runInNewContext(code, { module, exports: module.exports, document: page.doc,
    window: {
      addEventListener(name, fn, capture) { assert.equal(name, 'keydown'); (capture ? page.windowCaptureListeners : page.windowListeners).push(fn); },
      removeEventListener(name, fn, capture) { const list = capture ? page.windowCaptureListeners : page.windowListeners; list.splice(list.indexOf(fn), 1); },
    },
    require(name) {
      if (name === 'react') return react;
      if (name === 'react-router-dom') return { useNavigate: () => () => {} };
      if (name === 'react-icons/fi') return { FiSearch: 'icon' };
      return {};
    },
  });
  module.exports.default({ open: true, items: [], onClose: () => { closes += 1; overlay.remove(); cleanup?.(); } });
  cleanup = effects[0](); page.flush();
  return { overlay, panel, search, first, last, get closes() { return closes; }, close: () => { overlay.remove(); cleanup(); page.flush(); } };
}

test('the production palette Escape closes once, leaves the modal open, and restores its focus', () => {
  const page = mount(); const dialog = page.dialog('modal-content', { input: true }); page.flush(); dialog.field.focus();
  const palette = openPalette(page); assert.equal(page.doc.activeElement, palette.search);
  const event = page.key('Escape'); page.flush();
  assert.equal(event.defaultPrevented, true); assert.equal(palette.closes, 1); assert.equal(dialog.close.clicks, 0);
  assert.equal(page.doc.activeElement, dialog.field);
  page.key('Escape'); page.flush(); assert.equal(dialog.close.clicks, 1);
});

test('the production palette traps Tab above a mobile sidebar and stops Escape reaching it', () => {
  const page = mount('/customer-management'); // CRM panels keep their own keyboard behavior.
  const sidebarButton = page.container.append(new page.Element('button', 'sidebar-link')); sidebarButton.focus();
  let sidebarEscapes = 0, sidebarTabs = 0;
  page.windowListeners.push(event => { if (event.key === 'Escape') sidebarEscapes += 1; if (event.key === 'Tab') sidebarTabs += 1; });
  const palette = openPalette(page);
  page.key('Tab', { shiftKey: true }); assert.equal(page.doc.activeElement, palette.last);
  page.key('Tab'); assert.equal(page.doc.activeElement, palette.search);
  palette.last.disabled = true; page.key('Tab', { shiftKey: true }); assert.equal(page.doc.activeElement, palette.first);
  assert.equal(sidebarTabs, 0);
  page.key('Escape'); assert.equal(palette.closes, 1); assert.equal(sidebarEscapes, 0);
  assert.equal(page.doc.activeElement, sidebarButton);
  page.key('Escape'); assert.equal(sidebarEscapes, 1, 'the next Escape still belongs to the sidebar');
});

test('palette cleanup restores focus for backdrop dismissal and skips detached route openers', () => {
  const page = mount(); const palette = openPalette(page); palette.close(); assert.equal(page.doc.activeElement, page.opener);
  const next = openPalette(page); page.opener.remove(); next.close();
  assert.equal(page.windowCaptureListeners.length, 0); assert.notEqual(page.doc.activeElement, page.opener);
});

test('existing labels survive and cleanup removes listeners/observer without old-page focus', () => {
  const page = mount(); const dialog = page.dialog(); dialog.panel.setAttribute('aria-labelledby', 'existing-title');
  dialog.panel.setAttribute('aria-modal', 'true'); dialog.close.setAttribute('aria-label', '关闭退款预览'); page.flush();
  assert.equal(dialog.panel.hasAttribute('aria-label'), false); assert.equal(dialog.close.getAttribute('aria-label'), '关闭退款预览');
  page.cleanup(); const focusCalls = page.opener.focusCalls.length;
  dialog.overlay.remove(); page.flush(); page.key('Escape');
  assert.equal(page.opener.focusCalls.length, focusCalls); assert.equal(dialog.panel.hasAttribute('tabindex'), false);
  assert.equal(dialog.panel.getAttribute('aria-modal'), 'true'); assert.equal(page.observers[0].active, false);
  assert.equal(page.listeners.get('keydown').size, 0);
});

test('scope covers the reviewed 24 routes without taking over unrelated pages', () => {
  const allowed = ['/dashboard', '/users', '/user-workbench', '/designer-center', '/certification', '/membership-operations', '/recharge', '/virtualpay', '/pricing-config', '/recharge-config', '/membership-plans', '/ai-overview', '/ai-tasks', '/ai-content-audits', '/ai-tools', '/ai-pricing', '/ai-config', '/templates', '/inspirations', '/support-tickets', '/utility-tools', '/oss', '/customer-leads', '/customer-management'];
  const page = mount(); assert.ok(allowed.every(page.api.isManagedDialogPage));
  for (const path of ['/login', '/materials', '/unknown']) {
    const other = mount(path); const dialog = other.dialog(); other.flush(); other.key('Escape');
    assert.equal(other.observers.length, 0); assert.equal(dialog.close.clicks, 0); assert.equal(dialog.panel.hasAttribute('role'), false);
  }
});


for (const route of ['/templates', '/inspirations', '/support-tickets', '/utility-tools', '/oss']) {
  test(`${route}: generic modal gets focus trapping and guarded Escape`, () => {
    const page=mount(route), dialog=page.dialog('modal-content',{input:true});page.flush();
    assert.equal(page.doc.activeElement,dialog.close);page.key('Tab',{shiftKey:true});assert.equal(page.doc.activeElement,dialog.confirm);
    dialog.close.disabled=true;page.key('Escape');assert.equal(dialog.close.clicks,0);
    dialog.close.disabled=false;page.key('Escape');page.flush();assert.equal(dialog.close.clicks,1);assert.equal(page.doc.activeElement,page.opener);
  });
}

for (const route of ['/customer-leads','/customer-management']) {
  test(`${route}: only the top legacy lead layer closes, returns focus, and yields to the palette`, () => {
    const page=mount(route),detail=page.dialog('customer-lead-modal');page.flush();detail.confirm.focus();
    const action=page.dialog('customer-lead-action-modal',{input:true});page.flush();
    const palette=openPalette(page);page.key('Escape');page.flush();assert.equal(palette.closes,1);assert.equal(action.close.clicks,0);
    page.key('Tab',{shiftKey:true});assert.equal(page.doc.activeElement,action.confirm);page.key('Tab');assert.equal(page.doc.activeElement,action.close);
    page.key('Escape');page.flush();assert.equal(action.close.clicks,1);assert.equal(detail.close.clicks,0);assert.equal(page.doc.activeElement,detail.confirm);
    page.key('Escape');page.flush();assert.equal(detail.close.clicks,1);assert.equal(page.doc.activeElement,page.opener);
  });
}

test('CRM self-managed drawer is excluded and keeps focus/keys above a legacy lead dialog',()=>{
  const page=mount('/customer-management'),legacy=page.dialog('customer-lead-modal',{z:1000});page.flush();
  const drawer=page.dialog('crm-drawer',{z:1400,input:true});drawer.field.focus();page.flush();
  assert.equal(drawer.panel.hasAttribute('role'),false);assert.equal(drawer.panel.hasAttribute('tabindex'),false);
  assert.equal(page.doc.activeElement,drawer.field);assert.equal(legacy.panel.getAttribute('aria-modal'),'false');
  const escape=page.key('Escape');assert.equal(escape.defaultPrevented,false);assert.equal(legacy.close.clicks,0);
  drawer.overlay.remove();page.flush();page.key('Escape');assert.equal(legacy.close.clicks,1);
});

test('shared keyboard keeps the real pending legacy editor open and cannot reopen a duplicate write',async()=>{
  const env=environment(),pending=deferred();let writes=0;env.leadApi.updateCustomerLeadStatus=()=>{writes++;return pending.promise};
  const leads=await env.mount('customerLeads.tsx');await leads.button('详情').props.onClick();await leads.flush();
  const page=mount('/customer-leads'),detail=page.dialog('customer-lead-modal');page.flush();
  leads.button('记录跟进').props.onClick();leads.render();const action=page.dialog('customer-lead-action-modal');page.flush();
  const actionNode=leads.find(n=>n.props?.className==='customer-lead-action-modal');
  const closeNode=actionNode.props.children[0].props.children[1];
  action.close.onClick=()=>{closeNode.props.onClick();leads.render();if(!leads.all(n=>n.props?.className==='customer-lead-action-modal').length)action.overlay.remove()};
  const area=leads.find(n=>n.type==='textarea');area.props.onChange({target:{value:'Keep draft'}});leads.render();leads.button('保存跟进').props.onClick();
  page.key('Escape');page.flush();assert.equal(action.overlay.isConnected,true);assert.equal(detail.close.clicks,0);
  leads.button('记录跟进').props.onClick();leads.render();leads.find(n=>n.type==='textarea').props.onChange({target:{value:'Do not submit twice'}});leads.render();leads.button('保存跟进').props.onClick();assert.equal(writes,1);
  pending.reject(new Error('fixture save failed'));await leads.flush();page.key('Escape');page.flush();assert.equal(action.overlay.isConnected,false);assert.equal(detail.close.clicks,0);
});
