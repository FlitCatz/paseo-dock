package cli

import (
	"bytes"
	"os"
	"paseo-sbx/internal/config"
	"paseo-sbx/internal/operations"
	"path/filepath"
	"strings"
	"testing"
)

func configTestApp(t *testing.T) (*App, *cliCategoryBackend) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	lock, err := os.ReadFile("../../versions.lock.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "versions.lock.json"), lock, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "bin"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bin", "agent-wrapper"), []byte("wrapper"), 0700); err != nil {
		t.Fatal(err)
	}
	a, err := NewApp(root)
	if err != nil {
		t.Fatal(err)
	}
	a.In, a.Out, a.Err = &bytes.Buffer{}, &bytes.Buffer{}, &bytes.Buffer{}
	b := &cliCategoryBackend{}
	a.Backends = map[config.Category]operations.Backend{config.CategoryContainer: b}
	return a, b
}

func TestCreateConfigAndCLIOverrides(t *testing.T) {
	a, b := configTestApp(t)
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "project"), 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "create.json")
	body := `{"workspace":"./project","category":"container","pairs":[{"agent":"codex","methodology":"superpowers"}],"utilities":["rtk"],"ports":["8080:3000"],"build":true,"apply":true,"codex_userns":true}`
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	err := a.execute([]string{"create", "--config", path, "--build=false", "--apply=false", "--ports", "9090:4000", "--utilities=", "--codex-userns=false"})
	if err != nil {
		t.Fatal(err)
	}
	if len(b.created) != 1 {
		t.Fatal("no container created", b.created)
	}
	e := b.created[0]
	want, _ := config.Canonical(filepath.Join(dir, "project"))
	if e.Workspace != want || len(e.Utilities) != 0 || e.SecurityPolicy != "" || len(e.Ports) != 1 || e.Ports[0].HostPort != 9090 || e.Ports[0].ContainerPort != 4000 {
		t.Fatalf("wrong merged create: %+v", e)
	}
}

func TestContainerMemoryRejectedBeforeCreate(t *testing.T) {
	a, b := configTestApp(t)
	err := a.execute([]string{"create", "--profiles", "codex-superpowers", "--workspace", t.TempDir(), "--memory", "4g"})
	if err == nil || !strings.Contains(err.Error(), "sandbox") || len(b.created) != 0 {
		t.Fatalf("unused memory silently accepted: %v %+v", err, b.created)
	}
}

func TestCreateConfigRejectsUnknownOptionBeforeCreate(t *testing.T) {
	a, b := configTestApp(t)
	path := filepath.Join(t.TempDir(), "create.json")
	if err := os.WriteFile(path, []byte(`{"workspace":".","category":"container","memroy":"4g"}`), 0600); err != nil {
		t.Fatal(err)
	}
	err := a.execute([]string{"create", "--config", path})
	if err == nil || !strings.Contains(err.Error(), "memroy") || len(b.created) != 0 {
		t.Fatalf("unknown option: %v", err)
	}
}

func TestCreateConfigPairCannotExpandIntoAdditionalProfiles(t *testing.T) {
	a, b := configTestApp(t)
	path := filepath.Join(t.TempDir(), "create.json")
	body := `{"category":"container","pairs":[{"agent":"codex-superpowers,claude","methodology":"superpowers"}],"build":false}`
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	err := a.execute([]string{"create", "--config", path, "--workspace", t.TempDir()})
	if err == nil || len(b.created) != 0 {
		t.Fatalf("invalid structured pair expanded: %v %+v", err, b.created)
	}
}
