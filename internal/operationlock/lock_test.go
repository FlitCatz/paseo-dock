package operationlock

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestAcquireSharedPathAliases(t *testing.T) {
	dir := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(dir, alias); err != nil {
		t.Fatal(err)
	}
	for _, prefix := range []string{"root:", "home:"} {
		t.Run(prefix, func(t *testing.T) {
			release, err := Acquire(context.Background(), t.TempDir(), []string{prefix + dir})
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			retry, err := Acquire(context.Background(), t.TempDir(), []string{prefix + alias})
			if err == nil {
				retry()
				t.Fatal("symlink alias bypassed shared output lock")
			}
		})
	}
}

// The same root output or Paseo home must conflict even across different runtime bases.
func TestAcquireSharedPathsAcrossRuntimeBases(t *testing.T) {
	for _, prefix := range []string{"root:", "home:"} {
		t.Run(prefix, func(t *testing.T) {
			shared := filepath.Join(t.TempDir(), "shared")
			key := prefix + shared
			release, err := Acquire(context.Background(), t.TempDir(), []string{key})
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			retry, err := Acquire(context.Background(), t.TempDir(), []string{key})
			if err == nil {
				retry()
				t.Fatal("same shared path acquired through a different runtime base")
			}
		})
	}
}

// Conflict rejection and partial-acquisition unwind permit a later independent operation.
func TestAcquireConflictAndUnwind(t *testing.T) {
	base := t.TempDir()
	ctx := context.Background()
	release, err := Acquire(ctx, base, []string{"b"})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	other, err := Acquire(ctx, base, []string{"b", "a"})
	if err == nil {
		other()
		t.Fatal("concurrent mutation accepted")
	}
	retry, err := Acquire(ctx, base, []string{"a"})
	if err != nil {
		t.Fatal("partial acquisition not unwound", err)
	}
	retry()
	release()
	retry, err = Acquire(ctx, base, []string{"b", "b"})
	if err != nil {
		t.Fatal("release or duplicate keys blocked retry", err)
	}
	retry()
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	if release, err := Acquire(ctx, base, []string{"a"}); err == nil {
		release()
		t.Fatal("canceled lock acquisition accepted")
	}
}
