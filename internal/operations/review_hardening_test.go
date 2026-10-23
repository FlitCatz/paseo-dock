package operations

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"paseo-dock/internal/config"
)

func TestDefaultDockerSecurityRejectsEscalationWithoutMutation(t *testing.T) {
	for _, change := range []string{"privileged", "capadd", "host-pid", "host-ipc", "container-ipc", "host-network"} {
		t.Run(change, func(t *testing.T) {
			b, e, raw, log := dockerFixture(t)
			host := raw["HostConfig"].(map[string]any)
			switch change {
			case "privileged":
				host["Privileged"] = true
			case "capadd":
				host["CapAdd"] = []string{"SYS_ADMIN"}
			case "host-pid":
				host["PidMode"] = "host"
			case "host-ipc":
				host["IpcMode"] = "host"
			case "container-ipc":
				host["IpcMode"] = "container:other"
			case "host-network":
				host["NetworkMode"] = "host"
			}
			writeDockerInspect(t, raw)
			if _, err := b.Inspect(context.Background(), e); err == nil {
				t.Fatal("unsafe container accepted")
			}
			if err := b.Stop(context.Background(), e); err == nil {
				t.Fatal("unsafe container stopped")
			}
			if err := b.Exec(context.Background(), e, []string{"true"}, nil, io.Discard, io.Discard); err == nil {
				t.Fatal("unsafe container executed")
			}
			calls, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			for _, mutation := range []string{`"stop"`, `"exec"`, `"rm"`} {
				if strings.Contains(string(calls), mutation) {
					t.Fatal("unsafe container mutated", string(calls))
				}
			}
		})
	}
}

func TestDefaultDockerSecurityAcceptsDaemonDefaults(t *testing.T) {
	b, e, raw, _ := dockerFixture(t)
	for _, ipc := range []string{"", "private", "shareable", "none"} {
		raw["HostConfig"].(map[string]any)["IpcMode"] = ipc
		writeDockerInspect(t, raw)
		if _, err := b.Inspect(context.Background(), e); err != nil {
			t.Fatal(err)
		}
		if _, err := b.Create(context.Background(), e); err != nil {
			t.Fatalf("reuse container with IPC %q: %v", ipc, err)
		}
	}
}

func TestDefaultDockerCreationPinsPrivateIPC(t *testing.T) {
	b, e, raw, log := dockerFixture(t)
	e.ContainerID = ""
	raw["HostConfig"].(map[string]any)["IpcMode"] = "private"
	writeDockerInspect(t, raw)
	if _, err := b.Create(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	if calls := dockerCalls(t, log); !strings.Contains(calls, `"--ipc","private"`) {
		t.Fatal("creation depends on daemon IPC defaults", calls)
	}
}

// Reproduces the provider-loss path: global sync used to skip a corrupt registry,
// then remove its owned providers from the existing Paseo configuration.
func TestProviderSyncCorruptRegistryFailsBeforeAnyWrite(t *testing.T) {
	for _, contents := range []string{"{", `{"schema":999}`, `{"profiles":{}}`, "dangling-symlink"} {
		t.Run(contents, func(t *testing.T) {
			s, targets, home := providerScopeFixture(t)
			path := filepath.Join(targets[0].Runtime, "registry.json")
			if contents == "dangling-symlink" {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("missing-registry.json", path); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
					t.Fatal(err)
				}
			}
			configPath := filepath.Join(home, "config.json")
			before, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			a := s.execution(context.Background(), nil, nil)
			if err := a.syncProviders(home, true); err == nil {
				t.Fatal("corrupt registry silently treated as empty")
			}
			after, err := os.ReadFile(configPath)
			if err != nil || string(after) != string(before) {
				t.Fatal("provider config changed", err)
			}
			if contents == "dangling-symlink" {
				if target, err := os.Readlink(path); err != nil || target != "missing-registry.json" {
					t.Fatal("registry symlink changed", target, err)
				}
			} else {
				reg, err := os.ReadFile(path)
				if err != nil || string(reg) != contents {
					t.Fatal("registry changed", err)
				}
			}
			base, _ := config.BaseDir()
			for _, output := range []string{config.LauncherPath(base), filepath.Join(s.Root, ".generated", "paseo.providers.json"), configPath + ".bak"} {
				if _, err := os.Stat(output); !os.IsNotExist(err) {
					t.Fatalf("wrote %s: %v", output, err)
				}
			}
		})
	}
}
