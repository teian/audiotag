#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
command -v rpmbuild >/dev/null || { echo 'Install rpm-build first' >&2; exit 1; }
VERSION=${VERSION:-1.0.0}
ARCH=${ARCH:-$(go env GOARCH)}
case "$ARCH" in amd64) rpmarch=x86_64;; arm64) rpmarch=aarch64;; arm) rpmarch=armv7hl;; 386) rpmarch=i686;; riscv64) rpmarch=riscv64;; ppc64le) rpmarch=ppc64le;; *) echo "Unsupported RPM architecture: $ARCH" >&2; exit 1;; esac
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT HUP INT TERM
mkdir -p "$work/BUILD" "$work/BUILDROOT" "$work/RPMS" "$work/SOURCES" "$work/SPECS" "$work/SRPMS" "$work/audiotag-$VERSION" dist
GOOS=linux GOARCH=$ARCH GOARM=7 CGO_ENABLED=0 go build -buildvcs=false -trimpath -ldflags "-s -w -X main.version=$VERSION" -o "$work/audiotag-$VERSION/audiotag" ./cmd/audiotag
cp README.md TAGS.md BENCHMARKS.md CONTRIBUTING.md CLA.md LICENSE NOTICE Go-BSD-3-Clause.txt "$work/audiotag-$VERSION/"
cp -R examples "$work/audiotag-$VERSION/"
VERSION=$VERSION go run scripts/source.go --output "$work/audiotag-$VERSION/source.tar.gz"
cp cmd/audiotag/completions/* "$work/audiotag-$VERSION/"
tar -czf "$work/SOURCES/audiotag-$VERSION.tar.gz" -C "$work" "audiotag-$VERSION"
rpmbuild -bb --target "$rpmarch" --define "_topdir $work" --define "version $VERSION" packaging/rpm/audiotag.spec
find "$work/RPMS" -name '*.rpm' -exec cp '{}' dist/ \;
