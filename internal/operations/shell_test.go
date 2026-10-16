package operations

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"paseo-dock/internal/config"
	"paseo-dock/internal/operationlock"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// This process helper exercises the real Start/Wait boundary while guest runtimes
// remain external test dependencies. The existing TUI PTY suite tests terminal mode.
func categoryShellHelper() {
	_ = os.WriteFile(os.Getenv("SBX_TEST_LOG"), []byte(strings.Join(os.Args[1:], " ")), 0600)
	_ = os.WriteFile(os.Getenv("SBX_TEST_READY"), []byte("ready"), 0600)
	if os.Getenv("CATEGORY_SHELL_ECHO") == "1" {
		_, _ = io.Copy(os.Stdout, os.Stdin)
		fmt.Fprint(os.Stderr, "guest stderr")
	}
	if os.Getenv("CATEGORY_SHELL_BLOCK") == "1" {
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(os.Getenv("SBX_TEST_RELEASE")); err == nil {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		fmt.Fprintln(os.Stderr, "shell helper release timed out")
		os.Exit(1)
	}
}
func shellFixture(t *testing.T, category config.Category) (*Service, Target) {
	t.Helper()
	s, target, _ := categoryFixture(t, category)
	t.Setenv("CATEGORY_SHELL_HELPER", "1")
	if category == config.CategoryContainer {
		exe, _ := os.Executable()
		if err := os.Symlink(exe, filepath.Join(filepath.Dir(os.Getenv("SBX_TEST_LOG")), "docker")); err != nil {
			t.Fatal(err)
		}
	}
	return s, target
}
func TestShellHoldsTargetLockThroughProcessExit(t *testing.T) {
	for _, category := range []config.Category{config.CategorySandbox, config.CategoryContainer} {
		t.Run(string(category), func(t *testing.T) {
			s, target := shellFixture(t, category)
			t.Setenv("CATEGORY_SHELL_BLOCK", "1")
			shell, err := s.ShellCommand(context.Background(), target)
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- shell.Run() }()
			defer func() { _ = os.WriteFile(os.Getenv("SBX_TEST_RELEASE"), nil, 0600) }()
			waitReady(t)
			for _, kind := range []Kind{KindStop, KindUpdate} {
				result, err := s.Execute(context.Background(), Request{Kind: kind, Target: target}, nil)
				if !errors.Is(err, operationlock.ErrConflict) || result.Changed {
					t.Errorf("%s crossed live shell: result=%+v err=%v", kind, result, err)
				}
			}
			if err := os.WriteFile(os.Getenv("SBX_TEST_RELEASE"), nil, 0600); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("shell did not exit")
			}
			release, err := operationlock.Acquire(context.Background(), target.Runtime, []string{operationlock.EntryKey(target.Entry)})
			if err != nil {
				t.Fatal("shell leaked target lock", err)
			}
			release()
		})
	}
}
func TestShellRevalidatesRegistryAtLaunch(t *testing.T) {
	s, target := shellFixture(t, config.CategorySandbox)
	shell, err := s.ShellCommand(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	target.Entry.Image = "changed-after-preparation"
	writeTarget(t, target)
	if err := shell.Run(); err == nil {
		t.Fatal("shell launched changed target")
	}
	if log, _ := os.ReadFile(os.Getenv("SBX_TEST_LOG")); len(log) > 0 {
		t.Fatal("changed registry launched child", string(log))
	}
}
func TestShellRejectsHeldLockBeforeStartingChild(t *testing.T) {
	s, target := shellFixture(t, config.CategorySandbox)
	shell, err := s.ShellCommand(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	release, err := operationlock.Acquire(context.Background(), target.Runtime, []string{operationlock.EntryKey(target.Entry)})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if err := shell.Run(); !errors.Is(err, operationlock.ErrConflict) {
		t.Fatalf("shell crossed held lock: %v", err)
	}
	if log, _ := os.ReadFile(os.Getenv("SBX_TEST_LOG")); len(log) > 0 {
		t.Fatal("locked shell launched child", string(log))
	}
}
func TestShellPreservesTerminalStreams(t *testing.T) {
	s, target := shellFixture(t, config.CategorySandbox)
	t.Setenv("CATEGORY_SHELL_ECHO", "1")
	shell, err := s.ShellCommand(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	shell.SetStdin(strings.NewReader("interactive input"))
	shell.SetStdout(&out)
	shell.SetStderr(&stderr)
	if err := shell.Run(); err != nil {
		t.Fatal(err)
	}
	if out.String() != "interactive input" || stderr.String() != "guest stderr" {
		t.Fatalf("stdout=%q stderr=%q", out.String(), stderr.String())
	}
}
func TestShellCancellationReleasesTargetLock(t *testing.T) {
	s, target := shellFixture(t, config.CategorySandbox)
	t.Setenv("CATEGORY_SHELL_BLOCK", "1")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	shell, err := s.ShellCommand(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- shell.Run() }()
	waitReady(t)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("canceled shell did not exit")
	}
	release, err := operationlock.Acquire(context.Background(), target.Runtime, []string{operationlock.EntryKey(target.Entry)})
	if err != nil {
		t.Fatal("canceled shell leaked lock", err)
	}
	release()
}
func TestShellStartFailureReleasesTargetLock(t *testing.T) {
	s, target := shellFixture(t, config.CategorySandbox)
	s.Root = filepath.Join(t.TempDir(), "missing")
	shell, err := s.ShellCommand(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	if err := shell.Run(); err == nil {
		t.Fatal("shell with missing command directory ran")
	}
	release, err := operationlock.Acquire(context.Background(), target.Runtime, []string{operationlock.EntryKey(target.Entry)})
	if err != nil {
		t.Fatal("failed start leaked lock", err)
	}
	release()
}
