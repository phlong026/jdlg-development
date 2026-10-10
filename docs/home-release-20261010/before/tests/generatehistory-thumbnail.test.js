const fs = require('fs');
const path = require('path');
const assert = require('assert');

const root = path.resolve(__dirname, '..');

function read(relativePath) {
  return fs.readFileSync(path.join(root, relativePath), 'utf8');
}

const historyTs = read('miniprogram/pages/generatehistory/generatehistory.ts');
const historyJs = read('miniprogram/pages/generatehistory/generatehistory.js');
const historyWxml = read('miniprogram/pages/generatehistory/generatehistory.wxml');
const historyScss = read('miniprogram/pages/generatehistory/generatehistory.scss');

for (const source of [historyTs, historyJs]) {
  assert.match(source, /displayThumbnailUrl/, 'history records should expose a list thumbnail URL');
  assert.match(source, /thumbnail_url/, 'history records should prefer explicit thumbnail URLs when present');
}

assert.match(
  historyWxml,
  /src="\{\{item\.displayThumbnailUrl\}\}"[\s\S]*mode="aspectFit"/,
  'history list should render the thumbnail with aspectFit',
);

assert.match(
  historyScss,
  /\.item-thumbnail/,
  'history list should use a dedicated thumbnail media container',
);

console.log('generate history thumbnail checks passed');
