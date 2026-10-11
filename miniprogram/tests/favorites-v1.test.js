const fs = require('fs');
const path = require('path');
const assert = require('assert');

const root = path.resolve(__dirname, '..');

function read(relativePath) {
  return fs.readFileSync(path.join(root, relativePath), 'utf8');
}

const favoritesTs = read('miniprogram/pages/myfavorites/myfavorites.ts');
const favoritesWxml = read('miniprogram/pages/myfavorites/myfavorites.wxml');
const detailTs = read('miniprogram/pages/templatesquaredetails/templatesquaredetails.ts');

assert.match(favoritesTs, /pageSize:\s*20/, 'favorites page should request paged data');
assert.match(favoritesTs, /hasMore:\s*true/, 'favorites page should track whether more pages exist');
assert.match(favoritesTs, /onReachBottom\(\)/, 'favorites page should load more on reach bottom');
assert.match(favoritesTs, /removeFavorite/, 'favorites page should support removing a favorite in-place');
assert.match(favoritesWxml, /catchtap="removeFavorite"/, 'favorites card should expose a cancel-favorite action');
assert.match(detailTs, /已收藏/, 'template detail should use favorite wording after a successful add');
assert.match(detailTs, /已取消收藏/, 'template detail should use favorite wording after removal');

console.log('favorites V1 checks passed');
