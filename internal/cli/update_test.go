package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"paseo-dock/internal/config"
)

func TestUpdateCLISelectionAndFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PASEO_DOCK_AUTO_UPDATE", "0") // Explicit updates still execute.
	bin := t.TempDir()
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	stub := `#!/bin/sh
printf '%s\n' "$*" >> "$HOME/update-commands"
if [ "$4" = broken ]; then exit 1; fi
case "$5" in
  printenv) echo /home/agent ;;
  cat)
    case "$6" in
      */utilities/installed.json) exit 1 ;;
      *) echo '{"lock":{"tools":{"openspec":{"package":"@fission-ai/openspec"}}}}' ;;
    esac ;;
  codex) echo 0.159.2 ;;
  openspec) echo 1.14.0 ;;
esac
`
	if err := os.WriteFile(filepath.Join(bin, "sbx"), []byte(stub), 0700); err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	workspace, err := config.Canonical(workspace)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := config.RuntimePath(workspace)
	if err != nil {
		t.Fatal(err)
	}
	reg := config.Registry{Workspace: workspace, Active: []string{"claude-openspec", "codex-openspec"}, Profiles: map[string]config.Entry{
		"claude-openspec": {Agent: "claude", Tool: "openspec", Sandbox: "broken", Workspace: workspace},
		"codex-openspec":  {Agent: "codex", Tool: "openspec", Sandbox: "working", Workspace: workspace},
	}}
	if err = config.WriteJSON(filepath.Join(runtime, "registry.json"), reg); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if code := Run("../..", []string{"update", "--workspace", workspace}, &bytes.Buffer{}, &out, &stderr); code == 0 {
		t.Fatal("partial failure reported success")
	}
	log, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), "update-commands"))
	if err != nil {
		t.Fatal(err, stderr.String())
	}
	if !strings.Contains(string(log), "broken printenv HOME") || !strings.Contains(string(log), "working codex --version") {
		t.Fatal("remaining profiles not updated", string(log))
	}
	os.Remove(filepath.Join(os.Getenv("HOME"), "update-commands"))
	stderr.Reset()
	if code := Run("../..", []string{"update", "codex-openspec", "--workspace", workspace}, &bytes.Buffer{}, &out, &stderr); code != 0 {
		t.Fatal(stderr.String())
	}
	log, _ = os.ReadFile(filepath.Join(os.Getenv("HOME"), "update-commands"))
	if strings.Contains(string(log), "broken") || !strings.Contains(string(log), "working") {
		t.Fatal("profile selection ignored", string(log))
	}
}

func TestUpdateHelpStatesRefreshBoundary(t *testing.T) {
	var out, stderr bytes.Buffer
	if code := Run("../..", []string{"update", "--help"}, &bytes.Buffer{}, &out, &stderr); code != 0 {
		t.Fatalf("update --help exit %d: %s", code, stderr.String())
	}
	help := stderr.String()
	for _, want := range []string{"paseo-dock-managed tools", "Node, Python/uv, the base image", "omp/gjc", "ACP adapters", "versions.lock.json", "log in to each agent again", "-profile"} {
		if !strings.Contains(help, want) {
			t.Errorf("update help lacks %q:\n%s", want, help)
		}
	}
}
