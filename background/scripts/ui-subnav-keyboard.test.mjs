// Test production strip geometry helpers and component handlers with controlled
// hooks/measurements. No real browser layout, new dependencies, or navigation.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import test from 'node:test';
import { createRequire } from 'node:module';
import ts from 'typescript';
const require = createRequire(import.meta.url);
const React = require('react');
const source = fs.readFileSync(new URL('../src/component/SubNav.tsx', import.meta.url), 'utf8');
const compiled = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020, jsx: ts.JsxEmit.React, esModuleInterop: true } }).outputText;

function mount() {
  let scrollLeft = 0;
  const track = {
    clientWidth: 320, scrollWidth: 980, scrollTop: 277, children: [], scrolls: [],
    get scrollLeft() { return scrollLeft; },
    set scrollLeft(value) { scrollLeft = Math.max(0, Math.min(value, this.scrollWidth - this.clientWidth)); },
    getBoundingClientRect() { return { left: 100, right: 100 + this.clientWidth, width: this.clientWidth }; },
    querySelector() { return this.children.find(child => child.active); },
    scrollBy(options) { this.scrolls.push(options); this.scrollLeft += options.left; },
  };
  const link = (offset, width, active = false) => ({
    active,
    getBoundingClientRect() { return { left: 100 + offset - track.scrollLeft, right: 100 + offset + width - track.scrollLeft, width }; },
    closest(selector) { assert.equal(selector, 'a'); return this; },
  });
  track.children = [link(0, 120), link(390, 130), link(700, 140, true)];
  let edges = { left: false, right: false }, effect, cleanup, resize, reducedMotion = false;
  const frames = new Map(); let sequence = 0;
  const react = {
    ...React, useRef: () => ({ current: track }), useId: () => 'subnav-test',
    useState: () => [edges, next => { edges = typeof next === 'function' ? next(edges) : next; }],
    useEffect: fn => { effect = fn; },
  };
  react.default = react; react.__esModule = true;
  const module = { exports: {} };
  vm.runInNewContext(compiled, {
    module, exports: module.exports,
    require(name) {
      if (name === 'react') return react;
      if (name === 'react-router-dom') return { Link: 'link' };
      if (name === 'react-icons/fi') return { FiArrowLeft: 'icon', FiChevronRight: 'icon', FiSettings: 'icon' };
      if (name === './leadOverviewStore') return { useLeadOverviewCounts: () => ({}) };
      return {};
    },
    ResizeObserver: class {
      constructor(callback) { this.callback = callback; this.observed = []; resize = this; }
      observe(element) { this.observed.push(element); }
      disconnect() { this.disconnected = true; }
    },
    requestAnimationFrame(fn) { frames.set(++sequence, fn); return sequence; },
    cancelAnimationFrame(id) { frames.delete(id); },
    window: { matchMedia: () => ({ matches: reducedMotion }) },
  });
  const pages = ['/ai-overview', '/ai-tasks', '/ai-content-audits'].map((path, i) => ({ path, label: `页面${i}` }));
  const render = () => module.exports.default({ group: { label: 'AI', pages }, location: { page: pages[2], inSecondary: false } });
  const start = () => { render(); cleanup = effect(); return render(); };
  const fireResize = () => { resize.callback(); for (const fn of frames.values()) fn(); frames.clear(); };
  return { api: module.exports, track, link, render, start, fireResize, stop: () => cleanup(), get resize() { return resize; }, setReducedMotion(value) { reducedMotion = value; } };
}
const nodes = (tree, predicate) => {
  if (!tree || typeof tree !== 'object') return [];
  if (Array.isArray(tree)) return tree.flatMap(child => nodes(child, predicate));
  return [...(predicate(tree) ? [tree] : []), ...nodes(tree.props?.children, predicate)];
};

test('active and focused links scroll inside their own strip without moving vertical/page scroll', () => {
  const page = mount(); const { track, api } = page;
  api.revealSubNavLink(track, track.children[2]);
  assert.equal(track.scrollLeft, 564); assert.equal(track.scrollTop, 277);
  const visible = track.children[2].getBoundingClientRect();
  assert.ok(visible.left >= 144 && visible.right <= 376);
  const previous = track.scrollLeft; api.revealSubNavLink(track, track.children[2]); assert.equal(track.scrollLeft, previous);
  api.revealSubNavLink(track, track.children[0]); assert.equal(track.scrollLeft, 0); assert.equal(track.scrollTop, 277);
});

test('oversized links align their beginning and non-overflowing strips do not move', () => {
  const page = mount(); const oversized = page.link(300, 400); page.api.revealSubNavLink(page.track, oversized);
  assert.equal(page.track.scrollLeft, 256);
  page.track.clientWidth = 1000; page.track.scrollWidth = 1000; page.track.scrollLeft = 0;
  page.api.revealSubNavLink(page.track, page.track.children[2]); assert.equal(page.track.scrollLeft, 0);
});

test('overflow controls reflect left, middle, and right edges', () => {
  const page = mount();
  assert.deepEqual({ ...page.api.subNavScrollEdges(page.track) }, { left: false, right: true });
  page.track.scrollLeft = 200; assert.deepEqual({ ...page.api.subNavScrollEdges(page.track) }, { left: true, right: true });
  page.track.scrollLeft = 1000; assert.deepEqual({ ...page.api.subNavScrollEdges(page.track) }, { left: true, right: false });
});

test('component reveals active route, exposes labeled controls, and supports keyboard focus', () => {
  const page = mount(); const tree = page.start(); assert.equal(page.track.scrollLeft, 564);
  const active = nodes(tree, node => node.type === 'link' && node.props['aria-current'] === 'page'); assert.equal(active.length, 1);
  const buttons = nodes(tree, node => node.type === 'button'); assert.equal(buttons.length, 2);
  assert.ok(buttons.every(button => button.props['aria-label'].includes('滚动') && button.props['aria-controls'] === 'subnav-test'));
  const strip = nodes(tree, node => node.props?.className === 'subnav-scroll-track')[0];
  strip.props.onFocusCapture({ target: page.track.children[0] }); assert.equal(page.track.scrollLeft, 0); assert.equal(page.track.scrollTop, 277);
});

test('scroll buttons respect reduced motion and resize updates overflow cues', () => {
  const page = mount(); let tree = page.start();
  const right = nodes(tree, node => node.type === 'button' && node.props.className.includes('right'))[0];
  right.props.onClick(); assert.equal(page.track.scrolls.at(-1).behavior, 'smooth');
  page.setReducedMotion(true); right.props.onClick(); assert.equal(page.track.scrolls.at(-1).behavior, 'auto');
  assert.equal(page.resize.observed.length, 4, 'track and link sizes are observed');
  page.track.clientWidth = 1100; page.track.scrollWidth = 1100; page.track.scrollLeft = 0; page.fireResize(); tree = page.render();
  assert.equal(nodes(tree, node => node.type === 'button').length, 0);
  page.stop(); assert.equal(page.resize.disconnected, true);
});
