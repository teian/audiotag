package tag

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func ff(t *testing.T, args ...string) []byte {
	t.Helper()
	out, e := exec.Command("ffmpeg", args...).CombinedOutput()
	if e != nil {
		t.Fatalf("ffmpeg: %v: %s", e, out)
	}
	return out
}
func packetHash(t *testing.T, path string) []byte {
	t.Helper()
	b, e := exec.Command("ffmpeg", "-v", "error", "-i", path, "-map", "0:a:0", "-c", "copy", "-f", "hash", "-hash", "sha256", "-").Output()
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func TestRealContainers(t *testing.T) {
	if _, e := exec.LookPath("ffmpeg"); e != nil {
		t.Skip("ffmpeg only needed for integration fixtures")
	}
	if _, e := exec.LookPath("ffprobe"); e != nil {
		t.Skip("ffprobe only needed for integration validation")
	}
	// Use FFmpeg's built-in Vorbis encoder: platform packages may omit libvorbis.
	// It requires experimental mode and stereo input; neither affects the CLI.
	cases := []struct {
		name, codec string
		extra       []string
	}{{"book.flac", "flac", nil}, {"book.mp3", "libmp3lame", nil}, {"book.m4a", "aac", nil}, {"book.m4b", "aac", []string{"-movflags", "+faststart"}}, {"book.opus", "libopus", nil}, {"book.ogg", "vorbis", []string{"-strict", "experimental", "-ac", "2"}}, {"book.wav", "pcm_s16le", nil}, {"book.aiff", "pcm_s16be", nil}, {"book.wv", "wavpack", nil}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, tc.name)
			args := []string{"-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=3", "-c:a", tc.codec, "-metadata", "title=Before", "-metadata", "comment=keep me"}
			args = append(args, tc.extra...)
			args = append(args, path)
			ff(t, args...)
			before := packetHash(t, path)
			f, e := Open(path)
			if e != nil {
				t.Fatal(e)
			}
			defer f.Close()
			lyrics := strings.Repeat("Chapter 1: Grüße 日本語\nLine = # ; \\\n", 5000)
			for _, kv := range []struct {
				k string
				v []string
			}{{"title", []string{"Part 2 — Ω"}}, {"author", []string{"An Author"}}, {"narrator", []string{"A Narrator"}}, {"tracknumber", []string{"2"}}, {"tracktotal", []string{"7"}}, {"lyrics", []string{lyrics}}, {"custom-field", []string{"first", "second"}}} {
				if e = f.Set(kv.k, kv.v); e != nil {
					t.Fatal(kv.k, e)
				}
			}
			png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a5XcAAAAASUVORK5CYII=")
			p := Picture{MIME: "image/png", Type: 3, Data: png}
			if e = f.Cover(p); e != nil {
				t.Fatal(e)
			}
			chapters := []Chapter{{Start: 0, End: 1.2, Title: "Opening"}, {Start: 1.2, End: 3, Title: "Second — 二"}}
			if e = f.SetChapters(chapters); e != nil {
				t.Fatal(e)
			}
			var progress []Progress
			if e = f.SaveWithProgress(true, func(event Progress) { progress = append(progress, event) }); e != nil {
				t.Fatal(e)
			}
			if len(progress) == 0 || progress[len(progress)-1].Stage != "Replacing original" {
				t.Fatal("save progress did not cover the complete transaction", progress)
			}
			if !bytes.Equal(before, packetHash(t, path)) {
				t.Fatal("ffmpeg packet hash changed")
			}
			g, e := Open(path)
			if e != nil {
				t.Fatal(e)
			}
			defer g.Close()
			if got := g.Tags()["lyrics"]; len(got) != 1 || got[0] != lyrics {
				t.Fatal("lyrics did not round-trip")
			}
			if got := g.Tags()["custom-field"]; len(got) != 2 || got[1] != "second" {
				t.Fatal("multivalues did not round-trip", got)
			}
			if got := g.Tags()["tracktotal"]; len(got) != 1 || got[0] != "7" {
				t.Fatal("tracktotal", got)
			}
			pictures, e := g.Pictures()
			if e != nil || len(pictures) != 1 || !bytes.Equal(pictures[0].Data, p.Data) {
				t.Fatal("cover", pictures, e)
			}
			got, e := g.Chapters()
			if e != nil || len(got) != 2 || got[1].Title != chapters[1].Title || got[1].Start != 1.2 {
				t.Fatal("chapters", got, e)
			}
			// Unknown/native metadata must survive export/import without coercion.
			snap := g.Export()
			// Internal views must never escape through public snapshots/imports.
			assertSnapshotOwnership(t, g)
			model := cloneRaw(g.e.raw())
			var atomModel []byte
			if mp, ok := g.e.(*mp4); ok {
				atomModel = mp.moov.encode()
			}
			for n := 0; n < 2; n++ {
				if e := g.e.write(io.Discard); e != nil {
					t.Fatal(e)
				}
				if mp, ok := g.e.(*mp4); ok && !bytes.Equal(atomModel, mp.moov.encode()) {
					t.Fatal("write mutated MP4 offset model")
				}
				if !rawEqual(model, g.e.raw()) {
					t.Fatal("write mutated editable metadata")
				}
			}
			b, e := json.Marshal(snap)
			if e != nil {
				t.Fatal(e)
			}
			var decoded Snapshot
			if e = json.Unmarshal(b, &decoded); e != nil {
				t.Fatal(e)
			}
			if e = g.Import(decoded); e != nil {
				t.Fatal(e)
			}
			if e = g.Save(false); e != nil {
				t.Fatal(e)
			}
			if !bytes.Equal(before, packetHash(t, path)) {
				t.Fatal("audio changed after native import")
			}
			// Validate the rewritten container with an independent demuxer/decoder.
			ff(t, "-v", "error", "-xerror", "-i", path, "-map", "0:a:0", "-f", "null", "-")
			probe, e := exec.Command("ffprobe", "-v", "error", "-show_chapters", "-show_format", "-show_streams", "-of", "json", path).Output()
			if e != nil {
				t.Fatal(e)
			}
			var info struct {
				Chapters []struct {
					Tags map[string]string `json:"tags"`
				} `json:"chapters"`
			}
			if e = json.Unmarshal(probe, &info); e != nil {
				t.Fatal(e)
			}
			if strings.HasSuffix(path, ".mp3") || strings.HasSuffix(path, ".m4b") || strings.HasSuffix(path, ".m4a") || strings.HasSuffix(path, ".opus") {
				if len(info.Chapters) != 2 || info.Chapters[1].Tags["title"] != chapters[1].Title {
					t.Fatalf("ffprobe chapters: %s", probe)
				}
			}
			original, e := os.ReadFile(path + ".bak")
			if e != nil {
				t.Fatal(e)
			}
			if len(original) == 0 {
				t.Fatal("empty backup")
			}
		})
	}
}

func TestUnknownID3FrameAndVersion(t *testing.T) {
	for _, version := range []byte{3, 4} {
		t.Run(string(rune('0'+version)), func(t *testing.T) {
			v := id3{version: version, frames: []Raw{{"PRIV", 0, []byte("owner\x00\xff\x00\x11")}, {"POPM", 0, []byte("mail\x00\x80\x00\x00\x00\x09")}}}
			if e := v.set("lyrics", []string{"Grüße\n日本語"}); e != nil {
				t.Fatal(e)
			}
			b, e := v.encode()
			if e != nil {
				t.Fatal(e)
			}
			g, e := parseID3(b)
			if e != nil {
				t.Fatal(e)
			}
			if g.version != version || !bytes.Equal(g.frames[0].Data, v.frames[0].Data) || g.tags()["lyrics"][0] != "Grüße\n日本語" {
				t.Fatal("ID3 roundtrip", g.tags())
			}
		})
	}
}
func TestNativeMP4TypedData(t *testing.T) {
	v := &mp4{moov: &atom{typ: "moov", children: []*atom{}}}
	r := []Raw{{"tmpo", 0, dataAtom(21, []byte{0, 90}).encode()}, {"----:com.example:binary", 0, append(append((&atom{typ: "mean", data: append(make([]byte, 4), []byte("com.example")...)}).encode(), (&atom{typ: "name", data: append(make([]byte, 4), []byte("binary")...)}).encode()...), dataAtom(0, []byte{255, 0, 13}).encode()...)}}
	if e := v.replace(r); e != nil {
		t.Fatal(e)
	}
	if got := v.tags()["raw:tmpo"]; len(got) != 1 || got[0] != "90" {
		t.Fatal(got)
	}
	a, _ := json.Marshal(v.raw())
	b, _ := json.Marshal(r)
	if !bytes.Equal(a, b) {
		t.Fatal("typed native atoms lost")
	}
}
func FuzzParseID3(f *testing.F) {
	f.Add([]byte("ID3\x04\x00\x00\x00\x00\x00\x00"))
	f.Fuzz(func(t *testing.T, b []byte) { _, _ = parseID3(b) })
}
func FuzzParseComments(f *testing.F) {
	f.Add((&comments{vendor: "audiotag"}).encode())
	f.Fuzz(func(t *testing.T, b []byte) { _, _ = parseComments(b) })
}
func FuzzParseAtoms(f *testing.F) {
	f.Add((&atom{typ: "free", data: []byte{1, 2}}).encode())
	f.Fuzz(func(t *testing.T, b []byte) { _, _ = parseAtoms(b, 0) })
}

func TestOpaqueMetadataPreservation(t *testing.T) {
	dir := t.TempDir() // Synthetic APE/Musepack containers exercise native tag editing without requiring uncommon encoders.
	for _, magic := range []string{"MAC ", "MPCK"} {
		path := filepath.Join(dir, magic[:3]+".ape")
		audio := append([]byte(magic), bytes.Repeat([]byte{1, 2, 3, 255}, 100)...)
		if e := os.WriteFile(path, audio, 0644); e != nil {
			t.Fatal(e)
		}
		f, e := Open(path)
		if e != nil {
			t.Fatal(e)
		}
		a := f.e.(*ape)
		a.items = []Raw{{"opaque", 2, []byte{0, 255, 1, 0}}, {"readonly", 1, []byte("unchanged")}}
		if e = f.Set("title", []string{"Part 1"}); e != nil {
			t.Fatal(e)
		}
		if e = f.Save(false); e != nil {
			t.Fatal(e)
		}
		g, e := Open(path)
		if e != nil {
			t.Fatal(e)
		}
		if !bytes.Equal(g.Export().Raw[0].Data, []byte{0, 255, 1, 0}) {
			t.Fatal("binary APE item changed")
		}
		if e = g.Remove("readonly"); e == nil {
			t.Fatal("read-only APE tag removed")
		}
		g.Close()
		f.Close()
	}
	v := comments{vendor: "encoder", values: []Raw{{"TITLE", 0, []byte("old")}}}
	payload := append(v.encode(), []byte{0, 255, 1, 42}...)
	decoded, e := parseComments(payload)
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(decoded.trailer, []byte{0, 255, 1, 42}) {
		t.Fatal("comment suffix lost")
	}
}

func TestUnsafeInputsRetainOriginal(t *testing.T) {
	if _, e := exec.LookPath("ffmpeg"); e != nil {
		t.Skip("ffmpeg fixtures")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "book.flac")
	ff(t, "-v", "error", "-f", "lavfi", "-i", "sine=duration=0.2", "-c:a", "flac", path)
	before, _ := os.ReadFile(path)
	f, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	if e = f.Set("title", []string{string([]byte{255})}); e == nil {
		t.Fatal("invalid UTF-8 accepted")
	}
	link := filepath.Join(dir, "alias.flac")
	if e = os.Link(path, link); e == nil {
		st, _ := os.Stat(path)
		if hasMultipleLinks(st) {
			f.Set("title", []string{"no"})
			if e = f.Save(false); e == nil {
				t.Fatal("multiply-linked input replaced")
			}
		}
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("unsafe input changed")
	}
}

func assertSnapshotOwnership(t *testing.T, f *File) {
	t.Helper()
	original := f.Export()
	exported := f.Export()
	mutateSnapshot(&exported)
	if !rawEqual(original.Raw, f.Export().Raw) {
		t.Fatal("exported snapshot aliases internal metadata")
	}
	imported := f.Export()
	if e := f.Import(imported); e != nil {
		t.Fatal(e)
	}
	mutateSnapshot(&imported)
	if !rawEqual(original.Raw, f.Export().Raw) {
		t.Fatal("import retains caller-owned metadata")
	}
	if got := f.Tags()["lyrics"]; len(got) != 1 || got[0] != original.Tags["lyrics"][0] {
		t.Fatal("snapshot tag values alias internal metadata")
	}
}
func mutateSnapshot(s *Snapshot) {
	for i := range s.Raw {
		if len(s.Raw[i].Data) > 0 {
			s.Raw[i].Data[0] ^= 1
		}
		s.Raw[i].Key = "changed"
		s.Raw[i].Flags ^= 1
	}
	for key, values := range s.Tags {
		if len(values) > 0 {
			values[0] = "changed"
		}
		delete(s.Tags, key)
	}
}
