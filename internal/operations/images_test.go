package operations

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"paseo-dock/internal/config"
)

func TestSandboxBuildFailurePreservesRecordedImage(t *testing.T) {
	for _, tc := range []struct {
		name, command string
		wantFailure   bool
	}{
		{name: "failed bake", command: "false", wantFailure: true},
		{name: "successful bake", command: "true"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := imageTestExecution(t, config.Selection{Schema: 2, Category: config.CategorySandbox, Agent: "codex"})
			a.Root = t.TempDir()
			prior, err := a.bakeConfig("test", "", func(context.Context, string) (string, string, error) {
				return "9.8.7", "https://registry.npmjs.org/@openai%2Fcodex/9.8.7", nil
			})
			if err != nil {
				t.Fatal(err)
			}
			record := filepath.Join(a.Root, ".generated", "images", "sandbox-codex.json")
			if err := config.WriteJSON(record, prior); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(record)
			if err != nil {
				t.Fatal(err)
			}
			oldImage := a.image("sandbox/codex", "test")
			if oldImage == "" {
				t.Fatal("fixture has no recorded image")
			}
			old := stableAgentClient
			stableAgentClient = &http.Client{Transport: packageTransport(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"name":"@openai/codex","version":"9.8.8"}`))}, nil
			})}
			t.Cleanup(func() { stableAgentClient = old })
			command, err := exec.LookPath(tc.command)
			if err != nil {
				t.Fatal(err)
			}
			fake := t.TempDir()
			if err := os.Symlink(command, filepath.Join(fake, "docker")); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", fake+":"+os.Getenv("PATH"))
			err = a.build("test", "", false)
			if tc.wantFailure {
				var exit *exec.ExitError
				if !errors.As(err, &exit) || exit.ExitCode() != 1 {
					t.Fatalf("expected attempted Bake to fail: %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			var input struct {
				Target map[string]struct {
					Args map[string]string `json:"args"`
				} `json:"target"`
			}
			if err := config.ReadJSON(filepath.Join(a.Root, ".generated", "bake.json"), &input); err != nil {
				t.Fatal(err)
			}
			if input.Target["sandbox-codex"].Args["AGENT_VERSION"] != "9.8.8" {
				t.Fatal("Bake did not receive the newer resolved version")
			}
			after, err := os.ReadFile(record)
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantFailure {
				if !bytes.Equal(before, after) || a.image("sandbox/codex", "test") != oldImage {
					t.Fatal("failed Bake replaced the previous successful image record")
				}
			} else if bytes.Equal(before, after) || a.image("sandbox/codex", "test") == "" || a.image("sandbox/codex", "test") == oldImage {
				t.Fatal("successful Bake did not publish the newer image record")
			}
		})
	}
}

func imageTestExecution(t *testing.T, selection config.Selection) *execution {
	t.Helper()
	s, err := New("../..")
	if err != nil {
		t.Fatal(err)
	}
	return s.execution(context.Background(), &selection, nil)
}

func TestSandboxRecipeOmitsTools(t *testing.T) {
	old := stableAgentClient
	stableAgentClient = &http.Client{Transport: packageTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://registry.npmjs.org/@openai%2Fcodex/latest" {
			t.Fatalf("unofficial lookup: %s", r.URL)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"name":"@openai/codex","version":"9.8.7"}`))}, nil
	})}
	t.Cleanup(func() { stableAgentClient = old })
	a := imageTestExecution(t, config.Selection{Schema: 2, Category: config.CategorySandbox, Agent: "codex"})
	cfg := a.BakeConfig("test", "linux/amd64")
	targets := cfg["target"].(map[string]any)
	item, ok := targets["sandbox-codex"].(map[string]any)
	if !ok {
		t.Fatalf("agent-only target missing: %v", targets)
	}
	if item["dockerfile"] != "Dockerfile.sandbox" {
		t.Fatalf("wrong recipe: %v", item)
	}
	args := item["args"].(map[string]string)
	if args["BASE_IMAGE"] != "ubuntu:24.04" || args["AGENT_VERSION"] != "9.8.7" || args["AGENT_SOURCE"] != "https://registry.npmjs.org/@openai%2Fcodex/9.8.7" {
		t.Fatalf("unresolved sandbox recipe: %v", args)
	}
	var manifest map[string]string
	if err := json.Unmarshal([]byte(args["AGENT_MANIFEST"]), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest["version"] != "9.8.7" || manifest["source"] != args["AGENT_SOURCE"] || manifest["category"] != "sandbox" {
		t.Fatalf("wrong manifest: %v", manifest)
	}
	for _, name := range []string{"TOOL", "METHOD_INSTALLER", "UTILITIES"} {
		if _, ok := args[name]; ok {
			t.Fatalf("sandbox includes stack argument %s", name)
		}
	}
}

func TestContainerRecipeUsesLockedStack(t *testing.T) {
	a := imageTestExecution(t, config.Selection{Schema: 2, Category: config.CategoryContainer, Pairs: []config.Pair{{Agent: "codex", Methodology: "superpowers"}}, Utilities: []string{"rtk"}})
	item, ok := a.BakeConfig("test", "")["target"].(map[string]any)["container-codex-superpowers"].(map[string]any)
	if !ok {
		t.Fatal("container target missing")
	}
	args := item["args"].(map[string]string)
	if item["dockerfile"] != "Dockerfile" || args["AGENT_VERSION"] != "0.159.2" || args["BASE_IMAGE"] != a.Lock.BaseImages["codex"] || args["TOOL"] != "superpowers" || args["METHOD_INSTALLER"] != "git-plugin" || args["UTILITIES"] != "rtk" {
		t.Fatalf("locked stack changed: %v", item)
	}
	if _, ok := item["labels"]; !ok {
		t.Fatalf("missing category and fingerprint labels: %v", item)
	}
	var manifest map[string]string
	if err := json.Unmarshal([]byte(args["AGENT_MANIFEST"]), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest["version"] != "0.159.2" || manifest["source"] != "https://registry.npmjs.org/@openai%2Fcodex/0.159.2" || manifest["category"] != "container" {
		t.Fatalf("locked manifest changed: %v", manifest)
	}
}

func TestImageFingerprintSeparatesCategories(t *testing.T) {
	a := imageTestExecution(t, config.Selection{Schema: 2, Category: config.CategoryContainer, Pairs: []config.Pair{{Agent: "codex", Methodology: "superpowers"}}})
	item, ok := a.BakeConfig("test", "")["target"].(map[string]any)["container-codex-superpowers"].(map[string]any)
	if !ok {
		t.Fatal("container target missing")
	}
	labels, ok := item["labels"].(map[string]string)
	if !ok || labels["io.paseo-dock.category"] != "container" || labels["io.paseo-dock.fingerprint"] == "" {
		t.Fatalf("missing category identity: %v", item)
	}
	entry := config.Entry{Agent: "codex", Tool: "superpowers"}
	containerTag, _, err := ImageSpec(config.CategoryContainer, entry, a.Lock)
	if err != nil {
		t.Fatal(err)
	}
	legacyTag, _, err := ImageSpec(config.CategoryLegacySandbox, entry, a.Lock)
	if err != nil || containerTag == legacyTag {
		t.Fatalf("category collision: %s %s %v", containerTag, legacyTag, err)
	}
	entry.Utilities = []string{"rtk"}
	utilityTag, _, err := ImageSpec(config.CategoryContainer, entry, a.Lock)
	if err != nil || utilityTag == containerTag {
		t.Fatalf("utility collision: %s %s %v", containerTag, utilityTag, err)
	}
}

func TestStableLookupFailureDoesNotReuseLatest(t *testing.T) {
	a := imageTestExecution(t, config.Selection{Schema: 2, Category: config.CategorySandbox, Agent: "antigravity-cli"})
	cfg := a.BakeConfig("test", "")
	if cfg["error"] == nil {
		t.Fatalf("unverifiable stable agent succeeded: %v", cfg)
	}
}

type packageTransport func(*http.Request) (*http.Response, error)

func (f packageTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestStableAgentOfficialPackages(t *testing.T) {
	for _, tc := range []struct{ agent, name string }{{"codex", "@openai/codex"}, {"claude", "@anthropic-ai/claude-code"}} {
		t.Run(tc.agent, func(t *testing.T) {
			client := &http.Client{Transport: packageTransport(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"name":"` + tc.name + `","version":"3.2.1"}`))}, nil
			})}
			version, source, err := resolveStableAgent(context.Background(), tc.agent, client)
			if err != nil || version != "3.2.1" || !strings.HasSuffix(source, "/3.2.1") {
				t.Fatalf("%s %s %v", version, source, err)
			}
		})
	}
}

func TestStableAgentRejectsFailuresAndPrereleases(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		err        error
	}{
		{name: "network", err: errors.New("offline")},
		{name: "HTTP", status: 503, body: `{"name":"@openai/codex","version":"3.2.1"}`},
		{name: "malformed", status: 200, body: `{`},
		{name: "latest", status: 200, body: `{"name":"@openai/codex","version":"latest"}`},
		{name: "prerelease", status: 200, body: `{"name":"@openai/codex","version":"3.2.1-beta.1"}`},
		{name: "wrong-package", status: 200, body: `{"name":"evil","version":"3.2.1"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Transport: packageTransport(func(*http.Request) (*http.Response, error) {
				if tc.err != nil {
					return nil, tc.err
				}
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})}
			v, s, err := resolveStableAgent(context.Background(), "codex", client)
			if err == nil || v != "" || s != "" {
				t.Fatalf("failed lookup reused value: %q %q %v", v, s, err)
			}
		})
	}
}

func TestSandboxLoadRejectsChangedRecordedIdentity(t *testing.T) {
	a := imageTestExecution(t, config.Selection{Schema: 2, Category: config.CategorySandbox, Agent: "codex"})
	a.Root = t.TempDir()
	cfg, err := a.bakeConfig("test", "", func(context.Context, string) (string, string, error) {
		return "9.8.7", "https://registry.npmjs.org/@openai%2Fcodex/9.8.7", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	record := filepath.Join(a.Root, ".generated", "images", "sandbox-codex.json")
	if err := config.WriteJSON(record, cfg); err != nil {
		t.Fatal(err)
	}
	// A later container bake must not replace the sandbox's resolved version.
	if err := config.WriteJSON(filepath.Join(a.Root, ".generated", "bake.json"), map[string]any{"target": map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	if image := a.image("sandbox/codex", "test"); !strings.HasPrefix(image, "test/sandbox/codex:") {
		t.Fatalf("recorded image missing: %s", image)
	}
	if image := a.image("sandbox/codex", "other"); image != "" {
		t.Fatalf("namespace mismatch reused recorded image: %s", image)
	}
	item := cfg["target"].(map[string]any)["sandbox-codex"].(map[string]any)
	item["labels"].(map[string]string)["io.paseo-dock.category"] = "container"
	if err := config.WriteJSON(record, cfg); err != nil {
		t.Fatal(err)
	}
	if image := a.image("sandbox/codex", "test"); image != "" {
		t.Fatalf("category mismatch reused recorded image: %s", image)
	}
}

func TestContainerLoadDoesNotUseSBX(t *testing.T) {
	a := imageTestExecution(t, config.Selection{Schema: 2, Category: config.CategoryContainer, Pairs: []config.Pair{{Agent: "codex", Methodology: "superpowers"}}})
	if err := a.load("test", false); err == nil {
		t.Fatal("container image permitted SBX template load")
	}
}

func TestImageSpecNormalizesEmptyUtilitiesAndRecordsSource(t *testing.T) {
	a := imageTestExecution(t, config.Selection{})
	entry := config.Entry{Agent: "codex", Tool: "superpowers"}
	first, labels, err := ImageSpec(config.CategoryContainer, entry, a.Lock)
	if err != nil {
		t.Fatal(err)
	}
	if labels["io.paseo-dock.agent.source"] != "https://registry.npmjs.org/@openai%2Fcodex/0.159.2" {
		t.Fatalf("ImageSpec omitted package source: %v", labels)
	}
	entry.Utilities = []string{}
	second, _, err := ImageSpec(config.CategoryContainer, entry, a.Lock)
	if err != nil || first != second {
		t.Fatalf("empty utility selection changes identity: %s %s %v", first, second, err)
	}
}
