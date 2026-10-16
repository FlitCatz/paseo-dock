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

func TestBakeProfiles(t *testing.T) {
	a, err := NewApp("../..")
	if err != nil {
		t.Fatal(err)
	}
	cfg := a.BakeConfig("example/ns", "linux/amd64,linux/arm64")
	targets := cfg["target"].(map[string]any)
	if len(targets) != len(a.Catalog.AllProfiles()) {
		t.Fatal(targets)
	}
	target := targets["codex-gsd-core"].(map[string]any)
	if target["tags"].([]string)[0] != "example/ns/codex-gsd-core:2026.09.30" {
		t.Fatal(target)
	}
	if len(target["platforms"].([]string)) != 2 {
		t.Fatal(target)
	}
	agy := targets["antigravity-cli-native"].(map[string]any)["args"].(map[string]string)
	if agy["AGENT"] != "antigravity-cli" || agy["AGENT_VERSION"] == "" {
		t.Fatalf("Antigravity native CLI is not pinned: %v", agy)
	}
	if _, exists := target["args"].(map[string]string)["ANTIGRAVITY_COMPAT_IMAGE"]; exists {
		t.Fatal("Antigravity compatibility image leaked into Codex build args")
	}
}
func TestInvalidArguments(t *testing.T) {
	for _, args := range [][]string{{}, {"create"}, {"stop", "wrong"}, {"build", "--profile", "wrong"}, {"list", "--unexpected"}} {
		if Run("../..", args, &bytes.Buffer{}, &bytes.Buffer{}, &bytes.Buffer{}) == 0 {
			t.Fatalf("accepted %v", args)
		}
	}
}
func TestMergeProviderConfig(t *testing.T) {
	cfg := map[string]any{"agents": map[string]any{"providers": map[string]any{"custom": map[string]any{"enabled": true}}}, "daemon": map[string]any{"port": 1234}}
	generated := map[string]any{"agents": map[string]any{"providers": map[string]any{"codex-superpowers": map[string]any{"command": []string{"/trusted/agent-wrapper", "codex-superpowers"}}}}, "daemon": map[string]any{"mcp": map[string]any{"enabled": true}}}
	MergeConfig(cfg, generated)
	b, _ := json.Marshal(cfg)
	var got map[string]any
	json.Unmarshal(b, &got)
	providers := got["agents"].(map[string]any)["providers"].(map[string]any)
	if len(providers) != 2 || got["daemon"].(map[string]any)["port"] != float64(1234) {
		t.Fatal(got)
	}
}
func TestAtomicJSONPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "registry.json")
	if err := config.WriteJSON(path, map[string]any{"workspace": "한글"}); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
	var got map[string]any
	if err := config.ReadJSON(path, &got); err != nil {
		t.Fatal(err)
	}
	if got["workspace"] != "한글" {
		t.Fatal(got)
	}
}
func TestCanonicalHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path, err := config.Canonical("~")
	want, _ := filepath.EvalSymlinks(home)
	if err != nil || path != want {
		t.Fatalf("%s %v, want %s", path, err, want)
	}
}
func TestCommandLifecycle(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := filepath.Join(t.TempDir(), "한글 project")
	fake := t.TempDir()
	t.Setenv("PATH", fake+":"+os.Getenv("PATH"))
	b, _ := os.ReadFile("../../versions.lock.json")
	os.WriteFile(filepath.Join(root, "versions.lock.json"), b, 0600)
	os.WriteFile(filepath.Join(root, "Dockerfile"), []byte("FROM scratch"), 0600)
	os.Mkdir(filepath.Join(root, "bin"), 0755)
	os.WriteFile(filepath.Join(root, "bin", "agent-wrapper"), []byte("trusted executable"), 0700)
	os.WriteFile(filepath.Join(fake, "sbx"), []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$HOME/sbx-commands\"\nif [ \"$1\" = ls ]; then echo \"[]\"; fi\n"), 0700)
	os.WriteFile(filepath.Join(fake, "docker"), []byte("#!/bin/sh\nif [ \"$1\" = save ]; then touch \"$3\"; fi\n"), 0700)
	os.WriteFile(filepath.Join(fake, "paseo"), []byte("#!/bin/sh\nif [ \"$3\" = status ]; then echo '{\"localDaemon\":\"running\"}'; else echo \"$3\" > \"$2/action\"; fi\n"), 0700)
	invoke := func(args ...string) string {
		t.Helper()
		var out, stderr bytes.Buffer
		if code := runLegacyFixture(root, args, &bytes.Buffer{}, &out, &stderr); code != 0 {
			t.Fatalf("%v: %d %s", args, code, stderr.String())
		}
		return out.String()
	}
	invoke("build", "--profile", "codex-superpowers")
	if _, err := os.Stat(filepath.Join(root, ".generated", "bake.json")); err != nil {
		t.Fatal(err)
	}
	invoke("load", "--profile", "codex-superpowers")
	if _, err := os.Stat(filepath.Join(root, ".artifacts", "codex-superpowers.tar")); !os.IsNotExist(err) {
		t.Fatal("archive not removed")
	}
	invoke("create", "--workspace", workspace, "--profiles", strings.Join(config.Profiles, ","))
	var state map[string]string
	config.ReadJSON(filepath.Join(root, ".generated", "runtime.json"), &state)
	runtime := state["path"]
	var reg config.Registry
	if err := config.ReadJSON(filepath.Join(runtime, "registry.json"), &reg); err != nil || len(reg.Profiles) != len(config.Profiles) {
		t.Fatalf("registry %v %v", reg, err)
	}
	agy := reg.Profiles["antigravity-cli-native"]
	commands, _ := os.ReadFile(filepath.Join(home, "sbx-commands"))
	if strings.Count(string(commands), " openspec init ") != 1 || !strings.Contains(string(commands), "openspec init "+reg.Workspace+" --tools antigravity,claude,codex --profile core --no-animation") {
		t.Fatalf("selected OpenSpec integrations were not initialized together: %s", commands)
	}
	if agy.Agent != "antigravity-cli" || !strings.Contains(string(commands), agy.Image+" --env GEMINI_API_KEY=proxy-managed shell "+reg.Workspace) {
		t.Fatalf("Antigravity was not created as a shell sandbox: %s", commands)
	}
	fakeRows := []map[string]string{}
	for _, e := range reg.Profiles {
		fakeRows = append(fakeRows, map[string]string{"name": e.Sandbox})
	}
	rows, _ := json.Marshal(fakeRows)
	os.WriteFile(filepath.Join(fake, "sbx"), []byte("#!/bin/sh\nif [ \"$1\" = ls ]; then printf '%s\\n' '"+string(rows)+"'; fi\n"), 0700)
	invoke("create", "--workspace", workspace)
	invoke("list")
	invoke("shell", "codex-superpowers", "--workspace", workspace)
	invoke("stop", "--workspace", workspace, "claude-ponytail")
	paseoHome := filepath.Join(home, ".paseo")
	os.MkdirAll(paseoHome, 0700)
	original := []byte(`{"version":1,"agents":{"providers":{"custom":{"enabled":true}}},"daemon":{"port":4321}}`)
	os.WriteFile(filepath.Join(paseoHome, "config.json"), original, 0600)
	invoke("register", "--apply")
	var current map[string]any
	config.ReadJSON(filepath.Join(paseoHome, "config.json"), &current)
	providers := current["agents"].(map[string]any)["providers"].(map[string]any)
	if len(providers) != 9 {
		t.Fatal(providers)
	}
	if _, exists := providers["antigravity-cli-native"]; exists {
		t.Fatal("native plugin provider must not be registered as ACP")
	}
	command := providers["codex-superpowers"].(map[string]any)["command"].([]any)
	canonicalRuntime, err := filepath.EvalSymlinks(runtime)
	if err != nil {
		t.Fatal(err)
	}
	launcher := config.LauncherPath(filepath.Dir(canonicalRuntime))
	if len(command) != 2 || command[0] != launcher {
		t.Fatal(command)
	}
	if _, err = os.Stat(launcher); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(runtime, "agent-wrapper")); !os.IsNotExist(err) {
		t.Fatal("per-workspace launcher kept")
	}
	backups, _ := filepath.Glob(filepath.Join(paseoHome, "config.json.backup-*"))
	if len(backups) != 1 {
		t.Fatal(backups)
	}
	saved, _ := os.ReadFile(backups[0])
	if !bytes.Equal(saved, original) {
		t.Fatal("backup changed")
	}
	action, _ := os.ReadFile(filepath.Join(paseoHome, "action"))
	if string(action) != "reload\n" {
		t.Fatalf("action %s", action)
	}
	reg.Profiles["codex-superpowers"] = config.Entry{}
	config.WriteJSON(filepath.Join(runtime, "registry.json"), reg)
	if runLegacyFixture(root, []string{"create", "--workspace", workspace}, &bytes.Buffer{}, &bytes.Buffer{}, &bytes.Buffer{}) == 0 {
		t.Fatal("reused mismatching sandbox")
	}
}
func TestWorkspaceMustNotContainSourceRoot(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "paseo-dock")
	t.Setenv("HOME", t.TempDir())
	os.MkdirAll(filepath.Join(root, "bin"), 0755)
	b, _ := os.ReadFile("../../versions.lock.json")
	os.WriteFile(filepath.Join(root, "versions.lock.json"), b, 0600)
	os.WriteFile(filepath.Join(root, "Dockerfile"), []byte("FROM scratch"), 0600)
	os.WriteFile(filepath.Join(root, "bin", "agent-wrapper"), []byte("trusted executable"), 0700)
	fake := t.TempDir()
	t.Setenv("PATH", fake+":"+os.Getenv("PATH"))
	marker := filepath.Join(fake, "sbx-called")
	os.WriteFile(filepath.Join(fake, "sbx"), []byte("#!/bin/sh\ntouch \""+marker+"\"\necho \"[]\"\n"), 0700)
	for _, workspace := range []string{root, parent} {
		var stderr bytes.Buffer
		if runLegacyFixture(root, []string{"create", "--workspace", workspace, "--profile", "codex-superpowers"}, &bytes.Buffer{}, &bytes.Buffer{}, &stderr) == 0 {
			t.Fatalf("accepted workspace %s", workspace)
		}
		if !strings.Contains(stderr.String(), "source root") {
			t.Fatal(stderr.String())
		}
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("sbx invoked before workspace validation")
	}
	// A registry written before this check must not let register copy the wrapper.
	a, err := NewApp(root)
	if err != nil {
		t.Fatal(err)
	}
	a.Out, a.Err = &bytes.Buffer{}, &bytes.Buffer{}
	base := filepath.Join(t.TempDir(), "base")
	if err = a.installLauncher(base, []string{parent}); err == nil || !strings.Contains(err.Error(), "agent-wrapper source") {
		t.Fatal(err)
	}
	if _, err = os.Stat(config.LauncherPath(base)); !os.IsNotExist(err) {
		t.Fatal("wrapper installed from workspace")
	}
}
func TestWorkspacesShareProviders(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	fake := t.TempDir()
	t.Setenv("PATH", fake+":"+os.Getenv("PATH"))
	b, _ := os.ReadFile("../../versions.lock.json")
	os.WriteFile(filepath.Join(root, "versions.lock.json"), b, 0600)
	os.Mkdir(filepath.Join(root, "bin"), 0755)
	os.WriteFile(filepath.Join(root, "bin", "agent-wrapper"), []byte("trusted executable"), 0700)
	os.WriteFile(filepath.Join(fake, "sbx"), []byte("#!/bin/sh\nif [ \"$1\" = ls ]; then echo \"[]\"; fi\n"), 0700)
	os.WriteFile(filepath.Join(fake, "paseo"), []byte("#!/bin/sh\nif [ \"$3\" = status ]; then echo '{\"localDaemon\":\"running\"}'; fi\n"), 0700)
	paseoHome := filepath.Join(home, ".paseo")
	os.MkdirAll(paseoHome, 0700)
	base, _ := config.BaseDir()
	os.MkdirAll(base, 0700)
	base, _ = config.Canonical(base)
	legacy := filepath.Join(base, "0123456789", "agent-wrapper")
	os.WriteFile(filepath.Join(paseoHome, "config.json"), []byte(`{"version":1,"agents":{"providers":{"custom":{"command":["/usr/bin/custom"]},"codex-ponytail":{"command":["`+legacy+`","codex-ponytail"]}}}}`), 0600)
	invoke := func(args ...string) string {
		t.Helper()
		var out, stderr bytes.Buffer
		if code := runLegacyFixture(root, args, &bytes.Buffer{}, &out, &stderr); code != 0 {
			t.Fatalf("%v: %d %s", args, code, stderr.String())
		}
		return out.String()
	}
	parent := t.TempDir()
	projects := map[string]string{"A": "codex-superpowers,claude-superpowers", "B": "codex-superpowers,claude-superpowers", "D": "codex-gsd-core,claude-gsd-core"}
	for _, name := range []string{"A", "B", "D"} {
		invoke("create", "--workspace", filepath.Join(parent, name), "--profiles", projects[name], "--utilities", "", "--apply")
	}
	providers := func() map[string]any {
		var current map[string]any
		config.ReadJSON(filepath.Join(paseoHome, "config.json"), &current)
		return current["agents"].(map[string]any)["providers"].(map[string]any)
	}
	got := providers()
	if len(got) != 5 || got["custom"] == nil || got["codex-ponytail"] != nil {
		t.Fatal(got)
	}
	for _, p := range []string{"codex-superpowers", "claude-superpowers", "codex-gsd-core", "claude-gsd-core"} {
		command := got[p].(map[string]any)["command"].([]any)
		if command[0] != config.LauncherPath(base) || command[1] != p {
			t.Fatalf("%s: %v", p, command)
		}
	}
	var rows []map[string]any
	json.Unmarshal([]byte(invoke("list", "--all")), &rows)
	if len(rows) != 3 {
		t.Fatal(rows)
	}
	// Superpowers stays while A remains; gsd-core disappears with D.
	invoke("unregister", "--workspace", filepath.Join(parent, "B"))
	invoke("unregister", "--workspace", filepath.Join(parent, "D"))
	got = providers()
	if len(got) != 3 || got["codex-superpowers"] == nil || got["codex-gsd-core"] != nil || got["custom"] == nil {
		t.Fatal(got)
	}
	if runLegacyFixture(root, []string{"create", "--workspace", filepath.Join(base, "inside"), "--profile", "codex-superpowers"}, &bytes.Buffer{}, &bytes.Buffer{}, &bytes.Buffer{}) == 0 {
		t.Fatal("workspace inside the trusted runtime accepted")
	}
}
func TestPruneForgetsDeletedWorkspaces(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	fake := t.TempDir()
	t.Setenv("PATH", fake+":"+os.Getenv("PATH"))
	b, _ := os.ReadFile("../../versions.lock.json")
	os.WriteFile(filepath.Join(root, "versions.lock.json"), b, 0600)
	os.Mkdir(filepath.Join(root, "bin"), 0755)
	os.WriteFile(filepath.Join(root, "bin", "agent-wrapper"), []byte("trusted executable"), 0700)
	sbxLog := filepath.Join(home, "sbx.log")
	os.WriteFile(filepath.Join(fake, "sbx"), []byte("#!/bin/sh\nif [ \"$1\" = ls ]; then echo \"[]\"; else echo \"$*\" >> \""+sbxLog+"\"; fi\n"), 0700)
	os.WriteFile(filepath.Join(fake, "paseo"), []byte("#!/bin/sh\nif [ \"$3\" = status ]; then echo '{\"localDaemon\":\"running\"}'; fi\n"), 0700)
	os.MkdirAll(filepath.Join(home, ".paseo"), 0700)
	invoke := func(args ...string) string {
		t.Helper()
		var out, stderr bytes.Buffer
		if code := runLegacyFixture(root, args, &bytes.Buffer{}, &out, &stderr); code != 0 {
			t.Fatalf("%v: %d %s", args, code, stderr.String())
		}
		return out.String()
	}
	parent := t.TempDir()
	keep, gone := filepath.Join(parent, "A"), filepath.Join(parent, "D")
	invoke("create", "--workspace", keep, "--profiles", "codex-superpowers", "--utilities", "", "--apply")
	invoke("create", "--workspace", gone, "--profiles", "codex-gsd-core", "--utilities", "", "--apply")
	os.RemoveAll(gone)
	var rows []map[string]any
	json.Unmarshal([]byte(invoke("list", "--all")), &rows)
	missing := 0
	for _, r := range rows {
		if r["missing"] == true {
			missing++
		}
	}
	if len(rows) != 2 || missing != 1 {
		t.Fatal(rows)
	}
	base, _ := config.BaseDir()
	registries := func() int {
		installed, _ := config.Registries(base)
		return len(installed)
	}
	out := invoke("prune", "--dry-run")
	if !strings.Contains(out, gone) || registries() != 2 {
		t.Fatalf("dry run: %s", out)
	}
	out = invoke("prune")
	if registries() != 1 || !strings.Contains(out, "sbx rm paseo-codex-gsd-core-") {
		t.Fatalf("prune: %s", out)
	}
	var current map[string]any
	config.ReadJSON(filepath.Join(home, ".paseo", "config.json"), &current)
	providers := current["agents"].(map[string]any)["providers"].(map[string]any)
	if len(providers) != 1 || providers["codex-superpowers"] == nil {
		t.Fatal(providers)
	}
	if logged, _ := os.ReadFile(sbxLog); strings.Contains(string(logged), "rm") {
		t.Fatalf("prune removed sandboxes: %s", logged)
	}
	if !strings.Contains(invoke("prune"), "No missing workspaces.") {
		t.Fatal("second prune")
	}
}
func TestCreateMismatchPreservesRegistry(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := t.TempDir()
	workspace, _ = filepath.EvalSymlinks(workspace)
	b, _ := os.ReadFile("../../versions.lock.json")
	os.WriteFile(filepath.Join(root, "versions.lock.json"), b, 0600)
	a, err := NewApp(root)
	if err != nil {
		t.Fatal(err)
	}
	runtime, _ := config.RuntimePath(workspace)
	reg := config.Registry{Workspace: workspace, Profiles: map[string]config.Entry{}}
	rows := []map[string]string{}
	for _, p := range config.Profiles {
		agent, tool, _ := strings.Cut(p, "-")
		e := config.Entry{Agent: agent, Tool: tool, Workspace: workspace, Sandbox: "paseo-" + p + "-" + filepath.Base(runtime), Image: "paseo-dock/" + p + ":2026.09.30"}
		rows = append(rows, map[string]string{"name": e.Sandbox})
		reg.Profiles[p] = e
	}
	e := reg.Profiles["claude-ponytail"]
	e.Image = "different/image:tag"
	reg.Profiles["claude-ponytail"] = e
	config.WriteJSON(filepath.Join(runtime, "registry.json"), reg)
	before, _ := os.ReadFile(filepath.Join(runtime, "registry.json"))
	fake := t.TempDir()
	t.Setenv("PATH", fake+":"+os.Getenv("PATH"))
	list, _ := json.Marshal(rows)
	os.WriteFile(filepath.Join(fake, "sbx"), []byte("#!/bin/sh\nprintf '%s\\n' '"+string(list)+"'\n"), 0700)
	a.Out = &bytes.Buffer{}
	a.Err = &bytes.Buffer{}
	if err = a.create(workspace, "paseo-dock", "2g"); err == nil {
		t.Fatal("mismatch accepted")
	}
	after, _ := os.ReadFile(filepath.Join(runtime, "registry.json"))
	if !bytes.Equal(before, after) {
		t.Fatal("registry truncated after failed create")
	}
}
