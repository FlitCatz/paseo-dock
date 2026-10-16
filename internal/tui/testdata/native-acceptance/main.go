// Disposable live acceptance; no host authentication is copied.
package main

import (
	"context"
	"fmt"
	"os"
	"paseo-dock/internal/config"
	"paseo-dock/internal/launcher"
	"paseo-dock/internal/operations"
	"path/filepath"
	"strings"
	"time"
)

const root = "/private/tmp/paseo-sbx-category-native-20261008"

func must(e error) {
	if e != nil {
		panic(e)
	}
}
func main() {
	must(os.Setenv("HOME", root+"/home"))
	must(os.Setenv("PASEO_HOME", root+"/paseo"))
	must(os.Setenv("DOCKER_HOST", "unix:///Users/dev-r/.orbstack/run/docker.sock"))
	source, e := os.Getwd()
	must(e)
	for _, p := range []string{"home", "paseo", "workspace", "trusted", "trusted/bin"} {
		must(os.MkdirAll(root+"/"+p, 0700))
	}
	if len(os.Args) > 1 && os.Args[1] == "setup" {
		for _, p := range []string{"versions.lock.json", "Dockerfile", "go.mod", "go.sum", "scripts/install-tools.sh", "scripts/install-utilities.sh", "scripts/agent-instructions.md", "bin/agent-wrapper", "bin/install-runtime"} {
			b, e := os.ReadFile(filepath.Join(source, p))
			must(e)
			must(os.MkdirAll(filepath.Dir(root+"/trusted/"+p), 0700))
			must(os.WriteFile(root+"/trusted/"+p, b, 0755))
			must(os.Chmod(root+"/trusted/"+p, 0755))
		}
		for _, dir := range []string{"cmd/install-runtime", "internal/runtimeinstall"} {
			must(filepath.WalkDir(filepath.Join(source, dir), func(p string, d os.DirEntry, e error) error {
				if e != nil {
					return e
				}
				rel, e := filepath.Rel(source, p)
				if e != nil {
					return e
				}
				dst := root + "/trusted/" + rel
				if d.IsDir() {
					return os.MkdirAll(dst, 0700)
				}
				b, e := os.ReadFile(p)
				if e != nil {
					return e
				}
				return os.WriteFile(dst, b, 0600)
			}))
		}
		must(config.WriteJSON(root+"/paseo/config.json", map[string]any{"version": 1, "daemon": map[string]any{"listen": "127.0.0.1:0"}}))
		must(os.MkdirAll(root+"/home/.docker/cli-plugins", 0700))
		if _, e := os.Lstat(root + "/home/.docker/cli-plugins/docker-buildx"); os.IsNotExist(e) {
			must(os.Symlink("/Applications/OrbStack.app/Contents/MacOS/xbin/docker-buildx", root+"/home/.docker/cli-plugins/docker-buildx"))
		}
		fmt.Println("ISOLATION", root)
		return
	}
	runtime, e := config.RuntimePath(root + "/workspace")
	must(e)
	if len(os.Args) > 1 && os.Args[1] == "plugins-enable" {
		var cfg map[string]any
		must(config.ReadJSON(root+"/paseo/config.json", &cfg))
		cfg["pluginsEnabled"] = true
		must(config.WriteJSON(root+"/paseo/config.json", cfg))
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "probe" {
		var reg config.Registry
		must(config.ReadJSON(runtime+"/registry.json", &reg))
		entry := reg.Profiles["container/antigravity-cli-native"]
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		must((&operations.DockerBackend{}).Exec(ctx, entry, []string{"bash", "-lc", `set -eu; echo NATIVE_UID=$(id -u):$(id -g); printf NATIVE_WORKSPACE_OK > native-workspace-probe; cat native-workspace-probe; echo; getent hosts storage.googleapis.com >/dev/null; curl -sS -I --max-time 15 https://storage.googleapis.com | head -1; for p in /home/agent/.config/antigravity-cli /home/agent/.gemini; do if test -e "$p"; then echo AUTH_PATH_PRESENT:$p; else echo AUTH_PATH_ABSENT:$p; fi; done`}, nil, os.Stdout, os.Stderr))
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "assets" {
		must(os.MkdirAll(root+"/paseo/plugin-data/antigravity-cli", 0700))
		must(os.WriteFile(root+"/paseo/plugin-data/antigravity-cli/schema.json", []byte(`{"type":"object"}`), 0600))
		must(os.Chdir(root + "/workspace"))
		code, e := launcher.LaunchIO(runtime, "container-antigravity-cli-native", []string{"--input-format", "stream-json", "--output-format", "stream-json", "--json-schema", root + "/paseo/plugin-data/antigravity-cli/schema.json"}, strings.NewReader(""), os.Stdout, os.Stderr)
		fmt.Printf("REAL_NATIVE_EOF code=%d err=%v\n", code, e)
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "launch" {
		must(os.Chdir(root + "/workspace"))
		code, e := launcher.LaunchIO(runtime, "container-antigravity-cli-native", os.Args[2:], os.Stdin, os.Stdout, os.Stderr)
		if e != nil {
			fmt.Fprintln(os.Stderr, e)
		}
		os.Exit(code)
	}
	s, e := operations.New(root + "/trusted")
	must(e)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	selection, e := s.Catalog.Normalize(config.Selection{Schema: 2, Category: config.CategoryContainer, Pairs: []config.Pair{{Agent: "antigravity-cli", Methodology: "native"}}})
	must(e)
	kind := operations.KindCreate
	if len(os.Args) > 1 && os.Args[1] == "stop" {
		kind = operations.KindStop
	}
	target := operations.Target{Workspace: root + "/workspace", Category: config.CategoryContainer}
	if kind == operations.KindStop || len(os.Args) > 1 && os.Args[1] == "start" {
		var reg config.Registry
		must(config.ReadJSON(runtime+"/registry.json", &reg))
		entry := reg.Profiles["container/antigravity-cli-native"]
		if entry.ContainerID != "10f4af99d91d6c1a4a28750a2f1f053e3caeb2242f8dcf033f8b2db986f765be" {
			must(fmt.Errorf("unexpected disposable stop target"))
		}
		target.Runtime, target.Profile, target.Entry = runtime, entry.Key(), entry
	}
	result, e := s.Execute(ctx, operations.Request{Kind: kind, Target: target, Selection: selection, Namespace: "paseo-sbx-category-native-20261008", BuildFirst: kind == operations.KindCreate && !(len(os.Args) > 1 && os.Args[1] == "start"), PaseoHome: root + "/paseo"}, nil)
	must(e)
	fmt.Printf("RESULT %+v\n", result)
	var reg config.Registry
	must(config.ReadJSON(runtime+"/registry.json", &reg))
	entry := reg.Profiles["container/antigravity-cli-native"]
	fmt.Printf("IDENTITY cid=%s image=%s runtime=%s\n", entry.ContainerID, entry.ImageID, runtime)
}
