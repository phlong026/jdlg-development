const fs = require('node:fs');
const path = require('node:path');
const assert = require('node:assert/strict');

const root = path.resolve(__dirname, '..', 'miniprogram');
const read = (file) => fs.readFileSync(path.join(root, file), 'utf8');

const page = read('pages/customerservice/customerservice.wxml');
const styles = read('pages/customerservice/customerservice.scss');
const nav = read('components/navigation-bar/navigation-bar.wxml');
const navConfig = JSON.parse(read('components/navigation-bar/navigation-bar.json'));

assert.match(page, /<textarea[\s\S]*?auto-height[\s\S]*?\/>\s*<view class="composer-toolbar">/);
assert.match(styles, /\.service-input\s*\{[^}]*font-size:\s*32rpx/);
assert.match(styles, /\.service-input\s*\{[^}]*max-height:\s*240rpx/);
assert.match(nav, /class="uni-nav-chevron"/);
assert.doesNotMatch(nav, /<t-icon/);
assert.equal(navConfig.usingComponents?.['t-icon'], undefined);
