// Package config owns the lock file, trusted registry and canonical workspace paths.
package config

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var Profiles = DefaultCatalog().AllProfiles()

type Lock struct {
	Schema     int    `json:"schema"`
	Tag        string `json:"tag"`
	MinimumSBX string `json:"minimum_sbx"`
	Paseo      string `json:"paseo"`
	Node       struct {
		Version string            `json:"version"`
		SHA256  map[string]string `json:"sha256"`
	} `json:"node"`
	BaseImages     map[string]string          `json:"base_images"`
	Agents         map[string]string          `json:"agents"`
	Tools          map[string]json.RawMessage `json:"tools"`
	Utilities      map[string]json.RawMessage `json:"utilities,omitempty"`
	AgentArtifacts map[string]json.RawMessage `json:"agent_artifacts,omitempty"`
	// ACPAdapters pins the ACP servers installed for agents connected over acp.
	ACPAdapters map[string]ACPAdapter `json:"acp_adapters,omitempty"`
}

type Entry struct {
	SecurityPolicy       string        `json:"security_policy,omitempty"`
	SecurityPolicyDigest string        `json:"security_policy_digest,omitempty"`
	Category             Category      `json:"category,omitempty"`
	ContainerID          string        `json:"container_id,omitempty"`
	ImageID              string        `json:"image_id,omitempty"`
	Network              string        `json:"network,omitempty"`
	Ports                []PortMapping `json:"ports,omitempty"`
	Utilities            []string      `json:"utilities,omitempty"`
	Agent                string        `json:"agent"`
	Tool                 string        `json:"tool"`
	Workspace            string        `json:"workspace"`
	Sandbox              string        `json:"sandbox"`
	Image                string        `json:"image"`
	// Name marks a bundle: one container holding every selected pair.
	Name        string            `json:"name,omitempty"`
	Pairs       []Pair            `json:"pairs,omitempty"`
	Connections map[string]string `json:"connections,omitempty"`
}
type Registry struct {
	Schema    int              `json:"schema,omitempty"`
	Workspace string           `json:"workspace"`
	Profiles  map[string]Entry `json:"profiles"`
	Active    []string         `json:"active_profiles"`
	Catalog   *Catalog         `json:"catalog,omitempty"`
	Selection *Selection       `json:"selection,omitempty"`
	// DisabledProviders lists ACP providers whose last check failed; the
	// other agents of the same container stay registered.
	DisabledProviders []string `json:"disabled_providers,omitempty"`
}

func Load(root string) (Lock, error) {
	var lock Lock
	err := ReadJSON(filepath.Join(root, "versions.lock.json"), &lock)
	if err == nil && (lock.Schema != 1 || lock.Tag == "" || len(lock.BaseImages) == 0) {
		err = fmt.Errorf("invalid versions lock")
	}
	return lock, err
}
func ReadJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}
func WriteJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, 10)
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".paseo-dock-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
func Canonical(path string) (string, error) {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if path == "~" {
			path = home
		} else {
			path = home + "/" + strings.TrimPrefix(path, "~/")
		}
	}
	if !filepath.IsAbs(path) {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		path = cwd + "/" + path
	}
	// Resolve components before processing '..'; lexical cleaning can hide a
	// symlink escape. Missing suffixes are valid for future workspace paths.
	pending := strings.Split(path, "/")
	resolved := "/"
	links := 0
	for len(pending) > 0 {
		part := pending[0]
		pending = pending[1:]
		if part == "" || part == "." {
			continue
		}
		if part == ".." {
			resolved = filepath.Dir(resolved)
			continue
		}
		candidate := filepath.Join(resolved, part)
		info, err := os.Lstat(candidate)
		if os.IsNotExist(err) {
			resolved = candidate
			continue
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			links++
			if links > 255 {
				return "", fmt.Errorf("too many symlinks in workspace path")
			}
			target, err := os.Readlink(candidate)
			if err != nil {
				return "", err
			}
			if filepath.IsAbs(target) {
				resolved = "/"
			}
			pending = append(strings.Split(target, "/"), pending...)
		} else {
			resolved = candidate
		}
	}
	return resolved, nil
}
func Within(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// DefaultNamespace names images when no namespace is given.
const DefaultNamespace = "paseo-dock"

// BaseDir holds every trusted workspace runtime and the shared launcher.
func BaseDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "paseo-dock"), nil
}
func RuntimePath(workspace string) (string, error) {
	base, err := BaseDir()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(workspace))
	return filepath.Join(base, fmt.Sprintf("%x", sum)[:10]), nil
}
func LauncherPath(base string) string { return filepath.Join(base, "launcher", "agent-wrapper") }

type Installed struct {
	Runtime  string
	Registry Registry
}

// Missing reports a registered workspace whose directory no longer exists.
func (i Installed) Missing() bool {
	info, err := os.Stat(i.Registry.Workspace)
	return os.IsNotExist(err) || (err == nil && !info.IsDir())
}

// Registries lists workspace registries under base, sorted by runtime path.
// An unreadable registry must never look like an absent workspace to provider sync.
func Registries(base string) ([]Installed, error) {
	dirs, err := os.ReadDir(base)
	if os.IsNotExist(err) {
		return []Installed{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list registries %s: %w", base, err)
	}
	out := []Installed{}
	for _, dir := range dirs {
		if !dir.IsDir() && dir.Type()&os.ModeSymlink == 0 {
			continue
		}
		path := filepath.Join(base, dir.Name(), "registry.json")
		var reg Registry
		if err := ReadJSON(path, &reg); err != nil {
			if os.IsNotExist(err) {
				if _, statErr := os.Lstat(path); os.IsNotExist(statErr) {
					continue // Launcher/lock directories need not have registries.
				}
			}
			return nil, fmt.Errorf("read registry %s: %w", path, err)
		}
		if reg.Workspace == "" {
			return nil, fmt.Errorf("invalid registry %s: missing workspace", path)
		}
		out = append(out, Installed{Runtime: filepath.Dir(path), Registry: reg})
	}
	return out, nil
}
func ValidProfile(profile string) bool {
	for _, p := range Profiles {
		if p == profile {
			return true
		}
	}
	return false
}
func FindRoot(start string) string {
	for path := start; ; path = filepath.Dir(path) {
		if _, err := os.Stat(filepath.Join(path, "versions.lock.json")); err == nil {
			if _, err := os.Stat(filepath.Join(path, "Dockerfile")); err == nil {
				return path
			}
		}
		if filepath.Dir(path) == path {
			return ""
		}
	}
}
