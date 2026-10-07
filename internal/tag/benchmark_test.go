package tag

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"testing"
)

var benchCRC uint32
var benchData []byte

func BenchmarkOggCRC(b *testing.B) {
	for _, size := range []int{1024, 65536} {
		b.Run(fmt.Sprintf("%d", size), func(b *testing.B) {
			data := bytes.Repeat([]byte{0x19, 0xae, 0xff, 0, 0x48, 0x7a, 0x11, 0x29}, size/8)
			b.SetBytes(int64(size))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				benchCRC = oggCRC(data)
			}
		})
	}
}

func BenchmarkCommentsEncode(b *testing.B) {
	c := comments{vendor: "encoder", values: []Raw{{"TITLE", 0, []byte("Part 1")}, {"LYRICS", 0, bytes.Repeat([]byte("A long audiobook transcript.\n"), 40000)}}}
	b.SetBytes(int64(len(c.values[1].Data)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchData = c.encode()
	}
}

func BenchmarkFLACBookTags(b *testing.B) {
	lyrics := []byte(string(bytes.Repeat([]byte("A long audiobook transcript.\n"), 40000)))
	keys := []string{"album", "albumartist", "artist", "author", "composer", "genre", "language", "narrator", "title", "tracknumber", "tracktotal"}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		v := flac{blocks: []Raw{{"0", 0, make([]byte, 34)}}, c: comments{vendor: "encoder", values: []Raw{{"LYRICS", 0, lyrics}}}}
		for _, k := range keys {
			if e := v.set(k, []string{"value"}); e != nil {
				b.Fatal(e)
			}
		}
		benchData = v.raw()[0].Data
	}
}

func BenchmarkOggAudio(b *testing.B) {
	// AUDIOTAG_BENCH_OPUS points to an immutable fixture. Hashing is read-only.
	path := os.Getenv("AUDIOTAG_BENCH_OPUS")
	if path == "" {
		b.Skip("set AUDIOTAG_BENCH_OPUS to an Opus fixture")
	}
	f, e := Open(path)
	if e != nil {
		b.Fatal(e)
	}
	defer f.Close()
	st, e := os.Stat(path)
	if e != nil {
		b.Fatal(e)
	}
	b.SetBytes(st.Size())
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h := sha256.New()
		if e = f.e.audio(h); e != nil {
			b.Fatal(e)
		}
	}
}

func BenchmarkOggWrite(b *testing.B) {
	path := os.Getenv("AUDIOTAG_BENCH_OPUS")
	if path == "" {
		b.Skip("set AUDIOTAG_BENCH_OPUS to an Opus fixture")
	}
	f, e := Open(path)
	if e != nil {
		b.Fatal(e)
	}
	defer f.Close()
	st, e := os.Stat(path)
	if e != nil {
		b.Fatal(e)
	}
	b.SetBytes(st.Size())
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if e = f.e.write(io.Discard); e != nil {
			b.Fatal(e)
		}
	}
}

func BenchmarkMP4BookMetadata(b *testing.B) {
	path := os.Getenv("AUDIOTAG_BENCH_M4B")
	if path == "" {
		b.Skip("set AUDIOTAG_BENCH_M4B to an immutable M4B fixture")
	}
	lyrics := string(bytes.Repeat([]byte("A long audiobook transcript.\n"), 40000))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f, e := Open(path)
		if e != nil {
			b.Fatal(e)
		}
		for _, key := range []string{"title", "album", "artist", "author", "narrator", "tracknumber", "tracktotal"} {
			if e = f.Set(key, []string{"2"}); e != nil {
				f.Close()
				b.Fatal(e)
			}
		}
		if e = f.Set("lyrics", []string{lyrics}); e != nil {
			f.Close()
			b.Fatal(e)
		}
		if e = f.e.write(io.Discard); e != nil {
			f.Close()
			b.Fatal(e)
		}
		benchMetadata = f.e.raw()
		f.Close()
	}
}

var benchMetadata []Raw
