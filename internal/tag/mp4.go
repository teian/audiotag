package tag

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

type atom struct {
	generated         bool
	typ               string
	data              []byte
	children          []*atom
	prefix            []byte
	off, size, header int64
}

var containers = map[string]bool{"moov": true, "trak": true, "mdia": true, "minf": true, "stbl": true, "udta": true, "meta": true, "ilst": true, "tref": true, "edts": true}

func parseAtoms(b []byte, depth int) ([]*atom, error) {
	return parseAtomsMode(b, depth, true)
}

// Views are used only for inspection or buffers owned by the caller.
func parseAtomsView(b []byte, depth int) ([]*atom, error) {
	return parseAtomsMode(b, depth, false)
}
func parseAtomsMode(b []byte, depth int, owned bool) ([]*atom, error) {
	if depth > 24 {
		return nil, errors.New("MP4 nesting exceeds limit")
	}
	var out []*atom
	for pos := 0; pos < len(b); {
		if len(b)-pos < 8 {
			return nil, errors.New("truncated MP4 atom")
		}
		n := uint64(be.Uint32(b[pos:]))
		head := 8
		if n == 1 {
			if len(b)-pos < 16 {
				return nil, errors.New("truncated MP4 extended size")
			}
			n = be.Uint64(b[pos+8:])
			head = 16
		} else if n == 0 {
			n = uint64(len(b) - pos)
		}
		if n < uint64(head) || n > uint64(len(b)-pos) {
			return nil, errors.New("invalid MP4 atom size")
		}
		a := &atom{typ: string(b[pos+4 : pos+8]), data: b[pos+head : pos+int(n)]}
		if containers[a.typ] {
			body := a.data
			if a.typ == "meta" {
				if len(body) < 4 {
					return nil, errors.New("truncated meta fullbox")
				}
				a.prefix = body[:4]
				if owned {
					a.prefix = bytes.Clone(a.prefix)
				}
				body = body[4:]
			}
			c, e := parseAtomsMode(body, depth+1, owned)
			if e != nil {
				return nil, e
			}
			a.children = c
			a.data = nil
		} else if owned {
			a.data = bytes.Clone(a.data)
		}
		out = append(out, a)
		pos += int(n)
	}
	return out, nil
}
func (a *atom) encodedSize() int {
	n := 8
	if a.children == nil {
		return n + len(a.data)
	}
	n += len(a.prefix)
	for _, c := range a.children {
		n += c.encodedSize()
	}
	return n
}
func (a *atom) appendEncoded(b []byte) []byte {
	start := len(b)
	b = append(b, make([]byte, 8)...)
	copy(b[start+4:], a.typ)
	if a.children == nil {
		b = append(b, a.data...)
	} else {
		b = append(b, a.prefix...)
		for _, c := range a.children {
			b = c.appendEncoded(b)
		}
	}
	be.PutUint32(b[start:], uint32(len(b)-start))
	return b
}
func (a *atom) payload() []byte {
	if a.children == nil {
		return a.data
	}
	b := make([]byte, 0, a.encodedSize()-8)
	b = append(b, a.prefix...)
	for _, c := range a.children {
		b = c.appendEncoded(b)
	}
	return b
}
func (a *atom) encode() []byte { return a.appendEncoded(make([]byte, 0, a.encodedSize())) }
func (a *atom) find(typ string) *atom {
	for _, c := range a.children {
		if c.typ == typ {
			return c
		}
	}
	return nil
}
func (a *atom) ensure(typ string) *atom {
	if c := a.find(typ); c != nil {
		return c
	}
	c := &atom{typ: typ, children: []*atom{}}
	if typ == "meta" {
		c.prefix = make([]byte, 4)
		hdlr := make([]byte, 25)
		copy(hdlr[8:], "mdir")
		c.children = append(c.children, &atom{typ: "hdlr", data: hdlr})
	}
	a.children = append(a.children, c)
	return c
}
func (a *atom) remove(typ string) {
	var out []*atom
	for _, c := range a.children {
		if c.typ != typ {
			out = append(out, c)
		}
	}
	a.children = out
}
func atomKey(typ string) string {
	var b strings.Builder
	for _, v := range []byte(typ) {
		b.WriteRune(rune(v))
	}
	return b.String()
}
func keyAtom(k string) (string, error) {
	r := []rune(k)
	if len(r) != 4 {
		return "", errors.New("MP4 atom key must contain four Latin-1 characters")
	}
	b := make([]byte, 4)
	for i, c := range r {
		if c > 255 {
			return "", errors.New("invalid MP4 atom key")
		}
		b[i] = byte(c)
	}
	return string(b), nil
}

var mp4Names = map[string]string{"title": "\xa9nam", "album": "\xa9alb", "artist": "\xa9ART", "albumartist": "aART", "composer": "\xa9wrt", "genre": "\xa9gen", "date": "\xa9day", "comment": "\xa9cmt", "lyrics": "\xa9lyr", "copyright": "cprt", "encodedby": "\xa9too", "grouping": "\xa9grp", "description": "desc", "longdescription": "ldes", "titlesort": "sonm", "albumsort": "soal", "artistsort": "soar", "albumartistsort": "soaa", "composersort": "soco", "purchasedate": "purd", "podcasturl": "purl", "podcastguid": "egid", "tvshow": "tvsh", "tvnetwork": "tvnn", "tvepisodeid": "tven", "category": "catg", "keywords": "keyw"}

type mp4 struct {
	quickChapters []Chapter
	f             *os.File
	top           []*atom
	moov          *atom
	size          int64
}

func openMP4(f *os.File) (engine, error) {
	st, e := f.Stat()
	if e != nil {
		return nil, e
	}
	v := &mp4{f: f, size: st.Size()}
	for off := int64(0); off < v.size; {
		h, e := readAt(f, off, 8)
		if e != nil {
			return nil, e
		}
		size := uint64(be.Uint32(h))
		head := int64(8)
		if size == 1 {
			ex, e := readAt(f, off+8, 8)
			if e != nil {
				return nil, e
			}
			size = be.Uint64(ex)
			head = 16
		} else if size == 0 {
			size = uint64(v.size - off)
		}
		if size < uint64(head) || size > uint64(v.size-off) {
			return nil, errors.New("invalid MP4 top-level atom size")
		}
		a := &atom{typ: string(h[4:]), off: off, size: int64(size), header: head}
		if a.typ == "moof" || a.typ == "sidx" || a.typ == "mfra" {
			return nil, errors.New("fragmented/indexed MP4 is unsupported")
		}
		if a.typ == "moov" {
			if v.moov != nil {
				return nil, errors.New("multiple moov atoms")
			}
			b, e := readAt(f, off+head, int(size)-int(head))
			if e != nil {
				return nil, e
			}
			a.children, e = parseAtomsView(b, 0)
			if e != nil {
				return nil, e
			}
			v.moov = a
		}
		v.top = append(v.top, a)
		off += int64(size)
	}
	if v.moov == nil {
		return nil, errors.New("MP4 has no moov")
	}
	if e = walkAtoms(v.moov, func(a *atom) error {
		if a.typ == "stsd" {
			if len(a.data) < 8 {
				return errors.New("truncated MP4 sample descriptions")
			}
			entries, e := parseAtomsView(a.data[8:], 0)
			if e != nil {
				return e
			}
			if len(entries) != int(be.Uint32(a.data[4:])) {
				return errors.New("invalid MP4 sample description count")
			}
			for _, entry := range entries {
				if entry.typ == "enca" || entry.typ == "encv" {
					return errors.New("encrypted MP4 is unsupported")
				}
			}
		}
		if a.typ == "saio" || a.typ == "iloc" || a.typ == "mvex" {
			return errors.New("unsupported MP4 offset/encryption layout")
		}
		return nil
	}); e != nil {
		return nil, e
	}
	return v, nil
}
func walkAtoms(a *atom, fn func(*atom) error) error {
	if e := fn(a); e != nil {
		return e
	}
	for _, c := range a.children {
		if e := walkAtoms(c, fn); e != nil {
			return e
		}
	}
	return nil
}
func (v *mp4) format() string { return "mp4" }
func (v *mp4) ilst(create bool) *atom {
	u := v.moov.find("udta")
	if u == nil && !create {
		return nil
	}
	if u == nil {
		u = v.moov.ensure("udta")
	}
	m := u.find("meta")
	if m == nil && !create {
		return nil
	}
	if m == nil {
		m = u.ensure("meta")
	}
	i := m.find("ilst")
	if i == nil && create {
		i = m.ensure("ilst")
	}
	return i
}
func mp4ItemKey(a *atom) string {
	if a.typ != "----" {
		return atomKey(a.typ)
	}
	c, e := parseAtomsView(a.data, 0)
	if e != nil {
		return "----"
	}
	mean, name := "", ""
	for _, x := range c {
		if len(x.data) >= 4 {
			switch x.typ {
			case "mean":
				mean = string(x.data[4:])
			case "name":
				name = string(x.data[4:])
			}
		}
	}
	return "----:" + mean + ":" + name
}
func (v *mp4) raw() []Raw {
	var r []Raw
	if i := v.ilst(false); i != nil {
		for _, a := range i.children {
			r = append(r, Raw{mp4ItemKey(a), 0, a.payload()})
		}
	}
	if u := v.moov.find("udta"); u != nil {
		if c := u.find("chpl"); c != nil {
			r = append(r, Raw{"@chpl", 0, c.data})
		}
	}
	r = append(r, v.quickRaw()...)
	return r
}
func (v *mp4) replace(r []Raw) error {
	var items []*atom
	var chpl []byte
	var quick []Chapter
	quickSeen := false
	for _, x := range r {
		if x.Flags != 0 || len(x.Data) > MaxMetadata {
			return errors.New("invalid native MP4 metadata")
		}
		if x.Key == "@qtchapters" {
			if quickSeen {
				return errors.New("duplicate QuickTime chapters")
			}
			quickSeen = true
			if err := json.Unmarshal(x.Data, &quick); err != nil {
				return err
			}
			continue
		}
		if x.Key == "@chpl" {
			if chpl != nil {
				return errors.New("duplicate chpl")
			}
			if _, e := parseChpl(x.Data); e != nil {
				return e
			}
			chpl = bytes.Clone(x.Data)
			continue
		}
		typ := "----"
		var e error
		if !strings.HasPrefix(x.Key, "----:") {
			typ, e = keyAtom(x.Key)
			if e != nil {
				return e
			}
		}
		if _, e = parseAtomsView(x.Data, 0); e != nil {
			return fmt.Errorf("invalid ilst item: %w", e)
		}
		a := &atom{typ: typ, data: bytes.Clone(x.Data)}
		if mp4ItemKey(a) != x.Key {
			return errors.New("native MP4 item key disagrees with payload")
		}
		items = append(items, a)
	}
	v.ilst(true).children = items
	u := v.moov.ensure("udta")
	u.remove("chpl")
	if chpl != nil {
		u.children = append(u.children, &atom{typ: "chpl", data: chpl})
	}
	if quickSeen {
		t, err := v.quickTrack()
		if err != nil {
			return err
		}
		if t == nil {
			return errors.New("native QuickTime chapters require an existing chapter track")
		}
		return v.setQuick(t, quick)
	}
	return nil
}
func mp4Data(a *atom) ([]Raw, error) {
	c, e := parseAtomsView(a.payload(), 0)
	if e != nil {
		return nil, e
	}
	var out []Raw
	for _, x := range c {
		if x.typ == "data" {
			if len(x.data) < 8 {
				return nil, errors.New("truncated MP4 data atom")
			}
			out = append(out, Raw{"data", be.Uint32(x.data), x.data[8:]})
		}
	}
	return out, nil
}
func (v *mp4) tags() map[string][]string {
	m := map[string][]string{}
	i := v.ilst(false)
	if i == nil {
		return m
	}
	for _, a := range i.children {
		key := mp4ItemKey(a)
		name := "raw:" + key
		for k, spec := range mp4Integers {
			if a.typ == spec.atom {
				name = k
			}
		}
		for k, t := range mp4Names {
			if a.typ == t {
				name = k
			}
		}
		if strings.HasPrefix(key, "----:com.apple.iTunes:") {
			name = canon(strings.TrimPrefix(key, "----:com.apple.iTunes:"))
		}
		data, e := mp4Data(a)
		if e != nil {
			continue
		}
		for _, d := range data {
			switch {
			case a.typ == "trkn" || a.typ == "disk":
				if len(d.Data) >= 6 {
					number, total := "tracknumber", "tracktotal"
					if a.typ == "disk" {
						number, total = "discnumber", "disctotal"
					}
					m[number] = []string{strconv.Itoa(int(be.Uint16(d.Data[2:])))}
					m[total] = []string{strconv.Itoa(int(be.Uint16(d.Data[4:])))}
				}
			case d.Flags == 1 && utf8.Valid(d.Data):
				m[name] = append(m[name], string(d.Data))
			case d.Flags == 21 && len(d.Data) > 0 && len(d.Data) <= 8:
				var n uint64
				for _, b := range d.Data {
					n = n<<8 | uint64(b)
				}
				m[name] = append(m[name], strconv.FormatUint(n, 10))
				if name != "raw:"+key {
					if _, ok := mp4Integers[name]; ok {
						m["raw:"+key] = append(m["raw:"+key], strconv.FormatUint(n, 10))
					}
				}
			}
		}
	}
	return m
}
func mp4NativeKey(k string) string {
	k = canon(k)
	if spec, ok := mp4Integers[k]; ok {
		return spec.atom
	}
	if t := mp4Names[k]; t != "" {
		return atomKey(t)
	}
	switch k {
	case "tracknumber", "tracktotal":
		return "trkn"
	case "discnumber", "disctotal":
		return "disk"
	}
	if strings.HasPrefix(k, "raw:") {
		return strings.TrimPrefix(k, "raw:")
	}
	return "----:com.apple.iTunes:" + k
}
func (v *mp4) remove(k string) error {
	key := mp4NativeKey(k)
	i := v.ilst(false)
	if i == nil {
		return nil
	}
	var out []*atom
	for _, a := range i.children {
		if mp4ItemKey(a) != key {
			out = append(out, a)
		}
	}
	i.children = out
	return nil
}
func dataAtom(typ uint32, b []byte) *atom {
	p := make([]byte, 8+len(b))
	be.PutUint32(p, typ)
	copy(p[8:], b)
	return &atom{typ: "data", data: p}
}
func (v *mp4) set(k string, values []string) error {
	k = canon(k)
	if handled, err := v.setInteger(k, values); handled {
		return err
	}
	key := mp4NativeKey(k)
	typ := "----"
	var e error
	var children []*atom
	if strings.HasPrefix(key, "----:") {
		parts := strings.SplitN(key, ":", 3)
		if len(parts) != 3 || parts[1] == "" || parts[2] == "" {
			return errors.New("invalid MP4 freeform key")
		}
		children = append(children, &atom{typ: "mean", data: append(make([]byte, 4), []byte(parts[1])...)}, &atom{typ: "name", data: append(make([]byte, 4), []byte(parts[2])...)})
	} else {
		typ, e = keyAtom(key)
		if e != nil {
			return e
		}
	}
	if key == "trkn" || key == "disk" {
		if len(values) != 1 {
			return errors.New("track/disc fields require one value")
		}
		total := "tracktotal"
		if key == "disk" {
			total = "disctotal"
		}
		n, t := "0", "0"
		if items := v.ilst(false); items != nil {
			for _, item := range items.children {
				if item.typ != typ {
					continue
				}
				data, e := mp4Data(item)
				if e != nil {
					continue
				}
				for _, d := range data {
					if len(d.Data) >= 6 {
						n = strconv.Itoa(int(be.Uint16(d.Data[2:])))
						t = strconv.Itoa(int(be.Uint16(d.Data[4:])))
					}
				}
			}
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
		nn, e := strconv.ParseUint(n, 10, 16)
		if e != nil {
			return e
		}
		tt, e := strconv.ParseUint(t, 10, 16)
		if e != nil {
			return e
		}
		b := make([]byte, 8)
		be.PutUint16(b[2:], uint16(nn))
		be.PutUint16(b[4:], uint16(tt))
		children = append(children, dataAtom(0, b))
	} else if strings.HasPrefix(k, "raw:") && !strings.HasPrefix(key, "----:") {
		return errors.New("typed MP4 atoms require native JSON import; use normalized names for text")
	} else {
		for _, s := range values {
			p := make([]byte, 8+len(s))
			be.PutUint32(p, 1)
			copy(p[8:], s)
			children = append(children, &atom{typ: "data", data: p})
		}
	}
	v.remove(k)
	size := 0
	for _, c := range children {
		size += c.encodedSize()
	}
	a := &atom{typ: typ, data: make([]byte, 0, size)}
	for _, c := range children {
		a.data = c.appendEncoded(a.data)
	}
	v.ilst(true).children = append(v.ilst(true).children, a)
	return nil
}
func (v *mp4) audio(h hash.Hash) error {
	if t, err := v.quickTrack(); err != nil {
		return err
	} else if t != nil {
		return v.hashQuickMedia(h, t)
	}
	for _, a := range v.top {
		if a.typ == "mdat" {
			if e := copyRange(h, v.f, a.off+a.header, a.size-a.header); e != nil {
				return e
			}
		}
	}
	return nil
}
func (v *mp4) write(w io.Writer) error {
	// Serialize a copy: chunk-offset adjustments must not mutate the editable model.
	encoded := v.moov.encode()
	parsed, e := parseAtomsView(encoded, 0)
	if e != nil {
		return e
	}
	moov := parsed[0]
	newMoovSize := int64(len(encoded))
	type shift struct{ start, end, delta int64 }
	var ranges []shift
	newOff := int64(0)
	for _, a := range v.top {
		if a.typ == "mdat" {
			ranges = append(ranges, shift{a.off + a.header, a.off + a.size, newOff - a.off})
		}
		if a == v.moov {
			newOff += newMoovSize
		} else {
			newOff += a.size
		}
	}
	e = walkAtoms(moov, func(a *atom) error {
		if a.typ != "stco" && a.typ != "co64" {
			return nil
		}
		if len(a.data) < 8 {
			return errors.New("truncated chunk offset table")
		}
		count := uint64(be.Uint32(a.data[4:]))
		width := 8
		if a.typ == "stco" {
			width = 4
		}
		if count > uint64((len(a.data)-8)/width) || 8+int(count)*width != len(a.data) {
			return errors.New("invalid chunk offset count")
		}
		for i := 0; i < int(count); i++ {
			p := a.data[8+i*width:]
			old := uint64(be.Uint32(p))
			if width == 8 {
				old = be.Uint64(p)
			}
			found := false
			for _, r := range ranges {
				if old >= uint64(r.start) && old < uint64(r.end) {
					next := int64(old) + r.delta
					if next < 0 || width == 4 && uint64(next) > 0xffffffff {
						return errors.New("MP4 stco overflow; co64 conversion required")
					}
					if width == 4 {
						be.PutUint32(p, uint32(next))
					} else {
						be.PutUint64(p, uint64(next))
					}
					found = true
					break
				}
			}
			if !found {
				return errors.New("MP4 chunk offset outside local mdat; external media unsupported")
			}
		}
		return nil
	})
	if e != nil {
		return e
	}
	for _, a := range v.top {
		if a == v.moov {
			if e = writeBytes(w, encoded); e != nil {
				return e
			}
		} else if a.generated {
			if e = writeBytes(w, a.encode()); e != nil {
				return e
			}
		} else {
			if e = copyRange(w, v.f, a.off, a.size); e != nil {
				return e
			}
		}
	}
	return nil
}
func (v *mp4) pictures() ([]Picture, error) {
	var out []Picture
	i := v.ilst(false)
	if i == nil {
		return out, nil
	}
	for _, a := range i.children {
		if a.typ != "covr" {
			continue
		}
		data, e := mp4Data(a)
		if e != nil {
			return nil, e
		}
		for _, d := range data {
			mime := ""
			switch d.Flags {
			case 13:
				mime = "image/jpeg"
			case 14:
				mime = "image/png"
			default:
				return nil, errors.New("unsupported MP4 cover type")
			}
			out = append(out, Picture{MIME: mime, Type: 3, Data: bytes.Clone(d.Data)})
		}
	}
	return out, nil
}
func (v *mp4) cover(p Picture) error {
	typ := uint32(13)
	if p.MIME == "image/png" {
		typ = 14
	} else if p.MIME != "image/jpeg" {
		return errors.New("MP4 supports JPEG/PNG covers")
	}
	i := v.ilst(true)
	i.remove("covr")
	i.children = append(i.children, &atom{typ: "covr", data: dataAtom(typ, p.Data).encode()})
	return nil
}
func parseChpl(b []byte) ([]Chapter, error) {
	if len(b) < 9 || b[0] != 1 {
		return nil, errors.New("unsupported/truncated MP4 chpl")
	}
	count := int(b[8])
	pos := 9
	var ch []Chapter
	for i := 0; i < count; i++ {
		if len(b)-pos < 9 {
			return nil, errors.New("truncated MP4 chapter")
		}
		start := float64(be.Uint64(b[pos:])) / 1e7
		n := int(b[pos+8])
		pos += 9
		if n > len(b)-pos {
			return nil, errors.New("truncated MP4 chapter title")
		}
		ch = append(ch, Chapter{Start: start, Title: string(b[pos : pos+n])})
		pos += n
	}
	if pos != len(b) {
		return nil, errors.New("trailing chpl data")
	}
	for i := 0; i+1 < len(ch); i++ {
		ch[i].End = ch[i+1].Start
	}
	return ch, nil
}
func (v *mp4) chapters() ([]Chapter, error) {
	if u := v.moov.find("udta"); u != nil {
		if a := u.find("chpl"); a != nil {
			return parseChpl(a.data)
		}
	}
	if t, err := v.quickTrack(); err != nil {
		return nil, err
	} else if t != nil {
		return v.readQuick(t)
	}
	return []Chapter{}, nil
}
func (v *mp4) setChapters(ch []Chapter) error {
	if t, err := v.quickTrack(); err != nil {
		return err
	} else if t != nil {
		return v.setQuick(t, ch)
	}
	return v.setChpl(ch)
}
func (v *mp4) setChpl(ch []Chapter) error {
	if len(ch) > 255 {
		return errors.New("MP4 chpl supports at most 255 chapters")
	}
	b := make([]byte, 9)
	b[0] = 1
	b[8] = byte(len(ch))
	for _, c := range ch {
		if len([]byte(c.Title)) > 255 {
			return errors.New("MP4 chapter title exceeds 255 UTF-8 bytes")
		}
		n := make([]byte, 8)
		be.PutUint64(n, uint64(c.Start*1e7+0.5))
		b = append(b, n...)
		b = append(b, byte(len([]byte(c.Title))))
		b = append(b, []byte(c.Title)...)
	}
	u := v.moov.ensure("udta")
	u.remove("chpl")
	if len(ch) > 0 {
		u.children = append(u.children, &atom{typ: "chpl", data: b})
	}
	return nil
}
func sortChapters(c []Chapter) { sort.Slice(c, func(i, j int) bool { return c[i].Start < c[j].Start }) }
