#!/bin/sh
# Install a checksummed release without root or install-time compilation.
set -eu
fail() { printf 'mulch: %s\n' "$*" >&2; exit 1; }
command -v curl >/dev/null 2>&1 || fail 'curl is required'
version=${1:-${MULCH_VERSION:-}}
base=https://github.com/n1tishc/mulch/releases
if [ -z "$version" ]; then
  latest=$(curl --proto '=https' --tlsv1.2 -fsSL -o /dev/null -w '%{url_effective}' "$base/latest")
  version=${latest##*/}
fi
version=${version#v}
printf '%s\n' "$version" | LC_ALL=C grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$' || fail 'expected a release version such as v0.1.0'
case $(uname -s) in Darwin) os=darwin;; Linux) os=linux;; *) fail 'curl installation supports macOS and Linux; use npm/pnpm on Windows';; esac
case $(uname -m) in arm64|aarch64) arch=arm64;; x86_64|amd64) arch=amd64;; *) fail 'unsupported architecture';; esac
archive=mulch_${version}_${os}_${arch}.tar.gz
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
curl --proto '=https' --tlsv1.2 -fsSL "$base/download/v$version/$archive" -o "$tmp/$archive"
curl --proto '=https' --tlsv1.2 -fsSL "$base/download/v$version/checksums.txt" -o "$tmp/checksums.txt"
expected=$(awk -v file="$archive" '$2 == file { print $1 }' "$tmp/checksums.txt")
[ ${#expected} -eq 64 ] || fail 'missing or ambiguous checksum'
if command -v sha256sum >/dev/null 2>&1; then
  actual=$(sha256sum "$tmp/$archive" | awk '{print $1}')
elif command -v shasum >/dev/null 2>&1; then
  actual=$(shasum -a 256 "$tmp/$archive" | awk '{print $1}')
else fail 'sha256sum or shasum is required'; fi
[ "$actual" = "$expected" ] || fail 'checksum mismatch; installation aborted'
tar -xzf "$tmp/$archive" -C "$tmp" mulch
install_dir=${MULCH_INSTALL_DIR:-"$HOME/.local/bin"}
mkdir -p "$install_dir"
# Stage beside the destination so replacement is atomic on its filesystem.
staged=$(mktemp "$install_dir/.mulch.XXXXXX")
trap 'rm -rf "$tmp"; rm -f "$staged"' EXIT HUP INT TERM
cp "$tmp/mulch" "$staged"
chmod 755 "$staged"
mv -f "$staged" "$install_dir/mulch"
printf 'Installed mulch %s to %s/mulch\n' "$version" "$install_dir"
case :$PATH: in *:"$install_dir":*) ;; *) printf 'Add %s to your PATH.\n' "$install_dir";; esac
