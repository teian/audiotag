# Supported tags by file format

This is the reference for AudioTag 1.0.0. The tables list every built-in text
mapping in the implementation. Custom tag names are also supported, so there
is no finite whitelist of all possible tags. Native JSON exposes metadata that
has no convenient text mapping; that does not imply a semantic editor for every
binary field or support for every metadata system a container can carry.

## Editing and naming

```sh
audiotag inspect book.m4b
audiotag inspect book.m4b --raw
audiotag set book.flac --set 'title=Chapter One' --set narrator='Jane Reader'
audiotag set book.opus --set-file lyrics=transcript.txt
audiotag export book.mp3 --output metadata.json
audiotag import book.mp3 --tags metadata.json
```

Ordinary text keys are case-insensitive and normalize to lowercase. `raw:` and
`riff:` prefixes and their native identifiers are case-sensitive. Aliases are:

| Accepted names | Normalized name |
|---|---|
| `album_artist`, `album artist` | `albumartist` |
| `track`, `track_number` | `tracknumber` |
| `totaltracks`, `track_total` | `tracktotal` |
| `disc`, `disc_number` | `discnumber` |
| `totaldiscs`, `disc_total` | `disctotal` |
| `year` | `date` |

Repeated `--set KEY=VALUE` arguments replace the key with a list of values.
`--remove KEY` removes the field. Native import **replaces** metadata; export
first and edit that document to retain unrelated fields. Native import requires
version 1 JSON and the exact format identifier reported by the export, including
ID3 version variants. Native `Data` is base64 in JSON; keep its structure and flags
when editing structured fields.

## MP3: ID3v2.3 and ID3v2.4

WAV, AIFF and AIFC use the same mappings in their embedded ID3 tags.

| Text key | Native ID3 frame |
|---|---|
| `title` | `TIT2` |
| `album` | `TALB` |
| `artist` | `TPE1` |
| `albumartist` | `TPE2` |
| `composer` | `TCOM` |
| `genre` | `TCON` |
| `date` | `TDRC` in v2.4; `TYER` in v2.3 |
| `tracknumber`, `tracktotal` | `TRCK`, number/total |
| `discnumber`, `disctotal` | `TPOS`, number/total |
| `language` | `TLAN` |
| `copyright` | `TCOP` |
| `publisher` | `TPUB` |
| `encodedby` | `TENC` |
| `conductor` | `TPE3` |
| `grouping` | `TIT1` |
| `subtitle` | `TIT3` |
| `bpm` | `TBPM` |
| `isrc` | `TSRC` |
| `titlesort` | `TSOT` |
| `albumsort` | `TSOA` |
| `artistsort` | `TSOP` |
| `comment` | `COMM` |
| `lyrics` | `USLT`, unsynchronized lyrics/transcript |
| Any other ordinary key | `TXXX`, description = normalized key |

`author`, `narrator`, `series`, `seriespart`, `isbn`, `asin`, `description`,
`longdescription`, `albumartistsort`, `composersort`, ReplayGain and MusicBrainz
keys therefore use `TXXX`; they are not aliases for other native frames.

Additional valid four-character text frame identifiers can be written with
`raw:T...`, for example `--set raw:TSO2='Author Name'`. The identifier must begin
with `T` and cannot be `TXXX`. Binary/structured frames such as `APIC`, `PRIV`,
`UFID`, `GEOB` and other unmapped frames use native JSON for
exact payload editing. POPM, WXXX, SYLT and ETCO have semantic editors below;
cover/chapter commands handle APIC, CHAP and CTOC. Unknown frames are retained. Flagged frames are exposed
as raw metadata rather than decoded text. Dedicated structured editors are listed below.

- New tags use v2.4; existing v2.3 tags retain their version.
- v2.4 ordinary text fields support multiple NUL-separated values. v2.3 ordinary
  multivalue writes are refused. The `raw:` route writes a text payload directly
  and should not be used to circumvent v2.3 compatibility restrictions.
- Comment and lyrics writes require one value and use language `eng` with an
  empty description. They replace matching normalized frames, including other
  language/description variants; use native JSON to retain those distinctions.
  The `language` tag does not change the `COMM`/`USLT` language bytes.
- Track/disc fields take one value, with each number in 0–65535. Setting a total
  retains the number; setting a number can include `/total`. Removing a total
  retains the number. Removing the number removes the combined frame.
- Cover editing uses `APIC`; chapters use `CHAP` and an ordered `CTOC` table,
  with at most 255 entries and 32-bit millisecond chapter times.
- ID3v2.2, extended headers, tag-level unsynchronization and footer tags are
  refused. MP3 ID3v1 editing is described below.

## M4A, M4B and MP4: iTunes `ilst`

M4A and M4B have identical tag mappings; M4B has no extra built-in text keys.
Here `©` denotes the native single-byte copyright character in a four-byte atom.

| Text key | Native atom |
|---|---|
| `title` | `©nam` |
| `album` | `©alb` |
| `artist` | `©ART` |
| `albumartist` | `aART` |
| `composer` | `©wrt` |
| `genre` | `©gen` |
| `date` | `©day` |
| `comment` | `©cmt` |
| `lyrics` | `©lyr` |
| `copyright` | `cprt` |
| `encodedby` | `©too` |
| `grouping` | `©grp` |
| `description` | `desc` |
| `longdescription` | `ldes` |
| `titlesort` | `sonm` |
| `albumsort` | `soal` |
| `artistsort` | `soar` |
| `albumartistsort` | `soaa` |
| `composersort` | `soco` |
| `tracknumber`, `tracktotal` | `trkn`, binary number/total pair |
| `discnumber`, `disctotal` | `disk`, binary number/total pair |
| Any other ordinary key | `----:com.apple.iTunes:KEY`, UTF-8 freeform data |

`author`, `narrator`, `language`, `publisher`, `subtitle`, `conductor`,
`isrc`, `series`, `seriespart`, `isbn` and `asin` are freeform fields here.
`bpm` writes typed `tmpo`; see the additional mappings below.

Text fields support multiple `data` atoms. Existing UTF-8 text (type 1) and
unsigned integer data (type 21, up to eight bytes) appear in text inspection.
Typed native atoms such as `tmpo`, `cpil`, `pgap`, `stik`, `rtng`, `pcst`,
`purd`, `purl` and `egid`, and unknown `ilst` items, can be retained/exported/
imported as native JSON; dedicated writable mappings are listed below.
Text writes produce UTF-8 data; typed fields retain their integer data type.

Explicit freeform namespaces can be written with, for example,
`--set 'raw:----:org.example:catalogue=123'`. Other `raw:` atom text writes are
refused; use native JSON for their typed payloads. Native four-byte atom keys
containing non-ASCII bytes use the export's key encoding; copy the exported key
rather than guessing its JSON spelling.

- Track/disc numbers and totals accept 0–65535. Setting one retains its partner;
  setting a number can include `/total`. **Removing either key removes the whole
  `trkn` or `disk` item**, including its partner.
- Cover editing uses JPEG/PNG `covr` data. Native JSON retains typed cover data.
- Chapter editing uses Nero `chpl`: at most 255 chapters and 255 UTF-8 bytes per
  title. Only start time and title are stored; ends are inferred on inspection.
- Existing QuickTime `text` chapter tracks can be read, replaced and cleared.
  Starts must begin at zero; ends meet the next start and fit the existing
  duration/timescale. Complex edit lists, multiple chapter tracks and other
  chapter codecs are refused. A new QuickTime track is not created.
  A Nero companion imposes its 255-entry/title-byte limits.
- Fragmented/indexed, encrypted and external-media layouts are refused. Arbitrary
  metadata outside `ilst` and the supported `chpl` item has no general editor.

## FLAC: Vorbis comments and metadata blocks

Every valid Vorbis comment key can be read, set and removed, with repeated UTF-8
values. Keys are nonempty printable ASCII bytes 0x20–0x7D excluding `=`; writes
use uppercase normalized names. No separate built-in field mapping is needed.
For example, `title` becomes `TITLE`, `albumartist` becomes `ALBUMARTIST`,
`tracknumber` becomes `TRACKNUMBER`, and `tracktotal` becomes `TRACKTOTAL`.
Track/disc totals are independent comments, not binary number/total pairs.

All names in the [shared text-key list](#shared-text-key-list) work as ordinary
comments, as do custom keys. Existing aliases normalize in inspection; spelling
conventions expected by other players can differ. `raw:` is not a special native
block interface for comments: use native JSON for metadata blocks.

- Covers use native PICTURE blocks (block type 6). Cover writes replace front
  covers (picture type 3) and retain other picture types.
- Chapters use `CHAPTER001=HH:MM:SS.mmm` and `CHAPTER001NAME=Title`, continuing
  sequentially. Writing replaces chapter comments; ends are inferred from the
  next start. Reading stops at the first missing index after index 0/1.
- Native JSON exposes metadata block payloads by decimal block-type key:
  STREAMINFO (0), PADDING (1), APPLICATION (2), SEEKTABLE (3), VORBIS_COMMENT (4),
  CUESHEET (5), PICTURE (6), and supported unknown block types. Cuesheets have a dedicated editor below; `chapters` still edits comments.
- STREAMINFO must remain unchanged during native import. Individual FLAC blocks
  cannot exceed 16 MiB minus one byte.

## Opus and Ogg Vorbis: comments

OpusTags and Vorbis comments have the same text-key rules, normalization,
multivalues, custom fields and chapter convention as FLAC comments. Every valid
comment key is supported, including all shared text keys.

Pictures are base64 FLAC picture payloads in `METADATA_BLOCK_PICTURE` comments;
cover editing replaces front covers and retains other picture types. Native JSON
exposes the comment entries, not arbitrary Ogg packets. There is no separate
semantic editor for legacy `COVERART`/`COVERARTMIME` conventions; they remain
ordinary comments. Multiplexed/chained Ogg and Ogg FLAC are refused.

## WAV: embedded ID3 and existing RIFF INFO

All [ID3 mappings](#mp3-id3v23-and-id3v24) apply. ID3 values take precedence over
legacy values during text inspection. These additional legacy mappings are
updated when their existing INFO list is present:

| Text key | RIFF INFO entry |
|---|---|
| `title` | `INAM` |
| `artist` | `IART` |
| `album` | `IPRD` |
| `comment` | `ICMT` |
| `genre` | `IGNR` |
| `date` | `ICRD` |
| `copyright` | `ICOP` |
| `encodedby` | `ISFT` |

Writes always update embedded ID3 as well. They do not create a missing INFO
list. Multiple values in legacy text are joined with `; `.
Unknown INFO entries appear as `riff:FOUR`, preserving the exact four-byte key.
They can be removed by that name; arbitrary INFO writes use native JSON rather
than `--set riff:FOUR=...`. The `@LIST` native payload includes the INFO structure.
BWF `bext` and iXML have dedicated editors below. Cue points and arbitrary
other chunks are retained without a semantic editor. RF64 and oversized IFF files are
refused; audio `data` and format chunks cannot be edited through native JSON.

## AIFF and AIFC: embedded ID3 and existing legacy text

All ID3 mappings apply, with the same precedence and structured metadata
support as WAV. These existing legacy chunks are also updated:

| Text key | AIFF/AIFC chunk |
|---|---|
| `title` | `NAME` |
| `artist` | `AUTH` |
| `comment` | `ANNO` |
| `copyright` | `(c) ` |

Missing legacy chunks are not created by text writes. Multiple legacy values
are joined with `; `. Native JSON can replace the exposed `@NAME`, `@AUTH`,
`@ANNO`, `@(c) ` and `@LIST` metadata chunks. Other chunks are retained without
an editing interface; audio `SSND` and format `COMM` are protected.

## APE, WavPack and Musepack: APEv2

All three formats use the same APEv2 item implementation. Every valid text key
is supported: 2–255 printable ASCII bytes 0x20–0x7E, excluding `=`. Ordinary keys
normalize to lowercase when written; item matching is case-insensitive. Multiple
values are stored as NUL-separated UTF-8 text in one item. Native import refuses
duplicate case-insensitive item names.

All shared text keys and arbitrary custom keys work as item names. There is no
special mapping to conventional `Track`, `Disc`, `Year`, or `Album Artist` item
names: normalized `tracknumber`, `discnumber`, `date`, and `albumartist` are written
literally. This matters for compatibility with readers that expect those other
names. Track/disc totals are independent text items.

- Native JSON exposes text, binary, external-reference and read-only item flags.
  Text inspection includes only valid UTF-8 text items; other item payloads use
  native JSON. Text removal/replacement and cover/chapter writes refuse affected
  read-only items. Native import replaces the complete item list, including flags.
- Covers use binary `Cover Art (Front)` with a filename, NUL separator and image
  bytes. Existing `Cover Art (...)` items can be inspected/extracted.
- Chapters use the same sequential `CHAPTERnnn`/`CHAPTERnnnNAME` convention as
  Vorbis comments. No separate APEv2 chapter structure is created.
- A trailing ID3v1 tag is preserved but not exposed as editable text.

## Shared text-key list

These portable text suggestions are available alongside the format-specific
keys documented below. They are
convenience names, not a whitelist and not a promise of player interoperability:

`album`, `albumartist`, `albumartistsort`, `albumsort`, `artist`, `artistsort`,
`asin`, `author`, `bpm`, `comment`, `composer`, `composersort`, `conductor`,
`copyright`, `date`, `description`, `discnumber`, `disctotal`, `encodedby`, `genre`,
`grouping`, `isbn`, `isrc`, `language`, `longdescription`, `lyrics`,
`musicbrainz_albumartistid`, `musicbrainz_albumid`, `musicbrainz_artistid`,
`musicbrainz_trackid`, `narrator`, `publisher`, `replaygain_album_gain`,
`replaygain_album_peak`, `replaygain_track_gain`, `replaygain_track_peak`, `series`,
`seriespart`, `subtitle`, `title`, `titlesort`, `tracknumber`, `tracktotal`.

For audiobooks, `artist` and `albumartist` usually carry the author alongside
explicit custom `author` and `narrator` fields. Choose player-compatible fields
for your library. See [README.md](README.md) for the audiobook workflow, covers,
chapters, text files, and native metadata import examples.

## Additional ID3 editors

| Keys | Frames / values |
|---|---|
| `artisturl`, `audiourl`, `sourceurl`, `commercialurl` | WOAR, WOAF, WOAS, WCOM |
| `copyrighturl`, `radiourl`, `paymenturl`, `publisherurl` | WCOP, WORS, WPAY, WPUB |
| `url:DESCRIPTION` | WXXX, Latin-1 URL |
| `comment:LANG:DESCRIPTION`, `lyrics:LANG:DESCRIPTION` | COMM / USLT; three-letter ASCII language; description preserved |
| `rating:EMAIL` | POPM rating, 0–255 |
| `playcount:EMAIL` | POPM unsigned 64-bit counter |
| `synchronizedlyrics` | SYLT millisecond timestamps, JSON or timestamped LRC input |
| `eventtiming` | ETCO JSON array of `{ "type": 2, "milliseconds": 1000 }` |

Qualified descriptions and POPM owners are case-sensitive. A qualified write
retains other variants. Rating and counter writes retain their partner; removing
either removes that owner's POPM frame. URLs and owners must fit Latin-1.

```sh
audiotag set book.mp3 --set 'comment:deu:Summary=Zusammenfassung' \
  --set 'rating:reader@example.org=200' --set 'playcount:reader@example.org=12'
audiotag set book.mp3 --set-file synchronizedlyrics=transcript.lrc
audiotag lyrics book.mp3 --output transcript.lrc
```

SYLT JSON contains `language`, `description`, `content_type` (0–8), and `entries`
with `milliseconds` and `text`. Entries must be ordered. LRC accepts timestamped
lines and multiple leading timestamps, using `eng` and content type 1; directives
such as offsets are unsupported. `synchronizedlyrics_lrc` is inspection-only.
A synchronized-lyrics write replaces all SYLT frames; use native JSON for multiple
independent variants. Frame-based native timestamps remain available as raw data.

### MP3 ID3v1 and synchronization

Editable keys are `id3v1.title`, `id3v1.artist`, `id3v1.album`, `id3v1.date`,
`id3v1.comment`, `id3v1.tracknumber`, and `id3v1.genre`. Removing `id3v1` removes
the footer. Genres accept standard names or numeric codes 0–255; tracks are
0–255. Legacy fields are Latin-1, generally 30 bytes, date 4 bytes and comment
28 bytes when a track number is present. Unsupported characters become `?`.

```sh
# Ordinary writes leave ID3v1 untouched.
audiotag set book.mp3 --set title='Full Unicode title'
# Synchronize shared fields, following the explicitly edited version.
audiotag set book.mp3 --set title='Full Unicode title' --sync
audiotag set book.mp3 --set id3v1.title='Legacy title' --sync
```

`--sync` is off by default and only valid for MP3 writes. Shared fields edited
only in ID3v1 copy into ID3v2; remaining shared fields copy into ID3v1. Conflicting
explicit edits to both versions fail before saving; agreement is checked against
the representable legacy value. Full ID3v2 values survive truncation/replacement
in the legacy copy. Unrecognized genre text becomes legacy code 255. Sync cannot
be combined with deleting the whole footer. This does not convert ID3v2 versions.

## Additional typed MP4 fields

| Key | Atom | Accepted values |
|---|---|---|
| `bpm` | tmpo | 0–65535 |
| `compilation`, `gapless`, `podcast` | cpil, pgap, pcst | true/false or 0/1 |
| `mediakind`, `advisory` | stik, rtng | 0–255 |
| `tvseason`, `tvepisode` | tvsn, tves | 0–4294967295 |
| `purchasedate`, `podcasturl`, `podcastguid` | purd, purl, egid | UTF-8 text |
| `tvshow`, `tvnetwork`, `tvepisodeid` | tvsh, tvnn, tven | UTF-8 text |
| `category`, `keywords` | catg, keyw | UTF-8 text |

Integer fields require one value and write native data type 21 with the correct
width. Values are stored directly; enumerated media-kind/advisory meanings depend
on the consuming player.

## FLAC cuesheets and seek tables

`cuesheet` accepts JSON or single-file .cue text through `--set-file`.
`seektable` accepts a JSON array of `{ "sample": 0, "offset": 0,
"frame_samples": 4096 }`. Offsets are relative to the first encoded audio frame;
they must be supplied from an existing index. AudioTag does not generate seek
points by decoding or scanning audio. Real samples must increase and lie within
the stream; placeholder sample 18446744073709551615 requires zero offset/frame
count and follows real entries.

```sh
audiotag set book.flac --set-file cuesheet=book.cue
audiotag cuesheet book.flac --output exported.cue
audiotag set book.flac --set-file seektable=seekpoints.json
```

Cuesheet JSON fields are `catalogue`, `lead_in_samples`, `compact_disc`, and
`tracks`. Tracks contain `offset_samples`, `number`, `isrc`, `data`,
`pre_emphasis`, and `indices` (`offset_samples`, `number`). Include a final
lead-out track matching total samples, numbered 170 for CD or 255 otherwise.
CD offsets use 588-sample alignment and require a two-second lead-in.

.cue imports accept FILE, TRACK AUDIO, CATALOG, ISRC, FLAGS PRE and INDEX.
TITLE/PERFORMER/SONGWRITER/REM/CDTEXTFILE are accepted but not stored: the native
FLAC cuesheet block cannot represent them. PREGAP/POSTGAP and multiple files are
refused. Times must map exactly to samples; 44.1-kHz imports use CD rules.
Export uses the placeholder filename `audio.flac`; adjust it as needed.
`cuesheettext` is inspection-only. Remove `cuesheet`/`seektable` to delete the
corresponding block. A same-named Vorbis comment requires native JSON.

## WAV broadcast metadata

| Keys | Representation |
|---|---|
| `bext.description` | ASCII, at most 256 bytes |
| `bext.originator`, `bext.originatorreference` | ASCII, at most 32 bytes |
| `bext.originationdate`, `bext.originationtime` | ASCII, at most 10 / 8 bytes |
| `bext.timereference` | Unsigned 64-bit sample count |
| `bext.version` | 0, 1 or 2 |
| `bext.umid` | 64 bytes as hexadecimal; requires version ≥1 |
| `bext.loudnessvalue`, `bext.loudnessrange`, `bext.maxtruepeaklevel`, `bext.maxmomentaryloudness`, `bext.maxshorttermloudness` | Hundredths, −327.68 to 327.66 or `unknown`; requires version 2 |
| `bext.codinghistory` | ASCII text |
| `ixml` | Complete XML document rooted at BWFXML |
| `ixml.PATH` | Case-insensitive dot-separated element path, for example `ixml.project` |

New BEXT chunks use version 2 and unknown loudness values. Removing a field
clears it; remove `bext` or `ixml` to delete the chunk. Set version explicitly
rather than removing it. XML leaf editing preserves other elements, attributes
and comments, although lexical XML formatting can change. Repeated leaves take
a matching number of values; structured parents cannot be replaced as text.
DTD/directives are refused. These editors are WAV-only, not AIFF.

```sh
audiotag set recording.wav --set bext.originator='Studio' \
  --set bext.timereference=4294967296 --set ixml.project='Audiobook'
```

ReplayGain fields across formats store supplied values only. No audio analysis,
ReplayGain calculation or external decoder is required.
