package operations

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"paseo-dock/internal/config"
)

func TestGeneratedProvidersMapRenamedBase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "paseo.providers.json")
	if err := config.WriteJSON(path, ProviderConfig("/trusted/launcher", map[string]string{"sbx-antigravity-cli": "antigravity-cli"})); err != nil {
		t.Fatal(err)
	}
	var generated map[string]any
	if err := config.ReadJSON(path, &generated); err != nil {
		t.Fatal(err)
	}
	current := map[string]any{"version": 1}
	MergeManagedConfig(current, generated, "/trusted")
	provider := object(object(current, "agents"), "providers")["sbx-antigravity-cli"].(map[string]any)
	if provider["extends"] != "antigravity" {
		t.Fatalf("generated config has dangling base: %v", provider["extends"])
	}
}

func TestFilterGeneratedProviderBases(t *testing.T) {
	var warnings bytes.Buffer
	calls := map[string]int{}
	s := &Service{Err: &warnings, ValidatePaseoConfig: func(_ context.Context, home string) error {
		var probe map[string]any
		if err := config.ReadJSON(filepath.Join(home, "config.json"), &probe); err != nil {
			return err
		}
		p := object(object(probe, "agents"), "providers")["paseo-sbx-base-probe"].(map[string]any)
		base := p["extends"].(string)
		calls[base]++
		if base == "missing" {
			return fmt.Errorf("Provider %q extends unknown provider %q.", "paseo-sbx-base-probe", base)
		}
		return nil
	}}
	path := filepath.Join(t.TempDir(), "paseo.providers.json")
	generated := map[string]any{"agents": map[string]any{"providers": map[string]any{
		"old":         map[string]any{"extends": "antigravity-cli"},
		"current":     map[string]any{"extends": "antigravity"},
		"unsupported": map[string]any{"extends": "missing"},
		"new-version": map[string]any{"extends": "future-provider"},
	}}}
	if err := config.WriteJSON(path, generated); err != nil {
		t.Fatal(err)
	}
	if err := config.ReadJSON(path, &generated); err != nil {
		t.Fatal(err)
	}
	if err := s.execution(context.Background(), nil, nil).filterProviderBases(generated); err != nil {
		t.Fatal(err)
	}
	providers := object(object(generated, "agents"), "providers")
	if len(providers) != 3 || providers["unsupported"] != nil || providers["old"].(map[string]any)["extends"] != "antigravity" {
		t.Fatal(providers)
	}
	if calls["antigravity"] != 1 || calls["future-provider"] != 1 {
		t.Fatal(calls)
	}
	if !strings.Contains(warnings.String(), `skipping provider "unsupported": base "missing"`) {
		t.Fatal(warnings.String())
	}
}

func TestProviderDiscoveryFailureStopsSync(t *testing.T) {
	s, _, home := providerScopeFixture(t)
	path := filepath.Join(home, "config.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	failure := errors.New("validator unavailable")
	s.ValidatePaseoConfig = func(context.Context, string) error { return failure }
	err = s.execution(context.Background(), nil, nil).syncProviders(home, true)
	if !errors.Is(err, failure) {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("config changed: %s %v", after, err)
	}
}

func TestProviderValidationRollback(t *testing.T) {
	for _, exists := range []bool{true, false} {
		t.Run(fmt.Sprint("existing=", exists), func(t *testing.T) {
			s, targets, home := providerScopeFixture(t)
			home, _ = config.Canonical(home)
			path := filepath.Join(home, "config.json")
			original := []byte("{\n  \"version\": 1, \"untouched\": true\n}\n")
			if exists {
				if err := os.WriteFile(path, original, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(path, 0640); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			wrapper := filepath.Join(targets[0].Runtime, "agent-wrapper")
			if err := os.WriteFile(wrapper, []byte("old wrapper"), 0700); err != nil {
				t.Fatal(err)
			}
			failure := errors.New("Paseo rejected new config")
			validated := false
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			s.ValidatePaseoConfig = func(_ context.Context, checkHome string) error {
				if checkHome != home {
					return nil
				}
				validated = true
				var candidate map[string]any
				if err := config.ReadJSON(path, &candidate); err != nil {
					t.Fatal(err)
				}
				if len(object(object(candidate, "agents"), "providers")) == 0 {
					t.Fatal("validator did not see merged config")
				}
				cancel() // Rollback must work even after the request is canceled.
				return failure
			}
			err := s.execution(ctx, nil, nil).syncProviders(home, true)
			if !validated || !errors.Is(err, failure) || !strings.Contains(err.Error(), "original config restored") {
				t.Fatal(err)
			}
			after, err := os.ReadFile(path)
			if exists {
				if err != nil || !bytes.Equal(original, after) {
					t.Fatalf("original bytes not restored: %s %v", after, err)
				}
				info, err := os.Stat(path)
				if err != nil || info.Mode().Perm() != 0640 {
					t.Fatalf("mode not restored: %v %v", info, err)
				}
			} else if !os.IsNotExist(err) {
				t.Fatalf("new config survived: %s %v", after, err)
			}
			if _, err := os.Stat(wrapper); err != nil {
				t.Fatal("old launcher removed before validation", err)
			}
		})
	}
}

func TestProviderValidationSuccessRetainsBackup(t *testing.T) {
	s, _, home := providerScopeFixture(t)
	home, _ = config.Canonical(home)
	path := filepath.Join(home, "config.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	validated := false
	s.ValidatePaseoConfig = func(_ context.Context, checkHome string) error {
		if checkHome == home {
			validated = true
		}
		return nil
	}
	if err := s.execution(context.Background(), nil, nil).syncProviders(home, true); err != nil {
		t.Fatal(err)
	}
	backups, err := filepath.Glob(path + ".backup-*")
	if err != nil || len(backups) != 1 || !validated {
		t.Fatalf("backup/validation missing: %v %v", backups, err)
	}
	backup, err := os.ReadFile(backups[0])
	if err != nil || !bytes.Equal(before, backup) {
		t.Fatal("backup differs", err)
	}
}

func TestRemoveValidationFailurePreservesConfigAndResource(t *testing.T) {
	s, request, _, backend := removalFixture(t)
	home, err := config.Canonical(request.PaseoHome)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "config.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s.ValidatePaseoConfig = func(_ context.Context, checkHome string) error {
		if checkHome == home {
			return errors.New("invalid user configuration")
		}
		return nil
	}
	_, err = s.Execute(context.Background(), request, nil)
	if err == nil || !strings.Contains(err.Error(), "original config restored") || backend.removed != 0 {
		t.Fatalf("removal proceeded: %v, removed=%d", err, backend.removed)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("original config changed", err)
	}
}

// Opt in to check the installed CLI contract without connecting to a daemon.
func TestInstalledPaseoConfigValidator(t *testing.T) {
	if os.Getenv("PASEO_DOCK_TEST_INSTALLED_PASEO") != "1" {
		t.Skip("set PASEO_DOCK_TEST_INSTALLED_PASEO=1 to check the installed Paseo CLI")
	}
	s := &Service{Root: t.TempDir()}
	a := s.execution(context.Background(), nil, nil)
	generated := ProviderConfig("/trusted/launcher", map[string]string{"sbx-codex": "codex", "sbx-antigravity-cli": "antigravity-cli", "sbx-unsupported": "paseo-sbx-nonexistent-base"})
	if err := a.filterProviderBases(generated); err != nil {
		t.Fatal(err)
	}
	providers := object(object(generated, "agents"), "providers")
	if providers["sbx-codex"] == nil || providers["sbx-unsupported"] != nil {
		t.Fatal(providers)
	}
	if p, ok := providers["sbx-antigravity-cli"].(map[string]any); ok && p["extends"] != "antigravity" {
		t.Fatal(p)
	}
	home := t.TempDir()
	if err := config.WriteJSON(filepath.Join(home, "config.json"), generated); err != nil {
		t.Fatal(err)
	}
	if err := a.validatePaseoConfig(home); err != nil {
		t.Fatal(err)
	}
}

func TestPaseoValidatorDecodesStderrDiagnostic(t *testing.T) {
	s, _, _ := providerScopeFixture(t)
	message := `Provider "probe" extends unknown provider "missing".`
	t.Setenv("SBX_TEST_VALIDATOR_ERROR", message)
	err := s.execution(context.Background(), nil, nil).validatePaseoConfig(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), message) {
		t.Fatalf("lost Paseo diagnostic: %v", err)
	}
}
