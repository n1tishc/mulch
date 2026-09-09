// Stage npm packages from the exact checksummed GoReleaser archives. Never publish here.
import { readFileSync, mkdirSync, writeFileSync, copyFileSync, chmodSync } from 'node:fs';
import { resolve, join } from 'node:path';
import { execFileSync } from 'node:child_process';
import { createHash } from 'node:crypto';
const [version, directory = 'dist'] = process.argv.slice(2);
if (!/^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$/.test(version ?? '')) throw new Error('Usage: node scripts/package-npm.mjs VERSION [dist]');
const dist = resolve(directory);
const source = JSON.parse(readFileSync('npm/mulch/package.json'));
const checksums = readFileSync(join(dist, 'checksums.txt'), 'utf8');
const output = join(dist, 'npm');
const manifest = { ...source, version, optionalDependencies: {} };
for (const os of ['darwin', 'linux', 'win32']) {
  for (const cpu of ['arm64', 'x64']) {
    const name = `@n1tishc/mulch-${os}-${cpu}`;
    const goos = os === 'win32' ? 'windows' : os;
    const goarch = cpu === 'x64' ? 'amd64' : cpu;
    const archive = `mulch_${version}_${goos}_${goarch}.${os === 'win32' ? 'zip' : 'tar.gz'}`;
    const checksum = checksums.split('\n').map(line => line.trim().split(/\s+/)).find(parts => parts[1] === archive)?.[0];
    const actual = createHash('sha256').update(readFileSync(join(dist, archive))).digest('hex');
    if (checksum !== actual) throw new Error(`Checksum mismatch: ${archive}`);
    const destination = join(output, `${os}-${cpu}`);
    mkdirSync(join(destination, 'bin'), { recursive: true });
    const binary = os === 'win32' ? 'mulch.exe' : 'mulch';
    const data = os === 'win32'
      ? execFileSync('unzip', ['-p', join(dist, archive), binary], { maxBuffer: 128 * 1024 * 1024 })
      : execFileSync('tar', ['-xOf', join(dist, archive), binary], { maxBuffer: 128 * 1024 * 1024 });
    writeFileSync(join(destination, 'bin', binary), data, { mode: 0o755 });
    chmodSync(join(destination, 'bin', binary), 0o755);
    writeFileSync(join(destination, 'package.json'), JSON.stringify({ name, version, description: `Mulch native binary for ${os}/${cpu}`, os: [os], cpu: [cpu], files: ['bin'], repository: source.repository, publishConfig: source.publishConfig }, null, 2) + '\n');
    manifest.optionalDependencies[name] = version;
  }
}
const wrapper = join(output, 'mulch');
mkdirSync(join(wrapper, 'bin'), { recursive: true });
copyFileSync('npm/mulch/bin/mulch.cjs', join(wrapper, 'bin/mulch.cjs'));
chmodSync(join(wrapper, 'bin/mulch.cjs'), 0o755);
copyFileSync('README.md', join(wrapper, 'README.md'));
writeFileSync(join(wrapper, 'package.json'), JSON.stringify(manifest, null, 2) + '\n');
console.log(`Staged ${version}: six native packages and ${manifest.name} in ${output}`);
