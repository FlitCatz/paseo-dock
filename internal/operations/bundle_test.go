package operations

import (
	"context"
	"io"
	"strings"
	"testing"

	"paseo-dock/internal/config"
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
	targets["container-dev-acp"] = ProviderTarget{ID: "container-dev-acp", Agent: "codex", Mode: config.ConnectionACP, Label: "Dock dev · codex"}
	generated := ProviderConfigTargets("/trusted/launcher", targets)
	providers := object(object(generated, "agents"), "providers")
	native := providers["container-dev-codex"].(map[string]any)
	acp := providers["container-dev-acp"].(map[string]any)
	if native["extends"] != "codex" || len(native["command"].([]string)) != 2 || native["label"] != "Dock dev · codex" {
		t.Fatal(native)
	}
	if command := acp["command"].([]string); acp["extends"] != "acp" || len(command) != 3 || command[2] != "--acp" {
		t.Fatal(acp)
	}
}

func TestMergeRetiresOwnedACPProviderOfAnyAgent(t *testing.T) {
	base := "/trusted/paseo-dock"
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
	replies map[string]string
	probed  []string
}

func (b *acpBackend) Exec(ctx context.Context, e config.Entry, args []string, in io.Reader, out, stderr io.Writer) error {
	if in == nil {
		return b.categoryBackend.Exec(ctx, e, args, in, out, stderr)
	}
	b.probed = append(b.probed, e.Agent+": "+strings.Join(args, " "))
	reply := b.replies[e.Agent]
	request, _ := io.ReadAll(in)
	if !strings.Contains(string(request), `"method":"initialize"`) {
		return io.ErrUnexpectedEOF
	}
	_, err := io.WriteString(out, reply)
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
	valid := `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":1}}` + "\n"
	b := &acpBackend{replies: map[string]string{"codex": valid}}
	bundle := config.Entry{Category: config.CategoryContainer, Name: "dev", Pairs: []config.Pair{{Agent: "claude", Methodology: "superpowers"}, {Agent: "codex", Methodology: "superpowers"}, {Agent: "gjc", Methodology: "native"}}, Connections: map[string]string{"codex": config.ConnectionACP}}
	reg := config.Registry{Profiles: map[string]config.Entry{bundle.Key(): bundle}, Active: []string{bundle.Key()}}
	// gjc (acp by default) fails; codex passes; claude is native and never probed.
	err := a.recordACP(b, &reg, bundle.Key(), bundle)
	if err == nil || !strings.Contains(err.Error(), "ACP check failed for gjc in container/dev; its Paseo provider container-dev-gjc stays disabled") || strings.Contains(err.Error(), "codex") {
		t.Fatal(err)
	}
	if strings.Join(b.probed, "\n") != "codex: env CODEX_PATH=/opt/paseo-dock/runtime/bin/codex codex-acp\ngjc: env GJC_ACP_PERMISSION_MODE=prompt gjc acp" {
		t.Fatal(b.probed)
	}
	if strings.Join(reg.DisabledProviders, ",") != "container-dev-gjc" {
		t.Fatal(reg.DisabledProviders)
	}
	targets := ActiveProviders([]config.Installed{{Registry: reg}})
	if len(targets) != 2 || targets["container-dev-codex"].Mode != config.ConnectionACP || targets["container-dev-claude"].Mode != config.ConnectionNative {
		t.Fatal(targets)
	}
	// A later passing check re-enables the provider.
	b.replies["gjc"] = valid
	if err := a.recordACP(b, &reg, bundle.Key(), bundle); err != nil || reg.DisabledProviders != nil {
		t.Fatal(err, reg.DisabledProviders)
	}
}

func TestCreateApplyDisablesOnlyFailedACPProvider(t *testing.T) {
	s, r, path := restoreFixture(t)
	b := &acpBackend{replies: map[string]string{}}
	s.Backends = map[config.Category]Backend{config.CategoryContainer: b}
	selection := config.Selection{Schema: 2, Category: config.CategoryContainer, Pairs: []config.Pair{{Agent: "codex", Methodology: "superpowers"}, {Agent: "gjc", Methodology: "native"}}}
	selection.UseBundle()
	_, err := s.Execute(context.Background(), Request{Kind: KindCreate, Target: Target{Workspace: r.Target.Workspace}, Selection: selection, Apply: true, PaseoHome: r.PaseoHome}, nil)
	if err == nil || !strings.Contains(err.Error(), "ACP check failed for gjc") {
		t.Fatal(err)
	}
	var reg config.Registry
	if err := config.ReadJSON(path, &reg); err != nil {
		t.Fatal(err)
	}
	key := "container/codex-gjc-native-superpowers"
	if !reg.IsActive(key) || strings.Join(reg.DisabledProviders, ",") != "container-codex-gjc-native-superpowers-gjc" {
		t.Fatal(reg.Active, reg.DisabledProviders)
	}
	providers, err := readProviders(r.PaseoHome)
	if err != nil {
		t.Fatal(err)
	}
	if providers["container-codex-gjc-native-superpowers-codex"] == nil || providers["container-codex-gjc-native-superpowers-gjc"] != nil {
		t.Fatalf("providers=%v", providers)
	}
}

func TestRepeatedCreateKeepsEarlierContainersActive(t *testing.T) {
	s, r, path := restoreFixture(t)
	b := &categoryBackend{}
	s.Backends = map[config.Category]Backend{config.CategoryContainer: b}
	first := config.Selection{Schema: 2, Category: config.CategoryContainer, Pairs: []config.Pair{{Agent: "codex", Methodology: "superpowers"}}}
	second := config.Selection{Schema: 2, Category: config.CategoryContainer, Name: "review", Pairs: []config.Pair{{Agent: "claude", Methodology: "ponytail"}, {Agent: "codex", Methodology: "ponytail"}}}
	for _, selection := range []config.Selection{first, second} {
		if _, err := s.Execute(context.Background(), Request{Kind: KindCreate, Target: Target{Workspace: r.Target.Workspace}, Selection: selection, Apply: true, PaseoHome: r.PaseoHome}, nil); err != nil {
			t.Fatal(err)
		}
	}
	var reg config.Registry
	if err := config.ReadJSON(path, &reg); err != nil {
		t.Fatal(err)
	}
	if !reg.IsActive("container/codex-superpowers") || !reg.IsActive("container/review") || reg.Selection.Name != "review" {
		t.Fatal(reg.Active)
	}
	providers, err := readProviders(r.PaseoHome)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"container-codex-superpowers", "container-review-claude", "container-review-codex"} {
		if providers[id] == nil {
			t.Fatalf("missing %s: %v", id, providers)
		}
	}
}

type versionsBackend struct{ categoryBackend }

func (b *versionsBackend) Exec(ctx context.Context, e config.Entry, args []string, in io.Reader, out, stderr io.Writer) error {
	switch command := strings.Join(args, " "); command {
	case "cat /home/agent/.local/share/paseo-dock/installed.json":
		b.calls = append(b.calls, command)
		_, err := io.WriteString(out, `{"pairs":[{"agent":"omp","methodology":"native"},{"agent":"claude","methodology":"openspec"},{"agent":"codex","methodology":"openspec"}],"lock":{"tools":{"openspec":{"package":"@fission-ai/openspec"}}}}`)
		return err
	case "claude --version", "omp --version", "openspec --version":
		b.calls = append(b.calls, command)
		_, err := io.WriteString(out, args[0]+" 1.0.0")
		return err
	}
	return b.categoryBackend.Exec(ctx, e, args, in, out, stderr)
}

func TestBundleVersionsProbeEveryAgentAndMethodology(t *testing.T) {
	s, target, _ := categoryFixture(t, config.CategoryContainer)
	b := &versionsBackend{}
	s.Backends = map[config.Category]Backend{config.CategoryContainer: b}
	target.Entry.Agent, target.Entry.Tool = "", ""
	target.Entry.Name = "dev"
	target.Entry.Pairs = []config.Pair{{Agent: "claude", Methodology: "openspec"}, {Agent: "codex", Methodology: "openspec"}, {Agent: "omp", Methodology: "native"}}
	target.Profile = target.Entry.Key()
	writeTarget(t, target)
	result, err := s.Execute(context.Background(), Request{Kind: KindVersions, Target: target}, nil)
	if err != nil {
		t.Fatalf("err=%v calls=%v", err, b.calls)
	}
	want := map[string]string{"agent:claude": "claude 1.0.0", "agent:codex": "codex 0.159.2", "agent:omp": "omp 1.0.0", "methodology:openspec": "openspec 1.0.0"}
	for id, version := range want {
		if result.Versions[id] != version {
			t.Fatalf("%s: %v", id, result.Versions)
		}
	}
	if strings.Count(strings.Join(b.calls, "\n"), "openspec --version") != 1 {
		t.Fatal(b.calls)
	}
}
