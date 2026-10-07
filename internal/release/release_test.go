package release

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T) (string, string, string) {
	t.Helper()
	base := t.TempDir()
	input := filepath.Join(base, "input")
	root := filepath.Join(base, "root")
	os.Mkdir(input, 0755)
	os.Mkdir(root, 0755)
	names := []string{"audiotag-1.2.3-source.tar.gz", "audiotag.nixpkg", "audiotag-1.2.3-r0.apk", "builder-123.rsa.pub"}
	for _, target := range Targets {
		ext := ".tar.gz"
		if strings.HasPrefix(target, "windows/") {
			ext = ".zip"
		}
		names = append(names, "audiotag-1.2.3-"+strings.ReplaceAll(target, "/", "-")+ext)
	}
	for _, arch := range strings.Fields("amd64 arm64 armhf i386 riscv64 ppc64el") {
		names = append(names, "audiotag_1.2.3_"+arch+".deb")
	}
	for _, arch := range strings.Fields("x86_64 aarch64 armv7hl i686 riscv64 ppc64le") {
		names = append(names, "audiotag-1.2.3-1."+arch+".rpm")
	}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(input, name), []byte("test payload"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"LICENSE", "NOTICE", "Go-BSD-3-Clause.txt"} {
		os.WriteFile(filepath.Join(root, name), []byte(name), 0644)
	}
	return input, filepath.Join(base, "output"), root
}
func TestCompleteInventoryAndPublicationGate(t *testing.T) {
	input, output, root := fixture(t)
	if err := Stage(input, output, root, "1.2.3", strings.Repeat("a", 40)); err != nil {
		t.Fatal(err)
	}
	assets, err := Inventory(output)
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) != 39 {
		t.Fatalf("got %d assets", len(assets))
	}
	remote := map[string]any{"tag_name": "v1.2.3", "draft": true}
	var uploaded []map[string]any
	for _, a := range assets {
		uploaded = append(uploaded, map[string]any{"name": a.Name, "size": a.Size, "state": "uploaded", "digest": "sha256:" + a.SHA256})
	}
	remote["assets"] = uploaded
	data, _ := json.Marshal(remote)
	if draft, err := Verify(data, assets, "v1.2.3"); err != nil || !draft {
		t.Fatalf("%v %v", draft, err)
	}
	for _, change := range []string{"digest", "state", "size", "duplicate", "missing"} {
		t.Run(change, func(t *testing.T) {
			data, _ := json.Marshal(remote)
			var broken map[string]any
			json.Unmarshal(data, &broken)
			a := broken["assets"].([]any)
			first := a[0].(map[string]any)
			switch change {
			case "digest":
				first["digest"] = "sha256:bad"
			case "state":
				first["state"] = "starter"
			case "size":
				first["size"] = 0
			case "duplicate":
				a[0] = a[1]
			case "missing":
				broken["assets"] = a[1:]
			}
			data, _ = json.Marshal(broken)
			if _, err := Verify(data, assets, "v1.2.3"); err == nil {
				t.Fatal("unverified upload accepted")
			}
		})
	}
}
func TestStageRejectsIncompleteOrAmbiguousInputs(t *testing.T) {
	for _, fault := range []string{"missing", "duplicate", "version", "symlink", "unexpected", "stale"} {
		t.Run(fault, func(t *testing.T) {
			input, output, root := fixture(t)
			switch fault {
			case "missing":
				os.Remove(filepath.Join(input, "audiotag-1.2.3-windows-arm64.zip"))
			case "duplicate":
				os.Mkdir(filepath.Join(input, "nested"), 0755)
				os.WriteFile(filepath.Join(input, "nested", "audiotag.nixpkg"), []byte("duplicate"), 0644)
			case "version":
				os.Rename(filepath.Join(input, "audiotag-1.2.3-source.tar.gz"), filepath.Join(input, "audiotag-1.2.4-source.tar.gz"))
			case "symlink":
				if err := os.Symlink(filepath.Join(input, "audiotag.nixpkg"), filepath.Join(input, "linked")); err != nil {
					t.Skip(err)
				}
			case "unexpected":
				os.WriteFile(filepath.Join(input, "private.key"), []byte("secret"), 0600)
			case "stale":
				os.Mkdir(output, 0755)
			}
			if err := Stage(input, output, root, "1.2.3", strings.Repeat("a", 40)); err == nil {
				t.Fatal("invalid inputs accepted")
			}
		})
	}
}
