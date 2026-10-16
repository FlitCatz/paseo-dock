package operations

import (
	"context"
	"errors"
	"io"
	"os"
	"paseo-dock/internal/config"
	"paseo-dock/internal/operationlock"
	"path/filepath"
	"strings"
	"testing"
)

type unreachableRelayBackend struct{ *categoryBackend }

func (b *unreachableRelayBackend) Exec(context.Context, config.Entry, []string, io.Reader, io.Writer, io.Writer) error {
	return errors.New("bridge relay unreachable")
}
func TestDockerRelayUnreachableRefusesRegistration(t *testing.T) {
	s, target, b := categoryFixture(t, config.CategoryContainer)
	s.Backends[target.Category] = &unreachableRelayBackend{b}
	home := t.TempDir()
	before, _ := os.ReadFile(filepath.Join(target.Runtime, "registry.json"))
	_, err := s.Execute(context.Background(), Request{Kind: KindRegister, Target: target, Targets: []Target{target}, PaseoHome: home, Apply: true}, nil)
	if err == nil || !strings.Contains(err.Error(), "bridge relay unreachable") {
		t.Fatalf("registration did not check Docker reachability: %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(target.Runtime, "registry.json"))
	if string(before) != string(after) {
		t.Fatal("failed relay changed activation")
	}
	if _, err := os.Stat(filepath.Join(home, "config.json")); !os.IsNotExist(err) {
		t.Fatal("failed relay wrote config")
	}
	base, _ := config.BaseDir()
	if _, err := os.Stat(config.LauncherPath(base)); !os.IsNotExist(err) {
		t.Fatal("failed relay installed launcher")
	}
}
func TestProviderUnionRetainsUserEntries(t *testing.T) {
	user := map[string]any{"command": []string{"user-agent"}, "enabled": true}
	current := map[string]any{"agents": map[string]any{"providers": map[string]any{"user": user}}}
	generated := ProviderConfig("/trusted/launcher", map[string]string{"sbx-codex": "codex", "container-codex-superpowers": "codex", "codex-superpowers": "codex"})
	MergeManagedConfig(current, generated, "/trusted")
	providers := object(object(current, "agents"), "providers")
	if len(providers) != 4 || providers["user"].(map[string]any)["enabled"] != true {
		t.Fatalf("provider union %v", providers)
	}
}
func TestDockerRelayCreateFailureKeepsCIDAndPriorActivation(t *testing.T) {
	s, r, path := restoreFixture(t)
	var prior config.Registry
	if err := config.ReadJSON(path, &prior); err != nil {
		t.Fatal(err)
	}
	s.Backends = map[config.Category]Backend{config.CategoryContainer: &unreachableRelayBackend{&categoryBackend{}}}
	selection := config.Selection{Schema: 2, Category: config.CategoryContainer, Pairs: []config.Pair{{Agent: "codex", Methodology: "superpowers"}}}
	result, err := s.Execute(context.Background(), Request{Kind: KindCreate, Target: Target{Workspace: r.Target.Workspace}, Selection: selection, Apply: true, PaseoHome: t.TempDir()}, nil)
	if err == nil || !strings.Contains(err.Error(), "bridge relay unreachable") {
		t.Fatalf("missing pre-activation check: %v", err)
	}
	var after config.Registry
	if err := config.ReadJSON(path, &after); err != nil {
		t.Fatal(err)
	}
	if after.Profiles["container/codex-superpowers"].ContainerID == "" || after.IsActive("container/codex-superpowers") {
		t.Fatal("identity lost or failed target activated", after)
	}
	for _, key := range prior.ActiveProfiles() {
		if !after.IsActive(key) {
			t.Fatal("prior activation lost", key)
		}
	}
	if !result.NeedsVerification || len(result.Remaining) == 0 {
		t.Fatal("partial resource omitted", result)
	}
}
func TestDockerRelayProbeSharesEntryLock(t *testing.T) {
	s, target, b := categoryFixture(t, config.CategoryContainer)
	release, err := operationlock.Acquire(context.Background(), target.Runtime, []string{operationlock.EntryKey(target.Entry)})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	a := s.execution(context.Background(), nil, nil)
	reg := config.Registry{Workspace: target.Workspace, Profiles: map[string]config.Entry{target.Profile: target.Entry}, Active: []string{target.Profile}}
	err = a.checkDockerRelays([]config.Installed{{Runtime: target.Runtime, Registry: reg}})
	if !errors.Is(err, operationlock.ErrConflict) || len(b.calls) != 0 {
		t.Fatalf("probe ignored category lock: %v %v", err, b.calls)
	}
}
func TestNativeCategoryProviderKeepsLegacyPluginAndUserEntries(t *testing.T) {
	generated := ProviderConfig(config.LauncherPath("/trusted"), map[string]string{"antigravity-cli-native": "antigravity-cli", "container-antigravity-cli-native": "antigravity-cli"})
	providers := object(object(generated, "agents"), "providers")
	if _, ok := providers["antigravity-cli-native"]; ok {
		t.Fatal("legacy plugin provider replaced")
	}
	native, ok := providers["container-antigravity-cli-native"].(map[string]any)
	if !ok {
		t.Fatal("category native provider missing")
	}
	args := native["command"].([]string)
	if len(args) != 3 || args[2] != "--acp" {
		t.Fatal(args)
	}
	current := map[string]any{"agents": map[string]any{"providers": map[string]any{"user": map[string]any{"extends": "antigravity-cli", "command": []string{"user-wrapper", "container-antigravity-cli-native", "--acp"}}, "container-antigravity-cli-native": native}}}
	MergeManagedConfig(current, ProviderConfig(config.LauncherPath("/trusted"), nil), "/trusted")
	remaining := object(object(current, "agents"), "providers")
	if len(remaining) != 1 || remaining["user"] == nil {
		t.Fatal(remaining)
	}
}
func TestUnregisterRepairsWithoutProbingRemainingDockerRelay(t *testing.T) {
	s, targets, home := providerScopeFixture(t)
	workspace, _ := config.Canonical(t.TempDir())
	runtime, _ := config.RuntimePath(workspace)
	e := config.Entry{Category: config.CategoryContainer, Agent: "codex", Tool: "superpowers", Workspace: workspace, ContainerID: strings.Repeat("a", 64), Image: "managed", ImageID: "sha256:image", Network: "bridge"}
	reg := config.Registry{Workspace: workspace, Profiles: map[string]config.Entry{e.Key(): e}, Active: []string{e.Key()}}
	if err := config.WriteJSON(filepath.Join(runtime, "registry.json"), reg); err != nil {
		t.Fatal(err)
	}
	s.Backends = map[config.Category]Backend{config.CategoryContainer: &unreachableRelayBackend{&categoryBackend{}}}
	_, err := s.Execute(context.Background(), Request{Kind: KindUnregister, Target: Target{Workspace: targets[0].Workspace, Runtime: targets[0].Runtime}, Targets: targets, PaseoHome: home}, nil)
	if err != nil {
		t.Fatalf("repair blocked by unrelated unreachable relay: %v", err)
	}
	providers, err := readProviders(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(providers) != 2 || providers["container-codex-superpowers"] == nil || providers["user-provider"] == nil {
		t.Fatal(providers)
	}
	var after config.Registry
	if err := config.ReadJSON(filepath.Join(runtime, "registry.json"), &after); err != nil || !after.IsActive(e.Key()) {
		t.Fatal("remaining activation changed", after, err)
	}
}

// D1: Docker SBX sandboxes keep the "SBX" label; containers are labelled "Dock".
func TestProviderLabelsFollowCategory(t *testing.T) {
	sandbox := ProviderTargets("sandbox/codex", config.Entry{Category: config.CategorySandbox, Agent: "codex"})
	legacy := ProviderTargets("codex-superpowers", config.Entry{Agent: "codex", Tool: "superpowers"})
	container := ProviderTargets("container/codex/superpowers", config.Entry{Category: config.CategoryContainer, Agent: "codex", Tool: "superpowers"})
	bundle := ProviderTargets("container/bundle/dev", config.Entry{Category: config.CategoryContainer, Name: "dev", Pairs: []config.Pair{{Agent: "codex", Methodology: "superpowers"}}})
	for got, want := range map[string]string{sandbox[0].Label: "SBX sbx-codex", legacy[0].Label: "SBX codex-superpowers", container[0].Label: "Dock container-codex-superpowers", bundle[0].Label: "Dock dev · codex"} {
		if got != want {
			t.Fatalf("label %q, want %q", got, want)
		}
	}
	generated := object(object(ProviderConfig("/trusted/launcher", map[string]string{"sbx-codex": "codex", "container-codex-superpowers": "codex"}), "agents"), "providers")
	if generated["sbx-codex"].(map[string]any)["label"] != "SBX sbx-codex" || generated["container-codex-superpowers"].(map[string]any)["label"] != "Dock container-codex-superpowers" {
		t.Fatalf("generated labels %v", generated)
	}
}
