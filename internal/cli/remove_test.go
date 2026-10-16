//go:build darwin || linux

package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
	"paseo-dock/internal/config"
	"paseo-dock/internal/operations"
)

func init() {
	if os.Getenv("CLI_REMOVE_PASEO_FAKE") != "1" || filepath.Base(os.Args[0]) != "paseo" {
		return
	}
	for _, arg := range os.Args[1:] {
		if arg == "status" {
			fmt.Println(`{"localDaemon":"running"}`)
		}
	}
	os.Exit(0)
}

type cliRemovalBackend struct {
	cliCategoryBackend
	removed, inspected int
}

func (b *cliRemovalBackend) Inspect(context.Context, config.Entry) (operations.RuntimeState, error) {
	b.inspected++
	if b.removed > 0 {
		return operations.RuntimeState{}, operations.ErrRuntimeNotFound
	}
	return operations.RuntimeState{Known: true}, nil
}
func (b *cliRemovalBackend) Remove(context.Context, config.Entry) error { b.removed++; return nil }

func cliRemovalFixture(t *testing.T, prune bool) (*App, []string, string, *cliRemovalBackend) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	a, err := NewApp("../..")
	if err != nil {
		t.Fatal(err)
	}
	a.Root = t.TempDir()
	if err := os.MkdirAll(filepath.Join(a.Root, "bin"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.Root, "bin", "agent-wrapper"), []byte("test launcher"), 0700); err != nil {
		t.Fatal(err)
	}
	fake := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(exe, filepath.Join(fake, "paseo")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fake+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CLI_REMOVE_PASEO_FAKE", "1")
	t.Setenv("GORACE", os.Getenv("GORACE")+" atexit_sleep_ms=0")
	w, err := config.Canonical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := config.RuntimePath(w)
	if err != nil {
		t.Fatal(err)
	}
	e := config.Entry{Workspace: w, Sandbox: "test-sandbox", Agent: "codex", Tool: "native", Image: "test-image"}
	path := filepath.Join(runtime, "registry.json")
	if err := config.WriteJSON(path, config.Registry{Workspace: w, Profiles: map[string]config.Entry{"codex-native": e}, Active: []string{"codex-native"}}); err != nil {
		t.Fatal(err)
	}
	b := &cliRemovalBackend{}
	a.Backends = map[config.Category]operations.Backend{config.CategoryLegacySandbox: b}
	a.In, a.Out, a.Err = &bytes.Buffer{}, &bytes.Buffer{}, &bytes.Buffer{}
	args := []string{"remove", "--workspace", w, "--profile", "codex-native", "--paseo-home", t.TempDir()}
	if prune {
		if err := os.RemoveAll(w); err != nil {
			t.Fatal(err)
		}
		args = []string{"prune", "--remove-runtimes", "--paseo-home", t.TempDir()}
	}
	return a, args, path, b
}

func TestCLIRemovalNonTTYRequiresYes(t *testing.T) {
	for _, prune := range []bool{false, true} {
		t.Run(fmt.Sprint(prune), func(t *testing.T) {
			a, args, path, b := cliRemovalFixture(t, prune)
			before, _ := os.ReadFile(path)
			err := a.execute(args)
			after, _ := os.ReadFile(path)
			if err == nil || !strings.Contains(err.Error(), "--yes") || !strings.Contains(err.Error(), "TTY") || b.removed != 0 || string(before) != string(after) {
				t.Fatal(err, b.removed)
			}
		})
	}
}

func TestCLIRemovalRequiresExactTypedConfirmation(t *testing.T) {
	for _, prune := range []bool{false, true} {
		for _, typed := range []string{"wrong", " codex-native", "codex-native", "prune"} {
			t.Run(fmt.Sprintf("prune=%v/typed=%q", prune, typed), func(t *testing.T) {
				a, args, path, b := cliRemovalFixture(t, prune)
				master, input, err := pty.Open()
				if err != nil {
					t.Fatal(err)
				}
				defer master.Close()
				defer input.Close()
				a.In = input
				if _, err := master.WriteString(typed + "\n"); err != nil {
					t.Fatal(err)
				}
				before, _ := os.ReadFile(path)
				err = a.execute(args)
				want := "codex-native"
				if prune {
					want = "prune"
				}
				if typed == want {
					if err != nil || b.removed != 1 {
						t.Fatal(err, b.removed)
					}
				} else {
					after, _ := os.ReadFile(path)
					if err == nil || !strings.Contains(err.Error(), "confirmation") || b.removed != 0 || string(before) != string(after) {
						t.Fatal(err, b.removed)
					}
				}
			})
		}
	}
}

func TestCLIRemovalYesAndPruneDryRun(t *testing.T) {
	for _, command := range []string{"remove", "prune", "dry-run"} {
		t.Run(command, func(t *testing.T) {
			a, args, path, b := cliRemovalFixture(t, command != "remove")
			if command == "dry-run" {
				args = append(args, "--dry-run")
			} else {
				args = append(args, "--yes")
			}
			if err := a.execute(args); err != nil {
				t.Fatal(err)
			}
			if command == "dry-run" {
				if b.removed != 0 {
					t.Fatal("dry run removed runtime")
				}
				if _, err := os.Stat(path); err != nil {
					t.Fatal(err)
				}
			} else if b.removed != 1 {
				t.Fatal("runtime not removed", b.removed)
			}
		})
	}
}

type cliConfirmationWriter struct {
	io.Writer
	once   sync.Once
	prompt chan struct{}
}

func (w *cliConfirmationWriter) Write(p []byte) (int, error) {
	n, err := w.Writer.Write(p)
	if strings.Contains(string(p), "to confirm removal:") {
		w.once.Do(func() { close(w.prompt) })
	}
	return n, err
}

func TestCLIRemovalPinsTargetWhileAwaitingConfirmation(t *testing.T) {
	for _, prune := range []bool{false, true} {
		t.Run(fmt.Sprint(prune), func(t *testing.T) {
			a, args, path, b := cliRemovalFixture(t, prune)
			master, input, err := pty.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer master.Close()
			defer input.Close()
			a.In = input
			prompt := make(chan struct{})
			a.Out = &cliConfirmationWriter{Writer: &bytes.Buffer{}, prompt: prompt}
			done := make(chan error, 1)
			go func() { done <- a.execute(args) }()
			select {
			case <-prompt:
			case err := <-done:
				t.Fatal("confirmation not requested", err)
			case <-time.After(5 * time.Second):
				t.Fatal("confirmation prompt timed out")
			}
			var reg config.Registry
			if err := config.ReadJSON(path, &reg); err != nil {
				t.Fatal(err)
			}
			e := reg.Profiles["codex-native"]
			e.Image = "replacement"
			reg.Profiles["codex-native"] = e
			if err := config.WriteJSON(path, reg); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(path)
			confirmation := "codex-native"
			if prune {
				confirmation = "prune"
			}
			if _, err := master.WriteString(confirmation + "\n"); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("changed target accepted")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("confirmation did not finish")
			}
			after, _ := os.ReadFile(path)
			if b.removed != 0 || string(before) != string(after) {
				t.Fatal("changed confirmation mutated target", b.removed)
			}
		})
	}
}
