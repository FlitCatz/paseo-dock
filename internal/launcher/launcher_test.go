package launcher

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRewriteTree(t *testing.T) {
	var input any
	json.Unmarshal([]byte(`{"params":{"config":{"mcp_servers":{"paseo":{"url":"http://127.0.0.1:6767/mcp/agents?callerAgentId=parent","http_headers":{"Authorization":"Bearer HOST_SECRET"},"bearer_token_env_var":"HOST_TOKEN"},"other":{"url":"https://example.com/mcp"}}}}}`), &input)
	calls := 0
	out, err := RewriteTree(input, func(u string, h map[string]string) (string, map[string]string, error) {
		calls++
		return "http://host.docker.internal:12345/key", map[string]string{"Authorization": "Bearer SESSION"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	original, _ := json.Marshal(input)
	if strings.Contains(string(b), "HOST_SECRET") || strings.Contains(string(b), "HOST_TOKEN") || !strings.Contains(string(b), "Bearer SESSION") || !strings.Contains(string(b), "example.com") || calls != 1 || !strings.Contains(string(original), "HOST_SECRET") {
		t.Fatalf("bad rewrite %s", b)
	}
}
func TestWorkspaceBoundary(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	os.Symlink(outside, filepath.Join(root, "escape"))
	for _, tc := range []struct {
		cwd string
		ok  bool
	}{{"sub/new", true}, {root, true}, {"../outside", false}, {filepath.Join(root, "escape", "new"), false}} {
		if err := CheckWorkdirs(map[string]any{"nested": []any{map[string]any{"cwd": tc.cwd}}}, root); (err == nil) != tc.ok {
			t.Errorf("%s: %v", tc.cwd, err)
		}
	}
}
func TestLaunchStdioAndExit(t *testing.T) {
	// This fixture exercises the process cwd fallback, independent of Paseo's
	// session environment inherited by a developer running the suite.
	t.Setenv("PASEO_AGENT_CWD", "")
	workspace := t.TempDir()
	runtime := t.TempDir()
	bin := t.TempDir()
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	old, _ := os.Getwd()
	os.Chdir(workspace)
	defer os.Chdir(old)
	os.WriteFile(filepath.Join(runtime, "registry.json"), []byte(`{"workspace":"`+workspace+`","profiles":{"codex-superpowers":{"agent":"codex","workspace":"`+workspace+`","sandbox":"box"}}}`), 0600)
	os.WriteFile(filepath.Join(bin, "sbx"), []byte("#!/bin/sh\ncat\n"), 0700)
	var out strings.Builder
	code, err := LaunchIO(runtime, "codex-superpowers", []string{"app-server"}, strings.NewReader("{\"method\":\"ping\",\"params\":{}}\n"), &out, &strings.Builder{})
	if err != nil || code != 0 || !strings.Contains(out.String(), "ping") {
		t.Fatalf("%d %v %s", code, err, out.String())
	}
	os.WriteFile(filepath.Join(bin, "sbx"), []byte("#!/bin/sh\nexit 7\n"), 0700)
	in, writer := io.Pipe()
	defer in.Close()
	defer writer.Close()
	done := make(chan int, 1)
	go func() {
		code, _ := LaunchIO(runtime, "codex-superpowers", []string{"app-server"}, in, &strings.Builder{}, &strings.Builder{})
		done <- code
	}()
	select {
	case code := <-done:
		if code != 7 {
			t.Fatalf("exit %d", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("child exit blocked on stdin")
	}
}
func TestResolveRoutesBySessionCwd(t *testing.T) {
	base, _ := filepath.EvalSymlinks(t.TempDir())
	projects, _ := filepath.EvalSymlinks(t.TempDir())
	write := func(id, workspace string, profiles ...string) string {
		os.MkdirAll(workspace, 0755)
		entries := map[string]any{}
		for _, p := range profiles {
			entries[p] = map[string]string{"agent": "codex", "workspace": workspace, "sandbox": id}
		}
		b, _ := json.Marshal(map[string]any{"workspace": workspace, "profiles": entries, "active_profiles": profiles})
		os.MkdirAll(filepath.Join(base, id), 0700)
		os.WriteFile(filepath.Join(base, id, "registry.json"), b, 0600)
		return filepath.Join(base, id)
	}
	a := write("a", filepath.Join(projects, "A"), "codex-superpowers")
	b := write("b", filepath.Join(projects, "B"), "codex-superpowers")
	nested := write("n", filepath.Join(projects, "A", "inner"), "codex-superpowers")
	write("d", filepath.Join(projects, "D"), "codex-gsd-core")
	write("x", filepath.Join(base, "inside"), "codex-superpowers")
	// A deleted workspace sorts first but must not win the discovery fallback.
	write("0", filepath.Join(projects, "0gone"), "codex-superpowers")
	os.RemoveAll(filepath.Join(projects, "0gone"))
	os.Symlink(filepath.Join(projects, "B"), filepath.Join(projects, "link-b"))
	old, _ := os.Getwd()
	defer os.Chdir(old)
	os.Chdir(projects)
	for _, tc := range []struct {
		env, profile string
		args         []string
		want         string
	}{
		{filepath.Join(projects, "A", "src"), "codex-superpowers", []string{"app-server"}, a},
		{filepath.Join(projects, "B"), "codex-superpowers", nil, b},
		{filepath.Join(projects, "link-b", "x"), "codex-superpowers", nil, b},
		{filepath.Join(projects, "A", "inner", "deep"), "codex-superpowers", nil, nested},
		{filepath.Join(projects, "D"), "codex-superpowers", []string{"app-server"}, ""},
		{filepath.Join(projects, "A"), "codex-gsd-core", nil, ""},
		{filepath.Join(base, "inside"), "codex-superpowers", nil, ""},
		{"", "codex-superpowers", []string{"app-server"}, a},
		{"", "codex-superpowers", []string{"--version"}, a},
		{"", "codex-superpowers", []string{"exec"}, ""},
	} {
		t.Setenv("PASEO_AGENT_CWD", tc.env)
		got, err := Resolve(base, tc.profile, tc.args)
		if got != tc.want || (tc.want == "") != (err != nil) {
			t.Errorf("%s %s %v: got %q %v", tc.env, tc.profile, tc.args, got, err)
		}
	}
}
func TestExplicitSessionCwdIsEnforced(t *testing.T) {
	workspace, _ := filepath.EvalSymlinks(t.TempDir())
	other, _ := filepath.EvalSymlinks(t.TempDir())
	runtime := t.TempDir()
	bin := t.TempDir()
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	os.WriteFile(filepath.Join(runtime, "registry.json"), []byte(`{"workspace":"`+workspace+`","profiles":{"codex-superpowers":{"agent":"codex","workspace":"`+workspace+`","sandbox":"box"}}}`), 0600)
	os.WriteFile(filepath.Join(bin, "sbx"), []byte("#!/bin/sh\ncat\n"), 0700)
	// A session for another project must not fall back to this workspace.
	t.Setenv("PASEO_AGENT_CWD", other)
	if code, err := LaunchIO(runtime, "codex-superpowers", []string{"app-server"}, strings.NewReader(""), &strings.Builder{}, &strings.Builder{}); err == nil || code == 0 {
		t.Fatal("explicit cwd outside workspace accepted")
	}
	// RPC cwd must stay within the workspace chosen from PASEO_AGENT_CWD.
	t.Setenv("PASEO_AGENT_CWD", workspace)
	in := strings.NewReader(`{"method":"thread/start","params":{"cwd":"` + other + `"}}` + "\n")
	if code, err := LaunchIO(runtime, "codex-superpowers", []string{"app-server"}, in, &strings.Builder{}, &strings.Builder{}); err == nil || code == 0 {
		t.Fatal("RPC cwd in another workspace accepted")
	}
}
func TestSymlinkParentTraversalRejected(t *testing.T) {
	workspace := t.TempDir()
	outside := t.TempDir()
	os.Mkdir(filepath.Join(outside, "sub"), 0755)
	os.Symlink(filepath.Join(outside, "sub"), filepath.Join(workspace, "link"))
	for _, cwd := range []string{"link/../secret", workspace + "/link/../secret"} {
		if err := CheckWorkdirs(map[string]any{"cwd": cwd}, workspace); err == nil {
			t.Errorf("accepted escaping path %s", cwd)
		}
	}
}
func TestOptionalNullWorkdir(t *testing.T) {
	if err := CheckWorkdirs(map[string]any{"cwd": nil}, t.TempDir()); err != nil {
		t.Fatal(err)
	}
}
func TestDuplicateHeadersDoNotLeak(t *testing.T) {
	v := map[string]any{"url": "http://127.0.0.1:1234/mcp/agents?callerAgentId=parent", "headers": map[string]any{"Authorization": "Bearer HOST_SECRET"}, "http_headers": map[string]any{"Authorization": "Bearer HOST_SECRET"}}
	out, err := RewriteTree(v, func(string, map[string]string) (string, map[string]string, error) {
		return "http://host.docker.internal:4321/key", map[string]string{"Authorization": "Bearer SESSION"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	if strings.Contains(string(b), "HOST_SECRET") {
		t.Fatalf("credential leak %s", b)
	}
}

func TestACPMCPHeaders(t *testing.T) {
	var input map[string]any
	json.Unmarshal([]byte(`{"method":"session/new","params":{"mcpServers":[{"type":"http","name":"paseo","url":"http://localhost:6767/mcp/agents?callerAgentId=parent","headers":[{"name":"Authorization","value":"Bearer HOST_SECRET"}]}]}}`), &input)
	out, err := RewriteTree(input, func(_ string, h map[string]string) (string, map[string]string, error) {
		if h["Authorization"] != "Bearer HOST_SECRET" {
			t.Fatal("lost upstream credential")
		}
		return "http://host.docker.internal:12345/key", map[string]string{"Authorization": "Bearer SESSION"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	if strings.Contains(string(b), "HOST_SECRET") || !strings.Contains(string(b), `"headers":[{"name":"Authorization","value":"Bearer SESSION"}]`) {
		t.Fatalf("bad ACP header translation: %s", b)
	}
}

func TestAntigravityLaunchAndBoundaries(t *testing.T) {
	workspace, _ := filepath.EvalSymlinks(t.TempDir())
	runtime := t.TempDir()
	bin := t.TempDir()
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	t.Setenv("PASEO_AGENT_CWD", workspace)
	os.WriteFile(filepath.Join(runtime, "registry.json"), []byte(`{"workspace":"`+workspace+`","profiles":{"antigravity-cli-native":{"agent":"antigravity-cli","workspace":"`+workspace+`","sandbox":"agy-box"}}}`), 0600)
	os.WriteFile(filepath.Join(bin, "sbx"), []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >&2\ncat\n"), 0700)
	var out, stderr strings.Builder
	input := `{"role":"user","parts":[{"text":"hello"}]}` + "\n"
	in := strings.NewReader(input)
	code, err := LaunchIO(runtime, "antigravity-cli-native", []string{"--input-format", "stream-json", "--output-format", "stream-json", "--add-dir", workspace}, in, &out, &stderr)
	if err != nil || code != 0 || !strings.Contains(stderr.String(), "agy-box agy") || out.String() != input {
		t.Fatalf("%d %v stdout=%s stderr=%s", code, err, out.String(), stderr.String())
	}
	if code, err := LaunchIO(runtime, "antigravity-cli-native", []string{"--add-dir", t.TempDir()}, strings.NewReader(input), &strings.Builder{}, &strings.Builder{}); err == nil || code == 0 {
		t.Fatal("native directory outside workspace accepted")
	}
}

func TestNativePluginLaunchArguments(t *testing.T) {
	p, args, err := LaunchArguments([]string{"--input-format", "stream-json", "--paseo-dock-profile", "antigravity-cli-native"})
	if err != nil || p != "antigravity-cli-native" || strings.Join(args, " ") != "--input-format stream-json" {
		t.Fatalf("%s %v %v", p, args, err)
	}
	for _, args := range [][]string{{"--paseo-dock-profile"}, {"--input-format", "stream-json"}, {"--paseo-dock-profile", "a", "--paseo-dock-profile", "b"}} {
		if _, _, err := LaunchArguments(args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}
