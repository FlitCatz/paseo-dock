// Package operations owns host sandbox execution shared by CLI and dashboard.
package operations

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"paseo-sbx/internal/config"
	"paseo-sbx/internal/operationlock"
	"paseo-sbx/internal/process"
	"paseo-sbx/internal/sandboxupdate"
)

type Service struct {
	// ValidatePaseoConfig overrides the local Paseo validator for tests or embedders.
	// It must validate home/config.json without starting or contacting a daemon.
	ValidatePaseoConfig func(context.Context, string) error
	Backends            map[config.Category]Backend
	Root                string
	Lock                config.Lock
	Catalog             config.Catalog
	In                  io.Reader
	Out, Err            io.Writer
}

// execution holds only request-local mutable state. Service is never modified.
type execution struct {
	*Service
	Selection *config.Selection
	ctx       context.Context
	emit      func(Event)
	changed   bool
	missing   []Target
	uncertain bool
	ports     []config.PortMapping
	remaining []Target
}

func New(root string) (*Service, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	lock, err := config.Load(root)
	if err != nil {
		return nil, err
	}
	catalog, err := config.LoadCatalog(root)
	return &Service{Root: root, Lock: lock, Catalog: catalog, In: os.Stdin, Out: os.Stdout, Err: os.Stderr}, err
}

func (s *Service) execution(ctx context.Context, selection *config.Selection, emit func(Event)) *execution {
	copyService := *s
	if copyService.Out == nil {
		copyService.Out = io.Discard
	}
	if copyService.Err == nil {
		copyService.Err = io.Discard
	}
	if emit != nil {
		// Background observers own output, while Bubble Tea owns terminal input.
		// Only the request-local copy changes; CLI execution without an observer
		// keeps its interactive input and shell transfers ownership explicitly.
		copyService.In = nil
		var mu sync.Mutex
		observer := emit
		emit = func(e Event) { mu.Lock(); defer mu.Unlock(); observer(e) }
		copyService.Out = eventWriter{stage: "stdout", emit: emit}
		copyService.Err = eventWriter{stage: "stderr", emit: emit}
	}
	if selection != nil {
		copySelection := *selection
		copySelection.Pairs = slices.Clone(selection.Pairs)
		copySelection.Utilities = slices.Clone(selection.Utilities)
		selection = &copySelection
	}
	return &execution{Service: &copyService, Selection: selection, ctx: ctx, emit: emit}
}

type eventWriter struct {
	stage string
	emit  func(Event)
}

func (w eventWriter) Write(p []byte) (int, error) {
	w.emit(Event{Stage: w.stage, Text: string(p), Time: time.Now()})
	return len(p), nil
}
func (a *execution) writeJSON(path string, v any) error {
	err := config.WriteJSON(path, v)
	if err == nil {
		a.changed = true
	}
	return err
}
func (a *execution) stage(stage, text string) {
	if a.emit != nil {
		a.emit(Event{Stage: stage, Text: text, Time: time.Now()})
	}
}
func (a *execution) run(args ...string) error {
	if err := a.ctx.Err(); err != nil {
		return err
	}
	a.stage(args[0], fmt.Sprint(args))
	cmd := process.Command(a.ctx, args[0], args[1:]...)
	cmd.Dir = a.Root
	cmd.Stdin = a.In
	cmd.Stdout = a.Out
	cmd.Stderr = a.Err
	err := cmd.Run()
	if cmd.Process != nil {
		a.changed = true
	}
	if err != nil && a.ctx.Err() != nil {
		return errors.Join(a.ctx.Err(), err)
	}
	return err
}
func (a *execution) capture(args ...string) ([]byte, error) {
	cmd := process.Command(a.ctx, args[0], args[1:]...)
	cmd.Dir = a.Root
	cmd.Stderr = a.Err
	return cmd.Output()
}

func (s *Service) SelectedRuntime(workspace string) (string, error) {
	return s.execution(context.Background(), nil, nil).selectedRuntime(workspace)
}
func (s *Service) ListAll() error { return s.execution(context.Background(), nil, nil).listAll() }
func (s *Service) InstallLauncher(base string, workspaces []string) error {
	release, err := operationlock.Acquire(context.Background(), base, []string{"runtime"})
	if err != nil {
		return err
	}
	defer release()
	return s.execution(context.Background(), nil, nil).installLauncher(base, workspaces)
}
func (s *Service) BakeConfig(selection *config.Selection, namespace, platforms string) map[string]any {
	return s.execution(context.Background(), selection, nil).BakeConfig(namespace, platforms)
}

func (s *Service) resolveTarget(target Target) (Target, error) {
	if target.Runtime == "" {
		runtime, err := s.SelectedRuntime(target.Workspace)
		if err != nil {
			return target, err
		}
		target.Runtime = runtime
	}
	var err error
	target.Runtime, err = config.Canonical(target.Runtime)
	if err != nil {
		return target, err
	}
	var reg config.Registry
	if err := config.ReadJSON(filepath.Join(target.Runtime, "registry.json"), &reg); err != nil {
		return target, err
	}
	if target.Workspace != "" {
		w, err := config.Canonical(target.Workspace)
		if err != nil {
			return target, err
		}
		if reg.Workspace != w {
			return target, errors.New("registry workspace changed")
		}
	}
	entry, ok := reg.Profiles[target.Profile]
	if !ok {
		return target, fmt.Errorf("profile %s missing from registry", target.Profile)
	}
	if !target.Entry.EqualIdentity(config.Entry{}) {
		if err := reg.ValidateIdentity(target.Profile, target.Entry); err != nil {
			return target, err
		}
	}
	if target.Category != "" && target.Category != entry.EffectiveCategory() {
		return target, errors.New("confirmed target category changed")
	}
	if sandboxupdate.Identity(entry) == "" || (entry.EffectiveCategory() == config.CategoryContainer && entry.ContainerID == "") || entry.Workspace != reg.Workspace {
		return target, errors.New("invalid sandbox target")
	}
	target.Workspace = reg.Workspace
	target.Entry = entry
	target.Category = entry.EffectiveCategory()
	expected, err := config.RuntimePath(target.Workspace)
	if err != nil {
		return target, err
	}
	expected, err = config.Canonical(expected)
	if err != nil {
		return target, err
	}
	if target.Runtime != expected {
		return target, errors.New("sandbox runtime identity mismatch")
	}
	return target, nil
}

func (s *Service) ShellCommand(ctx context.Context, target Target) (ShellProcess, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	target, err := s.resolveTarget(target)
	if err != nil {
		return nil, err
	}
	target.Entry.Ports = slices.Clone(target.Entry.Ports)
	target.Entry.Utilities = slices.Clone(target.Entry.Utilities)
	copy := *s
	return &shellProcess{service: &copy, ctx: ctx, target: target}, nil
}

func (s *Service) updateTargets(r Request) ([]Target, error) {
	targets := slices.Clone(r.Targets)
	if len(targets) == 0 && r.Target.Profile != "" {
		targets = []Target{r.Target}
	}
	if len(targets) == 0 {
		runtime := r.Target.Runtime
		if runtime == "" {
			var err error
			runtime, err = s.SelectedRuntime(r.Target.Workspace)
			if err != nil {
				return nil, err
			}
		}
		var reg config.Registry
		if err := config.ReadJSON(filepath.Join(runtime, "registry.json"), &reg); err != nil {
			return nil, err
		}
		categories := map[config.Category]bool{}
		for _, p := range reg.ActiveProfiles() {
			category := reg.Profiles[p].EffectiveCategory()
			if r.Target.Category != "" && r.Target.Category != category {
				continue
			}
			categories[category] = true
			targets = append(targets, Target{Workspace: reg.Workspace, Runtime: runtime, Profile: p, Entry: reg.Profiles[p]})
		}
		if len(categories) > 1 {
			return nil, errors.New("multiple runtime categories are active; specify a category or exact target")
		}
	}
	for i, t := range targets {
		validated, err := s.resolveTarget(t)
		if err != nil {
			return nil, err
		}
		targets[i] = validated
	}
	return targets, nil
}

func (s *Service) Execute(ctx context.Context, r Request, emit func(Event)) (result Result, err error) {
	unregisterCategory := r.Target.Category // Resolution must not turn an omitted category into a removal subset.
	if err = ctx.Err(); err != nil {
		return result, err
	}
	r.Targets = slices.Clone(r.Targets)
	r.RestoreTargets = slices.Clone(r.RestoreTargets)
	r.PruneTargets = slices.Clone(r.PruneTargets)
	a := s.execution(ctx, nil, emit)
	a.ports = slices.Clone(r.Ports)
	if r.Kind == KindRegister && r.RestoreSelection {
		saved := r.Selection
		saved.Pairs = slices.Clone(saved.Pairs)
		saved.Utilities = slices.Clone(saved.Utilities)
		a.Selection = &saved
	}
	consumesSelection := r.Kind == KindBuild || r.Kind == KindLoad || r.Kind == KindCreate
	if consumesSelection && (len(r.Selection.Pairs) > 0 || r.Selection.Schema != 0) {
		normalized, e := s.Catalog.Normalize(r.Selection)
		if e != nil {
			return result, e
		}
		if normalized.Schema == 1 {
			selection, err := s.ResolveSelection(SelectionOptions{Profiles: joinProfiles(normalized), Utilities: joinUtilities(normalized), SetUtilities: true})
			if err != nil {
				return result, err
			}
			normalized = selection
		}
		a.Selection = &normalized
	}
	if len(a.ports) > 0 && (a.Selection == nil || a.Selection.Category != config.CategoryContainer || r.Kind != KindCreate) {
		return result, errors.New("port mappings require container creation")
	}
	if r.Kind == KindCreate && a.Selection != nil {
		memoryCategory := a.Selection.Category
		if a.Selection.Schema == 1 {
			memoryCategory = config.CategorySandbox
		}
		if err := config.ValidateMemory(memoryCategory, r.Memory); err != nil {
			return result, err
		}
	}
	if r.Namespace == "" {
		r.Namespace = "paseo-sbx"
	}
	if r.Memory == "" {
		r.Memory = "2g"
	}
	if r.PaseoHome == "" {
		r.PaseoHome = "~/.paseo"
	}
	a.stage(string(r.Kind), "Starting "+string(r.Kind))
	if r.Kind == KindVersions {
		return a.checkVersions(r.Target)
	}
	if r.Kind == KindRemove {
		err = a.remove(r)
		if a.changed && ctx.Err() != nil {
			a.uncertain = true
		}
		return Result{Changed: a.changed, NeedsVerification: a.uncertain, Remaining: a.remaining}, err
	}
	if r.Kind == KindUpdate || r.Kind == KindStop {
		if r.Kind == KindUpdate && r.AutomaticUpdate && os.Getenv("PASEO_SBX_AUTO_UPDATE") == "0" {
			return result, nil
		}
		targets, e := s.updateTargets(r)
		if e != nil {
			return result, e
		}
		if r.Kind == KindStop && len(targets) != 1 {
			return result, errors.New("stop requires one sandbox target")
		}
		var failures []error
		for i, t := range targets {
			if e = ctx.Err(); e != nil {
				result.Remaining = append(result.Remaining, targets[i:]...)
				failures = append(failures, e)
				break
			}
			// Multi-agent containers keep their pinned image versions for now.
			if r.Kind == KindUpdate && t.Entry.Bundle() {
				if !r.AutomaticUpdate {
					failures = append(failures, fmt.Errorf("%s: multi-agent containers are not updated in place yet; recreate from a rebuilt image", t.Profile))
				}
				continue
			}
			release, e := operationlock.Acquire(ctx, t.Runtime, []string{operationlock.EntryKey(t.Entry)})
			if e != nil {
				if r.Kind == KindUpdate && r.AutomaticUpdate && errors.Is(e, operationlock.ErrConflict) {
					continue
				}
				failures = append(failures, e)
				result.Remaining = append(result.Remaining, t)
				continue
			}
			validated, e := s.resolveTarget(t)
			if e == nil {
				var backend Backend
				backend, e = a.backend(validated.Category)
				if e != nil {
					release()
					return result, e
				}
				if r.Kind == KindUpdate {
					started := false
					transport := func(ctx context.Context, args []string, capture bool) (string, error) {
						var out bytes.Buffer
						writer := a.Err
						if capture {
							writer = &out
						}
						entry := validated.Entry
						// SBX installs must not read workspace npm configuration.
						if validated.Category != config.CategoryContainer {
							entry.Workspace = "/tmp"
						} else {
							args = append([]string{"env", "-C", "/tmp"}, args...)
						}
						err := backend.Exec(ctx, entry, args, nil, writer, a.Err)
						return strings.TrimSpace(out.String()), err
					}
					e = sandboxupdate.UpdateTransportLockedContext(ctx, validated.Runtime, validated.Entry, !r.AutomaticUpdate, a.Err, func(stage string) { started = true; a.stage(stage, stage) }, transport)
					result.Changed = result.Changed || started
					if started && (ctx.Err() != nil || errors.Is(e, context.Canceled) || errors.Is(e, context.DeadlineExceeded)) {
						result.NeedsVerification = true
					}
				} else {
					e = backend.Stop(ctx, validated.Entry)
					a.changed = true
				}
			}
			release()
			if e != nil {
				result.Remaining = append(result.Remaining, t)
				failures = append(failures, fmt.Errorf("%s: %w", t.Profile, e))
			}
		}
		result.Changed = result.Changed || a.changed
		if a.changed && ctx.Err() != nil {
			result.NeedsVerification = true
		}
		return result, errors.Join(failures...)
	}
	base, e := config.BaseDir()
	if e != nil {
		return result, e
	}
	base, e = config.Canonical(base)
	if e != nil {
		return result, e
	}
	keys := []string{}
	switch r.Kind {
	case KindBuild, KindLoad:
		keys = append(keys, "root:"+a.Root)
	case KindCreate, KindUnregister:
		if r.Target.Workspace == "" {
			return result, errors.New("--workspace is required")
		}
		w, e := config.Canonical(r.Target.Workspace)
		if e != nil {
			return result, e
		}
		r.Target.Workspace = w
		keys = append(keys, "workspace:"+w)
		if r.Kind == KindCreate {
			keys = append(keys, "runtime")
			if r.BuildFirst {
				keys = append(keys, "root:"+a.Root)
			}
		}
	case KindRegister, KindPrune:
	default:
		return result, fmt.Errorf("unknown operation %q", r.Kind)
	}
	if r.Kind == KindRegister || r.Kind == KindUnregister || r.Kind == KindPrune || (r.Kind == KindCreate && r.Apply) {
		home, e := config.Canonical(r.PaseoHome)
		if e != nil {
			return result, e
		}
		r.PaseoHome = home
		keys = append(keys, "runtime", "home:"+home, "root:"+a.Root)
	}
	release, e := operationlock.Acquire(ctx, base, keys)
	if e != nil {
		return result, e
	}
	defer release()

	// Workspace/provider mutations also honor an explicitly confirmed registry entry.
	if r.Target.Profile != "" || !r.Target.Entry.EqualIdentity(config.Entry{}) {
		validated, e := s.resolveTarget(r.Target)
		if e != nil {
			return result, e
		}
		r.Target = validated
	}
	if (r.Kind == KindRegister || r.Kind == KindUnregister) && r.Targets != nil {
		if err := s.validateProviderScope(r.Target, r.Targets); err != nil {
			return result, err
		}
	}
	switch r.Kind {
	case KindBuild:
		err = a.build(r.Namespace, r.Platforms, r.Push)
	case KindLoad:
		err = a.load(r.Namespace, r.KeepArchive)
	case KindCreate:
		err = a.prepareRepository(r.Target.Workspace, r.Repository)
		if err == nil && r.BuildFirst {
			err = a.build(r.Namespace, r.Platforms, false)
			if err == nil && (a.Selection == nil || a.Selection.Category != config.CategoryContainer) {
				err = a.load(r.Namespace, r.KeepArchive)
			}
		}
		if err == nil {
			err = a.create(r.Target.Workspace, r.Namespace, r.Memory, r.Apply)
		}
		if err == nil && r.Apply {
			runtime, e := a.selectedRuntime(r.Target.Workspace)
			err = e
			if err == nil {
				err = a.register(runtime, r.PaseoHome, true)
			}
		}
		runtime, e := a.selectedRuntime(r.Target.Workspace)
		if e == nil {
			var reg config.Registry
			if config.ReadJSON(filepath.Join(runtime, "registry.json"), &reg) == nil {
				for _, p := range sortedProfiles(reg.Profiles) {
					result.Remaining = append(result.Remaining, Target{Category: reg.Profiles[p].EffectiveCategory(), Workspace: reg.Workspace, Runtime: runtime, Profile: p, Entry: reg.Profiles[p]})
				}
			}
		}
		for _, target := range a.remaining {
			found := false
			for _, recorded := range result.Remaining {
				if recorded.EqualIdentity(target) {
					found = true
					break
				}
			}
			if !found {
				result.Remaining = append(result.Remaining, target)
			}
		}
	case KindRegister:
		runtime := r.Target.Runtime
		if runtime == "" {
			runtime, err = a.selectedRuntime(r.Target.Workspace)
		}
		if err == nil {
			if r.RestoreSelection {
				err = a.restoreRegistration(runtime, r)
			}
		}
		if err == nil {
			err = a.register(runtime, r.PaseoHome, r.Apply)
		}
	case KindUnregister:
		err = a.unregister(r.Target.Workspace, r.PaseoHome, unregisterCategory)
	case KindPrune:
		err = a.prune(r.PaseoHome, r.DryRun, r.PruneTargets, r.RemoveRuntimes)
		result.Remaining = a.remaining
	}
	result.Changed = a.changed
	result.NeedsVerification = a.uncertain
	result.Missing = a.missing
	if a.changed && ctx.Err() != nil {
		result.NeedsVerification = true
	}
	return result, err
}
func sortedProfiles(entries map[string]config.Entry) []string {
	keys := make([]string, 0, len(entries))
	for k := range entries {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
