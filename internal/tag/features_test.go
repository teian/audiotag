package tag

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDescribedID3AndTiming(t *testing.T) {
	for _, version := range []byte{3, 4} {
		v := id3{version: version}
		for key, value := range map[string]string{"comment:deu:Notes": "Grüße 日本語", "comment:eng:Notes": "English", "lyrics:jpn:Transcript": "日本語", "artisturl": "https://example.org/artist", "url:Catalogue": "https://example.org/catalogue", "rating:user@example.org": "200", "playcount:user@example.org": "4294967297", "synchronizedlyrics": "[00:01.234]Hello\n[01:02.500]日本語", "eventtiming": "[{\"type\":2,\"milliseconds\":1234}]"} {
			if err := v.set(key, []string{value}); err != nil {
				t.Fatal(version, key, err)
			}
		}
		b, err := v.encode()
		if err != nil {
			t.Fatal(err)
		}
		got, err := parseID3(b)
		if err != nil {
			t.Fatal(err)
		}
		tags := got.tags()
		if tags["comment:deu:Notes"][0] != "Grüße 日本語" || tags["comment:eng:Notes"][0] != "English" || tags["playcount:user@example.org"][0] != "4294967297" || tags["rating:user@example.org"][0] != "200" || !strings.Contains(tags["synchronizedlyrics_lrc"][0], "[01:02.500]日本語") {
			t.Fatal(tags)
		}
		got.remove("comment:deu:Notes")
		if len(got.tags()["comment:deu:Notes"]) != 0 || len(got.tags()["comment:eng:Notes"]) != 1 {
			t.Fatal("targeted comment removal lost other languages")
		}
		for _, key := range []string{"synchronizedlyrics", "eventtiming"} {
			got.remove(key)
			if len(got.tags()[key]) != 0 {
				t.Fatal("timing removal failed")
			}
		}
	}
}
func TestTypedMP4Fields(t *testing.T) {
	v := &mp4{moov: &atom{typ: "moov", children: []*atom{}}}
	for key, value := range map[string]string{"bpm": "90", "compilation": "true", "gapless": "false", "mediakind": "2", "advisory": "4", "podcast": "1", "tvseason": "123", "tvepisode": "456"} {
		if err := v.set(key, []string{value}); err != nil {
			t.Fatal(key, err)
		}
	}
	for _, r := range v.raw() {
		a := &atom{typ: r.Key, data: r.Data}
		d, err := mp4Data(a)
		if err != nil || len(d) != 1 || d[0].Flags != 21 {
			t.Fatal("not a typed integer", r, err)
		}
	}
	if v.tags()["bpm"][0] != "90" || v.tags()["compilation"][0] != "1" {
		t.Fatal(v.tags())
	}
	before, _ := json.Marshal(v.raw())
	for _, key := range []string{"bpm", "compilation", "mediakind"} {
		if err := v.set(key, []string{"99999999999"}); err == nil {
			t.Fatal("invalid integer accepted")
		}
	}
	after, _ := json.Marshal(v.raw())
	if !bytes.Equal(before, after) {
		t.Fatal("failed set changed metadata")
	}
}
func featureFixture(t *testing.T, name, codec string) string {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("FFmpeg fixture generator missing")
	}
	path := filepath.Join(t.TempDir(), name)
	ff(t, "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=3", "-ar", "44100", "-c:a", codec, path)
	return path
}
func TestID3v1ExplicitSynchronization(t *testing.T) {
	path := featureFixture(t, "book.mp3", "libmp3lame")
	before := packetHash(t, path)
	f, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Set("id3v1.title", []string{"Legacy"}); err != nil {
		t.Fatal(err)
	}
	if err = f.Set("title", []string{"Unicode 日本語 " + strings.Repeat("x", 50)}); err != nil {
		t.Fatal(err)
	}
	if err = f.Save(false); err != nil {
		t.Fatal(err)
	}
	f.Close()
	f, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if f.Tags()["id3v1.title"][0] != "Legacy" {
		t.Fatal("ordinary write synchronized legacy tag")
	}
	if err = f.Set("genre", []string{"Speech"}); err != nil {
		t.Fatal(err)
	}
	if err = f.SyncID3v1(); err != nil {
		t.Fatal(err)
	}
	if err = f.Save(false); err != nil {
		t.Fatal(err)
	}
	f.Close()
	f, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	tags := f.Tags()
	if !strings.HasPrefix(tags["id3v1.title"][0], "Unicode ???") || len(tags["id3v1.title"][0]) != 30 || tags["id3v1.genre"][0] != "101" || !strings.Contains(tags["title"][0], "日本語") {
		t.Fatal(tags)
	}
	f.Close()
	if !bytes.Equal(before, packetHash(t, path)) {
		t.Fatal("encoded MP3 audio changed")
	}
}
func TestBroadcastWaveRoundTrip(t *testing.T) {
	path := featureFixture(t, "book.wav", "pcm_s16le")
	before := packetHash(t, path)
	f, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for k, value := range map[string]string{"bext.description": "Narration", "bext.originator": "Author", "bext.timereference": "12345678901234", "bext.loudnessvalue": "-23.00", "ixml": "<BWFXML><PROJECT>Book</PROJECT><UNKNOWN keep=\"yes\">value</UNKNOWN></BWFXML>"} {
		if err = f.Set(k, []string{value}); err != nil {
			t.Fatal(k, err)
		}
	}
	if err = f.Set("ixml.project", []string{"Updated"}); err != nil {
		t.Fatal(err)
	}
	if err = f.Save(false); err != nil {
		t.Fatal(err)
	}
	snapshot := f.Export()
	f.Close()
	f, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if f.Tags()["bext.loudnessvalue"][0] != "-23.00" || f.Tags()["bext.timereference"][0] != "12345678901234" || !strings.Contains(f.Tags()["ixml"][0], "UNKNOWN") || f.Tags()["ixml.project"][0] != "Updated" {
		t.Fatal(f.Tags())
	}
	if err = f.Import(snapshot); err != nil {
		t.Fatal(err)
	}
	if err = f.Save(false); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if !bytes.Equal(before, packetHash(t, path)) {
		t.Fatal("WAV audio changed")
	}
	if err := validIXML([]byte("<BWFXML/><BWFXML/>")); err == nil {
		t.Fatal("multiple XML roots accepted")
	}
}
func TestFLACCueAndSeekTable(t *testing.T) {
	path := featureFixture(t, "book.flac", "flac")
	before := packetHash(t, path)
	f, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	cue := "FILE \"book.flac\" WAVE\n TRACK 01 AUDIO\n INDEX 01 00:00:00\n TRACK 02 AUDIO\n INDEX 01 00:01:00\n"
	if err = f.Set("cuesheet", []string{cue}); err != nil {
		t.Fatal(err)
	}
	if err = f.Set("seektable", []string{`[{"sample":18446744073709551615,"offset":0,"frame_samples":0}]`}); err != nil {
		t.Fatal(err)
	}
	if err = f.Save(false); err != nil {
		t.Fatal(err)
	}
	f.Close()
	f, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	tags := f.Tags()
	if !strings.Contains(tags["cuesheettext"][0], "INDEX 01 00:01:00") {
		t.Fatal(tags)
	}
	if err = f.Set("cuesheet", []string{`{"tracks":[]}`}); err == nil {
		t.Fatal("empty cuesheet accepted")
	}
	f.Close()
	if !bytes.Equal(before, packetHash(t, path)) {
		t.Fatal("FLAC audio changed")
	}
	if _, err := exec.LookPath("metaflac"); err == nil {
		b, err := exec.Command("metaflac", "--export-cuesheet-to=-", path).CombinedOutput()
		if err != nil || !strings.Contains(string(b), "TRACK 02") {
			t.Fatalf("metaflac rejected cuesheet: %s %v", b, err)
		}
	}
}
func TestQuickTimeChapters(t *testing.T) {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("FFprobe missing")
	}
	for _, flags := range []string{"+disable_chpl", "+faststart"} {
		t.Run(flags, func(t *testing.T) {
			path := featureFixture(t, "base.m4a", "aac")
			dir := filepath.Dir(path)
			metadata := filepath.Join(dir, "chapters.ffmeta")
			os.WriteFile(metadata, []byte(";FFMETADATA1\n[CHAPTER]\nTIMEBASE=1/1000\nSTART=0\nEND=1200\ntitle=First\n[CHAPTER]\nTIMEBASE=1/1000\nSTART=1200\nEND=3000\ntitle=Last\n"), 0644)
			book := filepath.Join(dir, "book.m4b")
			ff(t, "-v", "error", "-i", path, "-i", metadata, "-map_metadata", "1", "-map_chapters", "1", "-c", "copy", "-movflags", flags, book)
			before := packetHash(t, book)
			f, err := Open(book)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.Chapters(); err != nil {
				t.Fatal(err)
			}
			chapters := []Chapter{{Start: 0, End: 1, Title: "Longer title 日本語"}, {Start: 1, End: 2, Title: "Added"}, {Start: 2, End: 3, Title: "End"}}
			if err = f.SetChapters(chapters); err != nil {
				t.Fatal(err)
			}
			snapshot := f.Export()
			if err = f.Save(false); err != nil {
				t.Fatal(err)
			}
			f.Close()
			f, err = Open(book)
			if err != nil {
				t.Fatal(err)
			}
			if err = f.Import(snapshot); err != nil {
				t.Fatal(err)
			}
			if err = f.Save(false); err != nil {
				t.Fatal("native QT import", err)
			}
			f.Close()
			probe, err := exec.Command("ffprobe", "-v", "error", "-show_chapters", "-of", "json", book).Output()
			if err != nil {
				t.Fatal(err)
			}
			var info struct {
				Chapters []struct{ Tags map[string]string }
			}
			json.Unmarshal(probe, &info)
			if len(info.Chapters) != 3 || info.Chapters[0].Tags["title"] != "Longer title 日本語" {
				t.Fatalf("%s", probe)
			}
			if !bytes.Equal(before, packetHash(t, book)) {
				t.Fatal("encoded AAC audio changed")
			}
			f, err = Open(book)
			if err != nil {
				t.Fatal(err)
			}
			if err = f.SetChapters(nil); err != nil {
				t.Fatal(err)
			}
			if err = f.Save(false); err != nil {
				t.Fatal("clear QuickTime chapters", err)
			}
			f.Close()
		})
	}
}

func TestIXMLRepeatedFieldsAndUnknownNodes(t *testing.T) {
	v := &iff{kind: "wav", id3: id3{version: 4}}
	source := `<BWFXML><!--keep--><PROJECT>Old</PROJECT><TRACK_LIST><TRACK id="1"><NAME>First</NAME></TRACK><TRACK id="2"><NAME>Second</NAME></TRACK></TRACK_LIST><custom xmlns="urn:vendor"><child>Opaque</child></custom></BWFXML>`
	if err := v.set("ixml", []string{source}); err != nil {
		t.Fatal(err)
	}
	if err := v.set("ixml.track_list.track.name", []string{"One", "Two"}); err != nil {
		t.Fatal(err)
	}
	if err := v.set("ixml.project", []string{"New & escaped"}); err != nil {
		t.Fatal(err)
	}
	tags := v.tags()
	if tags["ixml.track_list.track.name"][1] != "Two" || tags["ixml.project"][0] != "New & escaped" || !strings.Contains(tags["ixml"][0], "urn:vendor") || !strings.Contains(tags["ixml"][0], "Opaque") || !strings.Contains(tags["ixml"][0], "<!--keep-->") {
		t.Fatal(tags)
	}
	if err := v.remove("ixml.project"); err != nil {
		t.Fatal(err)
	}
	if len(v.tags()["ixml.project"]) != 0 || len(v.tags()["ixml.track_list.track.name"]) != 2 {
		t.Fatal("iXML removal damaged siblings")
	}
}
func TestStructuredMetadataRejectsMalformedValues(t *testing.T) {
	v := id3{version: 4}
	for key, value := range map[string]string{"comment:english:note": "text", "raw:WXXX": "url", "rating:owner": "256", "synchronizedlyrics": `{"language":"eng","content_type":1,"entries":[{"milliseconds":2,"text":"a"},{"milliseconds":1,"text":"b"}]}`, "eventtiming": `[{"type":1,"milliseconds":2},{"type":2,"milliseconds":1}]`} {
		if err := v.set(key, []string{value}); err == nil {
			t.Fatal(key, "malformed metadata accepted")
		}
		if len(v.frames) != 0 {
			t.Fatal(key, "failure changed the original frames")
		}
	}
	for _, b := range [][]byte{{}, make([]byte, 395), append(make([]byte, 395), 1)} {
		if _, err := parseCue(b); err == nil {
			t.Fatal("truncated cue accepted")
		}
	}
}

func TestSyncFollowsEditedVersionAndRejectsConflicts(t *testing.T) {
	v := &mp3{id3: id3{version: 4}}
	f := &File{e: v}
	v.set("title", []string{"Primary"})
	v.set("id3v1.title", []string{"Legacy"})
	if err := f.SyncTags([]string{"id3v1.title"}); err != nil {
		t.Fatal(err)
	}
	if v.tags()["title"][0] != "Legacy" {
		t.Fatal("sync did not follow legacy edit")
	}
	v.set("title", []string{"Primary conflict"})
	v.set("id3v1.title", []string{"Legacy conflict"})
	if err := f.SyncTags([]string{"title", "id3v1.title"}); err == nil {
		t.Fatal("conflicting edits accepted")
	}
	if v.tags()["title"][0] != "Primary conflict" || v.tags()["id3v1.title"][0] != "Legacy conflict" {
		t.Fatal("conflict changed tag data")
	}
	v.set("title", []string{"Same"})
	v.set("id3v1.title", []string{"Same"})
	if err := f.SyncTags([]string{"title", "id3v1.title"}); err != nil {
		t.Fatal("matching edits refused", err)
	}
}
