package launcher

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"paseo-sbx/internal/config"
	"paseo-sbx/internal/operationlock"
	"paseo-sbx/internal/operations"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if os.Getenv("LAUNCH_CATEGORY_HELPER") == "1" && (filepath.Base(os.Args[0]) == "docker" || filepath.Base(os.Args[0]) == "sbx") {
		f, _ := os.OpenFile(os.Getenv("LAUNCH_LOG"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		json.NewEncoder(f).Encode(os.Args[1:])
		f.Close()
		if nativeReviewBlockHelper() {
			os.Exit(0)
		}
		if os.Getenv("LAUNCH_TAR_FAIL") == "1" && strings.Contains(strings.Join(os.Args[1:], " "), "/usr/bin/tar") {
			os.Exit(17)
		}
		if os.Args[1] == "inspect" {
			fmt.Print(os.Getenv("LAUNCH_INSPECT"))
			os.Exit(0)
		}
		if root := os.Getenv("LAUNCH_ACTIVATION_FIXTURE"); root != "" && (strings.Contains(strings.Join(os.Args, " "), "--version") || os.Args[len(os.Args)-1] == "activation-check") {
			guestPath := "/opt/paseo-sbx/runtime/bin:/usr/bin:/bin"
			for i, arg := range os.Args {
				if arg == "--env" && i+1 < len(os.Args) && strings.HasPrefix(os.Args[i+1], "PATH=") {
					guestPath = strings.TrimPrefix(os.Args[i+1], "PATH=")
				}
			}
			for _, dir := range strings.Split(guestPath, ":") {
				var fixture string
				switch dir {
				case "/home/agent/.local/bin":
					fixture = "updated"
				case "/opt/paseo-sbx/runtime/bin":
					fixture = "image"
				default:
					continue
				}
				if data, err := os.ReadFile(filepath.Join(root, fixture, "codex")); err == nil {
					fmt.Print(string(data))
					os.Exit(0)
				}
			}
			os.Exit(127)
		}
		if strings.Contains(strings.Join(os.Args, " "), "mktemp") {
			fmt.Println("/tmp/paseo-sbx-assets.fixture")
			os.Exit(0)
		}
		if strings.Contains(strings.Join(os.Args, " "), "curl") {
			if os.Getenv("LAUNCH_RELAY_FAIL") == "1" {
				os.Exit(7)
			}
			res, err := http.Get(strings.Replace(os.Args[len(os.Args)-1], "host.docker.internal", "127.0.0.1", 1))
			if err != nil {
				os.Exit(8)
			}
			io.Copy(os.Stdout, res.Body)
			res.Body.Close()
			os.Exit(0)
		}
		if os.Getenv("LAUNCH_WAIT") == "1" && strings.Contains(strings.Join(os.Args, " "), "app-server") {
			ch := make(chan os.Signal, 1)
			signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
			if os.Getenv("LAUNCH_WORKER") != "1" {
				cmd := exec.Command(os.Args[0], os.Args[1:]...)
				cmd.Env = append(os.Environ(), "LAUNCH_WORKER=1")
				if cmd.Start() != nil {
					os.Exit(2)
				}
				os.WriteFile(os.Getenv("LAUNCH_READY"), nil, 0600)
				<-ch
				cmd.Wait()
				os.WriteFile(os.Getenv("LAUNCH_STOPPED"), nil, 0600)
			} else {
				os.WriteFile(os.Getenv("LAUNCH_WORKER_READY"), nil, 0600)
				<-ch
				os.WriteFile(os.Getenv("LAUNCH_WORKER_STOPPED"), nil, 0600)
			}
			os.Exit(0)
		}
		if strings.Contains(strings.Join(os.Args, " "), "--version") {
			fmt.Println(filepath.Base(os.Args[0]) + " agent version")
		} else {
			io.Copy(os.Stdout, os.Stdin)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestSandboxProviderSelectsActivatedAgentBeforeImage(t *testing.T) {
	runtime, id, entry, log := launcherCategoryFixture(t, config.CategorySandbox)
	root := t.TempDir()
	for _, dir := range []string{"updated", "image"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "updated", "codex"), []byte("updated-0.161.0"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "image", "codex"), []byte("image-0.160.1"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LAUNCH_ACTIVATION_FIXTURE", root)
	for _, want := range []string{"updated-0.161.0", "image-0.160.1"} {
		var direct strings.Builder
		backend := &operations.SBXBackend{}
		if err := backend.Exec(context.Background(), entry, []string{"codex", "--version"}, nil, &direct, io.Discard); err != nil || direct.String() != want {
			t.Fatalf("direct activated executable selection: output=%q err=%v want=%q", direct.String(), err, want)
		}
		for _, args := range [][]string{{"--version"}, {"activation-check"}} {
			var out strings.Builder
			code, err := LaunchIO(runtime, id, args, strings.NewReader(""), &out, io.Discard)
			if err != nil || code != 0 || out.String() != want {
				t.Fatalf("activated executable selection %v: output=%q code=%d err=%v want=%q", args, out.String(), code, err, want)
			}
		}
		if err := os.Remove(filepath.Join(root, "updated", "codex")); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), root) {
		t.Fatal("host PATH leaked into guest command")
	}
}
func launcherCategoryFixture(t *testing.T, category config.Category) (string, string, config.Entry, string) {
	t.Helper()
	runtime := t.TempDir()
	workspace, _ := config.Canonical(t.TempDir())
	bin := t.TempDir()
	exe, _ := os.Executable()
	for _, name := range []string{"sbx", "docker"} {
		if err := os.Symlink(exe, filepath.Join(bin, name)); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	t.Setenv("LAUNCH_CATEGORY_HELPER", "1")
	t.Setenv("PASEO_SBX_AUTO_UPDATE", "0")
	t.Setenv("PASEO_AGENT_CWD", workspace)
	log := filepath.Join(bin, "log")
	t.Setenv("LAUNCH_LOG", log)
	e := config.Entry{Category: category, Agent: "codex", Tool: "superpowers", Workspace: workspace, Sandbox: "box", Image: "managed"}
	id := "codex-superpowers"
	if category == config.CategorySandbox {
		e.Tool = ""
		id = "sbx-codex"
	}
	if category == config.CategoryContainer {
		e.Sandbox = ""
		e.ContainerID = strings.Repeat("a", 64)
		e.ImageID = "sha256:image"
		e.Network = "bridge"
		id = "container-codex-superpowers"
		raw := []any{map[string]any{"Id": e.ContainerID, "Image": e.ImageID, "State": map[string]any{"Running": true}, "Config": map[string]any{"Labels": map[string]string{"io.paseo-sbx.managed": "true", "io.paseo-sbx.category": "container"}}, "Mounts": []any{map[string]any{"Type": "bind", "Source": workspace, "Destination": workspace, "RW": true}}, "HostConfig": map[string]any{"NetworkMode": "bridge"}, "NetworkSettings": map[string]any{"Networks": map[string]any{"bridge": map[string]any{}}}}}
		b, _ := json.Marshal(raw)
		t.Setenv("LAUNCH_INSPECT", string(b))
	}
	key := e.Key()
	if category == "" {
		key = id
	}
	if err := config.WriteJSON(filepath.Join(runtime, "registry.json"), config.Registry{Workspace: workspace, Profiles: map[string]config.Entry{key: e}, Active: []string{key}}); err != nil {
		t.Fatal(err)
	}
	return runtime, id, e, log
}
func TestLauncherRoutesCategoryAndPreservesLegacyID(t *testing.T) {
	for _, category := range []config.Category{"", config.CategorySandbox, config.CategoryContainer} {
		t.Run(string(category), func(t *testing.T) {
			runtime, id, e, log := launcherCategoryFixture(t, category)
			var out strings.Builder
			code, err := LaunchIO(runtime, id, []string{"--version"}, strings.NewReader(""), &out, io.Discard)
			if code != 0 || err != nil {
				t.Fatalf("route %s: %d %v", id, code, err)
			}
			want := "sbx"
			if category == config.CategoryContainer {
				want = "docker"
			}
			if !strings.Contains(out.String(), want+" agent version") {
				t.Fatal(out.String())
			}
			calls, _ := os.ReadFile(log)
			if category == config.CategoryContainer && (!strings.Contains(string(calls), e.ContainerID) || strings.Contains(string(calls), `"box"`) || !strings.Contains(string(calls), `"inspect"`)) {
				t.Fatal(string(calls))
			}
		})
	}
}
func TestLauncherSignalStopsDockerExecGroup(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP} {
		t.Run(sig.String(), func(t *testing.T) {
			runtime, id, entry, _ := launcherCategoryFixture(t, config.CategoryContainer)
			t.Setenv("LAUNCH_WAIT", "1")
			for _, key := range []string{"LAUNCH_READY", "LAUNCH_STOPPED", "LAUNCH_WORKER_READY", "LAUNCH_WORKER_STOPPED"} {
				t.Setenv(key, filepath.Join(t.TempDir(), key))
			}
			done := make(chan error, 1)
			go func() {
				_, err := LaunchIO(runtime, id, []string{"app-server"}, strings.NewReader(""), io.Discard, io.Discard)
				done <- err
			}()
			wait := func(key string) {
				t.Helper()
				deadline := time.Now().Add(5 * time.Second)
				for time.Now().Before(deadline) {
					if _, err := os.Stat(os.Getenv(key)); err == nil {
						return
					}
					select {
					case err := <-done:
						t.Fatalf("launcher exited before ready: %v", err)
					default:
					}
					time.Sleep(10 * time.Millisecond)
				}
				t.Fatalf("missing %s", key)
			}
			wait("LAUNCH_READY")
			wait("LAUNCH_WORKER_READY")
			release, err := operationlock.Acquire(context.Background(), runtime, []string{operationlock.EntryKey(entry)})
			if err != nil {
				t.Fatalf("model session holds stop/update lock: %v", err)
			}
			release()
			syscall.Kill(os.Getpid(), sig)
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("Docker group survived signal")
			}
			for _, key := range []string{"LAUNCH_STOPPED", "LAUNCH_WORKER_STOPPED"} {
				if _, err := os.Stat(os.Getenv(key)); err != nil {
					t.Fatalf("signal did not stop %s", key)
				}
			}
		})
	}
}
func TestNativeAssetsUseInspectedDockerID(t *testing.T) {
	runtime, _, entry, log := launcherCategoryFixture(t, config.CategoryContainer)
	home := t.TempDir()
	t.Setenv("PASEO_HOME", home)
	asset := filepath.Join(home, "plugin-data", "antigravity-cli", "schema.json")
	os.MkdirAll(filepath.Dir(asset), 0700)
	os.WriteFile(asset, []byte(`{}`), 0600)
	args, _, cleanup, err := prepareNativeContext(context.Background(), runtime, entry.Key(), []string{"--json-schema", asset}, entry.Workspace, entry, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	cleanup()
	if args[1] == asset || !strings.HasPrefix(args[1], "/tmp/paseo-sbx-assets.") {
		t.Fatal(args)
	}
	calls, _ := os.ReadFile(log)
	if !strings.Contains(string(calls), `"/usr/bin/tar"`) || !strings.Contains(string(calls), entry.ContainerID) || strings.Contains(string(calls), `"cp"`) || strings.Contains(string(calls), `"root"`) || strings.Contains(string(calls), `"box"`) {
		t.Fatal(string(calls))
	}
	var lines [][]string
	for _, line := range strings.Split(strings.TrimSpace(string(calls)), "\n") {
		var args []string
		if err := json.Unmarshal([]byte(line), &args); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, args)
	}
	for i, args := range lines {
		if (args[0] == "exec" || args[0] == "cp") && (i == 0 || lines[i-1][0] != "inspect") {
			t.Fatalf("uninspected mutation: %v", lines)
		}
	}
}
func TestDockerLauncherRefusesUnreachableActualRelay(t *testing.T) {
	runtime, id, _, log := launcherCategoryFixture(t, config.CategoryContainer)
	t.Setenv("LAUNCH_RELAY_FAIL", "1")
	_, err := LaunchIO(runtime, id, []string{"app-server"}, strings.NewReader(""), io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "cannot reach local Paseo relay") {
		t.Fatal(err)
	}
	calls, _ := os.ReadFile(log)
	if strings.Contains(string(calls), `"app-server"`) {
		t.Fatal("agent spawned before relay check", string(calls))
	}
}
func TestDockerLauncherProtocolKeepsHostBearerInRelay(t *testing.T) {
	runtime, id, e, log := launcherCategoryFixture(t, config.CategoryContainer)
	var out strings.Builder
	message := `{"method":"thread/start","params":{"cwd":"` + e.Workspace + `","config":{"mcp_servers":{"paseo":{"url":"http://127.0.0.1:6767/mcp/agents?callerAgentId=parent","http_headers":{"Authorization":"Bearer HOST_SECRET"}}}}}}` + "\n"
	code, err := LaunchIO(runtime, id, []string{"app-server"}, strings.NewReader(message), &out, io.Discard)
	if err != nil || code != 0 || strings.Contains(out.String(), "HOST_SECRET") || !strings.Contains(out.String(), "host.docker.internal") || !strings.Contains(out.String(), e.Workspace) {
		t.Fatalf("%d %v %s", code, err, out.String())
	}
	calls, _ := os.ReadFile(log)
	if strings.Contains(string(calls), "HOST_SECRET") || strings.Contains(string(calls), `"policy"`) || !strings.Contains(string(calls), `"--workdir","`+e.Workspace+`"`) {
		t.Fatal(string(calls))
	}
}
