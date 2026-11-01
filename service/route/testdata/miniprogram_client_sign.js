'use strict';

const fs = require('fs');

const input = JSON.parse(fs.readFileSync(0, 'utf8'));
const signer = require(input.signerPath);
const args = [input.token, input.body, input.path, input.deviceID];

if (input.explicitDefaults) {
  args.push('jiadilinguang-hmac-secret-key-2024', '1234567890123456');
}

process.stdout.write(JSON.stringify(signer.paramsToHeaders(signer.generateRequestParams(...args))));
