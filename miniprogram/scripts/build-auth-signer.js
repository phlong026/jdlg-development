'use strict';

const fs = require('node:fs');
const path = require('node:path');
const ts = require('../tests/report-runtime')('typescript');

const sourcePath = path.join(__dirname, '../miniprogram/utils/parameter.ts');
const outputPath = path.join(__dirname, '../miniprogram/utils/parameter.js');
const source = fs.readFileSync(sourcePath, 'utf8');
const result = ts.transpileModule(source, {
  compilerOptions: {
    module: ts.ModuleKind.CommonJS,
    target: ts.ScriptTarget.ES2020,
  },
  fileName: sourcePath,
  reportDiagnostics: true,
});

if (result.diagnostics?.length) {
  const message = ts.formatDiagnostics(result.diagnostics, {
    getCanonicalFileName: file => file,
    getCurrentDirectory: () => process.cwd(),
    getNewLine: () => '\n',
  });
  process.stderr.write(message);
  process.exitCode = 1;
} else if (process.argv.includes('--check')) {
  if (!fs.existsSync(outputPath) || fs.readFileSync(outputPath, 'utf8') !== result.outputText) {
    console.error('Source/build mismatch: miniprogram/utils/parameter.ts');
    console.error('Run npm run build:auth-signer before uploading the mini-program.');
    process.exitCode = 1;
  }
} else {
  fs.writeFileSync(outputPath, result.outputText);
}
