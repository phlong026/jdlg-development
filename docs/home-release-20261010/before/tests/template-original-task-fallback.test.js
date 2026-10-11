const fs = require('fs');
const path = require('path');
const assert = require('assert');

const root = path.resolve(__dirname, '..');

function read(relativePath) {
  return fs.readFileSync(path.join(root, relativePath), 'utf8');
}

const detailTs = read('miniprogram/pages/templatesquaredetails/templatesquaredetails.ts');
const generateTs = read('miniprogram/pages/aigenerate/aigenerate.ts');
const originalTaskLoader = generateTs.match(/async loadTemplateOriginalTask[\s\S]*?\n  async loadTaskInfo/)?.[0] || '';

assert.match(
  detailTs,
  /typeof res\.has_original_task === 'boolean' \? res\.has_original_task : false/,
  'template detail should not assume original-task exists when the backend field is absent',
);

assert.match(
  originalTaskLoader,
  /requestRes\.statusCode === 404[\s\S]*resolve\(\{\}\)/,
  'aigenerate should silently fall back when original-task is missing',
);

assert.doesNotMatch(
  originalTaskLoader,
  /wx\.showToast\(\{ title: error\.message \|\|/,
  'original-task load failure should not show a blocking toast',
);

console.log('template original-task fallback checks passed');
