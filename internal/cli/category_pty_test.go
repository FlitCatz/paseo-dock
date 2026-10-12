//go:build darwin || linux

package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
	"paseo-sbx/internal/config"
	"paseo-sbx/internal/operations"
)

func TestInteractivePortsHelper(t *testing.T) {
	if os.Getenv("CLI_PORTS_PTY_HELPER") != "1" {
		return
	}
	a, err := NewApp(os.Getenv("CLI_PORTS_ROOT"))
	if err != nil {
		t.Fatal(err)
	}
	a.In, a.Out, a.Err = os.Stdin, os.Stdout, os.Stderr
	a.Backends = map[config.Category]operations.Backend{config.CategoryContainer: &cliCategoryBackend{}}
	args := []string{"create", "--skip-build", "--workspace", os.Getenv("CLI_PORTS_WORKSPACE"), "--ports", "8080:3000"}
	if os.Getenv("CLI_PORTS_PROFILE") == "1" {
		args = append(args, "--interactive", "--profiles", "codex-superpowers")
	}
	if err = a.execute(args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("CLI_CREATE_FINISHED")
	os.Exit(0)
}

func TestCLIInteractivePortsReachConfirmationAndCreate(t *testing.T) {
	for _, preselected := range []bool{true, false} {
		t.Run(fmt.Sprint(preselected), func(t *testing.T) {
			home, root, workspace := t.TempDir(), t.TempDir(), t.TempDir()
			lock, err := os.ReadFile("../../versions.lock.json")
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(root, "versions.lock.json"), lock, 0600); err != nil {
				t.Fatal(err)
			}
			if err = os.Mkdir(filepath.Join(root, "bin"), 0700); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(root, "bin", "agent-wrapper"), []byte("wrapper"), 0700); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(os.Args[0], "-test.run=^TestInteractivePortsHelper$")
			for _, item := range os.Environ() {
				if !strings.HasPrefix(item, "HOME=") {
					cmd.Env = append(cmd.Env, item)
				}
			}
			cmd.Env = append(cmd.Env, "HOME="+home, "TERM=xterm-256color", "NO_COLOR=1", "CLI_PORTS_PTY_HELPER=1", "CLI_PORTS_ROOT="+root, "CLI_PORTS_WORKSPACE="+workspace)
			if preselected {
				cmd.Env = append(cmd.Env, "CLI_PORTS_PROFILE=1")
			}
			terminal, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: 160, Rows: 50})
			if err != nil {
				t.Fatal(err)
			}
			defer terminal.Close()
			defer cmd.Process.Kill()
			var mu sync.Mutex
			var transcript bytes.Buffer
			go func() {
				b := make([]byte, 4096)
				for {
					n, e := terminal.Read(b)
					if n > 0 {
						mu.Lock()
						transcript.Write(b[:n])
						mu.Unlock()
					}
					if e != nil {
						return
					}
				}
			}()
			text := func() string { mu.Lock(); defer mu.Unlock(); return transcript.String() }
			wait := func(marker string) {
				t.Helper()
				deadline := time.Now().Add(8 * time.Second)
				for time.Now().Before(deadline) {
					if strings.Contains(text(), marker) {
						return
					}
					time.Sleep(10 * time.Millisecond)
				}
				t.Fatalf("terminal missing %q: %s", marker, text())
			}
			send := func(keys string) {
				t.Helper()
				if _, err := io.WriteString(terminal, keys); err != nil {
					t.Fatal(err)
				}
			}
			wait("Category")
			send("\x1b[B\r")
			wait("1  Agent")
			if !preselected {
				send(" ")
			}
			send("\r")
			wait("Methodology · Codex")
			if !preselected {
				send(" ")
			}
			send("\r\r")
			wait("┃  4  최종 확인")
			if !strings.Contains(text(), "127.0.0.1:8080") || !strings.Contains(text(), "3000") {
				t.Fatal("interactive confirmation discarded explicit ports", text())
			}
			send("\r")
			wait("CLI_CREATE_FINISHED")
			if err = cmd.Wait(); err != nil {
				t.Fatal(err, text())
			}
			t.Setenv("HOME", home)
			w, err := config.Canonical(workspace)
			if err != nil {
				t.Fatal(err)
			}
			runtime, err := config.RuntimePath(w)
			if err != nil {
				t.Fatal(err)
			}
			var reg config.Registry
			if err = config.ReadJSON(filepath.Join(runtime, "registry.json"), &reg); err != nil {
				t.Fatal(err)
			}
			e := reg.Profiles["container/codex-superpowers"]
			want := config.PortMapping{HostAddress: "127.0.0.1", HostPort: 8080, ContainerPort: 3000}
			if len(e.Ports) != 1 || e.Ports[0] != want {
				t.Fatal("interactive create request lost ports", e)
			}
		})
	}
}
