package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCreateConfigPathsAndSandboxOptions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "create.json")
	data := `{"category":"sandbox","agent":"codex","workspace":"./project","paseo_home":"./paseo","memory":"4g"}`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadCreateConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	workspace, _ := Canonical(filepath.Join(dir, "project"))
	home, _ := Canonical(filepath.Join(dir, "paseo"))
	if c.Workspace != workspace || c.PaseoHome != home || c.Schema != 2 || c.Agent != "codex" || c.Memory != "4g" || c.Build == nil || !*c.Build {
		t.Fatalf("lost config/defaults: %+v", c)
	}
	if _, err := DefaultCatalog().Normalize(c.Selection()); err != nil {
		t.Fatal(err)
	}
}

func TestCreateConfigRejectsMalformedDocuments(t *testing.T) {
	for _, body := range []string{`{"schema":1}`, `{"category":"container","typo":true}`, `{} {}`, `{"repository":{"ref":"main"}}`, `{"repository":{"url":"x","typo":1}}`} {
		t.Run(body, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "create.json")
			if err := os.WriteFile(path, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadCreateConfig(path); err == nil {
				t.Fatal("invalid config accepted")
			}
		})
	}
}
