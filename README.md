# AudioTag

[![License: EUPL 1.2](https://img.shields.io/badge/license-EUPL--1.2-blue)](LICENSE)

A standalone audio and audiobook tagging CLI, written entirely in Go. **Zero
external Go dependencies, zero runtime dependencies, no CGO.** It edits metadata
without decoding, remuxing, or re-encoding audio. Audio payloads are streamed, so
multi-hour books do not have to fit in memory.

- Audiobook workflows for authors, narrators, parts, tracks, and transcripts.
- Text and custom tags, artwork, chapters, and native binary metadata export/import.
- Compact inspection, JSON output, write progress, and Bash/Zsh/Fish completion.
- Verified staged writes with optional backups and independent audio hashes.
- Static release builds for Linux, BSD, macOS, and Windows.

Start with [build and run](#build-and-run), explore the
[CLI examples](#tags-and-supported-formats), or read
[CONTRIBUTING.md](CONTRIBUTING.md) to contribute.

## Build and run

Go 1.22 or later:

```sh
go build -buildvcs=false -o dist/audiotag ./cmd/audiotag
./dist/audiotag --help
./dist/audiotag formats
```

The binary runs without Go, FFmpeg, Python, or any shared metadata library.

## Tags and supported formats

See [the complete tag reference](TAGS.md) for every built-in mapping, custom
fields, native metadata access, and restrictions for each format.

| Container | Text / custom tags | Structured / binary metadata | Chapters |
|---|---|---|---|
| FLAC | Arbitrary Vorbis comments, repeated values | Native blocks, pictures, editable cuesheets and seek tables | CHAPTER comments |
| Opus | Arbitrary OpusTags, repeated values | Picture comments | CHAPTER comments |
| Ogg Vorbis | Arbitrary comments, repeated values | Picture comments | CHAPTER comments |
| MP3 | ID3v2.3/v2.4, TXXX, explicit ID3v1 fields | Qualified comments/lyrics, URLs, ratings/counters, synchronized lyrics, native frames | CHAP / CTOC |
| M4A / M4B / MP4 | iTunes text, typed fields and arbitrary freeform fields | Native `ilst` items and `covr` | Nero `chpl`; existing QuickTime text tracks |
| WAV | ID3 and existing RIFF INFO | Native ID3, BEXT fields and iXML leaf paths/XML | ID3 CHAP / CTOC |
| AIFF / AIFC | ID3 and existing legacy text | Native ID3 frames and legacy text chunks | ID3 CHAP / CTOC |
| APE / WavPack / Musepack | APEv2 text and repeated values | APEv2 binary items and cover art | CHAPTER comments |

Common names include `title`, `album`, `artist`, `albumartist`, `author`,
`narrator`, `composer`, `genre`, `language`, `date`, `lyrics`, `comment`,
`tracknumber`, `tracktotal`, `discnumber`, `disctotal`, `copyright`, and
`publisher`. Unknown names become TXXX on ID3, freeform iTunes fields on MP4,
and ordinary comment/item keys on Vorbis/APEv2. Format-specific native text
frames can be addressed with `raw:TIT2`. Use native JSON for unmapped binary atoms and structured frames. Typed MP4 fields, qualified ID3
fields, FLAC cuesheets/seek tables and WAV broadcast metadata have dedicated
editors; see [TAGS.md](TAGS.md). Normalized names are conveniences,
not a restriction to a fixed tag whitelist.

“All tags” means native metadata is exposed and retained, including fields with
no normalized text representation. It does not mean every container supports
every field, or that every player recognizes custom fields. FLAC STREAMINFO
cannot be changed by metadata import, since it describes the encoded audio.

```sh
audiotag inspect book.m4b --raw
audiotag set book.flac --set 'title=Part 1 — Beginning' \
  --set 'artist=Author' --set 'artist=Co-author' \
  --set tracknumber=1 --set tracktotal=4
audiotag set book.opus --set-file lyrics=transcript.txt --cover cover.jpg
audiotag remove book.mp3 --remove comment --remove 'raw:PRIV'
audiotag set --tags examples/tags.json --recursive ./book
```

Repeated `--set KEY=VALUE` values replace that key with a multivalue list.
`--set-file` reads the full UTF-8 text, including newlines. Shell arguments are
never executed. ID3v2.3 is retained on existing files, so multivalues that
would require an ambiguous v2.3 encoding are refused; ID3v2.4 is used on
new tags. Arbitrary RIFF INFO chunks are available through native JSON import.

## Format-specific editing

MP3 legacy synchronization is **off by default**. Edit `id3v1.*` fields explicitly,
or add `--sync` to synchronize shared fields using whichever version you edit.
Conflicting edits to both versions fail before saving. Full ID3v2 values remain
intact; the legacy copy truncates long values and replaces unsupported characters
with `?`.

```sh
audiotag set book.mp3 --set title='Full Unicode title' --sync
audiotag set book.mp3 --set id3v1.title='Legacy title' --sync
audiotag set book.mp3 --set 'comment:deu:Summary=Zusammenfassung' \
  --set 'rating:reader@example.org=200'
audiotag set book.mp3 --set-file synchronizedlyrics=transcript.lrc
audiotag lyrics book.mp3 --output transcript.lrc
```

MP4 supports typed fields such as `bpm`, `compilation`, `gapless`, `mediakind`,
`advisory`, `podcast`, `tvseason` and `tvepisode`, alongside podcast and TV text
fields. WAV supports BEXT broadcast fields and full iXML or individual XML leaf
paths. FLAC cuesheets accept structured JSON or single-file .cue text; seek tables
accept supplied sample/frame offsets as JSON.

```sh
audiotag set book.m4b --set mediakind=2 --set gapless=true
audiotag set recording.wav --set bext.originator='Studio' --set ixml.project='Book'
audiotag set book.flac --set-file cuesheet=book.cue
audiotag cuesheet book.flac --output exported.cue
audiotag set book.flac --set-file seektable=seekpoints.json
```

ReplayGain editing stores supplied values without calculating them. Cuesheet
export uses `audio.flac` as a placeholder filename; .cue titles and performer
fields cannot be stored in the native FLAC cuesheet block. See [TAGS.md](TAGS.md)
for field types, limits and supported LRC/.cue syntax.

## Inspection and progress

`inspect` defaults to a compact text view: ordered tag names, shortened long
values, transcript character/line counts, artwork details, and the first three
chapters. `chapters` prints the full chapter list and `cover` lists pictures.
Use `--json` for complete, untruncated metadata in scripts. `inspect --raw`
continues to emit native metadata JSON, with or without `--json`.

```sh
audiotag inspect book.m4b
audiotag inspect book.m4b --json
audiotag chapters book.m4b --json
```

Write progress automatically appears on stderr when it is a terminal. It shows
file position in the batch, the current phase (original audio check, staged
write, verification, backup, replacement), bytes processed, and elapsed time.
Progress keeps refreshing during slow operations; it does not guess a percentage.
`--json` and redirected stderr are quiet by default. Force progress into logs
with `--progress always`, or disable it with `--progress never` / `--no-progress`.
Progress never enters stdout, and dry-runs do not start write progress.

```sh
audiotag set book.flac --set title=Opening --progress always
audiotag set book.flac --set title=Opening --json --no-progress
```

## Audiobook workflow

The workflow follows the conventions in the Nextcloud audiobook scripts:

* Book config assigns album, author to artist/albumartist, narrator to
  composer/narrator, plus genre and language (defaults to `eng`). Extra rendering
  settings in a `kokoro.json` are accepted and ignored by tagging. Album-only
  renderer configs are accepted; absent author/narrator fields preserve existing
  attribution. Stormwind-style `source_template` supports `{n}` part substitution.
* Automatic part detection prefers the nearest `Part N ...` folder, which
  supplies title and track number. For flat books such as Stormwind, it uses
  the existing Part title or a `part1_stormwind` filename. The highest
  sibling Part folder or flat audio filename number supplies `tracktotal`,
  including gaps in numbering.
* `--part-from title` reads Part N from the existing title, while still deriving
  the total from sibling Part folders or flat filenames. `--part-from folder`
  and `--part-from filename` select those authorities explicitly.
* Markdown headings `# Chapter N: Title` and `## Chapter N — Title` become
  full transcripts in `lyrics`. A chapter range comes from `Chapter 155-200`
  in the filename, or `--chapter-range 155-200`.
* A matching `.render.json` supplies its source Markdown if available, with the
  same local-basename fallback as the Python workflow. Otherwise config `source`
  is resolved relative to the book root (the parent of `tools/`). Use
  `--transcript` to override. Ambiguous sources or missing requested chapters
  are rejected.
* Recursive discovery skips hidden/build directories and `.tagging.` files.
  Other formats in the same book receive the same metadata.

```sh
# Preview first; this does not change files.
audiotag audiobook --config ./tools/kokoro.json --recursive --dry-run .

# Apply book identity and available transcripts to all supported audio copies.
audiotag audiobook --config ./tools/kokoro.json --recursive --backup .

# Explicit transcript and chapter manifest for a particular part.
audiotag audiobook --config ./tools/kokoro.json \
  --transcript ./book.md --chapter-range 155-200 \
  --chapters './Part 1/Chapter 155-200.render.json' \
  --cover cover.jpg './Part 1/flac/Chapter 155-200.flac'
```

Config examples are in [examples/book.json](examples/book.json). CUE files and
render manifests are read as inputs and are not rewritten; the tagger does not
render audio, resize pictures, or detect silence. Embedded art uses the supplied
JPEG/PNG exactly. Replacing the front cover preserves other picture types,
except MP4, whose `covr` list has no picture-type classification and is replaced.

## Chapters, covers, and lossless metadata export

```sh
audiotag chapters book.mp3
audiotag chapters book.m4b --chapters examples/chapters.json
audiotag cover book.flac --cover cover.png
audiotag cover book.m4a --output extracted-cover.jpg

audiotag export book.flac --output metadata.json
audiotag import book.flac --tags metadata.json
```

Native export contains a versioned format identifier, normalized text for
inspection, and native `raw` records with base64 binary data. Import uses the
`raw` records, replacing metadata for that format; editing the inspection-only
`tags` object in a native export has no effect. Use `set --tags` with a plain
text-tag object for portable changes across formats. Exports/imports require
one audio file and the same container/tag version. Export and cover extraction
never overwrite an existing output file.

Chapter JSON accepts an array of objects with `start_seconds`, `end_seconds`,
and `title`, or a render manifest with a `chapters` array. Starts must increase.
CHAPTER comments and MP4 chpl store starts and titles, with ends inferred from
the next chapter; the final end is not stored. ID3 stores millisecond starts
and ends. MP4 chpl has a maximum of 255 chapters and 255 UTF-8 bytes per title;
ID3 CTOC is limited to 255 entries. Existing QuickTime text chapter tracks can
be read, replaced and cleared.
Their existing timescale and duration are retained; complex edits and other
chapter codecs are refused. Files without a chapter track use Nero chpl.

## Integrity and limits

Every edit creates a temporary file beside the original, preserves file mode,
and reads it back before replacing the original. SHA-256 checks cover encoded
audio bytes; Ogg also checks lacing, granules, and page CRCs; MP4 updates chunk
offsets if metadata growth moves `mdat`. Native metadata must also round-trip
exactly. Verification is always enabled (`--verify-audio` is accepted for script
compatibility). `--backup` creates an exclusive `.bak` before replacement.

All requested files are parsed and changes validated before the first commit.
Each file is its own transaction; a later I/O failure may leave earlier files
successfully tagged. Changed inputs, symlinks, and Unix multiply-linked files are refused. Replacement uses
`os.Rename`; Go does not promise atomic rename on non-Unix systems. Ownership,
ACLs, xattrs, and creation times are not copied. Metadata is limited to 64 MiB;
FLAC block limits and 16 MiB cover input limits also apply.

Unsupported inputs fail explicitly: ASF/WMA, raw AAC, RF64/WAVE64, ID3v2.2,
ID3 headers with unsynchronization/extended/footer flags, fragmented/indexed or
encrypted MP4, Ogg FLAC, chained/multiplexed Ogg, and Ogg layouts with audio on
the final header page. FLAC chapter editing uses comments independently of
native cuesheets. This is a new implementation: the generated-fixture tests below are its validation
scope, not a claim of compatibility with every encoder/player.

## Shell completion

```sh
# Bash: load immediately, or install in bash-completion's completions directory.
source <(audiotag completion bash)

# Zsh: put this file on $fpath and run compinit.
audiotag completion zsh > ~/.zsh/completions/_audiotag

# Fish:
audiotag completion fish > ~/.config/fish/completions/audiotag.fish
```

Packagers and `go run ./cmd/task install` install all three completion files. All shells
suggest common tag names for `--set`, `--set-file`, and `--remove`, including
`author`, `narrator`, `lyrics`, `tracktotal`, and custom audiobook fields such as
`isbn`. Assignments insert `KEY=`; `--set-file` then completes the filename.
Bash also handles its split `=` tokens and filenames containing spaces. Unknown
keys remain accepted. Suggestions come from `audiotag tag-keys`, so they stay in
sync with the executable. Progress modes and Part authorities are completed too.

```text
audiotag set book.flac --set nar<TAB>        → narrator=
audiotag set book.flac --set-file ly<TAB>    → lyrics=
audiotag remove book.flac --remove track<TAB>
```

## Cross-compilation and releases

The standard-library-only Go task runner replaces Make. Run it from the project
root; `go run ./cmd/task help` lists commands. Build, test, check, release,
installation, cleanup, and DEB packaging work without a POSIX shell. Plain
`go build ./cmd/audiotag`, `go test ./...`, and `go vet ./...` also work.
`go run ./cmd/task clean` removes `dist/`.

```sh
go run ./cmd/task build
go run ./cmd/task test
go run ./cmd/task check
go run ./cmd/task release
# Limit builds or change version:
VERSION=1.0.0 TARGETS='linux/amd64 darwin/arm64 windows/amd64 freebsd/amd64' \
  go run ./cmd/task release
```

The default matrix builds Linux (amd64, arm64, ARMv7, 386, riscv64, ppc64le),
FreeBSD/OpenBSD/NetBSD (amd64, arm64), DragonFly BSD (amd64), macOS (amd64,
arm64), and Windows (amd64, arm64, 386). `CGO_ENABLED=0` produces standalone
binaries without platform C compilers. Archives and `SHA256SUMS` go to `dist/`.
Archives have deterministic timestamps; set `SOURCE_DATE_EPOCH` to override.
Cross-compilation proves buildability; runtime validation requires the target
OS. CI tests Linux, macOS, and Windows and cross-builds the matrix.

## Packaging

```sh
# Optional: PREFIX=/usr DESTDIR=/staging go run ./cmd/task install
go run ./cmd/task package deb      # Go-only DEB builder; ARCH=arm64 supported
go run ./cmd/task package rpm      # rpmbuild and POSIX shell; ARCH=arm64 supported
go run ./cmd/task package apk      # Alpine, abuild-enabled user and POSIX shell
go run ./cmd/task package nix      # Nix, configured nixpkgs and POSIX shell
```

DEB/RPM packages contain static Go binaries and need no runtime dependencies.
The DEB builder uses Go's archive libraries and does not require dpkg-deb.
Alpine provides an APKBUILD and a script that creates a local source tarball,
computes its checksum, and invokes `abuild -r`. Configure the abuild signing key
and build group first, as for any local Alpine package. Packaging tools are
build-time requirements only.

Nix's normal interface is `nix-build default.nix` or
`nix-env -if default.nix`. **`.nixpkg` is not a standard Nix package format.**
The Nix script exports the result as a Nix store export stream named
`dist/audiotag.nixpkg`, importable with
`nix-store --import < dist/audiotag.nixpkg`. The derivation in
`packaging/nix/default.nix` is the reusable package definition.

## Tests and implementation references

`go test ./...` runs parser, binary preservation, audiobook workflow, and
failure-safety tests. With FFmpeg/FFprobe installed it also generates real
FLAC, MP3, M4A, fast-start M4B, Opus, Vorbis, WAV, AIFF, and WavPack fixtures;
it checks independent packet hashes, decode validity, chapters, large Unicode
transcripts, covers, multivalues, native JSON round-trips, and backups.
FFmpeg is not shipped or called by the CLI. Fuzz targets cover ID3, MP4 atoms,
and Vorbis comment parsers.

Format references: [FLAC RFC 9639](https://www.rfc-editor.org/rfc/rfc9639.html),
[Ogg RFC 3533](https://www.rfc-editor.org/rfc/rfc3533.html),
[Opus RFC 7845](https://www.rfc-editor.org/rfc/rfc7845.html),
[ID3v2.4 structure](https://id3.org/id3v2.4.0-structure), and
[QuickTime file format](https://developer.apple.com/documentation/quicktime-file-format).

## GitHub Actions

[Build and test](.github/workflows/ci.yml) runs on pushes, pull requests, and
manual dispatch. It checks Go formatting and vet, runs race tests on Linux,
macOS, and Windows, and validates real containers with FFmpeg/FFprobe. Linux
also tests the minimum Go 1.22 version and all three shell completions.

After tests pass, CI cross-builds all 18 release targets, verifies archive
checksums, builds DEB/RPM/APK/Nix packages, and uploads downloadable artifacts.
Alpine and Nix packages are smoke-tested in disposable containers, including a
fresh-store import of the Nix export. DEB and RPM packages cover all six Linux
architectures; APK and Nix exports currently target x86_64.
Actions use the latest stable releases verified on 2026-10-07: Checkout 7.0.1,
Setup Go 7.0.0, Upload Artifact 7.0.1, and Download Artifact 8.0.1. They are pinned
to full commit hashes; weekly Dependabot updates keep those pins current.
Build jobs need only read access. No secrets are needed for pull-request builds.
Production builds and native tests select the latest stable Go through Setup Go
(`stable`, currently Go 1.27.1). Alpine uses that same toolchain; Nix uses
Nixpkgs' `buildGoLatestModule`. The additional Go 1.22 test checks minimum-version
compatibility.

[Publish release](.github/workflows/release.yml) runs when a stable
`vMAJOR.MINOR.PATCH` tag is pushed. It runs the full CI suite against the exact
tagged commit, stages all 18 binary archives, corresponding source, six DEBs,
six RPMs, an Alpine APK and public signing key, a Nix store export, and legal
notices. A manifest records the version, commit and asset hashes; `SHA256SUMS`
covers every asset including the manifest.

To release a committed version containing these workflows:

```sh
git tag -a v1.0.0 -m 'AudioTag 1.0.0'
git push origin v1.0.0
```

Only the publishing job receives `contents: write` through `GITHUB_TOKEN`.
It uploads to a draft, verifies the complete remote inventory and server-side
SHA-256 digests, then publishes with generated release notes. Failed uploads
leave a draft: rerun the failed job to reuse its artifacts, or manually dispatch
the workflow with the existing tag to rebuild. Published assets are never
overwritten; a rerun succeeds only if all assets still match. Rebuilds can
produce different package signatures, so use a new tag for changed artifacts.
Prerelease tags are rejected until package-specific prerelease versions are
supported. The tag must already exist; the workflow does not create tags.

The standard-library-only publisher also supports local staging without GitHub
access (the output directory must not already exist):

```sh
go run scripts/publish-release.go --input dist/release-input \
  --output dist/release-assets --version 1.0.0 --commit FULL_COMMIT_SHA
```

The container packaging checks can also be run locally with Docker and Go:

```sh
sh scripts/ci-alpine.sh
sh scripts/ci-nix.sh
```

APK artifacts include the public key needed to verify the locally built
package; private signing keys stay inside the disposable build container.

### Reproducible builds: assessment

The release archives already use `CGO_ENABLED=0`, `-trimpath`, disabled VCS
stamping, an empty build ID, sorted entries, normalized ownership, and
`SOURCE_DATE_EPOCH`. The release workflow derives the epoch from the tagged
commit. The source and DEB packers also normalize timestamps and ownership.

A local two-directory rebuild on 2026-10-07 with identical sources, Go
1.26.8-X:nodwarf5 and epoch 1791350000 matched all 18 binary archives, the source
archive, and the amd64 DEB byte for byte. The amd64 RPM differed; its build-time
header changed between builds. This checks repeatability on one machine and
toolchain, not independent-host reproducibility or signed packages.

To make reproducibility a release guarantee, add these controls:

- Resolve the latest stable Go once per release, record its exact version and
  build environment, and use that version for every build and verifier. Keep
  testing against `stable` while freezing the inputs of each published release.
- Pin package-builder container digests and the Nixpkgs revision. Moving
  `nixpkgs-unstable`, `nixos/nix:latest`, and package repositories currently make
  the build environment change over time.
- Normalize RPM payload timestamps, build host, build time, compression, and
  distro macros. RPM provides `use_source_date_epoch_as_buildtime` and
  `build_mtime_policy` controls; normalize the source tar as well.
- Compare unsigned APK payloads separately from signatures. The current fresh
  signing key changes both the APK and the published public key on every run.
  A persistent release signing key would need secret storage and a defined
  signing process.
- Rebuild archives and DEBs in isolated environments with separate caches,
  different checkout paths and times; compare hashes before publication.
  Add package-specific verification after locking their build environments.

Start by gating binary archives and DEBs, then extend the guarantee to RPM,
APK, and the Nix export. No additional application dependencies are needed.
The assessment follows [Go's reproducibility guidance](https://go.dev/blog/rebuild),
[Nixpkgs pinning guidance](https://nix.dev/tutorials/first-steps/towards-reproducibility-pinning-nixpkgs),
and [RPM's build configuration](https://rpm.org/docs/6.0.x/man/rpmbuild-config.5).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development checks, preservation
requirements, performance benchmarks, and pull request guidance. Contributors
must accept [CLA v1.0](CLA.md) for their submitted work. Acceptance is recorded
in the pull request and reviewed manually for each contributing author.
Contributors retain copyright and grant permissions under EUPL 1.2.

## License

Copyright (c) 2026 AudioTag contributors. Licensed under the European Union
Public Licence **version 1.2 only**, SPDX identifier `EUPL-1.2`. See
[LICENSE](LICENSE) for the full text and [NOTICE](NOTICE) for the project notice.
The [Go runtime and standard-library notice](Go-BSD-3-Clause.txt) is included
with compiled binaries and applies to those Go components.
The licence text comes from the
[European Commission's official EUPL publication](https://interoperable-europe.ec.europa.eu/collection/eupl/eupl-text-eupl-12).

Release archives contain the matching source in `source.tar.gz`; installed
packages contain it under `share/doc/audiotag/source.tar.gz`. A separate source
archive is also included among CI release artifacts. The source includes the
Go code, tests, examples, packaging recipes, and build scripts, with no external
Go dependencies. Preserve the licence and attribution notices when distributing.

## Performance

See [BENCHMARKS.md](BENCHMARKS.md) for reproducible warm-cache audiobook
benchmarks, allocation measurements, optimizations, and filesystem timing
limitations. `scripts/benchmark.go` measures verified writes on private copies;
its optional `--cli` mode includes CLI and progress-reporting overhead.
