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
	for _, ipc := range []string{"", "private"} {
		raw["HostConfig"].(map[string]any)["IpcMode"] = ipc
		writeDockerInspect(t, raw)
		if _, err := b.Inspect(context.Background(), e); err != nil {
			t.Fatal(err)
		}
	}
}

// Reproduces the provider-loss path: global sync used to skip a corrupt registry,
// then remove its owned providers from the existing Paseo configuration.
func TestProviderSyncCorruptRegistryFailsBeforeAnyWrite(t *testing.T) {
	for _, contents := range []string{"{", `{"schema":999}`, `{"profiles":{}}`} {
		t.Run(contents, func(t *testing.T) {
			s, targets, home := providerScopeFixture(t)
			path := filepath.Join(targets[0].Runtime, "registry.json")
			if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
				t.Fatal(err)
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
			reg, err := os.ReadFile(path)
			if err != nil || string(reg) != contents {
				t.Fatal("registry changed", err)
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
