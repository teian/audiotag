package main

import (
	"audiotag/internal/cli"
	"embed"
	"fmt"
	"os"
)

//go:embed completions/*
var completions embed.FS
var version = "1.0.0"

func main() {
	args := os.Args[1:]
	if len(args) > 0 && (args[0] == "version" || args[0] == "--version") {
		fmt.Println("audiotag", version)
		return
	}
	if len(args) > 0 && args[0] == "completion" {
		if len(args) != 2 {
			fmt.Fprintln(os.Stderr, "usage: audiotag completion bash|zsh|fish")
			os.Exit(2)
		}
		switch args[1] {
		case "bash", "zsh", "fish":
		default:
			fmt.Fprintln(os.Stderr, "unsupported shell")
			os.Exit(2)
		}
		b, e := completions.ReadFile("completions/audiotag." + args[1])
		if e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(1)
		}
		fmt.Print(string(b))
		return
	}
	if e := cli.Run(args, os.Stdout, os.Stderr); e != nil {
		fmt.Fprintln(os.Stderr, "audiotag:", e)
		os.Exit(1)
	}
}
