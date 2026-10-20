import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

// 新接入的客户（企微、小程序、分享）在没人判断过之前是"待判断"，不能默认显示成"中意向"。
test('客户意向映射包含待判断', () => {
  const source = readFileSync(new URL('../src/api/wecomCustomers.ts', import.meta.url), 'utf8');
  const line = source.split('\n').find((l) => l.startsWith('export const intents'));
  assert.ok(line, '找到 intents 定义');
  assert.match(line, /unknown:\s*'待判断'/);
  for (const level of ["high:'高'", "medium:'中'", "low:'低'"]) assert.ok(line.includes(level), level);
});
