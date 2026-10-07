package tag

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash"
	"io"
	"os"
	"strings"
)

type iffChunk struct {
	key       string
	off, size int64
	data      []byte
	edited    bool
}
type iff struct {
	f *os.File
	id3
	kind        string
	chunks      []iffChunk
	size        int64
	order       binary.ByteOrder
	magic, form string
}

func openIFF(f *os.File) (engine, error) {
	st, e := f.Stat()
	if e != nil {
		return nil, e
	}
	h, e := readAt(f, 0, 12)
	if e != nil {
		return nil, e
	}
	v := &iff{f: f, size: st.Size(), id3: id3{version: 4}, order: le, kind: "wav", magic: string(h[:4]), form: string(h[8:])}
	if v.magic == "FORM" {
		v.order = be
		v.kind = "aiff"
	}
	if int64(v.order.Uint32(h[4:]))+8 != v.size {
		return nil, errors.New("IFF size mismatch; RF64/trailing data unsupported")
	}
	seenID3 := false
	for off := int64(12); off < v.size; {
		h, e := readAt(f, off, 8)
		if e != nil {
			return nil, e
		}
		n := int64(v.order.Uint32(h[4:]))
		if n > v.size-off-8 {
			return nil, errors.New("truncated IFF chunk")
		}
		c := iffChunk{key: string(h[:4]), off: off + 8, size: n}
		if strings.EqualFold(c.key, "id3 ") {
			if seenID3 {
				return nil, errors.New("multiple IFF ID3 chunks")
			}
			seenID3 = true
			c.data, e = readAt(f, c.off, int(n))
			if e != nil {
				return nil, e
			}
			v.id3, e = parseID3View(c.data)
			if e != nil {
				return nil, e
			}
		}
		if broadcastChunk(c.key) || c.key == "LIST" || c.key == "NAME" || c.key == "AUTH" || c.key == "ANNO" || c.key == "(c) " {
			c.data, e = readAt(f, c.off, int(n))
			if e != nil {
				return nil, e
			}
		}
		v.chunks = append(v.chunks, c)
		off += 8 + n + n%2
		if off > v.size {
			return nil, errors.New("missing IFF pad byte")
		}
	}
	return v, nil
}
func (v *iff) format() string {
	suffix := ""
	if v.version == 3 {
		suffix = "-id3v23"
	}
	return v.kind + suffix
}
func (v *iff) raw() []Raw {
	r := append([]Raw(nil), v.frames...)
	for _, c := range v.chunks {
		if c.data != nil && !strings.EqualFold(c.key, "id3 ") {
			r = append(r, Raw{"@" + c.key, 0, c.data})
		}
	}
	return r
}
func (v *iff) replace(r []Raw) error {
	var frames []Raw
	extras := map[string][][]byte{}
	for _, x := range r {
		if strings.HasPrefix(x.Key, "@") {
			key := strings.TrimPrefix(x.Key, "@")
			if key != "LIST" && key != "NAME" && key != "AUTH" && key != "ANNO" && key != "(c) " && !broadcastChunk(key) || x.Flags != 0 {
				return errors.New("unsupported native IFF chunk")
			}
			extras[key] = append(extras[key], bytes.Clone(x.Data))
		} else {
			frames = append(frames, x)
		}
	}
	if e := v.id3.replace(frames); e != nil {
		return e
	}
	var out []iffChunk
	for _, c := range v.chunks {
		if c.data != nil && !strings.EqualFold(c.key, "id3 ") {
			continue
		}
		out = append(out, c)
	} // stable native chunk order from snapshot.
	for _, x := range r {
		if strings.HasPrefix(x.Key, "@") {
			out = append(out, iffChunk{key: x.Key[1:], data: bytes.Clone(x.Data), size: int64(len(x.Data)), edited: true})
		}
	}
	v.chunks = out
	return nil
}

var riffNames = map[string]string{"title": "INAM", "artist": "IART", "album": "IPRD", "comment": "ICMT", "genre": "IGNR", "date": "ICRD", "copyright": "ICOP", "encodedby": "ISFT"}

func riffInfo(b []byte) ([]Raw, error) {
	if len(b) < 4 || string(b[:4]) != "INFO" {
		return nil, nil
	}
	var r []Raw
	for pos := 4; pos < len(b); {
		if len(b)-pos < 8 {
			return nil, errors.New("truncated RIFF INFO")
		}
		n := int(le.Uint32(b[pos+4:]))
		key := string(b[pos : pos+4])
		pos += 8
		if n > len(b)-pos {
			return nil, errors.New("truncated RIFF INFO value")
		}
		r = append(r, Raw{key, 0, bytes.Clone(b[pos : pos+n])})
		pos += n + n%2
		if pos > len(b) {
			return nil, errors.New("missing RIFF INFO padding")
		}
	}
	return r, nil
}
func (v *iff) tags() map[string][]string {
	m := v.id3.tags()
	v.broadcastTags(m)
	for _, c := range v.chunks {
		if c.key == "LIST" {
			r, e := riffInfo(c.data)
			if e != nil {
				continue
			}
			for _, x := range r {
				key := "riff:" + x.Key
				for k, n := range riffNames {
					if n == x.Key {
						key = k
					}
				}
				if len(m[key]) == 0 {
					m[key] = []string{strings.TrimRight(string(x.Data), "\x00")}
				}
			}
		} else {
			key := ""
			switch c.key {
			case "NAME":
				key = "title"
			case "AUTH":
				key = "artist"
			case "ANNO":
				key = "comment"
			case "(c) ":
				key = "copyright"
			}
			if key != "" && len(m[key]) == 0 {
				m[key] = []string{strings.TrimRight(string(c.data), "\x00")}
			}
		}
	}
	return m
}
func (v *iff) editLegacy(k string, values []string) error {
	k = canon(k)
	native := riffNames[k]
	if strings.HasPrefix(k, "riff:") {
		native = strings.TrimPrefix(k, "riff:")
		if len(native) != 4 {
			return errors.New("RIFF INFO key must be four bytes")
		}
	}
	for i := range v.chunks {
		c := &v.chunks[i]
		if c.key == "LIST" && native != "" {
			r, e := riffInfo(c.data)
			if e != nil {
				return e
			}
			if len(c.data) < 4 || string(c.data[:4]) != "INFO" {
				continue
			}
			var out []Raw
			for _, x := range r {
				if x.Key != native {
					out = append(out, x)
				}
			}
			if len(values) > 0 {
				out = append(out, Raw{native, 0, append([]byte(strings.Join(values, "; ")), 0)})
			}
			var b bytes.Buffer
			b.WriteString("INFO")
			for _, x := range out {
				b.WriteString(x.Key)
				putLE32(&b, uint32(len(x.Data)))
				b.Write(x.Data)
				if len(x.Data)%2 != 0 {
					b.WriteByte(0)
				}
			}
			c.data = b.Bytes()
			c.size = int64(len(c.data))
			c.edited = true
		}
		if v.kind == "aiff" {
			key := ""
			switch k {
			case "title":
				key = "NAME"
			case "artist":
				key = "AUTH"
			case "comment":
				key = "ANNO"
			case "copyright":
				key = "(c) "
			}
			if key != "" && c.key == key {
				c.data = []byte(strings.Join(values, "; "))
				c.size = int64(len(c.data))
				c.edited = true
			}
		}
	}
	return nil
}
func (v *iff) set(k string, s []string) error {
	if handled, err := v.setIXMLPath(canon(k), s); handled {
		return err
	}
	if handled, err := v.setBroadcast(canon(k), s); handled {
		return err
	}
	if e := v.editLegacy(k, s); e != nil {
		return e
	}
	if strings.HasPrefix(k, "riff:") {
		return errors.New("use native JSON import for arbitrary RIFF INFO chunks")
	}
	return v.id3.set(k, s)
}
func (v *iff) remove(k string) error {
	if handled, err := v.removeIXMLPath(canon(k)); handled {
		return err
	}
	if handled, err := v.removeBroadcast(canon(k)); handled {
		return err
	}
	if e := v.editLegacy(k, nil); e != nil {
		return e
	}
	return v.id3.remove(k)
}
func (v *iff) write(w io.Writer) error {
	b, e := v.id3.encode()
	if e != nil {
		return e
	}
	chunks := append([]iffChunk(nil), v.chunks...)
	found := false
	for i := range chunks {
		if strings.EqualFold(chunks[i].key, "id3 ") {
			chunks[i].data = b
			chunks[i].size = int64(len(b))
			chunks[i].edited = true
			found = true
		}
	}
	if !found && len(v.frames) > 0 {
		key := "id3 "
		if v.kind == "aiff" {
			key = "ID3 "
		}
		chunks = append(chunks, iffChunk{key: key, data: b, size: int64(len(b)), edited: true})
	}
	size := int64(4)
	for _, c := range chunks {
		size += 8 + c.size + c.size%2
	}
	if size > 0xffffffff {
		return errors.New("IFF exceeds 4 GiB; RF64 unsupported")
	}
	h := make([]byte, 12)
	copy(h, v.magic)
	v.order.PutUint32(h[4:], uint32(size))
	copy(h[8:], v.form)
	if e = writeBytes(w, h); e != nil {
		return e
	}
	for _, c := range chunks {
		head := make([]byte, 8)
		copy(head, c.key)
		v.order.PutUint32(head[4:], uint32(c.size))
		if e = writeBytes(w, head); e != nil {
			return e
		}
		if c.edited {
			e = writeBytes(w, c.data)
		} else {
			e = copyRange(w, v.f, c.off, c.size)
		}
		if e != nil {
			return e
		}
		if c.size%2 != 0 {
			if e = writeBytes(w, []byte{0}); e != nil {
				return e
			}
		}
	}
	return nil
}
func (v *iff) audio(h hash.Hash) error {
	for _, c := range v.chunks {
		if c.key == "data" || c.key == "SSND" || c.key == "fmt " || c.key == "COMM" {
			h.Write([]byte(c.key))
			if e := copyRange(h, v.f, c.off, c.size); e != nil {
				return e
			}
		}
	}
	return nil
}
