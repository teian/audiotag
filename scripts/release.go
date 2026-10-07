//go:build ignore

// Build release binaries and deterministic archives using only the Go toolchain.
package main

import (
	"archive/tar"
	"archive/zip"
	"audiotag/internal/release"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func run() error {
	version := os.Getenv("VERSION")
	if version == "" {
		version = "1.0.0"
	}
	for _, c := range version {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || strings.ContainsRune(".-_", c)) {
			return fmt.Errorf("invalid VERSION")
		}
	}
	targets := strings.Fields(os.Getenv("TARGETS"))
	if len(targets) == 0 {
		targets = release.Targets
	}
	epoch := int64(0)
	if s := os.Getenv("SOURCE_DATE_EPOCH"); s != "" {
		n, e := strconv.ParseInt(s, 10, 64)
		if e != nil || n < 0 {
			return fmt.Errorf("invalid SOURCE_DATE_EPOCH")
		}
		epoch = n
	}
	stamp := time.Unix(epoch, 0).UTC()
	os.MkdirAll("dist", 0755)
	var sums []string
	sourcePath := filepath.Join("dist", "audiotag-"+version+"-source.tar.gz")
	sourceCmd := exec.Command("go", "run", "scripts/source.go", "--output", sourcePath)
	sourceCmd.Env = append(os.Environ(), "VERSION="+version)
	sourceCmd.Stdout, sourceCmd.Stderr = os.Stdout, os.Stderr
	if err := sourceCmd.Run(); err != nil {
		return err
	}
	source, err := os.ReadFile(sourcePath)
	if err != nil {
		return err
	}
	sourceHash := sha256.Sum256(source)
	sums = append(sums, hex.EncodeToString(sourceHash[:])+"  "+filepath.Base(sourcePath))
	for _, target := range targets {
		parts := strings.Split(target, "/")
		if len(parts) != 2 {
			return fmt.Errorf("invalid target %s", target)
		}
		name := "audiotag-" + version + "-" + parts[0] + "-" + parts[1]
		dir := filepath.Join("dist", name)
		if e := os.MkdirAll(filepath.Join(dir, "completions"), 0755); e != nil {
			return e
		}
		binary := "audiotag"
		if parts[0] == "windows" {
			binary += ".exe"
		}
		cmd := exec.Command("go", "build", "-buildvcs=false", "-trimpath", "-ldflags", "-s -w -buildid= -X main.version="+version, "-o", filepath.Join(dir, binary), "./cmd/audiotag")
		cmd.Env = append(os.Environ(), "GOOS="+parts[0], "GOARCH="+parts[1], "CGO_ENABLED=0", "GOARM=7")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		fmt.Println("Building", target)
		if e := cmd.Run(); e != nil {
			return e
		}
		for _, f := range []string{"README.md", "TAGS.md", "BENCHMARKS.md", "CONTRIBUTING.md", "CLA.md", "LICENSE", "NOTICE", "Go-BSD-3-Clause.txt", "cmd/audiotag/completions/audiotag.bash", "cmd/audiotag/completions/audiotag.zsh", "cmd/audiotag/completions/audiotag.fish", "examples/book.json", "examples/tags.json", "examples/chapters.json"} {
			b, e := os.ReadFile(f)
			if e != nil {
				return e
			}
			dst := filepath.Join(dir, filepath.Base(f))
			if strings.Contains(f, "completions/") {
				dst = filepath.Join(dir, "completions", filepath.Base(f))
			}
			if strings.HasPrefix(f, "examples/") {
				dst = filepath.Join(dir, f)
				if e = os.MkdirAll(filepath.Dir(dst), 0755); e != nil {
					return e
				}
			}
			if e = os.WriteFile(dst, b, 0644); e != nil {
				return e
			}
		}
		if err := os.WriteFile(filepath.Join(dir, "source.tar.gz"), source, 0644); err != nil {
			return err
		}
		ext := ".tar.gz"
		if parts[0] == "windows" {
			ext = ".zip"
		}
		archive := filepath.Join("dist", name+ext)
		if e := pack(dir, archive, stamp, parts[0] == "windows"); e != nil {
			return e
		}
		f, e := os.Open(archive)
		if e != nil {
			return e
		}
		h := sha256.New()
		_, e = io.Copy(h, f)
		f.Close()
		if e != nil {
			return e
		}
		sums = append(sums, hex.EncodeToString(h.Sum(nil))+"  "+filepath.Base(archive))
	}
	sort.Strings(sums)
	return os.WriteFile("dist/SHA256SUMS", []byte(strings.Join(sums, "\n")+"\n"), 0644)
}
func pack(dir, dst string, stamp time.Time, isZip bool) (err error) {
	f, e := os.Create(dst)
	if e != nil {
		return e
	}
	defer func() {
		e := f.Close()
		if err == nil {
			err = e
		}
	}()
	var tw *tar.Writer
	var zw *zip.Writer
	var gz *gzip.Writer
	if isZip {
		zw = zip.NewWriter(f)
	} else {
		gz = gzip.NewWriter(f)
		gz.Header.ModTime = stamp
		tw = tar.NewWriter(gz)
	}
	err = filepath.WalkDir(dir, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		rel, e := filepath.Rel(filepath.Dir(dir), path)
		if e != nil {
			return e
		}
		name := filepath.ToSlash(rel)
		var w io.Writer
		if isZip {
			h := &zip.FileHeader{Name: name, Method: zip.Deflate}
			h.SetMode(info.Mode())
			h.SetModTime(stamp)
			w, e = zw.CreateHeader(h)
		} else {
			h := &tar.Header{Name: name, Size: info.Size(), Mode: int64(info.Mode().Perm()), ModTime: stamp}
			e = tw.WriteHeader(h)
			w = tw
		}
		if e != nil {
			return e
		}
		src, e := os.Open(path)
		if e != nil {
			return e
		}
		defer src.Close()
		_, e = io.Copy(w, src)
		return e
	})
	if isZip {
		e = zw.Close()
	} else {
		e = tw.Close()
		if ce := gz.Close(); e == nil {
			e = ce
		}
	}
	if err == nil {
		err = e
	}
	return err
}
