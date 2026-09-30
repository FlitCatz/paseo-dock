package main

import (
	"fmt"
	"os"
	"paseo-sbx/internal/cli"
	"paseo-sbx/internal/config"
	"path/filepath"
)

func main() {
	root := os.Getenv("PASEO_SBX_ROOT")
	if root == "" {
		if exe, err := os.Executable(); err == nil {
			root = config.FindRoot(filepath.Dir(exe))
		}
	}
	if root == "" {
		cwd, _ := os.Getwd()
		root = config.FindRoot(cwd)
	}
	if root == "" {
		fmt.Fprintln(os.Stderr, "paseo-sbx: source root not found; set PASEO_SBX_ROOT")
		os.Exit(1)
	}
	os.Exit(cli.Run(root, os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
