#!/bin/sh
# Build and smoke-test an APK in a disposable Alpine container.
set -eu
cd "$(dirname "$0")/.."
mkdir -p dist/ci dist/apk/keys
go run scripts/source.go --output dist/ci/alpine-source.tar.gz
docker run --rm \
 -e VERSION="${VERSION:-1.0.0}" -e SOURCE_DATE_EPOCH="${SOURCE_DATE_EPOCH:-0}" \
 -e GOTOOLCHAIN="$(go env GOVERSION)" \
 -v "$PWD/dist/ci/alpine-source.tar.gz:/source.tar.gz:ro" \
 -v "$PWD/dist/apk:/out" alpine:3.22 sh -eu -c '
 apk add --no-cache alpine-sdk go ffmpeg bash zsh fish
 adduser -D builder
 addgroup builder abuild
 mkdir /work
 tar -xzf /source.tar.gz -C /work --strip-components=1
 chown -R builder /work /out
 su builder -c "set -e; cd /work; abuild-keygen -a -n"
 cp /home/builder/.abuild/*.pub /etc/apk/keys/
 su builder -c "set -e; cd /work; GOTOOLCHAIN=$GOTOOLCHAIN VERSION=$VERSION SOURCE_DATE_EPOCH=$SOURCE_DATE_EPOCH sh scripts/package-apk.sh"
 cp -R /work/dist/apk/. /out/
 cp /home/builder/.abuild/*.pub /out/keys/
 apk add --no-network /work/dist/apk/local/*/audiotag-$VERSION-r0.apk
 audiotag formats
 test "$(audiotag version)" = "audiotag $VERSION"
 cmp /work/LICENSE /usr/share/licenses/audiotag/LICENSE
 test -f /usr/share/doc/audiotag/source.tar.gz
 '
