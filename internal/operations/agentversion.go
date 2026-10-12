package operations

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var stableAgentClient = &http.Client{Timeout: 30 * time.Second}
var stableVersion = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

func agentPackage(agent string) (string, error) {
	switch agent {
	case "codex":
		return "@openai/codex", nil
	case "claude":
		return "@anthropic-ai/claude-code", nil
	default:
		return "", fmt.Errorf("no verified stable package source for agent %q", agent)
	}
}

func packageSource(name, version string) string {
	return "https://registry.npmjs.org/" + strings.ReplaceAll(name, "/", "%2F") + "/" + version
}

// ResolveStableAgent resolves the official npm stable channel to an exact version.
// It never falls back to a lock value or stores a mutable "latest" version.
func ResolveStableAgent(ctx context.Context, agent string) (version, source string, err error) {
	return resolveStableAgent(ctx, agent, stableAgentClient)
}

func resolveStableAgent(ctx context.Context, agent string, client *http.Client) (string, string, error) {
	name, err := agentPackage(agent)
	if err != nil {
		return "", "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, packageSource(name, "latest"), nil)
	if err != nil {
		return "", "", err
	}
	res, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("resolve stable %s: %w", agent, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("stable %s lookup returned HTTP %d", agent, res.StatusCode)
	}
	const limit = 1024 * 1024
	b, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return "", "", err
	}
	var metadata struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if len(b) > limit {
		return "", "", fmt.Errorf("stable metadata exceeds size limit")
	}
	if err := json.Unmarshal(b, &metadata); err != nil {
		return "", "", fmt.Errorf("invalid stable metadata: %w", err)
	}
	if metadata.Name != name || !stableVersion.MatchString(metadata.Version) {
		return "", "", fmt.Errorf("invalid stable package or version for %s", agent)
	}
	return metadata.Version, packageSource(name, metadata.Version), nil
}
