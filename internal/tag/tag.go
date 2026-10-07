// Package tag edits metadata without decoding or re-encoding audio.
package tag

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"unicode/utf8"
)

const MaxMetadata = 64 << 20

var be = binary.BigEndian
var le = binary.LittleEndian

// Raw holds native metadata. Data is base64 in JSON, preserving binary fields.
type Raw struct {
	Key   string `json:"key"`
	Flags uint32 `json:"flags,omitempty"`
	Data  []byte `json:"data"`
}
type Chapter struct {
	Start float64 `json:"start_seconds"`
	End   float64 `json:"end_seconds,omitempty"`
	Title string  `json:"title"`
}
type Picture struct {
	MIME        string `json:"mime"`
	Type        uint32 `json:"type"`
	Description string `json:"description,omitempty"`
	Data        []byte `json:"data"`
}
type Snapshot struct {
	Version int                 `json:"version"`
	Format  string              `json:"format"`
	Tags    map[string][]string `json:"tags"`
	Raw     []Raw               `json:"raw"`
}

type engine interface {
	format() string
	// raw returns internal read-only views. Export clones at the public boundary.
	raw() []Raw
	replace([]Raw) error
	tags() map[string][]string
	set(string, []string) error
	remove(string) error
	write(io.Writer) error
	audio(hash.Hash) error
	pictures() ([]Picture, error)
	cover(Picture) error
	chapters() ([]Chapter, error)
	setChapters([]Chapter) error
}
type File struct {
	Path string
	f    *os.File
	e    engine
}

func Open(path string) (*File, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	fail := func(err error) (*File, error) { f.Close(); return nil, fmt.Errorf("%s: %w", path, err) }
	var head [12]byte
	n, _ := f.ReadAt(head[:], 0)
	var eng engine
	switch {
	case n >= 4 && string(head[:4]) == "fLaC":
		eng, e = openFLAC(f)
	case n >= 4 && string(head[:4]) == "OggS":
		eng, e = openOgg(f)
	case n >= 8 && string(head[4:8]) == "ftyp":
		eng, e = openMP4(f)
	case n >= 12 && (string(head[:4]) == "RIFF" && string(head[8:12]) == "WAVE" || string(head[:4]) == "FORM" && (string(head[8:12]) == "AIFF" || string(head[8:12]) == "AIFC")):
		eng, e = openIFF(f)
	case n >= 3 && string(head[:3]) == "ID3" || strings.EqualFold(filepath.Ext(path), ".mp3"):
		eng, e = openMP3(f)
	default:
		ext := strings.ToLower(filepath.Ext(path))
		switch ext {
		case ".ape", ".wv", ".mpc":
			eng, e = openAPE(f)
		default:
			e = errors.New("unsupported container (use formats for supported types)")
		}
	}
	if e != nil {
		return fail(e)
	}
	return &File{path, f, eng}, nil
}
func (f *File) Close() error              { return f.f.Close() }
func (f *File) Format() string            { return f.e.format() }
func (f *File) Tags() map[string][]string { return f.e.tags() }
func (f *File) Set(k string, v []string) error {
	if k == "" || !utf8.ValidString(k) || strings.ContainsRune(k, 0) {
		return errors.New("tag key must not be empty")
	}
	if len(v) == 0 {
		return errors.New("tag needs at least one value")
	}
	for _, s := range v {
		if !utf8.ValidString(s) {
			return errors.New("tag text must be UTF-8")
		}
		if strings.ContainsRune(s, 0) {
			return errors.New("text tag contains NUL")
		}
	}
	return f.e.set(k, v)
}
func (f *File) Remove(k string) error { return f.e.remove(k) }
func (f *File) Export() Snapshot      { return Snapshot{1, f.Format(), f.Tags(), cloneRaw(f.e.raw())} }
func (f *File) Import(s Snapshot) error {
	if s.Version != 1 || s.Format != f.Format() {
		return errors.New("native import requires version 1 and matching format; use --tags for portable text tags")
	}
	return f.e.replace(s.Raw)
}
func (f *File) Pictures() ([]Picture, error) { return f.e.pictures() }
func (f *File) Cover(p Picture) error        { return f.e.cover(p) }
func (f *File) Chapters() ([]Chapter, error) { return f.e.chapters() }
func (f *File) SetChapters(c []Chapter) error {
	for i, x := range c {
		if x.Start < 0 || x.Start != x.Start || x.Start > 1e10 || x.End < 0 || x.End != x.End || x.End > 1e10 || x.End != 0 && x.End <= x.Start || i > 0 && x.Start <= c[i-1].Start {
			return errors.New("chapter times must be finite, nonnegative, increasing; ends must follow starts")
		}
		if !utf8.ValidString(x.Title) || strings.ContainsRune(x.Title, 0) {
			return errors.New("chapter title must be UTF-8 without NUL")
		}
		if len(x.Title) > 65535 {
			return errors.New("chapter title too long")
		}
	}
	return f.e.setChapters(c)
}
func (f *File) AudioHash() ([]byte, error) {
	h := sha256.New()
	if e := f.e.audio(h); e != nil {
		return nil, e
	}
	return h.Sum(nil), nil
}

// Save stages a same-directory file, checks the encoded audio payload, then replaces
// the original. Hard links and symlinks are refused to avoid surprising replacements.
func (f *File) Save(backup bool) (err error) {
	return f.SaveWithProgress(backup, nil)
}

// Progress describes the current save phase and the bytes processed in that phase.
// Totals vary with container metadata; no estimated percentage is reported.
type Progress struct {
	Stage string
	Bytes int64
}

type progressWriter struct {
	io.Writer
	stage  string
	bytes  int64
	report func(Progress)
}

func (w *progressWriter) Write(b []byte) (int, error) {
	n, err := w.Writer.Write(b)
	w.bytes += int64(n)
	w.report(Progress{w.stage, w.bytes})
	return n, err
}

type progressHash struct {
	hash.Hash
	counter *progressWriter
}

func (h *progressHash) Write(b []byte) (int, error) { return h.counter.Write(b) }

// SaveWithProgress calls report synchronously; callbacks must not modify the file.
// A nil callback has the same behavior and cost as Save.
func (f *File) SaveWithProgress(backup bool, report func(Progress)) (err error) {
	stage := func(name string) {
		if report != nil {
			report(Progress{Stage: name})
		}
	}
	writer := func(w io.Writer, name string) io.Writer {
		stage(name)
		if report == nil {
			return w
		}
		return &progressWriter{Writer: w, stage: name, report: report}
	}
	hashAudio := func(file *File, name string) ([]byte, error) {
		h := sha256.New()
		w := writer(h, name)
		var digest hash.Hash = h
		if report != nil {
			digest = &progressHash{Hash: h, counter: w.(*progressWriter)}
		}
		if err := file.e.audio(digest); err != nil {
			return nil, err
		}
		return h.Sum(nil), nil
	}
	st, err := os.Lstat(f.Path)
	if err != nil {
		return err
	}
	if hasMultipleLinks(st) {
		return errors.New("refusing multiply-linked file")
	}
	if !st.Mode().IsRegular() {
		return errors.New("refusing nonregular file or symlink")
	}
	opened, err := f.f.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(st, opened) {
		return errors.New("file replaced since opening")
	}
	before, err := hashAudio(f, "Checking original audio")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(f.Path), ".audiotag-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	defer tmp.Close()
	if err = tmp.Chmod(st.Mode().Perm()); err != nil {
		return err
	}
	if err = f.e.write(writer(tmp, "Writing metadata")); err != nil {
		return err
	}
	stage("Syncing staged file")
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	check, err := Open(name)
	if err != nil { // APE and tagless MP3 need the extension for detection.
		check, err = openAs(name, f.Format())
	}
	if err != nil {
		return fmt.Errorf("validate staged file: %w", err)
	}
	after, err := hashAudio(check, "Verifying staged audio")
	if err != nil {
		check.Close()
		return err
	}
	stage("Verifying metadata")
	metadataEqual := rawEqual(f.e.raw(), check.e.raw())
	check.Close()
	if !bytes.Equal(before, after) {
		return errors.New("audio payload changed; original retained")
	}
	if !metadataEqual {
		return errors.New("metadata did not round-trip; original retained")
	}
	now, err := os.Lstat(f.Path)
	if err != nil {
		return err
	}
	if !os.SameFile(st, now) || now.Size() != st.Size() || !now.ModTime().Equal(st.ModTime()) {
		return errors.New("file changed during tagging; original retained")
	}
	if backup {
		b, e := os.OpenFile(f.Path+".bak", os.O_WRONLY|os.O_CREATE|os.O_EXCL, st.Mode().Perm())
		if e != nil {
			return fmt.Errorf("backup: %w", e)
		}
		_, e = io.Copy(writer(b, "Creating backup"), io.NewSectionReader(f.f, 0, st.Size()))
		syncErr := b.Sync()
		closeErr := b.Close()
		if e == nil {
			e = syncErr
		}
		if e == nil {
			e = closeErr
		}
		if e != nil {
			os.Remove(f.Path + ".bak")
			return e
		}
	}
	stage("Replacing original")
	// Windows cannot replace a file while its source handle is open.
	if err = f.f.Close(); err != nil {
		return err
	}
	if err = os.Rename(name, f.Path); err != nil {
		return err
	}
	return nil
}
func openAs(path, format string) (*File, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	var eng engine
	switch format {
	case "mp3":
		eng, e = openMP3(f)
	case "ape":
		eng, e = openAPE(f)
	default:
		f.Close()
		return nil, errors.New("unsupported staged format")
	}
	if e != nil {
		f.Close()
		return nil, e
	}
	return &File{path, f, eng}, nil
}
func readAt(f *os.File, off int64, n int) ([]byte, error) {
	if n < 0 || n > MaxMetadata {
		return nil, errors.New("metadata exceeds 64 MiB limit")
	}
	b := make([]byte, n)
	_, e := f.ReadAt(b, off)
	return b, e
}
func copyRange(w io.Writer, f *os.File, off, n int64) error {
	_, e := io.Copy(w, io.NewSectionReader(f, off, n))
	return e
}
func writeBytes(w io.Writer, b []byte) error { _, e := w.Write(b); return e }
func canon(k string) string {
	if strings.HasPrefix(k, "raw:") || strings.HasPrefix(k, "riff:") || strings.HasPrefix(k, "comment:") || strings.HasPrefix(k, "lyrics:") || strings.HasPrefix(k, "url:") || strings.HasPrefix(k, "rating:") || strings.HasPrefix(k, "playcount:") {
		return k
	}
	k = strings.ToLower(k)
	switch k {
	case "album_artist", "album artist":
		return "albumartist"
	case "track", "track_number":
		return "tracknumber"
	case "totaltracks", "track_total":
		return "tracktotal"
	case "disc", "disc_number":
		return "discnumber"
	case "totaldiscs", "disc_total":
		return "disctotal"
	case "year":
		return "date"
	}
	return k
}
func cloneRaw(r []Raw) []Raw {
	out := make([]Raw, len(r))
	for i, x := range r {
		out[i] = Raw{x.Key, x.Flags, bytes.Clone(x.Data)}
	}
	return out
}
func put32(b *bytes.Buffer, n uint32)   { var v [4]byte; be.PutUint32(v[:], n); b.Write(v[:]) }
func putLE32(b *bytes.Buffer, n uint32) { var v [4]byte; le.PutUint32(v[:], n); b.Write(v[:]) }

// Unix stat structs expose Nlink; reflection avoids platform-specific dependencies.
func hasMultipleLinks(st os.FileInfo) bool {
	s := reflect.ValueOf(st.Sys())
	if s.Kind() == reflect.Ptr {
		if s.IsNil() {
			return false
		}
		s = s.Elem()
	}
	if s.Kind() != reflect.Struct {
		return false
	}
	n := s.FieldByName("Nlink")
	if !n.IsValid() {
		return false
	}
	switch n.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return n.Uint() > 1
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return n.Int() > 1
	}
	return false
}

func rawEqual(a, b []Raw) bool {
	if len(a) != len(b) {
		return false
	}
	for i, x := range a {
		y := b[i]
		if x.Key != y.Key || x.Flags != y.Flags || !bytes.Equal(x.Data, y.Data) {
			return false
		}
	}
	return true
}
