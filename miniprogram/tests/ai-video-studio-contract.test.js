'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const rules = require('../miniprogram/utils/ai-video-studio');

test('reference limits consume server duration_seconds and reject unknown or non-finite media durations', () => {
  const input = [{ media_id: 'audio-1', media_type: 'audio', duration_seconds: 5 }];
  assert.equal(rules.validateReferences(input).ok, true);
  for (const duration of [NaN, Infinity, -Infinity]) {
    assert.equal(rules.validateReferences([{ media_id: 'audio-1', media_type: 'audio', duration_seconds: duration }]).code, 'reference_duration');
  }
});

test('saved material metadata is loaded from its own authenticated studio endpoint', async () => {
  const module = { exports: {} };
  let requested;
  vm.runInNewContext(fs.readFileSync(path.join(__dirname, '../miniprogram/utils/ai-video-studio-api.js'), 'utf8'), {
    module,
    require: () => ({ generateRequestParams: (_token, _body, requestPath) => ({ requestPath }), paramsToHeaders: value => value }),
    wx: {
      getStorageSync: () => 'test-account',
      request(options) { requested = options; options.success({ statusCode: 200, data: { code: 0, data: { media_id: 'owned-media', url: 'https://media.example.com/signed-image' } } }); },
    },
  });
  const item = await module.exports.getMedia('owned-media');
  assert.equal(item.media_id, 'owned-media');
  assert.equal(requested.url, 'https://api.jiadilingguang.com/api/v1/miniprogram/ai-video-studio/media/owned-media');
  assert.equal(requested.header.requestPath, '/api/v1/miniprogram/ai-video-studio/media/owned-media');
});

test('creation cannot quote non-finite counts and durations', () => {
  const input = { mode: 'text', prompt: '庭院镜头', ratio: '16:9', resolution: '768p', duration: 8, quantity: 1 };
  assert.equal(rules.validateCreation(input).ok, true);
  for (const field of ['duration', 'quantity']) {
    for (const value of [NaN, Infinity, 0, -1, 1.5]) {
      assert.equal(rules.validateCreation({ ...input, [field]: value }).code, 'parameters', `${field}=${value}`);
    }
  }
});
