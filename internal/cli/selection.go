package cli

import (
	"paseo-sbx/internal/config"
	"paseo-sbx/internal/operations"
)

func (a *App) resolveSelection(workspace, profile, profiles, utilities, path, agents, methods string, setUtilities bool) (config.Selection, error) {
	return a.Service.ResolveSelection(operations.SelectionOptions{Workspace: workspace, Profile: profile, Profiles: profiles, Utilities: utilities, Path: path, Agents: agents, Methods: methods, SetUtilities: setUtilities})
}
func readSavedSelection(runtime string) (config.Selection, error) {
	return operations.ReadSavedSelection(runtime)
}
