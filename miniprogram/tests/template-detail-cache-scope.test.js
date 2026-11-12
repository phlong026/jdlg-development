const fs = require('fs');
const path = require('path');
const assert = require('assert');

const root = path.resolve(__dirname, '..');

function read(relativePath) {
  return fs.readFileSync(path.join(root, relativePath), 'utf8');
}

const indexTs = read('miniprogram/pages/index/index.ts');
const indexJs = read('miniprogram/pages/index/index.js');
const detailTs = read('miniprogram/pages/templatesquaredetails/templatesquaredetails.ts');
const detailJs = read('miniprogram/pages/templatesquaredetails/templatesquaredetails.js');

for (const source of [indexTs, indexJs]) {
  assert.match(source, /buildPublicTemplateDetailCacheKey/, 'home prefetch should use the public template-detail cache key helper');
  assert.match(source, /template-detail:public:/, 'home prefetch should only write public detail cache entries');
  assert.doesNotMatch(source, /template-detail:\$\{Number\(templateId \|\| 0\)\}/, 'home prefetch should not write the old shared detail cache key');
}

for (const source of [detailTs, detailJs]) {
  assert.match(source, /template-detail:public:/, 'detail page should keep a public detail cache scope for anonymous visits');
  assert.doesNotMatch(source, /template-detail:auth:/, 'public detail data must not vary by cached profile token');
  assert.match(source, /getPageCache(?:<any>)?\)?\(buildPublicTemplateDetailCacheKey\(/, 'detail page should reuse the public projection for every reader');
}

console.log('template detail cache scope checks passed');
