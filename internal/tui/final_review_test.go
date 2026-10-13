package tui

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"paseo-sbx/internal/config"
	"paseo-sbx/internal/operations"
)

func reviewExternal() {
	name := filepath.Base(os.Args[0])
	args := strings.Join(os.Args[1:], " ")
	f, _ := os.OpenFile(os.Getenv("TUI_REVIEW_LOG"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	fmt.Fprintln(f, name+" "+args)
	f.Close()
	if name == "paseo" {
		if strings.Contains(args, "status") {
			fmt.Println(`{"localDaemon":"running"}`)
		}
		return
	}
	switch {
	case args == "ls --json":
		fmt.Println(`[{"name":"one","status":"running"}]`)
	case args == "stop one":
	case strings.HasSuffix(args, "printenv HOME"):
		fmt.Fprintln(os.Stderr, "guest home unavailable")
		os.Exit(1)
	default:
		fmt.Fprintln(os.Stderr, "unexpected external call", args)
		os.Exit(2)
	}
}

func reviewDashboardFixture(t *testing.T) (*operations.Service, string, string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GORACE", os.Getenv("GORACE")+" atexit_sleep_ms=0")
	dir := t.TempDir()
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	t.Setenv("TUI_REVIEW_PROCESS", "1")
	t.Setenv("TUI_REVIEW_LOG", filepath.Join(dir, "commands"))
	exe, _ := os.Executable()
	for _, name := range []string{"sbx", "paseo"} {
		if err := os.Symlink(exe, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	s, err := operations.New("../..")
	if err != nil {
		t.Fatal(err)
	}
	s.Root = dir
	s.Out, s.Err = io.Discard, io.Discard
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bin", "agent-wrapper"), []byte("fixture launcher"), 0700); err != nil {
		t.Fatal(err)
	}
	workspace, err := config.Canonical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := config.RuntimePath(workspace)
	if err != nil {
		t.Fatal(err)
	}
	selection := config.Selection{Schema: 1, Pairs: []config.Pair{{Agent: "codex", Methodology: "superpowers"}}, Utilities: []string{"rtk"}}
	reg := config.Registry{Workspace: workspace, Selection: &selection, Active: []string{"codex-superpowers"}, Profiles: map[string]config.Entry{"codex-superpowers": {Workspace: workspace, Agent: "codex", Tool: "superpowers", Sandbox: "one", Image: "historical-image"}}}
	if err := config.WriteJSON(filepath.Join(runtime, "registry.json"), reg); err != nil {
		t.Fatal(err)
	}
	return s, workspace, runtime
}

func restartedDashboard(t *testing.T, s *operations.Service, workspace string) *Dashboard {
	t.Helper()
	fresh := *s
	snap, err := fresh.Snapshot(context.Background(), operations.Query{IncludePaseo: true})
	if err != nil {
		t.Fatal(err)
	}
	m := NewDashboard(&fresh, fresh.Catalog, workspace)
	t.Cleanup(m.cancel)
	deliver(m, snap)
	if m.selectedSandbox() == nil {
		t.Fatalf("missing historical registry: %+v", snap)
	}
	return m
}

func TestHistoricalSelectionManagement(t *testing.T) {
	for _, missing := range []string{"utility", "pin"} {
		for _, kind := range []operations.Kind{operations.KindStop, operations.KindUnregister, operations.KindVersions} {
			t.Run(missing+"/"+string(kind), func(t *testing.T) {
				s, workspace, runtime := reviewDashboardFixture(t)
				if missing == "utility" {
					s.Catalog.Utilities = nil
				} else {
					delete(s.Lock.Utilities, "rtk")
				}
				m := restartedDashboard(t, s, workspace)
				for _, a := range m.actions() {
					if a.kind == kind {
						m.chooseAction(a)
						break
					}
				}
				if m.confirm == nil {
					t.Fatal("historical target could not be confirmed", m.actionError)
				}
				req := m.confirm.request
				if len(req.Selection.Pairs) != 0 {
					t.Error("management request carried obsolete build selection")
				}
				// Even older callers that still supply informational selection must work.
				req.Selection = *m.selectedWorkspace().Selection
				result, err := s.Execute(context.Background(), req, nil)
				log, _ := os.ReadFile(os.Getenv("TUI_REVIEW_LOG"))
				switch kind {
				case operations.KindStop:
					if err != nil || !strings.Contains(string(log), "sbx stop one") {
						t.Fatalf("historical stop failed: %v; %s", err, log)
					}
				case operations.KindUnregister:
					var reg config.Registry
					_ = config.ReadJSON(filepath.Join(runtime, "registry.json"), &reg)
					if err != nil || len(reg.ActiveProfiles()) != 0 {
						t.Fatalf("historical unregister failed: %v; %+v", err, reg)
					}
				case operations.KindVersions:
					if err == nil || result.VersionErrors["environment"] == "" || !strings.Contains(string(log), "printenv HOME") {
						t.Fatalf("historical probe did not reach guest: %+v %v", result, err)
					}
				}
				for _, buildKind := range []operations.Kind{operations.KindBuild, operations.KindLoad, operations.KindCreate} {
					req.Kind = buildKind
					if _, err := s.Execute(context.Background(), req, nil); err == nil {
						t.Fatalf("obsolete %s selection accepted", buildKind)
					}
				}
			})
		}
	}
}

func TestProviderConfigurationDiagnosticSurvivesSnapshot(t *testing.T) {
	for _, failure := range []string{"parse", "read"} {
		t.Run(failure, func(t *testing.T) {
			s, w, _ := reviewDashboardFixture(t)
			path := filepath.Join(os.Getenv("HOME"), ".paseo", "config.json")
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if failure == "parse" {
				_ = os.WriteFile(path, []byte(`{"agents":`), 0600)
			} else {
				_ = os.Mkdir(path, 0700)
			}
			m := restartedDashboard(t, s, w)
			detail := m.settingsContent()
			if m.Snapshot.Paseo.Status != "running" || m.Snapshot.Paseo.Error != "" || m.selectedSandbox().Registration != "unknown" {
				t.Fatal("provider observation conflated with daemon connectivity")
			}
			if !strings.Contains(detail, "config.json") || !strings.Contains(detail, filepath.Dir(path)) {
				t.Fatalf("provider error path lost: %s", detail)
			}
			if failure == "parse" && !strings.Contains(detail, "unexpected end") {
				t.Fatalf("provider parse cause lost: %s", detail)
			}
		})
	}
}

func TestWhollyFailedVersionCheckSurvivesRestart(t *testing.T) {
	s, w, _ := reviewDashboardFixture(t)
	m := restartedDashboard(t, s, w)
	result, err := s.Execute(context.Background(), operations.Request{Kind: operations.KindVersions, Target: m.selectedSandbox().Target}, nil)
	if err == nil || len(result.Versions) != 0 || !result.VersionCheckedAt.IsZero() {
		t.Fatalf("failed check fabricated confirmation: %+v %v", result, err)
	}
	restarted := restartedDashboard(t, s, w)
	check := restarted.selectedSandbox().VersionCheck
	if check == nil || len(check.Errors) == 0 || !check.CheckedAt.IsZero() || len(check.Versions) != 0 {
		t.Fatalf("failed check ledger lost or fabricated: %+v", check)
	}
	detail := restarted.versionContent()
	if !strings.Contains(detail, "guest home unavailable") || !strings.Contains(detail, "버전 확인 오류") || strings.Contains(detail, "버전 마지막 확인:") {
		t.Fatalf("failed check hidden or presented as confirmed: %s", detail)
	}
}
