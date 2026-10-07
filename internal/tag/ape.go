package tag

import (
	"bytes"
	"errors"
	"hash"
	"io"
	"os"
	"strings"
	"unicode/utf8"
)

type ape struct {
	f         *os.File
	items     []Raw
	end, size int64
	id3v1     []byte
}

func openAPE(f *os.File) (engine, error) {
	st, e := f.Stat()
	if e != nil {
		return nil, e
	}
	v := &ape{f: f, size: st.Size(), end: st.Size()}
	head, e := readAt(f, 0, 4)
	if e != nil {
		return nil, e
	}
	if string(head) != "MAC " && string(head) != "wvpk" && string(head) != "MPCK" && string(head[:3]) != "MP+" {
		return nil, errors.New("unsupported APEv2 audio signature")
	}
	if v.end >= 128 {
		b, e := readAt(f, v.end-128, 128)
		if e != nil {
			return nil, e
		}
		if string(b[:3]) == "TAG" {
			v.id3v1 = b
			v.end -= 128
		}
	}
	if v.end < 32 {
		return v, nil
	}
	footer, e := readAt(f, v.end-32, 32)
	if e != nil {
		return nil, e
	}
	if string(footer[:8]) != "APETAGEX" {
		return v, nil
	}
	if le.Uint32(footer[8:]) != 2000 {
		return nil, errors.New("only APEv2 is supported")
	}
	size := int64(le.Uint32(footer[12:]))
	count := int(le.Uint32(footer[16:]))
	flags := le.Uint32(footer[20:])
	if size < 32 || size > MaxMetadata || size > v.end {
		return nil, errors.New("invalid APEv2 size")
	}
	data, e := readAt(f, v.end-size, int(size-32))
	if e != nil {
		return nil, e
	}
	if count > len(data)/9 {
		return nil, errors.New("invalid APE item count")
	}
	pos := 0
	for i := 0; i < count; i++ {
		if len(data)-pos < 9 {
			return nil, errors.New("truncated APE item")
		}
		n := int(le.Uint32(data[pos:]))
		flag := le.Uint32(data[pos+4:])
		pos += 8
		idx := bytes.IndexByte(data[pos:], 0)
		if idx < 0 {
			return nil, errors.New("missing APE key terminator")
		}
		key := string(data[pos : pos+idx])
		pos += idx + 1
		if !validAPEKey(key) || n > len(data)-pos {
			return nil, errors.New("invalid APE item")
		}
		v.items = append(v.items, Raw{key, flag, bytes.Clone(data[pos : pos+n])})
		pos += n
	}
	if pos != len(data) {
		return nil, errors.New("trailing APE item data")
	}
	v.end -= size
	if flags&0x80000000 != 0 {
		if v.end < 32 {
			return nil, errors.New("missing APE header")
		}
		h, e := readAt(f, v.end-32, 32)
		if e != nil || string(h[:8]) != "APETAGEX" {
			return nil, errors.New("invalid APE header")
		}
		v.end -= 32
	}
	return v, nil
}
func validAPEKey(k string) bool {
	if len(k) < 2 || len(k) > 255 {
		return false
	}
	for _, c := range k {
		if c < 32 || c > 126 || c == '=' {
			return false
		}
	}
	return true
}
func (v *ape) format() string { return "ape" }
func (v *ape) raw() []Raw     { return v.items }
func (v *ape) replace(r []Raw) error {
	seen := map[string]bool{}
	for _, x := range r {
		if !validAPEKey(x.Key) || x.Flags&^uint32(7) != 0 {
			return errors.New("invalid APEv2 item")
		}
		k := strings.ToLower(x.Key)
		if seen[k] {
			return errors.New("duplicate APEv2 key")
		}
		seen[k] = true
	}
	v.items = cloneRaw(r)
	return nil
}
func (v *ape) tags() map[string][]string {
	m := map[string][]string{}
	for _, r := range v.items {
		if r.Flags&6 == 0 && utf8.Valid(r.Data) {
			m[canon(r.Key)] = strings.Split(string(r.Data), "\x00")
		}
	}
	return m
}
func (v *ape) remove(k string) error {
	out := v.items[:0]
	for _, r := range v.items {
		if canon(r.Key) == canon(k) {
			if r.Flags&1 != 0 {
				return errors.New("APE tag is read-only")
			}
			continue
		}
		out = append(out, r)
	}
	v.items = out
	return nil
}
func (v *ape) set(k string, s []string) error {
	k = canon(k)
	if !validAPEKey(k) {
		return errors.New("invalid APE key")
	}
	if e := v.remove(k); e != nil {
		return e
	}
	v.items = append(v.items, Raw{k, 0, []byte(strings.Join(s, "\x00"))})
	return nil
}
func (v *ape) write(w io.Writer) error {
	if e := copyRange(w, v.f, 0, v.end); e != nil {
		return e
	}
	var b bytes.Buffer
	for _, r := range v.items {
		putLE32(&b, uint32(len(r.Data)))
		putLE32(&b, r.Flags)
		b.WriteString(r.Key)
		b.WriteByte(0)
		b.Write(r.Data)
	}
	if b.Len() > MaxMetadata-32 {
		return errors.New("APE metadata exceeds limit")
	}
	if len(v.items) > 0 {
		if e := writeBytes(w, b.Bytes()); e != nil {
			return e
		}
		h := make([]byte, 32)
		copy(h, "APETAGEX")
		le.PutUint32(h[8:], 2000)
		le.PutUint32(h[12:], uint32(b.Len()+32))
		le.PutUint32(h[16:], uint32(len(v.items)))
		if e := writeBytes(w, h); e != nil {
			return e
		}
	}
	return writeBytes(w, v.id3v1)
}
func (v *ape) audio(h hash.Hash) error { return copyRange(h, v.f, 0, v.end) }
func (v *ape) pictures() ([]Picture, error) {
	var out []Picture
	for _, r := range v.items {
		if !strings.HasPrefix(strings.ToLower(r.Key), "cover art (") {
			continue
		}
		_, data, ok := bytes.Cut(r.Data, []byte{0})
		if !ok {
			return nil, errors.New("invalid APE cover")
		}
		mime := "image/jpeg"
		if bytes.HasPrefix(data, []byte{137, 'P', 'N', 'G'}) {
			mime = "image/png"
		}
		typ := uint32(0)
		if strings.EqualFold(r.Key, "Cover Art (Front)") {
			typ = 3
		}
		out = append(out, Picture{MIME: mime, Type: typ, Data: bytes.Clone(data)})
	}
	return out, nil
}
func (v *ape) cover(p Picture) error {
	if e := v.remove("Cover Art (Front)"); e != nil {
		return e
	}
	name := "cover.jpg"
	if p.MIME == "image/png" {
		name = "cover.png"
	}
	v.items = append(v.items, Raw{"Cover Art (Front)", 2, append(append([]byte(name), 0), p.Data...)})
	return nil
}
func (v *ape) chapters() ([]Chapter, error) {
	c := comments{}
	for _, r := range v.items {
		if r.Flags&6 == 0 {
			for _, s := range strings.Split(string(r.Data), "\x00") {
				c.values = append(c.values, Raw{r.Key, 0, []byte(s)})
			}
		}
	}
	return c.chapters()
}
func (v *ape) setChapters(ch []Chapter) error {
	c := comments{}
	if e := c.setChapters(ch); e != nil {
		return e
	}
	var out []Raw
	for _, r := range v.items {
		if !isChapterKey(strings.ToUpper(r.Key)) {
			out = append(out, r)
		} else if r.Flags&1 != 0 {
			return errors.New("APE chapter tag is read-only")
		}
	}
	v.items = append(out, c.values...)
	return nil
}
