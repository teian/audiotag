# Contributing to AudioTag

AudioTag is a Go-only audio metadata CLI with no external Go or runtime
dependencies. Contributions should preserve encoded audio, arbitrary native
metadata, safe staged writes, and support for the existing target platforms.

## Development

Install Go 1.22 or later. Clone your fork and create a branch for the change.
The normal build and checks are:

```sh
go run ./cmd/task build
go run ./cmd/task check
```

On Windows, build directly with:

```powershell
go build -buildvcs=false -o dist/audiotag.exe ./cmd/audiotag
go vet -buildvcs=false ./...
go test -buildvcs=false -race ./...
```

Race checks require a C compiler supported by Go's race detector. The shipped
CLI uses `CGO_ENABLED=0` and requires no C compiler or shared libraries.
Format changed Go files with `gofmt`; keep shell scripts POSIX-compatible
unless they are explicitly shell completion scripts.

Install FFmpeg and FFprobe to run real-container integration tests. Bash,
Zsh, and Fish enable the respective completion tests. Tests that need a
missing optional tool are skipped; CI installs the validation tools.

## Changes and validation

- Keep changes focused and explain the concrete behavior before and after.
- Add meaningful tests for parser changes, metadata preservation, unsupported
  layouts, and failure paths. A rejected write must leave the original intact.
- Preserve independent original/staged audio checks and native metadata
  verification. Stream audio rather than loading whole books into memory.
- Keep public exports and imports independent of caller-owned mutable data.
- Exercise long Unicode transcripts, covers, chapters, and unknown binary
  metadata when those paths change.
- Update CLI help, completion, examples, and README when the interface changes.
- Discuss new dependencies and unsupported format layouts in the pull request.

For performance changes, follow [BENCHMARKS.md](BENCHMARKS.md), use private
copies of audio, and report before/after measurements under the same conditions.
Distinguish allocation totals from peak RAM and include integrity verification
and filesystem sync in end-to-end timings.

`go run ./cmd/task release` cross-builds the complete release matrix. Packaging
commands and their build-time requirements are in [README.md](README.md).
Cross-compilation checks buildability; native runtime tests cover Linux,
macOS, and Windows.

## Pull requests and CLA

Read [CLA.md](CLA.md) before submitting a contribution. Accept CLA v1.0 using
the checkbox in the pull request template or the acceptance comment described
there. Each contributing author must accept for their own work. You retain
copyright and license the contributions under EUPL 1.2 only.

Include the reason for the change, relevant validation, and any limitations.
For third-party material, include its origin and licence and retain its
notices. Discuss bugs using small reproducible fixtures you can share; avoid
uploading audiobook content you do not have permission to distribute.

Maintainers check the CLA records for all authors, review third-party notices,
and require the relevant CI jobs to pass before merging. CLA review is manual.
Questions and bug reports that submit no material for inclusion do not require
CLA acceptance.

## Releases

See the release instructions in [README.md](README.md#github-actions).
Stable `vMAJOR.MINOR.PATCH` tags run the complete build and package suite before
publication. Keep the target inventory in `internal/release` aligned with the
package matrices. Publisher changes must preserve the draft verification gate,
retry behavior, and refusal to overwrite published assets; the release tests
exercise these failure cases without contacting GitHub.
