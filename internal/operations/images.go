package operations

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"paseo-sbx/internal/config"
	"path/filepath"
	"slices"
	"strings"
)

const sandboxBaseImage = "ubuntu:24.04"

// ImageSpec derives reusable image identity from installation inputs only.
// For sandbox, lock.Agents must contain the version returned by ResolveStableAgent.
func ImageSpec(category config.Category, entry config.Entry, lock config.Lock) (string, map[string]string, error) {
	if category != config.CategorySandbox && category != config.CategoryContainer && category != config.CategoryLegacySandbox {
		return "", nil, fmt.Errorf("invalid image category %q", category)
	}
	if entry.Category != "" && entry.Category != category {
		return "", nil, fmt.Errorf("image category mismatch")
	}
	version := lock.Agents[entry.Agent]
	if !stableVersion.MatchString(version) {
		return "", nil, fmt.Errorf("missing exact agent version for %s", entry.Agent)
	}
	utilities := append([]string{}, entry.Utilities...)
	slices.Sort(utilities)
	for i, id := range utilities {
		if i > 0 && utilities[i-1] == id {
			return "", nil, fmt.Errorf("duplicate utility %s", id)
		}
	}
	if category == config.CategorySandbox {
		if _, err := agentPackage(entry.Agent); err != nil {
			return "", nil, err
		}
		if entry.Tool != "" || len(utilities) != 0 {
			return "", nil, fmt.Errorf("sandbox image cannot include methodology or utilities")
		}
	} else {
		if lock.BaseImages[entry.Agent] == "" || lock.Tools[entry.Tool] == nil {
			return "", nil, fmt.Errorf("missing locked stack for %s/%s", entry.Agent, entry.Tool)
		}
		for _, id := range utilities {
			if lock.Utilities[id] == nil {
				return "", nil, fmt.Errorf("missing locked utility %s", id)
			}
		}
	}
	// A sandbox does not inherit unrelated methodology, utility or agent locks.
	var inputs any = lock
	if category == config.CategorySandbox {
		inputs = struct {
			Node    any
			Base    string
			Version string
		}{lock.Node, sandboxBaseImage, version}
	}
	b, err := json.Marshal(struct {
		Category    config.Category
		Agent, Tool string
		Utilities   []string
		Lock        any
		Recipe      string
	}{category, entry.Agent, entry.Tool, utilities, inputs, "runtime-categories-v2"})
	if err != nil {
		return "", nil, err
	}
	sum := sha256.Sum256(b)
	fingerprint := fmt.Sprintf("%x", sum[:])
	labels := map[string]string{"io.paseo-sbx.category": string(category), "io.paseo-sbx.agent": entry.Agent, "io.paseo-sbx.tool": entry.Tool, "io.paseo-sbx.utilities": strings.Join(utilities, ","), "io.paseo-sbx.fingerprint": fingerprint, "io.paseo-sbx.agent.version": version}
	source, err := imageAgentSource(entry.Agent, version, lock)
	if err != nil {
		return "", nil, err
	}
	labels["io.paseo-sbx.agent.source"] = source
	return lock.Tag + "-" + fingerprint[:12], labels, nil
}

func imageAgentSource(agent, version string, lock config.Lock) (string, error) {
	if name, err := agentPackage(agent); err == nil {
		return packageSource(name, version), nil
	}
	if lock.AgentArtifacts[agent] != nil {
		// The manifest retains the architecture-specific verified artifact sources.
		return "versions.lock.json#agent_artifacts/" + agent, nil
	}
	return "", fmt.Errorf("no recorded package source for agent %s", agent)
}

func (a *execution) image(profile, namespace string) string {
	if a.Selection != nil && a.Selection.Schema == 2 {
		// Sandbox versions are frozen by build, never looked up again at load/create.
		if a.Selection.Category == config.CategorySandbox {
			var cfg map[string]json.RawMessage
			if config.ReadJSON(filepath.Join(a.Root, ".generated", "images", "sandbox-"+a.Selection.Agent+".json"), &cfg) == nil {
				var targets map[string]struct {
					Tags       []string          `json:"tags"`
					Labels     map[string]string `json:"labels"`
					Args       map[string]string `json:"args"`
					Dockerfile string            `json:"dockerfile"`
				}
				name := strings.ReplaceAll(profile, "/", "-")
				if json.Unmarshal(cfg["target"], &targets) == nil && len(targets[name].Tags) == 1 {
					recorded := targets[name]
					entry := config.Entry{Category: config.CategorySandbox, Agent: a.Selection.Agent}
					lock := a.Lock
					lock.Agents = map[string]string{entry.Agent: recorded.Args["AGENT_VERSION"]}
					tag, labels, err := ImageSpec(config.CategorySandbox, entry, lock)
					valid := err == nil && entry.Key() == profile && recorded.Dockerfile == "Dockerfile.sandbox" && recorded.Args["BASE_IMAGE"] == sandboxBaseImage && recorded.Tags[0] == namespace+"/"+profile+":"+tag
					for key, value := range labels {
						if recorded.Labels[key] != value {
							valid = false
						}
					}
					if valid {
						return recorded.Tags[0]
					}
				}
			}
			return ""
		}
		if a.Selection.Name != "" {
			entry := a.Selection.BundleEntry()
			if entry.Key() != profile {
				return ""
			}
			tag, _, err := bundleImageSpec(a.Catalog, entry, a.Lock)
			if err != nil {
				return ""
			}
			return namespace + "/" + profile + ":" + tag
		}
		for _, pair := range a.Selection.Pairs {
			entry := config.Entry{Category: a.Selection.Category, Agent: pair.Agent, Tool: pair.Methodology, Utilities: a.Selection.Utilities}
			if entry.Key() == profile {
				tag, _, err := ImageSpec(a.Selection.Category, entry, a.Lock)
				if err == nil {
					return namespace + "/" + profile + ":" + tag
				}
			}
		}
		return ""
	}
	tag := a.Lock.Tag
	if a.Selection != nil {
		p, _ := a.Catalog.Pair(profile)
		s := *a.Selection
		s.Pairs = []config.Pair{p}
		tag = config.ImageTag(a.Lock, s, a.Catalog)
	}
	return namespace + "/" + profile + ":" + tag
}
func (a *execution) profiles() []string {
	if a.Selection != nil {
		return a.Selection.Profiles()
	}
	return a.Catalog.AllProfiles()
}
func (a *execution) BakeConfig(namespace, platforms string) map[string]any {
	cfg, err := a.bakeConfig(namespace, platforms, ResolveStableAgent)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	return cfg
}

type stableResolver func(context.Context, string) (string, string, error)

func (a *execution) bakeConfig(namespace, platforms string, resolve stableResolver) (map[string]any, error) {
	targets := map[string]any{}
	names := []string{}
	for _, p := range a.profiles() {
		if a.Selection != nil && a.Selection.Schema == 2 && a.Selection.Name != "" {
			t, err := a.bundleBakeTarget(p, namespace, platforms, a.Selection.BundleEntry())
			if err != nil {
				return nil, err
			}
			name := strings.ReplaceAll(p, "/", "-")
			targets[name] = t
			names = append(names, name)
			continue
		}
		category := config.CategoryLegacySandbox
		profile := p
		if a.Selection != nil && a.Selection.Schema == 2 {
			category = a.Selection.Category
			_, profile, _ = strings.Cut(p, "/")
		}
		pair, _ := a.Catalog.Pair(profile)
		if category == config.CategorySandbox {
			pair.Agent = a.Selection.Agent
		}
		agent, tool := pair.Agent, pair.Methodology
		item, _ := a.Catalog.Agent(agent)
		utilities := ""
		if a.Selection != nil {
			utilities = strings.Join(a.Selection.Utilities, ",")
		}
		methodInstaller := ""
		for _, m := range a.Catalog.Methodologies {
			if m.ID == tool {
				methodInstaller = m.Installer
			}
		}
		entry := config.Entry{Category: category, Agent: agent, Tool: tool}
		if a.Selection != nil {
			entry.Utilities = a.Selection.Utilities
		}
		lock := a.Lock
		source, err := imageAgentSource(agent, lock.Agents[agent], lock)
		if category == config.CategorySandbox {
			version, resolvedSource, lookupErr := resolve(a.ctx, agent)
			if lookupErr != nil {
				return nil, lookupErr
			}
			if !stableVersion.MatchString(version) {
				return nil, fmt.Errorf("stable lookup did not return an exact version")
			}
			lock.Agents = map[string]string{agent: version}
			source, err = resolvedSource, nil
		}
		if err != nil {
			return nil, err
		}
		tag, labels, err := ImageSpec(category, entry, lock)
		if err != nil {
			return nil, err
		}
		labels["io.paseo-sbx.agent.source"] = source
		manifest := map[string]any{"category": category, "agent": agent, "version": lock.Agents[agent], "source": source}
		if artifacts := lock.AgentArtifacts[agent]; artifacts != nil {
			manifest["artifacts"] = artifacts
		}
		encoded, err := json.Marshal(manifest)
		if err != nil {
			return nil, err
		}
		dockerfile := "Dockerfile"
		args := map[string]string{"BASE_IMAGE": lock.BaseImages[agent], "AGENT": item.Adapter, "AGENT_VERSION": lock.Agents[agent], "AGENT_SOURCE": source, "AGENT_MANIFEST": string(encoded), "CATEGORY": string(category), "TOOL": tool, "METHOD_INSTALLER": methodInstaller, "UTILITIES": utilities}
		image := namespace + "/" + p + ":" + tag
		if category == config.CategoryLegacySandbox {
			image = a.image(p, namespace)
		}
		if category == config.CategorySandbox {
			dockerfile = "Dockerfile.sandbox"
			args = map[string]string{"BASE_IMAGE": sandboxBaseImage, "AGENT": item.Adapter, "AGENT_VERSION": lock.Agents[agent], "AGENT_SOURCE": source, "AGENT_MANIFEST": string(encoded)}
		}
		t := map[string]any{"context": a.Root, "dockerfile": dockerfile, "tags": []string{image}, "args": args, "labels": labels}
		if platforms != "" {
			t["platforms"] = strings.Split(platforms, ",")
		}
		name := strings.ReplaceAll(p, "/", "-")
		targets[name] = t
		names = append(names, name)
	}
	return map[string]any{"group": map[string]any{"default": map[string]any{"targets": names}}, "target": targets}, nil
}
func (a *execution) build(namespace, platforms string, push bool) error {
	cfg, err := a.bakeConfig(namespace, platforms, ResolveStableAgent)
	if err != nil {
		return err
	}
	path := filepath.Join(a.Root, ".generated", "bake.json")
	if err := a.writeJSON(path, cfg); err != nil {
		return err
	}
	cmd := []string{"docker", "buildx", "bake", "--file", path, "--progress", "plain"}
	if push {
		cmd = append(cmd, "--push")
	} else {
		cmd = append(cmd, "--load")
	}
	fmt.Fprintln(a.Out, "Building selected images:", strings.Join(a.profiles(), ", "))
	if err := a.run(cmd...); err != nil {
		return err
	}
	if a.Selection != nil && a.Selection.Category == config.CategorySandbox {
		return a.writeJSON(filepath.Join(a.Root, ".generated", "images", "sandbox-"+a.Selection.Agent+".json"), cfg)
	}
	return nil
}
func (a *execution) load(namespace string, keep bool) error {
	if a.Selection != nil && a.Selection.Category == config.CategoryContainer {
		return fmt.Errorf("container images use Docker directly; SBX template load is sandbox-only")
	}
	dir := filepath.Join(a.Root, ".artifacts")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	for _, p := range a.profiles() {
		image := a.image(p, namespace)
		if image == "" {
			return fmt.Errorf("no resolved image for %s; build first", p)
		}
		tar := filepath.Join(dir, p+".tar")
		fmt.Fprintln(a.Out, "Loading sandbox template:", p)
		if err := os.MkdirAll(filepath.Dir(tar), 0755); err != nil {
			return err
		}
		if err := a.run("docker", "save", "-o", tar, image); err != nil {
			return err
		}
		if err := a.run("sbx", "template", "load", tar); err != nil {
			return err
		}
		if !keep {
			if err := os.Remove(tar); err != nil {
				return err
			}
		}
	}
	return nil
}
