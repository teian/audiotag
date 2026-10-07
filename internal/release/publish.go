package release

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

// Publish leaves failed uploads in draft state and never changes public assets.
// Run invokes gh with arguments; the workflow supplies its repository and token.
func Publish(output, version, commit, repo string, run func(...string) ([]byte, error)) error {
	if repo == "" {
		return fmt.Errorf("GH_REPO required")
	}
	assets, err := Inventory(output)
	if err != nil {
		return err
	}
	tag := "v" + version
	endpoint := "repos/" + repo + "/releases/tags/" + tag
	data, err := run("api", endpoint)
	if err != nil {
		if !strings.Contains(string(data), "(HTTP 404)") {
			return err
		}
		if _, err = run("release", "create", tag, "--draft", "--verify-tag", "--target", commit, "--title", "AudioTag "+tag, "--generate-notes"); err != nil {
			return err
		}
	} else {
		var existing struct {
			Draft bool `json:"draft"`
		}
		if err = json.Unmarshal(data, &existing); err != nil {
			return err
		}
		if !existing.Draft {
			_, err = Verify(data, assets, tag)
			return err
		}
	}
	args := []string{"release", "upload", tag, "--clobber"}
	for _, a := range assets {
		args = append(args, filepath.Join(output, a.Name))
	}
	if _, err = run(args...); err != nil {
		return err
	}
	data, err = run("api", endpoint)
	if err != nil {
		return err
	}
	draft, err := Verify(data, assets, tag)
	if err != nil {
		return err
	}
	if !draft {
		return fmt.Errorf("release ceased to be a draft during upload")
	}
	if _, err = run("release", "edit", tag, "--draft=false", "--verify-tag", "--latest"); err != nil {
		return err
	}
	data, err = run("api", endpoint)
	if err != nil {
		return err
	}
	draft, err = Verify(data, assets, tag)
	if err != nil {
		return err
	}
	if draft {
		return fmt.Errorf("release is still a draft")
	}
	return nil
}
