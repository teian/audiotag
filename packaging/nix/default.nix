{ lib, buildGoLatestModule, version ? "1.0.0" }:
buildGoLatestModule {
  pname = "audiotag";
  inherit version;
  src = lib.cleanSourceWith {
    src = ../..;
    filter = path: type:
      let name = baseNameOf path; in
      !(builtins.elem name [ "dist" ".git" "result" ".venv" ]);
  };
  vendorHash = null;
  subPackages = [ "cmd/audiotag" ];
  env.CGO_ENABLED = "0";
  ldflags = [ "-s" "-w" "-X main.version=${version}" ];
  postInstall = ''
    install -Dm644 cmd/audiotag/completions/audiotag.bash $out/share/bash-completion/completions/audiotag
    install -Dm644 cmd/audiotag/completions/audiotag.zsh $out/share/zsh/site-functions/_audiotag
    install -Dm644 cmd/audiotag/completions/audiotag.fish $out/share/fish/vendor_completions.d/audiotag.fish
    install -Dm644 LICENSE $out/share/licenses/audiotag/LICENSE
    install -Dm644 NOTICE $out/share/licenses/audiotag/NOTICE
    install -Dm644 Go-BSD-3-Clause.txt $out/share/licenses/audiotag/Go-BSD-3-Clause.txt
    for doc in README.md TAGS.md BENCHMARKS.md CONTRIBUTING.md CLA.md; do
      install -Dm644 "$doc" "$out/share/doc/audiotag/$doc"
    done
    cp -R examples $out/share/doc/audiotag/
    VERSION=${lib.escapeShellArg version} go run scripts/source.go --output source.tar.gz
    install -Dm644 source.tar.gz $out/share/doc/audiotag/source.tar.gz
  '';
  meta = {
    description = "Native lossless audio and audiobook metadata CLI";
    license = lib.licenses.eupl12;
    platforms = lib.platforms.unix ++ lib.platforms.windows;
    mainProgram = "audiotag";
  };
}
