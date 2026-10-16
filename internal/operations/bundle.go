package operations

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"paseo-dock/internal/config"
	"slices"
	"strings"
)

// bundleAgents orders a bundle's agents by catalog order, which also selects
// the base image: the first agent's base supplies the OS and agent user.
func bundleAgents(c config.Catalog, e config.Entry) []string {
	var out []string
	for _, a := range c.Agents {
		if slices.Contains(e.AgentIDs(), a.ID) {
			out = append(out, a.ID)
		}
	}
	return out
}

// bundleImageSpec derives a bundle image identity from installation inputs.
// The container name is not an input, so equal selections share an image tag.
func bundleImageSpec(c config.Catalog, entry config.Entry, lock config.Lock) (string, map[string]string, error) {
	if entry.EffectiveCategory() != config.CategoryContainer || !c.ValidBundle(entry) {
		return "", nil, fmt.Errorf("invalid bundle entry")
	}
	agents := bundleAgents(c, entry)
	if len(agents) == 0 || lock.BaseImages[agents[0]] == "" {
		return "", nil, fmt.Errorf("missing base image for bundle")
	}
	var versions, sources []string
	for _, agent := range agents {
		version := lock.Agents[agent]
		if !stableVersion.MatchString(version) {
			return "", nil, fmt.Errorf("missing exact agent version for %s", agent)
		}
		source, err := imageAgentSource(agent, version, lock)
		if err != nil {
			return "", nil, err
		}
		versions = append(versions, agent+"@"+version)
		sources = append(sources, source)
	}
	for _, agent := range acpAdapters(entry) {
		if a := lock.ACPAdapters[agent]; a.Package == "" || !stableVersion.MatchString(a.Version) || !strings.HasPrefix(a.Integrity, "sha512-") || a.Bin == "" {
			return "", nil, fmt.Errorf("missing pinned ACP adapter for %s", agent)
		}
	}
	var tools []string
	for _, p := range entry.Pairs {
		if lock.Tools[p.Methodology] == nil {
			return "", nil, fmt.Errorf("missing locked stack for %s/%s", p.Agent, p.Methodology)
		}
		if !slices.Contains(tools, p.Methodology) {
			tools = append(tools, p.Methodology)
		}
	}
	slices.Sort(tools)
	utilities := slices.Clone(entry.Utilities)
	slices.Sort(utilities)
	for i, id := range utilities {
		if (i > 0 && utilities[i-1] == id) || lock.Utilities[id] == nil {
			return "", nil, fmt.Errorf("duplicate or unlocked utility %s", id)
		}
	}
	b, err := json.Marshal(struct {
		Category    config.Category
		Pairs       []config.Pair
		Connections map[string]string
		Utilities   []string
		Lock        config.Lock
		Recipe      string
	}{config.CategoryContainer, entry.Pairs, entry.Connections, utilities, lock, "bundle-v1"})
	if err != nil {
		return "", nil, err
	}
	sum := sha256.Sum256(b)
	fingerprint := fmt.Sprintf("%x", sum[:])
	labels := map[string]string{"io.paseo-dock.category": string(config.CategoryContainer), "io.paseo-dock.agent": strings.Join(agents, ","), "io.paseo-dock.tool": strings.Join(tools, ","), "io.paseo-dock.utilities": strings.Join(utilities, ","), "io.paseo-dock.fingerprint": fingerprint, "io.paseo-dock.agent.version": strings.Join(versions, ","), "io.paseo-dock.agent.source": strings.Join(sources, ",")}
	return lock.Tag + "-" + fingerprint[:12], labels, nil
}

// bundleBakeTarget builds every pair into one image. Agents install in catalog
// order; PAIRS carries agent:methodology:installer for the per-pair installer.
func (a *execution) bundleBakeTarget(profile, namespace, platforms string, entry config.Entry) (map[string]any, error) {
	tag, labels, err := bundleImageSpec(a.Catalog, entry, a.Lock)
	if err != nil {
		return nil, err
	}
	agents := bundleAgents(a.Catalog, entry)
	manifests := map[string]any{}
	for _, agent := range agents {
		source, err := imageAgentSource(agent, a.Lock.Agents[agent], a.Lock)
		if err != nil {
			return nil, err
		}
		manifest := map[string]any{"category": config.CategoryContainer, "agent": agent, "version": a.Lock.Agents[agent], "source": source}
		if artifacts := a.Lock.AgentArtifacts[agent]; artifacts != nil {
			manifest["artifacts"] = artifacts
		}
		manifests[agent] = manifest
	}
	encoded, err := json.Marshal(manifests)
	if err != nil {
		return nil, err
	}
	var pairs []string
	for _, p := range entry.Pairs {
		installer := ""
		for _, m := range a.Catalog.Methodologies {
			if m.ID == p.Methodology {
				installer = m.Installer
			}
		}
		pairs = append(pairs, p.Agent+":"+p.Methodology+":"+installer)
	}
	args := map[string]string{"BASE_IMAGE": a.Lock.BaseImages[agents[0]], "AGENTS": strings.Join(agents, ","), "PAIRS": strings.Join(pairs, ","), "AGENT_MANIFESTS": string(encoded), "CATEGORY": string(config.CategoryContainer), "UTILITIES": strings.Join(entry.Utilities, ","), "ACP_ADAPTERS": strings.Join(acpAdapters(entry), ",")}
	t := map[string]any{"context": a.Root, "dockerfile": "Dockerfile", "tags": []string{namespace + "/" + profile + ":" + tag}, "args": args, "labels": labels}
	if platforms != "" {
		t["platforms"] = strings.Split(platforms, ",")
	}
	return t, nil
}

// entryRegistration aggregates a bundle's providers: registered only when every
// connected agent is; agents connected as none need no provider.
func entryRegistration(providers map[string]any, err error, base, profile string, entry config.Entry) string {
	if !entry.Bundle() {
		return providerRegistration(providers, err, base, providerID(profile, entry), entry.Agent)
	}
	if err != nil {
		return "unknown"
	}
	connected := 0
	for _, t := range ProviderTargets(profile, entry) {
		if t.Mode == config.ConnectionNone {
			continue
		}
		connected++
		p, ok := providers[t.ID].(map[string]any)
		want, args := config.ProviderAdapter(t.Agent), 2
		if t.Mode == config.ConnectionACP {
			want, args = "acp", 3
		}
		if !ok || p["enabled"] != true || p["extends"] != want {
			return "unregistered"
		}
		command, ok := p["command"].([]any)
		if !ok || len(command) != args || command[0] != config.LauncherPath(base) || command[1] != t.ID || (args == 3 && command[2] != "--acp") {
			return "unregistered"
		}
	}
	if connected == 0 {
		return "unmanaged"
	}
	return "registered"
}

// acpAdapters lists bundle agents whose acp connection needs a pinned adapter package.
func acpAdapters(entry config.Entry) []string {
	var out []string
	for _, agent := range entry.AgentIDs() {
		if entry.Connection(agent) == config.ConnectionACP && config.ACPAdapterAgent(agent) {
			out = append(out, agent)
		}
	}
	return out
}
