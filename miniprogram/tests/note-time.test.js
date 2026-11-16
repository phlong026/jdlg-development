'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');
const { execFileSync } = require('node:child_process');
const { formatNoteTime } = require('../miniprogram/utils/note-time');

const modulePath = path.resolve(__dirname, '../miniprogram/utils/note-time.js');

function formatInTimezone(value, timezone) {
  const script = `const { formatNoteTime } = require(${JSON.stringify(modulePath)}); process.stdout.write(formatNoteTime(${JSON.stringify(value)}));`;
  return execFileSync(process.execPath, ['-e', script], {
    encoding: 'utf8',
    env: { ...process.env, TZ: timezone },
  });
}

test('formats the screenshot timestamp in the device local timezone', () => {
  assert.equal(formatInTimezone('2026-11-09T05:25:19.153277048Z', 'Asia/Shanghai'), '2026-11-09 13:25');
});

test('accepts long fractional seconds and explicit RFC3339 offsets', () => {
  assert.equal(formatInTimezone('2026-11-09T13:25:19.153277048+08:00', 'UTC'), '2026-11-09 05:25');
  assert.equal(formatInTimezone('2026-11-09T05:25:19.1Z', 'UTC'), '2026-11-09 05:25');
});

test('uses the current device timezone instead of a fixed server timezone', () => {
  assert.equal(formatInTimezone('2026-11-09T05:25:19Z', 'America/New_York'), '2026-11-09 00:25');
});

test('keeps a valid date-only value unchanged in every device timezone', () => {
  assert.equal(formatInTimezone('2026-11-09', 'Asia/Shanghai'), '2026-11-09');
  assert.equal(formatInTimezone('2026-11-09', 'America/New_York'), '2026-11-09');
});

test('rejects an impossible date-only calendar value', () => {
  assert.equal(formatInTimezone('2026-02-30', 'Asia/Shanghai'), '');
});

test('returns an empty string for empty or invalid values', () => {
  for (const value of ['', '   ', null, undefined, 'not-a-date', 0, {}]) {
    assert.equal(formatNoteTime(value), '');
  }
});
