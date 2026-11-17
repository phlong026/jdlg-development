'use strict';
const path = require('node:path');
const ts = require('../tests/report-runtime')('typescript');

const configPath = path.resolve(process.argv[2] || path.join(__dirname, '../tsconfig.json'));
const config = ts.readConfigFile(configPath, ts.sys.readFile);
const parsed = config.error ? null : ts.parseJsonConfigFileContent(config.config, ts.sys, path.dirname(configPath), { noEmit: true });
const diagnostics = config.error ? [config.error] : parsed.errors.length ? parsed.errors : ts.getPreEmitDiagnostics(ts.createProgram(parsed.fileNames, parsed.options));

if (diagnostics.length) {
  process.stderr.write(ts.formatDiagnostics(diagnostics, {
    getCanonicalFileName: file => file,
    getCurrentDirectory: () => process.cwd(),
    getNewLine: () => '\n',
  }));
  process.exitCode = 1;
} else {
  process.stdout.write('TypeScript: 0 errors\n');
}
