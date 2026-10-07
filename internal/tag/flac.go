package tag

import (
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"strconv"
)

type flac struct {
	f            *os.File
	blocks       []Raw
	c            comments
	dirty        bool
	offset, size int64
}

func openFLAC(f *os.File) (engine, error) {
	s, e := f.Stat()
	if e != nil {
		return nil, e
	}
	v := &flac{f: f, size: s.Size()}
	off := int64(4)
	total := 0
	for {
		h, e := readAt(f, off, 4)
		if e != nil {
			return nil, e
		}
		n := int(h[1])<<16 | int(h[2])<<8 | int(h[3])
		total += n
		if total > MaxMetadata {
			return nil, errors.New("FLAC metadata exceeds limit")
		}
		b, e := readAt(f, off+4, n)
		if e != nil {
			return nil, e
		}
		typ := h[0] & 127
		if len(v.blocks) == 0 && (typ != 0 || n != 34) {
			return nil, errors.New("invalid FLAC STREAMINFO")
		}
		if typ == 127 {
			return nil, errors.New("invalid FLAC block type")
		}
		v.blocks = append(v.blocks, Raw{strconv.Itoa(int(typ)), 0, b})
		off += int64(4 + n)
		if typ == 4 {
			v.c, e = parseCommentsView(b)
			if e != nil {
				return nil, e
			}
		}
		if h[0]&128 != 0 {
			break
		}
	}
	v.offset = off
	if v.c.vendor == "" {
		v.c.vendor = "audiotag"
	}
	return v, nil
}
func (v *flac) format() string { return "flac" }
func (v *flac) sync() {
	if !v.dirty {
		return
	}
	v.dirty = false
	data := v.c.encode()
	for i := range v.blocks {
		if v.blocks[i].Key == "4" {
			v.blocks[i].Data = data
			return
		}
	}
	v.blocks = append(v.blocks, Raw{"4", 0, data})
}
func (v *flac) raw() []Raw { v.sync(); return v.blocks }
func (v *flac) replace(r []Raw) error {
	if len(r) == 0 || r[0].Key != "0" || len(r[0].Data) != 34 {
		return errors.New("FLAC import needs STREAMINFO")
	}
	if string(r[0].Data) != string(v.blocks[0].Data) {
		return errors.New("cannot change FLAC stream properties")
	}
	count := 0
	c := comments{vendor: "audiotag"}
	for _, x := range r {
		n, e := strconv.Atoi(x.Key)
		if e != nil || n < 0 || n > 126 || len(x.Data) > 0xffffff || x.Flags != 0 {
			return errors.New("invalid FLAC metadata block")
		}
		if n == 4 {
			count++
			c, e = parseComments(x.Data)
			if e != nil {
				return e
			}
		}
	}
	if count > 1 {
		return errors.New("duplicate FLAC comments")
	}
	v.blocks = cloneRaw(r)
	v.c = c
	v.dirty = false
	return nil
}
func (v *flac) tags() map[string][]string { m := v.c.tags(); v.structuredTags(m); return m }
func (v *flac) set(k string, s []string) error {
	if handled, err := v.setStructured(canon(k), s); handled {
		return err
	}
	if e := v.c.set(k, s); e != nil {
		return e
	}
	v.dirty = true
	return nil
}
func (v *flac) remove(k string) error {
	if canon(k) == "cuesheet" {
		v.replaceBlock("5", nil)
		return nil
	}
	if canon(k) == "seektable" {
		v.replaceBlock("3", nil)
		return nil
	}
	v.c.remove(k)
	v.dirty = true
	return nil
}
func (v *flac) write(w io.Writer) error {
	v.sync()
	if e := writeBytes(w, []byte("fLaC")); e != nil {
		return e
	}
	for i, r := range v.blocks {
		n := len(r.Data)
		if n > 0xffffff {
			return errors.New("FLAC block exceeds 16 MiB")
		}
		t, _ := strconv.Atoi(r.Key)
		h := []byte{byte(t), byte(n >> 16), byte(n >> 8), byte(n)}
		if i == len(v.blocks)-1 {
			h[0] |= 128
		}
		if e := writeBytes(w, h); e != nil {
			return e
		}
		if e := writeBytes(w, r.Data); e != nil {
			return e
		}
	}
	return copyRange(w, v.f, v.offset, v.size-v.offset)
}
func (v *flac) audio(h hash.Hash) error { return copyRange(h, v.f, v.offset, v.size-v.offset) }
func (v *flac) pictures() ([]Picture, error) {
	var out []Picture
	for _, r := range v.blocks {
		if r.Key == "6" {
			p, e := parsePicture(r.Data)
			if e != nil {
				return nil, e
			}
			out = append(out, p)
		}
	}
	return out, nil
}
func (v *flac) cover(p Picture) error {
	var out []Raw
	for _, r := range v.blocks {
		if r.Key == "6" {
			old, e := parsePicture(r.Data)
			if e != nil {
				return fmt.Errorf("existing picture: %w", e)
			}
			if old.Type == 3 {
				continue
			}
		}
		out = append(out, r)
	}
	v.blocks = append(out, Raw{"6", 0, encodePicture(p)})
	return nil
}
func (v *flac) chapters() ([]Chapter, error) { return v.c.chapters() }
func (v *flac) setChapters(ch []Chapter) error {
	if e := v.c.setChapters(ch); e != nil {
		return e
	}
	v.dirty = true
	return nil
}
