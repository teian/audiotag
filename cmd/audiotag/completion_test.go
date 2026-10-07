package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Exercise shell behavior, including Bash's '=' word splitting and paths with spaces.
func TestShellCompletions(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "audiotag")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	if b, e := exec.Command("go", "build", "-buildvcs=false", "-o", binary, ".").CombinedOutput(); e != nil {
		t.Fatal(e, string(b))
	}
	root, e := os.Getwd()
	if e != nil {
		t.Fatal(e)
	}
	env := append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if e = os.WriteFile(filepath.Join(dir, "transcript with spaces.txt"), []byte("words"), 0644); e != nil {
		t.Fatal(e)
	}
	for _, shell := range []string{"bash", "zsh", "fish"} {
		t.Run(shell, func(t *testing.T) {
			path, e := exec.LookPath(shell)
			if e != nil {
				t.Skip(shell + " not installed")
			}
			if shell == "bash" {
				// Nix's build shell may omit programmable-completion builtins.
				if e := exec.Command(path, "-c", "type complete compgen >/dev/null && test -n \"$COMP_WORDBREAKS\"").Run(); e != nil {
					t.Skip("Bash lacks programmable completion")
				}
			}
			script := filepath.Join(root, "completions", "audiotag."+shell)
			cmd := exec.Command(path, "-n", script)
			if b, e := cmd.CombinedOutput(); e != nil {
				t.Fatal(e, string(b))
			}
			var body string
			switch shell {
			case "bash":
				body = `source "$1"
check() { _audiotag; printf '%s\n' "${COMPREPLY[@]}"; }
COMP_WORDS=(audiotag set --set nar); COMP_CWORD=3; check
COMP_WORDS=(audiotag remove --remove ly); COMP_CWORD=3; check
COMP_WORDS=(audiotag set --set = nar); COMP_CWORD=4; check
COMP_WORDS=(audiotag set --set-file = lyrics = trans); COMP_CWORD=6; check
COMP_WORDS=(audiotag set --progress = al); COMP_CWORD=4; check
COMP_WORDS=(audiotag set --set arbitrary=nar); COMP_CWORD=3; _audiotag; test ${#COMPREPLY[@]} -eq 0
`
				cmd = exec.Command(path, "-c", body, "completion-test", script)
			case "fish":
				body = `source $argv[1]
complete -C 'audiotag set --set nar'
complete -C 'audiotag remove --remove ly'
complete -C 'audiotag set --set=nar'
complete -C 'audiotag set --set-file lyrics=trans'
complete -C 'audiotag set --progress al'
`
				cmd = exec.Command(path, "-c", body, script)
			case "zsh":
				body = `compdef() { :; }
compadd() { print -rl -- "$@"; }
source "$1"
words=(audiotag set --set nar); CURRENT=4
_audiotag_tag_assignment text
_audiotag_tag_remove
`
				cmd = exec.Command(path, "-c", body, "completion-test", script)
			}
			cmd.Dir = dir
			cmd.Env = env
			b, e := cmd.CombinedOutput()
			if e != nil {
				t.Fatal(e, string(b))
			}
			output := string(b)
			if !strings.Contains(output, "narrator") || !strings.Contains(output, "lyrics") {
				t.Fatal("tag suggestions missing", output)
			}
			if shell == "bash" || shell == "fish" {
				if !strings.Contains(output, "transcript") || !strings.Contains(output, "always") {
					t.Fatal("path or mode suggestions missing", output)
				}
			}
			if shell == "bash" && strings.Contains(output, "lyrics=transcript") {
				t.Fatal("Bash duplicated the KEY= prefix", output)
			}
		})
	}
}
