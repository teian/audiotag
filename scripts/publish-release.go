//go:build ignore

package main

import (
	"audiotag/internal/release"
	"flag"
	"fmt"
	"os"
	"os/exec"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func gh(args ...string) ([]byte, error) {
	b, err := exec.Command("gh", args...).CombinedOutput()
	if err != nil {
		return b, fmt.Errorf("gh %s: %w: %s", args[0], err, b)
	}
	return b, nil
}
func run() error {
	input := flag.String("input", "dist/release-input", "downloaded artifacts")
	output := flag.String("output", "dist/release-assets", "new staging directory")
	version := flag.String("version", "", "stable release version")
	commit := flag.String("commit", "", "full commit SHA")
	publish := flag.Bool("publish", false, "publish verified assets using GH_TOKEN and GH_REPO")
	flag.Parse()
	if err := release.Stage(*input, *output, ".", *version, *commit); err != nil {
		return err
	}
	assets, err := release.Inventory(*output)
	if err != nil {
		return err
	}
	fmt.Printf("Validated %d release assets\n", len(assets))
	if !*publish {
		return nil
	}
	return release.Publish(*output, *version, *commit, os.Getenv("GH_REPO"), gh)
}
