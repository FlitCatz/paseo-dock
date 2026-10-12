package operations

import (
	"context"
	"os"
	"paseo-sbx/internal/config"
	"strings"
	"testing"
)

func TestExistingTargetIgnoresCurrentCatalog(t *testing.T) {
	s, target, _ := operationFixture(t)
	target.Profile = "codex-retired"
	target.Entry.Tool = "retired"
	target.Category = config.CategoryLegacySandbox
	writeTarget(t, target)
	s.Catalog = config.Catalog{}
	cmd, err := s.ShellCommand(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	log, _ := os.ReadFile(os.Getenv("SBX_TEST_LOG"))
	if !strings.Contains(string(log), " one bash") {
		t.Fatalf("lost saved identity: %s", log)
	}
}
