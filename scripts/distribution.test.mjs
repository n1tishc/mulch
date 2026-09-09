import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync, copyFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { execFileSync, spawnSync } from 'node:child_process';
import { createHash } from 'node:crypto';
const root = resolve('.');
const run = (command, args, options = {}) => execFileSync(command, args, { encoding: 'utf8', ...options });

test('release packages install without lifecycle scripts; checksum failures preserve existing installation', () => {
 const temp = mkdtempSync(join(tmpdir(), 'mulch-distribution-'));
 try {
  const dist = join(temp, 'dist'); mkdirSync(dist);
  const fixture = join(temp, 'fixture'); mkdirSync(fixture);
  if (!process.env.MULCH_TEST_BINARY) throw new Error('Set MULCH_TEST_BINARY to a built mulch binary (version 0.1.0)');
  copyFileSync(resolve(process.env.MULCH_TEST_BINARY), join(fixture, 'mulch'));
  copyFileSync(resolve(process.env.MULCH_TEST_BINARY), join(fixture, 'mulch.exe'));
  let checksums = '';
  for (const os of ['darwin', 'linux', 'windows']) for (const arch of ['arm64', 'amd64']) {
   const archive = `mulch_0.1.0_${os}_${arch}.${os === 'windows' ? 'zip' : 'tar.gz'}`;
   if (os === 'windows') run('zip', ['-q', join(dist, archive), 'mulch.exe'], { cwd: fixture });
   else run('tar', ['-czf', join(dist, archive), '-C', fixture, 'mulch']);
   checksums += `${createHash('sha256').update(readFileSync(join(dist, archive))).digest('hex')}  ${archive}\n`;
  }
  writeFileSync(join(dist, 'checksums.txt'), checksums);
  run(process.execPath, ['scripts/package-npm.mjs', '0.1.0', dist]);
  const stage = join(dist, 'npm');
  const wrapper = join(stage, 'mulch');
  const manifestPath = join(wrapper, 'package.json');
  const manifest = JSON.parse(readFileSync(manifestPath));
  assert.equal(manifest.scripts, undefined);
  for (const name of Object.keys(manifest.optionalDependencies)) {
   const platform = name.replace('@n1tishc/mulch-', '');
   const packed = JSON.parse(run('npm', ['pack', '--json', '--ignore-scripts'], { cwd: join(stage, platform), env: { ...process.env, npm_config_cache: join(temp, 'npm-cache') } }));
   manifest.optionalDependencies[name] = `file:${join(stage, platform, packed[0].filename)}`;
  }
  // Replace only registry locations in this fixture: package versions, files and
  // platform constraints are exactly those produced for publication.
  writeFileSync(manifestPath, JSON.stringify(manifest));
  const packed = JSON.parse(run('npm', ['pack', '--json', '--ignore-scripts'], { cwd: wrapper, env: { ...process.env, npm_config_cache: join(temp, 'npm-cache') } }));
  for (const manager of ['npm', 'pnpm']) {
   const consumer = join(temp, manager); mkdirSync(consumer);
   writeFileSync(join(consumer, 'package.json'), '{"private":true}');
   run(manager, ['install', '--ignore-scripts', '--offline', join(wrapper, packed[0].filename)], { cwd: consumer, env: { ...process.env, npm_config_cache: join(temp, 'npm-cache'), PNPM_HOME: join(temp, 'pnpm-home') } });
   assert.equal(run(join(consumer, 'node_modules/.bin/mulch'), ['version']).trim(), 'mulch 0.1.0');
   const bad = spawnSync(join(consumer, 'node_modules/.bin/mulch'), ['not-a-command']);
   assert.equal(bad.status, 1, 'wrapper preserves nonzero exit status');
  }
  const mock = join(temp, 'mock'); mkdirSync(mock);
  writeFileSync(join(mock, 'curl'), `#!/bin/sh\nset -eu\nurl=\nout=\nwhile [ "$#" -gt 0 ]; do\n case "$1" in https://*) url=$1;; -o) shift; out=$1;; esac\n shift\ndone\ncp "$MULCH_FIXTURES/\${url##*/}" "$out"\n`, { mode: 0o755 });
  const installDir = join(temp, 'installed with spaces');
  const env = { ...process.env, PATH: `${mock}:${process.env.PATH}`, MULCH_FIXTURES: dist, MULCH_INSTALL_DIR: installDir };
  run('sh', [join(root, 'scripts/install.sh'), 'v0.1.0'], { env });
  assert.equal(run(join(installDir, 'mulch'), ['version']).trim(), 'mulch 0.1.0');
  writeFileSync(join(dist, 'checksums.txt'), checksums.replace(/[a-f0-9]{64}/g, '0'.repeat(64)));
  const rejected = spawnSync('sh', [join(root, 'scripts/install.sh'), 'v0.1.0'], { env, encoding: 'utf8' });
  assert.notEqual(rejected.status, 0);
  assert.match(rejected.stderr, /checksum mismatch/);
  assert.equal(run(join(installDir, 'mulch'), ['version']).trim(), 'mulch 0.1.0');
  const staging = spawnSync(process.execPath, ['scripts/package-npm.mjs', '0.1.0', dist], { encoding:'utf8' });
  assert.notEqual(staging.status, 0);
  assert.match(staging.stderr, /Checksum mismatch/);
 } finally { rmSync(temp, { recursive: true, force: true }); }
});
