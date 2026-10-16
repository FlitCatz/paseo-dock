package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"paseo-dock/internal/config"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunPluginSelectsNativeContainer(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	workspace, err := config.Canonical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := config.RuntimePath(workspace)
	if err != nil {
		t.Fatal(err)
	}
	entry := config.Entry{Category: config.CategoryContainer, Workspace: workspace, Agent: "antigravity-cli", Tool: "native", ContainerID: strings.Repeat("a", 64), ImageID: "sha256:" + strings.Repeat("b", 64), Network: "bridge"}
	reg := config.Registry{Schema: 2, Workspace: workspace, Profiles: map[string]config.Entry{entry.Key(): entry}, Active: []string{entry.Key()}}
	if err = config.WriteJSON(filepath.Join(runtime, "registry.json"), reg); err != nil {
		t.Fatal(err)
	}
	// Dry-run routing installs a wrapper copy but never executes it. Keep its
	// trusted source in an isolated fixture instead of requiring make build.
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
	if err := os.WriteFile(filepath.Join(root, "bin", "agent-wrapper"), []byte("trusted fixture wrapper"), 0700); err != nil {
		t.Fatal(err)
	}
	a, err := NewApp(root)
	if err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	a.Out = &out
	a.Err = &stderr
	if err = a.runPlugin([]string{"--profile", "antigravity-cli-native", "--workspace", workspace, "--dry-run"}); err != nil {
		t.Fatal(err)
	}
	var settings struct {
		ProviderOptions struct {
			ExtraArgs []string `json:"extraArgs"`
		} `json:"providerOptions"`
	}
	if err = json.Unmarshal(out.Bytes(), &settings); err != nil {
		t.Fatal(err)
	}
	if len(settings.ProviderOptions.ExtraArgs) != 2 || settings.ProviderOptions.ExtraArgs[1] != "container-antigravity-cli-native" {
		t.Fatalf("wrong native runtime: %s", out.String())
	}
	legacy := config.Entry{Workspace: workspace, Agent: "antigravity-cli", Tool: "native", Sandbox: "legacy-native"}
	reg.Profiles[legacy.Key()] = legacy
	reg.Active = append(reg.Active, legacy.Key())
	if err = config.WriteJSON(filepath.Join(runtime, "registry.json"), reg); err != nil {
		t.Fatal(err)
	}
	if err = a.runPlugin([]string{"--workspace", workspace, "--dry-run"}); err == nil || !strings.Contains(err.Error(), "specify --category") {
		t.Fatalf("ambiguous native runtime accepted: %v", err)
	}
	for _, row := range []struct{ category, provider string }{{"container", "container-antigravity-cli-native"}, {"legacy-sandbox", "antigravity-cli-native"}} {
		out.Reset()
		if err = a.runPlugin([]string{"--category", row.category, "--workspace", workspace, "--dry-run"}); err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(out.Bytes(), &settings); err != nil {
			t.Fatal(err)
		}
		if settings.ProviderOptions.ExtraArgs[1] != row.provider {
			t.Fatalf("wrong %s route: %s", row.category, out.String())
		}
	}
}
