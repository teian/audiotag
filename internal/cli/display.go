package cli

import (
	"audiotag/internal/tag"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"unicode/utf8"
)

// inline keeps metadata, paths, and control characters on a single terminal line.
func inline(s string, limit int) string {
	if limit > 0 && utf8.RuneCountInString(s) > limit {
		s = string([]rune(s)[:limit-1]) + "…"
	}
	quoted := strconv.Quote(s)
	return quoted[1 : len(quoted)-1]
}

func textValue(key string, values []string) string {
	if key == "lyrics" {
		chars, lines := 0, 0
		for _, value := range values {
			chars += utf8.RuneCountInString(value)
			if value != "" {
				lines += strings.Count(strings.TrimSuffix(value, "\n"), "\n") + 1
			}
		}
		return fmt.Sprintf("%d characters, %d lines (use --json for full text)", chars, lines)
	}
	parts := make([]string, 0, len(values))
	for i, value := range values {
		if i == 3 {
			parts = append(parts, fmt.Sprintf("… +%d values", len(values)-i))
			break
		}
		parts = append(parts, inline(value, 100))
	}
	return strings.Join(parts, "; ")
}

func inspectText(out io.Writer, f *tag.File, pics []tag.Picture, chapters []tag.Chapter, chapterErr error) error {
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintf(w, "%s\n", inline(f.Path, 0))
	fmt.Fprintf(w, "  format\t%s\n", f.Format())
	tags := f.Tags()
	seen := map[string]bool{}
	show := func(key string) {
		seen[key] = true
		if values, ok := tags[key]; ok {
			fmt.Fprintf(w, "  %s\t%s\n", inline(key, 40), textValue(key, values))
		}
	}
	for _, key := range []string{"title", "album", "author", "narrator", "artist", "albumartist", "composer", "tracknumber", "tracktotal", "discnumber", "disctotal", "genre", "language", "date", "lyrics"} {
		show(key)
	}
	var keys []string
	for key := range tags {
		if seen[key] || key == "metadata_block_picture" || chapterComment(key) {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		show(key)
	}
	fmt.Fprintf(w, "  artwork\t%d picture(s)\n", len(pics))
	for _, p := range pics {
		kind := fmt.Sprintf("type %d", p.Type)
		if p.Type == 3 {
			kind = "front cover"
		}
		fmt.Fprintf(w, "\t%s, %s, %s\n", kind, inline(p.MIME, 40), byteSize(int64(len(p.Data))))
	}
	fmt.Fprintf(w, "  chapters\t%d\n", len(chapters))
	for i, chapter := range chapters {
		if i == 3 {
			fmt.Fprintf(w, "\t… %d more (use audiotag chapters for the full list)\n", len(chapters)-i)
			break
		}
		fmt.Fprintf(w, "\t%s  %s\n", clockTime(chapter.Start), inline(chapter.Title, 100))
	}
	if chapterErr != nil {
		fmt.Fprintf(w, "  chapter note\t%s\n", inline(chapterErr.Error(), 0))
	}
	return w.Flush()
}

func chapterComment(key string) bool {
	key = strings.TrimSuffix(strings.ToLower(key), "name")
	if !strings.HasPrefix(key, "chapter") {
		return false
	}
	digits := key[len("chapter"):]
	if len(digits) < 3 {
		return false
	}
	for _, r := range digits {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func chaptersText(out io.Writer, path string, chapters []tag.Chapter) error {
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintf(w, "%s — %d chapter(s)\n", inline(path, 0), len(chapters))
	for i, chapter := range chapters {
		fmt.Fprintf(w, "  %d\t%s\t%s\n", i+1, clockTime(chapter.Start), inline(chapter.Title, 0))
	}
	return w.Flush()
}

func coversText(out io.Writer, path string, pictures []tag.Picture) error {
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintf(w, "%s — %d picture(s)\n", inline(path, 0), len(pictures))
	for i, p := range pictures {
		kind := fmt.Sprintf("type %d", p.Type)
		if p.Type == 3 {
			kind = "front cover"
		}
		fmt.Fprintf(w, "  %d\t%s\t%s\t%s\t%s\n", i+1, kind, inline(p.MIME, 0), byteSize(int64(len(p.Data))), inline(p.Description, 100))
	}
	return w.Flush()
}

func clockTime(seconds float64) string {
	ms := int64(seconds*1000 + 0.5)
	return fmt.Sprintf("%02d:%02d:%02d.%03d", ms/3600000, ms/60000%60, ms/1000%60, ms%1000)
}

func byteSize(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	for _, unit := range []struct {
		size int64
		name string
	}{{1 << 30, "GiB"}, {1 << 20, "MiB"}, {1 << 10, "KiB"}} {
		if n >= unit.size {
			return fmt.Sprintf("%.1f %s", float64(n)/float64(unit.size), unit.name)
		}
	}
	return "0 B"
}
