package operations

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"paseo-dock/internal/config"
	"paseo-dock/internal/process"
)

func (a *execution) validatePaseoConfig(home string) error {
	ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
	defer cancel()
	if a.ValidatePaseoConfig != nil {
		return a.ValidatePaseoConfig(ctx, home)
	}
	// get parses the entire persisted config, including provider inheritance.
	// Reading only version avoids printing configuration secrets. Unlike provider
	// ls, this command neither needs nor starts a running daemon.
	cmd := process.Command(ctx, "paseo", "--home", home, "daemon", "config", "get", "version", "--json")
	cmd.Dir = a.Root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		var result struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		// CLI errors are JSON on stderr; desktop launchers can prefix that
		// stream with native diagnostics. Also accept stdout for CLI versions
		// that send command errors there.
		for _, stream := range [][]byte{output, stderr.Bytes()} {
			if start := bytes.IndexByte(stream, '{'); start >= 0 {
				if json.Unmarshal(stream[start:], &result) == nil && result.Error.Message != "" {
					return fmt.Errorf("paseo daemon config get: %w: %s", err, result.Error.Message)
				}
			}
		}
		return fmt.Errorf("paseo daemon config get: %w: %s", err, strings.TrimSpace(string(output)+stderr.String()))
	}
	return nil
}

// Probe each distinct base against the installed Paseo schema in an isolated
// home. This discovers support even when the real config is already broken or
// the daemon is stopped, without maintaining a version-specific provider list.
func (a *execution) filterProviderBases(generated map[string]any) error {
	providers := object(object(generated, "agents"), "providers")
	ids := make([]string, 0, len(providers))
	for id := range providers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	known := map[string]bool{}
	probeHome, err := os.MkdirTemp("", "paseo-dock-provider-check-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(probeHome)
	for _, id := range ids {
		provider, ok := providers[id].(map[string]any)
		if !ok {
			return fmt.Errorf("invalid generated provider %q", id)
		}
		base, ok := provider["extends"].(string)
		if !ok || base == "" {
			return fmt.Errorf("generated provider %q has no base", id)
		}
		base = config.ProviderAdapter(base)
		provider["extends"] = base
		supported, checked := known[base]
		if !checked {
			probe := map[string]any{"version": 1, "agents": map[string]any{"providers": map[string]any{
				"paseo-dock-base-probe": map[string]any{"label": "paseo-dock base probe", "extends": base, "command": []string{"paseo-dock-base-probe"}},
			}}}
			if err := config.WriteJSON(filepath.Join(probeHome, "config.json"), probe); err != nil {
				return err
			}
			err := a.validatePaseoConfig(probeHome)
			supported = err == nil
			// Only an explicit unknown-base diagnostic means unsupported. Missing CLI,
			// cancellation, schema changes, and other failures must stop the sync.
			if err != nil && !strings.Contains(err.Error(), fmt.Sprintf("extends unknown provider %q", base)) {
				return fmt.Errorf("check Paseo base provider %q: %w", base, err)
			}
			known[base] = supported
		}
		if !supported {
			fmt.Fprintf(a.Err, "Warning: skipping provider %q: base %q is not known to installed Paseo\n", id, base)
			delete(providers, id)
		}
	}
	return nil
}
