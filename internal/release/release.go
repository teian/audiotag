// Package release validates and stages the complete public release inventory.
package release

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var Version = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
var Targets = strings.Fields("linux/amd64 linux/arm64 linux/arm linux/386 linux/riscv64 linux/ppc64le freebsd/amd64 freebsd/arm64 openbsd/amd64 openbsd/arm64 netbsd/amd64 netbsd/arm64 dragonfly/amd64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64 windows/386")

type Asset struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

func Inventory(dir string) ([]Asset, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var result []Asset
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Size() == 0 {
			return nil, fmt.Errorf("invalid asset %s", entry.Name())
		}
		f, err := os.Open(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		h := sha256.New()
		_, err = io.Copy(h, f)
		f.Close()
		if err != nil {
			return nil, err
		}
		result = append(result, Asset{entry.Name(), info.Size(), hex.EncodeToString(h.Sum(nil))})
	}
	return result, nil
}
func Stage(input, output, root, version, commit string) error {
	if !Version.MatchString(version) || !regexp.MustCompile(`^[a-f0-9]{40}$`).MatchString(commit) {
		return fmt.Errorf("stable version and full commit SHA required")
	}
	if err := os.Mkdir(output, 0755); err != nil {
		return err
	} // Refuse reuse of stale staging directories.
	expected := map[string]string{}
	prefix := "audiotag-" + version
	for _, target := range Targets {
		ext := ".tar.gz"
		if strings.HasPrefix(target, "windows/") {
			ext = ".zip"
		}
		name := prefix + "-" + strings.ReplaceAll(target, "/", "-") + ext
		expected[name] = name
	}
	expected[prefix+"-source.tar.gz"] = prefix + "-source.tar.gz"
	for _, arch := range strings.Fields("amd64 arm64 armhf i386 riscv64 ppc64el") {
		name := "audiotag_" + version + "_" + arch + ".deb"
		expected[name] = name
	}
	expected["audiotag.nixpkg"] = prefix + "-linux-amd64.nixpkg"
	expected[prefix+"-r0.apk"] = prefix + "-alpine-x86_64.apk"
	rpm := regexp.MustCompile(`^audiotag-` + regexp.QuoteMeta(version) + `-[A-Za-z0-9._+]+\.(x86_64|aarch64|armv7hl|i686|riscv64|ppc64le)\.rpm$`)
	rpmSeen := map[string]bool{}
	keyCount := 0
	seen := map[string]bool{}
	copyFile := func(src, name string) error {
		if seen[name] {
			return fmt.Errorf("duplicate asset %s", name)
		}
		seen[name] = true
		b, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		if len(b) == 0 {
			return fmt.Errorf("empty asset %s", name)
		}
		return os.WriteFile(filepath.Join(output, name), b, 0644)
	}
	err := filepath.WalkDir(input, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("nonregular input %s", path)
		}
		name := d.Name()
		if name == "SHA256SUMS" {
			return nil
		}
		if dest, ok := expected[name]; ok {
			return copyFile(path, dest)
		}
		if match := rpm.FindStringSubmatch(name); match != nil {
			if rpmSeen[match[1]] {
				return fmt.Errorf("duplicate RPM architecture")
			}
			rpmSeen[match[1]] = true
			return copyFile(path, name)
		}
		if regexp.MustCompile(`^[A-Za-z0-9_.@+-]+\.rsa\.pub$`).MatchString(name) {
			keyCount++
			return copyFile(path, name)
		}
		return fmt.Errorf("unexpected input %s", name)
	})
	if err != nil {
		return err
	}
	for _, name := range expected {
		if !seen[name] {
			return fmt.Errorf("missing asset %s", name)
		}
	}
	if len(rpmSeen) != 6 || keyCount != 1 {
		return fmt.Errorf("need six RPM architectures and one APK public key")
	}
	for _, name := range []string{"LICENSE", "NOTICE", "Go-BSD-3-Clause.txt"} {
		if err := copyFile(filepath.Join(root, name), name); err != nil {
			return err
		}
	}
	assets, err := Inventory(output)
	if err != nil {
		return err
	}
	manifest := struct {
		Version string  `json:"version"`
		Commit  string  `json:"commit"`
		Assets  []Asset `json:"assets"`
	}{version, commit, assets}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(output, "manifest.json"), append(data, '\n'), 0644); err != nil {
		return err
	}
	assets, err = Inventory(output)
	if err != nil {
		return err
	}
	var sums []string
	for _, a := range assets {
		sums = append(sums, a.SHA256+"  "+a.Name)
	}
	sort.Strings(sums)
	return os.WriteFile(filepath.Join(output, "SHA256SUMS"), []byte(strings.Join(sums, "\n")+"\n"), 0644)
}

// Verify requires exact inventory and server-side digests before draft publication.
func Verify(data []byte, assets []Asset, tag string) (bool, error) {
	var remote struct {
		Tag    string `json:"tag_name"`
		Draft  bool   `json:"draft"`
		Assets []struct {
			Name   string `json:"name"`
			Size   int64  `json:"size"`
			State  string `json:"state"`
			Digest string `json:"digest"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(data, &remote); err != nil {
		return false, err
	}
	if remote.Tag != tag || len(remote.Assets) != len(assets) {
		return remote.Draft, fmt.Errorf("release inventory mismatch")
	}
	expected := map[string]Asset{}
	for _, a := range assets {
		expected[a.Name] = a
	}
	for _, a := range remote.Assets {
		e, ok := expected[a.Name]
		if !ok || a.Size != e.Size || a.State != "uploaded" || a.Digest != "sha256:"+e.SHA256 {
			return remote.Draft, fmt.Errorf("asset verification failed: %s", a.Name)
		}
		delete(expected, a.Name)
	}
	return remote.Draft, nil
}
