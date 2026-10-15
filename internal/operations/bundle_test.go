package operations

import (
	"context"
	"io"
	"strings"
	"testing"

	"paseo-sbx/internal/config"
)

func TestBundleCreateMakesOneContainer(t *testing.T) {
	s, r, path := restoreFixture(t)
	b := &categoryBackend{}
	s.Backends = map[config.Category]Backend{config.CategoryContainer: b}
	selection := config.Selection{Schema: 2, Category: config.CategoryContainer, Pairs: []config.Pair{{Agent: "codex", Methodology: "openspec"}, {Agent: "claude", Methodology: "openspec"}, {Agent: "codex", Methodology: "superpowers"}}, Connections: map[string]string{"claude": config.ConnectionNone}}
	if _, err := s.Execute(context.Background(), Request{Kind: KindCreate, Target: Target{Workspace: r.Target.Workspace}, Selection: selection}, nil); err != nil {
		t.Fatal(err)
	}
	creates, openspec := 0, []string{}
	for _, call := range b.calls {
		if call == "create" {
			creates++
		}
		if strings.Contains(call, "openspec init") {
			openspec = append(openspec, call)
		}
	}
	if creates != 1 || len(openspec) != 1 || !strings.Contains(openspec[0], "--tools claude,codex ") {
		t.Fatalf("calls=%q", b.calls)
	}
	var reg config.Registry
	if err := config.ReadJSON(path, &reg); err != nil {
		t.Fatal(err)
	}
	key := "container/claude-codex-openspec-superpowers"
	e, ok := reg.Profiles[key]
	if !ok || !reg.IsActive(key) || e.Agent != "" || len(e.Pairs) != 3 || e.Connections["claude"] != config.ConnectionNone || e.ContainerID == "" || !strings.Contains(e.Image, "/"+key+":") {
		t.Fatalf("bundle entry: %+v", e)
	}
	for _, separate := range []string{"container/codex-openspec", "container/claude-openspec", "container/codex-superpowers"} {
		if _, ok := reg.Profiles[separate]; ok {
			t.Fatal("created a separate entry", separate)
		}
	}
}

func TestBundleProvidersFollowConnections(t *testing.T) {
	bundle := config.Entry{Category: config.CategoryContainer, Name: "dev", Pairs: []config.Pair{{Agent: "antigravity-cli", Methodology: "native"}, {Agent: "claude", Methodology: "superpowers"}, {Agent: "codex", Methodology: "superpowers"}}, Connections: map[string]string{"claude": config.ConnectionNone}}
	legacy := config.Entry{Category: config.CategoryContainer, Agent: "antigravity-cli", Tool: "native"}
	installed := []config.Installed{{Registry: config.Registry{Profiles: map[string]config.Entry{bundle.Key(): bundle, legacy.Key(): legacy}, Active: []string{bundle.Key(), legacy.Key()}}}}
	targets := ActiveProviders(installed)
	if len(targets) != 1 || targets["container-dev-codex"].Agent != "codex" || targets["container-dev-codex"].Mode != config.ConnectionNative {
		t.Fatalf("targets=%+v", targets)
	}
	// Every agent keeps a launcher ID, even when it has no Paseo provider.
	ids := []string{}
	for _, target := range ProviderTargets(bundle.Key(), bundle) {
		ids = append(ids, target.ID)
	}
	if strings.Join(ids, ",") != "container-dev-antigravity-cli,container-dev-claude,container-dev-codex" {
		t.Fatal(ids)
	}
	targets["container-dev-acp"] = ProviderTarget{ID: "container-dev-acp", Agent: "codex", Mode: config.ConnectionACP, Label: "SBX dev · codex"}
	generated := ProviderConfigTargets("/trusted/launcher", targets)
	providers := object(object(generated, "agents"), "providers")
	native := providers["container-dev-codex"].(map[string]any)
	acp := providers["container-dev-acp"].(map[string]any)
	if native["extends"] != "codex" || len(native["command"].([]string)) != 2 || native["label"] != "SBX dev · codex" {
		t.Fatal(native)
	}
	if command := acp["command"].([]string); acp["extends"] != "acp" || len(command) != 3 || command[2] != "--acp" {
		t.Fatal(acp)
	}
}

func TestMergeRetiresOwnedACPProviderOfAnyAgent(t *testing.T) {
	base := "/trusted/paseo-sbx"
	stale := "container-dev-codex"
	providers := map[string]any{stale: map[string]any{"command": []any{config.LauncherPath(base), stale, "--acp"}}, "user": map[string]any{"command": []any{"/external/agent-wrapper", "user", "--acp"}}}
	current := map[string]any{"agents": map[string]any{"providers": providers}}
	MergeManagedConfig(current, ProviderConfigTargets(config.LauncherPath(base), nil), base)
	if _, ok := providers[stale]; ok {
		t.Fatal("stale owned ACP provider retained")
	}
	if _, ok := providers["user"]; !ok {
		t.Fatal("external provider removed")
	}
}

func TestBundleRegistrationNeedsEveryConnectedAgent(t *testing.T) {
	base := "/trusted"
	bundle := config.Entry{Category: config.CategoryContainer, Name: "dev", Pairs: []config.Pair{{Agent: "claude", Methodology: "superpowers"}, {Agent: "codex", Methodology: "superpowers"}}}
	provider := func(id, agent string) map[string]any {
		return map[string]any{"enabled": true, "extends": agent, "command": []any{config.LauncherPath(base), id}}
	}
	providers := map[string]any{"container-dev-codex": provider("container-dev-codex", "codex")}
	if got := entryRegistration(providers, nil, base, bundle.Key(), bundle); got != "unregistered" {
		t.Fatal(got)
	}
	providers["container-dev-claude"] = provider("container-dev-claude", "claude")
	if got := entryRegistration(providers, nil, base, bundle.Key(), bundle); got != "registered" {
		t.Fatal(got)
	}
	bundle.Connections = map[string]string{"claude": config.ConnectionNone, "codex": config.ConnectionNone}
	if got := entryRegistration(providers, nil, base, bundle.Key(), bundle); got != "unmanaged" {
		t.Fatal(got)
	}
}

type acpBackend struct {
	categoryBackend
	reply string
	argv  []string
}

func (b *acpBackend) Exec(_ context.Context, _ config.Entry, args []string, in io.Reader, out, _ io.Writer) error {
	b.argv = args
	request, _ := io.ReadAll(in)
	if !strings.Contains(string(request), `"method":"initialize"`) {
		return io.ErrUnexpectedEOF
	}
	_, err := io.WriteString(out, b.reply)
	return err
}

func TestACPCheck(t *testing.T) {
	for _, tc := range []struct {
		reply string
		ok    bool
	}{
		{`{"jsonrpc":"2.0","method":"log"}` + "\n" + `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":1,"agentCapabilities":{}}}` + "\n", true},
		{`{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"no"}}` + "\n", false},
		{`{"jsonrpc":"2.0","id":2,"result":{"protocolVersion":1}}` + "\n", false},
		{"Usage: agent [options]\n", false},
	} {
		if acpInitialized([]byte(tc.reply)) != tc.ok {
			t.Fatalf("%q: want %v", tc.reply, tc.ok)
		}
	}
	s := &Service{}
	a := s.execution(context.Background(), nil, nil)
	b := &acpBackend{}
	native := config.Entry{Category: config.CategoryContainer, Name: "dev", Pairs: []config.Pair{{Agent: "codex", Methodology: "superpowers"}}}
	if err := a.checkACP(b, native.Key(), native); err != nil || b.argv != nil {
		t.Fatalf("native agent probed: %v %v", err, b.argv)
	}
	// No ACP server is pinned yet, so an ACP connection cannot pass the check.
	acp := native
	acp.Connections = map[string]string{"codex": config.ConnectionACP}
	if err := a.checkACP(b, acp.Key(), acp); err == nil || !strings.Contains(err.Error(), "no ACP server is pinned for codex") {
		t.Fatal(err)
	}
}
