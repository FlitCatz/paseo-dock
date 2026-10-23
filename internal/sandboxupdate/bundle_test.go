package sandboxupdate

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"paseo-dock/internal/config"
)

type bundleFixture struct {
	fixture
	head string
}

func (f *bundleFixture) run(ctx context.Context, args []string, capture bool) (string, error) {
	s := strings.Join(args, " ")
	switch {
	case strings.HasPrefix(s, "cat ") && strings.HasSuffix(s, "/installed.json") && !strings.Contains(s, "utilities"):
		f.calls = append(f.calls, s)
		return `{"pairs":[{"agent":"claude","methodology":"superpowers"}],"lock":{"tools":{"superpowers":{"repository":"https://example.invalid/sp.git"},"openspec":{"package":"@fission-ai/openspec"}}}}`, nil
	case strings.HasSuffix(s, "rev-parse HEAD"):
		f.calls = append(f.calls, s)
		return f.head, nil
	case strings.HasSuffix(s, "rev-parse FETCH_HEAD"):
		f.calls = append(f.calls, s)
		return "8ca22dba9a94f28898bbce59f2537ff4d87c747d", nil
	case strings.Contains(s, "checkout --detach 8ca22dba9a94f28898bbce59f2537ff4d87c747d"):
		f.head = "new"
	}
	if strings.HasPrefix(s, "cat ") && strings.Contains(s, "utilities/installed.json") {
		f.calls = append(f.calls, s)
		return "", os.ErrNotExist
	}
	return f.fixture.run(ctx, args, capture)
}

func TestBundleUpdatesEveryAgentAndSharedMethodologyOnce(t *testing.T) {
	f := &bundleFixture{head: "old"}
	entry := config.Entry{Category: config.CategoryContainer, Name: "dev", ContainerID: "c", Pairs: []config.Pair{{Agent: "claude", Methodology: "superpowers"}, {Agent: "codex", Methodology: "openspec"}, {Agent: "codex", Methodology: "superpowers"}, {Agent: "omp", Methodology: "native"}}}
	s := refresh(context.Background(), entry, f.run, time.Now())
	calls := strings.Join(f.calls, "\n")
	if len(s.Errors) != 0 || s.Success.IsZero() {
		t.Fatal(s.Errors, calls)
	}
	for _, id := range []string{"agent:claude", "agent:codex", "methodology:openspec", "methodology:superpowers"} {
		if s.Versions[id] == "" {
			t.Fatalf("missing %s: %v", id, s.Versions)
		}
	}
	// omp stays pinned; the shared plugin is fetched once and reinstalled for both agents.
	if s.Versions["agent:omp"] != "" || strings.Contains(calls, "omp") || strings.Count(calls, "fetch --depth 1") != 1 ||
		!strings.Contains(calls, "codex plugin add superpowers@paseo-dock") || !strings.Contains(calls, "claude plugin update superpowers@paseo-dock") {
		t.Fatal(calls)
	}
}

func TestPinnedAgentsAreSupported(t *testing.T) {
	f := &fixture{}
	s := refresh(context.Background(), config.Entry{Category: config.CategoryContainer, Agent: "gjc", Tool: "native", ContainerID: "c"}, f.run, time.Now())
	if s.Errors["agent"] != "" || s.Versions["agent"] != "" || !supported(config.Entry{Agent: "omp"}) {
		t.Fatal(s)
	}
}
