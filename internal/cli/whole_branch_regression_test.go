package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"paseo-dock/internal/config"
	"paseo-dock/internal/operations"
	"paseo-dock/internal/sandboxupdate"
)

func TestCLIShellRespectsAutomaticUpdatePolicy(t *testing.T) {
	for _, category := range []config.Category{config.CategoryLegacySandbox, config.CategorySandbox, config.CategoryContainer} {
		for _, policy := range []string{"disabled", "recent-success", "recent-failure", "expired", "explicit"} {
			t.Run(string(category)+"/"+policy, func(t *testing.T) {
				w, runtime := signalFixture(t)
				t.Setenv("CLI_SIGNAL_ENTRY", "shell")
				t.Setenv("GORACE", os.Getenv("GORACE")+" atexit_sleep_ms=0")
				var reg config.Registry
				if err := config.ReadJSON(filepath.Join(runtime, "registry.json"), &reg); err != nil {
					t.Fatal(err)
				}
				e := reg.Profiles["codex-superpowers"]
				e.Category = category
				if category == config.CategorySandbox {
					e.Tool = ""
				}
				if category == config.CategoryContainer {
					e.ContainerID = strings.Repeat("a", 64)
					e.ImageID = "sha256:managed"
					e.Network = "bridge"
				}
				reg.Profiles = map[string]config.Entry{e.Key(): e}
				reg.Active = []string{e.Key()}
				if err := config.WriteJSON(filepath.Join(runtime, "registry.json"), reg); err != nil {
					t.Fatal(err)
				}
				// Docker shell is still an actual subprocess, replaced by this Go test helper.
				exe, _ := os.Executable()
				bin := filepath.Dir(os.Getenv("CLI_SIGNAL_READY"))
				if err := os.Symlink(exe, filepath.Join(bin, "docker")); err != nil {
					t.Fatal(err)
				}
				if policy == "disabled" || policy == "explicit" {
					t.Setenv("PASEO_DOCK_AUTO_UPDATE", "0")
				}
				if strings.HasPrefix(policy, "recent-") || policy == "expired" {
					state := sandboxupdate.State{Sandbox: sandboxupdate.Identity(e), Image: e.Image, Attempt: time.Now(), Success: time.Now()}
					if policy == "recent-failure" {
						state.Success = time.Time{}
						state.Errors = map[string]string{"agent": "prior failure"}
					}
					if policy == "expired" {
						state.Attempt = time.Now().Add(-25 * time.Hour)
					}
					sum := sha256.Sum256([]byte(state.Sandbox))
					if err := config.WriteJSON(filepath.Join(runtime, "updates", fmt.Sprintf("%x.json", sum[:8])), state); err != nil {
						t.Fatal(err)
					}
				}
				a, err := NewApp("../..")
				if err != nil {
					t.Fatal(err)
				}
				var out, stderr bytes.Buffer
				a.In, a.Out, a.Err = &bytes.Buffer{}, &out, &stderr
				b := &startupNoticeBackend{errout: &stderr}
				a.Backends = map[config.Category]operations.Backend{category: b}
				command := "shell"
				if policy == "explicit" {
					command = "update"
				}
				_ = a.execute([]string{command, e.Key(), "--workspace", w})
				wantUpdate := policy == "expired" || policy == "explicit"
				if (len(b.before) > 0) != wantUpdate {
					t.Fatalf("%s unexpectedly invoked/skipped guest updater: calls=%d stderr=%s", policy, len(b.before), stderr.String())
				}
				if category != config.CategoryContainer && command == "shell" {
					if _, err := os.Stat(os.Getenv("CLI_SIGNAL_READY") + "-launched"); err != nil {
						t.Fatal("shell was not opened", err, stderr.String())
					}
				}
			})
		}
	}
}

type registryFailureBackend struct{ cliCategoryBackend }

func (b *registryFailureBackend) Create(ctx context.Context, e config.Entry) (config.Entry, error) {
	e, err := b.cliCategoryBackend.Create(ctx, e)
	if err != nil {
		return e, err
	}
	runtime, err := config.RuntimePath(e.Workspace)
	if err != nil {
		return e, err
	}
	// A directory at the destination reproduces post-create atomic rename failure.
	if err = os.Mkdir(filepath.Join(runtime, "registry.json"), 0700); err != nil {
		return e, err
	}
	return e, nil
}

func TestCLIPartialCreateReportsDockerIdentity(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	a, err := NewApp("../..")
	if err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	a.Out, a.Err = &out, &stderr
	b := &registryFailureBackend{}
	a.Backends = map[config.Category]operations.Backend{config.CategoryContainer: b}
	err = a.execute([]string{"create", "--workspace", t.TempDir(), "--profiles", "codex-superpowers"})
	if err == nil || errors.Is(err, context.Canceled) || len(b.created) != 1 {
		t.Fatal("post-create registry failure not reproduced", err)
	}
	if !strings.Contains(stderr.String(), strings.Repeat("a", 64)) || !strings.Contains(stderr.String(), "container") || !strings.Contains(stderr.String(), "registry") {
		t.Fatalf("retained identity and recovery location missing: %s; err=%v", stderr.String(), err)
	}
	if out.Len() != 0 {
		t.Fatal("recovery diagnostics contaminated stdout", out.String())
	}
}

func TestActiveCLIShellSignalCancelsChild(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
		t.Run(sig.String(), func(t *testing.T) {
			signalFixture(t)
			t.Setenv("PASEO_DOCK_AUTO_UPDATE", "0")
			t.Setenv("CLI_SIGNAL_ENTRY", "shell")
			t.Setenv("CLI_SIGNAL_SHELL_BLOCK", "1")
			t.Setenv("GORACE", os.Getenv("GORACE")+" atexit_sleep_ms=0")
			cmd := exec.Command(os.Args[0], "-test.run=^TestSignalEntrypointHelper$")
			var out bytes.Buffer
			cmd.Stdout, cmd.Stderr = &out, &out
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = cmd.Process.Kill() })
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			pid := 0
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				b, _ := os.ReadFile(os.Getenv("CLI_SIGNAL_READY") + "-launched")
				pid, _ = strconv.Atoi(string(b))
				if pid > 0 {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if pid == 0 {
				t.Fatal("interactive shell never launched")
			}
			t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
			if err := cmd.Process.Signal(sig); err != nil {
				t.Fatal(err)
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				// Orphaned shell holds the output pipe after the parent exits.
				_ = syscall.Kill(pid, syscall.SIGKILL)
				<-done
				t.Fatalf("shell survived %s directed at CLI: %s", sig, out.String())
			}
			if syscall.Kill(pid, 0) == nil {
				t.Fatal("interactive shell child still exists after CLI cancellation")
			}
		})
	}
}
