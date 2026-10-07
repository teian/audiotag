package cli

import (
	"audiotag/internal/tag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

func terminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

type saveProgress struct {
	mu          sync.Mutex
	out         io.Writer
	prefix      string
	current     tag.Progress
	started     time.Time
	interactive bool
	width       int
	stop, done  chan struct{}
}

func startProgress(out io.Writer, path string, index, total int, interactive bool) *saveProgress {
	p := &saveProgress{out: out, prefix: fmt.Sprintf("[%d/%d] %s", index, total, inline(filepath.Base(path), 80)), started: time.Now(), interactive: interactive, stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(p.done)
		interval := time.Second
		if interactive {
			interval = 200 * time.Millisecond
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-p.stop:
				return
			case <-ticker.C:
				p.mu.Lock()
				if p.current.Stage != "" {
					p.render()
				}
				p.mu.Unlock()
			}
		}
	}()
	return p
}

func (p *saveProgress) update(event tag.Progress) {
	p.mu.Lock()
	defer p.mu.Unlock()
	changed := event.Stage != p.current.Stage
	p.current = event
	if changed {
		p.render()
	}
}

// Called under mu, so concurrent stage callbacks and the ticker never interleave.
func (p *saveProgress) render() {
	line := p.prefix + " · " + p.current.Stage
	if p.current.Bytes > 0 {
		line += " · " + byteSize(p.current.Bytes)
	}
	line += " · " + time.Since(p.started).Round(time.Second).String()
	if p.interactive {
		padding := ""
		if len(line) < p.width {
			padding = strings.Repeat(" ", p.width-len(line))
		}
		fmt.Fprint(p.out, "\r", line, padding)
		p.width = len(line)
	} else {
		fmt.Fprintln(p.out, line)
	}
}

func (p *saveProgress) finish(err error) {
	close(p.stop)
	<-p.done
	p.mu.Lock()
	defer p.mu.Unlock()
	status := "Verified and saved"
	if err != nil {
		status = "Failed"
	}
	p.current = tag.Progress{Stage: status}
	p.render()
	if p.interactive {
		fmt.Fprintln(p.out)
	}
}

func saveFile(f *tag.File, o options, out io.Writer, index, total int) error {
	interactive := terminal(out)
	enabled := !o.noProgress && o.progress != "never" && (o.progress == "always" || !o.json && interactive)
	if !enabled {
		return f.Save(o.backup)
	}
	p := startProgress(out, f.Path, index, total, interactive)
	err := f.SaveWithProgress(o.backup, p.update)
	p.finish(err)
	return err
}
