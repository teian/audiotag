package tag

import (
	"bytes"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
)

func TestOggCRCIncremental(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	for _, n := range []int{0, 1, 7, 8, 9, 27, 282, 1024, 65025} {
		b := make([]byte, n)
		r.Read(b)
		var want uint32
		for _, v := range b {
			want ^= uint32(v) << 24
			for bit := 0; bit < 8; bit++ {
				if want&0x80000000 != 0 {
					want = want<<1 ^ 0x04c11db7
				} else {
					want <<= 1
				}
			}
		}
		if got := oggCRC(b); got != want {
			t.Fatalf("size %d: %x != %x", n, got, want)
		}
		for split := 0; split <= n; split += 1 + n/31 {
			if got := oggUpdateCRC(oggCRC(b[:split]), b[split:]); got != want {
				t.Fatalf("split %d/%d", split, n)
			}
		}
	}
}
func TestRawEqual(t *testing.T) {
	a := []Raw{{"key", 1, []byte("value")}}
	if !rawEqual(a, cloneRaw(a)) {
		t.Fatal("equal metadata rejected")
	}
	for _, b := range [][]Raw{nil, {{"other", 1, []byte("value")}}, {{"key", 2, []byte("value")}}, {{"key", 1, []byte("changed")}}} {
		if rawEqual(a, b) {
			t.Fatal("different metadata accepted")
		}
	}
}
func TestFLACDeferredComments(t *testing.T) {
	v := &flac{c: comments{vendor: "test"}, blocks: []Raw{{"0", 0, make([]byte, 34)}}}
	if e := v.set("title", []string{"first"}); e != nil {
		t.Fatal(e)
	}
	if e := v.set("title", []string{"second"}); e != nil {
		t.Fatal(e)
	}
	raw := cloneRaw(v.raw())
	c, e := parseComments(raw[1].Data)
	if e != nil || !bytes.Equal(c.values[0].Data, []byte("second")) {
		t.Fatalf("deferred export: %v", e)
	}
	raw[1].Data[0] ^= 1
	if rawEqual(raw, v.raw()) {
		t.Fatal("raw snapshot aliases metadata")
	}
}

func TestOggScannerRejectsDamage(t *testing.T) {
	var b bytes.Buffer
	if e := writePage(&b, 123, 0, 2, 0, []byte{5}, []byte("hello")); e != nil {
		t.Fatal(e)
	}
	for _, mode := range []string{"valid", "checksum", "truncated"} {
		data := bytes.Clone(b.Bytes())
		if mode == "checksum" {
			data[len(data)-1] ^= 1
		}
		if mode == "truncated" {
			data = data[:len(data)-1]
		}
		path := filepath.Join(t.TempDir(), "page.ogg")
		if e := os.WriteFile(path, data, 0600); e != nil {
			t.Fatal(e)
		}
		f, e := os.Open(path)
		if e != nil {
			t.Fatal(e)
		}
		p, e := newOggScanner(f, 0, int64(len(data))).read()
		f.Close()
		if mode == "valid" {
			if e != nil || !bytes.Equal(p.body, []byte("hello")) {
				t.Fatalf("valid page: %v", e)
			}
		} else if e == nil {
			t.Fatalf("accepted %s page", mode)
		}
	}
}

func TestPairedFieldsWithLargeLyrics(t *testing.T) {
	for _, tc := range []struct {
		name string
		e    engine
	}{
		{"id3v23", &mp3{id3: id3{version: 3}}},
		{"id3v24", &mp3{id3: id3{version: 4}}},
		{"mp4", &mp4{moov: &atom{typ: "moov", children: []*atom{}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lyrics := string(bytes.Repeat([]byte("Grüße 日本語\n"), 50000))
			if e := tc.e.set("lyrics", []string{lyrics}); e != nil {
				t.Fatal(e)
			}
			for _, pair := range []struct{ key, value string }{
				{"tracknumber", "3/12"}, {"tracktotal", "14"}, {"tracknumber", "4"},
				{"discnumber", "1/2"}, {"disctotal", "3"}, {"discnumber", "2"},
			} {
				if e := tc.e.set(pair.key, []string{pair.value}); e != nil {
					t.Fatal(e)
				}
			}
			tags := tc.e.tags()
			for key, want := range map[string]string{"tracknumber": "4", "tracktotal": "14", "discnumber": "2", "disctotal": "3", "lyrics": lyrics} {
				if got := tags[key]; len(got) != 1 || got[0] != want {
					t.Fatalf("%s did not survive paired-field edits", key)
				}
			}
		})
	}
}
