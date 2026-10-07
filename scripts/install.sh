#!/bin/sh
# Build and install a native binary; DESTDIR supports staged packaging.
set -eu
cd "$(dirname "$0")/.."
PREFIX=${PREFIX:-/usr/local}
DESTDIR=${DESTDIR:-}
VERSION=${VERSION:-1.0.0}
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT HUP INT TERM
CGO_ENABLED=0 go build -buildvcs=false -trimpath -ldflags "-s -w -X main.version=$VERSION" -o "$work/audiotag" ./cmd/audiotag
root=$DESTDIR$PREFIX
mkdir -p "$root/bin" "$root/share/bash-completion/completions" "$root/share/zsh/site-functions" "$root/share/fish/vendor_completions.d" "$root/share/licenses/audiotag" "$root/share/doc/audiotag"
install -m755 "$work/audiotag" "$root/bin/audiotag"
install -m644 cmd/audiotag/completions/audiotag.bash "$root/share/bash-completion/completions/audiotag"
install -m644 cmd/audiotag/completions/audiotag.zsh "$root/share/zsh/site-functions/_audiotag"
install -m644 cmd/audiotag/completions/audiotag.fish "$root/share/fish/vendor_completions.d/audiotag.fish"
install -m644 LICENSE "$root/share/licenses/audiotag/LICENSE"
install -m644 NOTICE "$root/share/licenses/audiotag/NOTICE"
install -m644 Go-BSD-3-Clause.txt "$root/share/licenses/audiotag/Go-BSD-3-Clause.txt"
for doc in README.md TAGS.md BENCHMARKS.md CONTRIBUTING.md CLA.md; do
 install -m644 "$doc" "$root/share/doc/audiotag/$doc"
done
cp -R examples "$root/share/doc/audiotag/"
GOOS=$(go env GOHOSTOS) GOARCH=$(go env GOHOSTARCH) VERSION=$VERSION go run scripts/source.go --output "$work/source.tar.gz"
install -m644 "$work/source.tar.gz" "$root/share/doc/audiotag/source.tar.gz"
