package operations

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"paseo-sbx/internal/config"
	"paseo-sbx/internal/operationlock"
	"paseo-sbx/internal/sandboxupdate"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type categoryBackend struct {
	calls              []string
	rows               []RuntimeState
	listErr, createErr error
	cancel             context.CancelFunc
	onCreate           func()
}

func (b *categoryBackend) List(context.Context) ([]RuntimeState, error) {
	b.calls = append(b.calls, "list")
	return b.rows, b.listErr
}
func (b *categoryBackend) Inspect(context.Context, config.Entry) (RuntimeState, error) {
	return RuntimeState{Known: true, Running: true}, nil
}
func (b *categoryBackend) Create(_ context.Context, e config.Entry) (config.Entry, error) {
	if b.onCreate != nil {
		b.onCreate()
	}
	b.calls = append(b.calls, "create")
	if e.Category == config.CategoryContainer {
		e.ContainerID = strings.Repeat("b", 64)
		e.ImageID = "sha256:created"
	}
	return e, b.createErr
}
func (b *categoryBackend) Stop(_ context.Context, e config.Entry) error {
	b.calls = append(b.calls, "stop:"+e.Key())
	return nil
}
func (b *categoryBackend) Exec(_ context.Context, e config.Entry, args []string, _ io.Reader, out, stderr io.Writer) error {
	command := strings.Join(args, " ")
	b.calls = append(b.calls, command)
	if len(args) > 0 && args[0] == "curl" {
		res, err := http.Get(strings.Replace(args[len(args)-1], "host.docker.internal", "127.0.0.1", 1))
		if err != nil {
			return err
		}
		defer res.Body.Close()
		_, err = io.Copy(out, res.Body)
		return err
	}
	if b.cancel != nil {
		b.cancel()
		return context.Canceled
	}
	switch strings.TrimPrefix(command, "env -C /tmp ") {
	case "printenv HOME":
		fmt.Fprint(out, "/home/agent")
	case "npm view @openai/codex@latest version":
		fmt.Fprint(out, "1.2.3")
	case "codex --version":
		fmt.Fprint(out, "codex 1.2.3")
	case "rtk --version":
		fmt.Fprint(out, "rtk 0.1.0")
	case "cat /home/agent/.local/share/paseo-sbx/installed.json":
		fmt.Fprint(out, `{"agent":"codex","tool":"native","lock":{"tools":{}}}`)
	case "cat /home/agent/.local/share/paseo-sbx/utilities/installed.json":
		fmt.Fprint(out, `{"agent":"codex","utilities":{"rtk":{"installer":"rtk"}}}`)
	case "uname -m":
		return errors.New("RTK stage reached")
	}
	return nil
}
func categoryFixture(t *testing.T, c config.Category) (*Service, Target, *categoryBackend) {
	t.Helper()
	s, target, _ := operationFixture(t)
	target.Category = c
	target.Entry.Category = c
	if c == config.CategorySandbox {
		target.Entry.Tool = ""
	}
	if c == config.CategoryContainer {
		target.Entry.Sandbox = ""
		target.Entry.ContainerID = strings.Repeat("a", 64)
		target.Entry.ImageID = "sha256:img"
		target.Entry.Network = "bridge"
	}
	target.Profile = target.Entry.Key()
	writeTarget(t, target)
	b := &categoryBackend{}
	s.Backends = map[config.Category]Backend{c: b}
	return s, target, b
}
func TestDispatchUsesExactCategory(t *testing.T) {
	for _, c := range []config.Category{config.CategorySandbox, config.CategoryContainer, config.CategoryLegacySandbox} {
		t.Run(string(c), func(t *testing.T) {
			s, target, b := categoryFixture(t, c)
			result, err := s.Execute(context.Background(), Request{Kind: KindStop, Target: target}, nil)
			if err != nil || !result.Changed || !slices.Equal(b.calls, []string{"stop:" + target.Profile}) {
				t.Fatalf("result=%+v err=%v calls=%v", result, err, b.calls)
			}
		})
	}
}
func TestSnapshotIsolatesDockerFailure(t *testing.T) {
	s, target, b := categoryFixture(t, config.CategorySandbox)
	b.rows = []RuntimeState{{ID: target.Entry.Sandbox, Known: true, Running: true}}
	s.Backends[config.CategoryContainer] = &categoryBackend{listErr: errors.New("daemon offline")}
	snap, err := s.Snapshot(context.Background(), Query{IncludeDocker: true})
	if err != nil || snap.SBX.Error != "" || !strings.Contains(snap.Docker.Error, "daemon offline") || len(snap.Workspaces) != 1 || snap.Workspaces[0].Sandboxes[0].RuntimeState != "running" {
		t.Fatalf("snap=%+v err=%v", snap, err)
	}
}
func TestLegacyStopWithRemovedUtility(t *testing.T) {
	s, target, b := categoryFixture(t, config.CategoryLegacySandbox)
	target.Entry.Utilities = []string{"removed"}
	writeTarget(t, target)
	s.Catalog = config.Catalog{}
	if _, err := s.Execute(context.Background(), Request{Kind: KindStop, Target: target}, nil); err != nil || len(b.calls) != 1 {
		t.Fatalf("err=%v calls=%v", err, b.calls)
	}
}
func TestNewSandboxUpdateTouchesAgentOnly(t *testing.T) {
	s, target, b := categoryFixture(t, config.CategorySandbox)
	result, err := s.Execute(context.Background(), Request{Kind: KindUpdate, Target: target}, nil)
	if err != nil || !result.Changed {
		t.Fatalf("%+v %v", result, err)
	}
	want := []string{"printenv HOME", "mkdir -p /home/agent/.local/share/paseo-sbx/updates /home/agent/.local/bin", "npm view @openai/codex@latest version", "codex --version"}
	if !slices.Equal(b.calls, want) {
		t.Fatalf("calls %v", b.calls)
	}
	var state sandboxupdate.State
	if err := config.ReadJSON(observationPath(target.Runtime, "updates", target.Entry.Sandbox), &state); err != nil || state.Success.IsZero() || len(state.Versions) != 1 || state.Versions["agent"] != "codex 1.2.3" {
		t.Fatalf("state=%+v err=%v", state, err)
	}
}
func TestContainerUpdateKeepsStackStages(t *testing.T) {
	s, target, b := categoryFixture(t, config.CategoryContainer)
	_, err := s.Execute(context.Background(), Request{Kind: KindUpdate, Target: target}, nil)
	if err == nil || !slices.Contains(b.calls, "env -C /tmp cat /home/agent/.local/share/paseo-sbx/installed.json") || !slices.Contains(b.calls, "env -C /tmp uname -m") {
		t.Fatalf("err=%v calls=%v", err, b.calls)
	}
}
func TestCancellationMarksGuestUncertain(t *testing.T) {
	s, target, b := categoryFixture(t, config.CategorySandbox)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b.cancel = cancel
	result, err := s.Execute(ctx, Request{Kind: KindUpdate, Target: target}, nil)
	if !errors.Is(err, context.Canceled) || !result.NeedsVerification || !result.Changed || len(result.Remaining) != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	var state sandboxupdate.State
	if err := config.ReadJSON(observationPath(target.Runtime, "updates", target.Entry.Sandbox), &state); err != nil || len(state.Errors) == 0 || !state.Success.IsZero() {
		t.Fatalf("state=%+v err=%v", state, err)
	}
}

func TestCategoryVersionsUseSelectedTransport(t *testing.T) {
	for _, c := range []config.Category{config.CategorySandbox, config.CategoryContainer} {
		t.Run(string(c), func(t *testing.T) {
			s, target, b := categoryFixture(t, c)
			result, err := s.Execute(context.Background(), Request{Kind: KindVersions, Target: target}, nil)
			if err != nil || result.Versions["agent"] != "codex 1.2.3" {
				t.Fatalf("result=%+v err=%v calls=%v", result, err, b.calls)
			}
			if c == config.CategorySandbox && !slices.Equal(b.calls, []string{"printenv HOME", "codex --version"}) {
				t.Fatalf("sandbox probed stack: %v", b.calls)
			}
		})
	}
}
func TestCategoryLocksRejectOverlappingStop(t *testing.T) {
	s, target, b := categoryFixture(t, config.CategoryContainer)
	release, err := operationlock.Acquire(context.Background(), target.Runtime, []string{"container:" + target.Entry.ContainerID})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, err = s.Execute(context.Background(), Request{Kind: KindStop, Target: target}, nil); !errors.Is(err, operationlock.ErrConflict) || len(b.calls) != 0 {
		t.Fatalf("err=%v calls=%v", err, b.calls)
	}
}
func TestCategoryCreatePreservesPartialDockerIdentity(t *testing.T) {
	s, r, path := restoreFixture(t)
	selection := config.Selection{Schema: 2, Category: config.CategoryContainer, Pairs: []config.Pair{{Agent: "codex", Methodology: "superpowers"}}}
	b := &categoryBackend{createErr: errors.New("startup verification failed")}
	s.Backends = map[config.Category]Backend{config.CategoryContainer: b}
	result, err := s.Execute(context.Background(), Request{Kind: KindCreate, Target: Target{Workspace: r.Target.Workspace}, Selection: selection}, nil)
	if err == nil || !result.Changed || !result.NeedsVerification {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	var reg config.Registry
	if err := config.ReadJSON(path, &reg); err != nil {
		t.Fatal(err)
	}
	entry := reg.Profiles["container/codex-superpowers"]
	if entry.ContainerID != strings.Repeat("b", 64) || reg.IsActive("container/codex-superpowers") {
		t.Fatalf("lost partial identity or activated it: %+v", reg)
	}
	found := false
	for _, target := range result.Remaining {
		if target.Entry.ContainerID == entry.ContainerID && target.Category == config.CategoryContainer {
			found = true
		}
	}
	if !found {
		t.Fatalf("partial identity absent in result: %+v", result)
	}
	log, _ := os.ReadFile(os.Getenv("SBX_TEST_LOG"))
	if strings.Contains(string(log), "create ") {
		t.Fatal("container create called SBX", string(log))
	}
}
func TestCategoryContainerLoadRejected(t *testing.T) {
	s, r, _ := restoreFixture(t)
	selection := config.Selection{Schema: 2, Category: config.CategoryContainer, Pairs: []config.Pair{{Agent: "codex", Methodology: "superpowers"}}}
	result, err := s.Execute(context.Background(), Request{Kind: KindLoad, Target: r.Target, Selection: selection}, nil)
	if err == nil || result.Changed {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
func TestProviderIDsPreserveLegacy(t *testing.T) {
	installed := []config.Installed{{Registry: config.Registry{Active: []string{"codex-native", "sandbox/codex", "container/codex-native"}, Profiles: map[string]config.Entry{
		"codex-native": {Agent: "codex", Tool: "native"}, "sandbox/codex": {Category: config.CategorySandbox, Agent: "codex"}, "container/codex-native": {Category: config.CategoryContainer, Agent: "codex", Tool: "native"},
	}}}}
	got := ActiveAdapters(installed)
	if len(got) != 3 || got["codex-native"] != "codex" || got["sbx-codex"] != "codex" || got["container-codex-native"] != "codex" {
		t.Fatal(got)
	}
}
func TestContainerShellUsesExactID(t *testing.T) {
	s, target := shellFixture(t, config.CategoryContainer)
	cmd, err := s.ShellCommand(context.Background(), target)
	if err != nil || cmd == nil {
		t.Fatalf("cmd=%v err=%v", cmd, err)
	}
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	log, _ := os.ReadFile(os.Getenv("SBX_TEST_LOG"))
	if !strings.Contains(string(log), " "+target.Entry.ContainerID+" bash") {
		t.Fatalf("wrong shell identity: %s", log)
	}
}
func TestSnapshotContainerNotMissingWhenSBXIsEmpty(t *testing.T) {
	s, target, b := categoryFixture(t, config.CategoryContainer)
	b.rows = []RuntimeState{{ID: target.Entry.ContainerID, Known: true, Running: true, ImageID: target.Entry.ImageID}}
	s.Backends[config.CategorySandbox] = &categoryBackend{}
	snap, err := s.Snapshot(context.Background(), Query{})
	if err != nil || len(snap.Workspaces) != 1 || len(snap.Workspaces[0].Sandboxes) != 1 || snap.Workspaces[0].Sandboxes[0].RuntimeState != "running" {
		t.Fatalf("snap=%+v err=%v", snap, err)
	}
}
func TestSandboxSnapshotDoesNotClaimPinnedAgentVersion(t *testing.T) {
	s, target, b := categoryFixture(t, config.CategorySandbox)
	s.Lock.Agents = map[string]string{"codex": "old-lock"}
	b.rows = []RuntimeState{{ID: target.Entry.Sandbox, Known: true}}
	snap, err := s.Snapshot(context.Background(), Query{})
	if err != nil {
		t.Fatal(err)
	}
	if got := snap.Workspaces[0].Sandboxes[0].InitialVersions["agent"]; got != "" {
		t.Fatalf("agent-only snapshot claimed unrelated lock version: %q", got)
	}
}
func TestContainerCreatePreservesOtherCategoryActivation(t *testing.T) {
	s, r, path := restoreFixture(t)
	var before config.Registry
	if err := config.ReadJSON(path, &before); err != nil {
		t.Fatal(err)
	}
	b := &categoryBackend{}
	s.Backends = map[config.Category]Backend{config.CategoryContainer: b}
	selection := config.Selection{Schema: 2, Category: config.CategoryContainer, Pairs: []config.Pair{{Agent: "codex", Methodology: "superpowers"}}}
	result, err := s.Execute(context.Background(), Request{Kind: KindCreate, Target: Target{Workspace: r.Target.Workspace}, Selection: selection}, nil)
	if err != nil || !result.Changed {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	var after config.Registry
	if err := config.ReadJSON(path, &after); err != nil {
		t.Fatal(err)
	}
	if !after.IsActive("container/codex-superpowers") {
		t.Fatal(after)
	}
	for _, key := range before.ActiveProfiles() {
		if !after.IsActive(key) {
			t.Fatal("lost other category activation", key)
		}
	}
}
func TestContainerCreateRecordsExplicitPorts(t *testing.T) {
	s, r, path := restoreFixture(t)
	b := &categoryBackend{}
	s.Backends = map[config.Category]Backend{config.CategoryContainer: b}
	selection := config.Selection{Schema: 2, Category: config.CategoryContainer, Pairs: []config.Pair{{Agent: "codex", Methodology: "superpowers"}}}
	ports := []config.PortMapping{{HostPort: 8080, ContainerPort: 3000}}
	_, err := s.Execute(context.Background(), Request{Kind: KindCreate, Target: Target{Workspace: r.Target.Workspace}, Selection: selection, Ports: ports}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var reg config.Registry
	if err := config.ReadJSON(path, &reg); err != nil {
		t.Fatal(err)
	}
	got := reg.Profiles["container/codex-superpowers"].Ports
	if len(got) != 1 || got[0].HostAddress != "127.0.0.1" || got[0].HostPort != 8080 || got[0].ContainerPort != 3000 {
		t.Fatalf("ports=%+v", got)
	}
}

func TestRestoreCategorySelection(t *testing.T) {
	for _, category := range []config.Category{config.CategorySandbox, config.CategoryContainer} {
		t.Run(string(category), func(t *testing.T) {
			s, target, _ := categoryFixture(t, category)
			s.Catalog = config.DefaultCatalog()
			selection := config.Selection{Schema: 2, Category: category, Agent: "codex"}
			if category == config.CategoryContainer {
				selection.Agent = ""
				selection.Pairs = []config.Pair{{Agent: "codex", Methodology: "superpowers"}}
				target.Entry.Tool = "superpowers"
				target.Profile = "container/codex-superpowers"
			}
			path := filepath.Join(target.Runtime, "registry.json")
			reg := config.Registry{Workspace: target.Workspace, Profiles: map[string]config.Entry{target.Profile: target.Entry}, Active: []string{}, Selection: &selection}
			if err := config.WriteJSON(path, reg); err != nil {
				t.Fatal(err)
			}
			a := s.execution(context.Background(), &selection, nil)
			err := a.restoreRegistration(target.Runtime, Request{Targets: []Target{}, RestoreTargets: []Target{target}})
			if err != nil {
				t.Fatal(err)
			}
			if err := config.ReadJSON(path, &reg); err != nil {
				t.Fatal(err)
			}
			if !reg.IsActive(target.Profile) {
				t.Fatal(reg)
			}
		})
	}
}
func TestContainerUpdateRunsOutsideWorkspace(t *testing.T) {
	s, target, b := categoryFixture(t, config.CategoryContainer)
	_, _ = s.Execute(context.Background(), Request{Kind: KindUpdate, Target: target}, nil)
	if len(b.calls) == 0 {
		t.Fatal("no guest transport")
	}
	for _, call := range b.calls {
		if !strings.HasPrefix(call, "env -C /tmp ") {
			t.Fatalf("installer read workspace configuration: %s", call)
		}
	}
}
func TestContainerUpdateRequiresExplicitTransport(t *testing.T) {
	_, target, _ := categoryFixture(t, config.CategoryContainer)
	err := sandboxupdate.UpdateTransportLockedContext(context.Background(), target.Runtime, target.Entry, true, io.Discard, nil, nil)
	log, _ := os.ReadFile(os.Getenv("SBX_TEST_LOG"))
	if err == nil || len(log) > 0 {
		t.Fatalf("err=%v SBX calls=%s", err, log)
	}
}
func TestPartialDockerIdentitySurvivesRegistryWriteFailure(t *testing.T) {
	for _, createErr := range []error{nil, errors.New("created but startup failed")} {
		t.Run(fmt.Sprint(createErr), func(t *testing.T) {
			s, r, path := restoreFixture(t)
			b := &categoryBackend{createErr: createErr, onCreate: func() {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			}}
			s.Backends = map[config.Category]Backend{config.CategoryContainer: b}
			selection := config.Selection{Schema: 2, Category: config.CategoryContainer, Pairs: []config.Pair{{Agent: "codex", Methodology: "superpowers"}}}
			result, err := s.Execute(context.Background(), Request{Kind: KindCreate, Target: Target{Workspace: r.Target.Workspace}, Selection: selection}, nil)
			if err == nil || !result.NeedsVerification || len(result.Remaining) != 1 || result.Remaining[0].Entry.ContainerID != strings.Repeat("b", 64) {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
}
func TestSBXBackendRejectsContainerEntry(t *testing.T) {
	s, target, _ := categoryFixture(t, config.CategoryContainer)
	err := (&SBXBackend{Root: s.Root}).Stop(context.Background(), target.Entry)
	log, _ := os.ReadFile(os.Getenv("SBX_TEST_LOG"))
	if err == nil || len(log) > 0 {
		t.Fatalf("err=%v calls=%s", err, log)
	}
}
func TestImplicitUpdateRejectsMixedCategories(t *testing.T) {
	s, target, b := categoryFixture(t, config.CategorySandbox)
	container := target.Entry
	container.Category = config.CategoryContainer
	container.Tool = "native"
	container.Sandbox = ""
	container.ContainerID = strings.Repeat("a", 64)
	container.ImageID = "sha256:image"
	container.Network = "bridge"
	reg := config.Registry{Workspace: target.Workspace, Active: []string{"sandbox/codex", "container/codex-native"}, Profiles: map[string]config.Entry{"sandbox/codex": target.Entry, "container/codex-native": container}}
	if err := config.WriteJSON(filepath.Join(target.Runtime, "registry.json"), reg); err != nil {
		t.Fatal(err)
	}
	s.Backends[config.CategoryContainer] = b
	_, err := s.Execute(context.Background(), Request{Kind: KindUpdate, Target: Target{Workspace: target.Workspace}}, nil)
	if err == nil || len(b.calls) > 0 {
		t.Fatalf("ambiguous update dispatched: err=%v calls=%v", err, b.calls)
	}
}

func TestCategoryUnregisterPreservesOtherActiveCategory(t *testing.T) {
	s, target, _ := categoryFixture(t, config.CategorySandbox)
	b := &categoryBackend{}
	s.Backends[config.CategoryContainer] = b
	other := target
	other.Category = config.CategoryContainer
	other.Entry.Category = other.Category
	other.Entry.Tool = "superpowers"
	other.Entry.ContainerID = strings.Repeat("d", 64)
	other.Entry.ImageID = "sha256:other"
	other.Entry.Network = "bridge"
	other.Profile = other.Entry.Key()
	var reg config.Registry
	path := filepath.Join(target.Runtime, "registry.json")
	if err := config.ReadJSON(path, &reg); err != nil {
		t.Fatal(err)
	}
	reg.Profiles[other.Profile] = other.Entry
	reg.Active = []string{target.Profile, other.Profile}
	if err := config.WriteJSON(path, reg); err != nil {
		t.Fatal(err)
	}
	// Provider merge failure after the registry write does not invalidate the scope proof.
	_, _ = s.Execute(context.Background(), Request{Kind: KindUnregister, Target: Target{Workspace: target.Workspace, Runtime: target.Runtime, Category: config.CategorySandbox}, Targets: []Target{target, other}, PaseoHome: filepath.Join(t.TempDir(), "paseo")}, nil)
	if err := config.ReadJSON(path, &reg); err != nil {
		t.Fatal(err)
	}
	if reg.IsActive(target.Profile) || !reg.IsActive(other.Profile) {
		t.Fatalf("category unregister changed other activation: %v", reg.ActiveProfiles())
	}
}

func TestUnregisterWithoutCategoryStillDeactivatesWholeWorkspace(t *testing.T) {
	s, target, _ := categoryFixture(t, config.CategorySandbox)
	other := target
	other.Entry.Category = config.CategoryContainer
	other.Category = config.CategoryContainer
	other.Entry.Tool = "superpowers"
	other.Entry.ContainerID = strings.Repeat("c", 64)
	other.Entry.ImageID = "sha256:other"
	other.Entry.Network = "bridge"
	other.Profile = other.Entry.Key()
	path := filepath.Join(target.Runtime, "registry.json")
	var reg config.Registry
	if err := config.ReadJSON(path, &reg); err != nil {
		t.Fatal(err)
	}
	reg.Profiles[other.Profile] = other.Entry
	reg.Active = []string{target.Profile, other.Profile}
	if err := config.WriteJSON(path, reg); err != nil {
		t.Fatal(err)
	}
	target.Category = "" // Existing callers pin an entry without an explicit removal category.
	_, _ = s.Execute(context.Background(), Request{Kind: KindUnregister, Target: target, Targets: []Target{target, other}, PaseoHome: t.TempDir()}, nil)
	if err := config.ReadJSON(path, &reg); err != nil {
		t.Fatal(err)
	}
	if len(reg.ActiveProfiles()) != 0 {
		t.Fatal("implicit selected entry category narrowed workspace unregister", reg.ActiveProfiles())
	}
}
