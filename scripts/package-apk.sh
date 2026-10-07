#!/bin/sh
# Run as an abuild-enabled non-root user on Alpine.
set -eu
cd "$(dirname "$0")/.."
command -v abuild >/dev/null || { echo 'Run on Alpine with alpine-sdk and Go installed' >&2; exit 1; }
VERSION=${VERSION:-1.0.0}
root=$(pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT HUP INT TERM
mkdir -p "$work/audiotag-$VERSION" "$work/local/audiotag" dist
VERSION=$VERSION go run scripts/source.go --output "$work/local/audiotag/audiotag-$VERSION.tar.gz"
sed "s/^pkgver=.*/pkgver=$VERSION/" packaging/alpine/APKBUILD > "$work/local/audiotag/APKBUILD"
cd "$work/local/audiotag"
abuild checksum
REPODEST="$root/dist/apk" abuild -r
