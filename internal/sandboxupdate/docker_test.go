package sandboxupdate

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"paseo-dock/internal/config"
)

// Opt-in integration test: use a disposable container, never a user's sandbox.
func TestDockerUpdate(t *testing.T) {
	image := os.Getenv("PASEO_DOCK_UPDATE_TEST_IMAGE")
	if image == "" {
		t.Skip("set PASEO_DOCK_UPDATE_TEST_IMAGE for a disposable-container smoke test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	b, err := exec.CommandContext(ctx, "docker", "run", "--detach", "--entrypoint", "sleep", image, "300").CombinedOutput()
	if err != nil {
		t.Fatalf("start container: %v %s", err, b)
	}
	id := strings.TrimSpace(string(b))
	defer exec.Command("docker", "rm", "--force", id).Run()
	run := func(ctx context.Context, args []string, capture bool) (string, error) {
		// Simulate an older installed version while exercising real downloads,
		// checksum checks, installation and executable activation.
		if os.Getenv("PASEO_DOCK_UPDATE_TEST_FORCE_DOWNLOAD") == "1" && capture {
			if len(args) == 2 && args[1] == "--version" && (args[0] == "agy" || args[0] == "rtk") {
				return "0.0.0", nil
			}
			if len(args) > 2 && args[0] == "uv" && args[1] == "pip" && args[2] == "list" {
				return `[{}]`, nil
			}
		}
		cmd := exec.CommandContext(ctx, "docker", append([]string{"exec", "--workdir", "/tmp", id}, args...)...)
		if capture {
			b, err := cmd.Output()
			return strings.TrimSpace(string(b)), err
		}
		b, err := cmd.CombinedOutput()
		if len(b) > 0 {
			t.Log(string(b))
		}
		return "", err
	}
	if utilities := os.Getenv("PASEO_DOCK_UPDATE_TEST_UTILITIES"); utilities != "" {
		if _, err := run(ctx, []string{"bash", "/opt/paseo-sbx/install/install-utilities.sh", "codex", utilities}, false); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := run(ctx, []string{"cat", "/home/agent/.local/share/paseo-sbx/installed.json"}, true)
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Agent string `json:"agent"`
		Tool  string `json:"tool"`
	}
	if err = json.Unmarshal([]byte(raw), &manifest); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("PASEO_DOCK_UPDATE_TEST_OLDER_PLUGIN") == "1" {
		dir := "/home/agent/.local/share/paseo-sbx/marketplace/plugins/" + manifest.Tool
		for _, args := range [][]string{{"git", "-C", dir, "fetch", "--depth", "2", "origin", "HEAD"}, {"git", "-C", dir, "checkout", "--detach", "FETCH_HEAD~1"}} {
			if _, err := run(ctx, args, false); err != nil {
				t.Fatal(err)
			}
		}
	}
	s := refresh(ctx, config.Entry{Agent: manifest.Agent, Tool: manifest.Tool, Sandbox: id}, run, time.Now())
	t.Logf("result: %+v", s)
	if len(s.Errors) > 0 {
		t.Fatal(s.Errors)
	}
	if s.Versions["agent"] == "" {
		t.Fatal("agent version missing")
	}
	// Read the active command directly so forced-download probes don't affect this check.
	b, err = exec.CommandContext(ctx, "docker", "exec", id, config.AgentCommand(manifest.Agent), "--version").Output()
	version := strings.TrimSpace(string(b))
	if err != nil || version != s.Versions["agent"] {
		t.Fatalf("active agent: %q %v", version, err)
	}
}
