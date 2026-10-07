#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
command -v nix-build >/dev/null || { echo 'Install Nix and configure nixpkgs first' >&2; exit 1; }
mkdir -p dist
store_path=$(nix-build --no-out-link --argstr version "${VERSION:-1.0.0}" default.nix)
# Nix has no standard .nixpkg extension. This is a Nix store export stream.
# Import with: nix-store --import < dist/audiotag.nixpkg
closure_paths=$(nix-store --query --requisites "$store_path")
printf '%s\n' "$closure_paths" | xargs nix-store --export > dist/audiotag.nixpkg
printf 'Exported %s to dist/audiotag.nixpkg\n' "$store_path"
