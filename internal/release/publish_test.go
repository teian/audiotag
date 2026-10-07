package release

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestPublishFailureRetryAndPublicImmutability(t *testing.T) {
	input, output, root := fixture(t)
	if err := Stage(input, output, root, "1.2.3", strings.Repeat("a", 40)); err != nil {
		t.Fatal(err)
	}
	assets, _ := Inventory(output)
	var exists, draft, failUpload, corrupt bool
	failUpload = true
	edits, uploads := 0, 0
	remote := func() []byte {
		var a []map[string]any
		for _, asset := range assets {
			digest := "sha256:" + asset.SHA256
			if corrupt {
				digest = "bad"
			}
			a = append(a, map[string]any{"name": asset.Name, "size": asset.Size, "state": "uploaded", "digest": digest})
		}
		b, _ := json.Marshal(map[string]any{"tag_name": "v1.2.3", "draft": draft, "assets": a})
		return b
	}
	run := func(args ...string) ([]byte, error) {
		switch args[0] {
		case "api":
			if !exists {
				return []byte("gh: Not Found (HTTP 404)"), fmt.Errorf("not found")
			}
			return remote(), nil
		case "release":
			switch args[1] {
			case "create":
				exists = true
				draft = true
			case "upload":
				uploads++
				if failUpload {
					return nil, fmt.Errorf("network failure")
				}
			case "edit":
				edits++
				draft = false
			default:
				t.Fatal(args)
			}
		}
		return nil, nil
	}
	publish := func() error { return Publish(output, "1.2.3", strings.Repeat("a", 40), "owner/repo", run) }
	if err := publish(); err == nil || !draft || edits != 0 {
		t.Fatalf("failed upload published: %v", err)
	}
	failUpload = false
	corrupt = true
	if err := publish(); err == nil || !draft || edits != 0 {
		t.Fatal("corrupt remote upload published")
	}
	corrupt = false
	if err := publish(); err != nil || draft || edits != 1 {
		t.Fatalf("retry failed: %v", err)
	}
	before := uploads
	if err := publish(); err != nil || uploads != before || edits != 1 {
		t.Fatal("public release changed")
	}
	corrupt = true
	if err := publish(); err == nil || uploads != before {
		t.Fatal("mismatched public release overwritten")
	}
}
func TestPublishDoesNotTreatAuthorizationFailureAsMissing(t *testing.T) {
	input, output, root := fixture(t)
	Stage(input, output, root, "1.2.3", strings.Repeat("a", 40))
	calls := 0
	err := Publish(output, "1.2.3", strings.Repeat("a", 40), "owner/repo", func(...string) ([]byte, error) { calls++; return []byte("HTTP 403"), fmt.Errorf("forbidden") })
	if err == nil || calls != 1 {
		t.Fatal("attempted mutation after authorization failure")
	}
}
