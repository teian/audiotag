# Tagging performance

Measured on 2026-10-07 with Go 1.26.8, Linux amd64, Ryzen 7 5800X,
32 GiB RAM, and an NVMe-backed Btrfs working directory. These are warm-cache
measurements, not cold-disk estimates. Each case restores a private working
copy, then measures open, edit, staged write, fsync, independent original and
staged audio hashing, metadata verification, and replacement. Restore and
explicit pre-run GC are outside the timer. No source audiobook was modified.

The audiobook workload changes title and ten attribution/organization tags
and installs approximately 1 MiB of Unicode transcript text. Table entries are
medians of five measured iterations after one warm-up. Allocation totals are
Go bytes allocated per operation, **not peak resident memory**.

| File | Size | Before | After | Allocated before | Allocated after |
| --- | ---: | ---: | ---: | ---: | ---: |
| FLAC | 257 MiB | 563.6 ms | 545.4 ms | 18.9 MiB | 8.4 MiB |
| MP3 | 27.5 MiB | 63.3 ms | 60.6 ms | 16.0 MiB | 10.1 MiB |
| M4B | 145 MiB | 345.9 ms | 476.7 ms | 122.7 MiB | 24.9 MiB |
| Opus | 62.5 MiB | 939.8 ms | 226.4 ms | 488.5 MiB | 18.2 MiB |

Opus shows a substantial, repeatable improvement: the full CLI audiobook
workload measured 951.6 → 228.7 ms; forced progress measured 942.6 → 227.2 ms.
Do not interpret the small FLAC/MP3 timing differences as established speedups.
M4B timings are unstable: its initial optimized run regressed, with the progress
phase report showing fsync increasing from 55.5 to 221.7 ms. A nine-iteration
repeat measured 485.2 ms without progress and 329.1 ms with progress; CLI cases
measured 481.7 and 339.1 ms respectively. These paths perform the same tagging
and validation. The allocation reduction is clear; an M4B wall-clock speedup
is **not established**, and the observed regressions remain a limitation of
these measurements. A final sequential control (five iterations, progress)
measured 344.5 → 316.8 ms, with fsync back at 51.5 ms. Filesystem sync
time is included rather than hidden.

## Changes supported by profiling

- Ogg CRC consumed approximately 63% of the original Opus CPU profile. Pure-Go
  slicing-by-eight CRC raises the 64 KiB checksum microbenchmark from roughly
  536 MB/s to 2406 MB/s. Incremental header/body checks avoid concatenation.
- Buffered Ogg scanning reuses bounded page storage; buffered writing reduces
  small writes. All page CRCs and stream-layout checks remain enabled.
- Vorbis comment encoding allocates its exact output size once. FLAC defers
  comment serialization until write/export, rather than repeating it for every
  assignment. The FLAC metadata microbenchmark fell from about 4.35 ms and
  26.8 MB allocated to 0.40 ms and 2.33 MB.
- MP4 parsing copies leaf payloads rather than every enclosing container.
  Serialization writes nested atoms into one pre-sized output buffer.
- Native metadata verification compares ordered keys, flags, and binary values
  directly instead of creating two base64 JSON documents. Public native
  snapshots still own their data.

Checks include race tests, vet, real FFmpeg/FFprobe round-trips across nine
formats, binary metadata preservation, backups, audio packet hashes, CRC
comparison against a bitwise reference, and damaged/truncated Ogg pages.
No dependencies were added, and no integrity checks were removed.

## Reproduce

Use a working directory on the filesystem being measured, with enough room
for the largest file, its staged replacement, and optional backup. The runner
creates `working.<extension>`, `transcript.txt`, and `.bak` there: reserve that
directory for benchmarking. Input files must be outside this working directory.

```sh
go build -buildvcs=false -o dist/benchmark scripts/benchmark.go
./dist/benchmark --iterations 5 --warmup 1 \
  --work dist/bench/work --output dist/bench/results.json \
  /path/to/book.flac /path/to/book.mp3 /path/to/book.m4b /path/to/book.opus

go run ./cmd/task build
./dist/benchmark --cli "$PWD/dist/audiotag" \
  --modes book,progress --output dist/bench/cli.json /path/to/book.opus

AUDIOTAG_BENCH_OPUS=/path/to/book.opus \
  go test -buildvcs=false ./internal/tag -run '^$' \
  -bench 'Benchmark(Ogg|Comments|FLAC)' -benchmem -count 3
```

Modes: `title`, `book`, `progress`, and `backup`. API progress counts callbacks;
CLI progress also exercises the actual reporter. CLI allocation totals are not
available. JSON includes samples, median times, allocation totals, and API
progress phase timings. Throughput divides original input size by complete
operation time; it is not physical disk bandwidth.

Local baseline binaries, immutable baseline source, profiles, fixture copies,
and raw reports are retained under ignored `dist/bench/`. Microbenchmarks
isolate CPU/allocation effects; they do not replace end-to-end filesystem tests.


## Further allocation reductions

A second pass compared preserved binaries from the first optimization against
this implementation, using the same files and audiobook workload, five runs
and one warm-up per case. Full audio and metadata verification remain enabled.

| Format | Previously allocated | Now allocated | Reduction | Median time before → after |
| --- | ---: | ---: | ---: | ---: |
| FLAC | 8.4 MiB | 3.7 MiB | 56% | 550.2 → 554.5 ms |
| MP3 | 10.1 MiB | 5.6 MiB | 44% | 59.9 → 59.3 ms |
| M4B | 24.9 MiB | 10.1 MiB | 60% | 320.6 → 318.5 ms |
| Opus | 18.2 MiB | 9.0 MiB | 50% | 301.3 → 300.0 ms |

Allocation totals fell substantially; elapsed times in this matched run were
essentially unchanged. As above, allocation totals do not establish peak RSS.
The earlier Opus speedup persists, but filesystem sync variability means its
absolute end-to-end timings differ between runs.

The MP4 metadata allocation profile initially attributed about 73% of allocation
space to atom encoding and byte cloning. The metadata-only benchmark fell from
18.85 MB and 1302 allocations to 7.41 MB and 762 allocations per operation.
This benchmark opens the M4B fixture, edits tags and large lyrics, serializes
metadata while copying audio to a discard writer, and reads native metadata;
it excludes fsync and staged integrity verification.

Additional changes:

- Internal native metadata views avoid copies during verification. `Export`
  makes the deep copy at the public boundary, and import retains independent
  ownership of caller data.
- Parsers use views of internally owned input buffers. Standalone parser and
  import paths retain their copying behavior where ownership requires it.
  Views can retain a complete metadata buffer until the file model is released;
  this trades fewer allocations for possible retention of unused bytes.
- MP4 field inspection avoids copying payloads. Chunk offsets are adjusted in a
  separately encoded buffer, then that buffer is written directly. The editable
  atom model remains unchanged, including across repeated writes.
- ID3 encoding allocates one exact-sized output buffer. Opus comment encoding
  writes directly into its packet buffer; assembling large Ogg header packets
  uses geometric buffer growth instead of repeated slice growth.
- Track/disc setters decode only the relevant fields, avoiding full lyric
  decoding after a large transcript has already been assigned.

Tests exercise snapshot mutation and import ownership for all nine integration
formats, paired fields with large transcripts in ID3v2.3/v2.4 and MP4, and
repeatable writes without modifying native metadata or the MP4 offset model.
Race tests and vet pass. Reports, profiles, and preserved comparison binaries
are in `dist/bench/allocation-pass/`.

For the metadata allocation profile:

```sh
AUDIOTAG_BENCH_M4B=/path/to/book.m4b \
  go test -buildvcs=false ./internal/tag -run '^$' \
  -bench BenchmarkMP4BookMetadata -benchmem \
  -memprofile dist/mp4.mem -o dist/mp4.test
go tool pprof -alloc_space dist/mp4.test dist/mp4.mem
```
