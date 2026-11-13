'use strict';
const ts = require('../tests/report-runtime')('typescript');

// The reader has typed lifecycle boundaries; compile it with the same locked
// runtime already used by the authentication signer, rather than stripping types.
function compileReader(source, fileName = 'contentreader.ts') {
  const result = ts.transpileModule(source, {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 },
    fileName,
    reportDiagnostics: true,
  });
  if (result.diagnostics && result.diagnostics.length) {
    throw new Error('TypeScript reader build failed: ' + ts.formatDiagnostics(result.diagnostics, {
      getCanonicalFileName: file => file,
      getCurrentDirectory: () => process.cwd(),
      getNewLine: () => '\n',
    }));
  }
  return result.outputText;
}
module.exports = { compileReader };
