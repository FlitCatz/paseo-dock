//go:build darwin || linux

package cli

import (
	"bytes"
	"encoding/json"
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
	"paseo-dock/internal/config"
	"paseo-dock/internal/operations"
)

func TestCodexUserNSInteractiveHelper(t *testing.T) {
	if os.Getenv("CLI_USERNS_HELPER") != "1" {
		return
	}
	a, err := NewApp(os.Getenv("CLI_USERNS_ROOT"))
	if err != nil {
		t.Fatal(err)
	}
	a.In, a.Out, a.Err = os.Stdin, os.Stdout, os.Stderr
	a.Backends = map[config.Category]operations.Backend{config.CategoryContainer: &cliCategoryBackend{}}
	var args []string
	if err = json.Unmarshal([]byte(os.Getenv("CLI_USERNS_ARGS")), &args); err != nil {
		t.Fatal(err)
	}
	if err = a.execute(args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("USERNS_CREATE_FINISHED=%t\n", a.Selection.CodexUserNS)
	os.Exit(0)
}

func TestCLICodexUserNSInteractiveSeeds(t *testing.T) {
	for _, tc := range []struct {
		name                         string
		saved, profile, toggle, want bool
		flag                         string
	}{
		{name: "saved-preserved", saved: true, want: true},
		{name: "saved-explicit-false", saved: true, flag: "--codex-userns=false"},
		{name: "explicit-profile", profile: true, flag: "--codex-userns", want: true},
		{name: "fresh-seed", flag: "--codex-userns", want: true},
		{name: "user-can-disable-seed", profile: true, flag: "--codex-userns", toggle: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, _, _ := userNSApp(t)
			workspace := t.TempDir()
			args := []string{"create", "--interactive", "--skip-build", "--workspace", workspace}
			if tc.profile {
				args = append(args, "--profile", "codex-superpowers")
			}
			if tc.flag != "" {
				args = append(args, tc.flag)
			}
			if tc.saved {
				w, _ := config.Canonical(workspace)
				runtime, _ := config.RuntimePath(w)
				saved := config.Selection{Schema: 2, Category: config.CategoryContainer, CodexUserNS: true, Pairs: []config.Pair{{Agent: "codex", Methodology: "superpowers"}}}
				if err := config.WriteJSON(filepath.Join(runtime, "registry.json"), config.Registry{Workspace: w, Selection: &saved, Profiles: map[string]config.Entry{}}); err != nil {
					t.Fatal(err)
				}
			}
			encoded, _ := json.Marshal(args)
			cmd := exec.Command(os.Args[0], "-test.run=^TestCodexUserNSInteractiveHelper$")
			cmd.Env = append(os.Environ(), "CLI_USERNS_HELPER=1", "CLI_USERNS_ROOT="+a.Root, "CLI_USERNS_ARGS="+string(encoded), "TERM=xterm-256color", "NO_COLOR=1")
			terminal, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: 160, Rows: 60})
			if err != nil {
				t.Fatal(err)
			}
			defer terminal.Close()
			defer cmd.Process.Kill()
			var mu sync.Mutex
			var transcript bytes.Buffer
			go func() {
				buf := make([]byte, 4096)
				for {
					n, err := terminal.Read(buf)
					if n > 0 {
						mu.Lock()
						transcript.Write(buf[:n])
						mu.Unlock()
					}
					if err != nil {
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
			if !tc.saved && !tc.profile {
				send(" ")
			}
			send("\r")
			// Section headings are updated incrementally; wait for the active step.
			wait("2 Methodology]")
			if !tc.saved && !tc.profile {
				send(" ")
			}
			send("\r\r")
			wait("u: Codex workspace-write")
			beforeToggle := tc.want != tc.toggle
			state := "off (default Docker policy)"
			if beforeToggle {
				state = "on (codex-userns-v1)"
			}
			if !strings.Contains(text(), state) {
				t.Fatalf("wrong interactive seed, want %s: %s", state, text())
			}
			if tc.toggle {
				send("u")
			}
			send("\r")
			wait(fmt.Sprintf("USERNS_CREATE_FINISHED=%t", tc.want))
			if err = cmd.Wait(); err != nil {
				t.Fatal(err, text())
			}
			w, _ := config.Canonical(workspace)
			runtime, _ := config.RuntimePath(w)
			var reg config.Registry
			if err := config.ReadJSON(filepath.Join(runtime, "registry.json"), &reg); err != nil {
				t.Fatal(err)
			}
			if reg.Selection == nil || reg.Selection.CodexUserNS != tc.want || (reg.Profiles["container/codex-superpowers"].SecurityPolicy != "") != tc.want {
				t.Fatalf("interactive create lost policy: %+v", reg)
			}
		})
	}
}
