package operations

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"paseo-sbx/internal/config"
	"paseo-sbx/internal/operationlock"
)

type removalBackend struct {
	categoryBackend
	state                             RuntimeState
	absent                            bool
	keepPresent                       bool
	inspectErr, removeErr, observeErr error
	removed                           int
	onRemove                          func()
}

func (b *removalBackend) Inspect(context.Context, config.Entry) (RuntimeState, error) {
	if b.absent {
		return RuntimeState{}, ErrRuntimeNotFound
	}
	if b.removed > 0 && b.observeErr != nil {
		return RuntimeState{}, b.observeErr
	}
	return b.state, b.inspectErr
}
func (b *removalBackend) List(context.Context) ([]RuntimeState, error) {
	if b.removed > 0 && b.observeErr != nil {
		return nil, b.observeErr
	}
	if b.absent {
		return nil, nil
	}
	return []RuntimeState{b.state}, b.inspectErr
}
func (b *removalBackend) Remove(context.Context, config.Entry) error {
	b.removed++
	if b.onRemove != nil {
		b.onRemove()
	}
	if b.removeErr == nil && b.observeErr == nil && !b.keepPresent {
		b.absent = true
	}
	return b.removeErr
}

func removalFixture(t *testing.T) (*Service, Request, string, *removalBackend) {
	t.Helper()
	s, r, path := restoreFixture(t)
	r.Kind = KindRemove
	r.Targets = nil
	b := &removalBackend{state: RuntimeState{ID: r.Target.Entry.Sandbox, Known: true}}
	s.Backends = map[config.Category]Backend{r.Target.Entry.EffectiveCategory(): b}
	return s, r, path, b
}

func TestRemoveUnregistersBeforeResourceAndCleansRecords(t *testing.T) {
	s, r, path, b := removalFixture(t)
	var reg config.Registry
	config.ReadJSON(path, &reg)
	reg.Active = []string{r.Target.Profile}
	config.WriteJSON(path, reg)
	for _, dir := range []string{"updates", "version-checks"} {
		config.WriteJSON(observationPath(r.Target.Runtime, dir, r.Target.ResourceIdentity()), map[string]string{"record": "test"})
	}
	release, err := operationlock.Acquire(context.Background(), r.Target.Runtime, []string{operationlock.EntryKey(r.Target.Entry)})
	if err != nil {
		t.Fatal(err)
	}
	release()
	locks, _ := filepath.Glob(filepath.Join(r.Target.Runtime, "updates", "*.lock"))
	before, _ := os.Stat(locks[0])
	synced := false
	b.onRemove = func() {
		config.ReadJSON(path, &reg)
		if len(reg.ActiveProfiles()) != 0 || !synced {
			t.Fatal("resource removed before provider sync", reg.ActiveProfiles(), synced)
		}
		providers, err := readProviders(r.PaseoHome)
		if _, ok := providers[r.Target.Profile]; err != nil || ok {
			t.Fatal("provider retained", providers, err)
		}
	}
	result, err := s.Execute(context.Background(), r, func(e Event) {
		if e.Stage == "paseo" {
			synced = true
		}
	})
	if err != nil || !result.Changed || b.removed != 1 {
		t.Fatal(result, err, b.removed)
	}
	config.ReadJSON(path, &reg)
	if _, ok := reg.Profiles[r.Target.Profile]; ok {
		t.Fatal("profile retained")
	}
	for _, dir := range []string{"updates", "version-checks"} {
		if _, err := os.Stat(observationPath(r.Target.Runtime, dir, r.Target.ResourceIdentity())); !os.IsNotExist(err) {
			t.Fatal("record retained", dir, err)
		}
	}
	after, err := os.Stat(locks[0])
	if err != nil || !os.SameFile(before, after) {
		t.Fatal("entry lock inode changed", err)
	}
}

func TestRemoveRefusesUnsafeOrChangedTarget(t *testing.T) {
	for _, mode := range []string{"running", "unknown", "inspection failure", "changed", "multiple"} {
		t.Run(mode, func(t *testing.T) {
			s, r, path, b := removalFixture(t)
			switch mode {
			case "running":
				b.state.Running = true
			case "unknown":
				b.state.Known = false
			case "inspection failure":
				b.inspectErr = context.DeadlineExceeded
			case "changed":
				var reg config.Registry
				config.ReadJSON(path, &reg)
				e := reg.Profiles[r.Target.Profile]
				e.Image = "replacement"
				reg.Profiles[r.Target.Profile] = e
				config.WriteJSON(path, reg)
			case "multiple":
				r.Targets = []Target{r.Target, r.Target}
			}
			before, _ := os.ReadFile(path)
			providerBefore, _ := os.ReadFile(filepath.Join(r.PaseoHome, "config.json"))
			result, err := s.Execute(context.Background(), r, nil)
			after, _ := os.ReadFile(path)
			if err == nil || result.Changed || b.removed != 0 || string(before) != string(after) {
				t.Fatal(result, err, b.removed)
			}
			if mode == "running" && !strings.Contains(err.Error(), "stop first") {
				t.Fatal(err)
			}
			if (mode == "unknown" || mode == "inspection failure") && !strings.Contains(err.Error(), "state unknown") {
				t.Fatal(err)
			}
			providerAfter, _ := os.ReadFile(filepath.Join(r.PaseoHome, "config.json"))
			if string(providerBefore) != string(providerAfter) {
				t.Fatal("provider changed")
			}
		})
	}
}

func TestRemoveFailureKeepsRegistryAndRetryCleansAbsent(t *testing.T) {
	for _, mode := range []string{"remove failure", "cancel", "observation failure", "still present", "already absent"} {
		t.Run(mode, func(t *testing.T) {
			s, r, path, b := removalFixture(t)
			var initial config.Registry
			config.ReadJSON(path, &initial)
			initial.Active = []string{r.Target.Profile}
			config.WriteJSON(path, initial)
			switch mode {
			case "remove failure":
				b.removeErr = errors.New("remove failed")
			case "cancel":
				b.removeErr = context.Canceled
			case "observation failure":
				b.observeErr = context.DeadlineExceeded
			case "still present":
				b.keepPresent = true
			case "already absent":
				b.absent = true
			}
			result, err := s.Execute(context.Background(), r, nil)
			var reg config.Registry
			config.ReadJSON(path, &reg)
			_, retained := reg.Profiles[r.Target.Profile]
			if mode == "already absent" {
				if err != nil || retained || b.removed != 0 {
					t.Fatal(result, err, retained, b.removed)
				}
				return
			}
			if err == nil || !retained || len(result.Remaining) != 1 {
				t.Fatal(result, err, retained)
			}
			if len(reg.ActiveProfiles()) != 0 {
				t.Fatal("failed removal rolled activation back", reg.ActiveProfiles())
			}
			if mode == "cancel" && !result.NeedsVerification {
				t.Fatal("cancellation lost", result)
			}
			b.absent = true
			removed := b.removed
			if _, err := s.Execute(context.Background(), r, nil); err != nil {
				t.Fatal("retry failed", err)
			}
			config.ReadJSON(path, &reg)
			if _, ok := reg.Profiles[r.Target.Profile]; ok || b.removed != removed {
				t.Fatal("absent retry touched runtime or retained profile")
			}
		})
	}
}
