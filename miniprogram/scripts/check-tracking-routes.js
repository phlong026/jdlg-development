'use strict';

const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

// An intentional app-route addition must update this reviewed baseline as well
// as both policy inventories. Never silently classify an unreviewed page.
const EXPECTED_ROUTE_COUNT = 58;

function validateTrackingRoutes({ app, policies, backendRoutes }) {
  assert.ok(Array.isArray(app.pages), 'app.pages must be an array');
  assert.ok(!(app.subPackages && app.subpackages), 'use only one subpackage spelling');
  const packages = app.subPackages || app.subpackages || [];
  assert.ok(Array.isArray(packages), 'app subpackages must be an array');
  const routes = [...app.pages];
  for (const entry of packages) {
    assert.ok(entry && typeof entry.root === 'string', 'subpackage root is required');
    assert.ok(Array.isArray(entry.pages), 'subpackage pages must be an array');
    for (const page of entry.pages) {
      assert.equal(typeof page, 'string', 'subpackage page must be a string');
      routes.push(`${entry.root}/${page}`);
    }
  }
  for (const route of routes) {
    assert.equal(typeof route, 'string', 'route must be a string');
    assert.match(route, /^pages\/[A-Za-z0-9_-]+(?:\/[A-Za-z0-9_-]+)+$/, `invalid route: ${route}`);
  }
  assert.equal(new Set(routes).size, routes.length, 'app routes must not contain duplicates');
  assert.equal(routes.length, EXPECTED_ROUTE_COUNT, 'app route count differs from the reviewed baseline');
  assert.ok(policies && typeof policies === 'object' && !Array.isArray(policies), 'frontend policies are required');
  assert.ok(backendRoutes && typeof backendRoutes === 'object' && !Array.isArray(backendRoutes), 'backend routes are required');
  const expected = [...routes].sort();
  assert.deepEqual(Object.keys(policies).sort(), expected, 'frontend policy keys must exactly match app routes');
  assert.deepEqual(Object.keys(backendRoutes).sort(), expected, 'backend route keys must exactly match app routes');

  let publicCount = 0;
  const publicKinds = new Set(['home', 'list', 'catalog', 'detail', 'form', 'landing', 'owned-content']);
  for (const route of expected) {
    const policy = policies[route];
    const backend = backendRoutes[route];
    assert.ok(policy && typeof policy === 'object', `${route}: missing frontend policy`);
    assert.ok(backend && typeof backend === 'object', `${route}: missing backend policy`);
    assert.equal(typeof policy.collect, 'boolean', `${route}: collect must be explicit`);
    assert.equal(typeof backend.Public, 'boolean', `${route}: Public must be explicit`);
    assert.equal(backend.Public, policy.collect, `${route}: backend Public differs from frontend collect`);
    assert.equal(backend.Type, policy.kind, `${route}: backend Type differs from frontend kind`);
    assert.ok(typeof backend.Title === 'string' && backend.Title.trim(), `${route}: backend Title is required`);
    if (policy.collect) {
      publicCount += 1;
      assert.ok(publicKinds.has(policy.kind), `${route}: unknown public policy kind`);
      assert.ok(!policy.exclusionReason, `${route}: public route cannot have an exclusion reason`);
      assert.ok(typeof policy.readiness === 'string', `${route}: readiness is required`);
      if (policy.readiness.startsWith('nonempty:')) {
        assert.match(policy.field || '', /^[A-Za-z][A-Za-z0-9_]*$/, `${route}: nonempty readiness requires a field`);
        assert.equal(policy.readiness, `nonempty:${policy.field}`, `${route}: readiness field mismatch`);
      } else {
        assert.ok(['rendered', 'explicit'].includes(policy.readiness), `${route}: unsupported readiness policy`);
      }
    } else {
      assert.equal(policy.kind, 'excluded', `${route}: private route must use excluded kind`);
      assert.match(policy.exclusionReason || '', /^[a-z]+(?:-[a-z]+)*$/, `${route}: exclusion reason is required`);
      assert.equal(policy.readiness, `excluded-${policy.exclusionReason}`, `${route}: exclusion readiness mismatch`);
    }
  }
  return { total: routes.length, public: publicCount, excluded: routes.length - publicCount };
}

function checkRepository() {
  const root = path.resolve(__dirname, '..');
  const app = JSON.parse(fs.readFileSync(path.join(root, 'miniprogram/app.json'), 'utf8'));
  const routeModule = require(path.join(root, 'miniprogram/utils/tracking/routes.js'));
  const backendRoutes = JSON.parse(fs.readFileSync(path.join(root, '../service/tracking/routes.json'), 'utf8'));
  const result = validateTrackingRoutes({ app, policies: routeModule.ROUTES, backendRoutes });
  for (const route of Object.keys(routeModule.ROUTES)) {
    assert.deepEqual(routeModule.policy(route), routeModule.ROUTES[route], `${route}: runtime policy mismatch`);
    assert.deepEqual(routeModule.policy(`/${route}`), routeModule.ROUTES[route], `${route}: leading slash policy mismatch`);
  }
  assert.equal(routeModule.policy('pages/__unregistered__/__unregistered__').collect, false, 'unknown routes must fail closed');
  return result;
}

if (require.main === module) {
  try {
    const result = checkRepository();
    console.log(`Tracking routes consistent: ${result.total} app routes, ${result.public} public, ${result.excluded} excluded; frontend and Go inventories match.`);
  } catch (error) {
    console.error(`Tracking route consistency failed: ${error.message}`);
    process.exitCode = 1;
  }
}

module.exports = { checkRepository, validateTrackingRoutes };
