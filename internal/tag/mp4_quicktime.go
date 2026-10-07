package tag

import (
	"encoding/json"
	"errors"
	"hash"
	"io"
	"math"
	"strings"
	"unicode/utf8"
)

func childPath(a *atom, path ...string) *atom {
	for _, p := range path {
		if a == nil {
			return nil
		}
		a = a.find(p)
	}
	return a
}
func trackID(t *atom) (uint32, error) {
	h := t.find("tkhd")
	if h == nil || len(h.data) < 20 {
		return 0, errors.New("missing track header")
	}
	p := 12
	if h.data[0] == 1 {
		p = 20
	}
	if len(h.data) < p+4 {
		return 0, errors.New("truncated track header")
	}
	return be.Uint32(h.data[p:]), nil
}
func (v *mp4) quickTrack() (*atom, error) {
	ids := map[uint32]bool{}
	for _, t := range v.moov.children {
		if t.typ != "trak" {
			continue
		}
		if refs := childPath(t, "tref", "chap"); refs != nil {
			if len(refs.data)%4 != 0 {
				return nil, errors.New("invalid chapter references")
			}
			for p := 0; p < len(refs.data); p += 4 {
				ids[be.Uint32(refs.data[p:])] = true
			}
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	if len(ids) != 1 {
		return nil, errors.New("multiple QuickTime chapter tracks are not supported")
	}
	var found *atom
	for _, t := range v.moov.children {
		if t.typ != "trak" {
			continue
		}
		id, err := trackID(t)
		if err != nil {
			return nil, err
		}
		if ids[id] {
			if found != nil {
				return nil, errors.New("duplicate chapter track")
			}
			found = t
		}
	}
	if found == nil {
		return nil, errors.New("chapter reference has no track")
	}
	desc := childPath(found, "mdia", "minf", "stbl", "stsd")
	if desc == nil || len(desc.data) < 16 || be.Uint32(desc.data[4:]) != 1 {
		return nil, errors.New("invalid chapter sample descriptions")
	}
	entries, err := parseAtomsView(desc.data[8:], 0)
	if err != nil || len(entries) != 1 || entries[0].typ != "text" {
		return nil, errors.New("only QuickTime text chapter tracks are supported")
	}
	return found, nil
}
func table(a *atom, width int) ([]byte, int, error) {
	if a == nil || len(a.data) < 8 {
		return nil, 0, errors.New("missing sample table")
	}
	n := uint64(be.Uint32(a.data[4:]))
	if n > uint64((len(a.data)-8)/width) || 8+int(n)*width != len(a.data) {
		return nil, 0, errors.New("invalid sample table count")
	}
	return a.data[8:], int(n), nil
}

// Each callback is bounded to a local media-data sample. No per-audio-sample allocation.
func (v *mp4) eachSample(t *atom, fn func(int, int64, int64) error) error {
	stbl := childPath(t, "mdia", "minf", "stbl")
	if stbl == nil {
		return errors.New("missing sample table")
	}
	sz := stbl.find("stsz")
	if sz == nil || len(sz.data) < 12 {
		return errors.New("missing sample sizes")
	}
	fixed := uint64(be.Uint32(sz.data[4:]))
	count := uint64(be.Uint32(sz.data[8:]))
	if fixed == 0 && (count > uint64((len(sz.data)-12)/4) || 12+int(count)*4 != len(sz.data)) || fixed != 0 && len(sz.data) != 12 {
		return errors.New("invalid sample sizes")
	}
	sc, nsc, err := table(stbl.find("stsc"), 12)
	if count == 0 && err == nil && nsc == 0 {
		offsets := stbl.find("stco")
		width := 4
		if offsets == nil {
			offsets = stbl.find("co64")
			width = 8
		}
		_, n, err := table(offsets, width)
		if err != nil {
			return err
		}
		if n != 0 {
			return errors.New("empty track has chunk offsets")
		}
		return nil
	}
	if err != nil || nsc == 0 {
		return errors.New("invalid sample-to-chunk table")
	}
	offsets := stbl.find("stco")
	width := 4
	if offsets == nil {
		offsets = stbl.find("co64")
		width = 8
	}
	chunks, nchunks, err := table(offsets, width)
	if err != nil {
		return err
	}
	if be.Uint32(sc) != 1 {
		return errors.New("first sample chunk must be 1")
	}
	var sample uint64
	entry := 0
	for chunk := 0; chunk < nchunks; chunk++ {
		for entry+1 < nsc && uint64(be.Uint32(sc[(entry+1)*12:])) <= uint64(chunk+1) {
			if be.Uint32(sc[(entry+1)*12:]) <= be.Uint32(sc[entry*12:]) {
				return errors.New("invalid chunk ordering")
			}
			entry++
		}
		n := uint64(be.Uint32(sc[entry*12+4:]))
		if n == 0 || sample+n > count {
			return errors.New("invalid samples per chunk")
		}
		off := uint64(be.Uint32(chunks[chunk*width:]))
		if width == 8 {
			off = be.Uint64(chunks[chunk*width:])
		}
		for i := uint64(0); i < n; i++ {
			size := fixed
			if size == 0 {
				size = uint64(be.Uint32(sz.data[12+sample*4:]))
			}
			if off > math.MaxInt64 || size > math.MaxInt64 || off+size < off {
				return errors.New("sample offset overflow")
			}
			local := false
			for _, a := range v.top {
				if a.typ == "mdat" && off >= uint64(a.off+a.header) && off+size <= uint64(a.off+a.size) {
					local = true
					break
				}
			}
			if !local {
				return errors.New("sample outside local mdat")
			}
			if err := fn(int(sample), int64(off), int64(size)); err != nil {
				return err
			}
			sample++
			off += size
		}
	}
	if sample != count {
		return errors.New("sample count mismatch")
	}
	return nil
}
func mediaTime(t *atom) (uint32, uint64, error) {
	h := childPath(t, "mdia", "mdhd")
	if h == nil || len(h.data) < 24 {
		return 0, 0, errors.New("missing media header")
	}
	if h.data[0] == 0 {
		return be.Uint32(h.data[12:]), uint64(be.Uint32(h.data[16:])), nil
	}
	if h.data[0] == 1 && len(h.data) >= 36 {
		return be.Uint32(h.data[20:]), be.Uint64(h.data[24:]), nil
	}
	return 0, 0, errors.New("unsupported media header")
}
func (v *mp4) readQuick(t *atom) ([]Chapter, error) {
	if v.quickChapters != nil {
		return append([]Chapter(nil), v.quickChapters...), nil
	}
	scale, _, err := mediaTime(t)
	if err != nil || scale == 0 {
		return nil, errors.New("invalid chapter timescale")
	}
	if edits := childPath(t, "edts", "elst"); edits != nil {
		b, n, err := table(edits, 12)
		if err != nil || n != 1 || be.Uint32(b[4:]) != 0 || be.Uint16(b[8:]) != 1 || be.Uint16(b[10:]) != 0 {
			return nil, errors.New("complex chapter edit lists are not supported")
		}
	}
	times, n, err := table(childPath(t, "mdia", "minf", "stbl", "stts"), 8)
	if err != nil {
		return nil, err
	}
	var durations []uint32
	for p := 0; p < n; p++ {
		count := be.Uint32(times[p*8:])
		delta := be.Uint32(times[p*8+4:])
		if count > 100000 || len(durations)+int(count) > 100000 {
			return nil, errors.New("chapter sample count exceeds limit")
		}
		for i := uint32(0); i < count; i++ {
			durations = append(durations, delta)
		}
	}
	var chapters []Chapter
	var stamp uint64
	err = v.eachSample(t, func(i int, off, size int64) error {
		if i >= len(durations) || size < 2 || size > 65537 {
			return errors.New("invalid chapter sample")
		}
		b, err := readAt(v.f, off, int(size))
		if err != nil {
			return err
		}
		length := int(be.Uint16(b))
		if length > len(b)-2 || !utf8.Valid(b[2:2+length]) {
			return errors.New("invalid chapter title")
		}
		chapter := Chapter{Start: float64(stamp) / float64(scale), End: float64(stamp+uint64(durations[i])) / float64(scale), Title: string(b[2 : 2+length])}
		chapters = append(chapters, chapter)
		stamp += uint64(durations[i])
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(chapters) != len(durations) {
		return nil, errors.New("chapter duration count mismatch")
	}
	return chapters, nil
}
func (v *mp4) hashQuickMedia(h hash.Hash, chapter *atom) error {
	buffer := make([]byte, 32<<10)
	for _, track := range v.moov.children {
		if track.typ != "trak" || track == chapter {
			continue
		}
		id, err := trackID(track)
		if err != nil {
			return err
		}
		var identifier [4]byte
		be.PutUint32(identifier[:], id)
		h.Write(identifier[:])
		var start, end int64
		flush := func() error {
			if end == start {
				return nil
			}
			_, err := io.CopyBuffer(h, io.NewSectionReader(v.f, start, end-start), buffer)
			return err
		}
		err = v.eachSample(track, func(_ int, off, size int64) error {
			if start == end {
				start = off
				end = off + size
				return nil
			}
			if off == end {
				end += size
				return nil
			}
			if err := flush(); err != nil {
				return err
			}
			start = off
			end = off + size
			return nil
		})
		if err != nil {
			return err
		}
		if err := flush(); err != nil {
			return err
		}
	}
	return nil
}
func (v *mp4) setQuick(t *atom, chapters []Chapter) error {

	for _, c := range chapters {
		if math.IsNaN(c.Start) || math.IsInf(c.Start, 0) || math.IsNaN(c.End) || math.IsInf(c.End, 0) || c.Start < 0 || c.End < 0 || !utf8.ValidString(c.Title) || strings.ContainsRune(c.Title, 0) {
			return errors.New("invalid chapter time or title")
		}
	}
	if len(chapters) > 100000 {
		return errors.New("too many chapters")
	}
	if _, err := v.readQuick(t); err != nil {
		return err
	}
	scale, duration, err := mediaTime(t)
	if err != nil {
		return err
	}
	if len(chapters) > 0 && chapters[0].Start != 0 {
		return errors.New("QuickTime chapter list must begin at zero")
	}
	var data []byte
	sz := make([]byte, 12+4*len(chapters))
	be.PutUint32(sz[8:], uint32(len(chapters)))
	tt := make([]byte, 8+8*len(chapters))
	be.PutUint32(tt[4:], uint32(len(chapters)))
	canonical := make([]Chapter, len(chapters))
	for i, c := range chapters {
		if len(c.Title) > 65535 {
			return errors.New("chapter title exceeds 65535 UTF-8 bytes")
		}
		start := uint64(math.Round(c.Start * float64(scale)))
		end := duration
		if i+1 < len(chapters) {
			end = uint64(math.Round(chapters[i+1].Start * float64(scale)))
		} else if c.End != 0 {
			end = uint64(math.Round(c.End * float64(scale)))
		}
		if start >= end || end > duration || end-start > math.MaxUint32 {
			return errors.New("chapter time outside existing track duration")
		}
		if c.End != 0 && i+1 < len(chapters) && uint64(math.Round(c.End*float64(scale))) != end {
			return errors.New("QuickTime chapter ends must match the next start")
		}
		canonical[i] = Chapter{Start: float64(start) / float64(scale), End: float64(end) / float64(scale), Title: c.Title}
		var n [2]byte
		be.PutUint16(n[:], uint16(len(c.Title)))
		data = append(data, n[:]...)
		data = append(data, c.Title...)
		be.PutUint32(sz[12+i*4:], uint32(len(c.Title)+2))
		be.PutUint32(tt[8+i*8:], 1)
		be.PutUint32(tt[12+i*8:], uint32(end-start))
	}
	if len(data) > MaxMetadata {
		return errors.New("chapter data exceeds metadata limit")
	}
	if udta := v.moov.find("udta"); udta != nil && udta.find("chpl") != nil {
		if len(canonical) > 255 {
			return errors.New("Nero companion supports at most 255 chapters")
		}
		for _, c := range canonical {
			if len(c.Title) > 255 {
				return errors.New("Nero companion title exceeds 255 bytes")
			}
		}
	}
	stbl := childPath(t, "mdia", "minf", "stbl")
	stbl.remove("stsz")
	stbl.remove("stts")
	stbl.remove("stsc")
	stbl.remove("stco")
	stbl.remove("co64")
	stbl.remove("stss")
	stbl.remove("ctts")
	sc := make([]byte, 20)
	be.PutUint32(sc[4:], 1)
	be.PutUint32(sc[8:], 1)
	be.PutUint32(sc[12:], uint32(len(chapters)))
	be.PutUint32(sc[16:], 1)
	// All replacement chapter samples live in one appended mdat. Keep old media bytes unchanged.
	for i := len(v.top) - 1; i >= 0; i-- {
		if v.top[i].generated {
			v.top = append(v.top[:i], v.top[i+1:]...)
		}
	}
	off := v.size + 8
	co := make([]byte, 16)
	be.PutUint32(co[4:], 1)
	be.PutUint64(co[8:], uint64(off))
	stbl.children = append(stbl.children, &atom{typ: "stsz", data: sz}, &atom{typ: "stts", data: tt}, &atom{typ: "stsc", data: sc}, &atom{typ: "co64", data: co})
	if len(chapters) == 0 {
		sc = make([]byte, 8)
		co = make([]byte, 8)
		stbl.find("stsc").data = sc
		stbl.find("co64").data = co
	}
	if len(data) > 0 {
		v.top = append(v.top, &atom{typ: "mdat", off: v.size, size: int64(len(data) + 8), header: 8, data: data, generated: true})
	}
	v.quickChapters = canonical
	if udta := v.moov.find("udta"); udta != nil && udta.find("chpl") != nil {
		if len(canonical) > 255 {
			return errors.New("existing Nero companion cannot represent more than 255 chapters")
		}
		if err := v.setChpl(canonical); err != nil {
			return err
		}
	}
	return nil
}
func (v *mp4) quickRaw() []Raw {
	t, err := v.quickTrack()
	if err != nil || t == nil {
		return nil
	}
	ch, err := v.readQuick(t)
	if err != nil {
		return nil
	}
	b, _ := json.Marshal(ch)
	return []Raw{{"@qtchapters", 0, b}}
}
