package cli

import (
	"audiotag/internal/tag"
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestTranscript(t *testing.T) {
	s := "# Chapter 1: One\n\nFirst.\n## Chapter 2 — Two\nSecond.\n# Chapter 3: Three!\nThird."
	got, e := transcript(s, 2, 3)
	if e != nil {
		t.Fatal(e)
	}
	if got != "Chapter 2: Two.\n\nSecond.\n\nChapter 3: Three!\n\nThird.\n" {
		t.Fatal(got)
	}
	if _, e = transcript(s, 2, 4); e == nil {
		t.Fatal("missing chapter accepted")
	}
}
func TestPartAuthority(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{"Part 1 - Start", "Part 4 - End"} {
		if e := os.MkdirAll(filepath.Join(root, p, "flac"), 0755); e != nil {
			t.Fatal(e)
		}
	}
	path := filepath.Join(root, "Part 1 - Start", "flac", "Chapter 10-20.flac")
	title, n, total, e := partIdentity(path, "folder", map[string][]string{"title": {"Part 9 wrong"}})
	if e != nil || n != 1 || total != 4 || title != "Part 1 - Start" {
		t.Fatal(title, n, total, e)
	}
}
func TestInterspersedFlags(t *testing.T) {
	o, p, e := parse([]string{"one.flac", "--set", "title=Hi", "--recursive", "two.opus", "--", "--name.mp3"}, &bytes.Buffer{})
	if e != nil || !o.recursive || len(o.sets) != 1 || len(p) != 3 || p[2] != "--name.mp3" {
		t.Fatal(o, p, e)
	}
}
func TestWorkflowAndFailureSafety(t *testing.T) {
	if _, e := exec.LookPath("ffmpeg"); e != nil {
		t.Skip("ffmpeg fixtures")
	}
	root := t.TempDir()
	tools := filepath.Join(root, "tools")
	part := filepath.Join(root, "Part 1 - Beginning", "flac")
	os.MkdirAll(tools, 0755)
	os.MkdirAll(part, 0755)
	os.MkdirAll(filepath.Join(root, "Part 3 - End"), 0755)
	path := filepath.Join(part, "Chapter 1-2.flac")
	out, e := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=duration=2", "-c:a", "flac", path).CombinedOutput()
	if e != nil {
		t.Fatal(e, string(out))
	}
	cfg := filepath.Join(tools, "kokoro.json")
	source := filepath.Join(root, "book.md")
	os.WriteFile(cfg, []byte(`{"album":"A Book","author":"Author","narrator":"Reader","source":"book.md"}`), 0644)
	os.WriteFile(source, []byte("# Chapter 1: First\nOne\n# Chapter 2: Second\nTwo\n"), 0644)
	before, _ := os.ReadFile(path)
	var stdout, stderr bytes.Buffer
	args := []string{"audiobook", "--config", cfg, "--recursive", root}
	if e = Run(append(args, "--dry-run"), &stdout, &stderr); e != nil {
		t.Fatal(e)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("dry run wrote file")
	}
	if !strings.Contains(stdout.String(), "tracktotal") {
		t.Fatal(stdout.String())
	}
	stdout.Reset()
	if e = Run(args, &stdout, &stderr); e != nil {
		t.Fatal(e)
	}
	f, e := tag.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	tags := f.Tags()
	f.Close()
	if tags["title"][0] != "Part 1 - Beginning" || tags["tracktotal"][0] != "3" || tags["lyrics"][0] != "Chapter 1: First.\n\nOne\n\nChapter 2: Second.\n\nTwo\n" {
		t.Fatal(tags)
	}
	before, _ = os.ReadFile(path)
	bad := filepath.Join(root, "bad.mp3")
	os.WriteFile(bad, []byte("not audio"), 0644)
	if e = Run([]string{"set", "--set", "title=Wrong", path, bad}, &stdout, &stderr); e == nil {
		t.Fatal("bad file accepted")
	}
	after, _ = os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("preflight failure modified earlier file")
	}
	os.WriteFile(path+".bak", []byte("precious backup"), 0644)
	if e = Run([]string{"set", "--set", "title=Wrong", "--backup", path}, &stdout, &stderr); e == nil {
		t.Fatal("overwrote backup")
	}
	after, _ = os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("backup failure modified original")
	}
	link := filepath.Join(root, "link.flac")
	if e = os.Symlink(path, link); e == nil {
		if e = Run([]string{"set", "--set", "title=Wrong", link}, &stdout, &stderr); e == nil {
			t.Fatal("symlink accepted")
		}
	}
}

func TestFlatPartWorkflow(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"part1_stormwind.opus", "part9_stormwind.flac", "part9_stormwind.opus"} {
		if e := os.WriteFile(filepath.Join(root, name), nil, 0644); e != nil {
			t.Fatal(e)
		}
	}
	path := filepath.Join(root, "part1_stormwind.opus")
	title, n, total, e := partIdentity(path, "auto", map[string][]string{"title": {"The Storm Lord — Part 1: Opening"}})
	if e != nil || n != 1 || total != 9 || title != "The Storm Lord — Part 1: Opening" {
		t.Fatal(title, n, total, e)
	}
	_, n, total, e = partIdentity(path, "filename", nil)
	if e != nil || n != 1 || total != 9 {
		t.Fatal(n, total, e)
	}
}

func TestSyncRequiresWritesAndFollowsExplicitID3v1(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("FFmpeg fixture generator missing")
	}
	path := filepath.Join(t.TempDir(), "book.mp3")
	if b, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "anullsrc=r=44100:cl=mono", "-t", "0.1", "-c:a", "libmp3lame", path).CombinedOutput(); err != nil {
		t.Fatalf("%s: %v", b, err)
	}
	var out bytes.Buffer
	if err := Run([]string{"set", path, "--set", "title=Primary", "--set", "id3v1.title=Legacy"}, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	f, err := tag.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if f.Tags()["id3v1.title"][0] != "Legacy" {
		t.Fatal("default synchronized legacy tag")
	}
	f.Close()
	if err := Run([]string{"set", path, "--sync", "--set", "id3v1.title=New legacy"}, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	f, err = tag.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if f.Tags()["title"][0] != "New legacy" {
		t.Fatal("CLI sync ignored explicit legacy edit")
	}
	f.Close()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Run([]string{"set", path, "--sync", "--set", "title=One", "--set", "id3v1.title=Two"}, &out, io.Discard); err == nil {
		t.Fatal("conflicting edits accepted")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("conflict modified file")
	}
	if err := Run([]string{"inspect", path, "--sync"}, &out, io.Discard); err == nil {
		t.Fatal("read-only sync accepted")
	}
	if err := Run([]string{"set", path, "--sync"}, &out, io.Discard); err != nil {
		t.Fatal("standalone sync failed", err)
	}
}
