package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegistriesUnreadableFileFailsClosed(t *testing.T) {
	base := t.TempDir()
	// A directory at the file path reliably produces a read failure, even as root.
	path := filepath.Join(base, "unreadable", "registry.json")
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := WriteJSON(filepath.Join(base, "earlier", "registry.json"), Registry{Workspace: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	registries, err := Registries(base)
	if err == nil || !strings.Contains(err.Error(), path) || registries != nil {
		t.Fatalf("unreadable registry returned partial/empty success: %v, %v", registries, err)
	}
}

func TestRegistriesUnreadableDirectoryFailsClosed(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "blocked")
	path := filepath.Join(dir, "registry.json")
	if err := WriteJSON(path, Registry{Workspace: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0700) })
	if _, err := os.ReadFile(path); err == nil {
		t.Skip("host can bypass directory permissions")
	}
	registries, err := Registries(base)
	if err == nil || !strings.Contains(err.Error(), path) || registries != nil {
		t.Fatalf("unreadable directory silently skipped: %v, %v", registries, err)
	}
}

func TestRegistriesAllowAbsentStateAndNonRegistryDirectories(t *testing.T) {
	base := filepath.Join(t.TempDir(), "absent")
	if regs, err := Registries(base); err != nil || len(regs) != 0 {
		t.Fatalf("new installation: %v, %v", regs, err)
	}
	if err := os.MkdirAll(filepath.Join(base, "launcher"), 0700); err != nil {
		t.Fatal(err)
	}
	if regs, err := Registries(base); err != nil || len(regs) != 0 {
		t.Fatalf("launcher directory: %v, %v", regs, err)
	}
}
