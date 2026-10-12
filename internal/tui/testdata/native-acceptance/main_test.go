package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"paseo-sbx/internal/config"
	"paseo-sbx/internal/launcher"
	"strings"
	"testing"
)

// Opt in only against the already created, exact disposable acceptance resource.
func TestRealDockerNativePrivateSchema(t *testing.T) {
	if os.Getenv("NATIVE_ACCEPTANCE_LIVE") != "1" {
		t.Skip("requires disposable live native acceptance")
	}
	t.Setenv("HOME", root+"/home")
	t.Setenv("PASEO_HOME", root+"/paseo")
	t.Setenv("DOCKER_HOST", "unix:///Users/dev-r/.orbstack/run/docker.sock")
	runtime, e := config.RuntimePath(root + "/workspace")
	if e != nil {
		t.Fatal(e)
	}
	var reg config.Registry
	if e = config.ReadJSON(runtime+"/registry.json", &reg); e != nil {
		t.Fatal(e)
	}
	entry := reg.Profiles["container/antigravity-cli-native"]
	if entry.ContainerID != "10f4af99d91d6c1a4a28750a2f1f053e3caeb2242f8dcf033f8b2db986f765be" || entry.Workspace != root+"/workspace" {
		t.Fatal("unexpected disposable target")
	}
	asset := root + "/paseo/plugin-data/antigravity-cli/schema.json"
	if e = os.WriteFile(asset, []byte(`{"type":"object"}`), 0600); e != nil {
		t.Fatal(e)
	}
	t.Chdir(entry.Workspace)
	var out, stderr bytes.Buffer
	_, e = launcher.LaunchIO(runtime, "container-antigravity-cli-native", []string{"--input-format", "stream-json", "--output-format", "stream-json", "--json-schema", asset}, strings.NewReader(""), &out, &stderr)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(stderr.String(), "invalid --json-schema") || strings.Contains(stderr.String(), "permission denied") {
		t.Fatalf("private copied schema inaccessible: %s", stderr.String())
	}
	resultFound := false
	scanner := bufio.NewScanner(&out)
	for scanner.Scan() {
		var frame struct {
			Event  string `json:"event"`
			Result *struct {
				Status string `json:"status"`
				Turns  int    `json:"num_turns"`
				Usage  struct {
					Total int `json:"total_tokens"`
				} `json:"usage"`
			} `json:"result"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &frame); err != nil {
			t.Fatalf("invalid real native frame: %v", err)
		}
		if frame.Event == "result" && frame.Result != nil {
			if frame.Result.Turns != 0 || frame.Result.Usage.Total != 0 {
				t.Fatal("empty-input acceptance unexpectedly ran a model turn")
			}
			resultFound = true
			t.Logf("real native result status=%s turns=%d total_tokens=%d", frame.Result.Status, frame.Result.Turns, frame.Result.Usage.Total)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if !resultFound {
		t.Fatalf("real native protocol result absent: stderr=%s", stderr.String())
	}
}
