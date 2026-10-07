package tag

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"regexp"
	"strings"
)

// Tree contents retain unknown elements, attributes, comments and text.
type xmlElement struct {
	Start   xml.StartElement
	Content []any
}

func readIXML(b []byte) (*xmlElement, error) {
	if err := validIXML(b); err != nil {
		return nil, err
	}
	d := xml.NewDecoder(bytes.NewReader(b))
	var root *xmlElement
	var stack []*xmlElement
	for {
		token, err := d.Token()
		if err == io.EOF {
			return root, nil
		}
		if err != nil {
			return nil, err
		}
		switch t := token.(type) {
		case xml.StartElement:
			e := &xmlElement{Start: t.Copy()}
			if len(stack) > 0 {
				parent := stack[len(stack)-1]
				parent.Content = append(parent.Content, e)
			} else {
				root = e
			}
			stack = append(stack, e)
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		default:
			if len(stack) > 0 {
				p := stack[len(stack)-1]
				p.Content = append(p.Content, xml.CopyToken(token))
			}
		}
	}
}
func writeXML(e *xmlElement, encoder *xml.Encoder) error {
	if err := encoder.EncodeToken(e.Start); err != nil {
		return err
	}
	for _, c := range e.Content {
		if child, ok := c.(*xmlElement); ok {
			if err := writeXML(child, encoder); err != nil {
				return err
			}
		} else {
			if err := encoder.EncodeToken(c.(xml.Token)); err != nil {
				return err
			}
		}
	}
	return encoder.EncodeToken(e.Start.End())
}
func ixmlText(e *xmlElement) (string, bool) {
	var b strings.Builder
	for _, c := range e.Content {
		if _, ok := c.(*xmlElement); ok {
			return "", false
		}
		if t, ok := c.(xml.CharData); ok {
			b.Write(t)
		}
	}
	return b.String(), true
}
func inspectIXML(b []byte, m map[string][]string) {
	root, err := readIXML(b)
	if err != nil {
		return
	}
	var walk func(*xmlElement, string)
	walk = func(e *xmlElement, path string) {
		if value, leaf := ixmlText(e); leaf && path != "" {
			m["ixml."+path] = append(m["ixml."+path], value)
		}
		for _, c := range e.Content {
			if child, ok := c.(*xmlElement); ok {
				next := strings.ToLower(child.Start.Name.Local)
				if path != "" {
					next = path + "." + next
				}
				walk(child, next)
			}
		}
	}
	walk(root, "")
}
func (v *iff) setIXMLPath(k string, values []string) (bool, error) {
	if !strings.HasPrefix(k, "ixml.") {
		return false, nil
	}
	if v.kind != "wav" {
		return true, errors.New("iXML requires WAV")
	}
	parts := strings.Split(strings.TrimPrefix(k, "ixml."), ".")
	valid := regexp.MustCompile(`^[a-z_][a-z0-9_-]*$`)
	for _, part := range parts {
		if !valid.MatchString(part) {
			return true, errors.New("invalid iXML field path")
		}
	}
	var b []byte
	count := 0
	for _, c := range v.chunks {
		if c.key == "iXML" {
			b = c.data
			count++
		}
	}
	if count > 1 {
		return true, errors.New("duplicate iXML chunks")
	}
	if b == nil {
		b = []byte("<BWFXML/>")
	}
	root, err := readIXML(b)
	if err != nil {
		return true, err
	}
	nodes := []*xmlElement{root}
	for _, part := range parts {
		var next []*xmlElement
		for _, parent := range nodes {
			for _, c := range parent.Content {
				if child, ok := c.(*xmlElement); ok && strings.EqualFold(child.Start.Name.Local, part) {
					next = append(next, child)
				}
			}
		}
		if len(next) == 0 {
			if len(nodes) != 1 {
				return true, errors.New("ambiguous iXML parent; edit the whole document")
			}
			child := &xmlElement{Start: xml.StartElement{Name: xml.Name{Local: strings.ToUpper(part)}}}
			nodes[0].Content = append(nodes[0].Content, child)
			next = append(next, child)
		}
		nodes = next
	}
	if len(nodes) != len(values) {
		return true, errors.New("iXML values must match the number of matching leaf elements")
	}
	for i, node := range nodes {
		if _, leaf := ixmlText(node); !leaf {
			return true, errors.New("iXML field is structured; edit the whole document")
		}
		var retained []any
		for _, token := range node.Content {
			if _, text := token.(xml.CharData); !text {
				retained = append(retained, token)
			}
		}
		node.Content = append(retained, xml.CharData([]byte(values[i])))
	}
	var output bytes.Buffer
	encoder := xml.NewEncoder(&output)
	if err = writeXML(root, encoder); err != nil {
		return true, err
	}
	if err = encoder.Flush(); err != nil {
		return true, err
	}
	return v.setBroadcast("ixml", []string{output.String()})
}

func (v *iff) removeIXMLPath(k string) (bool, error) {
	if !strings.HasPrefix(k, "ixml.") {
		return false, nil
	}
	var b []byte
	count := 0
	for _, c := range v.chunks {
		if c.key == "iXML" {
			b = c.data
			count++
		}
	}
	if count > 1 {
		return true, errors.New("duplicate iXML chunks")
	}
	if b == nil {
		return true, nil
	}
	root, err := readIXML(b)
	if err != nil {
		return true, err
	}
	parts := strings.Split(strings.TrimPrefix(k, "ixml."), ".")
	var remove func(*xmlElement, int)
	remove = func(parent *xmlElement, depth int) {
		var out []any
		for _, token := range parent.Content {
			child, ok := token.(*xmlElement)
			if ok && strings.EqualFold(child.Start.Name.Local, parts[depth]) {
				if depth == len(parts)-1 {
					continue
				}
				remove(child, depth+1)
			}
			out = append(out, token)
		}
		parent.Content = out
	}
	remove(root, 0)
	var out bytes.Buffer
	encoder := xml.NewEncoder(&out)
	if err := writeXML(root, encoder); err != nil {
		return true, err
	}
	if err := encoder.Flush(); err != nil {
		return true, err
	}
	return v.setBroadcast("ixml", []string{out.String()})
}
