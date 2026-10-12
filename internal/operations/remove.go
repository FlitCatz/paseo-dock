package operations

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"paseo-sbx/internal/config"
	"paseo-sbx/internal/operationlock"
)

func (a *execution) removalState(t Target) (bool, error) {
	b, err := a.backend(t.Entry.EffectiveCategory())
	if err != nil {
		return false, err
	}
	state, err := b.Inspect(a.ctx, t.Entry)
	if errors.Is(err, ErrRuntimeNotFound) {
		return true, nil
	}
	if err != nil || !state.Known {
		return false, fmt.Errorf("%s: state unknown; removal refused: %w", t.Profile, errors.Join(err, errors.New("inspection did not confirm state")))
	}
	if state.Running {
		return false, fmt.Errorf("%s is running; stop first", t.Profile)
	}
	return false, nil
}

func (a *execution) removeResource(t Target, absent bool) error {
	if absent {
		return nil
	}
	b, err := a.backend(t.Entry.EffectiveCategory())
	if err != nil {
		return err
	}
	if err := a.ctx.Err(); err != nil {
		return err
	}
	a.stage("remove-resource", "Removing "+t.ResourceIdentity())
	err = b.Remove(a.ctx, t.Entry)
	a.changed = true
	if a.ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		a.uncertain = true
	}
	if err != nil {
		return err
	}
	a.stage("verify-removal", "Verifying absence of "+t.ResourceIdentity())
	if t.Entry.EffectiveCategory() == config.CategoryContainer {
		_, err = b.Inspect(a.ctx, t.Entry)
		if errors.Is(err, ErrRuntimeNotFound) {
			return nil
		}
	} else {
		var rows []RuntimeState
		rows, err = b.List(a.ctx)
		if err == nil {
			found := false
			for _, row := range rows {
				if row.ID == t.Entry.Sandbox {
					found = true
					break
				}
			}
			if !found {
				return nil
			}
		}
	}
	a.uncertain = true
	if err != nil {
		return fmt.Errorf("observe removal of %s: %w", t.ResourceIdentity(), err)
	}
	return fmt.Errorf("resource %s still present after removal", t.ResourceIdentity())
}

func (a *execution) removeRecords(t Target) error {
	for _, dir := range []string{"updates", "version-checks"} {
		path := observationPath(t.Runtime, dir, t.ResourceIdentity())
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func (a *execution) remove(r Request) error {
	if len(r.Targets) == 0 && r.Target.Profile == "" {
		return errors.New("remove requires exactly one explicit target")
	}
	targets, err := a.updateTargets(r)
	if err != nil {
		return err
	}
	if len(targets) != 1 {
		return errors.New("remove requires exactly one target")
	}
	t := targets[0]
	release, err := operationlock.Acquire(a.ctx, t.Runtime, []string{operationlock.EntryKey(t.Entry)})
	if err != nil {
		return err
	}
	defer release()
	base, err := config.BaseDir()
	if err != nil {
		return err
	}
	base, err = config.Canonical(base)
	if err != nil {
		return err
	}
	home, err := config.Canonical(r.PaseoHome)
	if err != nil {
		return err
	}
	releaseMetadata, err := operationlock.Acquire(a.ctx, base, []string{"workspace:" + t.Workspace, "runtime", "home:" + home, "root:" + a.Root})
	if err != nil {
		return err
	}
	defer releaseMetadata()
	t, err = a.resolveTarget(t)
	if err != nil {
		return err
	}
	absent, err := a.removalState(t)
	if err != nil {
		return err
	}
	a.remaining = []Target{t}
	path := filepath.Join(t.Runtime, "registry.json")
	var reg config.Registry
	if err = config.ReadJSON(path, &reg); err != nil {
		return err
	}
	active := reg.ActiveProfiles()
	reg.Active = []string{}
	wasActive := false
	for _, key := range active {
		if key == t.Profile {
			wasActive = true
		} else {
			reg.Active = append(reg.Active, key)
		}
	}
	if wasActive {
		a.stage("unregister", "Deactivating "+t.Profile)
		if err = a.writeJSON(path, reg); err != nil {
			return err
		}
	}
	// Repeat sync on retries: a previous sync may have failed after deactivation.
	a.stage("sync-providers", "Synchronizing providers before removal")
	if err = a.syncProviders(home, true); err != nil {
		return err
	}
	if _, err = a.resolveTarget(t); err != nil {
		return err
	}
	if err = a.removeResource(t, absent); err != nil {
		return err
	}
	if _, err = a.resolveTarget(t); err != nil {
		return err
	}
	if err = a.ctx.Err(); err != nil {
		return err
	}
	if err = config.ReadJSON(path, &reg); err != nil {
		return err
	}
	a.stage("registry-cleanup", "Forgetting "+t.Profile)
	if err = a.removeRecords(t); err != nil {
		return err
	}
	delete(reg.Profiles, t.Profile)
	if err = a.writeJSON(path, reg); err != nil {
		return err
	}
	a.remaining = nil
	return nil
}
