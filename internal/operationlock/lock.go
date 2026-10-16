// Package operationlock coordinates host mutations across processes.
package operationlock

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"paseo-dock/internal/config"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
)

var ErrConflict = errors.New("operation already running")

// SandboxKey uses the updater's existing lock file identity.
func SandboxKey(sandbox string) string { return "sandbox:" + sandbox }

// EntryKey preserves historical SBX lock files and separates Docker identities.
func EntryKey(e config.Entry) string {
	if e.EffectiveCategory() == config.CategoryContainer {
		return "container:" + e.ContainerID
	}
	return SandboxKey(e.Sandbox)
}

func Acquire(ctx context.Context, base string, keys []string) (func(), error) {
	var files []*os.File
	var once sync.Once
	release := func() {
		once.Do(func() {
			for i := len(files) - 1; i >= 0; i-- {
				_ = syscall.Flock(int(files[i].Fd()), syscall.LOCK_UN)
				_ = files[i].Close()
			}
		})
	}
	keys = slices.Clone(keys)
	slices.Sort(keys)
	keys = slices.Compact(keys)
	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			release()
			return nil, err
		}
		sum := sha256.Sum256([]byte(key))
		path := filepath.Join(base, ".locks", fmt.Sprintf("%x.lock", sum[:16]))
		if strings.HasPrefix(key, "root:") {
			path = filepath.Join(strings.TrimPrefix(key, "root:"), ".generated", "locks", "operations.lock")
		}
		if strings.HasPrefix(key, "home:") {
			path = filepath.Join(strings.TrimPrefix(key, "home:"), ".locks", "providers.lock")
		}
		if strings.HasPrefix(key, "sandbox:") {
			sum = sha256.Sum256([]byte(strings.TrimPrefix(key, "sandbox:")))
			path = filepath.Join(base, "updates", fmt.Sprintf("%x.lock", sum[:8]))
		}
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			release()
			return nil, err
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			release()
			return nil, err
		}
		if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			_ = f.Close()
			release()
			if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
				return nil, fmt.Errorf("%w: %s", ErrConflict, key)
			}
			return nil, err
		}
		files = append(files, f)
	}
	if err := ctx.Err(); err != nil {
		release()
		return nil, err
	}
	return release, nil
}
