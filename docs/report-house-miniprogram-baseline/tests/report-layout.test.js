const fs = require('node:fs');
const path = require('node:path');
const assert = require('node:assert/strict');
const { test } = require('node:test');
const sass = require('../../AItestPlatform/node_modules/sass');

test('report actions fill the phone column instead of inheriting WeChat fixed button width', () => {
  const file = path.join(__dirname, '../miniprogram/pages/report/shared.scss');
  const css = sass.compileString(fs.readFileSync(file, 'utf8')).css;
  const actions = css.match(/button\.primary, button\.secondary\s*\{([^}]+)\}/)[1];
  assert.match(actions, /width:\s*100%/, 'long purchase and continue labels need the full reading column');
  assert.match(actions, /box-sizing:\s*border-box/, 'padding must stay inside the phone column');
  const choice = css.match(/button\.choice\s*\{([^}]+)\}/)[1];
  assert.match(choice, /width:\s*auto/, 'short options must not inherit the fixed default width');
});
