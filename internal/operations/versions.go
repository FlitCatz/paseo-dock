package operations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"paseo-sbx/internal/config"
	"paseo-sbx/internal/operationlock"
	"paseo-sbx/internal/sandboxupdate"
)

// checkVersions probes installed software only. Image lock fields select probe
// types and carry configuration provenance; they never supply confirmed values.
func (a *execution) checkVersions(target Target) (Result, error) {
	result := Result{Versions: map[string]string{}, VersionErrors: map[string]string{}}
	target, err := a.resolveTarget(target)
	if err != nil {
		return result, err
	}
	if target.Entry.Bundle() {
		return result, errors.New("version check is not supported for multi-agent containers yet; see the pinned image versions")
	}
	if !config.SupportedAdapter(target.Entry.Agent) {
		return result, errors.New("unsupported agent adapter")
	}
	release, err := operationlock.Acquire(a.ctx, target.Runtime, []string{operationlock.EntryKey(target.Entry)})
	if err != nil {
		return result, err
	}
	defer release()
	target, err = a.resolveTarget(target)
	if err != nil {
		return result, err
	}
	warning := "Version checks use sbx exec and may start a stopped sandbox."
	if target.Category == config.CategoryContainer {
		warning = "Version checks require a running Docker container."
	}
	result.Warnings = []string{warning}
	a.stage("warning", warning)
	var failures []error
	backend, err := a.backend(target.Category)
	if err != nil {
		return result, err
	}
	probe := func(args ...string) (string, error) {
		ctx, cancel := context.WithTimeout(a.ctx, observationTimeout)
		defer cancel()
		entry := target.Entry
		if target.Category != config.CategoryContainer {
			entry.Workspace = "/tmp"
		}
		var out, stderr boundedCapture
		err := backend.Exec(ctx, entry, args, nil, &out, &stderr)
		result.Changed = true
		if ctx.Err() != nil {
			err = errors.Join(err, ctx.Err())
		}
		if out.overflow || stderr.overflow {
			err = errors.Join(err, errors.New("status output exceeds capture limit"))
		}
		if err != nil && stderr.Len() > 0 {
			err = fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return strings.TrimSpace(out.String()), err
	}
	fail := func(id string, err error) {
		result.VersionErrors[id] = err.Error()
		failures = append(failures, fmt.Errorf("%s: %w", id, err))
	}
	confirm := func(id string, args ...string) {
		raw, err := probe(args...)
		if err == nil && raw == "" {
			err = errors.New("empty version response")
		}
		if err != nil {
			fail(id, err)
			return
		}
		result.Versions[id] = raw
		result.VersionCheckedAt = time.Now().UTC()
	}
	home, err := probe("printenv", "HOME")
	if err == nil && (!filepath.IsAbs(home) || filepath.Clean(home) != home || strings.ContainsAny(home, "\r\n")) {
		err = errors.New("invalid guest home")
	}
	if err != nil {
		fail("environment", err)
	} else {
		confirm("agent", config.AgentCommand(target.Entry.Agent), "--version")
		if target.Category != config.CategorySandbox {
			raw, err := probe("cat", home+"/.local/share/paseo-sbx/installed.json")
			var manifest struct {
				Agent string      `json:"agent"`
				Tool  string      `json:"tool"`
				Lock  config.Lock `json:"lock"`
			}
			if err == nil {
				err = json.Unmarshal([]byte(raw), &manifest)
			}
			if err == nil && (manifest.Agent != target.Entry.Agent || manifest.Tool != target.Entry.Tool) {
				err = errors.New("guest installation manifest identity mismatch")
			}
			if err != nil {
				fail("methodology", fmt.Errorf("cannot read guest installation manifest: %w", err))
			} else if target.Entry.Tool != "native" {
				var tool struct {
					Repository string `json:"repository"`
					Package    string `json:"package"`
				}
				err = json.Unmarshal(manifest.Lock.Tools[target.Entry.Tool], &tool)
				if err != nil {
					fail("methodology", err)
				} else {
					// Catalog validation prevents arbitrary manifest-defined executables.
					var reg config.Registry
					err = config.ReadJSON(filepath.Join(target.Runtime, "registry.json"), &reg)
					installer := ""
					if err == nil {
						for _, m := range reg.EffectiveCatalog().Methodologies {
							if m.ID == target.Entry.Tool {
								installer = m.Installer
								break
							}
						}
					}
					switch {
					case err != nil:
						fail("methodology", err)
					case installer == "git-plugin" && tool.Repository != "":
						confirm("methodology", "git", "-C", home+"/.local/share/paseo-sbx/marketplace/plugins/"+target.Entry.Tool, "rev-parse", "HEAD")
					case installer == "openspec":
						confirm("methodology", "openspec", "--version")
					default:
						fail("methodology", errors.New("installed methodology has no supported current-version probe; image lock is unconfirmed"))
					}
				}
			}
			raw, err = probe("cat", home+"/.local/share/paseo-sbx/utilities/installed.json")
			readFailed := err != nil
			var utilities struct {
				Agent     string                     `json:"agent"`
				Utilities map[string]json.RawMessage `json:"utilities"`
			}
			if err == nil {
				err = json.Unmarshal([]byte(raw), &utilities)
				if err == nil && (utilities.Agent != target.Entry.Agent || utilities.Utilities == nil) {
					err = errors.New("invalid utility installation manifest")
				}
			}
			if err != nil {
				// An absent utilities manifest is normal for images with no selected utilities.
				// Only suppress this when the registry selection independently confirms none.
				var reg config.Registry
				if !readFailed || config.ReadJSON(filepath.Join(target.Runtime, "registry.json"), &reg) != nil || reg.Selection == nil || len(reg.Selection.Utilities) > 0 {
					fail("utilities", fmt.Errorf("cannot read guest utility manifest: %w", err))
				}
			} else {
				ids := sortedRawKeys(utilities.Utilities)
				for _, id := range ids {
					// Utility IDs may equal component names; reserve those keys for
					// their actual probes in results, errors and the persisted ledger.
					key := "utility:" + id
					var utility struct {
						Installer string `json:"installer"`
					}
					err = json.Unmarshal(utilities.Utilities[id], &utility)
					if err != nil {
						fail(key, err)
						continue
					}
					if utility.Installer == "" {
						utility.Installer = id
					}
					switch utility.Installer {
					case "rtk", "headroom":
						confirm(key, utility.Installer, "--version")
					default:
						fail(key, fmt.Errorf("unsupported utility version probe %s", utility.Installer))
					}
				}
			}
		}
	}
	if a.ctx.Err() != nil && result.Changed {
		result.NeedsVerification = true
	}
	identity := sandboxupdate.Identity(target.Entry)
	check := VersionCheck{Sandbox: identity, Image: target.Entry.Image, CheckedAt: result.VersionCheckedAt, Versions: result.Versions, Errors: result.VersionErrors}
	// The host ledger records partial observations without changing updater attempts.
	if err := a.writeJSON(observationPath(target.Runtime, "version-checks", identity), check); err != nil {
		fail("record", err)
	} else {
		result.Changed = true
	}
	return result, errors.Join(append(failures, a.ctx.Err())...)
}
func sortedRawKeys(entries map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(entries))
	for id := range entries {
		keys = append(keys, id)
	}
	slices.Sort(keys)
	return keys
}
