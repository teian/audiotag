#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
VERSION=${VERSION:-1.0.0}
ARCH=${ARCH:-$(go env GOARCH)}
case "$ARCH" in amd64) debarch=amd64;; arm64) debarch=arm64;; arm) debarch=armhf;; 386) debarch=i386;; riscv64) debarch=riscv64;; ppc64le) debarch=ppc64el;; *) echo "Unsupported Debian architecture: $ARCH" >&2; exit 1;; esac
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT HUP INT TERM
GOOS=linux GOARCH=$ARCH GOARM=7 CGO_ENABLED=0 VERSION=$VERSION PREFIX=/usr DESTDIR=$work sh scripts/install.sh
mkdir -p "$work/DEBIAN" dist
cat > "$work/DEBIAN/control" <<CONTROL
Package: audiotag
Version: $VERSION
Section: sound
Priority: optional
Architecture: $debarch
Maintainer: AudioTag maintainers <audiotag@example.invalid>
Description: Native lossless audio and audiobook metadata CLI
 Standalone Go tag editor with shell completions and no runtime dependencies.
CONTROL
chmod 0755 "$work/DEBIAN"
go run scripts/deb.go --root "$work" --output "dist/audiotag_${VERSION}_${debarch}.deb"
printf 'Built dist/audiotag_%s_%s.deb\n' "$VERSION" "$debarch"
