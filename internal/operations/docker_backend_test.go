package operations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"paseo-dock/internal/config"
)

// The fake CLI is the Go test binary, keeping the real process boundary.
func init() {
	if os.Getenv("DOCKER_BACKEND_FAKE") != "1" || filepath.Base(os.Args[0]) != "docker" {
		return
	}
	f, _ := os.OpenFile(os.Getenv("DOCKER_BACKEND_LOG"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	_ = json.NewEncoder(f).Encode(os.Args[1:])
	_ = f.Close()
	if os.Getenv("DOCKER_BACKEND_FAIL") == "1" {
		fmt.Fprintln(os.Stderr, "Cannot connect to Docker daemon")
		os.Exit(1)
	}
	if os.Args[1] == "exec" && os.Getenv("DOCKER_BACKEND_EXEC_FAIL") == "1" {
		if signal := os.Getenv("DOCKER_BACKEND_READY_SIGNAL"); signal != "" {
			_ = os.WriteFile(signal, nil, 0600)
			for {
				time.Sleep(time.Second)
			}
		}
		fmt.Fprintln(os.Stderr, "guest home is not writable")
		os.Exit(1)
	}
	switch os.Args[1] {
	case "inspect":
		if message := os.Getenv("DOCKER_BACKEND_INSPECT_ERROR"); message != "" {
			fmt.Fprintln(os.Stderr, message)
			os.Exit(1)
		}
		b, _ := os.ReadFile(os.Getenv("DOCKER_BACKEND_INSPECT"))
		if os.Getenv("DOCKER_BACKEND_CHANGE_THIRD_INSPECT") == "1" {
			log, _ := os.ReadFile(os.Getenv("DOCKER_BACKEND_LOG"))
			if strings.Count(string(log), `"inspect"`) >= 3 {
				b = []byte(strings.ReplaceAll(string(b), strings.Repeat("a", 64), strings.Repeat("b", 64)))
			}
		}
		fmt.Print(string(b))
	case "image":
		arch := os.Getenv("DOCKER_BACKEND_ARCH")
		if arch == "" {
			arch = "arm64"
		}
		fmt.Printf(`[{"Id":"sha256:image","Architecture":%q}]`, arch)
	case "run":
		for i, arg := range os.Args {
			if arg == "--security-opt" && i+1 < len(os.Args) && strings.HasPrefix(os.Args[i+1], "seccomp=") {
				policy, err := os.ReadFile(strings.TrimPrefix(os.Args[i+1], "seccomp="))
				if err != nil {
					fmt.Fprintln(os.Stderr, err)
					os.Exit(1)
				}
				if target := os.Getenv("DOCKER_BACKEND_POLICY"); target != "" {
					_ = os.WriteFile(target, policy, 0600)
				}
			}
			if arg == "--cidfile" && i+1 < len(os.Args) {
				_ = os.WriteFile(os.Args[i+1], []byte(strings.Repeat("a", 64)), 0600)
			}
		}
		if signal := os.Getenv("DOCKER_BACKEND_RUN_SIGNAL"); signal != "" {
			_ = os.WriteFile(signal, nil, 0600)
			for {
				time.Sleep(time.Second)
			}
		}
		if os.Getenv("DOCKER_BACKEND_RUN_FAIL") == "1" {
			fmt.Fprintln(os.Stderr, "port already allocated after creation")
			os.Exit(1)
		}
		fmt.Println(strings.Repeat("a", 64))
	case "ps":
		fmt.Println(strings.Repeat("a", 64))
	}
	os.Exit(0)
}

func TestDockerRemoveExactStoppedManagedID(t *testing.T) {
	for _, mode := range []string{"stopped", "running", "label", "category", "missing workspace"} {
		t.Run(mode, func(t *testing.T) {
			b, e, raw, log := dockerFixture(t)
			raw["State"] = map[string]any{"Running": mode == "running"}
			labels := raw["Config"].(map[string]any)["Labels"].(map[string]string)
			if mode == "label" {
				labels[DockerManagedLabel] = "false"
			}
			if mode == "category" {
				labels["io.paseo-dock.category"] = "sandbox"
			}
			if mode == "missing workspace" {
				e.Workspace = filepath.Join(e.Workspace, "missing")
				raw["Mounts"] = []any{map[string]any{"Type": "bind", "Source": e.Workspace, "Destination": e.Workspace, "RW": true}}
			}
			writeDockerInspect(t, raw)
			err := b.Remove(context.Background(), e)
			calls := dockerCalls(t, log)
			allowed := mode == "stopped" || mode == "missing workspace"
			if (err == nil) != allowed || strings.Contains(calls, `"stop"`) || strings.Contains(calls, `"--force"`) {
				t.Fatal(err, calls)
			}
			if strings.Contains(calls, `["rm","`+e.ContainerID+`"]`) != allowed {
				t.Fatal(err, calls)
			}
		})
	}
}

func TestDockerAbsenceIsNotInspectionFailure(t *testing.T) {
	for _, message := range []string{"Error: No such object: ", "Error: No such container: ", "Cannot connect to Docker daemon", "permission denied: No such object: "} {
		t.Run(message, func(t *testing.T) {
			b, e, _, _ := dockerFixture(t)
			t.Setenv("DOCKER_BACKEND_INSPECT_ERROR", message+e.ContainerID)
			_, err := b.Inspect(context.Background(), e)
			want := strings.HasPrefix(message, "Error: No such")
			if errors.Is(err, ErrRuntimeNotFound) != want {
				t.Fatal(err)
			}
		})
	}
}

func dockerFixture(t *testing.T) (*DockerBackend, config.Entry, map[string]any, string) {
	t.Helper()
	dir := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(exe, filepath.Join(dir, "docker")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DOCKER_BACKEND_FAKE", "1")
	t.Setenv("DOCKER_BACKEND_LOG", filepath.Join(dir, "log"))
	t.Setenv("DOCKER_BACKEND_INSPECT", filepath.Join(dir, "inspect"))
	workspace, err := config.Canonical(dir)
	if err != nil {
		t.Fatal(err)
	}
	e := config.Entry{Category: config.CategoryContainer, ContainerID: strings.Repeat("a", 64), Image: "managed:locked", ImageID: "sha256:image", Workspace: workspace, Network: "bridge", Agent: "codex", Tool: "superpowers", Ports: []config.PortMapping{{HostAddress: "127.0.0.1", HostPort: 3000, ContainerPort: 3000}}}
	raw := map[string]any{"Id": e.ContainerID, "Image": e.ImageID, "State": map[string]any{"Running": true}, "Config": map[string]any{"Labels": map[string]string{DockerManagedLabel: "true", "io.paseo-dock.category": "container"}}, "Mounts": []any{map[string]any{"Type": "bind", "Source": workspace, "Destination": workspace, "RW": true}}, "HostConfig": map[string]any{"NetworkMode": "bridge", "PortBindings": map[string]any{"3000/tcp": []any{map[string]string{"HostIp": "127.0.0.1", "HostPort": "3000"}}}}}
	raw["NetworkSettings"] = map[string]any{"Networks": map[string]any{"bridge": map[string]any{}}}
	writeDockerInspect(t, raw)
	return &DockerBackend{}, e, raw, filepath.Join(dir, "log")
}
func writeDockerInspect(t *testing.T, raw map[string]any) {
	t.Helper()
	b, err := json.Marshal([]any{raw})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv("DOCKER_BACKEND_INSPECT"), b, 0600); err != nil {
		t.Fatal(err)
	}
}
func dockerCalls(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestDockerRejectsForeignSameName(t *testing.T) {
	for _, change := range []string{"label", "id", "missing-id"} {
		t.Run(change, func(t *testing.T) {
			b, e, raw, log := dockerFixture(t)
			switch change {
			case "label":
				raw["Config"] = map[string]any{"Labels": map[string]string{}}
			case "id":
				raw["Id"] = strings.Repeat("b", 64)
			case "missing-id":
				e.ContainerID = ""
			}
			writeDockerInspect(t, raw)
			if err := b.Stop(context.Background(), e); err == nil {
				t.Fatal("foreign target accepted")
			}
			if err := b.Exec(context.Background(), e, []string{"true"}, nil, io.Discard, io.Discard); err == nil {
				t.Fatal("foreign exec accepted")
			}
			calls, _ := os.ReadFile(log)
			if strings.Contains(string(calls), `"stop"`) || strings.Contains(string(calls), `"exec"`) {
				t.Fatalf("mutated foreign target: %s", calls)
			}
		})
	}
}
func TestDockerRejectsChangedImageMountOrPorts(t *testing.T) {
	for _, change := range []string{"image", "mount", "extra-mount", "readonly", "network", "attached-network", "ports"} {
		t.Run(change, func(t *testing.T) {
			b, e, raw, log := dockerFixture(t)
			switch change {
			case "image":
				raw["Image"] = "sha256:other"
			case "mount":
				raw["Mounts"].([]any)[0].(map[string]any)["Source"] = "/other"
			case "extra-mount":
				raw["Mounts"] = append(raw["Mounts"].([]any), map[string]any{"Type": "bind", "Source": "/home", "Destination": "/host-home", "RW": true})
			case "readonly":
				raw["Mounts"].([]any)[0].(map[string]any)["RW"] = false
			case "network":
				raw["HostConfig"].(map[string]any)["NetworkMode"] = "host"
			case "attached-network":
				raw["NetworkSettings"] = map[string]any{"Networks": map[string]any{"bridge": map[string]any{}, "foreign": map[string]any{}}}
			case "ports":
				raw["HostConfig"].(map[string]any)["PortBindings"] = map[string]any{}
			}
			writeDockerInspect(t, raw)
			if err := b.Stop(context.Background(), e); err == nil {
				t.Fatal("changed target accepted")
			}
			if err := b.Exec(context.Background(), e, []string{"true"}, nil, io.Discard, io.Discard); err == nil {
				t.Fatal("changed exec accepted")
			}
			calls := dockerCalls(t, log)
			if strings.Contains(calls, `"stop"`) || strings.Contains(calls, `"exec"`) {
				t.Fatalf("mutated changed target: %s", calls)
			}
		})
	}
}
func TestDockerUsesBridgeAndLoopbackPorts(t *testing.T) {
	b, e, _, log := dockerFixture(t)
	e.ContainerID = ""
	e.ImageID = ""
	e.Network = ""
	e.Ports[0].HostAddress = ""
	created, err := b.Create(context.Background(), e)
	if err != nil {
		t.Fatal(err)
	}
	if created.ContainerID != strings.Repeat("a", 64) || created.ImageID != "sha256:image" || created.Network != "bridge" || created.Ports[0].HostAddress != "127.0.0.1" {
		t.Fatalf("unresolved identity: %+v", created)
	}
	calls := dockerCalls(t, log)
	for _, want := range []string{`"--network","bridge"`, `"--publish","127.0.0.1:3000:3000/tcp"`, `"--mount","type=bind,src=` + e.Workspace + `,dst=` + e.Workspace + `"`, `"--label","` + DockerManagedLabel + `=true"`} {
		if !strings.Contains(calls, want) {
			t.Errorf("missing %s in %s", want, calls)
		}
	}
	if strings.Contains(calls, `"--rm"`) || strings.Count(calls, `"--mount"`) != 1 {
		t.Fatalf("unsafe persistence/mounts: %s", calls)
	}
}
func TestDockerPreservesHomeOnRestart(t *testing.T) {
	b, e, raw, log := dockerFixture(t)
	raw["State"] = map[string]any{"Running": false}
	writeDockerInspect(t, raw)
	if _, err := b.Create(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	calls := dockerCalls(t, log)
	if !strings.Contains(calls, `"start","`+e.ContainerID+`"`) || strings.Contains(calls, `"run"`) || strings.Contains(calls, `"rm"`) {
		t.Fatalf("recreated persistent target: %s", calls)
	}
}
func TestDockerDaemonFailureIsUnknown(t *testing.T) {
	b, e, _, _ := dockerFixture(t)
	t.Setenv("DOCKER_BACKEND_FAIL", "1")
	state, err := b.Inspect(context.Background(), e)
	if err == nil || state.Known {
		t.Fatalf("daemon failure became known: %+v, %v", state, err)
	}
	if _, err := b.List(context.Background()); err == nil {
		t.Fatal("daemon failure became empty list")
	}
}

func TestDockerReuseRejectsUnconfirmedNetwork(t *testing.T) {
	b, e, _, log := dockerFixture(t)
	e.Network = ""
	if _, err := b.Create(context.Background(), e); err == nil {
		t.Fatal("reuse silently normalized the confirmed identity")
	}
	calls, _ := os.ReadFile(log)
	if strings.Contains(string(calls), `"start"`) || strings.Contains(string(calls), `"exec"`) {
		t.Fatalf("unconfirmed reuse mutated target: %s", calls)
	}
}
func TestDockerStartupFailureRetainsExactIdentity(t *testing.T) {
	b, e, _, log := dockerFixture(t)
	e.ContainerID = ""
	t.Setenv("DOCKER_BACKEND_EXEC_FAIL", "1")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	signal := filepath.Join(t.TempDir(), "ready")
	t.Setenv("DOCKER_BACKEND_READY_SIGNAL", signal)
	type outcome struct {
		entry config.Entry
		err   error
	}
	done := make(chan outcome, 1)
	go func() { created, err := b.Create(ctx, e); done <- outcome{created, err} }()
	waitDockerSignal(t, signal)
	cancel()
	result := <-done
	created, err := result.entry, result.err
	if err == nil || created.ContainerID != strings.Repeat("a", 64) {
		t.Fatalf("startup failure lost resource: %+v %v", created, err)
	}
	calls := dockerCalls(t, log)
	if !strings.Contains(calls, `"HOME=/home/agent"`) || !strings.Contains(calls, "paseo-dock-agent-ready") || !strings.Contains(calls, "runuser -u agent") || !strings.Contains(calls, "chown -R") {
		t.Fatalf("missing ownership/readiness: %s", calls)
	}
}

func waitDockerSignal(t *testing.T, path string) {
	t.Helper()
	deadline := time.After(15 * time.Second)
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		select {
		case <-deadline:
			t.Fatal("fake Docker boundary never reached")
		case <-tick.C:
		}
	}
}

func TestDockerRejectsMissingRunningBridgeAttachment(t *testing.T) {
	for _, mode := range []string{"missing", "empty", "null-bridge"} {
		t.Run(mode, func(t *testing.T) {
			b, e, raw, log := dockerFixture(t)
			if mode == "missing" {
				delete(raw, "NetworkSettings")
			} else {
				raw["NetworkSettings"] = map[string]any{"Networks": map[string]any{}}
			}
			if mode == "null-bridge" {
				raw["NetworkSettings"] = map[string]any{"Networks": map[string]any{"bridge": nil}}
			}
			writeDockerInspect(t, raw)
			if err := b.Stop(context.Background(), e); err == nil {
				t.Fatal("running target missing bridge accepted")
			}
			if strings.Contains(dockerCalls(t, log), `"stop"`) {
				t.Fatal("stopped unvalidated target")
			}
		})
	}
}
func TestDockerStoppedEmptyAttachmentsAreObservable(t *testing.T) {
	b, e, raw, _ := dockerFixture(t)
	raw["State"] = map[string]any{"Running": false}
	raw["NetworkSettings"] = map[string]any{"Networks": map[string]any{}}
	writeDockerInspect(t, raw)
	state, err := b.Inspect(context.Background(), e)
	if err != nil || !state.Known || state.Running {
		t.Fatalf("stopped representation: %+v %v", state, err)
	}
}
func TestDockerPartialCreationRetainsCID(t *testing.T) {
	for _, mode := range []string{"failed", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			b, e, _, log := dockerFixture(t)
			e.ContainerID = ""
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "failed" {
				t.Setenv("DOCKER_BACKEND_RUN_FAIL", "1")
			} else {
				signal := filepath.Join(t.TempDir(), "created")
				t.Setenv("DOCKER_BACKEND_RUN_SIGNAL", signal)
				go func() {
					for {
						if _, err := os.Stat(signal); err == nil {
							cancel()
							return
						}
						select {
						case <-ctx.Done():
							return
						case <-time.After(10 * time.Millisecond):
						}
					}
				}()
			}
			created, err := b.Create(ctx, e)
			if err == nil || created.ContainerID != strings.Repeat("a", 64) {
				t.Fatalf("partial creation lost CID: %+v %v", created, err)
			}
			if mode == "canceled" && !errors.Is(err, context.Canceled) {
				t.Fatalf("lost cancellation: %v", err)
			}
			calls := dockerCalls(t, log)
			if !strings.Contains(calls, `"inspect","--type","container","`+created.ContainerID+`"`) {
				t.Fatalf("no exact recovery inspection: %s", calls)
			}
			if strings.Contains(calls, `"rm"`) || strings.Contains(calls, `"stop"`) || strings.Contains(calls, `"exec"`) {
				t.Fatalf("partial resource was mutated: %s", calls)
			}
		})
	}
}
func TestDockerReadOnlyListDoesNotStartGuest(t *testing.T) {
	b, _, _, log := dockerFixture(t)
	states, err := b.List(context.Background())
	if err != nil || len(states) != 1 || !states[0].Known || !states[0].Running {
		t.Fatalf("list: %+v %v", states, err)
	}
	calls := dockerCalls(t, log)
	if strings.Contains(calls, `"start"`) || strings.Contains(calls, `"exec"`) {
		t.Fatalf("list started guest: %s", calls)
	}
}
func TestDockerStoppedExecRequiresExplicitStart(t *testing.T) {
	b, e, raw, log := dockerFixture(t)
	raw["State"] = map[string]any{"Running": false}
	writeDockerInspect(t, raw)
	if err := b.Exec(context.Background(), e, []string{"bash"}, nil, io.Discard, io.Discard); err == nil {
		t.Fatal("stopped exec succeeded")
	}
	calls := dockerCalls(t, log)
	if strings.Contains(calls, `"start"`) || strings.Contains(calls, `"exec"`) {
		t.Fatalf("exec started stopped guest: %s", calls)
	}
}

func TestDockerReadinessRevalidatesBeforeExec(t *testing.T) {
	b, e, _, log := dockerFixture(t)
	t.Setenv("DOCKER_BACKEND_CHANGE_THIRD_INSPECT", "1")
	if _, err := b.Create(context.Background(), e); err == nil {
		t.Fatal("readiness accepted a changed runtime")
	}
	calls := dockerCalls(t, log)
	if strings.Contains(calls, `"exec"`) {
		t.Fatalf("readiness touched changed runtime: %s", calls)
	}
}

func TestDockerExecAndStopUseValidatedID(t *testing.T) {
	b, e, _, log := dockerFixture(t)
	if err := b.Exec(context.Background(), e, []string{"printf", "hello"}, strings.NewReader("input"), io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := b.Stop(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	calls := dockerCalls(t, log)
	user := fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid())
	for _, want := range []string{`"--user","` + user + `"`, `"HOME=/home/agent"`, `"--workdir","` + e.Workspace + `"`, `"` + e.ContainerID + `","printf","hello"`, `"stop","` + e.ContainerID + `"`} {
		if !strings.Contains(calls, want) {
			t.Errorf("missing %s in %s", want, calls)
		}
	}
}
