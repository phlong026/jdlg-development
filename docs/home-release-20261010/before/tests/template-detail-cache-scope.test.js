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
  assert.match(source, /template-detail:auth:/, 'detail page should use an auth-scoped cache for logged-in detail responses');
  assert.match(source, /buildTemplateDetailCacheKey\([^,\n]+,\s*token\)/, 'detail page cache reads should include the current token');
  assert.match(source, /buildTokenCacheScope/, 'detail page should avoid storing raw tokens in cache keys');
  assert.doesNotMatch(source, /getPageCache(?:<any>)?\(buildTemplateDetailCacheKey\([^,\n)]+\)\)/, 'detail page should not read template detail cache without auth scope');
}

console.log('template detail cache scope checks passed');
