package operations

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestSBXRemoveExactArgumentsWithoutStop(t *testing.T) {
	s, target, _ := operationFixture(t)
	argvPath := filepath.Join(t.TempDir(), "argv.json")
	t.Setenv("SBX_TEST_ARGV_FILE", argvPath)
	b := &SBXBackend{Root: s.Root}
	if err := b.Remove(context.Background(), target.Entry); err != nil {
		t.Fatal(err)
	}
	log, _ := os.ReadFile(os.Getenv("SBX_TEST_LOG"))
	if string(log) != "rm --force "+target.Entry.Sandbox+"\n" {
		t.Fatal(string(log))
	}
	var args []string
	raw, err := os.ReadFile(argvPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(args, []string{"rm", "--force", target.Entry.Sandbox}) {
		t.Fatal(args)
	}
}

func TestSBXAbsenceRequiresSuccessfulList(t *testing.T) {
	s, target, _ := operationFixture(t)
	b := &SBXBackend{Root: s.Root}
	_, err := b.Inspect(context.Background(), target.Entry)
	if !errors.Is(err, ErrRuntimeNotFound) {
		t.Fatal(err)
	}
	t.Setenv("SBX_TEST_NAMES", "invalid json")
	_, err = b.Inspect(context.Background(), target.Entry)
	if err == nil || errors.Is(err, ErrRuntimeNotFound) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = b.Inspect(ctx, target.Entry)
	if err == nil || errors.Is(err, ErrRuntimeNotFound) {
		t.Fatal(err)
	}
	log, _ := os.ReadFile(os.Getenv("SBX_TEST_LOG"))
	if strings.Contains(string(log), "stop") || strings.Contains(string(log), "rm") {
		t.Fatal(string(log))
	}
}
