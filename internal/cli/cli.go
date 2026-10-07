// Package cli implements the user-facing commands using only the Go standard library.
package cli

import (
	"audiotag/internal/tag"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const help = `audiotag — native, lossless audio metadata editing

Usage: audiotag COMMAND [OPTIONS] FILE_OR_DIRECTORY...

Commands:
  inspect       Print normalized tags, native metadata, covers, and chapters
  set           Set text tags (--set KEY=VALUE; repeat for multiple values)
  remove        Remove named tags (--remove KEY; repeat)
  export        Export all native metadata to JSON (exact-format round-trip)
  import        Replace native metadata from JSON (--tags FILE; one audio file)
  audiobook     Apply book config, Part folder identity, and Markdown transcript
  chapters      Read chapters; --chapters JSON replaces chapter markers
  lyrics        Export synchronized lyrics as LRC (write with --set-file)
  cuesheet      Export FLAC cuesheet text (write with --set-file cuesheet=FILE)
  cover         List covers; --cover JPEG/PNG replaces front cover
  formats       Print supported containers and limitations
  tag-keys      List common tag names (also used by shell completion)
  completion    Print bash, zsh, or fish completion script
  version       Print version

Options (may appear before or after filenames):
  --set KEY=VALUE        Text tag; unknown names become freeform tags
  --set-file KEY=FILE    Read UTF-8 tag text from file (lyrics/transcripts)
  --tags FILE           Text-tag JSON for set; native export JSON for import
  --remove KEY          Remove tag by normalized name or raw: native key
  --cover FILE          JPEG/PNG front cover (other pictures preserved)
  --chapters FILE       Chapter JSON array or render manifest
  --recursive           Recurse directories; skip hidden/build directories
  --dry-run             Validate and print intended changes without writing
  --sync                Sync edited ID3v1/ID3v2 fields (MP3 writes only; default off)
  --backup              Keep exclusive FILE.bak (never overwrite a backup)
  --json                Emit machine-readable JSON
  --raw                 Include native binary metadata in inspect JSON
  --progress MODE       auto (terminal only), always, or never; writes to stderr
  --no-progress         Disable write progress
  --output FILE         Export JSON or extract front cover (one audio file)
  --config FILE         Audiobook JSON, compatible with tools/kokoro.json
  --part-from SOURCE    auto (default), folder, title, or filename
  --transcript FILE     Markdown '# Chapter N: Title' or '## Chapter N — Title'
  --chapter-range N-M   Limit transcript; otherwise infer from audio filename
  --verify-audio        Accepted for scripts; payload verification is always on

Existing unrelated tags, pictures, chapter tracks, and encoded audio are preserved.
All writes are staged and verified before replacement; each file is a transaction.
`
const formats = `Container       Tags / artwork / chapters
FLAC            Arbitrary Vorbis comments, native blocks, cuesheets/seek tables, pictures; CHAPTER tags
Opus            Arbitrary OpusTags, picture comments; CHAPTER tags
Ogg Vorbis      Arbitrary comments, picture comments; CHAPTER tags
MP3             ID3v2.3/v2.4, ID3v1, qualified/structured fields, APIC, CHAP/CTOC
M4A/M4B/MP4     iTunes text/typed/freeform, covr, Nero/existing QuickTime chapters
WAV / AIFF      ID3/artwork/chapters; RIFF INFO / AIFF text; WAV BEXT/iXML
APE / WavPack /
Musepack        APEv2 text/multivalue/binary items, artwork; CHAPTER tags

No decoding, re-encoding, Python, FFmpeg, CGO, or runtime libraries are required.
Native JSON export/import exposes binary/structured fields that have no text mapping.
Native imports require the exact tag/container variant of the export.
Unsupported: ASF/WMA, raw AAC, RF64, ID3v2.2, extended/unsynchronized ID3 headers,
fragmented/encrypted MP4, multiplexed/chained Ogg, Ogg FLAC.
Existing QuickTime text chapter tracks can be edited; complex layouts are refused.
CHAPTER comments/chpl store start/title; chapter ends are inferred by readers.
`

type stringsFlag []string

func (s *stringsFlag) String() string     { return strings.Join(*s, ",") }
func (s *stringsFlag) Set(v string) error { *s = append(*s, v); return nil }

type options struct {
	sync, recursive, dry, backup, json, raw, verify, noProgress           bool
	sets, files, removes                                                  stringsFlag
	tags, cover, chapters, config, part, transcript, chapterRange, output string
	progress                                                              string
}

func parse(args []string, errout io.Writer) (options, []string, error) {
	o := options{}
	fs := flag.NewFlagSet("audiotag", flag.ContinueOnError)
	fs.SetOutput(errout)
	fs.BoolVar(&o.sync, "sync", false, "")
	fs.BoolVar(&o.recursive, "recursive", false, "")
	fs.BoolVar(&o.dry, "dry-run", false, "")
	fs.BoolVar(&o.backup, "backup", false, "")
	fs.BoolVar(&o.json, "json", false, "")
	fs.BoolVar(&o.raw, "raw", false, "")
	fs.BoolVar(&o.verify, "verify-audio", false, "")
	fs.BoolVar(&o.noProgress, "no-progress", false, "")
	fs.StringVar(&o.progress, "progress", "auto", "")
	fs.Var(&o.sets, "set", "")
	fs.Var(&o.files, "set-file", "")
	fs.Var(&o.removes, "remove", "")
	fs.StringVar(&o.tags, "tags", "", "")
	fs.StringVar(&o.cover, "cover", "", "")
	fs.StringVar(&o.chapters, "chapters", "", "")
	fs.StringVar(&o.config, "config", "", "")
	fs.StringVar(&o.part, "part-from", "auto", "")
	fs.StringVar(&o.transcript, "transcript", "", "")
	fs.StringVar(&o.chapterRange, "chapter-range", "", "")
	fs.StringVar(&o.output, "output", "", "")
	// Interspersed flags, without interpreting filenames after -- as options.
	var flags, paths []string
	for i := 0; i < len(args); i++ {
		s := args[i]
		if s == "--" {
			paths = append(paths, args[i+1:]...)
			break
		}
		if strings.HasPrefix(s, "-") {
			name := strings.TrimLeft(s, "-")
			name, _, equal := strings.Cut(name, "=")
			f := fs.Lookup(name)
			if f == nil {
				return o, nil, fmt.Errorf("unknown option %s", s)
			}
			flags = append(flags, s)
			if bf, ok := f.Value.(interface{ IsBoolFlag() bool }); (!ok || !bf.IsBoolFlag()) && !equal {
				if i+1 == len(args) {
					return o, nil, fmt.Errorf("missing value for %s", s)
				}
				i++
				flags = append(flags, args[i])
			}
		} else {
			paths = append(paths, s)
		}
	}
	e := fs.Parse(flags)
	return o, paths, e
}

var extensions = map[string]bool{".flac": true, ".mp3": true, ".m4a": true, ".m4b": true, ".mp4": true, ".opus": true, ".ogg": true, ".oga": true, ".wav": true, ".aif": true, ".aiff": true, ".aifc": true, ".ape": true, ".wv": true, ".mpc": true}

func collect(paths []string, recursive bool) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	add := func(p string) error {
		abs, e := filepath.Abs(p)
		if e != nil {
			return e
		}
		if !seen[abs] {
			seen[abs] = true
			out = append(out, abs)
		}
		return nil
	}
	for _, p := range paths {
		st, e := os.Lstat(p)
		if e != nil {
			return nil, e
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("symlink input refused: %s", p)
		}
		if st.IsDir() {
			e = filepath.WalkDir(p, func(path string, d os.DirEntry, e error) error {
				if e != nil {
					return e
				}
				if d.IsDir() {
					if path != p && (!recursive || strings.HasPrefix(d.Name(), ".")) {
						return filepath.SkipDir
					}
					return nil
				}
				if d.Type()&os.ModeSymlink != 0 {
					return nil
				}
				if extensions[strings.ToLower(filepath.Ext(path))] && !strings.Contains(d.Name(), ".tagging.") {
					return add(path)
				}
				return nil
			})
			if e != nil {
				return nil, e
			}
		} else if st.Mode().IsRegular() {
			if e = add(p); e != nil {
				return nil, e
			}
		} else {
			return nil, fmt.Errorf("not a regular file: %s", p)
		}
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil, errors.New("no audio files found")
	}
	return out, nil
}
func jsonOut(w io.Writer, v any) error {
	e := json.NewEncoder(w)
	e.SetIndent("", "  ")
	return e.Encode(v)
}
func readJSON(path string, v any) error {
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, tag.MaxMetadata+1))
	if e = d.Decode(v); e != nil {
		return e
	}
	var extra any
	if e = d.Decode(&extra); e != io.EOF {
		return errors.New("JSON must contain a single value")
	}
	return nil
}
func readText(path string) (string, error) {
	f, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, tag.MaxMetadata+1))
	if e != nil {
		return "", e
	}
	if len(b) > tag.MaxMetadata {
		return "", errors.New("text exceeds 64 MiB")
	}
	return string(b), nil
}
func assignments(o options) (map[string][]string, error) {
	m := map[string][]string{}
	if o.tags != "" {
		var raw map[string]json.RawMessage
		if e := readJSON(o.tags, &raw); e != nil {
			return nil, e
		}
		for k, b := range raw {
			var s string
			if json.Unmarshal(b, &s) == nil {
				m[k] = []string{s}
			} else {
				var values []string
				if e := json.Unmarshal(b, &values); e != nil || len(values) == 0 {
					return nil, fmt.Errorf("tag %s must be string or nonempty string array", k)
				}
				m[k] = values
			}
		}
	}
	groups := map[string]bool{}
	for _, a := range o.sets {
		k, v, ok := strings.Cut(a, "=")
		if !ok || k == "" {
			return nil, errors.New("--set requires KEY=VALUE")
		}
		if !groups[k] {
			m[k] = nil
			groups[k] = true
		}
		m[k] = append(m[k], v)
	}
	for _, a := range o.files {
		k, p, ok := strings.Cut(a, "=")
		if !ok || k == "" {
			return nil, errors.New("--set-file requires KEY=FILE")
		}
		s, e := readText(p)
		if e != nil {
			return nil, e
		}
		m[k] = []string{s}
	}
	return m, nil
}
func chapterJSON(path string) ([]tag.Chapter, error) {
	var raw json.RawMessage
	if e := readJSON(path, &raw); e != nil {
		return nil, e
	}
	var chapters []tag.Chapter
	if len(raw) > 0 && raw[0] == '[' {
		if e := json.Unmarshal(raw, &chapters); e != nil {
			return nil, e
		}
	} else {
		var manifest struct {
			Chapters []tag.Chapter `json:"chapters"`
		}
		if e := json.Unmarshal(raw, &manifest); e != nil {
			return nil, e
		}
		if manifest.Chapters == nil {
			return nil, errors.New("manifest must contain chapters")
		}
		chapters = manifest.Chapters
	}
	return chapters, nil
}
func picture(path string) (tag.Picture, error) {
	f, e := os.Open(path)
	if e != nil {
		return tag.Picture{}, e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, 16<<20+1))
	if e != nil {
		return tag.Picture{}, e
	}
	if len(b) > 16<<20 {
		return tag.Picture{}, errors.New("cover exceeds 16 MiB")
	}
	mime := ""
	if bytes.HasPrefix(b, []byte{137, 'P', 'N', 'G', 13, 10, 26, 10}) {
		mime = "image/png"
	} else if bytes.HasPrefix(b, []byte{255, 216, 255}) {
		mime = "image/jpeg"
	} else {
		return tag.Picture{}, errors.New("cover must be JPEG or PNG")
	}
	return tag.Picture{MIME: mime, Type: 3, Data: b}, nil
}
func exclusiveOutput(path string, write func(io.Writer) error) error {
	f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if e != nil {
		return e
	}
	e = write(f)
	closeErr := f.Close()
	if e == nil {
		e = closeErr
	}
	if e != nil {
		os.Remove(path)
	}
	return e
}
func Run(args []string, out, errout io.Writer) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		_, e := io.WriteString(out, help)
		return e
	}
	cmd := args[0]
	for _, s := range args[1:] {
		if s == "--help" || s == "-h" {
			_, e := io.WriteString(out, help)
			return e
		}
	}
	if cmd == "formats" {
		_, e := io.WriteString(out, formats)
		return e
	}
	if cmd == "tag-keys" {
		if len(args) != 1 {
			return errors.New("usage: audiotag tag-keys")
		}
		for _, key := range tag.Keys() {
			if _, e := fmt.Fprintln(out, key); e != nil {
				return e
			}
		}
		return nil
	}
	if cmd == "doctor" {
		_, e := io.WriteString(out, "Native Go backend; no runtime dependencies. All saves verify audio payloads.\n")
		return e
	}
	switch cmd {
	case "inspect", "set", "remove", "export", "import", "audiobook", "chapters", "cover", "cuesheet", "lyrics":
	default:
		return fmt.Errorf("unknown command %q", cmd)
	}
	o, paths, e := parse(args[1:], errout)
	if e != nil {
		return e
	}
	if o.progress != "auto" && o.progress != "always" && o.progress != "never" {
		return errors.New("--progress must be auto, always, or never")
	}
	if len(paths) == 0 {
		return errors.New("provide audio files or directories")
	}
	files, e := collect(paths, o.recursive)
	if e != nil {
		return e
	}
	if (cmd == "export" || cmd == "import" || o.output != "") && len(files) != 1 {
		return errors.New("export/import/--output require exactly one audio file")
	}
	if o.output != "" && cmd != "export" && cmd != "cover" && cmd != "cuesheet" && cmd != "lyrics" {
		return errors.New("--output is supported by export, cover, cuesheet and lyrics")
	}
	if o.output != "" && o.cover != "" {
		return errors.New("cover --output extracts; do not combine with --cover")
	}
	if o.output != "" && o.dry {
		return errors.New("--dry-run cannot be combined with --output")
	}
	if cmd != "audiobook" && (o.config != "" || o.transcript != "" || o.chapterRange != "" || o.part != "auto") {
		return errors.New("book config, transcript, chapter range, and part authority require audiobook")
	}
	if cmd == "import" && o.tags == "" {
		return errors.New("import requires --tags JSON")
	}
	if cmd == "audiobook" && o.config == "" {
		return errors.New("audiobook requires --config JSON")
	}
	if o.part != "auto" && o.part != "folder" && o.part != "title" && o.part != "filename" {
		return errors.New("--part-from must be auto, folder, title, or filename")
	}
	mutating := cmd == "set" || cmd == "remove" || cmd == "import" || cmd == "audiobook" || cmd == "chapters" && o.chapters != "" || cmd == "cover" && o.cover != ""
	if !mutating && (len(o.sets) > 0 || len(o.files) > 0 || len(o.removes) > 0 || o.tags != "" || o.cover != "" || o.chapters != "" || o.config != "" || o.transcript != "" || o.chapterRange != "") {
		return errors.New("write options require a modifying command")
	}
	var tags map[string][]string
	var snap tag.Snapshot
	if cmd == "import" {
		if e = readJSON(o.tags, &snap); e != nil {
			return e
		}
	} else {
		tags, e = assignments(o)
		if e != nil {
			return e
		}
	}
	if cmd == "set" && len(tags) == 0 && len(o.removes) == 0 && o.cover == "" && o.chapters == "" && !o.sync {
		return errors.New("set needs --set, --set-file, --tags, --remove, --cover, --chapters, or --sync")
	}
	if cmd == "remove" && len(o.removes) == 0 {
		return errors.New("remove needs --remove KEY")
	}
	var book map[string]json.RawMessage
	if cmd == "audiobook" {
		if e = readJSON(o.config, &book); e != nil {
			return e
		}
	}
	var ch []tag.Chapter
	if o.chapters != "" {
		ch, e = chapterJSON(o.chapters)
		if e != nil {
			return e
		}
	}
	var pic tag.Picture
	if o.cover != "" {
		pic, e = picture(o.cover)
		if e != nil {
			return e
		}
	}
	// Preflight every file, including format-specific validation, before any commit.
	type plan struct {
		f      *tag.File
		values map[string][]string
	}
	plans := make([]plan, 0, len(files))
	defer func() {
		for _, p := range plans {
			p.f.Close()
		}
	}()
	for _, path := range files {
		f, e := tag.Open(path)
		if e != nil {
			return e
		}
		plans = append(plans, plan{f: f})
		values := map[string][]string{}
		if cmd == "audiobook" {
			values, e = bookTags(book, o, path, f.Tags())
			if e != nil {
				return fmt.Errorf("%s: %w", path, e)
			}
		}
		for k, v := range tags {
			values[k] = v
		}
		for _, k := range o.removes {
			if e = f.Remove(k); e != nil {
				return e
			}
		}
		if cmd == "import" {
			if e = f.Import(snap); e != nil {
				return e
			}
		}
		keys := make([]string, 0, len(values))
		for k := range values {
			keys = append(keys, k)
		}
		sort.Strings(keys) // number before total, independent of JSON object iteration.
		for _, k := range keys {
			if e = f.Set(k, values[k]); e != nil {
				return fmt.Errorf("%s (%s): %w", path, k, e)
			}
		}
		if o.cover != "" {
			if e = f.Cover(pic); e != nil {
				return e
			}
		}
		if o.chapters != "" {
			if e = f.SetChapters(ch); e != nil {
				return e
			}
		}
		if o.sync {
			if !mutating {
				return errors.New("--sync requires a write command")
			}
			changed := append([]string(nil), keys...)
			changed = append(changed, o.removes...)
			if e = f.SyncTags(changed); e != nil {
				return e
			}
		}
		plans[len(plans)-1].values = values
	}
	var results []any
	for index, p := range plans {
		f := p.f
		path := f.Path
		switch cmd {
		case "lyrics":
			text := f.Tags()["synchronizedlyrics_lrc"]
			if len(text) != 1 {
				return errors.New("file has no millisecond synchronized lyrics")
			}
			if o.output != "" {
				return exclusiveOutput(o.output, func(w io.Writer) error { _, err := io.WriteString(w, text[0]); return err })
			}
			if _, err := io.WriteString(out, text[0]); err != nil {
				return err
			}
		case "cuesheet":
			text := f.Tags()["cuesheettext"]
			if len(text) != 1 {
				return errors.New("file has no text-exportable FLAC cuesheet")
			}
			if o.output != "" {
				return exclusiveOutput(o.output, func(w io.Writer) error { _, err := io.WriteString(w, text[0]); return err })
			}
			if _, err := io.WriteString(out, text[0]); err != nil {
				return err
			}
		case "export":
			if o.output != "" {
				return exclusiveOutput(o.output, func(w io.Writer) error { return jsonOut(w, f.Export()) })
			}
			return jsonOut(out, f.Export())
		case "inspect":
			pics, e := f.Pictures()
			if e != nil {
				return e
			}
			chapters, chapterErr := f.Chapters()
			if !o.json && !o.raw {
				if index > 0 {
					if _, e := fmt.Fprintln(out); e != nil {
						return e
					}
				}
				if e := inspectText(out, f, pics, chapters, chapterErr); e != nil {
					return e
				}
				continue
			}
			summary := []any{}
			for _, p := range pics {
				summary = append(summary, map[string]any{"type": p.Type, "mime": p.MIME, "bytes": len(p.Data), "description": p.Description})
			}
			r := map[string]any{"path": path, "format": f.Format(), "tags": f.Tags(), "pictures": summary, "chapters": chapters}
			if chapterErr != nil {
				r["chapter_note"] = chapterErr.Error()
			}
			if o.raw {
				r["raw"] = f.Export().Raw
			}
			results = append(results, r)
		case "chapters":
			if !mutating {
				c, e := f.Chapters()
				if e != nil {
					return e
				}
				if !o.json {
					if e := chaptersText(out, path, c); e != nil {
						return e
					}
					continue
				}
				results = append(results, map[string]any{"path": path, "chapters": c})
				continue
			}
			fallthrough
		case "set", "remove", "import", "audiobook":
			if o.dry {
				r := map[string]any{"path": path, "dry_run": true, "tags": f.Tags()}
				if o.chapters != "" {
					r["chapters"] = ch
				}
				if o.cover != "" {
					r["cover"] = o.cover
				}
				results = append(results, r)
			} else {
				if e = saveFile(f, o, errout, index+1, len(plans)); e != nil {
					return fmt.Errorf("%s: %w", path, e)
				}
				results = append(results, map[string]any{"path": path, "status": "tagged", "audio_verified": true})
			}
		case "cover":
			if mutating {
				if o.dry {
					results = append(results, map[string]any{"path": path, "dry_run": true, "cover": o.cover})
				} else {
					if e = saveFile(f, o, errout, index+1, len(plans)); e != nil {
						return e
					}
					results = append(results, map[string]any{"path": path, "status": "tagged", "audio_verified": true})
				}
			} else {
				pics, e := f.Pictures()
				if e != nil {
					return e
				}
				if o.output != "" {
					for _, p := range pics {
						if p.Type == 3 {
							return exclusiveOutput(o.output, func(w io.Writer) error { _, e := w.Write(p.Data); return e })
						}
					}
					return errors.New("no front cover found")
				}
				if !o.json {
					if e := coversText(out, path, pics); e != nil {
						return e
					}
					continue
				}
				summary := []any{}
				for _, p := range pics {
					summary = append(summary, map[string]any{"type": p.Type, "mime": p.MIME, "bytes": len(p.Data)})
				}
				results = append(results, map[string]any{"path": path, "pictures": summary})
			}
		}
	}
	if o.json || o.raw && cmd == "inspect" || o.dry {
		return jsonOut(out, results)
	}
	for _, r := range results {
		m := r.(map[string]any)
		fmt.Fprintf(out, "Tagged and verified %s\n", m["path"])
	}
	return nil
}

var partRE = regexp.MustCompile(`(?i)\bPart\s+(\d+)\b`)
var rangeRE = regexp.MustCompile(`(\d+)-(\d+)`)
var headingRE = regexp.MustCompile(`(?m)^#{1,6}\s+Chapter\s+(\d+)\s*(?::|—|–|-)\s*(.+?)\r?$`)

func partNumber(s string) (int, error) {
	m := partRE.FindStringSubmatch(s)
	if m == nil {
		return 0, errors.New("no positive Part N in selected authority")
	}
	n, e := strconv.Atoi(m[1])
	if e != nil || n <= 0 {
		return 0, errors.New("invalid Part number")
	}
	return n, nil
}

var filenamePartRE = regexp.MustCompile(`(?i)^part[ _-]?(\d+)(?:[ _.:-]|$)`)

func filenamePart(path string) (int, error) {
	m := filenamePartRE.FindStringSubmatch(filepath.Base(path))
	if m == nil {
		return 0, errors.New("filename must start with partN or Part N")
	}
	n, e := strconv.Atoi(m[1])
	if e != nil || n <= 0 {
		return 0, errors.New("invalid filename Part number")
	}
	return n, nil
}
func partIdentity(path, authority string, current map[string][]string) (string, int, int, error) {
	folder := ""
	for p := filepath.Dir(path); ; p = filepath.Dir(p) {
		if _, e := partNumber(filepath.Base(p)); e == nil {
			folder = p
			break
		}
		if filepath.Dir(p) == p {
			break
		}
	}
	title := ""
	if len(current["title"]) > 0 {
		title = current["title"][0]
	}
	if authority == "auto" {
		if folder != "" {
			authority = "folder"
		} else if _, e := partNumber(title); e == nil {
			authority = "title"
		} else {
			authority = "filename"
		}
	}
	var n int
	var e error
	switch authority {
	case "folder":
		if folder == "" {
			return "", 0, 0, errors.New("no Part N folder above audio file")
		}
		title = filepath.Base(folder)
		n, e = partNumber(title)
	case "title":
		n, e = partNumber(title)
	case "filename":
		n, e = filenamePart(path)
		if title == "" {
			title = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		}
	default:
		return "", 0, 0, errors.New("unknown part authority")
	}
	if e != nil {
		return "", 0, 0, e
	}
	parent := filepath.Dir(path)
	if folder != "" {
		parent = filepath.Dir(folder)
	}
	entries, e := os.ReadDir(parent)
	if e != nil {
		return "", 0, 0, e
	}
	total := n
	for _, d := range entries {
		var x int
		var err error
		if folder != "" {
			if !d.IsDir() {
				continue
			}
			x, err = partNumber(d.Name())
		} else {
			if d.IsDir() || !extensions[strings.ToLower(filepath.Ext(d.Name()))] {
				continue
			}
			x, err = filenamePart(d.Name())
		}
		if err == nil && x > total {
			total = x
		}
	}
	return title, n, total, nil
}

func bookTags(cfg map[string]json.RawMessage, o options, path string, current map[string][]string) (map[string][]string, error) {
	get := func(k string) string { var s string; json.Unmarshal(cfg[k], &s); return s }
	m := map[string][]string{}
	for _, k := range []string{"album", "author", "narrator", "genre", "language", "date", "publisher", "copyright"} {
		if s := get(k); s != "" {
			m[k] = []string{s}
		}
	}
	if get("album") == "" {
		return nil, errors.New("book config needs an album string")
	}
	author := get("author")
	if author == "" && len(current["author"]) > 0 {
		author = current["author"][0]
	}
	if author != "" {
		m["author"] = []string{author}
		m["artist"] = []string{author}
		m["albumartist"] = []string{author}
	}
	if s := get("narrator"); s != "" {
		m["composer"] = []string{s}
	}
	if get("language") == "" {
		m["language"] = []string{"eng"}
	}
	title, n, total, e := partIdentity(path, o.part, current)
	if e != nil {
		return nil, e
	}
	m["title"] = []string{title}
	m["tracknumber"] = []string{strconv.Itoa(n)}
	m["tracktotal"] = []string{strconv.Itoa(total)}
	source := o.transcript
	first, last := 0, 0
	r := o.chapterRange
	if r == "" {
		r = rangeRE.FindString(filepath.Base(path))
	}
	if r != "" {
		a, b, ok := strings.Cut(r, "-")
		if !ok {
			return nil, errors.New("chapter range must be N-M")
		}
		first, e = strconv.Atoi(a)
		if e != nil {
			return nil, e
		}
		last, e = strconv.Atoi(b)
		if e != nil || first <= 0 || last < first {
			return nil, errors.New("invalid chapter range")
		}
	}
	root := filepath.Dir(o.config)
	if filepath.Base(root) == "tools" {
		root = filepath.Dir(root)
	}
	if source == "" && first > 0 {
		source, e = manifestSource(root, first, last)
		if e != nil {
			return nil, e
		}
	}
	if source == "" {
		source = get("source")
		if source == "" {
			source = get("source_template")
			source = strings.ReplaceAll(source, "{n}", strconv.Itoa(n))
			if strings.ContainsAny(source, "{}") {
				return nil, errors.New("source_template only supports {n}; use --transcript for other templates")
			}
		}
		if source != "" && !filepath.IsAbs(source) {
			source = filepath.Join(root, source)
		}
	}
	if source != "" {
		text, e := readText(source)
		if e != nil {
			return nil, e
		}
		lyrics, e := transcript(text, first, last)
		if e != nil {
			return nil, e
		}
		m["lyrics"] = []string{lyrics}
	}
	return m, nil
}
func manifestSource(root string, first, last int) (string, error) {
	var matches []string
	e := filepath.WalkDir(root, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			if path != root && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".render.json") {
			return nil
		}
		var m struct {
			Source   string `json:"source"`
			Chapters []struct {
				Number int `json:"number"`
			} `json:"chapters"`
		}
		if e = readJSON(path, &m); e != nil {
			return fmt.Errorf("manifest %s: %w", path, e)
		}
		if len(m.Chapters) > 0 && m.Chapters[0].Number <= first && m.Chapters[len(m.Chapters)-1].Number >= last {
			local := filepath.Join(filepath.Dir(path), filepath.Base(m.Source))
			if st, e := os.Stat(local); e == nil && st.Mode().IsRegular() {
				matches = append(matches, local)
			} else {
				source := m.Source
				if !filepath.IsAbs(source) {
					source = filepath.Join(root, source)
				}
				matches = append(matches, source)
			}
		}
		return nil
	})
	if e != nil {
		return "", e
	}
	unique := map[string]bool{}
	for _, s := range matches {
		unique[s] = true
	}
	if len(unique) > 1 {
		return "", errors.New("multiple transcript sources match chapter range; use --transcript")
	}
	for s := range unique {
		return s, nil
	}
	return "", nil
}
func transcript(text string, first, last int) (string, error) {
	matches := headingRE.FindAllStringSubmatchIndex(text, -1)
	var result []string
	seen := map[int]bool{}
	for i, m := range matches {
		n, e := strconv.Atoi(text[m[2]:m[3]])
		if e != nil {
			return "", e
		}
		if first > 0 && (n < first || n > last) {
			continue
		}
		if seen[n] {
			return "", fmt.Errorf("duplicate Chapter %d", n)
		}
		seen[n] = true
		end := len(text)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		title := strings.TrimSpace(text[m[4]:m[5]])
		heading := fmt.Sprintf("Chapter %d: %s", n, title)
		if !strings.ContainsAny(heading[len(heading)-1:], ".!?") {
			heading += "."
		}
		result = append(result, heading+"\n\n"+strings.TrimSpace(text[m[1]:end]))
	}
	if len(result) == 0 {
		return "", errors.New("no matching Markdown chapter headings")
	}
	if first > 0 {
		for n := first; n <= last; n++ {
			if !seen[n] {
				return "", fmt.Errorf("missing Chapter %d in transcript", n)
			}
		}
	}
	return strings.Join(result, "\n\n") + "\n", nil
}
