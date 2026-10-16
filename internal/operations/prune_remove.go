package operations

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"paseo-dock/internal/config"
)

func installedTargets(i config.Installed) []Target {
	var targets []Target
	for _, p := range sortedProfiles(i.Registry.Profiles) {
		e := i.Registry.Profiles[p]
		targets = append(targets, Target{Category: e.EffectiveCategory(), Workspace: i.Registry.Workspace, Runtime: i.Runtime, Profile: p, Entry: e})
	}
	return targets
}

// The caller holds every entry lock and has revalidated the confirmed registry set.
func (a *execution) pruneRuntimes(home string, dryRun bool, missing []config.Installed) error {
	absent := map[string]bool{}
	var blockers []error
	for _, i := range missing {
		for _, t := range installedTargets(i) {
			gone, err := a.removalState(t)
			absent[t.Runtime+"\x00"+t.Profile] = gone
			command := "sbx rm --force " + t.Entry.Sandbox
			if t.Category == config.CategoryContainer {
				command = "docker rm " + t.Entry.ContainerID
			}
			fmt.Fprintf(a.Out, "  %s / %s: %s\n", t.Workspace, t.Profile, command)
			if err != nil {
				fmt.Fprintln(a.Out, "    blocked:", err)
				a.remaining = append(a.remaining, t)
				blockers = append(blockers, err)
			} else if gone {
				fmt.Fprintln(a.Out, "    already absent; registry cleanup only")
			}
		}
	}
	if dryRun {
		return nil
	}
	if len(blockers) > 0 {
		return errors.Join(blockers...)
	}
	if err := a.ctx.Err(); err != nil {
		return err
	}
	a.stage("sync-providers", "Synchronizing providers before runtime pruning")
	if err := a.syncProviders(home, true); err != nil {
		for _, i := range missing {
			a.remaining = append(a.remaining, installedTargets(i)...)
		}
		return err
	}
	var failures []error
	for _, i := range missing {
		targets := installedTargets(i)
		valid, err := pruneStillMissing(i)
		if err != nil || !valid {
			a.remaining = append(a.remaining, targets...)
			failures = append(failures, errors.Join(err, errors.New("prune workspace or registry changed; retained")))
			continue
		}
		complete := true
		for _, t := range targets {
			valid, err := pruneStillMissing(i)
			if err == nil && !valid {
				err = errors.New("workspace reappeared; runtime retained")
			}
			if err == nil {
				err = a.removeResource(t, absent[t.Runtime+"\x00"+t.Profile])
			}
			if err != nil {
				complete = false
				a.remaining = append(a.remaining, t)
				failures = append(failures, fmt.Errorf("%s / %s: %w", t.Workspace, t.Profile, err))
			}
		}
		if !complete {
			continue
		}
		if err := a.pruneRuntimeRegistry(i, targets); err != nil {
			a.remaining = append(a.remaining, targets...)
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (a *execution) pruneRuntimeRegistry(i config.Installed, targets []Target) error {
	valid, err := pruneStillMissing(i)
	if err != nil {
		return err
	}
	if !valid {
		return errors.New("workspace reappeared; registry retained")
	}
	if err := a.ctx.Err(); err != nil {
		return err
	}
	a.stage("registry-cleanup", "Forgetting "+i.Registry.Workspace)
	for _, t := range targets {
		if err := a.removeRecords(t); err != nil {
			return err
		}
	}
	// Preserve all entry lock files and remove the registry last for retryability.
	for _, name := range []string{"selection.json", "agent-wrapper", "registry.json"} {
		if err := a.ctx.Err(); err != nil {
			return err
		}
		valid, err := pruneStillMissing(i)
		if err != nil {
			return err
		}
		if !valid {
			return errors.New("workspace reappeared; registry retained")
		}
		if err := os.Remove(filepath.Join(i.Runtime, name)); err != nil && !os.IsNotExist(err) {
			return err
		}
		a.changed = true
	}
	fmt.Fprintln(a.Out, "Removed registry:", i.Runtime)
	return nil
}
