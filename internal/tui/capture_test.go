//go:build darwin || linux

package tui

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/creack/pty"
	"paseo-dock/internal/config"
	"paseo-dock/internal/operations"
)

// Test-only fixture: the real Bubble Tea application runs in a PTY; no Docker,
// SBX or provider mutations are invoked. Raw captures can be replayed in xterm.
func captureDashboard() *Dashboard {
	c := config.DefaultCatalog()
	stamp := time.Date(2026, 10, 20, 0, 30, 0, 0, time.UTC)
	connected := operations.Connection{Status: "connected", CheckedAt: stamp}
	bundle := config.Entry{Category: config.CategoryContainer, Name: "dev-suite", Pairs: []config.Pair{{Agent: "codex", Methodology: "openspec"}, {Agent: "claude", Methodology: "gsd-core"}}, Utilities: []string{"rtk", "headroom"}, Image: "paseo-dock/dev-suite:2026.10", ContainerID: "93c2d8b7e1af", Network: "bridge", Workspace: "/projects/paseo-dock"}
	sandbox := config.Entry{Category: config.CategorySandbox, Agent: "codex", Sandbox: "sbx-codex", Image: "paseo-dock/sandbox/codex:stable"}
	snap := operations.Snapshot{SBX: connected, Paseo: connected, Docker: connected, Templates: connected,
		Workspaces: []operations.Workspace{
			{ID: "paseo-dev", Path: "/projects/paseo-dock", Sandboxes: []operations.Sandbox{
				{Target: operations.Target{Entry: bundle}, RuntimeState: "running", Active: true, Registration: "registered"},
				{Target: operations.Target{Entry: sandbox}, RuntimeState: "stopped", Registration: "not registered"},
				{Target: operations.Target{Profile: "claude-review", Entry: config.Entry{Agent: "claude", Tool: "native", Sandbox: "review"}}, RuntimeState: "unknown"},
			}},
			{ID: "policy-dev", Path: "/projects/policy-kernel", Sandboxes: []operations.Sandbox{{Target: operations.Target{Entry: config.Entry{Category: config.CategoryContainer, Name: "backend", Agent: "codex", Tool: "native"}}, RuntimeState: "running", Active: true}}},
			{ID: "mobile", Path: "/projects/내비게이션", Sandboxes: []operations.Sandbox{}},
		}, TemplateRows: []operations.Template{{Repository: "paseo-dock/dev-suite", Tag: "2026.10", ID: "image-1"}, {Repository: "paseo-dock/sandbox/codex", Tag: "stable", ID: "image-2"}},
	}
	b := &dashboardBackend{snapshot: snap}
	m := NewDashboard(b, c, "paseo-dev")
	m.Snapshot = snap
	m.LastSuccess = stamp
	m.reconcileSelection()
	m.Focus = 1
	j := &job{id: 1, state: "completed", request: operations.Request{Kind: operations.KindUpdate, Target: operations.Target{Entry: bundle}}, started: stamp, finished: stamp.Add(12 * time.Second), log: newJobLog(), stage: "verify"}
	j.log.emit(operations.Event{Stage: "stdout", Text: "[prepare] Checking registered environment dev-suite\n[agent] codex: verified\n[agent] claude: verified\n[methodology] openspec: verified\n[utility] rtk: verified\n[utility] headroom: verified\n[verify] All selected tools are available\n완료 · Workspace와 provider 설정을 유지했습니다.\n"})
	m.jobs = []*job{j}
	return m
}

func TestControlCenterCaptureHelper(t *testing.T) {
	if os.Getenv("PASEO_CAPTURE_HELPER") != "1" {
		return
	}
	m := captureDashboard()
	var model tea.Model = m
	switch os.Getenv("PASEO_CAPTURE_SCENE") {
	case "logs":
		m.Focus, m.rightTab, m.expanded = 3, 1, true
	case "wizard", "review":
		w := newContainerModel(m.Catalog, config.Selection{Schema: 2, Category: config.CategoryContainer, Name: "dev-suite", Pairs: []config.Pair{{Agent: "codex", Methodology: "openspec"}, {Agent: "claude", Methodology: "gsd-core"}}, Utilities: []string{"rtk", "headroom"}}, "/projects/paseo-dock", "2g")
		w.Color = true
		if os.Getenv("PASEO_CAPTURE_SCENE") == "review" {
			for i := 0; w.Step < 3 && i < 8; i++ {
				key(w, tea.KeyEnter)
			}
			if w.Step != 3 {
				fmt.Fprintln(os.Stderr, w.Error)
				os.Exit(1)
			}
		}
		model = w
	case "create":
		m.startCreation()
	}
	_, err := tea.NewProgram(model, tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout)).Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

// Opt in: PASEO_CAPTURE_DIR=/absolute/path go test ./internal/tui
// -run '^TestControlCenterCapture$' -count=1. Capture before exiting alt-screen.
func TestControlCenterCapture(t *testing.T) {
	dir := os.Getenv("PASEO_CAPTURE_DIR")
	if dir == "" {
		t.Skip("set PASEO_CAPTURE_DIR for visual evidence")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, scene := range []struct {
		name          string
		width, height uint16
	}{
		{"dashboard", 120, 40}, {"compact", 80, 24}, {"small", 60, 18}, {"wizard", 120, 40}, {"review", 120, 40}, {"logs", 120, 32}, {"create", 120, 40},
	} {
		t.Run(scene.name, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestControlCenterCaptureHelper$")
			cmd.Env = ptyEnvironment(map[string]string{"TERM": "xterm-256color", "COLORTERM": "truecolor", "PASEO_CAPTURE_HELPER": "1", "PASEO_CAPTURE_SCENE": scene.name})
			env := cmd.Env[:0]
			for _, v := range cmd.Env {
				if !strings.HasPrefix(v, "NO_COLOR=") {
					env = append(env, v)
				}
			}
			cmd.Env = env
			terminal, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: scene.width, Rows: scene.height})
			if err != nil {
				t.Fatal(err)
			}
			defer terminal.Close()
			defer cmd.Process.Kill()
			transcript := &ptyTranscript{}
			go func() {
				buffer := make([]byte, 32768)
				for {
					n, err := terminal.Read(buffer)
					if n > 0 {
						transcript.append(buffer[:n])
					}
					if err != nil {
						return
					}
				}
			}()
			deadline := time.Now().Add(8 * time.Second)
			for !strings.Contains(transcript.text(), "PASEO DOCK") && time.Now().Before(deadline) {
				time.Sleep(20 * time.Millisecond)
			}
			if !strings.Contains(transcript.text(), "PASEO DOCK") {
				t.Fatalf("no application frame: %q", transcript.text())
			}
			// Wait for a stable output length, including the initial snapshot redraw.
			for i := 0; i < 20; i++ {
				before := transcript.length()
				time.Sleep(100 * time.Millisecond)
				if transcript.length() == before {
					break
				}
			}
			if err = os.WriteFile(filepath.Join(dir, scene.name+".ansi"), []byte(transcript.text()), 0644); err != nil {
				t.Fatal(err)
			}
			_, _ = io.WriteString(terminal, "\x03")
			time.Sleep(200 * time.Millisecond)
			_, _ = io.WriteString(terminal, "\x03")
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("capture did not exit")
			}
		})
	}
}
