package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The repo-root paseo-sbx shim warns once and forwards arguments and the exit
// status to paseo-dock, which runs bin/paseo-dock.
func TestLegacyWrapperForwardsToPaseoDock(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"paseo-sbx", "paseo-dock"} {
		data, err := os.ReadFile(filepath.Join("..", "..", name))
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(root, name), data, 0755); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(filepath.Join(root, "paseo-sbx"), "list", "--workspace", "a b")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err == nil || !strings.Contains(stderr.String(), "paseo-dock: run make build") {
		t.Fatalf("missing binary: %v %s", err, stderr.String())
	}

	if err := os.Mkdir(filepath.Join(root, "bin"), 0755); err != nil {
		t.Fatal(err)
	}
	fake := "#!/bin/sh\nprintf '%s|' \"$@\"\nexit 7\n"
	if err := os.WriteFile(filepath.Join(root, "bin", "paseo-dock"), []byte(fake), 0755); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command(filepath.Join(root, "paseo-sbx"), "list", "--workspace", "a b")
	stderr.Reset()
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 7 {
		t.Fatalf("exit status not forwarded: %v", err)
	}
	if string(out) != "list|--workspace|a b|" {
		t.Fatalf("arguments not forwarded: %q", out)
	}
	if warning := stderr.String(); strings.Count(warning, "\n") != 1 || !strings.Contains(warning, "renamed to paseo-dock") {
		t.Fatalf("deprecation warning %q", warning)
	}
}
