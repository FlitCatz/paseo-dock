package cli

import (
	"flag"
	"paseo-sbx/internal/config"
	"slices"
	"strconv"
	"strings"
)

// Flag values explicitly supplied by the user always win, including false/empty.
func applyCreateConfig(f *flag.FlagSet, c config.CreateConfig) error {
	explicit := map[string]bool{}
	f.Visit(func(v *flag.Flag) { explicit[v.Name] = true })
	values := map[string]string{
		"workspace": c.Workspace, "category": string(c.Category), "utilities": strings.Join(c.Utilities, ","),
		"ports": strings.Join(c.Ports, ","), "apply": strconv.FormatBool(c.Apply), "codex-userns": strconv.FormatBool(c.CodexUserNS),
	}
	for key, value := range map[string]string{"memory": c.Memory, "namespace": c.Namespace, "paseo-home": c.PaseoHome, "name": c.Name, "connections": connectionsFlag(c.Connections)} {
		if value != "" {
			values[key] = value
		}
	}
	if c.Build != nil {
		values["build"] = strconv.FormatBool(*c.Build)
	}
	hasSelection := explicit["profile"] || explicit["profiles"] || explicit["selection"] || explicit["agents"] || explicit["methodologies"] || explicit["agent"]
	if !hasSelection {
		if c.Agent != "" {
			values["agent"] = c.Agent
		}
		if len(c.Pairs) > 0 {
			var pairs []string
			for _, p := range c.Pairs {
				pairs = append(pairs, p.Agent+"-"+p.Methodology)
			}
			values["profiles"] = strings.Join(pairs, ",")
		}
	}
	for key, value := range values {
		if !explicit[key] {
			if err := f.Set(key, value); err != nil {
				return err
			}
		}
	}
	return nil
}

func connectionsFlag(connections map[string]string) string {
	items := make([]string, 0, len(connections))
	for agent, mode := range connections {
		items = append(items, agent+"="+mode)
	}
	slices.Sort(items)
	return strings.Join(items, ",")
}
