package tag

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

type comments struct {
	vendor  string
	values  []Raw
	trailer []byte
}

func parseComments(b []byte) (comments, error) { return parseCommentsMode(b, true) }

// Only use views for internally owned, immutable input buffers.
func parseCommentsView(b []byte) (comments, error) { return parseCommentsMode(b, false) }
func parseCommentsMode(b []byte, owned bool) (comments, error) {
	c := comments{}
	pos := 0
	take := func() ([]byte, error) {
		if len(b)-pos < 4 {
			return nil, errors.New("truncated Vorbis comment")
		}
		n := int(le.Uint32(b[pos:]))
		pos += 4
		if n > len(b)-pos {
			return nil, errors.New("truncated Vorbis string")
		}
		s := b[pos : pos+n]
		pos += n
		return s, nil
	}
	v, e := take()
	if e != nil {
		return c, e
	}
	c.vendor = string(v)
	if len(b)-pos < 4 {
		return c, errors.New("missing comment count")
	}
	n := int(le.Uint32(b[pos:]))
	pos += 4
	if n > (len(b)-pos)/4 {
		return c, errors.New("invalid comment count")
	}
	for i := 0; i < n; i++ {
		s, e := take()
		if e != nil {
			return c, e
		}
		split := bytes.IndexByte(s, '=')
		if split < 0 {
			return c, errors.New("invalid Vorbis comment")
		}
		k := string(s[:split])
		value := s[split+1:]
		if !validCommentKey(k) || !utf8.Valid(value) {
			return c, errors.New("invalid Vorbis comment")
		}
		if owned {
			value = bytes.Clone(value)
		}
		c.values = append(c.values, Raw{k, 0, value})
	}
	c.trailer = b[pos:]
	if owned {
		c.trailer = bytes.Clone(c.trailer)
	}
	return c, nil
}
func validCommentKey(k string) bool {
	if k == "" {
		return false
	}
	for _, c := range k {
		if c < 0x20 || c > 0x7d || c == '=' {
			return false
		}
	}
	return true
}
func (c *comments) encodedSize() int {
	n := 8 + len(c.vendor)
	for _, r := range c.values {
		n += 5 + len(r.Key) + len(r.Data)
	}
	return n
}
func (c *comments) encode() []byte { b := make([]byte, c.encodedSize()); c.encodeTo(b); return b }
func (c *comments) encodeTo(b []byte) {
	le.PutUint32(b, uint32(len(c.vendor)))
	pos := 4 + copy(b[4:], c.vendor)
	le.PutUint32(b[pos:], uint32(len(c.values)))
	pos += 4
	for _, r := range c.values {
		le.PutUint32(b[pos:], uint32(len(r.Key)+1+len(r.Data)))
		pos += 4
		pos += copy(b[pos:], r.Key)
		b[pos] = '='
		pos++
		pos += copy(b[pos:], r.Data)
	}
}
func (c *comments) tags() map[string][]string {
	m := map[string][]string{}
	for _, r := range c.values {
		m[canon(r.Key)] = append(m[canon(r.Key)], string(r.Data))
	}
	return m
}
func (c *comments) set(k string, v []string) error {
	k = canon(k)
	if !validCommentKey(k) {
		return errors.New("invalid comment key")
	}
	c.remove(k)
	for _, s := range v {
		c.values = append(c.values, Raw{strings.ToUpper(k), 0, []byte(s)})
	}
	return nil
}
func (c *comments) remove(k string) error {
	out := c.values[:0]
	for _, r := range c.values {
		if canon(r.Key) != canon(k) {
			out = append(out, r)
		}
	}
	c.values = out
	return nil
}
func (c *comments) replace(r []Raw) error {
	for _, x := range r {
		if !validCommentKey(x.Key) || !utf8.Valid(x.Data) || x.Flags != 0 {
			return errors.New("invalid Vorbis native tag")
		}
	}
	c.values = cloneRaw(r)
	return nil
}
func (c *comments) pictures() ([]Picture, error) {
	var p []Picture
	for _, r := range c.values {
		if strings.EqualFold(r.Key, "METADATA_BLOCK_PICTURE") {
			b, e := base64.StdEncoding.DecodeString(string(r.Data))
			if e != nil {
				return nil, e
			}
			v, e := parsePicture(b)
			if e != nil {
				return nil, e
			}
			p = append(p, v)
		}
	}
	return p, nil
}
func (c *comments) cover(p Picture) error {
	var out []Raw
	for _, r := range c.values {
		if strings.EqualFold(r.Key, "METADATA_BLOCK_PICTURE") {
			b, e := base64.StdEncoding.DecodeString(string(r.Data))
			if e != nil {
				return e
			}
			v, e := parsePicture(b)
			if e != nil {
				return e
			}
			if v.Type == 3 {
				continue
			}
		}
		out = append(out, r)
	}
	c.values = out
	c.values = append(c.values, Raw{"METADATA_BLOCK_PICTURE", 0, []byte(base64.StdEncoding.EncodeToString(encodePicture(p)))})
	return nil
}
func (c *comments) chapters() ([]Chapter, error) {
	m := c.tags()
	var out []Chapter
	for i := 0; i < 100000; i++ {
		key := fmt.Sprintf("chapter%03d", i)
		v := m[key]
		if len(v) == 0 {
			if i == 0 {
				continue
			}
			break
		}
		s, e := parseTime(v[0])
		if e != nil {
			return nil, e
		}
		title := ""
		if x := m[key+"name"]; len(x) > 0 {
			title = x[0]
		}
		out = append(out, Chapter{Start: s, Title: title})
	}
	for i := 0; i+1 < len(out); i++ {
		out[i].End = out[i+1].Start
	}
	return out, nil
}
func (c *comments) setChapters(ch []Chapter) error {
	out := c.values[:0]
	for _, r := range c.values {
		k := strings.ToUpper(r.Key)
		if isChapterKey(k) {
			continue
		}
		out = append(out, r)
	}
	c.values = out
	for i, v := range ch {
		ms := int64(v.Start*1000 + 0.5)
		key := fmt.Sprintf("CHAPTER%03d", i)
		c.values = append(c.values, Raw{key, 0, []byte(fmt.Sprintf("%02d:%02d:%02d.%03d", ms/3600000, ms/60000%60, ms/1000%60, ms%1000))}, Raw{key + "NAME", 0, []byte(v.Title)})
	}
	return nil
}
func isChapterKey(k string) bool {
	if !strings.HasPrefix(k, "CHAPTER") {
		return false
	}
	s := strings.TrimSuffix(k[7:], "NAME")
	if len(s) < 3 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
func parseTime(s string) (float64, error) {
	parts := strings.Split(s, ":")
	if len(parts) != 3 {
		return 0, errors.New("chapter time requires HH:MM:SS.mmm")
	}
	h, e := strconv.ParseUint(parts[0], 10, 32)
	if e != nil {
		return 0, e
	}
	m, e := strconv.ParseUint(parts[1], 10, 32)
	if e != nil || m >= 60 {
		return 0, errors.New("invalid chapter minutes")
	}
	sec, e := strconv.ParseFloat(parts[2], 64)
	if e != nil || sec < 0 || sec >= 60 || sec != sec {
		return 0, errors.New("invalid chapter seconds")
	}
	return float64(h)*3600 + float64(m)*60 + sec, nil
}
func parsePicture(b []byte) (Picture, error) {
	p := Picture{}
	pos := 0
	read := func() (uint32, error) {
		if len(b)-pos < 4 {
			return 0, errors.New("truncated picture")
		}
		v := be.Uint32(b[pos:])
		pos += 4
		return v, nil
	}
	take := func() ([]byte, error) {
		n, e := read()
		if e != nil {
			return nil, e
		}
		if uint64(n) > uint64(len(b)-pos) {
			return nil, errors.New("truncated picture field")
		}
		v := b[pos : pos+int(n)]
		pos += int(n)
		return v, nil
	}
	var e error
	p.Type, e = read()
	if e != nil {
		return p, e
	}
	v, e := take()
	if e != nil {
		return p, e
	}
	p.MIME = string(v)
	v, e = take()
	if e != nil {
		return p, e
	}
	p.Description = string(v)
	for i := 0; i < 4; i++ {
		if _, e = read(); e != nil {
			return p, e
		}
	}
	p.Data, e = take()
	return p, e
}
func encodePicture(p Picture) []byte {
	var b bytes.Buffer
	put32(&b, p.Type)
	put32(&b, uint32(len(p.MIME)))
	b.WriteString(p.MIME)
	put32(&b, uint32(len(p.Description)))
	b.WriteString(p.Description)
	for i := 0; i < 4; i++ {
		put32(&b, 0)
	}
	put32(&b, uint32(len(p.Data)))
	b.Write(p.Data)
	return b.Bytes()
}
