package tag

import (
	"bytes"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

type bextField struct{ offset, length int }

var bextStrings = map[string]bextField{"description": {0, 256}, "originator": {256, 32}, "originatorreference": {288, 32}, "originationdate": {320, 10}, "originationtime": {330, 8}}
var bextLoudness = map[string]int{"loudnessvalue": 412, "loudnessrange": 414, "maxtruepeaklevel": 416, "maxmomentaryloudness": 418, "maxshorttermloudness": 420}

func broadcastChunk(key string) bool { return key == "bext" || key == "iXML" }
func (v *iff) broadcastTags(m map[string][]string) {
	for _, c := range v.chunks {
		if c.key == "iXML" {
			m["ixml"] = []string{string(c.data)}
			inspectIXML(c.data, m)
			continue
		}
		if c.key != "bext" || len(c.data) < 602 {
			continue
		}
		for name, f := range bextStrings {
			m["bext."+name] = []string{strings.TrimRight(string(c.data[f.offset:f.offset+f.length]), "\x00")}
		}
		m["bext.timereference"] = []string{strconv.FormatUint(le.Uint64(c.data[338:]), 10)}
		m["bext.version"] = []string{strconv.Itoa(int(le.Uint16(c.data[346:])))}
		m["bext.umid"] = []string{hex.EncodeToString(c.data[348:412])}
		m["bext.codinghistory"] = []string{string(c.data[602:])}
		if le.Uint16(c.data[346:]) >= 2 {
			for name, off := range bextLoudness {
				n := int16(le.Uint16(c.data[off:]))
				text := strconv.FormatFloat(float64(n)/100, 'f', 2, 64)
				if n == 32767 {
					text = "unknown"
				}
				m["bext."+name] = []string{text}
			}
		}
	}
}
func validIXML(data []byte) error {
	d := xml.NewDecoder(bytes.NewReader(data))
	depth := 0
	roots := 0
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if depth == 0 {
				roots++
				if t.Name.Local != "BWFXML" {
					return errors.New("iXML root must be BWFXML")
				}
			}
			depth++
		case xml.EndElement:
			depth--
		case xml.Directive:
			return errors.New("XML directives/DTDs are refused")
		case xml.CharData:
			if depth == 0 && strings.TrimSpace(string(t)) != "" {
				return errors.New("text outside iXML root")
			}
		}
	}
	if roots != 1 {
		return errors.New("iXML requires one BWFXML root")
	}
	return nil
}
func (v *iff) setBroadcast(k string, values []string) (bool, error) {
	if k != "ixml" && !strings.HasPrefix(k, "bext.") {
		return false, nil
	}
	if v.kind != "wav" {
		return true, errors.New("broadcast fields require WAV")
	}
	if len(values) != 1 {
		return true, errors.New("broadcast fields require one value")
	}
	key := "bext"
	if k == "ixml" {
		key = "iXML"
	}
	index := -1
	for i, c := range v.chunks {
		if c.key == key {
			if index != -1 {
				return true, errors.New("duplicate broadcast chunks")
			}
			index = i
		}
	}
	var b []byte
	if index >= 0 {
		b = bytes.Clone(v.chunks[index].data)
	}
	if key == "iXML" {
		b = []byte(values[0])
		if err := validIXML(b); err != nil {
			return true, err
		}
	} else {
		if len(b) == 0 {
			b = make([]byte, 602)
			le.PutUint16(b[346:], 2)
			for _, off := range bextLoudness {
				le.PutUint16(b[off:], 32767)
			}
		}
		if len(b) < 602 {
			return true, errors.New("truncated bext")
		}
		name := strings.TrimPrefix(k, "bext.")
		s := values[0]
		if f, ok := bextStrings[name]; ok {
			if len(s) > f.length {
				return true, fmt.Errorf("%s exceeds %d bytes", k, f.length)
			}
			for _, c := range s {
				if c > 127 {
					return true, errors.New("bext fixed text must be ASCII")
				}
			}
			clear(b[f.offset : f.offset+f.length])
			copy(b[f.offset:], s)
		} else if off, ok := bextLoudness[name]; ok {
			if le.Uint16(b[346:]) < 2 {
				return true, errors.New("loudness requires bext version 2; set bext.version first")
			}
			n := int64(32767)
			if s != "unknown" {
				f, err := strconv.ParseFloat(s, 64)
				if err != nil || f != f || f < -327.68 || f > 327.66 {
					return true, errors.New("bext loudness must be -327.68..327.66 or unknown")
				}
				n = int64(math.Round(f * 100))
				if f*100-float64(n) > 0.000001 || f*100-float64(n) < -0.000001 {
					return true, errors.New("bext loudness supports two decimal places")
				}
			}
			le.PutUint16(b[off:], uint16(int16(n)))
		} else {
			switch name {
			case "timereference":
				n, err := strconv.ParseUint(s, 10, 64)
				if err != nil {
					return true, err
				}
				le.PutUint64(b[338:], n)
			case "version":
				n, err := strconv.ParseUint(s, 10, 16)
				if err != nil || n > 2 {
					return true, errors.New("bext.version must be 0, 1 or 2")
				}
				le.PutUint16(b[346:], uint16(n))
			case "umid":
				data, err := hex.DecodeString(s)
				if err != nil || len(data) != 64 {
					return true, errors.New("UMID requires 128 hexadecimal characters")
				}
				if le.Uint16(b[346:]) < 1 {
					return true, errors.New("UMID requires bext version 1 or 2")
				}
				copy(b[348:412], data)
			case "codinghistory":
				b = append(b[:602], []byte(s)...)
			default:
				return true, fmt.Errorf("unknown bext field %s", name)
			}
		}
	}
	c := iffChunk{key: key, data: b, size: int64(len(b)), edited: true}
	if index >= 0 {
		v.chunks[index] = c
	} else {
		v.chunks = append(v.chunks, c)
	}
	return true, nil
}
func (v *iff) removeBroadcast(k string) (bool, error) {
	if k != "ixml" && k != "bext" && !strings.HasPrefix(k, "bext.") {
		return false, nil
	}
	if strings.HasPrefix(k, "bext.") {
		name := strings.TrimPrefix(k, "bext.")
		value := ""
		if _, ok := bextLoudness[name]; ok {
			value = "unknown"
		}
		if name == "timereference" {
			value = "0"
		}
		if name == "umid" {
			value = strings.Repeat("0", 128)
		}
		if name == "version" {
			return true, errors.New("set bext.version explicitly")
		}
		return v.setBroadcast(k, []string{value})
	}
	key := "bext"
	if k == "ixml" {
		key = "iXML"
	}
	out := v.chunks[:0]
	for _, c := range v.chunks {
		if c.key != key {
			out = append(out, c)
		}
	}
	v.chunks = out
	return true, nil
}
