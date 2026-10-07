package tag

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var id3v1Fields = map[string]bextField{"title": {3, 30}, "artist": {33, 30}, "album": {63, 30}, "date": {93, 4}, "comment": {97, 30}}
var id3v1Genres = strings.Split("Blues|Classic Rock|Country|Dance|Disco|Funk|Grunge|Hip-Hop|Jazz|Metal|New Age|Oldies|Other|Pop|R&B|Rap|Reggae|Rock|Techno|Industrial|Alternative|Ska|Death Metal|Pranks|Soundtrack|Euro-Techno|Ambient|Trip-Hop|Vocal|Jazz+Funk|Fusion|Trance|Classical|Instrumental|Acid|House|Game|Sound Clip|Gospel|Noise|AlternRock|Bass|Soul|Punk|Space|Meditative|Instrumental Pop|Instrumental Rock|Ethnic|Gothic|Darkwave|Techno-Industrial|Electronic|Pop-Folk|Eurodance|Dream|Southern Rock|Comedy|Cult|Gangsta|Top 40|Christian Rap|Pop/Funk|Jungle|Native American|Cabaret|New Wave|Psychadelic|Rave|Showtunes|Trailer|Lo-Fi|Tribal|Acid Punk|Acid Jazz|Polka|Retro|Musical|Rock & Roll|Hard Rock|Folk|Folk-Rock|National Folk|Swing|Fast Fusion|Bebob|Latin|Revival|Celtic|Bluegrass|Avantgarde|Gothic Rock|Progressive Rock|Psychedelic Rock|Symphonic Rock|Slow Rock|Big Band|Chorus|Easy Listening|Acoustic|Humour|Speech|Chanson|Opera|Chamber Music|Sonata|Symphony|Booty Bass|Primus|Porn Groove|Satire|Slow Jam|Club|Tango|Samba|Folklore|Ballad|Power Ballad|Rhythmic Soul|Freestyle|Duet|Punk Rock|Drum Solo|A capella|Euro-House|Dance Hall|Goa|Drum & Bass|Club-House|Hardcore|Terror|Indie|BritPop|Negerpunk|Polsk Punk|Beat|Christian Gangsta Rap|Heavy Metal|Black Metal|Crossover|Contemporary Christian|Christian Rock|Merengue|Salsa|Thrash Metal|Anime|JPop|SynthPop", "|")

func legacyBytes(s string, n int) []byte {
	b := make([]byte, n)
	i := 0
	for _, r := range s {
		if i == n {
			break
		}
		if r > 255 {
			r = '?'
		}
		b[i] = byte(r)
		i++
	}
	return b
}
func genreNumber(s string) (byte, error) {
	if n, err := strconv.ParseUint(strings.Trim(s, "()"), 10, 8); err == nil {
		return byte(n), nil
	}
	for i, g := range id3v1Genres {
		if strings.EqualFold(g, s) {
			return byte(i), nil
		}
	}
	if strings.EqualFold(s, "unknown") {
		return 255, nil
	}
	return 0, fmt.Errorf("unknown ID3v1 genre %q; use a numeric code 0..255", s)
}
func (v *mp3) legacyTags(m map[string][]string) {
	if len(v.legacy) != 128 {
		return
	}
	for name, f := range id3v1Fields {
		n := f.length
		if name == "comment" && v.legacy[125] == 0 && v.legacy[126] != 0 {
			n = 28
		}
		m["id3v1."+name] = []string{fromLatin1(bytes.TrimRight(v.legacy[f.offset:f.offset+n], "\x00 "))}
	}
	if v.legacy[125] == 0 && v.legacy[126] != 0 {
		m["id3v1.tracknumber"] = []string{strconv.Itoa(int(v.legacy[126]))}
	}
	m["id3v1.genre"] = []string{strconv.Itoa(int(v.legacy[127]))}
}
func (v *mp3) tags() map[string][]string { m := v.id3.tags(); v.legacyTags(m); return m }
func (v *mp3) set(k string, values []string) error {
	k = canon(k)
	if !strings.HasPrefix(k, "id3v1.") {
		return v.id3.set(k, values)
	}
	if len(values) != 1 {
		return errors.New("ID3v1 fields require one value")
	}
	name := strings.TrimPrefix(k, "id3v1.")
	if _, ok := id3v1Fields[name]; !ok && name != "tracknumber" && name != "genre" {
		return fmt.Errorf("unknown ID3v1 field %s", name)
	}
	b := bytes.Clone(v.legacy)
	if len(b) == 0 {
		b = make([]byte, 128)
		copy(b, "TAG")
		b[127] = 255
	}
	if f, ok := id3v1Fields[name]; ok {
		n := f.length
		if name == "comment" && b[125] == 0 && b[126] != 0 {
			n = 28
		}
		copy(b[f.offset:f.offset+n], legacyBytes(values[0], n))
	} else if name == "tracknumber" {
		n, err := strconv.ParseUint(values[0], 10, 8)
		if err != nil {
			return err
		}
		b[125] = 0
		b[126] = byte(n)
	} else {
		n, err := genreNumber(values[0])
		if err != nil {
			return err
		}
		b[127] = n
	}
	v.legacy = b
	return nil
}
func (v *mp3) remove(k string) error {
	k = canon(k)
	if k == "id3v1" {
		v.legacy = nil
		return nil
	}
	if !strings.HasPrefix(k, "id3v1.") {
		return v.id3.remove(k)
	}
	if len(v.legacy) == 0 {
		return nil
	}
	value := ""
	if k == "id3v1.tracknumber" {
		value = "0"
	}
	if k == "id3v1.genre" {
		value = "255"
	}
	return v.set(k, []string{value})
}

// syncLegacy creates/updates ID3v1 from the primary ID3v2 values. It is opt-in.
func (v *mp3) syncLegacy() error {
	m := v.id3.tags()
	for _, name := range []string{"title", "artist", "album", "date", "comment", "tracknumber", "genre"} {
		value := ""
		if len(m[name]) > 0 {
			value = m[name][0]
		}
		if name == "date" && len(value) > 4 {
			value = value[:4]
		}
		if name == "tracknumber" {
			value = strings.SplitN(value, "/", 2)[0]
			n, err := strconv.ParseUint(value, 10, 8)
			if err != nil {
				n = 0
			}
			value = strconv.Itoa(int(n))
		}
		if name == "genre" {
			if _, err := genreNumber(value); err != nil {
				value = "255"
			}
		}
		if err := v.set("id3v1."+name, []string{value}); err != nil {
			return err
		}
	}
	return nil
}
func (f *File) SyncID3v1() error {
	v, ok := f.e.(*mp3)
	if !ok {
		return errors.New("--sync is supported only for MP3 ID3v1/ID3v2")
	}
	return v.syncLegacy()
}

// SyncTags follows explicitly edited ID3v1 fields, then updates the legacy copy.
// A field edited on both sides must agree after ID3v1's representation limits.
func (f *File) SyncTags(changed []string) error {
	v, ok := f.e.(*mp3)
	if !ok {
		return errors.New("--sync is supported only for MP3 ID3v1/ID3v2")
	}
	primary, legacy := map[string]bool{}, map[string]bool{}
	shared := func(k string) bool { _, ok := id3v1Fields[k]; return ok || k == "tracknumber" || k == "genre" }
	for _, key := range changed {
		key = canon(key)
		if key == "id3v1" {
			return errors.New("--sync cannot be combined with removing the entire ID3v1 tag")
		}
		if strings.HasPrefix(key, "id3v1.") {
			name := strings.TrimPrefix(key, "id3v1.")
			if shared(name) {
				legacy[name] = true
			}
		} else if shared(key) {
			primary[key] = true
		}
	}
	m := v.tags()
	var transfers = map[string]string{}
	for name := range legacy {
		value := ""
		if x := m["id3v1."+name]; len(x) > 0 {
			value = x[0]
		}
		if primary[name] {
			copy := *v
			copy.legacy = bytes.Clone(v.legacy)
			if err := copy.syncLegacy(); err != nil {
				return err
			}
			expected := copy.tags()["id3v1."+name]
			text := ""
			if len(expected) > 0 {
				text = expected[0]
			}
			if text != value {
				return fmt.Errorf("--sync conflicting ID3v1 and ID3v2 edits to %s", name)
			}
			continue
		}
		if name == "genre" {
			n, err := strconv.ParseUint(value, 10, 8)
			if err != nil {
				return err
			}
			if int(n) < len(id3v1Genres) {
				value = id3v1Genres[n]
			} else if n == 255 {
				value = ""
			}
		}
		transfers[name] = value
	}
	for name, value := range transfers {
		if value == "" {
			if err := v.id3.remove(name); err != nil {
				return err
			}
		} else {
			if err := v.id3.set(name, []string{value}); err != nil {
				return err
			}
		}
	}
	return v.syncLegacy()
}
