package launcher

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"paseo-dock/internal/config"
	"paseo-dock/internal/operationlock"
)

func bundleRegistry(t *testing.T) (string, config.Registry, config.Entry) {
	t.Helper()
	workspace, err := config.Canonical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runtime := t.TempDir()
	entry := config.Entry{Category: config.CategoryContainer, Name: "dev", Pairs: []config.Pair{{Agent: "claude", Methodology: "superpowers"}, {Agent: "codex", Methodology: "superpowers"}}, Workspace: workspace, ContainerID: strings.Repeat("c", 64), ImageID: "sha256:img", Image: "paseo-dock/container/dev:tag", Network: "bridge"}
	reg := config.Registry{Workspace: workspace, Profiles: map[string]config.Entry{entry.Key(): entry}, Active: []string{entry.Key()}}
	if err := config.WriteJSON(filepath.Join(runtime, "registry.json"), reg); err != nil {
		t.Fatal(err)
	}
	return runtime, reg, entry
}

func TestProviderEntryResolvesBundleAgent(t *testing.T) {
	runtime, reg, entry := bundleRegistry(t)
	key, stored, agent, err := providerEntry(reg, "container-dev-claude")
	if err != nil || key != "container/dev" || agent != "claude" || !stored.EqualIdentity(entry) {
		t.Fatalf("%s %s %v", key, agent, err)
	}
	if _, _, _, err := providerEntry(reg, "container-dev-antigravity-cli"); err == nil {
		t.Fatal("resolved an agent outside the bundle")
	}
	// The per-agent view validates against the stored bundle identity.
	if err := validateEntry(runtime, key, stored.ForAgent(agent)); err != nil {
		t.Fatal(err)
	}
	tampered := reg
	tampered.Profiles = map[string]config.Entry{}
	bad := entry
	bad.Pairs = []config.Pair{{Agent: "codex", Methodology: "unknown"}}
	tampered.Profiles[bad.Key()] = bad
	if _, _, _, err := providerEntry(tampered, "container-dev-codex"); err == nil {
		t.Fatal("accepted invalid bundle")
	}
}

func TestAgentArgv(t *testing.T) {
	entry := config.Entry{Category: config.CategoryContainer, Name: "dev", Pairs: []config.Pair{{Agent: "codex", Methodology: "superpowers"}}}
	argv, err := agentArgv(entry.ForAgent("codex"), []string{"app-server"})
	if err != nil || strings.Join(argv, " ") != "codex app-server" {
		t.Fatal(argv, err)
	}
	entry.Connections = map[string]string{"codex": config.ConnectionACP}
	if argv, err := agentArgv(entry.ForAgent("codex"), []string{"--acp"}); err != nil || strings.Join(argv, " ") != "env CODEX_PATH=/opt/paseo-dock/runtime/bin/codex codex-acp" {
		t.Fatal(argv, err)
	}
	gjc := config.Entry{Category: config.CategoryContainer, Agent: "gjc", Tool: "native"}
	if argv, err := agentArgv(gjc, []string{"--acp"}); err != nil || strings.Join(argv, " ") != "env GJC_ACP_PERMISSION_MODE=prompt gjc acp" {
		t.Fatal(argv, err)
	}
	if _, err := agentArgv(entry.ForAgent("codex"), []string{"app-server"}); err == nil {
		t.Fatal("ACP provider accepted non-ACP launch")
	}
}

func TestStartupLockWaitsForOtherAgent(t *testing.T) {
	runtime := t.TempDir()
	key := "container:" + strings.Repeat("c", 64)
	release, err := operationlock.Acquire(context.Background(), runtime, []string{key})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(300 * time.Millisecond)
		release()
	}()
	start := time.Now()
	second, err := acquireStartup(context.Background(), runtime, key)
	if err != nil {
		t.Fatal(err)
	}
	second()
	if time.Since(start) < 200*time.Millisecond {
		t.Fatal("lock was not held")
	}
	held, err := operationlock.Acquire(context.Background(), runtime, []string{key})
	if err != nil {
		t.Fatal(err)
	}
	defer held()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err := acquireStartup(ctx, runtime, key); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}
