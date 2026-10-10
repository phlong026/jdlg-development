const fs = require('fs');
const path = require('path');
const assert = require('assert');

const repoRoot = path.resolve(__dirname, '..', '..');

function read(relativePath) {
  return fs.readFileSync(path.join(repoRoot, relativePath), 'utf8');
}

function exists(relativePath) {
  return fs.existsSync(path.join(repoRoot, relativePath));
}

assert.ok(exists('service/model/user_favorite.go'), 'backend should define a generic user favorite model');
assert.ok(exists('service/route/favorite.go'), 'backend should register generic favorite routes');
assert.ok(exists('miniprogram/miniprogram/utils/favoriteApi.ts'), 'miniprogram should have a shared favorite API helper');

const favoriteRoute = read('service/route/favorite.go');
assert.match(favoriteRoute, /target_type/, 'favorite route should use target_type');
assert.match(favoriteRoute, /template_likes/, 'favorite route should preserve template_likes compatibility');
assert.match(favoriteRoute, /ai_tool/, 'favorite route should support AI tools');
assert.match(favoriteRoute, /designer/, 'favorite route should support designers');
assert.match(favoriteRoute, /inspiration/, 'favorite route should support inspirations');

const favoritesPage = read('miniprogram/miniprogram/pages/myfavorites/myfavorites.ts');
assert.match(favoritesPage, /favoriteTabs/, 'favorites page should define category tabs');
assert.match(favoritesPage, /targetType/, 'favorites page should carry target type per card');
assert.match(favoritesPage, /onFavoriteTabTap/, 'favorites page should switch categories');
assert.match(favoritesPage, /favorites\?type=/, 'favorites page should request filtered favorite types');

const favoritesWxml = read('miniprogram/miniprogram/pages/myfavorites/myfavorites.wxml');
assert.match(favoritesWxml, /favorite-tab/, 'favorites page should render visible category tabs');

for (const page of [
  'miniprogram/miniprogram/pages/aitooldetail/aitooldetail.ts',
  'miniprogram/miniprogram/pages/inspirationdetail/inspirationdetail.ts',
  'miniprogram/miniprogram/pages/designerhome/designerhome.ts',
]) {
  const content = read(page);
  assert.match(content, /toggleFavorite/, `${page} should support toggling favorite state`);
  assert.match(content, /loadFavoriteState/, `${page} should load favorite state`);
}

console.log('expanded favorites checks passed');
