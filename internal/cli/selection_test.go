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

func TestOpenSpecFailurePreservesActivationAndCanRetry(t *testing.T) {
	root, home, workspace, fake := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", fake+":"+os.Getenv("PATH"))
	b, err := os.ReadFile("../../versions.lock.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "versions.lock.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "bin"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bin", "agent-wrapper"), []byte("wrapper"), 0700); err != nil {
		t.Fatal(err)
	}
	// The marker simulates a project initialization failure, not a create failure.
	script := "#!/bin/sh\nif [ \"$1\" = ls ]; then echo '[]'; fi\nif [ \"$1\" = exec ] && [ -f \"$HOME/fail-init\" ]; then exit 1; fi\n"
	if err := os.WriteFile(filepath.Join(fake, "sbx"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	invoke := func(profile string) (int, string) {
		t.Helper()
		var stderr bytes.Buffer
		code := runLegacyFixture(root, []string{"create", "--workspace", workspace, "--profile", profile}, &bytes.Buffer{}, &bytes.Buffer{}, &stderr)
		return code, stderr.String()
	}
	if code, stderr := invoke("codex-superpowers"); code != 0 {
		t.Fatal(stderr)
	}
	if err := os.WriteFile(filepath.Join(home, "fail-init"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if code, stderr := invoke("antigravity-cli-openspec"); code == 0 || !strings.Contains(stderr, "OpenSpec initialization failed") {
		t.Fatalf("%d %s", code, stderr)
	}
	w, _ := config.Canonical(workspace)
	runtime, _ := config.RuntimePath(w)
	var registry config.Registry
	if err := config.ReadJSON(filepath.Join(runtime, "registry.json"), &registry); err != nil {
		t.Fatal(err)
	}
	if strings.Join(registry.ActiveProfiles(), ",") != "codex-superpowers" || registry.Profiles["antigravity-cli-openspec"].Sandbox == "" {
		t.Fatal(registry)
	}
	if err := os.Remove(filepath.Join(home, "fail-init")); err != nil {
		t.Fatal(err)
	}
	if code, stderr := invoke("antigravity-cli-openspec"); code != 0 {
		t.Fatal(stderr)
	}
	if err := config.ReadJSON(filepath.Join(runtime, "registry.json"), &registry); err != nil {
		t.Fatal(err)
	}
	if strings.Join(registry.ActiveProfiles(), ",") != "antigravity-cli-openspec" {
		t.Fatal(registry)
	}
}

func TestBuildUsesOnlySelectedPairs(t *testing.T) {
	var out, stderr bytes.Buffer
	if code := Run("../..", []string{"build", "--profile", "codex-ponytail", "--utilities", "rtk", "--print-bake"}, &bytes.Buffer{}, &out, &stderr); code != 0 {
		t.Fatal(stderr.String())
	}
	var cfg struct {
		Target map[string]struct {
			Args map[string]string `json:"args"`
			Tags []string          `json:"tags"`
		} `json:"target"`
	}
	if err := json.Unmarshal(out.Bytes(), &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Target) != 1 || cfg.Target["container-claude-superpowers"].Args != nil || cfg.Target["container-codex-ponytail"].Args["UTILITIES"] != "rtk" {
		t.Fatal(out.String())
	}
	if cfg.Target["container-codex-ponytail"].Tags[0] == "paseo-dock/codex-ponytail:2026.09.30" {
		t.Fatal("unversioned utility image")
	}
}
func TestNonInteractiveRequiresSelection(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	b, _ := os.ReadFile("../../versions.lock.json")
	os.WriteFile(filepath.Join(root, "versions.lock.json"), b, 0600)
	for _, args := range [][]string{{"create", "--workspace", t.TempDir()}, {"build", "--print-bake"}, {"build", "--profiles", "codex-superpowers", "--utilities", "unknown", "--print-bake"}} {
		if runLegacyFixture(root, args, &bytes.Buffer{}, &bytes.Buffer{}, &bytes.Buffer{}) == 0 {
			t.Fatalf("accepted %v", args)
		}
	}
}
func TestMergeRemovesOnlyOwnedInactiveProviders(t *testing.T) {
	providers := map[string]any{
		"codex-superpowers": map[string]any{"command": []any{"/base/abc/agent-wrapper", "codex-superpowers"}},
		"claude-ponytail":   map[string]any{"command": []any{"/base/launcher/agent-wrapper", "claude-ponytail"}},
		"codex-ponytail":    map[string]any{"command": []any{"/base/def/agent-wrapper", "codex-ponytail"}},
		"other":             map[string]any{"command": []any{"/someone/agent-wrapper", "other"}},
	}
	current := map[string]any{"agents": map[string]any{"providers": providers}}
	installed := []config.Installed{{Registry: config.Registry{Profiles: map[string]config.Entry{"codex-superpowers": {Agent: "codex"}, "claude-ponytail": {Agent: "claude"}}, Active: []string{"codex-superpowers"}}}}
	generated := ProviderConfig(config.LauncherPath("/base"), ActiveAdapters(installed))
	MergeManagedConfig(current, generated, "/base")
	if len(providers) != 2 || providers["claude-ponytail"] != nil || providers["codex-ponytail"] != nil || providers["other"] == nil {
		t.Fatal(providers)
	}
	command := providers["codex-superpowers"].(map[string]any)["command"].([]string)
	if command[0] != "/base/launcher/agent-wrapper" {
		t.Fatal(command)
	}
}
func TestSelectedCreatePreservesInactiveAndRejectsUtilityMismatch(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := t.TempDir()
	fake := t.TempDir()
	t.Setenv("PATH", fake+":"+os.Getenv("PATH"))
	b, _ := os.ReadFile("../../versions.lock.json")
	os.WriteFile(filepath.Join(root, "versions.lock.json"), b, 0600)
	os.Mkdir(filepath.Join(root, "bin"), 0755)
	os.WriteFile(filepath.Join(root, "bin", "agent-wrapper"), []byte("wrapper"), 0700)
	os.WriteFile(filepath.Join(fake, "sbx"), []byte("#!/bin/sh\nif [ \"$1\" = ls ]; then cat \"$HOME/rows\"; fi\n"), 0700)
	os.WriteFile(filepath.Join(home, "rows"), []byte("[]"), 0600)
	invoke := func(args ...string) int {
		t.Helper()
		var stderr bytes.Buffer
		code := runLegacyFixture(root, args, &bytes.Buffer{}, &bytes.Buffer{}, &stderr)
		if code != 0 {
			t.Log(stderr.String())
		}
		return code
	}
	if invoke("create", "--workspace", workspace, "--profiles", "codex-superpowers,codex-ponytail") != 0 {
		t.Fatal("create failed")
	}
	w, _ := config.Canonical(workspace)
	runtime, _ := config.RuntimePath(w)
	var reg config.Registry
	config.ReadJSON(filepath.Join(runtime, "registry.json"), &reg)
	if len(reg.Profiles) != 2 || len(reg.ActiveProfiles()) != 2 {
		t.Fatal(reg)
	}
	rows := []map[string]string{}
	for _, e := range reg.Profiles {
		rows = append(rows, map[string]string{"name": e.Sandbox})
	}
	config.WriteJSON(filepath.Join(home, "rows"), rows)
	// Changing the selection creates a different image identity, so an existing
	// sandbox must report the mismatch rather than claim it was reconfigured.
	before, _ := os.ReadFile(filepath.Join(runtime, "registry.json"))
	if invoke("create", "--workspace", workspace, "--profiles", "codex-superpowers", "--utilities", "rtk") == 0 {
		t.Fatal("reused sandbox with missing utility")
	}
	after, _ := os.ReadFile(filepath.Join(runtime, "registry.json"))
	if !bytes.Equal(before, after) {
		t.Fatal("mismatch changed registry")
	}
}

func TestFailedWrapperFinalizationPreservesActiveSelection(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := t.TempDir()
	fake := t.TempDir()
	t.Setenv("PATH", fake+":"+os.Getenv("PATH"))
	b, _ := os.ReadFile("../../versions.lock.json")
	os.WriteFile(filepath.Join(root, "versions.lock.json"), b, 0600)
	os.Mkdir(filepath.Join(root, "bin"), 0755)
	wrapper := filepath.Join(root, "bin", "agent-wrapper")
	os.WriteFile(wrapper, []byte("wrapper"), 0700)
	os.WriteFile(filepath.Join(fake, "sbx"), []byte("#!/bin/sh\nif [ \"$1\" = ls ]; then echo '[]'; fi\n"), 0700)
	run := func(profiles string) int {
		return runLegacyFixture(root, []string{"create", "--workspace", workspace, "--profiles", profiles}, &bytes.Buffer{}, &bytes.Buffer{}, &bytes.Buffer{})
	}
	if run("codex-superpowers") != 0 {
		t.Fatal("initial create")
	}
	os.Remove(wrapper)
	if run("claude-superpowers") == 0 {
		t.Fatal("missing wrapper accepted")
	}
	w, _ := config.Canonical(workspace)
	runtime, _ := config.RuntimePath(w)
	var reg config.Registry
	config.ReadJSON(filepath.Join(runtime, "registry.json"), &reg)
	if !reg.IsActive("codex-superpowers") || reg.IsActive("claude-superpowers") {
		t.Fatalf("failed finalization switched selection: %+v", reg)
	}
	a, _ := NewApp(root)
	s, err := a.resolveSelection(workspace, "", "", "", "", "", "", false)
	if err != nil || len(s.Pairs) != 1 || s.Pairs[0].Agent != "codex" {
		t.Fatalf("saved selection changed: %+v %v", s, err)
	}
}
func TestStopUsesPreservedRegistryAfterCatalogRemoval(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := t.TempDir()
	w, _ := config.Canonical(workspace)
	runtime, _ := config.RuntimePath(w)
	b, _ := os.ReadFile("../../versions.lock.json")
	os.WriteFile(filepath.Join(root, "versions.lock.json"), b, 0600)
	config.WriteJSON(filepath.Join(runtime, "registry.json"), config.Registry{Workspace: w, Profiles: map[string]config.Entry{"codex-removed": {Agent: "codex", Tool: "removed", Workspace: w, Sandbox: "preserved"}}})
	fake := t.TempDir()
	t.Setenv("PATH", fake+":"+os.Getenv("PATH"))
	log := filepath.Join(home, "stopped")
	os.WriteFile(filepath.Join(fake, "sbx"), []byte("#!/bin/sh\nprintf '%s' \"$*\" > \"$HOME/stopped\"\n"), 0700)
	if code := Run(root, []string{"stop", "codex-removed", "--workspace", workspace}, &bytes.Buffer{}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("catalog removal stranded sandbox")
	}
	got, _ := os.ReadFile(log)
	if string(got) != "stop preserved" {
		t.Fatal(string(got))
	}
}

func TestBuildBundlesSelectedPairsIntoOneImage(t *testing.T) {
	var out, stderr bytes.Buffer
	if code := Run("../..", []string{"build", "--profiles", "codex-superpowers,claude-ponytail,codex-ponytail", "--utilities", "rtk", "--name", "ci-all", "--print-bake"}, &bytes.Buffer{}, &out, &stderr); code != 0 {
		t.Fatal(stderr.String())
	}
	var cfg struct {
		Group  map[string]struct{ Targets []string } `json:"group"`
		Target map[string]struct {
			Args   map[string]string `json:"args"`
			Tags   []string          `json:"tags"`
			Labels map[string]string `json:"labels"`
		} `json:"target"`
	}
	if err := json.Unmarshal(out.Bytes(), &cfg); err != nil {
		t.Fatal(err)
	}
	target, ok := cfg.Target["container-ci-all"]
	if len(cfg.Target) != 1 || !ok || strings.Join(cfg.Group["default"].Targets, ",") != "container-ci-all" {
		t.Fatal(out.String())
	}
	args := target.Args
	if args["AGENTS"] != "codex,claude" || args["PAIRS"] != "claude:ponytail:git-plugin,codex:ponytail:git-plugin,codex:superpowers:git-plugin" || args["UTILITIES"] != "rtk" || args["AGENT"] != "" {
		t.Fatal(args)
	}
	lock, err := config.Load("../..")
	if err != nil {
		t.Fatal(err)
	}
	if args["BASE_IMAGE"] != lock.BaseImages["codex"] || !strings.HasPrefix(target.Tags[0], "paseo-dock/container/ci-all:"+lock.Tag+"-") || target.Labels["io.paseo-sbx.agent"] != "codex,claude" {
		t.Fatal(target)
	}
	var manifests map[string]map[string]any
	if err := json.Unmarshal([]byte(args["AGENT_MANIFESTS"]), &manifests); err != nil || manifests["codex"]["version"] != lock.Agents["codex"] || manifests["claude"]["version"] != lock.Agents["claude"] {
		t.Fatal(args["AGENT_MANIFESTS"], err)
	}
}
