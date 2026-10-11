const path = require('node:path');
module.exports = function runtime(name) {
  const roots = [process.env.REPORT_TEST_NODE_MODULES, path.resolve(__dirname, '../../background/node_modules'), path.resolve(__dirname, '../../AItestPlatform/node_modules')].filter(Boolean);
  for (const root of roots) {
    try { return require(require.resolve(name, { paths: [root] })); } catch (error) { if (error.code !== 'MODULE_NOT_FOUND') throw error; }
  }
  throw new Error('Set REPORT_TEST_NODE_MODULES to an existing runtime containing ' + name + '; no dependency installation is required.');
};
