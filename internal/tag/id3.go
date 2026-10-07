package tag

import (
	"bytes"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

type id3 struct {
	version byte
	frames  []Raw
}

var id3Names = map[string]string{"title": "TIT2", "album": "TALB", "artist": "TPE1", "albumartist": "TPE2", "composer": "TCOM", "genre": "TCON", "date": "TDRC", "tracknumber": "TRCK", "discnumber": "TPOS", "language": "TLAN", "copyright": "TCOP", "publisher": "TPUB", "encodedby": "TENC", "conductor": "TPE3", "grouping": "TIT1", "subtitle": "TIT3", "bpm": "TBPM", "isrc": "TSRC", "titlesort": "TSOT", "albumsort": "TSOA", "artistsort": "TSOP"}

func synchsafe(b []byte) (int, error) {
	if len(b) < 4 {
		return 0, errors.New("truncated ID3 size")
	}
	n := 0
	for _, v := range b[:4] {
		if v&128 != 0 {
			return 0, errors.New("invalid synchsafe size")
		}
		n = n<<7 | int(v)
	}
	return n, nil
}
func syncBytes(n int) []byte {
	return []byte{byte(n>>21) & 127, byte(n>>14) & 127, byte(n>>7) & 127, byte(n) & 127}
}
func parseID3(b []byte) (id3, error)     { return parseID3Mode(b, true) }
func parseID3View(b []byte) (id3, error) { return parseID3Mode(b, false) }
func parseID3Mode(b []byte, owned bool) (id3, error) {
	v := id3{version: 4}
	if len(b) == 0 {
		return v, nil
	}
	if len(b) < 10 || string(b[:3]) != "ID3" {
		return v, errors.New("invalid ID3 header")
	}
	v.version = b[3]
	if v.version != 3 && v.version != 4 {
		return v, errors.New("ID3v2.2 is unsupported; convert to v2.3/v2.4 first")
	}
	if b[4] != 0 || b[5] != 0 {
		return v, errors.New("ID3 extended/unsynchronized/footer tags are unsupported; refusing unsafe edit")
	}
	n, e := synchsafe(b[6:10])
	if e != nil || n != len(b)-10 {
		return v, errors.New("invalid ID3 length")
	}
	pos := 10
	for pos < len(b) {
		if b[pos] == 0 {
			for _, x := range b[pos:] {
				if x != 0 {
					return v, errors.New("nonzero ID3 padding")
				}
			}
			break
		}
		if len(b)-pos < 10 {
			return v, errors.New("truncated ID3 frame")
		}
		key := string(b[pos : pos+4])
		if !validFrameID(key) {
			return v, errors.New("invalid ID3 frame ID")
		}
		size := int(be.Uint32(b[pos+4:]))
		if v.version == 4 {
			size, e = synchsafe(b[pos+4:])
			if e != nil {
				return v, e
			}
		}
		flags := uint32(be.Uint16(b[pos+8:]))
		pos += 10
		if size > len(b)-pos {
			return v, errors.New("truncated ID3 frame body")
		}
		data := b[pos : pos+size]
		if owned {
			data = bytes.Clone(data)
		}
		v.frames = append(v.frames, Raw{key, flags, data})
		pos += size
	}
	return v, nil
}
func validFrameID(k string) bool {
	if len(k) != 4 {
		return false
	}
	for _, c := range k {
		if !(c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}
func (v *id3) encode() ([]byte, error) {
	n := 0
	for _, r := range v.frames {
		if !validFrameID(r.Key) || r.Flags > 65535 {
			return nil, errors.New("invalid native ID3 frame")
		}
		if len(r.Data) > MaxMetadata-10-n {
			return nil, errors.New("ID3 metadata exceeds limit")
		}
		n += 10 + len(r.Data)
	}
	out := make([]byte, n+10)
	copy(out, "ID3")
	out[3] = v.version
	copy(out[6:10], syncBytes(n))
	pos := 10
	for _, r := range v.frames {
		copy(out[pos:pos+4], r.Key)
		if v.version == 4 {
			copy(out[pos+4:pos+8], syncBytes(len(r.Data)))
		} else {
			be.PutUint32(out[pos+4:], uint32(len(r.Data)))
		}
		be.PutUint16(out[pos+8:], uint16(r.Flags))
		pos += 10
		pos += copy(out[pos:], r.Data)
	}
	return out, nil
}
func decodeText(enc byte, b []byte) string {
	switch enc {
	case 0:
		r := make([]rune, len(b))
		for i, v := range b {
			r[i] = rune(v)
		}
		return string(r)
	case 3:
		if !utf8.Valid(b) {
			return ""
		}
		return string(b)
	case 1, 2:
		little := false
		if len(b) >= 2 && enc == 1 {
			if b[0] == 255 && b[1] == 254 {
				little = true
				b = b[2:]
			} else if b[0] == 254 && b[1] == 255 {
				b = b[2:]
			}
		}
		u := make([]uint16, len(b)/2)
		for i := range u {
			if little {
				u[i] = le.Uint16(b[i*2:])
			} else {
				u[i] = be.Uint16(b[i*2:])
			}
		}
		return string(utf16.Decode(u))
	}
	return ""
}
func encodeText(version byte, s string) []byte {
	if version == 4 {
		return append([]byte{3}, []byte(s)...)
	}
	u := utf16.Encode([]rune(s))
	b := []byte{1, 255, 254}
	for _, v := range u {
		b = append(b, byte(v), byte(v>>8))
	}
	return b
}
func splitEncoded(enc byte, b []byte) ([]byte, []byte, bool) {
	step := 1
	if enc == 1 || enc == 2 {
		step = 2
	}
	for i := 0; i+step <= len(b); i += step {
		if b[i] == 0 && (step == 1 || b[i+1] == 0) {
			return b[:i], b[i+step:], true
		}
	}
	return b, nil, false
}
func (v *id3) key(r Raw) string {
	if r.Flags != 0 {
		return "raw:" + r.Key
	}
	if r.Key == "TXXX" && len(r.Data) > 0 {
		desc, _, _ := splitEncoded(r.Data[0], r.Data[1:])
		return canon(decodeText(r.Data[0], desc))
	}
	if r.Key == "COMM" {
		return "comment"
	}
	if r.Key == "USLT" {
		return "lyrics"
	}
	if r.Key == "TYER" {
		return "date"
	}
	for k, x := range id3Names {
		if x == r.Key {
			return k
		}
	}
	return "raw:" + r.Key
}
func (v *id3) text(r Raw) []string {
	if r.Flags != 0 || len(r.Data) == 0 {
		return nil
	}
	enc := r.Data[0]
	b := r.Data[1:]
	if r.Key == "TXXX" {
		_, b, _ = splitEncoded(enc, b)
	} else if r.Key == "COMM" || r.Key == "USLT" {
		if len(b) < 3 {
			return nil
		}
		_, b, _ = splitEncoded(enc, b[3:])
	} else if !strings.HasPrefix(r.Key, "T") {
		return nil
	}
	s := strings.TrimRight(decodeText(enc, b), "\x00")
	if v.version == 4 && r.Key != "COMM" && r.Key != "USLT" {
		return strings.Split(s, "\x00")
	}
	return []string{s}
}
func (v *id3) tags() map[string][]string {
	m := map[string][]string{}
	v.fieldTags(m)
	for _, r := range v.frames {
		key := v.key(r)
		text := v.text(r)
		if len(text) == 0 {
			continue
		}
		if key == "tracknumber" || key == "discnumber" {
			parts := strings.SplitN(text[0], "/", 2)
			m[key] = []string{parts[0]}
			if len(parts) == 2 {
				total := "tracktotal"
				if key == "discnumber" {
					total = "disctotal"
				}
				m[total] = []string{parts[1]}
			}
		} else {
			m[key] = append(m[key], text...)
		}
	}
	return m
}
func (v *id3) remove(k string) error {
	if handled, err := v.removeField(canon(k)); handled {
		return err
	}
	k = canon(k)
	if k == "tracktotal" || k == "disctotal" {
		number := "tracknumber"
		if k == "disctotal" {
			number = "discnumber"
		}
		m := v.tags()
		if n := m[number]; len(n) > 0 {
			return v.set(number, []string{strings.SplitN(n[0], "/", 2)[0]})
		}
		return nil
	}
	out := v.frames[:0]
	for _, r := range v.frames {
		if v.key(r) != k && !(strings.HasPrefix(k, "raw:") && r.Key == strings.TrimPrefix(k, "raw:")) {
			out = append(out, r)
		}
	}
	v.frames = out
	return nil
}
func (v *id3) set(k string, values []string) error {
	k = canon(k)
	if handled, err := v.setField(k, values); handled {
		return err
	}
	if strings.HasPrefix(k, "raw:") {
		id := strings.TrimPrefix(k, "raw:")
		if !validFrameID(id) || !strings.HasPrefix(id, "T") || id == "TXXX" {
			return errors.New("use native JSON import for structured/binary ID3 frames")
		}
		v.remove(k)
		v.frames = append(v.frames, Raw{id, 0, encodeText(v.version, strings.Join(values, "\x00"))})
		return nil
	}
	if k == "tracknumber" || k == "discnumber" || k == "tracktotal" || k == "disctotal" {
		if len(values) != 1 {
			return errors.New("track/disc fields require one value")
		}
		number, total := "tracknumber", "tracktotal"
		if strings.HasPrefix(k, "disc") {
			number, total = "discnumber", "disctotal"
		}
		pair := id3{version: v.version}
		for _, r := range v.frames {
			if r.Key != id3Names[number] && r.Key != "TXXX" {
				continue
			}
			key := v.key(r)
			if key == number || key == total {
				pair.frames = append(pair.frames, r)
			}
		}
		m := pair.tags()
		n, t := "0", ""
		if len(m[number]) > 0 {
			n = m[number][0]
		}
		if len(m[total]) > 0 {
			t = m[total][0]
		}
		if k == total {
			t = values[0]
		} else {
			parts := strings.SplitN(values[0], "/", 2)
			n = parts[0]
			if len(parts) == 2 {
				t = parts[1]
			}
		}
		for _, x := range []string{n, t} {
			if x != "" {
				if _, e := strconv.ParseUint(x, 10, 16); e != nil {
					return errors.New("invalid track/disc number")
				}
			}
		}
		s := n
		if t != "" {
			s += "/" + t
		}
		v.remove(number)
		id := id3Names[number]
		v.frames = append(v.frames, Raw{id, 0, encodeText(v.version, s)})
		return nil
	}
	if v.version == 3 && len(values) > 1 {
		return errors.New("ID3v2.3 multi-values are ambiguous; use v2.4 or one value")
	}
	id := id3Names[k]
	if v.version == 3 && k == "date" {
		id = "TYER"
	}
	if k == "comment" {
		id = "COMM"
	}
	if k == "lyrics" {
		id = "USLT"
	}
	s := strings.Join(values, "\x00")
	var data []byte
	if id == "COMM" || id == "USLT" {
		if len(values) != 1 {
			return errors.New("comment/lyrics require one value")
		}
		enc := encodeText(v.version, s)
		data = append([]byte{enc[0], 'e', 'n', 'g'}, enc[1:]...) // prepend empty description in same encoding, with BOM for v2.3.
		if enc[0] == 3 {
			data = append([]byte{3, 'e', 'n', 'g', 0}, []byte(s)...)
		} else {
			data = []byte{1, 'e', 'n', 'g', 255, 254, 0, 0}
			data = append(data, enc[1:]...)
		}
	} else if id != "" {
		data = encodeText(v.version, s)
	} else {
		id = "TXXX"
		desc := encodeText(v.version, k)
		val := encodeText(v.version, s)
		data = append(data, desc...)
		if desc[0] == 3 {
			data = append(data, 0)
		} else {
			data = append(data, 0, 0)
		}
		data = append(data, val[1:]...)
	}
	v.remove(k)
	v.frames = append(v.frames, Raw{id, 0, data})
	return nil
}
func (v *id3) replace(r []Raw) error {
	for _, x := range r {
		if !validFrameID(x.Key) || x.Flags > 65535 || len(x.Data) > MaxMetadata {
			return errors.New("invalid ID3 native frame")
		}
	}
	v.frames = cloneRaw(r)
	return nil
}
func (v *id3) pictures() ([]Picture, error) {
	var out []Picture
	for _, r := range v.frames {
		if r.Key != "APIC" {
			continue
		}
		if r.Flags != 0 {
			return nil, errors.New("encoded APIC requires native export")
		}
		b := r.Data
		if len(b) < 4 {
			return nil, errors.New("truncated APIC")
		}
		enc := b[0]
		mime, rest, ok := bytes.Cut(b[1:], []byte{0})
		if !ok || len(rest) < 1 {
			return nil, errors.New("invalid APIC MIME")
		}
		desc, data, ok := splitEncoded(enc, rest[1:])
		if !ok {
			return nil, errors.New("invalid APIC description")
		}
		out = append(out, Picture{string(mime), uint32(rest[0]), decodeText(enc, desc), bytes.Clone(data)})
	}
	return out, nil
}
func (v *id3) cover(p Picture) error {
	pics, e := v.pictures()
	if e != nil {
		return e
	}
	_ = pics
	out := v.frames[:0]
	for _, r := range v.frames {
		if r.Key == "APIC" {
			_, rest, _ := bytes.Cut(r.Data[1:], []byte{0})
			if len(rest) > 0 && rest[0] == 3 {
				continue
			}
		}
		out = append(out, r)
	}
	v.frames = out
	desc := encodeText(v.version, p.Description)
	b := []byte{desc[0]}
	b = append(b, []byte(p.MIME)...)
	b = append(b, 0, byte(p.Type))
	b = append(b, desc[1:]...)
	b = append(b, 0)
	if desc[0] != 3 {
		b = append(b, 0)
	}
	b = append(b, p.Data...)
	v.frames = append(v.frames, Raw{"APIC", 0, b})
	return nil
}
func (v *id3) chapters() ([]Chapter, error) {
	var out []Chapter
	for _, r := range v.frames {
		if r.Key != "CHAP" {
			continue
		}
		if r.Flags != 0 {
			return nil, errors.New("encoded CHAP requires native export")
		}
		_, b, ok := bytes.Cut(r.Data, []byte{0})
		if !ok || len(b) < 16 {
			return nil, errors.New("invalid CHAP")
		}
		c := Chapter{Start: float64(be.Uint32(b)) / 1000}
		end := be.Uint32(b[4:])
		if end != 0xffffffff {
			c.End = float64(end) / 1000
		}
		subHeader := []byte{'I', 'D', '3', v.version, 0, 0}
		subHeader = append(subHeader, syncBytes(len(b)-16)...)
		sub, e := parseID3(append(subHeader, b[16:]...))
		if e != nil {
			return nil, e
		}
		if titles := sub.tags()["title"]; len(titles) > 0 {
			c.Title = titles[0]
		}
		out = append(out, c)
	}
	sortChapters(out)
	return out, nil
}
func (v *id3) setChapters(ch []Chapter) error {
	out := v.frames[:0]
	for _, r := range v.frames {
		if r.Key != "CHAP" && r.Key != "CTOC" {
			out = append(out, r)
		}
	}
	v.frames = out
	if len(ch) > 255 {
		return errors.New("ID3 chapter table supports at most 255 entries")
	}
	toc := []byte("audiotag\x00")
	toc = append(toc, 3, byte(len(ch)))
	for i, c := range ch {
		if c.Start > 4294967 || c.End > 4294967 {
			return errors.New("ID3 chapter time exceeds 32-bit milliseconds")
		}
		id := fmt.Sprintf("ch%03d", i)
		toc = append(toc, []byte(id)...)
		toc = append(toc, 0)
		var b bytes.Buffer
		b.WriteString(id)
		b.WriteByte(0)
		put32(&b, uint32(c.Start*1000+0.5))
		end := c.End
		if end == 0 && i+1 < len(ch) {
			end = ch[i+1].Start
		}
		if end == 0 {
			put32(&b, 0xffffffff)
		} else {
			put32(&b, uint32(end*1000+0.5))
		}
		put32(&b, 0xffffffff)
		put32(&b, 0xffffffff)
		sub := id3{version: v.version}
		sub.set("title", []string{c.Title})
		data, e := sub.encode()
		if e != nil {
			return e
		}
		b.Write(data[10:])
		v.frames = append(v.frames, Raw{"CHAP", 0, b.Bytes()})
	}
	if len(ch) > 0 {
		v.frames = append(v.frames, Raw{"CTOC", 0, toc})
	}
	return nil
}

type mp3 struct {
	f *os.File
	id3
	offset, size int64
	end          int64
	legacy       []byte
}

func openMP3(f *os.File) (engine, error) {
	st, e := f.Stat()
	if e != nil {
		return nil, e
	}
	v := &mp3{f: f, size: st.Size(), end: st.Size(), id3: id3{version: 4}}
	if v.size >= 128 {
		b, err := readAt(f, v.size-128, 128)
		if err != nil {
			return nil, err
		}
		if string(b[:3]) == "TAG" {
			v.legacy = b
			v.end -= 128
		}
	}
	h := make([]byte, 10)
	n, _ := f.ReadAt(h, 0)
	if n >= 3 && string(h[:3]) == "ID3" {
		if n < 10 {
			return nil, errors.New("truncated ID3")
		}
		size, e := synchsafe(h[6:])
		if e != nil {
			return nil, e
		}
		b, e := readAt(f, 0, size+10)
		if e != nil {
			return nil, e
		}
		v.id3, e = parseID3View(b)
		if e != nil {
			return nil, e
		}
		v.offset = int64(size + 10)
	}
	audio := make([]byte, 2)
	if _, e = f.ReadAt(audio, v.offset); e != nil {
		return nil, e
	}
	if audio[0] != 255 || audio[1]&0xe0 != 0xe0 {
		return nil, errors.New("missing MPEG audio sync")
	}
	return v, nil
}
func (v *mp3) format() string {
	if v.version == 3 {
		return "mp3-id3v23"
	}
	return "mp3"
}
func (v *mp3) raw() []Raw {
	r := append([]Raw(nil), v.frames...)
	if len(v.legacy) > 0 {
		r = append(r, Raw{"@ID3V1", 0, v.legacy})
	}
	return r
}
func (v *mp3) replace(r []Raw) error {
	var frames []Raw
	var legacy []byte
	for _, x := range r {
		if x.Key == "@ID3V1" {
			if legacy != nil || len(x.Data) != 128 || string(x.Data[:3]) != "TAG" || x.Flags != 0 {
				return errors.New("invalid ID3v1 native tag")
			}
			legacy = bytes.Clone(x.Data)
		} else {
			frames = append(frames, x)
		}
	}
	if err := v.id3.replace(frames); err != nil {
		return err
	}
	v.legacy = legacy
	return nil
}
func (v *mp3) write(w io.Writer) error {
	b, e := v.encode()
	if e != nil {
		return e
	}
	if e = writeBytes(w, b); e != nil {
		return e
	}
	if err := copyRange(w, v.f, v.offset, v.end-v.offset); err != nil {
		return err
	}
	return writeBytes(w, v.legacy)
}
func (v *mp3) audio(h hash.Hash) error { return copyRange(h, v.f, v.offset, v.end-v.offset) }
