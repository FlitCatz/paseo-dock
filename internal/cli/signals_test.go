package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"paseo-sbx/internal/config"
	"paseo-sbx/internal/launcher"
	"paseo-sbx/internal/operationlock"
)

// The external host command and its descendant are Go subprocesses. Only the
// descendant writes readiness, so signals cannot race ahead of group creation.
func TestMain(m *testing.M) {
	if os.Getenv("CLI_SIGNAL_FAKE") == "1" && filepath.Base(os.Args[0]) == "sbx" {
		ready := os.Getenv("CLI_SIGNAL_READY")
		if os.Getenv("CLI_SIGNAL_ENTRY") == "" {
			_ = os.WriteFile(ready, []byte("unexpected external call"), 0600)
			os.Exit(0)
		}
		if os.Getenv("CLI_SIGNAL_CHILD") == "1" {
			_ = os.WriteFile(ready, []byte(fmt.Sprintf("%d %d", os.Getppid(), os.Getpid())), 0600)
			time.Sleep(time.Minute)
			os.Exit(0)
		}
		args := strings.Join(os.Args[1:], " ")
		if strings.Contains(args, "-it") || strings.Contains(args, "-i ") {
			_ = os.WriteFile(ready+"-launched", []byte(args), 0600)
			os.Exit(0)
		}
		child := exec.Command(os.Args[0])
		child.Env = append(os.Environ(), "CLI_SIGNAL_CHILD=1")
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if err := child.Run(); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestSignalEntrypointHelper(t *testing.T) {
	mode := os.Getenv("CLI_SIGNAL_ENTRY")
	if mode == "" {
		return
	}
	if mode == "launcher" {
		code, err := launcher.LaunchIO(os.Getenv("CLI_SIGNAL_RUNTIME"), "codex-superpowers", []string{"app-server"}, os.Stdin, os.Stdout, os.Stderr)
		fmt.Fprintln(os.Stderr, err)
		os.Exit(code)
	}
	args := []string{mode, "codex-superpowers", "--workspace", os.Getenv("PASEO_AGENT_CWD")}
	os.Exit(Run("../..", args, os.Stdin, os.Stdout, os.Stderr))
}

func signalFixture(t *testing.T) (string, string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PASEO_SBX_AUTO_UPDATE", "1")
	workspace, err := config.Canonical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := config.RuntimePath(workspace)
	if err != nil {
		t.Fatal(err)
	}
	reg := config.Registry{Workspace: workspace, Active: []string{"codex-superpowers"}, Profiles: map[string]config.Entry{"codex-superpowers": {Agent: "codex", Tool: "superpowers", Workspace: workspace, Sandbox: "signal-box", Image: "test-image"}}}
	if err := config.WriteJSON(filepath.Join(runtime, "registry.json"), reg); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	exe, _ := os.Executable()
	if err := os.Symlink(exe, filepath.Join(bin, "sbx")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	t.Setenv("CLI_SIGNAL_FAKE", "1")
	t.Setenv("CLI_SIGNAL_READY", filepath.Join(bin, "ready"))
	t.Setenv("CLI_SIGNAL_RUNTIME", runtime)
	t.Setenv("PASEO_AGENT_CWD", workspace)
	return workspace, runtime
}

func TestStopRequiresExplicitProfile(t *testing.T) {
	workspace, _ := signalFixture(t)
	var out bytes.Buffer
	code := Run("../..", []string{"stop", "--workspace", workspace}, &bytes.Buffer{}, &out, &out)
	if code == 0 {
		t.Fatalf("omitted stop profile succeeded: %s", out.String())
	}
	if _, err := os.Stat(os.Getenv("CLI_SIGNAL_READY")); !os.IsNotExist(err) {
		t.Fatal("omitted stop profile invoked sbx")
	}
}

func TestEntrypointSignalsCancelHostGroup(t *testing.T) {
	for _, mode := range []string{"stop", "update", "shell", "launcher"} {
		signals := []syscall.Signal{syscall.SIGINT, syscall.SIGTERM}
		if mode == "launcher" {
			signals = append(signals, syscall.SIGHUP)
		}
		for _, sig := range signals {
			t.Run(mode+"/"+sig.String(), func(t *testing.T) {
				_, runtime := signalFixture(t)
				t.Setenv("CLI_SIGNAL_ENTRY", mode)
				cmd := exec.Command(os.Args[0], "-test.run=^TestSignalEntrypointHelper$")
				var output bytes.Buffer
				cmd.Stdout = &output
				cmd.Stderr = &output
				if err := cmd.Start(); err != nil {
					t.Fatal(err)
				}
				defer cmd.Process.Kill()
				done := make(chan error, 1)
				go func() { done <- cmd.Wait() }()
				var parent, child int
				deadline := time.Now().Add(8 * time.Second)
				for time.Now().Before(deadline) {
					b, _ := os.ReadFile(os.Getenv("CLI_SIGNAL_READY"))
					if _, err := fmt.Sscanf(string(b), "%d %d", &parent, &child); err == nil {
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
				if child == 0 {
					select {
					case err := <-done:
						t.Fatalf("entrypoint failed before readiness: %v: %s", err, output.String())
					default:
						t.Fatal("external descendant never ready")
					}
				}
				defer syscall.Kill(-parent, syscall.SIGKILL)
				group, err := syscall.Getpgid(child)
				if err != nil || group != parent {
					t.Fatalf("descendant group %d parent %d: %v", group, parent, err)
				}
				if err := cmd.Process.Signal(sig); err != nil {
					t.Fatal(err)
				}
				select {
				case err := <-done:
					if err == nil {
						t.Errorf("cancelled entrypoint succeeded: %s", output.String())
					}
				case <-time.After(5 * time.Second):
					t.Fatal("entrypoint did not finish cleanup")
				}
				// Reap scheduling may lag group termination; zombies cannot execute work.
				for _, pid := range []int{parent, child} {
					deadline = time.Now().Add(3 * time.Second)
					for time.Now().Before(deadline) {
						if syscall.Kill(pid, 0) == syscall.ESRCH {
							break
						}
						state, _ := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
						if strings.HasPrefix(strings.TrimSpace(string(state)), "Z") {
							break
						}
						time.Sleep(10 * time.Millisecond)
					}
					state, _ := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
					if syscall.Kill(pid, 0) == nil && !strings.HasPrefix(strings.TrimSpace(string(state)), "Z") {
						t.Errorf("host process survived signal: pid %d state %s", pid, state)
					}
				}
				if _, err := os.Stat(os.Getenv("CLI_SIGNAL_READY") + "-launched"); !os.IsNotExist(err) {
					t.Error("shell/agent launched after cancellation")
				}
				release, err := operationlock.Acquire(context.Background(), runtime, []string{operationlock.SandboxKey("signal-box")})
				if err != nil {
					t.Fatal("lock not released after cleanup", err)
				}
				release()
			})
		}
	}
}
