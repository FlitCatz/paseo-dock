package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"paseo-sbx/internal/config"
	"paseo-sbx/internal/operations"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIProfileCreateMapsToContainer(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	fake := t.TempDir()
	t.Setenv("PATH", fake+":"+os.Getenv("PATH"))
	if err := os.WriteFile(filepath.Join(fake, "docker"), []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"create", "build"} {
		a, err := NewApp("../..")
		if err != nil {
			t.Fatal(err)
		}
		a.Out = &bytes.Buffer{}
		a.Err = &bytes.Buffer{}
		_ = a.execute([]string{command, "--workspace", t.TempDir(), "--profiles", "codex-superpowers"})
		if a.Selection == nil || a.Selection.Schema != 2 || a.Selection.Category != config.CategoryContainer || len(a.Selection.Pairs) != 1 {
			t.Fatalf("%s selection: %+v", command, a.Selection)
		}
	}
}
func TestCLISandboxRequiresAgent(t *testing.T) {
	for _, args := range [][]string{{"build", "--category", "sandbox", "--print-bake"}, {"build", "--category", "sandbox", "--agent", "antigravity-cli", "--print-bake"}, {"create", "--category", "sandbox", "--agent", "codex", "--utilities", "rtk"}} {
		var errout bytes.Buffer
		if Run("../..", args, &bytes.Buffer{}, &bytes.Buffer{}, &errout) == 0 || strings.Contains(errout.String(), "flag provided but not defined") {
			t.Fatalf("agent validation missing: %v: %s", args, errout.String())
		}
	}
	a, _ := NewApp("../..")
	a.Out = &bytes.Buffer{}
	a.Err = &bytes.Buffer{}
	// Load reaches operations without an external latest lookup and must keep the typed selection.
	_ = a.execute([]string{"load", "--category", "sandbox", "--agent", "codex"})
	if a.Selection == nil || a.Selection.Category != config.CategorySandbox || a.Selection.Agent != "codex" || len(a.Selection.Pairs) != 0 {
		t.Fatalf("agent-only request: %+v", a.Selection)
	}
}
func TestOmittedTargetIsAmbiguousAcrossCategories(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	w, _ := config.Canonical(t.TempDir())
	runtime, _ := config.RuntimePath(w)
	reg := config.Registry{Workspace: w, Profiles: map[string]config.Entry{"sandbox/codex": {Category: config.CategorySandbox, Agent: "codex", Workspace: w, Sandbox: "test"}, "container/codex-superpowers": {Category: config.CategoryContainer, Agent: "codex", Tool: "superpowers", Workspace: w, ContainerID: "cid", Sandbox: "dev"}}, Active: []string{"sandbox/codex", "container/codex-superpowers"}}
	if err := config.WriteJSON(filepath.Join(runtime, "registry.json"), reg); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"shell", "stop", "update", "versions", "register", "unregister"} {
		var errout bytes.Buffer
		code := Run("../..", []string{command, "--workspace", w}, &bytes.Buffer{}, &bytes.Buffer{}, &errout)
		if code == 0 || !strings.Contains(errout.String(), "categor") {
			t.Fatalf("%s did not report category ambiguity: %s", command, errout.String())
		}
	}
}

type cliCategoryBackend struct {
	created []config.Entry
	stopped []config.Entry
}

func (b *cliCategoryBackend) List(context.Context) ([]operations.RuntimeState, error) {
	return nil, nil
}
func (b *cliCategoryBackend) Create(_ context.Context, e config.Entry) (config.Entry, error) {
	e.ContainerID = strings.Repeat("a", 64)
	e.ImageID = "sha256:test"
	b.created = append(b.created, e)
	return e, nil
}
func (b *cliCategoryBackend) Inspect(context.Context, config.Entry) (operations.RuntimeState, error) {
	return operations.RuntimeState{Known: true, Running: true}, nil
}
func (b *cliCategoryBackend) Exec(_ context.Context, _ config.Entry, args []string, _ io.Reader, out io.Writer, _ io.Writer) error {
	if strings.Join(args, " ") == "printenv HOME" {
		_, err := io.WriteString(out, "/home/agent")
		return err
	}
	return nil
}
func (b *cliCategoryBackend) Stop(_ context.Context, e config.Entry) error {
	b.stopped = append(b.stopped, e)
	return nil
}

func TestCLIContainerDispatchPreservesPortsAndStoredTarget(t *testing.T) {
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
	a.Out = &bytes.Buffer{}
	a.Err = &bytes.Buffer{}
	b := &cliCategoryBackend{}
	a.Backends = map[config.Category]operations.Backend{config.CategoryContainer: b}
	workspace := t.TempDir()
	if err = a.execute([]string{"create", "--workspace", workspace, "--profiles", "codex-superpowers", "--ports", "8080:3000"}); err != nil {
		t.Fatal(err)
	}
	if len(b.created) != 1 || b.created[0].Category != config.CategoryContainer || b.created[0].Network != "bridge" || len(b.created[0].Ports) != 1 || b.created[0].Ports[0] != (config.PortMapping{HostAddress: "127.0.0.1", HostPort: 8080, ContainerPort: 3000}) {
		t.Fatalf("backend received wrong create: %+v", b.created)
	}
	a.Catalog.Methodologies = nil // Existing target management trusts persisted stack.
	if err = a.execute([]string{"stop", "codex-superpowers", "--workspace", workspace, "--category", "container"}); err != nil {
		t.Fatal(err)
	}
	if len(b.stopped) != 1 || !b.stopped[0].EqualIdentity(b.created[0]) {
		t.Fatal("stop lost exact category/resource identity", b.stopped)
	}
}

func TestCLIVersionsPrintsStoredTargetResult(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	w, _ := config.Canonical(t.TempDir())
	runtime, _ := config.RuntimePath(w)
	e := config.Entry{Category: config.CategoryContainer, Agent: "codex", Tool: "removed", Workspace: w, ContainerID: strings.Repeat("b", 64), Image: "kept", ImageID: "sha256:kept", Network: "bridge"}
	if err := config.WriteJSON(filepath.Join(runtime, "registry.json"), config.Registry{Workspace: w, Profiles: map[string]config.Entry{e.Key(): e}}); err != nil {
		t.Fatal(err)
	}
	a, err := NewApp("../..")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	a.Out = &out
	a.Err = &bytes.Buffer{}
	a.Backends = map[config.Category]operations.Backend{config.CategoryContainer: &cliCategoryBackend{}}
	_ = a.execute([]string{"versions", e.Key(), "--workspace", w}) // Partial probe failures still print their recorded results.
	if !strings.Contains(out.String(), "Versions") || !strings.Contains(out.String(), "VersionCheckedAt") {
		t.Fatal("versions command omitted structured result", out.String())
	}
}

func TestCLISandboxCategoryIncludesLegacyManagementScope(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	w, _ := config.Canonical(t.TempDir())
	runtime, _ := config.RuntimePath(w)
	reg := config.Registry{Workspace: w, Profiles: map[string]config.Entry{"codex-superpowers": {Agent: "codex", Tool: "superpowers", Workspace: w, Sandbox: "legacy"}, "sandbox/codex": {Category: config.CategorySandbox, Agent: "codex", Workspace: w, Sandbox: "agent"}}, Active: []string{"codex-superpowers", "sandbox/codex"}}
	if err := config.WriteJSON(filepath.Join(runtime, "registry.json"), reg); err != nil {
		t.Fatal(err)
	}
	a, err := NewApp("../..")
	if err != nil {
		t.Fatal(err)
	}
	_, targets, err := a.registryTargets(w, "", config.CategorySandbox, false)
	if err != nil || len(targets) != 2 {
		t.Fatal("explicit sandbox scope excluded legacy targets", err)
	}
}
