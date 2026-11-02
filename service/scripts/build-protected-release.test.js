'use strict';

const assert = require('node:assert/strict');
const crypto = require('node:crypto');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { execFileSync, spawnSync } = require('node:child_process');
const { test } = require('node:test');

const scriptSource = path.join(__dirname, 'build-protected-release.sh');
const verifierSource = path.join(__dirname, 'verify-auth-elf.py');
const sha256 = file => crypto.createHash('sha256').update(fs.readFileSync(file)).digest('hex');

function run(script, repo, cryptoFile, output, env) {
  return spawnSync(script, ['--crypto-file', cryptoFile, '--output-dir', output], {
    cwd: repo,
    env,
    encoding: 'utf8',
  });
}

test('protected release uses an archive, enforces strict auth and writes an isolated verified manifest', t => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'jdlg-release-script-'));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  const repo = path.join(root, 'repo');
  const protectedCrypto = path.join(root, 'protected-crypto.go');
  const placeholderCrypto = path.join(root, 'placeholder-crypto.go');
  fs.mkdirSync(path.join(repo, 'service/scripts'), { recursive: true });
  fs.mkdirSync(path.join(repo, 'service/function'), { recursive: true });
  fs.mkdirSync(path.join(repo, 'service/route'), { recursive: true });
  fs.copyFileSync(scriptSource, path.join(repo, 'service/scripts/build-protected-release.sh'));
  fs.copyFileSync(verifierSource, path.join(repo, 'service/scripts/verify-auth-elf.py'));
  fs.chmodSync(path.join(repo, 'service/scripts/build-protected-release.sh'), 0o755);
  fs.writeFileSync(path.join(repo, 'service/go.mod'), 'module releasefixture\n\ngo 1.22\n');
  fs.writeFileSync(path.join(repo, 'service/main.go'), 'package main\nimport ("fmt"; "releasefixture/function")\nfunc main() { fmt.Println(function.AuthKeyLengths()) }\n');
  fs.writeFileSync(path.join(repo, 'service/function/crypto.go'), cryptoSource('REPLACE_SM4_KEY!', 'REPLACE_WITH_LOCAL_HMAC_KEY'));
  fs.writeFileSync(path.join(repo, 'service/route/profile_client_signature_test.go'), `package route
import (
  "crypto/sha256"
  "encoding/hex"
  "os"
  "testing"
)
func requireStrict(t *testing.T) {
  if os.Getenv("JDLG_REQUIRE_CLIENT_AUTH_CONTRACT") != "1" { t.Fatal("strict contract environment missing") }
  source, err := os.ReadFile("../function/crypto.go"); if err != nil { t.Fatal(err) }
  digest := sha256.Sum256(source)
  if hex.EncodeToString(digest[:]) != os.Getenv("JDLG_AUTH_CRYPTO_SHA256") { t.Fatal("injected crypto digest mismatch") }
}
func TestMiniprogramClientSignerDefaultsPassTokenAuthRequired(t *testing.T) { requireStrict(t) }
func TestMiniprogramClientSignerExplicitDefaultsPassTokenAuthRequired(t *testing.T) { requireStrict(t) }
`);
  fs.writeFileSync(path.join(repo, 'tracked.txt'), 'clean\n');
  fs.writeFileSync(protectedCrypto, cryptoSource('test-only-key-12', 'test-only-hmac'), { mode: 0o600 });
  fs.writeFileSync(placeholderCrypto, cryptoSource('REPLACE_SM4_KEY!', 'REPLACE_WITH_LOCAL_HMAC_KEY'), { mode: 0o600 });

  execFileSync('git', ['init', '-q'], { cwd: repo });
  execFileSync('git', ['add', '.'], { cwd: repo });
  execFileSync('git', ['-c', 'user.name=Release Test', '-c', 'user.email=release@example.invalid', 'commit', '-qm', 'fixture'], { cwd: repo });
  const script = path.join(repo, 'service/scripts/build-protected-release.sh');
  const env = { ...process.env };

  const output = path.join(root, 'success-output');
  const success = run(script, repo, protectedCrypto, output, env);
  assert.equal(success.status, 0, success.stdout + success.stderr);
  assert.deepEqual(fs.readdirSync(output).sort(), ['auth-build.json', 'service']);
  assert.equal(fs.statSync(output).mode & 0o777, 0o700);
  assert.equal(fs.statSync(path.join(output, 'service')).mode & 0o777, 0o700);
  assert.equal(fs.statSync(path.join(output, 'auth-build.json')).mode & 0o777, 0o600);
  const manifest = JSON.parse(fs.readFileSync(path.join(output, 'auth-build.json'), 'utf8'));
  assert.equal(manifest.crypto_sha256, sha256(protectedCrypto));
  assert.equal(manifest.binary_sha256, sha256(path.join(output, 'service')));
  assert.equal(manifest.auth_binary_hmac_match, true);
  assert.equal(manifest.auth_binary_sm4_match, true);
  assert.match(manifest.go_version, /^go\d/);
  assert.equal(Object.hasOwn(manifest, 'files'), false);
  assert.doesNotMatch(JSON.stringify(manifest), /test-only|sm4Key|hmacKey/);

  fs.writeFileSync(path.join(repo, 'tracked.txt'), 'dirty\n');
  const dirty = run(script, repo, protectedCrypto, path.join(root, 'dirty-output'), env);
  assert.notEqual(dirty.status, 0);
  assert.match(dirty.stderr, /changes outside the protected crypto source/);
  execFileSync('git', ['checkout', '--', 'tracked.txt'], { cwd: repo });

  execFileSync('git', ['update-index', '--skip-worktree', 'tracked.txt'], { cwd: repo });
  const hidden = run(script, repo, protectedCrypto, path.join(root, 'hidden-output'), env);
  assert.notEqual(hidden.status, 0);
  assert.match(hidden.stderr, /unexpected hidden tracked files/);
  execFileSync('git', ['update-index', '--no-skip-worktree', 'tracked.txt'], { cwd: repo });

  const placeholder = run(script, repo, placeholderCrypto, path.join(root, 'placeholder-output'), env);
  assert.notEqual(placeholder.status, 0);
  assert.match(placeholder.stderr, /contains release placeholders/);
});

function cryptoSource(sm4, hmac) {
  return `package function
var sm4Key = []byte(${JSON.stringify(sm4)})
var hmacKey = []byte(${JSON.stringify(hmac)})
func AuthKeyLengths() (int, int) { return len(sm4Key), len(hmacKey) }
`;
}
