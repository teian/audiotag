package tag

import (
	"bufio"
	"bytes"
	"errors"
	"hash"
	"io"
	"os"
)

type oggPage struct {
	header, body []byte
	off, next    int64
}

func readPage(f *os.File, off int64) (oggPage, error) {
	p := oggPage{off: off}
	h, e := readAt(f, off, 27)
	if e != nil {
		return p, e
	}
	if string(h[:4]) != "OggS" || h[4] != 0 {
		return p, errors.New("invalid Ogg page")
	}
	laces, e := readAt(f, off+27, int(h[26]))
	if e != nil {
		return p, e
	}
	n := 0
	for _, v := range laces {
		n += int(v)
	}
	p.body, e = readAt(f, off+27+int64(len(laces)), n)
	if e != nil {
		return p, e
	}
	p.header = append(h, laces...)
	p.next = off + int64(len(p.header)+n)
	crc := le.Uint32(h[22:])
	for i := 22; i < 26; i++ {
		p.header[i] = 0
	}
	if oggUpdateCRC(oggCRC(p.header), p.body) != crc {
		return p, errors.New("Ogg CRC mismatch")
	}
	le.PutUint32(p.header[22:], crc)
	return p, nil
}

var oggTable = func() [8][256]uint32 {
	var t [8][256]uint32
	for i := range t[0] {
		crc := uint32(i) << 24
		for bit := 0; bit < 8; bit++ {
			if crc&0x80000000 != 0 {
				crc = crc<<1 ^ 0x04c11db7
			} else {
				crc <<= 1
			}
		}
		t[0][i] = crc
	}
	for n := 1; n < 8; n++ {
		for i, c := range t[n-1] {
			t[n][i] = c<<8 ^ t[0][byte(c>>24)]
		}
	}
	return t
}()

func oggCRC(b []byte) uint32 { return oggUpdateCRC(0, b) }
func oggUpdateCRC(crc uint32, b []byte) uint32 {
	for len(b) >= 8 {
		crc ^= be.Uint32(b)
		crc = oggTable[7][byte(crc>>24)] ^ oggTable[6][byte(crc>>16)] ^ oggTable[5][byte(crc>>8)] ^ oggTable[4][byte(crc)] ^ oggTable[3][b[4]] ^ oggTable[2][b[5]] ^ oggTable[1][b[6]] ^ oggTable[0][b[7]]
		b = b[8:]
	}
	for _, v := range b {
		crc = crc<<8 ^ oggTable[0][byte(crc>>24)^v]
	}
	return crc
}

// Page storage is reused only after the caller has consumed the previous page.
type oggScanner struct {
	r      *bufio.Reader
	header [282]byte
	body   [65025]byte
	off    int64
}

func newOggScanner(f *os.File, off, size int64) *oggScanner {
	return &oggScanner{r: bufio.NewReaderSize(io.NewSectionReader(f, off, size-off), 128<<10), off: off}
}
func (s *oggScanner) read() (oggPage, error) {
	p := oggPage{off: s.off}
	h := s.header[:27]
	if _, e := io.ReadFull(s.r, h); e != nil {
		return p, e
	}
	if string(h[:4]) != "OggS" || h[4] != 0 {
		return p, errors.New("invalid Ogg page")
	}
	h = s.header[:27+int(h[26])]
	if _, e := io.ReadFull(s.r, h[27:]); e != nil {
		return p, e
	}
	n := 0
	for _, v := range h[27:] {
		n += int(v)
	}
	body := s.body[:n]
	if _, e := io.ReadFull(s.r, body); e != nil {
		return p, e
	}
	crc := le.Uint32(h[22:])
	le.PutUint32(h[22:], 0)
	actual := oggUpdateCRC(oggCRC(h), body)
	le.PutUint32(h[22:], crc)
	if actual != crc {
		return p, errors.New("Ogg CRC mismatch")
	}
	s.off += int64(len(h) + n)
	return oggPage{header: h, body: body, off: p.off, next: s.off}, nil
}

type ogg struct {
	f               *os.File
	kind            string
	c               comments
	serial          uint32
	packets         [][]byte
	headerEnd, size int64
	headerPages     uint32
	suffix          []byte
}

func openOgg(f *os.File) (engine, error) {
	st, e := f.Stat()
	if e != nil {
		return nil, e
	}
	v := &ogg{f: f, size: st.Size()}
	off := int64(0)
	var packet bytes.Buffer
	want := 2
	for len(v.packets) < want {
		p, e := readPage(f, off)
		if e != nil {
			return nil, e
		}
		serial := le.Uint32(p.header[14:])
		if off == 0 {
			v.serial = serial
			if p.header[5]&2 == 0 {
				return nil, errors.New("missing Ogg BOS")
			}
		} else if serial != v.serial {
			return nil, errors.New("multiplexed Ogg is unsupported")
		}
		if le.Uint32(p.header[18:]) != v.headerPages {
			return nil, errors.New("nonconsecutive Ogg pages")
		}
		if (p.header[5]&1 != 0) != (packet.Len() > 0) {
			return nil, errors.New("invalid Ogg continuation")
		}
		pos := 0
		for _, n := range p.header[27:] {
			if packet.Len()+int(n) > MaxMetadata {
				return nil, errors.New("Ogg header too large")
			}
			packet.Write(p.body[pos : pos+int(n)])
			pos += int(n)
			if n < 255 {
				v.packets = append(v.packets, packet.Bytes())
				packet = bytes.Buffer{}
				if len(v.packets) == 1 {
					if bytes.HasPrefix(v.packets[0], []byte("OpusHead")) {
						v.kind = "opus"
					} else if bytes.HasPrefix(v.packets[0], []byte{1, 'v', 'o', 'r', 'b', 'i', 's'}) {
						v.kind = "vorbis"
						want = 3
					} else {
						return nil, errors.New("only Ogg Opus and Vorbis are supported")
					}
				}
			}
		}
		v.headerPages++
		off = p.next
	}
	if len(v.packets) != want || packet.Len() > 0 {
		return nil, errors.New("audio shares final Ogg header page; refusing unsupported layout")
	}
	v.headerEnd = off
	b := v.packets[1]
	prefix := 8
	if v.kind == "opus" {
		if !bytes.HasPrefix(b, []byte("OpusTags")) {
			return nil, errors.New("missing OpusTags")
		}
	} else {
		prefix = 7
		if !bytes.HasPrefix(b, []byte{3, 'v', 'o', 'r', 'b', 'i', 's'}) {
			return nil, errors.New("missing Vorbis comments")
		}
	}
	v.c, e = parseCommentsView(b[prefix:])
	if e != nil {
		return nil, e
	}
	v.suffix = bytes.Clone(v.c.trailer)
	if v.kind == "vorbis" && (len(v.suffix) == 0 || v.suffix[0]&1 == 0) {
		return nil, errors.New("missing Vorbis comment framing bit")
	}
	return v, nil
}
func (v *ogg) format() string                 { return v.kind }
func (v *ogg) raw() []Raw                     { return v.c.values }
func (v *ogg) replace(r []Raw) error          { return v.c.replace(r) }
func (v *ogg) tags() map[string][]string      { return v.c.tags() }
func (v *ogg) set(k string, s []string) error { return v.c.set(k, s) }
func (v *ogg) remove(k string) error          { return v.c.remove(k) }
func writePage(w io.Writer, serial, seq uint32, flags byte, granule uint64, laces, body []byte) error {
	h := make([]byte, 27+len(laces))
	copy(h, "OggS")
	h[5] = flags
	le.PutUint64(h[6:], granule)
	le.PutUint32(h[14:], serial)
	le.PutUint32(h[18:], seq)
	h[26] = byte(len(laces))
	copy(h[27:], laces)
	le.PutUint32(h[22:], oggUpdateCRC(oggCRC(h), body))
	if e := writeBytes(w, h); e != nil {
		return e
	}
	return writeBytes(w, body)
}
func (v *ogg) write(w io.Writer) error {
	buffered := bufio.NewWriterSize(w, 128<<10)
	w = buffered
	packets := append([][]byte(nil), v.packets...)
	prefix := []byte("OpusTags")
	if v.kind != "opus" {
		prefix = []byte{3, 'v', 'o', 'r', 'b', 'i', 's'}
	}
	commentSize := v.c.encodedSize()
	packet := make([]byte, len(prefix)+commentSize+len(v.suffix))
	pos := copy(packet, prefix)
	v.c.encodeTo(packet[pos : pos+commentSize])
	pos += commentSize
	copy(packet[pos:], v.suffix)
	packets[1] = packet
	var seq uint32
	for i, p := range packets {
		pos := 0
		first := true
		for {
			remaining := len(p) - pos
			segments := remaining/255 + 1
			if segments > 255 {
				segments = 255
			}
			laces := make([]byte, segments)
			bodyLen := 0
			complete := false
			for j := range laces {
				n := len(p) - pos - bodyLen
				if n >= 255 {
					laces[j] = 255
					bodyLen += 255
				} else {
					laces[j] = byte(n)
					bodyLen += n
					complete = true
					break
				}
			}
			flags := byte(0)
			if i == 0 && first {
				flags = 2
			}
			if !first {
				flags |= 1
			}
			gran := uint64(0)
			if !complete {
				gran = ^uint64(0)
			}
			if e := writePage(w, v.serial, seq, flags, gran, laces, p[pos:pos+bodyLen]); e != nil {
				return e
			}
			seq++
			pos += bodyLen
			first = false
			if complete {
				break
			}
		}
	}
	scanner := newOggScanner(v.f, v.headerEnd, v.size)
	for off := v.headerEnd; off < v.size; {
		p, e := scanner.read()
		if e != nil {
			return e
		}
		if le.Uint32(p.header[14:]) != v.serial || p.header[5]&2 != 0 {
			return errors.New("chained/multiplexed Ogg is unsupported")
		}
		oldSeq := le.Uint32(p.header[18:])
		if oldSeq < v.headerPages {
			return errors.New("invalid Ogg page sequence")
		}
		le.PutUint32(p.header[18:], seq)
		seq++
		for i := 22; i < 26; i++ {
			p.header[i] = 0
		}
		le.PutUint32(p.header[22:], oggUpdateCRC(oggCRC(p.header), p.body))
		if e = writeBytes(w, p.header); e != nil {
			return e
		}
		if e = writeBytes(w, p.body); e != nil {
			return e
		}
		off = p.next
	}
	return buffered.Flush()
}
func (v *ogg) audio(h hash.Hash) error {
	var expected uint32 = v.headerPages
	scanner := newOggScanner(v.f, v.headerEnd, v.size)
	for off := v.headerEnd; off < v.size; {
		p, e := scanner.read()
		if e != nil {
			return e
		}
		if le.Uint32(p.header[14:]) != v.serial || le.Uint32(p.header[18:]) != expected || p.header[5]&2 != 0 {
			return errors.New("chained, multiplexed, or nonconsecutive Ogg is unsupported")
		}
		expected++ // Include granules and lacing: packet boundaries and timing must also survive.
		h.Write(p.header[5:14])
		h.Write(p.header[26:])
		h.Write(p.body)
		off = p.next
	}
	return nil
}
func (v *ogg) pictures() ([]Picture, error)  { return v.c.pictures() }
func (v *ogg) cover(p Picture) error         { return v.c.cover(p) }
func (v *ogg) chapters() ([]Chapter, error)  { return v.c.chapters() }
func (v *ogg) setChapters(c []Chapter) error { return v.c.setChapters(c) }
