package operations

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"paseo-sbx/internal/config"
	"paseo-sbx/internal/process"
	"paseo-sbx/internal/sandboxupdate"
)

type Query struct {
	IncludePaseo, IncludeDocker, IncludeTemplates bool
	PaseoHome                                     string
}
type Connection struct {
	CheckedAt     time.Time
	Error, Status string
}
type Snapshot struct {
	Workspaces                    []Workspace
	Unmanaged                     []Sandbox
	SBX, Paseo, Docker, Templates Connection
	TemplateRows                  []Template
}
type Template struct {
	Repository string `json:"repository"`
	Tag        string `json:"tag"`
	ID         string `json:"id"`
}
type Workspace struct {
	ID, Path, Runtime string
	Missing           bool
	Selection         *config.Selection
	Sandboxes         []Sandbox
	Error             string
}
type Sandbox struct {
	Target            Target
	RuntimeState      string
	Active            bool
	Registration      string
	RegistrationError string
	ImageMismatch     bool
	Update            *sandboxupdate.State
	// InitialVersions holds configured image-lock provenance, never confirmed guest versions.
	InitialVersions map[string]string
	VersionCheck    *VersionCheck
}

// VersionCheck is independent of updater attempts and never changes their retry policy.
type VersionCheck struct {
	Sandbox   string            `json:"sandbox"`
	Image     string            `json:"image"`
	CheckedAt time.Time         `json:"checked_at"`
	Versions  map[string]string `json:"versions,omitempty"`
	Errors    map[string]string `json:"errors,omitempty"`
}

const observationLimit = 1 << 20
const observationTimeout = 5 * time.Second

type boundedCapture struct {
	bytes.Buffer
	overflow bool
}

func (b *boundedCapture) Write(p []byte) (int, error) {
	n := len(p)
	remaining := observationLimit - b.Len()
	if len(p) > remaining {
		b.overflow = true
		p = p[:remaining]
	}
	_, _ = b.Buffer.Write(p)
	return n, nil
}

// observe captures both streams within fixed bounds without changing shared Service streams.
func (s *Service) observe(ctx context.Context, args ...string) (string, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, observationTimeout)
	defer cancel()
	cmd := process.Command(ctx, args[0], args[1:]...)
	cmd.Dir = s.Root
	var out, stderr boundedCapture
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	err := cmd.Run()
	started := cmd.Process != nil
	if ctx.Err() != nil {
		err = errors.Join(ctx.Err(), err)
	}
	if out.overflow || stderr.overflow {
		err = errors.Join(err, errors.New("status output exceeds capture limit"))
	}
	if err != nil && stderr.Len() > 0 {
		err = fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return out.String(), started, err
}
func connection(err error) Connection {
	c := Connection{CheckedAt: time.Now(), Status: "connected"}
	if err != nil {
		c.Status = "error"
		c.Error = err.Error()
	}
	return c
}
func decodeRows[T any](raw, wrapper string) ([]T, error) {
	b := bytes.TrimSpace([]byte(raw))
	if len(b) == 0 {
		return nil, errors.New("empty status response")
	}
	if b[0] == '{' {
		var v map[string]json.RawMessage
		if err := json.Unmarshal(b, &v); err != nil {
			return nil, err
		}
		value, ok := v[wrapper]
		if !ok {
			return nil, fmt.Errorf("unrecognized status wrapper; expected %s", wrapper)
		}
		b = bytes.TrimSpace(value)
	}
	if len(b) == 0 || b[0] != '[' {
		return nil, errors.New("status rows must be an array")
	}
	var rows []T
	if err := json.Unmarshal(b, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

type runtimeRow struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

func runtimeState(status string) string {
	switch strings.ToLower(status) {
	case "running":
		return "running"
	case "stopped":
		return "stopped"
	default:
		return "unknown"
	}
}
func (s *Service) Snapshot(ctx context.Context, q Query) (Snapshot, error) {
	snap := Snapshot{Workspaces: []Workspace{}, Unmanaged: []Sandbox{}}
	if err := ctx.Err(); err != nil {
		return snap, err
	}
	base, err := config.BaseDir()
	if err != nil {
		return snap, err
	}
	base, err = config.Canonical(base)
	if err != nil {
		return snap, err
	}
	home := q.PaseoHome
	if home == "" {
		home = "~/.paseo"
	}
	home, homeErr := config.Canonical(home)
	providers, providerErr := readProviders(home)
	if homeErr != nil {
		providerErr = homeErr
	}
	sbx, _ := s.backend(config.CategorySandbox)
	states, sbxErr := sbx.List(ctx)
	var rows []runtimeRow
	for _, state := range states {
		status := "unknown"
		if state.Known {
			status = "stopped"
			if state.Running {
				status = "running"
			}
		}
		rows = append(rows, runtimeRow{Name: state.ID, Status: status})
	}
	dockerStates := map[string]RuntimeState{}
	var dockerErr error
	dockerObserved := false
	observeDocker := func() {
		if dockerObserved {
			return
		}
		dockerObserved = true
		backend, _ := s.backend(config.CategoryContainer)
		var states []RuntimeState
		states, dockerErr = backend.List(ctx)
		for _, state := range states {
			dockerStates[state.ID] = state
		}
		snap.Docker = connection(dockerErr)
	}
	if q.IncludeDocker {
		observeDocker()
	}
	byName := map[string]runtimeRow{}
	if sbxErr == nil {
		for _, r := range rows {
			if r.Name == "" {
				sbxErr = errors.New("sandbox status row has no name")
				break
			}
			if _, exists := byName[r.Name]; exists {
				sbxErr = fmt.Errorf("duplicate sandbox status %s", r.Name)
				break
			}
			byName[r.Name] = r
		}
	}
	snap.SBX = connection(sbxErr)
	paths, err := filepath.Glob(filepath.Join(base, "*", "registry.json"))
	if err != nil {
		return snap, err
	}
	managed := map[string]bool{}
	for _, path := range paths {
		runtime, err := config.Canonical(filepath.Dir(path))
		if err != nil {
			return snap, err
		}
		w := Workspace{ID: runtime, Runtime: runtime, Sandboxes: []Sandbox{}}
		var reg config.Registry
		err = config.ReadJSON(path, &reg)
		if err == nil {
			err = validateObservedRegistry(reg, runtime)
		}
		if err != nil {
			w.Error = err.Error()
			snap.Workspaces = append(snap.Workspaces, w)
			continue
		}
		w.Path = reg.Workspace
		w.Selection = reg.Selection
		info, statErr := os.Stat(w.Path)
		w.Missing = os.IsNotExist(statErr) || (statErr == nil && !info.IsDir())
		if statErr != nil && !os.IsNotExist(statErr) {
			w.Error = statErr.Error()
		}
		for _, profile := range sortedProfiles(reg.Profiles) {
			entry := reg.Profiles[profile]
			if entry.EffectiveCategory() != config.CategoryContainer {
				managed[entry.Sandbox] = true
			}
			row := Sandbox{Target: Target{Category: entry.EffectiveCategory(), Workspace: w.Path, Runtime: runtime, Profile: profile, Entry: entry}, Active: reg.IsActive(profile), RuntimeState: "unknown", Registration: entryRegistration(providers, providerErr, base, profile, entry), InitialVersions: s.initialVersions(reg, profile, entry)}
			if providerErr != nil {
				row.RegistrationError = fmt.Sprintf("%s: %v", filepath.Join(home, "config.json"), providerErr)
			}
			if entry.EffectiveCategory() == config.CategoryContainer {
				observeDocker()
				if dockerErr == nil {
					row.RuntimeState = "missing"
					if state, ok := dockerStates[entry.ContainerID]; ok {
						row.RuntimeState = "unknown"
						if state.Known {
							row.RuntimeState = "stopped"
							if state.Running {
								row.RuntimeState = "running"
							}
						}
					}
				}
			} else if sbxErr == nil {
				row.RuntimeState = "missing"
				if r, ok := byName[entry.Sandbox]; ok {
					row.RuntimeState = runtimeState(r.Status)
				}
			}
			row.ImageMismatch = s.imageMismatch(reg, profile, entry.Image)
			var state sandboxupdate.State
			identity := sandboxupdate.Identity(entry)
			if config.ReadJSON(observationPath(runtime, "updates", identity), &state) == nil && state.Sandbox == identity && state.Image == entry.Image {
				row.Update = &state
			}
			var checked VersionCheck
			if config.ReadJSON(observationPath(runtime, "version-checks", identity), &checked) == nil && checked.Sandbox == identity && checked.Image == entry.Image {
				row.VersionCheck = &checked
			}
			w.Sandboxes = append(w.Sandboxes, row)
		}
		snap.Workspaces = append(snap.Workspaces, w)
	}
	if sbxErr == nil {
		for _, r := range rows {
			if !managed[r.Name] {
				snap.Unmanaged = append(snap.Unmanaged, Sandbox{Target: Target{Entry: config.Entry{Sandbox: r.Name}}, RuntimeState: runtimeState(r.Status), Registration: "unmanaged"})
			}
		}
	}
	if q.IncludePaseo {
		raw, _, err := s.observe(ctx, "paseo", "--home", home, "status", "--json")
		var status struct {
			LocalDaemon string `json:"localDaemon"`
		}
		if err == nil {
			err = json.Unmarshal([]byte(raw), &status)
			if err == nil && status.LocalDaemon == "" {
				err = errors.New("Paseo status has no localDaemon")
			}
		}
		snap.Paseo = connection(err)
		if err == nil {
			snap.Paseo.Status = status.LocalDaemon
		}
	}
	if q.IncludeTemplates {
		raw, _, err := s.observe(ctx, "sbx", "template", "ls", "--json")
		if err == nil {
			snap.TemplateRows, err = decodeRows[Template](raw, "images")
			if err == nil {
				for _, r := range snap.TemplateRows {
					if r.Repository == "" || r.Tag == "" || r.ID == "" {
						err = errors.New("template status row lacks repository, tag or id")
						break
					}
				}
			}
		}
		snap.Templates = connection(err)
		if err != nil {
			snap.TemplateRows = nil
		}
	}
	return snap, ctx.Err()
}
func validateObservedRegistry(reg config.Registry, runtime string) error {
	if reg.Workspace == "" || reg.Profiles == nil {
		return errors.New("invalid workspace registry")
	}
	w, err := config.Canonical(reg.Workspace)
	if err != nil {
		return err
	}
	if w != reg.Workspace {
		return errors.New("registry workspace is not canonical")
	}
	expected, err := config.RuntimePath(w)
	if err != nil {
		return err
	}
	expected, err = config.Canonical(expected)
	if err != nil {
		return err
	}
	if expected != runtime {
		return errors.New("registry runtime identity mismatch")
	}
	for profile, e := range reg.Profiles {
		if profile == "" || e.Workspace != w || (e.EffectiveCategory() == config.CategoryContainer && e.ContainerID == "") || sandboxupdate.Identity(e) == "" {
			return errors.New("invalid sandbox registry entry")
		}
	}
	return nil
}
func observationPath(runtime, dir, sandbox string) string {
	sum := sha256.Sum256([]byte(sandbox))
	return filepath.Join(runtime, dir, fmt.Sprintf("%x.json", sum[:8]))
}
func readProviders(home string) (map[string]any, error) {
	var current map[string]any
	err := config.ReadJSON(filepath.Join(home, "config.json"), &current)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, errors.New("Paseo config must be an object")
	}
	if _, exists := current["agents"]; !exists {
		return nil, nil
	}
	agents, ok := current["agents"].(map[string]any)
	if !ok {
		return nil, errors.New("Paseo agents must be an object")
	}
	if _, exists := agents["providers"]; !exists {
		return nil, nil
	}
	providers, ok := agents["providers"].(map[string]any)
	if !ok {
		return nil, errors.New("Paseo providers must be an object")
	}
	for profile, value := range providers {
		if _, ok := value.(map[string]any); !ok {
			return nil, fmt.Errorf("Paseo provider %s must be an object", profile)
		}
	}
	return providers, nil
}
func providerRegistration(providers map[string]any, err error, base, profile, agent string) string {
	if err != nil {
		return "unknown"
	}
	// Native Antigravity belongs to the Paseo plugin, not to a managed launcher provider.
	if agent == "antigravity-cli" {
		return "unmanaged"
	}
	p, ok := providers[profile].(map[string]any)
	if !ok || p["enabled"] != true || p["extends"] != config.ProviderAdapter(agent) {
		return "unregistered"
	}
	command, ok := p["command"].([]any)
	if !ok || len(command) != 2 || command[0] != config.LauncherPath(base) || command[1] != profile {
		return "unregistered"
	}
	return "registered"
}
func (s *Service) imageMismatch(reg config.Registry, profile, image string) bool {
	entry := reg.Profiles[profile]
	if entry.EffectiveCategory() == config.CategorySandbox {
		return false
	}
	if entry.Bundle() {
		tag, _, err := bundleImageSpec(reg.EffectiveCatalog(), entry, s.Lock)
		return err != nil || !strings.HasSuffix(image, "/"+profile+":"+tag)
	}
	if entry.EffectiveCategory() == config.CategoryContainer {
		tag, _, err := ImageSpec(config.CategoryContainer, entry, s.Lock)
		return err != nil || !strings.HasSuffix(image, "/"+profile+":"+tag)
	}
	tag := s.Lock.Tag
	if reg.Selection != nil {
		selection := *reg.Selection
		pair, ok := reg.EffectiveCatalog().Pair(profile)
		if !ok {
			return true
		}
		selection.Pairs = []config.Pair{pair}
		tag = config.ImageTag(s.Lock, selection, reg.EffectiveCatalog())
	}
	return tag != "" && !strings.HasSuffix(image, "/"+profile+":"+tag)
}
func (s *Service) initialVersions(reg config.Registry, profile string, entry config.Entry) map[string]string {
	versions := map[string]string{}
	if entry.EffectiveCategory() == config.CategorySandbox {
		var record struct {
			Targets map[string]struct {
				Tags   []string          `json:"tags"`
				Labels map[string]string `json:"labels"`
			} `json:"target"`
		}
		if config.ReadJSON(filepath.Join(s.Root, ".generated", "images", "sandbox-"+entry.Agent+".json"), &record) == nil {
			for _, image := range record.Targets {
				if slices.Contains(image.Tags, entry.Image) && image.Labels["io.paseo-sbx.category"] == "sandbox" && image.Labels["io.paseo-sbx.agent"] == entry.Agent {
					versions["agent"] = image.Labels["io.paseo-sbx.agent.version"]
				}
			}
		}
		return versions
	}
	agent := entry.Agent
	if pair, ok := reg.EffectiveCatalog().Pair(profile); ok {
		agent = pair.Agent
	}
	if v := s.Lock.Agents[agent]; v != "" {
		versions["agent"] = v
	}
	add := func(key string, raw json.RawMessage) {
		var item struct {
			Version  string `json:"version"`
			Revision string `json:"revision"`
		}
		if json.Unmarshal(raw, &item) == nil {
			v := item.Version
			if v == "" {
				v = item.Revision
			}
			if v != "" {
				versions[key] = v
			}
		}
	}
	if entry.Bundle() {
		delete(versions, "agent")
		for _, a := range entry.AgentIDs() {
			if v := s.Lock.Agents[a]; v != "" {
				versions[a] = v
			}
		}
		for _, p := range entry.Pairs {
			add(p.Methodology, s.Lock.Tools[p.Methodology])
		}
	} else {
		add("methodology", s.Lock.Tools[entry.Tool])
	}
	utilities := entry.Utilities
	if entry.EffectiveCategory() == config.CategoryLegacySandbox && reg.Selection != nil {
		utilities = reg.Selection.Utilities
	}
	{
		for _, u := range utilities {
			add(u, s.Lock.Utilities[u])
		}
	}
	return versions
}
