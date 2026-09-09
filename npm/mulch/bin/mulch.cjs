#!/usr/bin/env node
'use strict';
const { spawn } = require('node:child_process');
const target = `${process.platform}-${process.arch}`;
const pkg = `@n1tishc/mulch-${target}`;
const manifest = require('../package.json');
if (!manifest.optionalDependencies[pkg]) {
  console.error(`mulch: unsupported platform ${target}`);
  process.exit(1);
}
let binary;
try {
  binary = require.resolve(`${pkg}/bin/mulch${process.platform === 'win32' ? '.exe' : ''}`);
} catch {
  console.error(`mulch: missing ${pkg}. Reinstall ${manifest.name} with optional dependencies enabled.`);
  process.exit(1);
}
const child = spawn(binary, process.argv.slice(2), { stdio: 'inherit' });
for (const signal of ['SIGINT', 'SIGTERM']) {
  process.on(signal, () => child.kill(signal));
}
child.on('error', err => { console.error(`mulch: ${err.message}`); process.exitCode = 1; });
child.on('exit', (code, signal) => {
  if (signal) {
    process.removeAllListeners(signal);
    process.kill(process.pid, signal);
  } else process.exitCode = code ?? 1;
});
