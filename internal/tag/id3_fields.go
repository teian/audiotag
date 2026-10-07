package tag

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

var id3URLs = map[string]string{"artisturl": "WOAR", "audiourl": "WOAF", "sourceurl": "WOAS", "commercialurl": "WCOM", "copyrighturl": "WCOP", "radiourl": "WORS", "paymenturl": "WPAY", "publisherurl": "WPUB"}

// Described frames use a qualified key so ordinary writes keep their existing semantics.
// comment:LANG:DESCRIPTION, lyrics:LANG:DESCRIPTION, url:DESCRIPTION, rating:EMAIL, playcount:EMAIL.
func describedID3Key(k string) (id, lang, description string, ok bool) {
	parts := strings.SplitN(k, ":", 3)
	if len(parts) == 3 && (parts[0] == "comment" || parts[0] == "lyrics") {
		id = "COMM"
		if parts[0] == "lyrics" {
			id = "USLT"
		}
		return id, parts[1], parts[2], true
	}
	if strings.HasPrefix(k, "url:") {
		return "WXXX", "", strings.TrimPrefix(k, "url:"), true
	}
	return "", "", "", false
}
func validLanguage(lang string) bool {
	if len(lang) != 3 {
		return false
	}
	for _, c := range lang {
		if c < 'a' || c > 'z' {
			return false
		}
	}
	return true
}
func latin1(s string) ([]byte, error) {
	var b []byte
	for _, r := range s {
		if r == 0 || r > 255 {
			return nil, errors.New("URL/identifier must be Latin-1 without NUL")
		}
		b = append(b, byte(r))
	}
	return b, nil
}
func fromLatin1(b []byte) string {
	var s strings.Builder
	for _, c := range b {
		s.WriteRune(rune(c))
	}
	return s.String()
}
func describedID3(r Raw) (key, value string, ok bool) {
	if r.Flags != 0 || len(r.Data) == 0 {
		return "", "", false
	}
	switch r.Key {
	case "COMM", "USLT":
		if len(r.Data) < 4 {
			return "", "", false
		}
		desc, body, valid := splitEncoded(r.Data[0], r.Data[4:])
		if !valid {
			return "", "", false
		}
		prefix := "comment"
		if r.Key == "USLT" {
			prefix = "lyrics"
		}
		return prefix + ":" + string(r.Data[1:4]) + ":" + decodeText(r.Data[0], desc), strings.TrimRight(decodeText(r.Data[0], body), "\x00"), true
	case "WXXX":
		desc, body, valid := splitEncoded(r.Data[0], r.Data[1:])
		if !valid {
			return "", "", false
		}
		return "url:" + decodeText(r.Data[0], desc), fromLatin1(bytes.TrimRight(body, "\x00")), true
	}
	return "", "", false
}
func (v *id3) fieldTags(m map[string][]string) {
	v.timingTags(m)
	for _, r := range v.frames {
		if r.Flags != 0 {
			continue
		}
		if key, value, ok := describedID3(r); ok {
			m[key] = append(m[key], value)
		}
		if strings.HasPrefix(r.Key, "W") && r.Key != "WXXX" {
			key := "raw:" + r.Key
			for k, id := range id3URLs {
				if id == r.Key {
					key = k
				}
			}
			m[key] = append(m[key], fromLatin1(bytes.TrimRight(r.Data, "\x00")))
		}
		if r.Key == "POPM" {
			email, body, ok := bytes.Cut(r.Data, []byte{0})
			if !ok || len(body) < 1 {
				continue
			}
			m["rating:"+string(email)] = []string{strconv.Itoa(int(body[0]))}
			if len(body) <= 9 {
				var n uint64
				for _, b := range body[1:] {
					n = n<<8 | uint64(b)
				}
				m["playcount:"+string(email)] = []string{strconv.FormatUint(n, 10)}
			}
		}
	}
}
func (v *id3) removeField(k string) (bool, error) {
	id, _, _, described := describedID3Key(k)
	url := id3URLs[k]
	if strings.HasPrefix(k, "raw:W") {
		url = strings.TrimPrefix(k, "raw:")
	}
	if k == "synchronizedlyrics" || k == "eventtiming" {
		id := "SYLT"
		if k == "eventtiming" {
			id = "ETCO"
		}
		out := v.frames[:0]
		for _, r := range v.frames {
			if r.Key != id {
				out = append(out, r)
			}
		}
		v.frames = out
		return true, nil
	}
	rating := strings.HasPrefix(k, "rating:") || strings.HasPrefix(k, "playcount:")
	if !described && url == "" && !rating {
		return false, nil
	}
	out := v.frames[:0]
	for _, r := range v.frames {
		match := false
		if described && r.Key == id {
			key, _, ok := describedID3(r)
			match = ok && key == k
		}
		if url != "" {
			match = r.Key == url
		}
		if rating && r.Key == "POPM" {
			email, _, ok := bytes.Cut(r.Data, []byte{0})
			match = ok && string(email) == strings.SplitN(k, ":", 2)[1]
		}
		if !match {
			out = append(out, r)
		}
	}
	v.frames = out
	return true, nil
}
func (v *id3) setField(k string, values []string) (bool, error) {
	id, lang, desc, ok := describedID3Key(k)
	if ok {
		if strings.ContainsRune(desc, 0) {
			return true, errors.New("description cannot contain NUL")
		}
		if len(values) != 1 {
			return true, errors.New("described ID3 fields require one value")
		}
		if id != "WXXX" && !validLanguage(lang) {
			return true, errors.New("language must be three lowercase ASCII letters")
		}
		d := encodeText(v.version, desc)
		data := []byte{d[0]}
		if id != "WXXX" {
			data = append(data, lang...)
		}
		data = append(data, d[1:]...)
		data = append(data, 0)
		if d[0] == 1 || d[0] == 2 {
			data = append(data, 0)
		}
		if id == "WXXX" {
			b, err := latin1(values[0])
			if err != nil {
				return true, err
			}
			data = append(data, b...)
		} else {
			b := encodeText(v.version, values[0])
			data = append(data, b[1:]...)
		}
		v.removeField(k)
		v.frames = append(v.frames, Raw{id, 0, data})
		return true, nil
	}
	url := id3URLs[k]
	if strings.HasPrefix(k, "raw:W") {
		url = strings.TrimPrefix(k, "raw:")
	}
	if url != "" {
		if !validFrameID(url) || url == "WXXX" {
			return true, errors.New("use url:DESCRIPTION for WXXX")
		}
		if len(values) != 1 {
			return true, errors.New("URL frames require one value")
		}
		b, err := latin1(values[0])
		if err != nil {
			return true, err
		}
		v.removeField(k)
		v.frames = append(v.frames, Raw{url, 0, b})
		return true, nil
	}
	if strings.HasPrefix(k, "rating:") || strings.HasPrefix(k, "playcount:") {
		if len(values) != 1 {
			return true, errors.New("rating/playcount require one value")
		}
		parts := strings.SplitN(k, ":", 2)
		email := parts[1]
		if email == "" || !utf8.ValidString(email) {
			return true, errors.New("rating/playcount need an owner identifier")
		}
		b, err := latin1(email)
		if err != nil {
			return true, err
		}
		n, err := strconv.ParseUint(values[0], 10, 64)
		if err != nil || parts[0] == "rating" && n > 255 {
			return true, errors.New("rating must be 0..255; playcount must be uint64")
		}
		rating := byte(0)
		count := []byte{0, 0, 0, 0}
		for _, r := range v.frames {
			if r.Key != "POPM" {
				continue
			}
			owner, body, ok := bytes.Cut(r.Data, []byte{0})
			if ok && string(owner) == email && len(body) > 0 {
				if r.Flags != 0 {
					return true, errors.New("flagged POPM requires native import")
				}
				rating = body[0]
				count = bytes.Clone(body[1:])
				break
			}
		}
		if parts[0] == "rating" {
			rating = byte(n)
		} else {
			width := 4
			if n > 0xffffffff {
				width = 8
			}
			count = make([]byte, width)
			for i := width - 1; i >= 0; i-- {
				count[i] = byte(n)
				n >>= 8
			}
		}
		v.removeField(k)
		data := append(b, 0, rating)
		data = append(data, count...)
		v.frames = append(v.frames, Raw{"POPM", 0, data})
		return true, nil
	}
	if k == "synchronizedlyrics" || k == "eventtiming" {
		return true, v.setTiming(k, values)
	}
	return false, nil
}

// Structured time data is exposed as a JSON value, usable with --set-file.
type LyricPoint struct {
	Time uint32 `json:"milliseconds"`
	Text string `json:"text"`
}
type SynchronizedLyrics struct {
	Language    string       `json:"language"`
	Description string       `json:"description"`
	ContentType byte         `json:"content_type"`
	Entries     []LyricPoint `json:"entries"`
}
type TimingEvent struct {
	Type byte   `json:"type"`
	Time uint32 `json:"milliseconds"`
}

func (v *id3) setTiming(k string, values []string) error {
	if len(values) != 1 {
		return errors.New("timing fields require one JSON value")
	}
	var data []byte
	id := "ETCO"
	if k == "synchronizedlyrics" {
		id = "SYLT"
		var lyrics SynchronizedLyrics
		if strings.HasPrefix(strings.TrimSpace(values[0]), "{") {
			if err := json.Unmarshal([]byte(values[0]), &lyrics); err != nil {
				return err
			}
		} else {
			var err error
			lyrics, err = parseLRC(values[0])
			if err != nil {
				return err
			}
		}
		if !validLanguage(lyrics.Language) || lyrics.ContentType > 8 || strings.ContainsRune(lyrics.Description, 0) {
			return errors.New("invalid SYLT language/content type")
		}
		desc := encodeText(v.version, lyrics.Description)
		data = []byte{desc[0]}
		data = append(data, lyrics.Language...)
		data = append(data, 2, lyrics.ContentType)
		data = append(data, desc[1:]...)
		data = append(data, 0)
		if desc[0] == 1 {
			data = append(data, 0)
		}
		var prev uint32
		for i, e := range lyrics.Entries {
			if i > 0 && e.Time < prev || strings.ContainsRune(e.Text, 0) {
				return errors.New("invalid SYLT timestamp/text")
			}
			text := encodeText(v.version, e.Text)
			data = append(data, text[1:]...)
			data = append(data, 0)
			if text[0] == 1 {
				data = append(data, 0)
			}
			var stamp [4]byte
			be.PutUint32(stamp[:], e.Time)
			data = append(data, stamp[:]...)
			prev = e.Time
		}
	} else {
		var events []TimingEvent
		if err := json.Unmarshal([]byte(values[0]), &events); err != nil {
			return err
		}
		data = []byte{2}
		var prev uint32
		for i, e := range events {
			if i > 0 && e.Time < prev {
				return fmt.Errorf("event times must be increasing")
			}
			data = append(data, e.Type)
			var stamp [4]byte
			be.PutUint32(stamp[:], e.Time)
			data = append(data, stamp[:]...)
			prev = e.Time
		}
	}
	out := v.frames[:0]
	for _, r := range v.frames {
		if r.Key != id {
			out = append(out, r)
		}
	}
	v.frames = append(out, Raw{id, 0, data})
	return nil
}

func (v *id3) timingTags(m map[string][]string) {
	for _, r := range v.frames {
		if r.Flags != 0 {
			continue
		}
		if r.Key == "ETCO" {
			if len(r.Data) < 1 || r.Data[0] != 2 || (len(r.Data)-1)%5 != 0 {
				continue
			}
			var events []TimingEvent
			for p := 1; p < len(r.Data); p += 5 {
				events = append(events, TimingEvent{r.Data[p], be.Uint32(r.Data[p+1:])})
			}
			b, _ := json.Marshal(events)
			m["eventtiming"] = []string{string(b)}
		}
		if r.Key == "SYLT" {
			if len(r.Data) < 7 || r.Data[4] != 2 {
				continue
			}
			enc := r.Data[0]
			desc, body, ok := splitEncoded(enc, r.Data[6:])
			if !ok {
				continue
			}
			lyrics := SynchronizedLyrics{Language: string(r.Data[1:4]), Description: decodeText(enc, desc), ContentType: r.Data[5]}
			valid := true
			for len(body) > 0 {
				text, remaining, ok := splitEncoded(enc, body)
				if !ok || len(remaining) < 4 {
					valid = false
					break
				}
				lyrics.Entries = append(lyrics.Entries, LyricPoint{be.Uint32(remaining), decodeText(enc, text)})
				body = remaining[4:]
			}
			if valid {
				b, _ := json.Marshal(lyrics)
				m["synchronizedlyrics"] = []string{string(b)}
				var lrc strings.Builder
				for _, e := range lyrics.Entries {
					fmt.Fprintf(&lrc, "[%02d:%02d.%03d]%s\n", e.Time/60000, e.Time/1000%60, e.Time%1000, e.Text)
				}
				m["synchronizedlyrics_lrc"] = []string{lrc.String()}
			}
		}
	}
}
func parseLRC(s string) (SynchronizedLyrics, error) {
	lyrics := SynchronizedLyrics{Language: "eng", ContentType: 1}
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		var stamps []uint32
		for strings.HasPrefix(line, "[") {
			close := strings.IndexByte(line, ']')
			if close < 0 {
				return lyrics, errors.New("invalid LRC timestamp")
			}
			stamp := line[1:close]
			parts := strings.SplitN(stamp, ":", 2)
			if len(parts) != 2 {
				return lyrics, errors.New("invalid LRC timestamp")
			}
			minutes, err := strconv.ParseUint(parts[0], 10, 32)
			if err != nil {
				return lyrics, err
			}
			seconds, err := strconv.ParseFloat(parts[1], 64)
			if err != nil || seconds < 0 || seconds >= 60 || seconds != seconds {
				return lyrics, errors.New("invalid LRC seconds")
			}
			ms := float64(minutes)*60000 + seconds*1000
			if ms > float64(^uint32(0)) {
				return lyrics, errors.New("LRC timestamp exceeds 32-bit milliseconds")
			}
			stamps = append(stamps, uint32(ms+0.5))
			line = line[close+1:]
		}
		if len(stamps) == 0 {
			return lyrics, errors.New("LRC line needs a timestamp (metadata/offset directives are not supported)")
		}
		for _, stamp := range stamps {
			lyrics.Entries = append(lyrics.Entries, LyricPoint{stamp, line})
		}
	}
	sort.SliceStable(lyrics.Entries, func(i, j int) bool { return lyrics.Entries[i].Time < lyrics.Entries[j].Time })
	return lyrics, nil
}
