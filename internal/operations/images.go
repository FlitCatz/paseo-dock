package operations

import (
	"fmt"
	"os"
	"paseo-sbx/internal/config"
	"path/filepath"
	"strings"
)

func (a *execution) image(profile, namespace string) string {
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
	targets := map[string]any{}
	for _, p := range a.profiles() {
		pair, _ := a.Catalog.Pair(p)
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
		t := map[string]any{"context": a.Root, "dockerfile": "Dockerfile", "tags": []string{a.image(p, namespace)}, "args": map[string]string{"BASE_IMAGE": a.Lock.BaseImages[agent], "AGENT": item.Adapter, "AGENT_VERSION": a.Lock.Agents[agent], "TOOL": tool, "METHOD_INSTALLER": methodInstaller, "UTILITIES": utilities}}
		if platforms != "" {
			t["platforms"] = strings.Split(platforms, ",")
		}
		targets[p] = t
	}
	return map[string]any{"group": map[string]any{"default": map[string]any{"targets": a.profiles()}}, "target": targets}
}
func (a *execution) build(namespace, platforms string, push bool) error {
	cfg := a.BakeConfig(namespace, platforms)
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
	return a.run(cmd...)
}
func (a *execution) load(namespace string, keep bool) error {
	dir := filepath.Join(a.Root, ".artifacts")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	for _, p := range a.profiles() {
		tar := filepath.Join(dir, p+".tar")
		fmt.Fprintln(a.Out, "Loading sandbox template:", p)
		if err := a.run("docker", "save", "-o", tar, a.image(p, namespace)); err != nil {
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
