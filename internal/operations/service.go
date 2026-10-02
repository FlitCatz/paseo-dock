// Package operations owns host sandbox execution shared by CLI and dashboard.
package operations

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"paseo-sbx/internal/config"
	"paseo-sbx/internal/operationlock"
	"paseo-sbx/internal/process"
	"paseo-sbx/internal/sandboxupdate"
)

type Service struct {
	Root     string
	Lock     config.Lock
	Catalog  config.Catalog
	In       io.Reader
	Out, Err io.Writer
}

// execution holds only request-local mutable state. Service is never modified.
type execution struct {
	*Service
	Selection *config.Selection
	ctx       context.Context
	emit      func(Event)
	changed   bool
	missing   []Target
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
	if target.Entry != (config.Entry{}) && target.Entry != entry {
		return target, errors.New("confirmed sandbox target changed")
	}
	if entry.Sandbox == "" || entry.Workspace != reg.Workspace {
		return target, errors.New("invalid sandbox target")
	}
	target.Workspace = reg.Workspace
	target.Entry = entry
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

func (s *Service) ShellCommand(ctx context.Context, target Target) (*exec.Cmd, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	target, err := s.resolveTarget(target)
	if err != nil {
		return nil, err
	}
	// Interactive commands retain the terminal's process group for tea.ExecProcess.
	cmd := exec.CommandContext(ctx, "sbx", "exec", "-it", "--workdir", target.Entry.Workspace, target.Entry.Sandbox, "bash")
	cmd.Dir = s.Root
	return cmd, nil
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
		for _, p := range reg.ActiveProfiles() {
			targets = append(targets, Target{Workspace: reg.Workspace, Runtime: runtime, Profile: p, Entry: reg.Profiles[p]})
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
	if err = ctx.Err(); err != nil {
		return result, err
	}
	r.Targets = slices.Clone(r.Targets)
	a := s.execution(ctx, nil, emit)
	if len(r.Selection.Pairs) > 0 || r.Selection.Schema != 0 {
		normalized, e := s.Catalog.Normalize(r.Selection)
		if e != nil {
			return result, e
		}
		selection, err := s.ResolveSelection(SelectionOptions{Profiles: joinProfiles(normalized), Utilities: joinUtilities(normalized), SetUtilities: true})
		if err != nil {
			return result, err
		}
		a.Selection = &selection
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
	if r.Kind == KindUpdate || r.Kind == KindStop {
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
			release, e := operationlock.Acquire(ctx, t.Runtime, []string{operationlock.SandboxKey(t.Entry.Sandbox)})
			if e != nil {
				failures = append(failures, e)
				result.Remaining = append(result.Remaining, t)
				continue
			}
			validated, e := s.resolveTarget(t)
			if e == nil {
				if r.Kind == KindUpdate {
					started := false
					e = sandboxupdate.UpdateLockedContext(ctx, validated.Runtime, validated.Entry, true, a.Err, func(stage string) { started = true; a.stage(stage, stage) })
					result.Changed = result.Changed || started
					if started && (ctx.Err() != nil || errors.Is(e, context.Canceled) || errors.Is(e, context.DeadlineExceeded)) {
						result.NeedsVerification = true
					}
				} else {
					e = a.run("sbx", "stop", validated.Entry.Sandbox)
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
	case KindVersions:
		return result, errors.New("versions is not implemented yet")
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
	if r.Target.Profile != "" || r.Target.Entry != (config.Entry{}) {
		validated, e := s.resolveTarget(r.Target)
		if e != nil {
			return result, e
		}
		r.Target = validated
	}
	switch r.Kind {
	case KindBuild:
		err = a.build(r.Namespace, r.Platforms, r.Push)
	case KindLoad:
		err = a.load(r.Namespace, r.KeepArchive)
	case KindCreate:
		if r.BuildFirst {
			err = a.build(r.Namespace, r.Platforms, false)
			if err == nil {
				err = a.load(r.Namespace, r.KeepArchive)
			}
		}
		if err == nil {
			err = a.create(r.Target.Workspace, r.Namespace, r.Memory)
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
					result.Remaining = append(result.Remaining, Target{Workspace: reg.Workspace, Runtime: runtime, Profile: p, Entry: reg.Profiles[p]})
				}
			}
		}
	case KindRegister:
		runtime := r.Target.Runtime
		if runtime == "" {
			runtime, err = a.selectedRuntime(r.Target.Workspace)
		}
		if err == nil {
			err = a.register(runtime, r.PaseoHome, r.Apply)
		}
	case KindUnregister:
		err = a.unregister(r.Target.Workspace, r.PaseoHome)
	case KindPrune:
		err = a.prune(r.PaseoHome, r.DryRun)
	}
	result.Changed = a.changed
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
