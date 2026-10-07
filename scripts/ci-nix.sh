#!/bin/sh
# Build and smoke-test the Nix derivation in a disposable official container.
set -eu
cd "$(dirname "$0")/.."
mkdir -p dist/ci
go run scripts/source.go --output dist/ci/nix-source.tar.gz
docker run --rm \
 -e VERSION="${VERSION:-1.0.0}" \
 -e SOURCE_DATE_EPOCH="${SOURCE_DATE_EPOCH:-0}" \
 -v "$PWD/dist/ci/nix-source.tar.gz:/source.tar.gz:ro" \
 -v "$PWD/dist:/out" nixos/nix:latest sh -eu -c '
 mkdir /work
 tar -xzf /source.tar.gz -C /work --strip-components=1
 nix-channel --add https://nixos.org/channels/nixpkgs-unstable nixpkgs
 nix-channel --update
 cd /work
 sh scripts/package-nix.sh
 store_path=$(nix-build --no-out-link --argstr version "$VERSION" default.nix)
 "$store_path/bin/audiotag" formats
 test "$("$store_path/bin/audiotag" version)" = "audiotag $VERSION"
 test "$(cat LICENSE)" = "$(cat "$store_path/share/licenses/audiotag/LICENSE")"
 test -f "$store_path/share/doc/audiotag/source.tar.gz"
 cp dist/audiotag.nixpkg /out/audiotag.nixpkg
 '
# A separate store exposes missing references that the build store would mask.
docker run --rm \
 -v "$PWD/dist/audiotag.nixpkg:/package.nixpkg:ro" \
 -v "$PWD/LICENSE:/LICENSE:ro" nixos/nix:latest sh -eu -c '
 imported_paths=$(nix-store --import < /package.nixpkg)
 found=no
 for store_path in $imported_paths; do
  if [ -x "$store_path/bin/audiotag" ]; then
   "$store_path/bin/audiotag" formats
   test "$(cat /LICENSE)" = "$(cat "$store_path/share/licenses/audiotag/LICENSE")"
   test -f "$store_path/share/doc/audiotag/source.tar.gz"
   found=yes
  fi
 done
 test "$found" = yes
 '
