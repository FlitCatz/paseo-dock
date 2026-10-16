package config

import "testing"

func TestGetenvPrefersDockAndFallsBackToSBX(t *testing.T) {
	for _, tc := range []struct{ dock, sbx, want string }{
		{"", "", ""},
		{"", "0", "0"},
		{"1", "0", "1"},
		{"/dock", "", "/dock"},
	} {
		t.Setenv("PASEO_DOCK_ROOT", tc.dock)
		t.Setenv("PASEO_SBX_ROOT", tc.sbx)
		if got := Getenv("ROOT"); got != tc.want {
			t.Fatalf("dock=%q sbx=%q: got %q, want %q", tc.dock, tc.sbx, got, tc.want)
		}
	}
}

func TestRecordedNamespaceKeepsExistingWorkspaceNamespace(t *testing.T) {
	r := Registry{Profiles: map[string]Entry{
		"codex-superpowers":             {Image: "paseo-sbx/codex-superpowers:2026.09.30-abc"},
		"container/claude/openspec":     {Image: "ghcr.io/owner/repo/container/claude/openspec:t1"},
		"container/codex/ponytail":      {Image: "unrelated:latest"},
		"container/bundle/dev":          {},
		"container/antigravity-cli/gsd": {Image: "paseo-sbx/container/antigravity-cli/gsd:t2"},
	}}
	for _, tc := range []struct {
		profiles []string
		want     string
	}{
		{[]string{"container/claude/openspec"}, "ghcr.io/owner/repo"},
		{[]string{"codex-superpowers"}, "paseo-sbx"},
		// A new profile in an existing workspace follows the workspace's recorded entries.
		{[]string{"container/codex/new"}, "paseo-sbx"},
		{nil, "paseo-sbx"},
	} {
		if got := RecordedNamespace(r, tc.profiles); got != tc.want {
			t.Fatalf("%v: got %q, want %q", tc.profiles, got, tc.want)
		}
	}
	if got := RecordedNamespace(Registry{}, []string{"codex-superpowers"}); got != "" {
		t.Fatalf("new workspace got %q", got)
	}
	if got := RecordedNamespace(Registry{Profiles: map[string]Entry{"x": {Image: "unrelated:latest"}}}, nil); got != "" {
		t.Fatalf("unparseable image got %q", got)
	}
}
