{ pkgs ? import <nixpkgs> {}, version ? "1.0.0" }:
pkgs.callPackage ./packaging/nix/default.nix { inherit version; }
