// 订单中心不能把内部代码（consume、ai_draw_*、wx_ 默认用户名）直接展示给运营。
// 渲染生产组件（手机卡片 + 桌面表格 + 详情抽屉），无浏览器、无网络。
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import vm from 'node:vm';
import test from 'node:test';
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
import { requireDisplayUtil } from './helpers/display-utils.mjs';

const require = createRequire(import.meta.url);
const React = require('react');
const ts = require('typescript');
const root = fileURLToPath(new URL('..', import.meta.url));
const source = fs.readFileSync(path.join(root, 'src/pages/orderCenter.tsx'), 'utf8');
const javascript = ts.transpileModule(source, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, jsx: ts.JsxEmit.React, target: ts.ScriptTarget.ES2020, esModuleInterop: true },
}).outputText;
const flush = () => new Promise(resolve => setImmediate(resolve));

const orders = [
  { id: 1, user_id: 11, username: 'wx_cae6b664', nickname: '张工', order_no: 'order-1', type: 'consume', order_category: 'consume', status: 'success', title: 'AI生成-ai_draw_single-3张', description: '', pay_channel: 'wechatpay', amount: -30, amount_fen: 0 },
  { id: 2, user_id: 22, username: 'wx_0f1e2d3c', nickname: '', order_no: 'order-2', type: 'consume', order_category: 'ai', status: 'success', title: 'AI生成-ai_draw_multi-4张', description: '', pay_channel: 'wechatpay', amount: -40, amount_fen: 0 },
  { id: 3, user_id: 33, username: 'wx_9a8b7c6d', order_no: 'order-3', type: 'consume', order_category: 'consume', status: 'success', title: 'AI生成-unknown_scene-1张', description: '', pay_channel: 'wechatpay', amount: -10, amount_fen: 0 },
];
const detail = id => {
  const order = orders.find(item => item.id === id);
  return { order, user: { id: order.user_id, username: order.username, nickname: order.nickname || '', user_type: 'miniprogram', can_withdraw: false } };
};

function textOf(node) {
  if (node == null || typeof node === 'boolean') return '';
  if (Array.isArray(node)) return node.map(textOf).join('');
  return typeof node === 'object' ? textOf(node.props?.children) : String(node);
}
function byClass(tree, name) {
  const found = [];
  const walk = node => {
    if (Array.isArray(node)) return node.forEach(walk);
    if (!node || typeof node !== 'object') return;
    if (String(node.props?.className || '').split(/\s+/).includes(name)) found.push(node);
    walk(node.props?.children);
  };
  walk(tree);
  return found;
}

async function render({ mobile, orderId }) {
  const slots = [];
  let cursor = 0;
  let dirty = false;
  let pending = [];
  const searchParams = new URLSearchParams(orderId ? `orderId=${orderId}` : '');
  const react = {
    ...React,
    useState(initial) {
      const index = cursor++;
      if (!(index in slots)) slots[index] = typeof initial === 'function' ? initial() : initial;
      return [slots[index], value => {
        const next = typeof value === 'function' ? value(slots[index]) : value;
        if (!Object.is(next, slots[index])) { slots[index] = next; dirty = true; }
      }];
    },
    useRef(initial) {
      const index = cursor++;
      if (!(index in slots)) slots[index] = { current: initial };
      return slots[index];
    },
    useEffect(effect, deps) {
      const index = cursor++;
      const previous = slots[index];
      if (!previous || deps.some((value, i) => !Object.is(value, previous.deps[i]))) {
        slots[index] = { deps };
        pending.push(effect);
      }
    },
    useCallback(fn) { return fn; },
  };
  react.default = react;
  const api = {
    getOrderList: async () => ({ list: orders, summary: { total_count: orders.length }, total: orders.length, page: 1 }),
    getOrderFundStats: async () => ({}),
    getOrderDetail: async id => detail(Number(id)),
  };
  const module = { exports: {} };
  vm.runInContext(javascript, vm.createContext({
    module, exports: module.exports, URLSearchParams,
    console: { ...console, error() {} },
    window: {},
    require(name) {
      if (name === 'react') return react;
      if (name === 'react-router-dom') return { useNavigate: () => () => {}, useSearchParams: () => [searchParams, () => {}] };
      if (name === 'react-icons/fi') return new Proxy({}, { get: (_, key) => key });
      if (name === '../api/orders') return api;
      if (name === '../api/virtualPay') return { getXPayLogs: async () => ({ list: [] }) };
      if (name === '../component/useIsMobile') return { useIsMobile: () => mobile };
      if (name === '../utils/pageLoadError') return { reportPageLoadError() {} };
      if (name === '../config/api') return { API_BASE_URL: '/fixture-only' };
      if (name.endsWith('.scss')) return {};
      const util = requireDisplayUtil(name);
      if (util) return util;
      throw new Error(`Unexpected import: ${name}`);
    },
  }));
  let tree;
  const commit = () => {
    do {
      dirty = false;
      cursor = 0;
      tree = module.exports.default();
      const effects = pending; pending = [];
      effects.forEach(effect => effect());
    } while (dirty);
  };
  commit();
  await flush(); commit();
  await flush(); commit();
  return tree;
}

const internalCodes = /consume|ai_draw_|wx_/;

test('手机订单卡片只显示中文类型、场景中文名和昵称', async () => {
  const tree = await render({ mobile: true });
  const cards = byClass(tree, 'order-card');
  assert.equal(cards.length, 3);
  const [first, second, third] = cards.map(textOf);
  assert.doesNotMatch(first, internalCodes);
  assert.match(first, /AI消费/);
  assert.doesNotMatch(first, /AI消费 · AI消费/, '分类与类型相同时只显示一次');
  assert.match(first, /AI生成-AI 绘画-单图-3张/);
  assert.match(first, /张工/);
  assert.match(second, /AI消费 · AI生成/);
  assert.match(second, /AI生成-AI 绘画-多图-4张/);
  // 没有昵称时显示「微信用户 #ID」，不外露 wx_ 登录名；未知场景保持原样
  assert.match(second, /微信用户 #22/);
  assert.match(third, /AI生成-unknown_scene-1张/);
});

test('桌面表格同样替换分类、场景代码和用户名', async () => {
  const tree = await render({ mobile: false });
  const text = textOf(byClass(tree, 'order-center-table')[0]);
  assert.match(text, /AI生成-AI 绘画-单图-3张/);
  assert.match(text, /张工/);
  assert.doesNotMatch(text, /ai_draw_single|wx_cae6b664/);
  assert.doesNotMatch(text, />consume</);
});

test('订单详情的标题、分类和用户也走同一套中文显示', async () => {
  const tree = await render({ mobile: true, orderId: 1 });
  const drawer = byClass(tree, 'order-drawer')[0];
  assert.ok(drawer, '详情抽屉已打开');
  const text = textOf(drawer);
  assert.match(text, /AI生成-AI 绘画-单图-3张/);
  assert.match(text, /张工/);
  assert.doesNotMatch(text, internalCodes);
});
