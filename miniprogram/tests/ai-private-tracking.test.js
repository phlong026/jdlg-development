'use strict';
const assert = require('node:assert/strict');
const test = require('node:test');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const root = path.join(__dirname, '../miniprogram/utils/tracking');
const settle = () => new Promise(resolve => setImmediate(resolve));

for (const route of ['pages/generatedetails/generatedetails', 'pages/generatehistory/generatehistory', 'pages/generateprogress/generateprogress']) {
  test(route + ' can render account content without collecting private generation visits', async () => {
    const requests = [];
    const storage = { token: 'account-token', userInfo: { id: 42 } };
    const wx = {
      getPerformance: () => ({ now: () => 1000 }),
      getStorageSync: key => storage[key],
      setStorageSync: (key, value) => { storage[key] = value; },
      removeStorageSync: key => { delete storage[key]; },
      hideShareMenu() {}, showShareMenu() {},
    };
    const sandbox = {
      module: { exports: {} }, wx, Date, Math, Promise, Uint8Array,
      setTimeout, clearTimeout, setInterval: () => 1,
      getCurrentPages: () => [],
      require: name => name === './api' ? {
        request(endpoint) {
          requests.push(endpoint);
          if (endpoint === '/config') return Promise.resolve({ enabled: true, consent_version: 'v1', privacy_notice: 'optional visits' });
          return Promise.reject(new Error('private generation page must not send tracking data'));
        },
      } : require(path.join(root, name)),
    };
    vm.runInNewContext(fs.readFileSync(path.join(root, 'index.js'), 'utf8'), sandbox);
    const tracking = sandbox.module.exports;
    await tracking.refreshConfig();
    const sdk = tracking.getSDK();
    sdk.appShow();
    let rendered = false;
    const wrapped = tracking.wrapPage({ onLoad() { this.setData({ contentReady: true }); }, onShow() { rendered = true; } });
    const page = { ...wrapped, route, data: {}, setData(values, done) { Object.assign(this.data, values); done?.(); } };
    page.onLoad({ task_no: 'private-task' });
    page.onShow();
    page.onReady();
    sdk.tick();
    await settle();
    assert.equal(rendered, true);
    assert.equal(page.data.contentReady, true);
    assert.equal(sdk.get(page), undefined);
    assert.deepEqual(requests, ['/config']);
    page.onHide();
    page.onUnload();
  });
}
