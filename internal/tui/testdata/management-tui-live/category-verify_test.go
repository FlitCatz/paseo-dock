package main

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"paseo-sbx/internal/config"
)

func TestMain(m *testing.M) {
	if os.Getenv("CATEGORY_GUARD_TEST_CHILD") == "1" {
		if filepath.Base(os.Args[0]) == "fixture-real" {
			if os.Args[len(os.Args)-1] == "app-server" {
				var request [4096]byte
				os.Stdin.Read(request[:])
				response := os.Getenv("CATEGORY_TEST_RESPONSE")
				if response != "" {
					fmt.Fprintln(os.Stdout, response)
					io.Copy(io.Discard, os.Stdin)
				}
				os.Exit(0)
			}
			os.WriteFile(os.Getenv("CATEGORY_TEST_CALLED"), []byte(strings.Join(os.Args[1:], "\n")), 0600)
			os.Exit(0)
		}
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestProviderSessionRequiresSuccessfulInitializeProcess(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.Mkdir(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(root, "home")
	t.Setenv("HOME", home)
	runtime, err := config.RuntimePath(workspace)
	if err != nil {
		t.Fatal(err)
	}
	reg := config.Registry{Schema: 2, Workspace: workspace, Active: []string{"sandbox/codex"}, Profiles: map[string]config.Entry{"sandbox/codex": {Category: config.CategorySandbox, Agent: "codex", Workspace: workspace, Sandbox: "owned-sbx", Image: "fixture"}}}
	if err := config.WriteJSON(filepath.Join(runtime, "registry.json"), reg); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"sbx", "fixture-real"} {
		if err := os.Symlink(exe, filepath.Join(bin, name)); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name, response string
		success        bool
	}{
		{"error", `{"id":1,"error":{"code":-32602,"message":"invalid initialize"}}`, false},
		{"absent", "", false},
		{"valid", `{"id":1,"result":{"platformOs":"linux"}}`, true},
		{"whitespace", `{ "id" : 1, "result" : {} }`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Three nested race-instrumented subprocesses each have a default
			// one-second exit delay; allow that chain without changing GORACE.
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, exe, "--provider-session", runtime, "sbx-codex")
			cmd.Dir = workspace
			cmd.Env = environment(map[string]string{"CATEGORY_GUARD_TEST_CHILD": "1", "CATEGORY_LIVE_ROOT": root, "HOME": home, "PATH": bin, "CATEGORY_AUTH_HOME": home, "CATEGORY_AUTH_PATH": bin, "CATEGORY_REAL_SBX": filepath.Join(bin, "fixture-real"), "CATEGORY_TEST_CALLED": filepath.Join(root, "called"), "CATEGORY_TEST_RESPONSE": tc.response, "PASEO_SBX_AUTO_UPDATE": "0", "PASEO_AGENT_CWD": workspace})
			output, err := cmd.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatalf("initialize gate did not terminate: %s", output)
			}
			if (err == nil) != tc.success {
				t.Fatalf("initialize success=%t want=%t output=%s", err == nil, tc.success, output)
			}
		})
	}
}

func TestProxyRejectsForeignMutationTargets(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.Mkdir(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(root, "home")
	t.Setenv("HOME", home)
	runtime, err := config.RuntimePath(workspace)
	if err != nil {
		t.Fatal(err)
	}
	cid := strings.Repeat("a", 64)
	foreign := strings.Repeat("b", 64)
	reg := config.Registry{Schema: 2, Workspace: workspace, Profiles: map[string]config.Entry{
		"sandbox/codex":               {Category: config.CategorySandbox, Agent: "codex", Workspace: workspace, Sandbox: "owned-sbx"},
		"container/codex-superpowers": {Category: config.CategoryContainer, Agent: "codex", Tool: "superpowers", Workspace: workspace, ContainerID: cid},
	}}
	if err := config.WriteJSON(filepath.Join(runtime, "registry.json"), reg); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"docker", "sbx", "fixture-real"} {
		if err := os.Symlink(exe, filepath.Join(bin, name)); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name    string
		args    []string
		allowed bool
	}{
		{"docker", []string{"exec", foreign, "echo", cid}, false},
		{"docker", []string{"exec", "--env", "PATH=" + cid, foreign, "echo", cid}, false},
		{"docker", []string{"stop", cid, foreign}, false},
		{"docker", []string{"start", cid, foreign}, false},
		{"docker", []string{"exec", "--unknown", cid, "true"}, false},
		{"docker", []string{"exec", "--env", cid}, false},
		{"docker", []string{"exec", "owned-sbx", "true"}, false},
		{"sbx", []string{"exec", "foreign-sbx", "echo", "owned-sbx"}, false},
		{"sbx", []string{"stop", "owned-sbx", "foreign-sbx"}, false},
		{"sbx", []string{"policy", "allow", "network", "--sandbox", "foreign-sbx", "owned-sbx"}, false},
		{"sbx", []string{"exec", cid, "true"}, false},
		{"docker", []string{"exec", "-i", "--user", "501:20", "--env", "HOME=/home/agent", "--workdir", workspace, cid, "echo", foreign}, true},
		{"docker", []string{"stop", cid}, true},
		{"sbx", []string{"exec", "--env", "PATH=/usr/bin", "--workdir", workspace, "owned-sbx", "echo", "foreign-sbx"}, true},
		{"sbx", []string{"policy", "allow", "network", "--sandbox", "owned-sbx", "localhost:1234"}, true},
		{"sbx", []string{"policy", "rm", "network", "--sandbox", "owned-sbx", "--resource", "localhost:1234", "--force"}, true},
	} {
		t.Run(strings.Join(append([]string{tc.name}, tc.args...), " "), func(t *testing.T) {
			marker := filepath.Join(root, "called")
			os.Remove(marker)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, filepath.Join(bin, tc.name), tc.args...)
			cmd.Env = environment(map[string]string{"CATEGORY_GUARD_TEST_CHILD": "1", "CATEGORY_LIVE_ROOT": root, "HOME": home, "CATEGORY_AUTH_HOME": home, "CATEGORY_AUTH_PATH": bin, "CATEGORY_REAL_DOCKER": filepath.Join(bin, "fixture-real"), "CATEGORY_REAL_SBX": filepath.Join(bin, "fixture-real"), "CATEGORY_TEST_CALLED": marker})
			out, err := cmd.CombinedOutput()
			_, called := os.Stat(marker)
			if tc.allowed {
				if err != nil || called != nil {
					t.Fatalf("owned call rejected: %v %s", err, out)
				}
			} else if err == nil || !os.IsNotExist(called) || !bytes.Contains(out, []byte("isolation guard rejected")) {
				t.Fatalf("foreign/malformed mutation reached external executable: err=%v marker=%v output=%s", err, called, out)
			}
		})
	}
}

func TestInitializeOutputRequiresCompleteSuccessfulResponse(t *testing.T) {
	for _, tc := range []struct {
		name    string
		chunks  []string
		success bool
	}{
		{"result", []string{"{\"id\":1,\"result\":{\"platformOs\":\"linux\"}}\n"}, true},
		{"whitespace", []string{"{ \"id\" : 1, \"result\" : {} }\n"}, true},
		{"split", []string{"{\"id\":1,", "\"result\":{}}", "\n"}, true},
		{"error", []string{"{\"id\":1,\"error\":{\"code\":-32602}}\n"}, false},
		{"null", []string{"{\"id\":1,\"result\":null}\n"}, false},
		{"both", []string{"{\"id\":1,\"result\":{},\"error\":{}}\n"}, false},
		{"nested", []string{"{\"method\":\"note\",\"params\":{\"id\":1,\"result\":{}}}\n"}, false},
		{"partial", []string{"{\"id\":1,\"result\":{}"}, false},
		{"string-id", []string{"{\"id\":\"1\",\"result\":{}}\n"}, false},
		{"other-id", []string{"{\"id\":10,\"result\":{}}\n"}, false},
		{"echo", []string{"{\"id\":1,\"method\":\"initialize\",\"params\":{}}\n"}, false},
		{"missing", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader, writer := io.Pipe()
			defer reader.Close()
			defer writer.Close()
			o := &initializeOutput{writer: io.Discard, stdin: writer}
			closed := make(chan struct{})
			go func() { io.Copy(io.Discard, reader); close(closed) }()
			for i, chunk := range tc.chunks {
				o.Write([]byte(chunk))
				if i < len(tc.chunks)-1 {
					select {
					case <-closed:
						t.Fatal("closed before complete frame")
					default:
					}
				}
			}
			if (o.resultError() == nil) != tc.success {
				t.Fatalf("structured initialize result success=%t want=%t", o.resultError() == nil, tc.success)
			}
			// Success closes stdin; all other transcripts must remain distinguishable
			// from success. An explicit matching error may close to terminate safely.
			if tc.success {
				select {
				case <-closed:
				case <-time.After(time.Second):
					t.Fatal("valid initialize did not close stdin")
				}
			} else {
				select {
				case <-closed:
					if tc.name != "error" && tc.name != "both" {
						t.Fatal("invalid initialize closed stdin as success")
					}
				default:
				}
			}
		})
	}
}

// Only the two actual initialize response frames from the historical passing
// run are retained as a regression fixture; tests do not depend on docs logs.
//
//go:embed provider-initialize.jsonl
var retainedProviderInitialize []byte

func TestRetainedProviderInitializeResults(t *testing.T) {
	count := 0
	for _, line := range strings.Split(string(retainedProviderInitialize), "\n") {
		if !strings.HasPrefix(line, "PROTOCOL ") {
			continue
		}
		_, frame, ok := strings.Cut(strings.TrimPrefix(line, "PROTOCOL "), " ")
		if !ok {
			t.Fatal("retained protocol line has no frame")
		}
		output := &initializeOutput{writer: io.Discard}
		output.Write([]byte(frame + "\n"))
		if err := output.resultError(); err != nil {
			t.Fatal(err)
		}
		count++
	}
	if count != 2 {
		t.Fatalf("retained provider results=%d want=2", count)
	}
}
