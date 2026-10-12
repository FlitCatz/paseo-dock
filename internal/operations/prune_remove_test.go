package operations

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"paseo-sbx/internal/config"
	"paseo-sbx/internal/operationlock"
)

func pruneRemovalFixture(t *testing.T) (*Service, Request, string, *removalBackend, *removalBackend) {
	t.Helper()
	s, r, path, sandbox := removalFixture(t)
	e := r.Target.Entry
	e.Category = config.CategorySandbox
	e.Tool = ""
	container := e
	container.Category = config.CategoryContainer
	container.Sandbox = ""
	container.Tool = "superpowers"
	container.ContainerID = strings.Repeat("a", 64)
	container.ImageID = "sha256:image"
	container.Network = "bridge"
	reg := config.Registry{Schema: 2, Workspace: r.Target.Workspace, Profiles: map[string]config.Entry{e.Key(): e, container.Key(): container}, Active: []string{e.Key(), container.Key()}}
	if err := config.WriteJSON(path, reg); err != nil {
		t.Fatal(err)
	}
	docker := &removalBackend{state: RuntimeState{ID: container.ContainerID, Known: true}}
	s.Backends = map[config.Category]Backend{config.CategorySandbox: sandbox, config.CategoryContainer: docker}
	if err := os.RemoveAll(r.Target.Workspace); err != nil {
		t.Fatal(err)
	}
	return s, Request{Kind: KindPrune, RemoveRuntimes: true, PaseoHome: r.PaseoHome}, path, sandbox, docker
}

func TestPruneRemoveRuntimesRefusesAllForAnyBlocker(t *testing.T) {
	for _, mode := range []string{"running", "unknown", "inspection failure"} {
		t.Run(mode, func(t *testing.T) {
			s, r, path, sandbox, docker := pruneRemovalFixture(t)
			switch mode {
			case "running":
				docker.state.Running = true
			case "unknown":
				docker.state.Known = false
			case "inspection failure":
				docker.inspectErr = context.DeadlineExceeded
			}
			before, _ := os.ReadFile(path)
			providerBefore, _ := os.ReadFile(filepath.Join(r.PaseoHome, "config.json"))
			result, err := s.Execute(context.Background(), r, nil)
			after, _ := os.ReadFile(path)
			providerAfter, _ := os.ReadFile(filepath.Join(r.PaseoHome, "config.json"))
			if err == nil || result.Changed || len(result.Remaining) != 1 || sandbox.removed != 0 || docker.removed != 0 || string(before) != string(after) || string(providerBefore) != string(providerAfter) {
				t.Fatal(result, err, sandbox.removed, docker.removed)
			}
		})
	}
}

func TestPruneRemoveRuntimesDispatchAndPartialRetry(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "partial failure"}[fail], func(t *testing.T) {
			s, r, path, sandbox, docker := pruneRemovalFixture(t)
			if fail {
				docker.removeErr = errors.New("remove failed")
			}
			synced := false
			checkSync := func() {
				if !synced {
					t.Fatal("remove before provider sync")
				}
			}
			sandbox.onRemove, docker.onRemove = checkSync, checkSync
			result, err := s.Execute(context.Background(), r, func(e Event) {
				if e.Stage == "paseo" {
					synced = true
				}
			})
			if sandbox.removed != 1 || docker.removed != 1 || (err != nil) != fail {
				t.Fatal(result, err, sandbox.removed, docker.removed)
			}
			_, statErr := os.Stat(path)
			if fail {
				if statErr != nil || len(result.Remaining) != 1 {
					t.Fatal("partial registry lost", result, statErr)
				}
				docker.removeErr = nil
				if _, err := s.Execute(context.Background(), r, nil); err != nil {
					t.Fatal(err)
				}
				if sandbox.removed != 1 || docker.removed != 2 {
					t.Fatal("retry removed absent resource", sandbox.removed, docker.removed)
				}
				_, statErr = os.Stat(path)
			}
			if !os.IsNotExist(statErr) {
				t.Fatal("registry retained after confirmed absence", statErr)
			}
		})
	}
}

func TestPruneRemoveRuntimesDryRunListsBlockersAndResources(t *testing.T) {
	s, r, path, sandbox, docker := pruneRemovalFixture(t)
	docker.state.Running = true
	sandbox.state.Known = false
	r.DryRun = true
	var out bytes.Buffer
	s.Out = &out
	before, _ := os.ReadFile(path)
	result, err := s.Execute(context.Background(), r, nil)
	after, _ := os.ReadFile(path)
	if err != nil || result.Changed || len(result.Missing) != 2 || len(result.Remaining) != 2 || sandbox.removed != 0 || docker.removed != 0 || string(before) != string(after) {
		t.Fatal(result, err)
	}
	for _, text := range []string{"sbx rm --force one", "docker rm " + strings.Repeat("a", 64), "stop first", "state unknown"} {
		if !strings.Contains(out.String(), text) {
			t.Fatal("preview missing", text, out.String())
		}
	}
}

func TestPlainPruneKeepsRuntimesAndHintsContainers(t *testing.T) {
	s, r, _, sandbox, docker := pruneRemovalFixture(t)
	r.RemoveRuntimes = false
	var out bytes.Buffer
	s.Out = &out
	if _, err := s.Execute(context.Background(), r, nil); err != nil {
		t.Fatal(err)
	}
	if sandbox.removed != 0 || docker.removed != 0 || !strings.Contains(out.String(), "docker rm "+strings.Repeat("a", 64)) {
		t.Fatal(out.String(), sandbox.removed, docker.removed)
	}
}

func TestPruneRemoveRuntimesConfirmedChangesAndLockConflicts(t *testing.T) {
	for _, mode := range []string{"changed identity", "added profile", "lock conflict"} {
		t.Run(mode, func(t *testing.T) {
			s, r, path, sandbox, docker := pruneRemovalFixture(t)
			preview := r
			preview.DryRun = true
			result, err := s.Execute(context.Background(), preview, nil)
			if err != nil {
				t.Fatal(err)
			}
			r.PruneTargets = result.Missing
			var reg config.Registry
			config.ReadJSON(path, &reg)
			if mode == "lock conflict" {
				release, err := operationlock.Acquire(context.Background(), filepath.Dir(path), []string{operationlock.EntryKey(reg.Profiles["container/codex-superpowers"])})
				if err != nil {
					t.Fatal(err)
				}
				defer release()
			} else {
				e := reg.Profiles["sandbox/codex"]
				e.Image = "changed"
				key := "sandbox/codex"
				if mode == "added profile" {
					key = "extra"
				}
				reg.Profiles[key] = e
				config.WriteJSON(path, reg)
			}
			before, _ := os.ReadFile(path)
			providers, _ := os.ReadFile(filepath.Join(r.PaseoHome, "config.json"))
			result, err = s.Execute(context.Background(), r, nil)
			after, _ := os.ReadFile(path)
			providersAfter, _ := os.ReadFile(filepath.Join(r.PaseoHome, "config.json"))
			if err == nil || result.Changed || sandbox.removed != 0 || docker.removed != 0 || string(before) != string(after) || string(providers) != string(providersAfter) {
				t.Fatal(result, err)
			}
		})
	}
}

func TestPruneRemoveRuntimesKeepsOnlyFailedWorkspaceRegistry(t *testing.T) {
	s, r, failedPath, _, docker := pruneRemovalFixture(t)
	docker.removeErr = errors.New("failed workspace container")
	w, err := config.Canonical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := config.RuntimePath(w)
	if err != nil {
		t.Fatal(err)
	}
	e := config.Entry{Workspace: w, Sandbox: "other", Agent: "codex", Tool: "native", Image: "image"}
	otherPath := filepath.Join(runtime, "registry.json")
	if err := config.WriteJSON(otherPath, config.Registry{Workspace: w, Profiles: map[string]config.Entry{"codex-native": e}, Active: []string{}}); err != nil {
		t.Fatal(err)
	}
	s.Backends[config.CategoryLegacySandbox] = &removalBackend{state: RuntimeState{ID: "other", Known: true}}
	if err := os.RemoveAll(w); err != nil {
		t.Fatal(err)
	}
	result, err := s.Execute(context.Background(), r, nil)
	if err == nil || len(result.Remaining) != 1 {
		t.Fatal(result, err)
	}
	if _, err := os.Stat(failedPath); err != nil {
		t.Fatal("failed workspace registry lost", err)
	}
	if _, err := os.Stat(otherPath); !os.IsNotExist(err) {
		t.Fatal("successful workspace registry retained", err)
	}
}
