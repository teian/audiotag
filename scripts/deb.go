//go:build ignore

// Assemble Debian binary packages with the Go standard library.
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func main() {
	root := flag.String("root", "", "staging directory")
	output := flag.String("output", "", ".deb output")
	flag.Parse()
	if e := build(*root, *output); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func build(root, output string) (err error) {
	if root == "" || output == "" {
		return fmt.Errorf("--root and --output are required")
	}
	stamp := time.Unix(0, 0)
	if v := os.Getenv("SOURCE_DATE_EPOCH"); v != "" {
		n, e := strconv.ParseInt(v, 10, 64)
		if e != nil || n < 0 {
			return fmt.Errorf("invalid SOURCE_DATE_EPOCH")
		}
		stamp = time.Unix(n, 0)
	}
	control, e := pack(root, true, stamp)
	if e != nil {
		return e
	}
	data, e := pack(root, false, stamp)
	if e != nil {
		return e
	}
	f, e := os.Create(output)
	if e != nil {
		return e
	}
	defer func() {
		closeErr := f.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			os.Remove(output)
		}
	}()
	if _, e = io.WriteString(f, "!<arch>\n"); e != nil {
		return e
	}
	for _, item := range []struct {
		name string
		data []byte
	}{{"debian-binary", []byte("2.0\n")}, {"control.tar.gz", control}, {"data.tar.gz", data}} {
		h := fmt.Sprintf("%-16s%-12d%-6d%-6d%-8s%-10d`\n", item.name+"/", stamp.Unix(), 0, 0, "100644", len(item.data))
		if len(h) != 60 {
			return fmt.Errorf("ar header overflow")
		}
		if _, e = io.WriteString(f, h); e != nil {
			return e
		}
		if _, e = f.Write(item.data); e != nil {
			return e
		}
		if len(item.data)%2 != 0 {
			if _, e = f.Write([]byte{'\n'}); e != nil {
				return e
			}
		}
	}
	return nil
}
func pack(root string, control bool, stamp time.Time) ([]byte, error) {
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	gz.Header.ModTime = stamp
	tw := tar.NewWriter(gz)
	base := root
	if control {
		base = filepath.Join(root, "DEBIAN")
	}
	err := filepath.WalkDir(base, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if path == base {
			return nil
		}
		rel, e := filepath.Rel(base, path)
		if e != nil {
			return e
		}
		if !control && (rel == "DEBIAN" || strings.HasPrefix(rel, "DEBIAN"+string(filepath.Separator))) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() && !info.IsDir() {
			return fmt.Errorf("unsupported package entry %s", path)
		}
		h := &tar.Header{Name: "./" + filepath.ToSlash(rel), Mode: int64(info.Mode().Perm()), Uid: 0, Gid: 0, ModTime: stamp, Typeflag: tar.TypeReg, Size: info.Size()}
		if d.IsDir() {
			h.Name += "/"
			h.Typeflag = tar.TypeDir
			h.Size = 0
		}
		if e = tw.WriteHeader(h); e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		f, e := os.Open(path)
		if e != nil {
			return e
		}
		_, e = io.Copy(tw, f)
		ce := f.Close()
		if e == nil {
			e = ce
		}
		return e
	})
	ce := tw.Close()
	if err == nil {
		err = ce
	}
	ce = gz.Close()
	if err == nil {
		err = ce
	}
	return b.Bytes(), err
}
