package sandboxupdate

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"paseo-sbx/internal/config"
)

type fixture struct {
	calls   []string
	fail    string
	latest  string
	current string
}

// A child timeout must survive failed persistence even though the caller remains live.
func TestInternalDeadlineAndRecordWriteFailure(t *testing.T) {
	parent := context.Background()
	child, cancel := context.WithDeadline(parent, time.Now().Add(-time.Second))
	defer cancel()
	base := filepath.Join(t.TempDir(), "box")
	if err := os.Mkdir(base+".json", 0700); err != nil {
		t.Fatal(err)
	}
	state := State{Sandbox: "box", Attempt: time.Now(), Errors: map[string]string{"agent": context.DeadlineExceeded.Error()}}
	err := finishUpdate(child, base, state, io.Discard)
	if parent.Err() != nil {
		t.Fatal("test accidentally canceled caller")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("internal deadline hidden by record-write failure: %v", err)
	}
	var persistence *os.LinkError
	if !errors.As(err, &persistence) {
		t.Errorf("record-write failure lost: %v", err)
	}
}

func (f *fixture) run(_ context.Context, args []string, _ bool) (string, error) {
	s := strings.Join(args, " ")
	f.calls = append(f.calls, s)
	if f.fail != "" && strings.Contains(s, f.fail) {
		return "", errors.New("installation failed")
	}
	switch {
	case s == "printenv HOME":
		return "/home/agent", nil
	case strings.HasPrefix(s, "cat ") && strings.Contains(s, "utilities/installed.json"):
		return "", os.ErrNotExist
	case strings.HasPrefix(s, "cat "):
		return `{"lock":{"tools":{"openspec":{"package":"@fission-ai/openspec"}}}}`, nil
	case strings.HasPrefix(s, "npm view "):
		if f.latest != "" {
			return f.latest, nil
		}
		return "9.0.0", nil
	case s == "codex --version" || s == "claude --version" || s == "openspec --version":
		return f.current, nil
	case strings.HasPrefix(s, "mktemp -d "):
		return "/home/agent/.local/share/paseo-sbx/updates/install.test", nil
	case strings.HasPrefix(s, "mktemp -u "):
		return strings.TrimSuffix(args[2], "XXXXXXXXXX") + "test", nil
	case strings.HasSuffix(s, "--version"):
		return "9.0.0", nil
	default:
		return "", nil
	}
}

func TestNPMInstallIsVerifiedBeforeSwitch(t *testing.T) {
	for _, failure := range []string{"", "npm install", "/bin/codex --version"} {
		t.Run(failure, func(t *testing.T) {
			f := &fixture{fail: failure, current: "codex-cli 1.0.0"}
			u := updater{ctx: context.Background(), run: f.run, home: "/home/agent", root: "/home/agent/.local/share/paseo-sbx/updates"}
			_, err := u.npm("@openai/codex", "codex")
			if (err != nil) != (failure != "") {
				t.Fatal(err)
			}
			calls := strings.Join(f.calls, "\n")
			if failure != "" && strings.Contains(calls, "mv -Tf") {
				t.Fatal("failed installation activated", calls)
			}
			if failure == "" {
				install := strings.Index(calls, "npm install")
				verify := strings.Index(calls, "install.test/bin/codex --version")
				activate := strings.Index(calls, "mv -Tf")
				if install < 0 || verify < install || activate < verify || !strings.Contains(calls, "@openai/codex@9.0.0") {
					t.Fatal(calls)
				}
			}
		})
	}
}

func TestCurrentNPMVersionDoesNotReinstall(t *testing.T) {
	f := &fixture{latest: "9.0.0", current: "codex-cli 9.0.0"}
	u := updater{ctx: context.Background(), run: f.run}
	if _, err := u.npm("@openai/codex", "codex"); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 2 {
		t.Fatal(f.calls)
	}
}

func TestComponentFailureDoesNotPreventOtherUpdates(t *testing.T) {
	f := &fixture{fail: "@openai/codex@9.0.0"}
	s := refresh(context.Background(), config.Entry{Agent: "codex", Tool: "openspec", Sandbox: "box"}, f.run, time.Now())
	if s.Errors["agent"] == "" || s.Versions["methodology"] != "9.0.0" || !s.Success.IsZero() {
		t.Fatal(s)
	}
}

func TestAutomaticIntervalAndConcurrentLock(t *testing.T) {
	runtime := t.TempDir()
	entry := config.Entry{Agent: "codex", Sandbox: "box"}
	sum := sha256.Sum256([]byte(entry.Sandbox))
	base := filepath.Join(runtime, "updates", fmt.Sprintf("%x", sum[:8]))
	if err := config.WriteJSON(base+".json", State{Sandbox: entry.Sandbox, Attempt: time.Now(), Success: time.Now()}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir()) // Any accidental subprocess must fail.
	if err := Update(runtime, entry, false, io.Discard); err != nil {
		t.Fatal("daily interval ignored", err)
	}
	f, err := os.OpenFile(base+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	if err := Update(runtime, entry, false, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := Update(runtime, entry, true, io.Discard); err == nil {
		t.Fatal("forced concurrent update accepted")
	}
}

func TestDisableOnlySkipsAutomaticUpdate(t *testing.T) {
	t.Setenv("PASEO_SBX_AUTO_UPDATE", "0")
	t.Setenv("PATH", t.TempDir())
	runtime := t.TempDir()
	entry := config.Entry{Agent: "codex", Sandbox: "box"}
	if err := Update(runtime, entry, false, io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(runtime, "updates")); !os.IsNotExist(err) {
		t.Fatal("disabled update changed state")
	}
	if err := Update(runtime, entry, true, io.Discard); err == nil {
		t.Fatal("force incorrectly skipped update")
	}
}

func TestPluginRollbackOnInstallFailure(t *testing.T) {
	var calls []string
	run := func(_ context.Context, args []string, _ bool) (string, error) {
		s := strings.Join(args, " ")
		calls = append(calls, s)
		if strings.HasSuffix(s, "rev-parse HEAD") {
			return "old", nil
		}
		if strings.HasSuffix(s, "rev-parse FETCH_HEAD") {
			return "new", nil
		}
		if strings.HasPrefix(s, "claude plugin update") {
			return "", errors.New("plugin install failed")
		}
		return "", nil
	}
	u := updater{ctx: context.Background(), run: run, home: "/home/agent"}
	if _, err := u.plugin([]string{"claude"}, "superpowers"); err == nil {
		t.Fatal("failure ignored")
	}
	if !strings.HasSuffix(calls[len(calls)-1], "checkout --detach old") {
		t.Fatal(calls)
	}
}

func TestRTKRejectsUnverifiedRelease(t *testing.T) {
	var calls []string
	run := func(_ context.Context, args []string, _ bool) (string, error) {
		s := strings.Join(args, " ")
		calls = append(calls, s)
		if s == "uname -m" {
			return "aarch64", nil
		}
		return `{"assets":[{"name":"rtk-aarch64-unknown-linux-gnu.tar.gz","browser_download_url":"https://github.com/rtk-ai/rtk/releases/download/v1/rtk.tar.gz"}]}`, nil
	}
	u := updater{ctx: context.Background(), run: run}
	if _, err := u.rtk(); err == nil {
		t.Fatal("missing digest accepted")
	}
	if len(calls) != 2 {
		t.Fatal("unverified release downloaded", calls)
	}
}

func TestAntigravityChecksumFailureKeepsInstalledAgent(t *testing.T) {
	var calls []string
	digest := strings.Repeat("a", 128)
	run := func(_ context.Context, args []string, _ bool) (string, error) {
		s := strings.Join(args, " ")
		calls = append(calls, s)
		switch {
		case s == "uname -m":
			return "aarch64", nil
		case s == "agy --version":
			return "1.0.0", nil
		case strings.HasPrefix(s, "curl ") && strings.Contains(s, "/manifests/"):
			return `{"version":"2.0.0","url":"https://storage.googleapis.com/antigravity-public/antigravity-cli/2/linux-arm/cli_linux_arm64.tar.gz","sha512":"` + digest + `"}`, nil
		case strings.HasPrefix(s, "mktemp -d "):
			return "/home/agent/.local/share/paseo-sbx/updates/install.test", nil
		case strings.HasPrefix(s, "sha512sum "):
			return strings.Repeat("b", 128) + "  archive", nil
		default:
			return "", nil
		}
	}
	u := updater{ctx: context.Background(), run: run, home: "/home/agent", root: "/home/agent/.local/share/paseo-sbx/updates"}
	if _, err := u.antigravity(); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatal(err)
	}
	for _, call := range calls {
		if strings.HasPrefix(call, "tar ") || strings.HasPrefix(call, "mv ") {
			t.Fatal("bad archive extracted or activated", calls)
		}
	}
}

func TestHeadroomDependencyComparison(t *testing.T) {
	a := `[{"name":"headroom-ai","version":"1"},{"name":"mcp","version":"2"}]`
	b := `[{"name":"mcp","version":"2"},{"name":"headroom-ai","version":"1"}]`
	if !samePackages(a, b) {
		t.Fatal("identical versions considered changed")
	}
	if samePackages(a, strings.Replace(b, `"version":"2"`, `"version":"3"`, 1)) {
		t.Fatal("dependency update ignored")
	}
	if samePackages(a, "invalid") {
		t.Fatal("invalid package metadata accepted")
	}
}
