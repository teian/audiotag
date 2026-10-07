package tag

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type SeekPoint struct {
	Sample uint64 `json:"sample"`
	Offset uint64 `json:"offset"`
	Frames uint16 `json:"frame_samples"`
}
type CueIndex struct {
	Offset uint64 `json:"offset_samples"`
	Number byte   `json:"number"`
}
type CueTrack struct {
	Offset      uint64     `json:"offset_samples"`
	Number      byte       `json:"number"`
	ISRC        string     `json:"isrc,omitempty"`
	Data        bool       `json:"data,omitempty"`
	PreEmphasis bool       `json:"pre_emphasis,omitempty"`
	Indices     []CueIndex `json:"indices"`
}
type CueSheet struct {
	Catalogue string     `json:"catalogue,omitempty"`
	LeadIn    uint64     `json:"lead_in_samples"`
	CD        bool       `json:"compact_disc"`
	Tracks    []CueTrack `json:"tracks"`
}

func (v *flac) block(kind string) []byte {
	for _, r := range v.blocks {
		if r.Key == kind {
			return r.Data
		}
	}
	return nil
}
func (v *flac) replaceBlock(kind string, b []byte) {
	out := v.blocks[:0]
	for _, r := range v.blocks {
		if r.Key != kind {
			out = append(out, r)
		}
	}
	v.blocks = out
	if b != nil {
		v.blocks = append(v.blocks, Raw{kind, 0, b})
	}
}
func parseCue(b []byte) (CueSheet, error) {
	var c CueSheet
	if len(b) < 396 {
		return c, errors.New("truncated FLAC cuesheet")
	}
	c.Catalogue = strings.TrimRight(string(b[:128]), "\x00")
	c.LeadIn = be.Uint64(b[128:])
	c.CD = b[136]&128 != 0
	pos := 396
	for i := 0; i < int(b[395]); i++ {
		if len(b)-pos < 36 {
			return c, errors.New("truncated cuesheet track")
		}
		r := b[pos:]
		t := CueTrack{Offset: be.Uint64(r), Number: r[8], ISRC: strings.TrimRight(string(r[9:21]), "\x00"), Data: r[21]&128 != 0, PreEmphasis: r[21]&64 != 0}
		pos += 36
		for n := 0; n < int(r[35]); n++ {
			if len(b)-pos < 12 {
				return c, errors.New("truncated cue index")
			}
			t.Indices = append(t.Indices, CueIndex{be.Uint64(b[pos:]), b[pos+8]})
			pos += 12
		}
		c.Tracks = append(c.Tracks, t)
	}
	if pos != len(b) {
		return c, errors.New("trailing cuesheet data")
	}
	return c, nil
}
func encodeCue(c CueSheet) ([]byte, error) {
	if len(c.Catalogue) > 128 || len(c.Tracks) < 1 || len(c.Tracks) > 255 {
		return nil, errors.New("invalid cuesheet catalogue/track count")
	}
	for _, r := range c.Catalogue {
		if r > 127 || r == 0 {
			return nil, errors.New("catalogue must be ASCII without NUL")
		}
	}
	if c.CD && c.LeadIn < 88200 {
		return nil, errors.New("CD cuesheet lead-in must be at least two seconds")
	}
	b := make([]byte, 396)
	copy(b, c.Catalogue)
	be.PutUint64(b[128:], c.LeadIn)
	if c.CD {
		b[136] = 128
	}
	b[395] = byte(len(c.Tracks))
	var prev uint64
	for i, t := range c.Tracks {
		last := i == len(c.Tracks)-1
		if t.Offset > c.Tracks[len(c.Tracks)-1].Offset {
			return nil, errors.New("cue track exceeds lead-out")
		}
		if i > 0 && t.Number <= c.Tracks[i-1].Number {
			return nil, errors.New("cue track numbers must increase")
		}
		if last && (!c.CD && t.Number != 255) {
			return nil, errors.New("non-CD lead-out track number must be 255")
		}
		if c.CD && c.LeadIn%588 != 0 {
			return nil, errors.New("CD lead-in must be divisible by 588")
		}
		if t.Number == 0 || len(t.ISRC) > 12 || len(t.Indices) > 255 || i > 0 && t.Offset < prev {
			return nil, errors.New("invalid cuesheet track")
		}
		for _, r := range t.ISRC {
			if r > 127 || r == 0 {
				return nil, errors.New("ISRC must be ASCII without NUL")
			}
		}
		if last && len(t.Indices) != 0 || !last && len(t.Indices) == 0 {
			return nil, errors.New("only lead-out may have no indices")
		}
		if c.CD && (t.Offset%588 != 0 || !last && t.Number > 99 || last && t.Number != 170) {
			return nil, errors.New("invalid CD track number/offset")
		}
		r := make([]byte, 36)
		be.PutUint64(r, t.Offset)
		r[8] = t.Number
		copy(r[9:21], t.ISRC)
		if t.Data {
			r[21] |= 128
		}
		if t.PreEmphasis {
			r[21] |= 64
		}
		r[35] = byte(len(t.Indices))
		b = append(b, r...)
		var previous CueIndex
		for j, index := range t.Indices {
			if j == 0 && index.Number > 1 || j > 0 && (index.Number != previous.Number+1 || index.Offset < previous.Offset) || c.CD && index.Offset%588 != 0 {
				return nil, errors.New("invalid cue index")
			}
			if index.Offset > c.Tracks[len(c.Tracks)-1].Offset-t.Offset {
				return nil, errors.New("cue index exceeds lead-out")
			}
			if !last && i+1 < len(c.Tracks) && index.Offset > c.Tracks[i+1].Offset-t.Offset {
				return nil, errors.New("cue index crosses track boundary")
			}
			r := make([]byte, 12)
			be.PutUint64(r, index.Offset)
			r[8] = index.Number
			b = append(b, r...)
			previous = index
		}
		prev = t.Offset
	}
	return b, nil
}
func (v *flac) sampleProperties() (uint64, uint64) {
	b := v.block("0")
	if len(b) != 34 {
		return 0, 0
	}
	n := be.Uint64(b[10:18])
	return n >> 44, n & ((1 << 36) - 1)
}
func (v *flac) structuredTags(m map[string][]string) {
	if b := v.block("3"); b != nil && len(b)%18 == 0 {
		var points []SeekPoint
		for i := 0; i < len(b); i += 18 {
			points = append(points, SeekPoint{be.Uint64(b[i:]), be.Uint64(b[i+8:]), be.Uint16(b[i+16:])})
		}
		data, _ := json.Marshal(points)
		m["seektable"] = []string{string(data)}
	}
	if b := v.block("5"); b != nil {
		if c, err := parseCue(b); err == nil {
			data, _ := json.Marshal(c)
			m["cuesheet"] = []string{string(data)}
			rate, _ := v.sampleProperties()
			if text, err := cueText(c, rate); err == nil {
				m["cuesheettext"] = []string{text}
			}
		}
	}
}
func (v *flac) setStructured(k string, values []string) (bool, error) {
	if k != "seektable" && k != "cuesheet" {
		return false, nil
	}
	if len(values) != 1 {
		return true, errors.New("structured FLAC fields require one value")
	}
	if k == "seektable" {
		var points []SeekPoint
		if err := json.Unmarshal([]byte(values[0]), &points); err != nil {
			return true, err
		}
		if len(points) > 0xffffff/18 {
			return true, errors.New("seektable exceeds FLAC block size")
		}
		b := make([]byte, 18*len(points))
		_, total := v.sampleProperties()
		var prev uint64
		for i, p := range points {
			if p.Sample == ^uint64(0) {
				if p.Offset != 0 || p.Frames != 0 {
					return true, errors.New("seektable placeholders need zero offset and frame_samples")
				}
			} else if p.Sample >= total || p.Offset >= uint64(v.size-v.offset) {
				return true, errors.New("seekpoint exceeds encoded stream bounds")
			}
			if p.Sample != ^uint64(0) && i > 0 && p.Sample <= prev {
				return true, errors.New("seek samples must increase; placeholders must be last")
			}
			if p.Sample != ^uint64(0) && p.Frames == 0 {
				return true, errors.New("seekpoint frame_samples must be nonzero")
			}
			be.PutUint64(b[i*18:], p.Sample)
			be.PutUint64(b[i*18+8:], p.Offset)
			be.PutUint16(b[i*18+16:], p.Frames)
			prev = p.Sample
		}
		v.replaceBlock("3", b)
		return true, nil
	}
	var c CueSheet
	rate, total := v.sampleProperties()
	if strings.HasPrefix(strings.TrimSpace(values[0]), "{") {
		if err := json.Unmarshal([]byte(values[0]), &c); err != nil {
			return true, err
		}
	} else {
		var err error
		c, err = parseCueText(values[0], rate, total)
		if err != nil {
			return true, err
		}
	}
	if c.CD && rate != 44100 {
		return true, errors.New("CD cuesheets require 44100 Hz")
	}
	if len(c.Tracks) == 0 {
		return true, errors.New("cuesheet needs tracks and lead-out")
	}
	if c.Tracks[len(c.Tracks)-1].Offset != total {
		return true, errors.New("cue lead-out must equal STREAMINFO total samples")
	}
	b, err := encodeCue(c)
	if err != nil {
		return true, err
	}
	v.replaceBlock("5", b)
	return true, nil
}
func parseCueText(text string, rate, total uint64) (CueSheet, error) {
	c := CueSheet{CD: rate == 44100}
	if c.CD {
		c.LeadIn = 88200
	}
	fileCount := 0
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		switch strings.ToUpper(f[0]) {
		case "REM", "TITLE", "PERFORMER", "SONGWRITER", "CDTEXTFILE":
			continue
		case "FILE":
			fileCount++
			if fileCount > 1 {
				return c, errors.New("embedded cuesheet supports one FILE")
			}
		case "CATALOG":
			if len(f) != 2 {
				return c, errors.New("invalid CATALOG")
			}
			c.Catalogue = f[1]
		case "TRACK":
			if len(f) != 3 || strings.ToUpper(f[2]) != "AUDIO" {
				return c, errors.New("only AUDIO cue tracks supported")
			}
			n, err := strconv.ParseUint(f[1], 10, 8)
			if err != nil {
				return c, err
			}
			c.Tracks = append(c.Tracks, CueTrack{Number: byte(n)})
		case "ISRC":
			if len(c.Tracks) == 0 || len(f) != 2 {
				return c, errors.New("ISRC needs TRACK")
			}
			c.Tracks[len(c.Tracks)-1].ISRC = f[1]
		case "FLAGS":
			if len(c.Tracks) == 0 {
				return c, errors.New("FLAGS needs TRACK")
			}
			for _, flag := range f[1:] {
				if flag != "PRE" {
					return c, fmt.Errorf("unsupported cue flag %s", flag)
				}
				c.Tracks[len(c.Tracks)-1].PreEmphasis = true
			}
		case "INDEX":
			if len(c.Tracks) == 0 || len(f) != 3 {
				return c, errors.New("invalid INDEX")
			}
			n, err := strconv.ParseUint(f[1], 10, 8)
			if err != nil {
				return c, err
			}
			parts := strings.Split(f[2], ":")
			if len(parts) != 3 {
				return c, errors.New("cue time requires MM:SS:FF")
			}
			minutes, err := strconv.ParseUint(parts[0], 10, 64)
			if err != nil {
				return c, err
			}
			seconds, err := strconv.ParseUint(parts[1], 10, 64)
			if err != nil {
				return c, err
			}
			frames, err := strconv.ParseUint(parts[2], 10, 64)
			if err != nil {
				return c, err
			}
			if rate == 0 || minutes > total/rate/60+1 || seconds >= 60 || frames >= 75 {
				return c, errors.New("cue time outside audio duration")
			}
			ticks := (minutes*60+seconds)*75 + frames
			if rate == 0 || ticks*rate%75 != 0 {
				return c, errors.New("cue time cannot represent an exact sample offset")
			}
			off := ticks * rate / 75
			t := &c.Tracks[len(c.Tracks)-1]
			if len(t.Indices) == 0 {
				t.Offset = off
			}
			if off < t.Offset {
				return c, errors.New("cue times must increase")
			}
			t.Indices = append(t.Indices, CueIndex{off - t.Offset, byte(n)})
		default:
			return c, fmt.Errorf("unsupported cue directive %s", f[0])
		}
	}
	if len(c.Tracks) == 0 {
		return c, errors.New("cue has no tracks")
	}
	lead := byte(255)
	if c.CD {
		lead = 170
	}
	c.Tracks = append(c.Tracks, CueTrack{Offset: total, Number: lead})
	return c, nil
}
func cueText(c CueSheet, rate uint64) (string, error) {
	var b bytes.Buffer
	if c.Catalogue != "" {
		fmt.Fprintf(&b, "CATALOG %s\n", c.Catalogue)
	}
	b.WriteString("FILE \"audio.flac\" WAVE\n")
	for i, t := range c.Tracks {
		if i == len(c.Tracks)-1 {
			break
		}
		if t.Data {
			return "", errors.New("data tracks cannot be exported as AUDIO cues")
		}
		fmt.Fprintf(&b, "  TRACK %02d AUDIO\n", t.Number)
		if t.ISRC != "" {
			fmt.Fprintf(&b, "    ISRC %s\n", t.ISRC)
		}
		if t.PreEmphasis {
			b.WriteString("    FLAGS PRE\n")
		}
		for _, idx := range t.Indices {
			off := t.Offset + idx.Offset
			if rate == 0 || off*75%rate != 0 {
				return "", errors.New("sample offset cannot be represented in cue frames")
			}
			ticks := off * 75 / rate
			fmt.Fprintf(&b, "    INDEX %02d %02d:%02d:%02d\n", idx.Number, ticks/4500, ticks/75%60, ticks%75)
		}
	}
	return b.String(), nil
}
