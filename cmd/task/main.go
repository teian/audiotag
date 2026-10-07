// Command task provides development and packaging shortcuts without Make.
package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const usage = `Usage: go run ./cmd/task COMMAND

  build          Build the standalone CLI in dist (CGO disabled)
  test           Run Go tests
  check          Run vet and race tests (requires a C compiler for race)
  release        Cross-build release archives; VERSION/TARGETS supported
  install        Install CLI, completions, docs and source; PREFIX/DESTDIR supported
  package FORMAT Build deb, rpm, apk or nix; VERSION/ARCH supported
  clean          Remove generated dist files

RPM/APK/Nix require their platform tools and a POSIX shell.
Build, test, release and DEB packaging work with Go alone.
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		fmt.Print(usage)
		return nil
	}
	if len(args) != 1 && !(len(args) == 2 && args[0] == "package") {
		return fmt.Errorf("invalid arguments\n%s", usage)
	}
	if args[0] == "package" && len(args) != 2 {
		return fmt.Errorf("package requires deb, rpm, apk or nix")
	}
	if _, err := os.Stat("cmd/audiotag/main.go"); err != nil {
		return fmt.Errorf("run from the audiotag project root")
	}
	switch args[0] {
	case "build":
		return build(binaryPath("dist"), nil)
	case "test":
		return command(nil, "go", "test", "-buildvcs=false", "./...")
	case "check":
		if err := command(nil, "go", "vet", "-buildvcs=false", "./..."); err != nil {
			return err
		}
		return command(nil, "go", "test", "-buildvcs=false", "-race", "./...")
	case "release":
		return command(nil, "go", "run", "scripts/release.go")
	case "install":
		return install(os.Getenv("DESTDIR"), value("PREFIX", "/usr/local"), nil)
	case "clean":
		return os.RemoveAll("dist")
	case "package":
		switch args[1] {
		case "deb":
			return deb()
		case "rpm", "apk", "nix":
			return command(nil, "sh", "scripts/package-"+args[1]+".sh")
		default:
			return fmt.Errorf("unknown package format %q", args[1])
		}
	default:
		return fmt.Errorf("unknown command %q\n%s", args[0], usage)
	}
}
func value(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
func command(env map[string]string, name string, args ...string) error {
	fmt.Printf("+ %s %s\n", name, strings.Join(args, " "))
	cmd := exec.Command(name, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = os.Environ()
	for key, v := range env {
		filtered := cmd.Env[:0]
		for _, e := range cmd.Env {
			if !strings.HasPrefix(e, key+"=") {
				filtered = append(filtered, e)
			}
		}
		cmd.Env = append(filtered, key+"="+v)
	}
	return cmd.Run()
}
func binaryPath(dir string) string {
	name := "audiotag"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(dir, name)
}
func build(output string, env map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(output), 0755); err != nil {
		return err
	}
	if env == nil {
		env = map[string]string{}
	}
	env["CGO_ENABLED"] = "0"
	return command(env, "go", "build", "-buildvcs=false", "-trimpath", "-ldflags", "-s -w -X main.version="+value("VERSION", "1.0.0"), "-o", output, "./cmd/audiotag")
}
func copyFile(src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	closeErr := out.Close()
	if err != nil {
		return err
	}
	return closeErr
}
func install(dest, prefix string, env map[string]string) error {
	root := prefix
	if dest != "" {
		root = filepath.Join(dest, strings.TrimLeft(prefix, "/\\"))
	}
	binary := "audiotag"
	targetOS := runtime.GOOS
	if env != nil && env["GOOS"] != "" {
		targetOS = env["GOOS"]
	}
	if targetOS == "windows" {
		binary += ".exe"
	}
	if err := build(filepath.Join(root, "bin", binary), env); err != nil {
		return err
	}
	files := map[string]string{
		"cmd/audiotag/completions/audiotag.bash": "share/bash-completion/completions/audiotag",
		"cmd/audiotag/completions/audiotag.zsh":  "share/zsh/site-functions/_audiotag",
		"cmd/audiotag/completions/audiotag.fish": "share/fish/vendor_completions.d/audiotag.fish",
	}
	for _, name := range []string{"LICENSE", "NOTICE", "Go-BSD-3-Clause.txt"} {
		files[name] = "share/licenses/audiotag/" + name
	}
	for _, name := range []string{"README.md", "TAGS.md", "BENCHMARKS.md", "CONTRIBUTING.md", "CLA.md"} {
		files[name] = "share/doc/audiotag/" + name
	}
	if err := filepath.WalkDir("examples", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("nonregular example %s", path)
		}
		files[path] = filepath.Join("share/doc/audiotag", path)
		return nil
	}); err != nil {
		return err
	}
	for src, dst := range files {
		if err := copyFile(src, filepath.Join(root, dst), 0644); err != nil {
			return err
		}
	}
	// Helper programs run on the host even when the CLI is cross-compiled.
	return command(map[string]string{"GOOS": runtime.GOOS, "GOARCH": runtime.GOARCH}, "go", "run", "scripts/source.go", "--output", filepath.Join(root, "share/doc/audiotag/source.tar.gz"))
}
func deb() error {
	arch := value("ARCH", runtime.GOARCH)
	debian := map[string]string{"amd64": "amd64", "arm64": "arm64", "arm": "armhf", "386": "i386", "riscv64": "riscv64", "ppc64le": "ppc64el"}[arch]
	if debian == "" {
		return fmt.Errorf("unsupported Debian architecture %q", arch)
	}
	work, err := os.MkdirTemp("", "audiotag-deb-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	if err = install(work, "/usr", map[string]string{"GOOS": "linux", "GOARCH": arch, "GOARM": "7"}); err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Join(work, "DEBIAN"), 0755); err != nil {
		return err
	}
	version := value("VERSION", "1.0.0")
	control := fmt.Sprintf("Package: audiotag\nVersion: %s\nSection: sound\nPriority: optional\nArchitecture: %s\nMaintainer: AudioTag maintainers <audiotag@example.invalid>\nDescription: Native lossless audio and audiobook metadata CLI\n Standalone Go tag editor with shell completions and no runtime dependencies.\n", version, debian)
	if err = os.WriteFile(filepath.Join(work, "DEBIAN/control"), []byte(control), 0644); err != nil {
		return err
	}
	if err = os.MkdirAll("dist", 0755); err != nil {
		return err
	}
	return command(map[string]string{"GOOS": runtime.GOOS, "GOARCH": runtime.GOARCH}, "go", "run", "scripts/deb.go", "--root", work, "--output", filepath.Join("dist", "audiotag_"+version+"_"+debian+".deb"))
}
