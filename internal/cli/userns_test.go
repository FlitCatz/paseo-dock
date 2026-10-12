package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"paseo-sbx/internal/config"
	"paseo-sbx/internal/operations"
)

func userNSApp(t *testing.T) (*App, *cliCategoryBackend, *bytes.Buffer) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
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
	a, err := NewApp(root)
	if err != nil {
		t.Fatal(err)
	}
	diagnostics := &bytes.Buffer{}
	a.In, a.Out, a.Err = &bytes.Buffer{}, &bytes.Buffer{}, diagnostics
	b := &cliCategoryBackend{}
	a.Backends = map[config.Category]operations.Backend{config.CategoryContainer: b}
	return a, b, diagnostics
}

func TestCLICodexUserNSCreatePropagation(t *testing.T) {
	a, b, diagnostics := userNSApp(t)
	workspace := t.TempDir()
	if err := a.execute([]string{"create", "--workspace", workspace, "--category", "container", "--profiles", "codex-superpowers,claude-superpowers", "--codex-userns", "--ports", "8080:3000"}); err != nil {
		t.Fatal(err)
	}
	if a.Selection == nil || !a.Selection.CodexUserNS || len(b.created) != 2 {
		t.Fatalf("lost opt-in: %+v / %+v", a.Selection, b.created)
	}
	for _, e := range b.created {
		want := ""
		if e.Agent == "codex" {
			want = "codex-userns-v1"
		}
		if e.SecurityPolicy != want || len(e.Ports) != 1 || e.Ports[0].HostPort != 8080 {
			t.Fatalf("wrong create policy/ports: %+v", e)
		}
	}
	for _, want := range []string{"codex-userns-v1", "workspace-write", "all guest processes", "kernel syscall access", "no added capabilities"} {
		if !strings.Contains(diagnostics.String(), want) {
			t.Fatalf("missing policy disclosure %q: %s", want, diagnostics)
		}
	}
}

func TestCLICodexUserNSSelectionOverrides(t *testing.T) {
	for _, source := range []string{"saved", "file"} {
		for _, flagValue := range []string{"", "--codex-userns=false", "--codex-userns=true"} {
			t.Run(source+flagValue, func(t *testing.T) {
				a, b, _ := userNSApp(t)
				workspace, _ := config.Canonical(t.TempDir())
				saved := config.Selection{Schema: 2, Category: config.CategoryContainer, CodexUserNS: true, Pairs: []config.Pair{{Agent: "codex", Methodology: "superpowers"}}}
				args := []string{"create", "--workspace", workspace}
				if source == "file" {
					path := filepath.Join(t.TempDir(), "selection.json")
					if err := config.WriteJSON(path, saved); err != nil {
						t.Fatal(err)
					}
					args = append(args, "--selection", path)
				} else {
					runtime, _ := config.RuntimePath(workspace)
					if err := config.WriteJSON(filepath.Join(runtime, "registry.json"), config.Registry{Workspace: workspace, Selection: &saved, Profiles: map[string]config.Entry{}}); err != nil {
						t.Fatal(err)
					}
				}
				if flagValue != "" {
					args = append(args, flagValue)
				}
				if err := a.execute(args); err != nil {
					t.Fatal(err)
				}
				want := flagValue != "--codex-userns=false"
				if a.Selection.CodexUserNS != want || len(b.created) != 1 || (b.created[0].SecurityPolicy != "") != want {
					t.Fatalf("override/preserve lost: %+v / %+v", a.Selection, b.created)
				}
			})
		}
	}
}

func TestCLICodexUserNSFalseCannotBypassInvalidFileFormat(t *testing.T) {
	a, b, _ := userNSApp(t)
	path := filepath.Join(t.TempDir(), "selection.json")
	stale := config.Selection{Schema: 2, Category: config.CategoryContainer, CodexUserNS: true, Pairs: []config.Pair{{Agent: "claude", Methodology: "superpowers"}}}
	if err := config.WriteJSON(path, stale); err != nil {
		t.Fatal(err)
	}
	if err := a.execute([]string{"create", "--workspace", t.TempDir(), "--selection", path, "--codex-userns=false"}); err == nil || !strings.Contains(err.Error(), "codex_userns") {
		t.Fatalf("invalid file format accepted: %v", err)
	}
	if len(b.created) != 0 {
		t.Fatal("invalid file reached backend")
	}
}

func TestCLICodexUserNSRejectedBeforeDispatch(t *testing.T) {
	for _, args := range [][]string{
		{"build", "--profile", "codex-superpowers", "--print-bake", "--codex-userns"},
		{"load", "--profile", "codex-superpowers", "--codex-userns=false"},
		{"stop", "--codex-userns"},
		{"create", "--category", "sandbox", "--agent", "codex", "--codex-userns"},
		{"create", "--category", "container", "--profile", "claude-superpowers", "--codex-userns"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			a, b, _ := userNSApp(t)
			err := a.execute(args)
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), "codex") || len(b.created) != 0 {
				t.Fatalf("invalid opt-in accepted: %v / %v", args, err)
			}
		})
	}
}

func TestCLICodexUserNSHelpIsCreationOnly(t *testing.T) {
	for _, command := range []string{"create", "build", "load", "stop", "update"} {
		var out bytes.Buffer
		if code := Run("../..", []string{command, "--help"}, &bytes.Buffer{}, &bytes.Buffer{}, &out); code != 0 {
			t.Fatal(out.String())
		}
		if strings.Contains(out.String(), "-codex-userns") != (command == "create") {
			t.Fatalf("wrong %s help: %s", command, &out)
		}
	}
}
