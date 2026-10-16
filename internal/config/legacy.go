package config

import (
	"maps"
	"os"
	"slices"
	"strings"
)

// The project was renamed from paseo-sbx to paseo-dock. These names are
// persisted in existing containers, images, registries and Paseo configs, so
// they deliberately keep the old name:
//   - Docker labels io.paseo-sbx.* and container names paseo-sbx-<hash>
//   - in-image paths /opt/paseo-sbx and ~/.local/share/paseo-sbx, the in-container
//     plugin marketplace paseo-sbx, and the /tmp/paseo-sbx-agent-ready marker
//   - the host data folder ~/.local/share/paseo-sbx (BaseDir)
//   - the launcher flag --paseo-sbx-profile and the paseo-sbx-relay-ready handshake
//   - provider IDs and the image namespace recorded in existing registry entries

// DefaultNamespace names images of workspaces without a recorded namespace.
const DefaultNamespace = "paseo-dock"

// Getenv reads PASEO_DOCK_<name>, falling back to the legacy PASEO_SBX_<name>.
// A non-empty new variable wins when both are set.
func Getenv(name string) string {
	if v := os.Getenv("PASEO_DOCK_" + name); v != "" {
		return v
	}
	return os.Getenv("PASEO_SBX_" + name)
}

// RecordedNamespace returns the image namespace an existing workspace was
// created with, preferring entries of the given profiles. Empty means none.
func RecordedNamespace(r Registry, profiles []string) string {
	keys := append(slices.Clone(profiles), slices.Sorted(maps.Keys(r.Profiles))...)
	for _, key := range keys {
		e, ok := r.Profiles[key]
		if !ok {
			continue
		}
		if i := strings.LastIndex(e.Image, "/"+key+":"); i > 0 {
			return e.Image[:i]
		}
	}
	return ""
}
