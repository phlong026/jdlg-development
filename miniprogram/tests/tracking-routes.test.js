'use strict';

const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');
const { checkRepository, validateTrackingRoutes } = require('../scripts/check-tracking-routes');

function fixture() {
  return {
    app: JSON.parse(fs.readFileSync(path.join(__dirname, '../miniprogram/app.json'), 'utf8')),
    policies: structuredClone(require('../miniprogram/utils/tracking/routes').ROUTES),
    backendRoutes: JSON.parse(fs.readFileSync(path.join(__dirname, '../../service/tracking/routes.json'), 'utf8')),
  };
}

test('actual app, runtime policies and Go inventory agree for every reviewed route', () => {
  assert.deepEqual(checkRepository(), { total: 58, public: 21, excluded: 37 });
});

const mutations = [
  ['duplicate app route', f => { f.app.pages[1] = f.app.pages[0]; }, /duplicates/],
  ['unreviewed app route', f => { f.app.pages.push('pages/new/new'); }, /reviewed baseline/],
  ['missing subpackage route', f => { f.app.subPackages.pop(); }, /reviewed baseline/],
  ['missing frontend policy', f => { delete f.policies['pages/index/index']; }, /frontend policy keys/],
  ['extra frontend policy', f => { f.policies['pages/extra/extra'] = f.policies['pages/index/index']; }, /frontend policy keys/],
  ['missing Go route', f => { delete f.backendRoutes['pages/contentreader/contentreader']; }, /backend route keys/],
  ['extra Go route', f => { f.backendRoutes['pages/extra/extra'] = f.backendRoutes['pages/index/index']; }, /backend route keys/],
  ['case drift', f => { f.app.pages[0] = 'pages/Index/index'; }, /frontend policy keys/],
  ['implicit frontend collect', f => { delete f.policies['pages/index/index'].collect; }, /collect must be explicit/],
  ['implicit Go Public', f => { delete f.backendRoutes['pages/index/index'].Public; }, /Public must be explicit/],
  ['public/private disagreement', f => { f.backendRoutes['pages/my/my'].Public = true; }, /Public differs/],
  ['type disagreement', f => { f.backendRoutes['pages/index/index'].Type = 'detail'; }, /Type differs/],
  ['missing readiness', f => { delete f.policies['pages/index/index'].readiness; }, /readiness is required/],
  ['unknown readiness', f => { f.policies['pages/index/index'].readiness = 'loading-finished'; }, /unsupported readiness/],
  ['nonempty field drift', f => { f.policies['pages/aitooldetail/aitooldetail'].field = 'wrong'; }, /readiness field mismatch/],
  ['private reason missing', f => { delete f.policies['pages/my/my'].exclusionReason; }, /exclusion reason/],
  ['private readiness drift', f => { f.policies['pages/my/my'].readiness = 'rendered'; }, /exclusion readiness/],
];

for (const [name, mutate, error] of mutations) {
  test(`route gate rejects ${name}`, () => {
    const data = fixture();
    mutate(data);
    assert.throws(() => validateTrackingRoutes(data), error);
  });
}
