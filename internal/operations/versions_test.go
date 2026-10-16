package operations

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"paseo-dock/internal/config"
)

// Any installer/network query or activation mutation fails the strict external boundary.
func TestVersionsDoesNotInstall(t *testing.T) {
	s, target, log := observationFixture(t)
	target.Profile = "codex-superpowers"
	target.Entry.Tool = "superpowers"
	writeTarget(t, target)
	observationResponses(t, map[string]observedResponse{
		"sbx exec --workdir /tmp one printenv HOME":                                                                            {Output: "/home/agent\n"},
		"sbx exec --workdir /tmp one codex --version":                                                                          {Output: "codex-cli 2.0.0\n"},
		"sbx exec --workdir /tmp one cat /home/agent/.local/share/paseo-sbx/installed.json":                                    {Output: `{"agent":"codex","tool":"superpowers","lock":{"schema":1,"tools":{"superpowers":{"repository":"https://example.invalid/tool","revision":"image-old"}}}}`},
		"sbx exec --workdir /tmp one git -C /home/agent/.local/share/paseo-sbx/marketplace/plugins/superpowers rev-parse HEAD": {Output: "aabbccdd\n"},
		"sbx exec --workdir /tmp one cat /home/agent/.local/share/paseo-sbx/utilities/installed.json":                          {Output: `{"agent":"codex","utilities":{"rtk":{"installer":"rtk","version":"old"}}}`},
		"sbx exec --workdir /tmp one rtk --version":                                                                            {Output: "rtk 0.99.0\n"},
		"sbx ls --json": {Output: `[]`},
	})
	before, err := os.ReadFile(filepath.Join(target.Runtime, "registry.json"))
	if err != nil {
		t.Fatal(err)
	}
	began := time.Now()
	var warningBeforeProbe bool
	result, err := s.Execute(context.Background(), Request{Kind: KindVersions, Target: target}, func(e Event) {
		if e.Stage == "warning" {
			b, _ := os.ReadFile(log)
			warningBeforeProbe = len(b) == 0 && strings.Contains(e.Text, "start")
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if !warningBeforeProbe || len(result.Warnings) == 0 {
		t.Fatal("guest-start warning missing before first exec")
	}
	if result.Versions["agent"] != "codex-cli 2.0.0" || result.Versions["methodology"] != "aabbccdd" || result.Versions["utility:rtk"] != "rtk 0.99.0" || len(result.VersionErrors) != 0 {
		t.Fatalf("probes lost: %+v", result)
	}
	if result.VersionCheckedAt.Before(began) || result.VersionCheckedAt.After(time.Now()) {
		t.Fatal("confirmation timestamp is not from actual observation")
	}
	after, _ := os.ReadFile(filepath.Join(target.Runtime, "registry.json"))
	if !reflect.DeepEqual(before, after) {
		t.Fatal("versions changed activation")
	}
	if paths, err := filepath.Glob(filepath.Join(target.Runtime, "updates", "*.json")); err != nil || len(paths) != 0 {
		t.Fatal("version check changed updater schedule")
	}
	// The confirmation must survive a fresh immutable Service without another exec.
	restarted := *s
	_, row := snapshotRow(t, &restarted, Query{PaseoHome: t.TempDir()})
	if row.VersionCheck == nil || !row.VersionCheck.CheckedAt.Equal(result.VersionCheckedAt) || row.VersionCheck.Versions["agent"] != "codex-cli 2.0.0" {
		t.Fatalf("explicit observation not persisted: %+v", row.VersionCheck)
	}
}
func TestVersionsPartialFailureDoesNotConfirmImageLock(t *testing.T) {
	s, target, _ := observationFixture(t)
	target.Profile = "codex-gsd-core"
	target.Entry.Tool = "gsd-core"
	writeTarget(t, target)
	observationResponses(t, map[string]observedResponse{
		"sbx exec --workdir /tmp one printenv HOME":                                                   {Output: "/home/agent\n"},
		"sbx exec --workdir /tmp one codex --version":                                                 {Output: "2.0.0"},
		"sbx exec --workdir /tmp one cat /home/agent/.local/share/paseo-sbx/installed.json":           {Output: `{"agent":"codex","tool":"gsd-core","lock":{"schema":1,"tools":{"gsd-core":{"package":"gsd-core","version":"image-only-1.0"}}}}`},
		"sbx exec --workdir /tmp one cat /home/agent/.local/share/paseo-sbx/utilities/installed.json": {Error: true},
		"sbx ls --json": {Output: `[]`},
	})
	result, err := s.Execute(context.Background(), Request{Kind: KindVersions, Target: target}, nil)
	if err == nil || result.Versions["agent"] != "2.0.0" || result.Versions["methodology"] != "" || result.VersionErrors["methodology"] == "" || result.VersionCheckedAt.IsZero() {
		t.Fatalf("unconfirmed lock value or partial success lost: %+v %v", result, err)
	}
	_, row := snapshotRow(t, s, Query{PaseoHome: t.TempDir()})
	if row.VersionCheck == nil || row.VersionCheck.Errors["methodology"] == "" {
		t.Fatal("partial errors were not persisted")
	}
}
func TestVersionCheckProvenance(t *testing.T) {
	for _, tc := range []struct {
		name, sandbox, image string
		want                 bool
	}{{"matching", "one", "image", true}, {"wrong-image", "one", "old", false}, {"wrong-sandbox", "other", "image", false}} {
		t.Run(tc.name, func(t *testing.T) {
			s, target, _ := observationFixture(t)
			observationResponses(t, map[string]observedResponse{"sbx ls --json": {Output: `[]`}})
			check := VersionCheck{Sandbox: tc.sandbox, Image: tc.image, CheckedAt: time.Now().UTC(), Versions: map[string]string{"agent": "2.0"}, Errors: map[string]string{"methodology": "unknown"}}
			// Hash derived independently, matching the established update-ledger convention.
			path := filepath.Join(target.Runtime, "version-checks", "7692c3ad3540bb80.json")
			if err := config.WriteJSON(path, check); err != nil {
				t.Fatal(err)
			}
			_, row := snapshotRow(t, s, Query{PaseoHome: t.TempDir()})
			if (row.VersionCheck != nil) != tc.want {
				t.Fatalf("wrong explicit confirmation identity: %+v", row.VersionCheck)
			}
		})
	}
}
func TestVersionsCancellationDoesNotConfirm(t *testing.T) {
	s, target, _ := observationFixture(t)
	observationResponses(t, map[string]observedResponse{"sbx exec --workdir /tmp one printenv HOME": {Output: "/home/agent", Delay: time.Second}})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	result, err := s.Execute(ctx, Request{Kind: KindVersions, Target: target}, nil)
	if !errors.Is(err, context.DeadlineExceeded) || !result.VersionCheckedAt.IsZero() || len(result.Versions) != 0 || !result.NeedsVerification {
		t.Fatalf("cancellation invented confirmation or missed exec start: %+v %v", result, err)
	}
}
func TestVersionsRejectsChangedTarget(t *testing.T) {
	s, target, log := observationFixture(t)
	replacement := target
	replacement.Entry.Sandbox = "changed"
	writeTarget(t, replacement)
	result, err := s.Execute(context.Background(), Request{Kind: KindVersions, Target: target}, nil)
	if err == nil || result.Changed {
		t.Fatalf("unconfirmed target accepted: %+v %v", result, err)
	}
	b, _ := os.ReadFile(log)
	if len(b) > 0 {
		t.Fatalf("executed changed target: %s", b)
	}
}

func TestVersionsMalformedUtilityManifestIsErrorWithEmptySelection(t *testing.T) {
	s, target, _ := observationFixture(t)
	var reg config.Registry
	_ = config.ReadJSON(filepath.Join(target.Runtime, "registry.json"), &reg)
	reg.Selection = &config.Selection{Schema: 1, Utilities: []string{}}
	_ = config.WriteJSON(filepath.Join(target.Runtime, "registry.json"), reg)
	observationResponses(t, map[string]observedResponse{
		"sbx exec --workdir /tmp one printenv HOME":                                                   {Output: "/home/agent"},
		"sbx exec --workdir /tmp one codex --version":                                                 {Output: "2.0"},
		"sbx exec --workdir /tmp one cat /home/agent/.local/share/paseo-sbx/installed.json":           {Output: `{"agent":"codex","tool":"native","lock":{}}`},
		"sbx exec --workdir /tmp one cat /home/agent/.local/share/paseo-sbx/utilities/installed.json": {Output: `{`},
	})
	result, err := s.Execute(context.Background(), Request{Kind: KindVersions, Target: target}, nil)
	if err == nil || result.VersionErrors["utilities"] == "" {
		t.Fatalf("malformed installed manifest suppressed: %+v %v", result, err)
	}
}

// Utility IDs are valid custom names but must not overwrite reserved confirmations or errors.
func TestVersionsUtilityIDsCannotOverwriteComponentProvenance(t *testing.T) {
	for _, tc := range []struct {
		name, manifest    string
		rtk, headroom     observedResponse
		wantVersions      map[string]string
		wantUtilityErrors []string
	}{
		{name: "success", manifest: `{"agent":"codex","utilities":{"agent":{"installer":"rtk"},"methodology":{"installer":"headroom"}}}`, rtk: observedResponse{Output: "rtk 0.99.0"}, headroom: observedResponse{Output: "headroom 3.0.0"}, wantVersions: map[string]string{"agent": "codex-cli 2.0.0", "utility:agent": "rtk 0.99.0", "utility:methodology": "headroom 3.0.0"}},
		{name: "probe-errors", manifest: `{"agent":"codex","utilities":{"agent":{"installer":"rtk"},"methodology":{"installer":"headroom"}}}`, rtk: observedResponse{Error: true}, headroom: observedResponse{Error: true}, wantVersions: map[string]string{"agent": "codex-cli 2.0.0"}, wantUtilityErrors: []string{"utility:agent", "utility:methodology"}},
		{name: "metadata-errors", manifest: `{"agent":"codex","utilities":{"agent":[],"methodology":{"installer":"unsupported"}}}`, wantVersions: map[string]string{"agent": "codex-cli 2.0.0"}, wantUtilityErrors: []string{"utility:agent", "utility:methodology"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, target, log := observationFixture(t)
			target.Profile = "codex-gsd-core"
			target.Entry.Tool = "gsd-core"
			writeTarget(t, target)
			observationResponses(t, map[string]observedResponse{
				"sbx exec --workdir /tmp one printenv HOME":                                                   {Output: "/home/agent"},
				"sbx exec --workdir /tmp one codex --version":                                                 {Output: "codex-cli 2.0.0"},
				"sbx exec --workdir /tmp one cat /home/agent/.local/share/paseo-sbx/installed.json":           {Output: `{"agent":"codex","tool":"gsd-core","lock":{"schema":1,"tools":{"gsd-core":{"package":"gsd-core","version":"image-only"}}}}`},
				"sbx exec --workdir /tmp one cat /home/agent/.local/share/paseo-sbx/utilities/installed.json": {Output: tc.manifest},
				"sbx exec --workdir /tmp one rtk --version":                                                   tc.rtk,
				"sbx exec --workdir /tmp one headroom --version":                                              tc.headroom,
				"sbx ls --json": {Output: `[]`},
			})
			var warnedBeforeExec bool
			result, err := s.Execute(context.Background(), Request{Kind: KindVersions, Target: target}, func(e Event) {
				if e.Stage == "warning" {
					b, _ := os.ReadFile(log)
					warnedBeforeExec = len(b) == 0 && strings.Contains(e.Text, "start")
				}
			})
			if err == nil {
				t.Fatal("GSD unconfirmed error lost")
			}
			if !reflect.DeepEqual(result.Versions, tc.wantVersions) {
				t.Fatalf("utility ID overwrote component confirmation: got %v want %v", result.Versions, tc.wantVersions)
			}
			if result.VersionErrors["agent"] != "" || !strings.Contains(result.VersionErrors["methodology"], "unconfirmed") {
				t.Fatalf("utility ID overwrote component error: %v", result.VersionErrors)
			}
			if len(result.VersionErrors) != 1+len(tc.wantUtilityErrors) {
				t.Fatalf("unscoped utility errors: %v", result.VersionErrors)
			}
			for _, key := range tc.wantUtilityErrors {
				if result.VersionErrors[key] == "" || !strings.Contains(err.Error(), key+":") {
					t.Fatalf("utility error missing namespace: %v; %v", result.VersionErrors, err)
				}
			}
			if !warnedBeforeExec || len(result.Warnings) == 0 || result.VersionCheckedAt.IsZero() {
				t.Fatal("warning or actual confirmation timestamp lost")
			}
			// A fresh Service poll reads the actual persisted ledger, including the namespace.
			restarted := *s
			_, row := snapshotRow(t, &restarted, Query{PaseoHome: t.TempDir()})
			if row.VersionCheck == nil || !reflect.DeepEqual(row.VersionCheck.Versions, tc.wantVersions) || row.VersionCheck.Errors["agent"] != "" || !strings.Contains(row.VersionCheck.Errors["methodology"], "unconfirmed") || len(row.VersionCheck.Errors) != 1+len(tc.wantUtilityErrors) {
				t.Fatalf("persisted provenance collision: %+v", row.VersionCheck)
			}
			for _, key := range tc.wantUtilityErrors {
				if row.VersionCheck.Errors[key] == "" {
					t.Fatalf("persisted utility error lacks namespace: %+v", row.VersionCheck)
				}
			}
		})
	}
}
