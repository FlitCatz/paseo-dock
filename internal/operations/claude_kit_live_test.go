package operations

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Opt-in runtime regression: nested kit mounts must not leave Claude's parent
// config directory root-owned. Uses one disposable container and an empty mount.
func TestClaudeSandboxConfigSurvivesKitMount(t *testing.T) {
	image := os.Getenv("PASEO_DOCK_LIVE_CLAUDE_IMAGE")
	if image == "" {
		t.Skip("set PASEO_DOCK_LIVE_CLAUDE_IMAGE to an exact disposable image ID")
	}
	if !strings.HasPrefix(image, "sha256:") || !dockerExactID.MatchString(strings.TrimPrefix(image, "sha256:")) {
		t.Fatal("exact image ID required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	data, err := exec.CommandContext(ctx, "docker", "image", "inspect", image).Output()
	if err != nil {
		t.Fatal(err)
	}
	var images []struct {
		Config struct{ Labels map[string]string }
	}
	if err := json.Unmarshal(data, &images); err != nil {
		t.Fatal(err)
	}
	if len(images) != 1 || images[0].Config.Labels["io.paseo-dock.category"] != "sandbox" || images[0].Config.Labels["io.paseo-dock.agent"] != "claude" {
		t.Fatal("requires disposable agent-only Claude image")
	}
	cmd := exec.CommandContext(ctx, "docker", "run", "--rm", "--network", "none", "--user", "agent", "--mount", "type=bind,src="+t.TempDir()+",dst=/home/agent/.claude/plugins/kit-probe,readonly", image, "sh", "-c", "set -e; test $(stat -c %u /home/agent/.claude) = $(id -u); printf kit-probe > /home/agent/.claude/settings.json; test $(cat /home/agent/.claude/settings.json) = kit-probe")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Claude kit config write failed: %v\n%s", err, out)
	}
}
