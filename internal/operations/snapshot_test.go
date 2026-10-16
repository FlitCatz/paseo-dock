package operations

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"paseo-dock/internal/config"
	"paseo-dock/internal/sandboxupdate"
)

type observedResponse struct {
	Output string
	Error  bool
	Delay  time.Duration
}

// Only the external process is replaced; snapshot discovery, parsing and joins run unchanged.
func observationFake() {
	args := os.Args[1:]
	name := filepath.Base(os.Args[0])
	f, _ := os.OpenFile(os.Getenv("SBX_TEST_LOG"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if f != nil {
		fmt.Fprintln(f, name+" "+strings.Join(args, " "))
		f.Close()
	}
	var responses map[string]observedResponse
	_ = json.Unmarshal([]byte(os.Getenv("OBSERVE_TEST_RESPONSES")), &responses)
	r, ok := responses[name+" "+strings.Join(args, " ")]
	if !ok {
		fmt.Fprintln(os.Stderr, "unexpected external command")
		os.Exit(23)
	}
	time.Sleep(r.Delay)
	fmt.Print(r.Output)
	if r.Error {
		fmt.Fprintln(os.Stderr, "external unavailable")
		os.Exit(1)
	}
	os.Exit(0)
}
func observationFixture(t *testing.T) (*Service, Target, string) {
	t.Helper()
	s, target, log := operationFixture(t)
	s.Catalog = config.DefaultCatalog()
	loaded, err := New("../..")
	if err != nil {
		t.Fatal(err)
	}
	s.Lock = loaded.Lock
	exe, _ := os.Executable()
	for _, name := range []string{"paseo", "docker"} {
		if err := os.Symlink(exe, filepath.Join(filepath.Dir(log), name)); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("OBSERVE_TEST_MODE", "1")
	return s, target, log
}
func observationResponses(t *testing.T, responses map[string]observedResponse) {
	t.Helper()
	b, err := json.Marshal(responses)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("OBSERVE_TEST_RESPONSES", string(b))
}
func snapshotRow(t *testing.T, s *Service, q Query) (Snapshot, Sandbox) {
	t.Helper()
	snap, err := s.Snapshot(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Workspaces) != 1 || len(snap.Workspaces[0].Sandboxes) != 1 {
		t.Fatalf("lost managed workspace: %+v", snap)
	}
	return snap, snap.Workspaces[0].Sandboxes[0]
}

// A poll that invokes exec or writes any runtime/provider file would fail this test.
func TestSnapshotReadOnly(t *testing.T) {
	s, target, log := observationFixture(t)
	home := t.TempDir()
	observationResponses(t, map[string]observedResponse{"sbx ls --json": {Output: `{"sandboxes":[{"name":"one","status":"running","workspaces":["/project"]}]}`}})
	before := readTree(t, target.Runtime)
	_, row := snapshotRow(t, s, Query{PaseoHome: home})
	if row.RuntimeState != "running" {
		t.Fatalf("runtime=%s", row.RuntimeState)
	}
	after := readTree(t, target.Runtime)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("poll mutated runtime")
	}
	if entries, _ := os.ReadDir(home); len(entries) != 0 {
		t.Fatal("poll wrote Paseo configuration")
	}
	b, _ := os.ReadFile(log)
	if string(b) != "sbx ls --json\n" {
		t.Fatalf("hidden slow query or mutation: %s", b)
	}
}

// A name prefix join, an unknown-wrapper-as-empty fallback, or missing-error suppression fails these cases.
func TestSnapshotJoinAndErrors(t *testing.T) {
	for _, tc := range []struct {
		name, raw, state string
		unmanaged        int
		failure          bool
	}{
		{"array", `[{"name":"one","status":"stopped"},{"name":"one-other","status":"running"}]`, "stopped", 1, false},
		{"wrapper", `{"sandboxes":[{"name":"one","status":"running","workspaces":[{"path":"/project"}]},{"name":"foreign","status":"stopped"}]}`, "running", 1, false},
		{"empty", `[]`, "missing", 0, false}, {"empty-wrapper", `{"sandboxes":[]}`, "missing", 0, false},
		{"bad-json", `{`, "unknown", 0, true}, {"wrong-wrapper", `{"items":[]}`, "unknown", 0, true},
		{"null", `null`, "unknown", 0, true}, {"null-wrapper", `{"sandboxes":null}`, "unknown", 0, true},
		{"bad-row", `{"sandboxes":[{"status":"running"}]}`, "unknown", 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, _ := observationFixture(t)
			observationResponses(t, map[string]observedResponse{"sbx ls --json": {Output: tc.raw}})
			snap, row := snapshotRow(t, s, Query{PaseoHome: t.TempDir()})
			if row.RuntimeState != tc.state || len(snap.Unmanaged) != tc.unmanaged || (snap.SBX.Error != "") != tc.failure {
				t.Fatalf("incorrect join/error: %+v %+v", snap, row)
			}
			if snap.SBX.CheckedAt.IsZero() {
				t.Fatal("query time missing")
			}
		})
	}
	t.Run("broken-registry", func(t *testing.T) {
		s, target, _ := observationFixture(t)
		_ = os.WriteFile(filepath.Join(target.Runtime, "registry.json"), []byte("{"), 0600)
		observationResponses(t, map[string]observedResponse{"sbx ls --json": {Output: `[]`}})
		snap, err := s.Snapshot(context.Background(), Query{PaseoHome: t.TempDir()})
		if err != nil {
			t.Fatal(err)
		}
		if len(snap.Workspaces) != 1 || snap.Workspaces[0].Error == "" || snap.Workspaces[0].Runtime != target.Runtime || snap.Workspaces[0].ID != target.Runtime {
			t.Fatalf("malformed registry hidden or unstable identity: %+v", snap)
		}
	})
}

// Update state belonging to another identity must never confirm this guest.
func TestVersionProvenance(t *testing.T) {
	for _, tc := range []struct {
		name, sandbox, image string
		want                 bool
	}{
		{"matching", "one", "image", true}, {"wrong-image", "one", "old-image", false}, {"wrong-sandbox", "foreign", "image", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, target, _ := observationFixture(t)
			observationResponses(t, map[string]observedResponse{"sbx ls --json": {Output: `[]`}})
			now := time.Now().UTC()
			state := sandboxupdate.State{Sandbox: tc.sandbox, Image: tc.image, Attempt: now, Versions: map[string]string{"agent": "2.0"}, Errors: map[string]string{"methodology": "failed"}}
			sum := sha256.Sum256([]byte("one"))
			path := filepath.Join(target.Runtime, "updates", fmt.Sprintf("%x.json", sum[:8]))
			if err := config.WriteJSON(path, state); err != nil {
				t.Fatal(err)
			}
			_, row := snapshotRow(t, s, Query{PaseoHome: t.TempDir()})
			if (row.Update != nil) != tc.want {
				t.Fatalf("identity mismatch leaked: %+v", row.Update)
			}
			if tc.want && (!row.Update.Attempt.Equal(now) || row.Update.Versions["agent"] != "2.0" || row.Update.Errors["methodology"] != "failed" || !row.Update.Success.IsZero()) {
				t.Fatalf("partial attempt lost: %+v", row.Update)
			}
			if row.InitialVersions["agent"] != s.Lock.Agents["codex"] {
				t.Fatal("image configuration provenance missing")
			}
		})
	}
}

// Active selection alone is insufficient; the shared managed provider and adapter must agree.
func TestProviderRegistrationIsNotActivation(t *testing.T) {
	for _, tc := range []struct {
		name     string
		provider any
		raw      string
		want     string
	}{
		{name: "absent", want: "unregistered"},
		{name: "disabled", provider: map[string]any{"enabled": false, "extends": "codex"}, want: "unregistered"},
		{name: "unreadable", raw: "{", want: "unknown"},
		{name: "wrong-command", provider: map[string]any{"enabled": true, "extends": "codex", "command": []string{"/other", "codex-native"}}, want: "unregistered"},
		{name: "wrong-adapter", provider: map[string]any{"enabled": true, "extends": "claude"}, want: "unregistered"},
		{name: "managed", want: "registered"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, _ := observationFixture(t)
			observationResponses(t, map[string]observedResponse{"sbx ls --json": {Output: `[]`}})
			home := t.TempDir()
			base, _ := config.BaseDir()
			base, _ = config.Canonical(base)
			p := tc.provider
			if tc.name == "managed" {
				p = map[string]any{"enabled": true, "extends": "codex", "command": []string{config.LauncherPath(base), "codex-native"}}
			}
			if tc.raw != "" {
				_ = os.WriteFile(filepath.Join(home, "config.json"), []byte(tc.raw), 0600)
			} else if p != nil {
				_ = config.WriteJSON(filepath.Join(home, "config.json"), map[string]any{"agents": map[string]any{"providers": map[string]any{"codex-native": p}}})
			}
			_, row := snapshotRow(t, s, Query{PaseoHome: home})
			if !row.Active || row.Registration != tc.want {
				t.Fatalf("activation conflated with registration: %+v", row)
			}
		})
	}
}
func readTree(t *testing.T, path string) map[string]string {
	t.Helper()
	result := map[string]string{}
	err := filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			result[p] = string(b)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// Optional failures must not turn a successful sbx query into an unknown/missing result.
func TestSnapshotOptionalConnections(t *testing.T) {
	s, _, log := observationFixture(t)
	home := t.TempDir()
	home, _ = config.Canonical(home)
	observationResponses(t, map[string]observedResponse{
		"sbx ls --json": {Output: `[{"name":"one","status":"running"}]`},
		"paseo --home " + home + " status --json": {Output: `{"localDaemon":"running"}`},
		"docker info --format {{json .}}":         {Error: true},
		"sbx template ls --json":                  {Output: `{"images":[{"repository":"docker.io/paseo-sbx/codex-native","tag":"test","id":"abc","flavor":"codex","created_at":"2026-10-01T07:54:03Z","size":2308130110}]}`},
	})
	snap, row := snapshotRow(t, s, Query{PaseoHome: home, IncludePaseo: true, IncludeDocker: true, IncludeTemplates: true})
	if row.RuntimeState != "running" || snap.SBX.Error != "" || snap.Paseo.Status != "running" || snap.Docker.Error == "" || snap.Templates.Error != "" || len(snap.TemplateRows) != 1 || snap.TemplateRows[0].ID != "abc" {
		t.Fatalf("independent status error lost: %+v", snap)
	}
	b, _ := os.ReadFile(log)
	if strings.Contains(string(b), "exec") {
		t.Fatalf("optional query executed guest: %s", b)
	}
}
func TestSnapshotTemplatesArrayAndMalformed(t *testing.T) {
	for _, tc := range []struct {
		raw     string
		want    int
		failure bool
	}{
		{`[{"repository":"repo","tag":"tag","id":"id"}]`, 1, false}, {`{"templates":[]}`, 0, true}, {`{"images":null}`, 0, true}, {`{"images":[{}]}`, 0, true},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			s, _, _ := observationFixture(t)
			observationResponses(t, map[string]observedResponse{"sbx ls --json": {Output: `[]`}, "sbx template ls --json": {Output: tc.raw}})
			snap, _ := snapshotRow(t, s, Query{PaseoHome: t.TempDir(), IncludeTemplates: true})
			if len(snap.TemplateRows) != tc.want || (snap.Templates.Error != "") != tc.failure {
				t.Fatalf("bad template schema handling: %+v", snap)
			}
		})
	}
}
func TestSnapshotCaptureBoundAndCancellation(t *testing.T) {
	t.Run("overflow", func(t *testing.T) {
		s, _, _ := observationFixture(t)
		observationResponses(t, map[string]observedResponse{"sbx ls --json": {Output: strings.Repeat(" ", 1<<20) + `[]`}})
		snap, row := snapshotRow(t, s, Query{PaseoHome: t.TempDir()})
		if snap.SBX.Error == "" || row.RuntimeState != "unknown" {
			t.Fatal("oversized capture accepted")
		}
	})
	t.Run("cancel", func(t *testing.T) {
		s, _, _ := observationFixture(t)
		observationResponses(t, map[string]observedResponse{"sbx ls --json": {Output: `[]`, Delay: time.Second}})
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()
		began := time.Now()
		snap, err := s.Snapshot(ctx, Query{PaseoHome: t.TempDir()})
		if err == nil || time.Since(began) > 500*time.Millisecond || snap.SBX.Error == "" {
			t.Fatalf("query failed to cancel promptly: %+v %v", snap, err)
		}
	})
}
func TestSnapshotImageMismatchAndSharedRegistration(t *testing.T) {
	s, target, _ := observationFixture(t)
	target.Profile = "codex-superpowers"
	target.Entry.Tool = "superpowers"
	reg := config.Registry{Workspace: target.Workspace, Profiles: map[string]config.Entry{target.Profile: target.Entry}, Active: []string{}, Selection: &config.Selection{Schema: 1, Pairs: []config.Pair{{Agent: "codex", Methodology: "superpowers"}}}}
	if err := config.WriteJSON(filepath.Join(target.Runtime, "registry.json"), reg); err != nil {
		t.Fatal(err)
	}
	base, _ := config.BaseDir()
	base, _ = config.Canonical(base)
	home := t.TempDir()
	_ = config.WriteJSON(filepath.Join(home, "config.json"), ProviderConfig(config.LauncherPath(base), map[string]string{target.Profile: "codex"}))
	observationResponses(t, map[string]observedResponse{"sbx ls --json": {Output: `[]`}})
	_, row := snapshotRow(t, s, Query{PaseoHome: home})
	if row.Active || row.Registration != "registered" || !row.ImageMismatch {
		t.Fatalf("shared provider or image mismatch lost: %+v", row)
	}
	reg.Profiles[target.Profile] = config.Entry{Agent: target.Entry.Agent, Tool: target.Entry.Tool, Workspace: target.Workspace, Sandbox: target.Entry.Sandbox, Image: "ns/" + target.Profile + ":" + config.ImageTag(s.Lock, *reg.Selection, s.Catalog)}
	_ = config.WriteJSON(filepath.Join(target.Runtime, "registry.json"), reg)
	_, row = snapshotRow(t, s, Query{PaseoHome: home})
	if row.ImageMismatch {
		t.Fatalf("matching image incorrectly marked incompatible: %+v; selection %+v; expected %s", row, reg.Selection, config.ImageTag(s.Lock, *reg.Selection, s.Catalog))
	}
}

func TestSnapshotMalformedProviderStructureIsUnknown(t *testing.T) {
	for _, raw := range []string{`{"agents":[]}`, `{"agents":{"providers":[]}}`, `{"agents":{"providers":{"codex-native":null}}}`} {
		t.Run(raw, func(t *testing.T) {
			s, _, _ := observationFixture(t)
			home := t.TempDir()
			_ = os.WriteFile(filepath.Join(home, "config.json"), []byte(raw), 0600)
			observationResponses(t, map[string]observedResponse{"sbx ls --json": {Output: `[]`}})
			_, row := snapshotRow(t, s, Query{PaseoHome: home})
			if row.Registration != "unknown" {
				t.Fatalf("malformed provider config treated as readable: %+v", row)
			}
		})
	}
}
func TestSnapshotMalformedDockerResponseIsError(t *testing.T) {
	s, _, _ := observationFixture(t)
	observationResponses(t, map[string]observedResponse{"sbx ls --json": {Output: `[]`}, "docker info --format {{json .}}": {Output: "not-json"}})
	snap, row := snapshotRow(t, s, Query{PaseoHome: t.TempDir(), IncludeDocker: true})
	if snap.Docker.Error == "" || row.RuntimeState != "missing" {
		t.Fatalf("malformed independent response accepted: %+v", snap)
	}
}
