# Distribution and releases

The current target is a [small portfolio release](release-readiness.md), not the deferred multi-provider platform. [Draft 0.1.0 notes](release-notes-0.1.0.md) and a [five-minute walkthrough](portfolio-demo.md) accompany it. Source/demo availability and native archives are sufficient for the portfolio; npm publication is optional.

Mulch has one native implementation and one version per release. GoReleaser builds Linux, macOS, and Windows for amd64 and arm64. Archives have the explicit name `mulch_VERSION_GOOS_GOARCH.tar.gz` (Windows uses `.zip`). `checksums.txt` covers the archives. The curl installer is attached as `install.sh`.

The npm entry package is `@n1tishc/mulch`. Six optional packages contain the corresponding native binaries with `os` and `cpu` constraints. Their exact versions match the entry package. The wrapper forwards arguments, standard streams, exit codes, and termination signals. There is no postinstall script, download on first run, or local compilation. Keep optional dependencies enabled. Node 22 or later is required for the wrapper; the curl-installed executable does not require Node.

This follows npm's [bin, os, cpu, and optionalDependencies conventions](https://docs.npmjs.com/files/package.json/) and avoids dependence on pnpm's [build-script approval policy](https://pnpm.io/settings#onlybuiltdependencies). Archive names are explicitly configured using [GoReleaser archive templates](https://goreleaser.com/customization/archive/).

## Local verification

```sh
make verify
make distribution-test
make release-dry
VERSION=$(node -p "require('./dist/metadata.json').version")
node scripts/package-npm.mjs "$VERSION"
```

Distribution tests require npm, pnpm, tar, and zip/unzip on macOS or Linux. They package a real host binary, install packed tarballs offline with npm and pnpm with lifecycle scripts disabled, exercise command success and failure, install to a path containing spaces, and verify that checksum rejection preserves an existing binary. Non-host archives in that test use the host executable as a packaging fixture; they are not cross-platform execution tests. CI separately builds and stages all six real GoReleaser targets. Windows execution still needs a native Windows smoke test.

## Prepare a release

1. Set `npm/mulch/package.json`'s version and all six optional dependency versions to the intended SemVer. Commit the change and create the matching `vVERSION` tag when ready.
2. Run `gh workflow run release.yml --ref vVERSION`. The workflow invokes CI for that ref, checks the tag against the entry package, builds a **draft** GitHub release, verifies archive checksums while staging npm packages, and attaches seven npm tarballs to the draft. Branch runs do not create releases.
3. Inspect the archives, checksums, installer, and npm tarballs. `scripts/package-npm.mjs` never publishes; it can be rerun against the same release artifacts. Release staging happens on macOS/Linux because it uses tar and unzip.
4. Before first npm publication, confirm that the npm account owns the `@n1tishc` scope. The GitHub username does not establish npm ownership. Authenticate to npm using an authorized account.
5. Publish the six native tarballs first, then the entry package. For locally staged artifacts:

```sh
for package in dist/npm/*; do
  (cd "$package" && npm pack --ignore-scripts)
done
for package in dist/npm/darwin-* dist/npm/linux-* dist/npm/win32-*; do
  npm publish "$package"/*.tgz --access public
done
npm publish dist/npm/mulch/*.tgz --access public
```

6. Publish the reviewed GitHub draft. Verify installation from both public distribution channels before announcing the release. A GitHub draft is not available to the public curl installer.

Registry publication and publishing the GitHub draft are explicit maintainer actions. No registry release or tag is created by local verification. Existing npm versions are immutable; resolve a partial publication by publishing the missing packages at the same version, without changing already-published contents.

## User commands after publication

```sh
curl -fsSL https://github.com/n1tishc/mulch/releases/latest/download/install.sh | sh
# Pin both the installer and binary version:
curl -fsSL https://github.com/n1tishc/mulch/releases/download/v0.1.0/install.sh | sh -s -- v0.1.0
pnpm add -g @n1tishc/mulch@0.1.0
# Or install into a project:
pnpm add -D @n1tishc/mulch@0.1.0
pnpm exec mulch version
```

The curl default is `$HOME/.local/bin`; set `MULCH_INSTALL_DIR` to override it. No sudo or shell-profile changes are made. Version selection accepts an argument or `MULCH_VERSION`, otherwise resolves the latest release. A missing release or checksum mismatch fails before replacing the installed executable. Checksums verify download integrity against GitHub release metadata; they are not independent artifact signatures.

Linux Bash execution additionally requires Bubblewrap and a host configuration permitting its sandbox. Windows supports session/replay/viewer operations but does not yet support the agent Bash tool. These requirements do not affect `mulch version` or package installation.
