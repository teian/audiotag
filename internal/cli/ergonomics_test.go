package cli

import (
	"audiotag/internal/tag"
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func fixture(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg fixtures")
	}
	path := filepath.Join(t.TempDir(), "book.flac")
	if b, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=duration=0.2", "-c:a", "flac", path).CombinedOutput(); err != nil {
		t.Fatal(err, string(b))
	}
	return path
}

func TestCompactInspectionAndLosslessJSON(t *testing.T) {
	path := fixture(t)
	f, err := tag.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	lyrics := strings.Repeat("Grüße 日本語\n", 10000)
	f.Set("title", []string{"A Book\x1b[2J"})
	f.Set("lyrics", []string{lyrics})
	chapters := make([]tag.Chapter, 10)
	for i := range chapters {
		chapters[i] = tag.Chapter{Start: float64(i), Title: "A chapter"}
	}
	f.SetChapters(chapters)
	if err = f.Save(false); err != nil {
		t.Fatal(err)
	}
	f.Close()
	var out, diagnostics bytes.Buffer
	if err = Run([]string{"inspect", path}, &out, &diagnostics); err != nil {
		t.Fatal(err)
	}
	summary := out.String()
	if len(summary) > 2000 || !strings.Contains(summary, "100000 characters, 10000 lines") || !strings.Contains(summary, "7 more") {
		t.Fatal(summary)
	}
	if strings.Contains(summary, "\x1b") || strings.Contains(summary, "chapter000") {
		t.Fatal("terminal control or raw chapter comments leaked", summary)
	}
	out.Reset()
	if err = Run([]string{"inspect", "--json", path}, &out, &diagnostics); err != nil {
		t.Fatal(err)
	}
	var inspected []struct {
		Tags map[string][]string `json:"tags"`
	}
	if err = json.Unmarshal(out.Bytes(), &inspected); err != nil {
		t.Fatal(err)
	}
	if inspected[0].Tags["lyrics"][0] != lyrics {
		t.Fatal("JSON truncated lyrics")
	}
	out.Reset()
	if err = Run([]string{"inspect", "--raw", path}, &out, &diagnostics); err != nil {
		t.Fatal(err)
	}
	if !json.Valid(out.Bytes()) || !bytes.Contains(out.Bytes(), []byte(`"raw"`)) {
		t.Fatal("--raw must retain native JSON output")
	}
	out.Reset()
	if err = Run([]string{"chapters", path}, &out, &diagnostics); err != nil {
		t.Fatal(err)
	}
	if strings.Count(out.String(), "A chapter") != 10 {
		t.Fatal("chapter command truncated list", out.String())
	}
}

func TestProgressAndScriptOutput(t *testing.T) {
	path := fixture(t)
	var out, diagnostics bytes.Buffer
	args := []string{"set", "--set", "title=New", "--json", "--progress", "always", path}
	if err := Run(args, &out, &diagnostics); err != nil {
		t.Fatal(err)
	}
	if !json.Valid(out.Bytes()) {
		t.Fatal("progress polluted JSON", out.String())
	}
	for _, stage := range []string{"[1/1] book.flac", "Checking original audio", "Writing metadata", "Verifying staged audio", "Verified and saved"} {
		if !strings.Contains(diagnostics.String(), stage) {
			t.Fatal("missing stage", stage, diagnostics.String())
		}
	}
	for _, flags := range [][]string{nil, {"--json"}, {"--progress", "always", "--no-progress"}, {"--progress", "never"}} {
		out.Reset()
		diagnostics.Reset()
		args = append([]string{"set", "--set", "title=Next"}, flags...)
		args = append(args, path)
		if err := Run(args, &out, &diagnostics); err != nil {
			t.Fatal(err)
		}
		if diagnostics.Len() != 0 {
			t.Fatal("unexpected progress", diagnostics.String())
		}
	}
	out.Reset()
	diagnostics.Reset()
	if err := Run([]string{"set", "--set", "title=Dry", "--dry-run", "--progress", "always", path}, &out, &diagnostics); err != nil {
		t.Fatal(err)
	}
	if diagnostics.Len() != 0 {
		t.Fatal("dry run started write progress")
	}
	if err := Run([]string{"set", "--set", "title=Oops", "--progress", "invalid", path}, &out, &diagnostics); err == nil {
		t.Fatal("invalid progress mode accepted")
	}
}

type progressCapture struct {
	mu     sync.Mutex
	buffer bytes.Buffer
	lines  chan string
}

func (w *progressCapture) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buffer.Write(b)
	select {
	case w.lines <- string(b):
	default:
	}
	return len(b), nil
}
func TestProgressHeartbeatAndFailure(t *testing.T) {
	out := &progressCapture{lines: make(chan string, 20)}
	p := startProgress(out, "book.flac", 2, 3, false)
	p.update(tag.Progress{Stage: "Writing metadata", Bytes: 1 << 20})
	<-out.lines // Immediate stage event.
	select {
	case line := <-out.lines:
		if !strings.Contains(line, "1.0 MiB") {
			t.Fatal(line)
		}
	case <-time.After(3 * time.Second):
		p.finish(nil)
		t.Fatal("no heartbeat during a long phase")
	}
	p.finish(exec.ErrNotFound)
	out.mu.Lock()
	defer out.mu.Unlock()
	if !strings.Contains(out.buffer.String(), "[2/3]") || !strings.Contains(out.buffer.String(), "Failed") || strings.Contains(out.buffer.String(), "Verified and saved") {
		t.Fatal(out.buffer.String())
	}
}

func TestTagKeyDiscovery(t *testing.T) {
	var out bytes.Buffer
	if err := Run([]string{"tag-keys"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"author", "narrator", "lyrics", "tracktotal", "isbn"} {
		if !strings.Contains("\n"+out.String(), "\n"+key+"\n") {
			t.Fatal("missing key", key)
		}
	}
}
