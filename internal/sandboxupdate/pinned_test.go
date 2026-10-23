package sandboxupdate

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	versionlock "paseo-dock"
)

func TestRTKUsesOnlyPinnedArtifacts(t *testing.T) {
	for _, arch := range []string{"aarch64", "arm64", "x86_64"} {
		for _, corrupt := range []bool{false, true} {
			t.Run(arch+map[bool]string{true: "-corrupt", false: "-valid"}[corrupt], func(t *testing.T) {
				lock := versionlock.Load()
				var pin struct {
					Version string
					Assets  map[string]struct{ URL, SHA256 string }
				}
				if err := json.Unmarshal(lock.Utilities["rtk"], &pin); err != nil {
					t.Fatal(err)
				}
				key := arch
				if key == "arm64" {
					key = "aarch64"
				}
				asset := pin.Assets[key]
				f := &fixture{}
				run := func(ctx context.Context, args []string, capture bool) (string, error) {
					call := strings.Join(args, " ")
					if strings.Contains(call, "latest") || strings.Contains(call, "api.github.com") || strings.HasPrefix(call, "npm view") {
						t.Fatalf("floating lookup: %s", call)
					}
					switch {
					case call == "uname -m":
						return arch, nil
					case call == "rtk --version":
						return "rtk 0.0.0", nil
					case strings.HasPrefix(call, "sha256sum "):
						if corrupt {
							return strings.Repeat("0", 64) + "  archive", nil
						}
						return asset.SHA256 + "  archive", nil
					case strings.HasPrefix(call, "find "):
						return "/home/agent/.local/share/paseo-dock/updates/install.test/rtk", nil
					}
					return f.run(ctx, args, capture)
				}
				u := updater{ctx: context.Background(), run: run, lock: lock, home: "/home/agent", root: "/home/agent/.local/share/paseo-dock/updates"}
				_, err := u.rtk()
				if (err != nil) != corrupt {
					t.Fatal(err)
				}
				calls := strings.Join(f.calls, "\n")
				if !strings.Contains(calls, "-o /home/agent/.local/share/paseo-dock/updates/install.test/rtk.tar.gz "+asset.URL) {
					t.Fatal(calls)
				}
				if corrupt && (strings.Contains(calls, "tar -xzf") || strings.Contains(calls, "mv -Tf")) {
					t.Fatal("corrupt artifact extracted or activated", calls)
				}
				if !corrupt && !strings.Contains(calls, "mv -Tf") {
					t.Fatal("verified artifact not activated", calls)
				}
			})
		}
	}
}

func TestPluginRejectsUnpinnedFetchBeforeCheckout(t *testing.T) {
	var calls []string
	u := updater{ctx: context.Background(), lock: versionlock.Load(), run: func(_ context.Context, args []string, _ bool) (string, error) {
		call := strings.Join(args, " ")
		calls = append(calls, call)
		if strings.HasSuffix(call, "rev-parse HEAD") {
			return "old", nil
		}
		if strings.HasSuffix(call, "rev-parse FETCH_HEAD") {
			return "different", nil
		}
		return "", nil
	}}
	if _, err := u.plugin([]string{"codex"}, "superpowers"); err == nil {
		t.Fatal("unpinned plugin accepted")
	}
	all := strings.Join(calls, "\n")
	if !strings.Contains(all, "fetch --depth 1 origin 8ca22dba9a94f28898bbce59f2537ff4d87c747d") || strings.Contains(all, "checkout") || strings.Contains(all, "plugin remove") {
		t.Fatal(all)
	}
}

func TestNPMMethodologyFailsClosedWithoutChecksum(t *testing.T) {
	f := &fixture{}
	u := updater{ctx: context.Background(), run: f.run, lock: versionlock.Load()}
	if _, err := u.methodology([]string{"codex"}, "gsd-core", u.lock)(); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatal(err)
	}
	if len(f.calls) != 0 {
		t.Fatal(f.calls)
	}
}

func TestAntigravityUsesOnlyPinnedArtifacts(t *testing.T) {
	for _, arch := range []string{"aarch64", "x86_64"} {
		t.Run(arch, func(t *testing.T) {
			lock := versionlock.Load()
			var pin struct {
				Assets map[string]struct{ URL, SHA512 string }
			}
			if err := json.Unmarshal(lock.AgentArtifacts["antigravity-cli"], &pin); err != nil {
				t.Fatal(err)
			}
			asset := pin.Assets[arch]
			f := &fixture{}
			run := func(ctx context.Context, args []string, capture bool) (string, error) {
				call := strings.Join(args, " ")
				switch {
				case call == "uname -m":
					return arch, nil
				case call == "agy --version":
					return "0.0.0", nil
				case strings.HasPrefix(call, "curl "):
					if args[len(args)-1] != asset.URL {
						t.Fatalf("unpinned download or manifest lookup: %s", call)
					}
				case strings.HasPrefix(call, "sha512sum "):
					return asset.SHA512 + "  archive", nil
				}
				return f.run(ctx, args, capture)
			}
			u := updater{ctx: context.Background(), run: run, lock: lock, home: "/home/agent", root: "/home/agent/.local/share/paseo-dock/updates"}
			if _, err := u.antigravity(); err != nil {
				t.Fatal(err)
			}
			calls := strings.Join(f.calls, "\n")
			if !strings.Contains(calls, asset.URL) || !strings.Contains(calls, "mv -Tf") {
				t.Fatal("pinned artifact not installed", calls)
			}
		})
	}
}
