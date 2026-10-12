package launcher

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"paseo-sbx/internal/config"
	"paseo-sbx/internal/operations"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNativeAssetsNeverRunRootMutation(t *testing.T) {
	runtime, _, entry, log, asset := nativeReviewFixture(t)
	_, _, cleanup, err := prepareNativeContext(context.Background(), runtime, entry.Key(), []string{"--json-schema", asset}, entry.Workspace, entry, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	cleanup()
	calls, _ := os.ReadFile(log)
	if strings.Contains(string(calls), `"root"`) || strings.Contains(string(calls), `"cp"`) || strings.Contains(string(calls), `"/usr/bin/chown"`) {
		t.Fatalf("native asset transport runs privileged mutation: %s", calls)
	}
}

func TestNativeExtractionUsesFixedAgentTar(t *testing.T) {
	_, _, entry, _, _ := nativeReviewFixture(t)
	cmd, err := nativeCommand(context.Background(), entry, nativeExtract, "/tmp/paseo-sbx-assets.fixture")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"docker", "exec", "-i", "--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()), "--env", "HOME=/home/agent", entry.ContainerID, "/usr/bin/tar", "-xpf", "-", "--no-same-owner", "-C", "/tmp/paseo-sbx-assets.fixture"}
	if strings.Join(cmd.Args, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("unsafe extraction argv: %q", cmd.Args)
	}
}

func TestNativeExtractionPropagatesSubprocessFailure(t *testing.T) {
	runtime, _, entry, _, asset := nativeReviewFixture(t)
	t.Setenv("LAUNCH_TAR_FAIL", "1")
	_, _, _, err := prepareNativeContext(context.Background(), runtime, entry.Key(), []string{"--json-schema", asset}, entry.Workspace, entry, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "cannot extract native plugin assets") {
		t.Fatalf("tar failure lost: %v", err)
	}
}

func TestNativeExtractionRejectsUnsafePaths(t *testing.T) {
	_, _, entry, log, _ := nativeReviewFixture(t)
	for _, path := range []string{"/home/agent/.config", "/tmp", "/tmp/paseo-sbx-assets.fixture/../elsewhere", "/tmp/paseo-sbx-assets.fixture/schema", "/tmp/paseo-sbx-assets.fixture/asset-x", "/tmp/paseo-sbx-assets.fixture/asset-0/child", "/tmp/paseo-sbx-assets.bad\nname"} {
		cmd, err := nativeCommand(context.Background(), entry, nativeExtract, path)
		if err == nil || cmd != nil {
			t.Fatalf("unsafe root ownership allowed: %q", path)
		}
	}
	calls, _ := os.ReadFile(log)
	for _, line := range strings.Split(strings.TrimSpace(string(calls)), "\n") {
		var argv []string
		if json.Unmarshal([]byte(line), &argv) != nil {
			continue
		}
		if len(argv) > 0 && argv[0] != "inspect" {
			t.Fatalf("unsafe path mutated guest: %v", argv)
		}
	}
}

func TestDockerNativePrivateAssetsLive(t *testing.T) {
	if os.Getenv("NATIVE_ACCEPTANCE_LIVE") != "1" {
		t.Skip("requires exact disposable native acceptance resource")
	}
	const root = "/private/tmp/paseo-sbx-category-native-20261008"
	t.Setenv("HOME", root+"/home")
	t.Setenv("PASEO_HOME", root+"/paseo")
	t.Setenv("DOCKER_HOST", "unix:///Users/dev-r/.orbstack/run/docker.sock")
	runtime, err := config.RuntimePath(root + "/workspace")
	if err != nil {
		t.Fatal(err)
	}
	var reg config.Registry
	if err = config.ReadJSON(filepath.Join(runtime, "registry.json"), &reg); err != nil {
		t.Fatal(err)
	}
	entry := reg.Profiles["container/antigravity-cli-native"]
	if entry.ContainerID != "10f4af99d91d6c1a4a28750a2f1f053e3caeb2242f8dcf033f8b2db986f765be" || entry.Workspace != root+"/workspace" {
		t.Fatal("unexpected disposable resource")
	}
	asset, err := os.MkdirTemp(root+"/paseo/plugin-data/antigravity-cli", "ownership-probe-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(asset)
	if err = os.WriteFile(filepath.Join(asset, "private.txt"), []byte("NATIVE_PRIVATE_ASSET_OK"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	args, _, cleanup, err := prepareNativeContext(ctx, runtime, entry.Key(), []string{"--add-dir", asset}, entry.Workspace, entry, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	var out bytes.Buffer
	backend := &operations.DockerBackend{}
	if err = backend.Exec(ctx, entry, []string{"cat", filepath.Join(args[1], "private.txt")}, nil, &out, io.Discard); err != nil {
		t.Fatalf("private directory unreadable by guest agent: %v", err)
	}
	if out.String() != "NATIVE_PRIVATE_ASSET_OK" {
		t.Fatal("copied private contents changed")
	}
	out.Reset()
	if err = backend.Exec(ctx, entry, []string{"stat", "-c", "%u:%g %a", args[1], filepath.Join(args[1], "private.txt")}, nil, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("%d:%d 700\n%d:%d 600\n", os.Getuid(), os.Getgid(), os.Getuid(), os.Getgid())
	if out.String() != want {
		t.Fatalf("private modes/ownership changed: got %q want %q", out.String(), want)
	}
}

func TestNativeArchiveIgnoresAgentChownLive(t *testing.T) {
	if os.Getenv("NATIVE_ACCEPTANCE_LIVE") != "1" {
		t.Skip("requires exact disposable native acceptance resource")
	}
	const root = "/private/tmp/paseo-sbx-category-native-20261008"
	t.Setenv("HOME", root+"/home")
	t.Setenv("PASEO_HOME", root+"/paseo")
	t.Setenv("DOCKER_HOST", "unix:///Users/dev-r/.orbstack/run/docker.sock")
	runtime, err := config.RuntimePath(root + "/workspace")
	if err != nil {
		t.Fatal(err)
	}
	var reg config.Registry
	if err = config.ReadJSON(runtime+"/registry.json", &reg); err != nil {
		t.Fatal(err)
	}
	entry := reg.Profiles["container/antigravity-cli-native"]
	if entry.ContainerID != "10f4af99d91d6c1a4a28750a2f1f053e3caeb2242f8dcf033f8b2db986f765be" || entry.Workspace != root+"/workspace" {
		t.Fatal("unexpected disposable resource")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	backend := &operations.DockerBackend{}
	const fake = "/home/agent/.local/bin/chown"
	const marker = "/home/agent/.local/bin/paseo-native-chown-hijack"
	for _, path := range []string{fake, marker} {
		if err = backend.Exec(ctx, entry, []string{"/usr/bin/test", "!", "-e", path}, nil, io.Discard, io.Discard); err != nil {
			t.Fatalf("refuse existing probe path %s: %v", path, err)
		}
	}
	dir, err := os.MkdirTemp(entry.Workspace, "native-path-probe-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	source := filepath.Join(dir, "main.go")
	binary := filepath.Join(dir, "fake-chown")
	program := `package main
import("os";"strconv")
func main(){os.WriteFile("/home/agent/.local/bin/paseo-native-chown-hijack",[]byte(strconv.Itoa(os.Geteuid())),0666)}
`
	if err = os.WriteFile(source, []byte(program), 0600); err != nil {
		t.Fatal(err)
	}
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, source)
	build.Env = append(os.Environ(), "GOOS=linux", "GOARCH=arm64", "CGO_ENABLED=0")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build Go PATH probe: %v %s", err, output)
	}
	if err = backend.Exec(ctx, entry, []string{"/usr/bin/install", "-m", "0700", binary, fake}, nil, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if err := backend.Exec(cleanup, entry, []string{"/usr/bin/rm", "-f", "--", fake, marker}, nil, io.Discard, io.Discard); err != nil {
			t.Errorf("PATH probe cleanup: %v", err)
		}
	}()
	asset, err := os.MkdirTemp(root+"/paseo/plugin-data/antigravity-cli", "path-ownership-probe-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(asset)
	if err = os.WriteFile(filepath.Join(asset, "private.txt"), []byte("PRIVATE_PATH_PROBE_OK"), 0600); err != nil {
		t.Fatal(err)
	}
	args, _, cleanup, err := prepareNativeContext(ctx, runtime, entry.Key(), []string{"--add-dir", asset}, entry.Workspace, entry, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if err = backend.Exec(ctx, entry, []string{"/usr/bin/test", "!", "-e", marker}, nil, io.Discard, io.Discard); err != nil {
		var out bytes.Buffer
		_ = backend.Exec(ctx, entry, []string{"/usr/bin/cat", marker}, nil, &out, io.Discard)
		t.Fatalf("guest-writable chown ran with UID %q", out.String())
	}
	var out bytes.Buffer
	if err = backend.Exec(ctx, entry, []string{"/usr/bin/cat", filepath.Join(args[1], "private.txt")}, nil, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	if out.String() != "PRIVATE_PATH_PROBE_OK" {
		t.Fatal("private asset unreadable")
	}
	t.Log("agent PATH fake chown not executed; private asset readable")
}
