//go:build ignore

// Package the corresponding source using only the Go standard library.
package main

import (
	"archive/tar"
	"compress/gzip"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	output := flag.String("output", "dist/source.tar.gz", "source archive to create")
	flag.Parse()
	version := os.Getenv("VERSION")
	if version == "" {
		version = "1.0.0"
	}
	for _, c := range version {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || strings.ContainsRune(".-_", c)) {
			return fmt.Errorf("invalid VERSION")
		}
	}
	epoch := int64(0)
	if value := os.Getenv("SOURCE_DATE_EPOCH"); value != "" {
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil || n < 0 {
			return fmt.Errorf("invalid SOURCE_DATE_EPOCH")
		}
		epoch = n
	}
	// Explicit roots exclude working audio, profiles, build output, and Git data.
	roots := []string{
		"cmd", "internal", "scripts", "packaging", "examples", ".github",
		"go.mod", "default.nix", "README.md", "TAGS.md", "BENCHMARKS.md",
		"LICENSE", "NOTICE", "Go-BSD-3-Clause.txt", "CLA.md", "CONTRIBUTING.md", ".gitignore", ".gitattributes",
	}
	var files []string
	for _, root := range roots {
		if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("source archive refuses symlink: %s", path)
			}
			files = append(files, path)
			return nil
		}); err != nil {
			return err
		}
	}
	sort.Strings(files)
	destination, err := filepath.Abs(*output)
	if err != nil {
		return err
	}
	for _, path := range files {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		if absolute == destination {
			return fmt.Errorf("output would overwrite source: %s", path)
		}
		if dest, err := os.Stat(destination); err == nil {
			info, err := os.Stat(path)
			if err != nil {
				return err
			}
			if os.SameFile(dest, info) {
				return fmt.Errorf("output aliases source: %s", path)
			}
		}
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		return err
	}
	file, err := os.Create(destination)
	if err != nil {
		return err
	}
	defer file.Close()
	gz := gzip.NewWriter(file)
	tw := tar.NewWriter(gz)
	stamp := time.Unix(epoch, 0).UTC()
	for _, path := range files {
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("source is not a regular file: %s", path)
		}
		header := &tar.Header{
			Name: "audiotag-" + version + "/" + filepath.ToSlash(path),
			Mode: int64(info.Mode().Perm()), Size: info.Size(), ModTime: stamp,
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		_, err = io.Copy(tw, input)
		closeErr := input.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	return file.Close()
}
