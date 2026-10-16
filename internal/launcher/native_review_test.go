package launcher

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"paseo-dock/internal/config"
	"paseo-dock/internal/operationlock"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func nativeReviewBlockHelper() bool {
	stage := os.Getenv("NATIVE_REVIEW_BLOCK")
	if stage == "" || (stage == "cp" && os.Args[1] != "cp") || (stage != "cp" && !strings.Contains(strings.Join(os.Args[1:], " "), stage)) {
		return false
	}
	role := "parent"
	if os.Getenv("NATIVE_REVIEW_WORKER") == "1" {
		role = "worker"
	}
	f, err := os.OpenFile(os.Getenv("NATIVE_REVIEW_READY")+"."+role, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		os.Exit(3)
	}
	if syscall.Flock(int(f.Fd()), syscall.LOCK_EX) != nil {
		os.Exit(4)
	}
	f.WriteString(strconv.Itoa(os.Getpid()))
	if role == "parent" {
		cmd := exec.Command(os.Args[0], os.Args[1:]...)
		cmd.Env = append(os.Environ(), "NATIVE_REVIEW_WORKER=1")
		if cmd.Start() != nil {
			os.Exit(5)
		}
		cmd.Wait()
	} else {
		for {
			time.Sleep(time.Hour)
		}
	}
	return true
}

func nativeReviewForceStop(ready string) {
	for _, role := range []string{"parent", "worker"} {
		b, _ := os.ReadFile(ready + "." + role)
		pid, _ := strconv.Atoi(string(b))
		if pid > 0 {
			syscall.Kill(pid, syscall.SIGKILL)
		}
	}
}

func nativeReviewWaitForExit(t *testing.T, ready string, deadline time.Time) {
	t.Helper()
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	for _, role := range []string{"parent", "worker"} {
		f, err := os.OpenFile(ready+"."+role, os.O_RDWR, 0600)
		if err != nil {
			t.Fatalf("native %s did not become ready: %v", role, err)
		}
		// Cmd.Wait reaps the direct child, but a descendant killed by the same
		// group signal can still be releasing its files. Wait for the actual
		// lock release, within the caller's original cancellation deadline.
		done := make(chan error, 1)
		go func() {
			err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
			f.Close()
			done <- err
		}()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("wait for native %s readiness lock: %v", role, err)
			}
		case <-timer.C:
			// The caller's deferred force-stop releases the lock if a regression
			// leaves the helper alive; the waiter owns and closes its file.
			t.Fatalf("native %s still holds readiness lock at cancellation deadline", role)
		}
	}
}

func TestNativePreparationSignalKillsSubprocessGroup(t *testing.T) {
	for _, stage := range []string{"mktemp", "tar"} {
		for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP} {
			t.Run(stage+"/"+sig.String(), func(t *testing.T) {
				runtime, id, entry, log, asset := nativeReviewFixture(t)
				ready := filepath.Join(t.TempDir(), "ready")
				defer nativeReviewForceStop(ready)
				t.Setenv("NATIVE_REVIEW_BLOCK", stage)
				t.Setenv("NATIVE_REVIEW_READY", ready)
				done := make(chan error, 1)
				go func() {
					_, err := LaunchIO(runtime, id, []string{"--acp", "--json-schema", asset}, strings.NewReader(""), io.Discard, io.Discard)
					done <- err
				}()
				deadline := time.Now().Add(10 * time.Second)
				for _, role := range []string{"parent", "worker"} {
					for {
						if b, _ := os.ReadFile(ready + "." + role); len(b) > 0 {
							break
						}
						select {
						case err := <-done:
							t.Fatalf("launcher ended before blocked native command readiness: %v", err)
						default:
						}
						if time.Now().After(deadline) {
							t.Fatal("native command never became ready")
						}
						time.Sleep(10 * time.Millisecond)
					}
				}
				if release, err := operationlock.Acquire(context.Background(), runtime, []string{operationlock.EntryKey(entry)}); err == nil {
					release()
					t.Fatal("preparation subprocess has no shared target lock")
				}
				deadline = time.Now().Add(1500 * time.Millisecond)
				syscall.Kill(os.Getpid(), sig)
				select {
				case err := <-done:
					if err == nil {
						t.Fatal("canceled native launch reported success")
					}
				case <-time.After(time.Until(deadline)):
					nativeReviewForceStop(ready)
					<-done
					t.Fatal("native preparation ignored launcher cancellation")
				}
				nativeReviewWaitForExit(t, ready, deadline)
				calls, _ := os.ReadFile(log)
				if strings.Contains(string(calls), `"agy"`) {
					t.Fatal("native model started after canceled preparation")
				}
			})
		}
	}
}

func nativeReviewFixture(t *testing.T) (string, string, config.Entry, string, string) {
	t.Helper()
	// The external CLI fixtures use this race-instrumented test binary. Disable
	// its artificial exit sleep so subprocess deadlines measure cancellation.
	t.Setenv("GORACE", strings.TrimSpace(os.Getenv("GORACE")+" atexit_sleep_ms=0"))
	runtime, _, entry, log := launcherCategoryFixture(t, config.CategoryContainer)
	entry.Agent = "antigravity-cli"
	entry.Tool = "native"
	reg := config.Registry{Workspace: entry.Workspace, Profiles: map[string]config.Entry{entry.Key(): entry}, Active: []string{entry.Key()}}
	if err := config.WriteJSON(filepath.Join(runtime, "registry.json"), reg); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	t.Setenv("PASEO_HOME", home)
	asset := filepath.Join(home, "plugin-data", "antigravity-cli", "schema.json")
	os.MkdirAll(filepath.Dir(asset), 0700)
	os.WriteFile(asset, []byte(`{}`), 0600)
	return runtime, "container-antigravity-cli-native", entry, log, asset
}

func TestNativeLauncherACPRelaysBeforeGuestWrite(t *testing.T) {
	for _, assets := range []bool{false, true} {
		t.Run(map[bool]string{false: "without-assets", true: "with-assets"}[assets], func(t *testing.T) {
			runtime, id, e, log, asset := nativeReviewFixture(t)
			args := []string{"--acp"}
			if assets {
				args = append(args, "--json-schema", asset)
			}
			message := map[string]any{"method": "session/new", "params": map[string]any{"cwd": e.Workspace, "mcpServers": []any{map[string]any{"type": "http", "name": "paseo", "url": "http://localhost:6767/mcp/agents?callerAgentId=parent", "headers": []any{map[string]any{"name": "Authorization", "value": "Bearer TEST_HOST_BEARER"}}}}, "schema": asset}}
			b, _ := json.Marshal(message)
			var out strings.Builder
			code, err := LaunchIO(runtime, id, args, strings.NewReader(string(b)+"\n"), &out, io.Discard)
			if err != nil || code != 0 {
				t.Fatalf("native launch failed: code=%d err=%v", code, err)
			}
			if strings.Contains(out.String(), "TEST_HOST_BEARER") {
				t.Fatal("host bearer reached native guest")
			}
			if !strings.Contains(out.String(), "host.docker.internal") || !strings.Contains(out.String(), "Bearer ") {
				t.Fatal("relay capability absent from ACP")
			}
			if assets && strings.Contains(out.String(), asset) {
				t.Fatal("native asset path not rewritten")
			}
			calls, _ := os.ReadFile(log)
			if strings.Contains(string(calls), "TEST_HOST_BEARER") {
				t.Fatal("host bearer reached guest argv")
			}
		})
	}
}
func TestNativeLauncherRejectsFramesBeforeGuestWrite(t *testing.T) {
	for _, frame := range []string{"malformed", "outside-cwd", "remote-mcp", "invalid-header"} {
		t.Run(frame, func(t *testing.T) {
			runtime, id, e, _, _ := nativeReviewFixture(t)
			message := `{"method":"session/new","params":{"cwd":"` + e.Workspace + `","mcpServers":[{"url":"http://localhost:6767/mcp/agents?callerAgentId=parent","headers":[{"name":"Authorization","value":"Bearer TEST_HOST_BEARER"}]}]}}`
			switch frame {
			case "malformed":
				message = `{"method":`
			case "outside-cwd":
				message = strings.Replace(message, e.Workspace, t.TempDir(), 1)
			case "remote-mcp":
				message = strings.Replace(message, "localhost", "example.com", 1)
			case "invalid-header":
				message = strings.Replace(message, `"value":"Bearer TEST_HOST_BEARER"`, `"value":42`, 1)
			}
			var out strings.Builder
			code, err := LaunchIO(runtime, id, []string{"--acp"}, strings.NewReader(message+"\n"), &out, io.Discard)
			if code == 0 || err == nil {
				t.Fatal("invalid native frame accepted")
			}
			if out.Len() != 0 {
				t.Fatal("invalid native frame forwarded to guest")
			}
		})
	}
}
func TestNativePreparationRejectsConflictAndStaleRegistry(t *testing.T) {
	for _, mode := range []string{"conflict", "stale"} {
		t.Run(mode, func(t *testing.T) {
			runtime, _, e, log, asset := nativeReviewFixture(t)
			if mode == "conflict" {
				release, err := operationlock.Acquire(context.Background(), runtime, []string{operationlock.EntryKey(e)})
				if err != nil {
					t.Fatal(err)
				}
				defer release()
			} else {
				var reg config.Registry
				config.ReadJSON(filepath.Join(runtime, "registry.json"), &reg)
				changed := e
				changed.Image = "changed"
				reg.Profiles[e.Key()] = changed
				config.WriteJSON(filepath.Join(runtime, "registry.json"), reg)
			}
			_, _, _, err := prepareNativeContext(context.Background(), runtime, e.Key(), []string{"--json-schema", asset}, e.Workspace, e, io.Discard)
			if err == nil {
				t.Fatalf("native preparation ignored %s", mode)
			}
			calls, _ := os.ReadFile(log)
			if len(calls) != 0 {
				t.Fatal("native mutation occurred before frozen-target validation")
			}
		})
	}
}
func TestNativeCleanupRejectsConflictAndStaleRegistry(t *testing.T) {
	for _, mode := range []string{"conflict", "stale"} {
		t.Run(mode, func(t *testing.T) {
			runtime, _, e, log, asset := nativeReviewFixture(t)
			_, _, cleanup, err := prepareNativeContext(context.Background(), runtime, e.Key(), []string{"--json-schema", asset}, e.Workspace, e, io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			os.WriteFile(log, nil, 0600)
			if mode == "conflict" {
				release, err := operationlock.Acquire(context.Background(), runtime, []string{operationlock.EntryKey(e)})
				if err != nil {
					t.Fatal(err)
				}
				defer release()
			} else {
				var reg config.Registry
				config.ReadJSON(filepath.Join(runtime, "registry.json"), &reg)
				reg.Active = []string{}
				config.WriteJSON(filepath.Join(runtime, "registry.json"), reg)
			}
			cleanup()
			calls, _ := os.ReadFile(log)
			if len(calls) != 0 {
				t.Fatal("native cleanup mutated conflicted or stale target")
			}
		})
	}
}
func TestNativeCleanupHasBoundedFreshContext(t *testing.T) {
	runtime, _, entry, _, asset := nativeReviewFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	_, _, cleanup, err := prepareNativeContext(ctx, runtime, entry.Key(), []string{"--json-schema", asset}, entry.Workspace, entry, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	ready := filepath.Join(t.TempDir(), "ready")
	defer nativeReviewForceStop(ready)
	t.Setenv("NATIVE_REVIEW_BLOCK", "rm")
	t.Setenv("NATIVE_REVIEW_READY", ready)
	started := time.Now()
	cleanup()
	if elapsed := time.Since(started); elapsed > 3500*time.Millisecond {
		t.Fatal("cleanup exceeded its fresh bounded context")
	}
	nativeReviewWaitForExit(t, ready, started.Add(3500*time.Millisecond))
}
