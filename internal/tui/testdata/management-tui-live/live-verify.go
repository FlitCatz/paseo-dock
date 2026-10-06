// Reproducible Task 6 evidence helper. No production runtime dependency.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/creack/pty"
	"paseo-sbx/internal/config"
	"paseo-sbx/internal/operations"
)

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "BLOCKER:", err)
		os.Exit(1)
	}
}
func env(values map[string]string) []string {
	out := []string{}
	for _, v := range os.Environ() {
		k, _, _ := strings.Cut(v, "=")
		if _, ok := values[k]; !ok {
			out = append(out, v)
		}
	}
	for k, v := range values {
		out = append(out, k+"="+v)
	}
	return out
}
func real(name string, args ...string) ([]byte, error) {
	c := exec.Command(name, args...)
	return c.Output()
}
func main() {
	if slices.Contains([]string{"sbx", "paseo"}, filepath.Base(os.Args[0])) {
		proxy()
		return
	}
	if os.Getenv("LIVE_CHILD") == "1" {
		child()
		return
	}
	root, err := os.Getwd()
	must(err)
	realSBX, err := exec.LookPath("sbx")
	must(err)
	realPaseo, err := exec.LookPath("paseo")
	must(err)
	b, err := real(realSBX, "template", "ls", "--json")
	must(err)
	var rows struct {
		Images []operations.Template `json:"images"`
	}
	must(json.Unmarshal(b, &rows))
	s, err := operations.New(root)
	must(err)
	var selection config.Selection
	var image string
	for _, profile := range append([]string{"codex-superpowers"}, s.Catalog.AllProfiles()...) {
		for _, utilities := range []string{"", "rtk", "headroom", "rtk,headroom"} {
			candidate, e := s.ResolveSelection(operations.SelectionOptions{Profiles: profile, Utilities: utilities, SetUtilities: true})
			if e != nil {
				continue
			}
			tag := config.ImageTag(s.Lock, candidate, s.Catalog)
			wanted := "paseo-sbx/" + profile + ":" + tag
			for _, row := range rows.Images {
				if strings.TrimPrefix(row.Repository, "docker.io/")+":"+row.Tag == wanted {
					selection = candidate
					image = wanted
					break
				}
			}
			if image != "" {
				break
			}
		}
		if image != "" {
			break
		}
	}
	if image == "" {
		must(fmt.Errorf("no loaded template exactly matches current lock/catalog/selection; no rebuild or retag authorized"))
	}
	temp, err := os.MkdirTemp("/private/tmp", "paseo-sbx-live-task6-")
	must(err)
	home := filepath.Join(temp, "home")
	trusted := filepath.Join(temp, "trusted-root")
	workspace := filepath.Join(temp, "workspace")
	paseoHome := filepath.Join(home, ".paseo")
	for _, dir := range []string{home, trusted, workspace, paseoHome, filepath.Join(trusted, "bin"), filepath.Join(temp, "commands")} {
		must(os.MkdirAll(dir, 0700))
	}
	for _, file := range []string{"versions.lock.json", "catalog.json", "Dockerfile", "bin/agent-wrapper"} {
		data, e := os.ReadFile(filepath.Join(root, file))
		if os.IsNotExist(e) && file == "catalog.json" {
			continue
		}
		must(e)
		must(os.WriteFile(filepath.Join(trusted, file), data, 0700))
	}
	exe, err := os.Executable()
	must(err)
	for _, name := range []string{"sbx", "paseo"} {
		must(os.Symlink(exe, filepath.Join(temp, "commands", name)))
	}
	data, err := json.Marshal(selection)
	must(err)
	// HOME is set only in this child environment. Caller/auth environment is untouched.
	c := exec.Command(exe)
	c.Env = env(map[string]string{"HOME": home, "PATH": filepath.Join(temp, "commands") + ":" + os.Getenv("PATH"), "LIVE_CHILD": "1", "LIVE_TEMP": temp, "LIVE_TRUSTED": trusted, "LIVE_WORKSPACE": workspace, "LIVE_PASEO_HOME": paseoHome, "LIVE_SELECTION": string(data), "LIVE_IMAGE": image, "LIVE_AUTH_HOME": os.Getenv("HOME"), "LIVE_AUTH_PATH": os.Getenv("PATH"), "LIVE_REAL_SBX": realSBX, "LIVE_REAL_PASEO": realPaseo, "LIVE_RUN": fmt.Sprint(slices.Contains(os.Args[1:], "--run"))})
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	err = c.Run()
	if err != nil {
		must(err)
	}
}
func proxy() {
	name := filepath.Base(os.Args[0])
	a := os.Args[1:]
	sandbox := os.Getenv("LIVE_SANDBOX")
	workspace := os.Getenv("LIVE_WORKSPACE")
	ok := false
	if name == "paseo" {
		ok = len(a) >= 3 && a[0] == "--home" && a[1] == os.Getenv("LIVE_PASEO_HOME") && slices.Contains([]string{"status", "start", "reload"}, a[2])
	} else if len(a) > 0 {
		switch a[0] {
		case "ls":
			ok = strings.Join(a, " ") == "ls --json"
		case "create":
			ok = len(a) > 1 && a[len(a)-1] == workspace && flag(a, "--name") == sandbox && sandbox != "" && flag(a, "--template") == os.Getenv("LIVE_IMAGE")
		case "stop":
			ok = len(a) == 2 && a[1] == sandbox && sandbox != ""
		case "exec":
			ok = sandbox != "" && slices.Contains(a, sandbox) && (flag(a, "--workdir") == "/tmp" || flag(a, "--workdir") == workspace)
		}
	}
	if !ok {
		must(fmt.Errorf("isolation guard rejected %s %q", name, a))
	}
	path := os.Getenv("LIVE_REAL_SBX")
	overrides := map[string]string{"PATH": os.Getenv("LIVE_AUTH_PATH"), "HOME": os.Getenv("LIVE_AUTH_HOME")}
	if name == "paseo" {
		path = os.Getenv("LIVE_REAL_PASEO")
		overrides["HOME"] = os.Getenv("HOME")
	}
	c := exec.Command(path, a...)
	c.Env = env(overrides)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	err := c.Run()
	code := 0
	if e, ok := err.(*exec.ExitError); ok {
		code = e.ExitCode()
	} else if err != nil {
		code = 1
	}
	fmt.Fprintf(os.Stderr, "external %s %q exit=%d\n", name, a, code)
	os.Exit(code)
}
func flag(a []string, key string) string {
	for i := 0; i+1 < len(a); i++ {
		if a[i] == key {
			return a[i+1]
		}
	}
	return ""
}
func child() {
	temp := os.Getenv("LIVE_TEMP")
	trusted := os.Getenv("LIVE_TRUSTED")
	workspace := os.Getenv("LIVE_WORKSPACE")
	paseoHome := os.Getenv("LIVE_PASEO_HOME")
	base, err := config.BaseDir()
	must(err)
	runtime, err := config.RuntimePath(workspace)
	must(err)
	launcher := config.LauncherPath(base)
	realBase := filepath.Join(os.Getenv("LIVE_AUTH_HOME"), ".local", "share", "paseo-sbx")
	for _, path := range []string{base, runtime, launcher, trusted, filepath.Join(paseoHome, "config.json")} {
		if !config.Within(path, temp) || config.Within(path, realBase) {
			must(fmt.Errorf("unsafe isolated path %s", path))
		}
	}
	must(config.WriteJSON(filepath.Join(paseoHome, "config.json"), map[string]any{"version": 1, "daemon": map[string]any{"listen": "127.0.0.1:0"}}))
	var selection config.Selection
	must(json.Unmarshal([]byte(os.Getenv("LIVE_SELECTION")), &selection))
	profile := selection.Profiles()[0]
	sandbox := "paseo-" + profile + "-" + filepath.Base(runtime)
	must(os.Setenv("LIVE_SANDBOX", sandbox))
	fmt.Printf("ISOLATION VERIFIED\nRoot=%s\nWorkspace=%s\nBaseDir=%s\nLauncherPath=%s\nPaseoConfig=%s\nCallerBase=%s (excluded)\nSandbox=%s\nImage=%s\n", trusted, workspace, base, launcher, filepath.Join(paseoHome, "config.json"), realBase, sandbox, os.Getenv("LIVE_IMAGE"))
	before, err := real("sbx", "ls", "--json")
	must(err)
	var rows struct {
		Sandboxes []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"sandboxes"`
	}
	must(json.Unmarshal(before, &rows))
	for _, row := range rows.Sandboxes {
		if row.Name == sandbox {
			must(fmt.Errorf("disposable name already exists"))
		}
	}
	if os.Getenv("LIVE_RUN") != "true" {
		fmt.Println("PREFLIGHT ONLY; no live resources mutated")
		return
	}
	service, err := operations.New(trusted)
	must(err)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	request := operations.Request{Kind: operations.KindCreate, Target: operations.Target{Workspace: workspace}, Selection: selection, PaseoHome: paseoHome, Memory: "2g"}
	result, err := service.Execute(ctx, request, nil)
	fmt.Printf("STEP create exit=%d result=%+v error=%v\n", exit(err), result, err)
	if err != nil {
		return
	}
	var reg config.Registry
	must(config.ReadJSON(filepath.Join(runtime, "registry.json"), &reg))
	target := operations.Target{Workspace: workspace, Runtime: runtime, Profile: profile, Entry: reg.Profiles[profile]}
	for _, kind := range []operations.Kind{operations.KindUpdate, operations.KindStop} {
		result, err = service.Execute(ctx, operations.Request{Kind: kind, Target: target}, nil)
		fmt.Printf("STEP %s exit=%d result=%+v error=%v\n", kind, exit(err), result, err)
	}
	cmd, err := service.ShellCommand(ctx, target)
	if err == nil {
		var terminal *os.File
		terminal, err = pty.StartWithSize(cmd, &pty.Winsize{Cols: 80, Rows: 24})
		if err == nil {
			terminal.Write([]byte("printf 'TASK6_LIVE_SHELL_OK\\n'; exit\n"))
			b := make([]byte, 4096)
			for {
				n, e := terminal.Read(b)
				if n > 0 {
					os.Stdout.Write(b[:n])
				}
				if e != nil {
					break
				}
			}
			terminal.Close()
			err = cmd.Wait()
		}
	}
	fmt.Printf("STEP shell exit=%d error=%v\n", exit(err), err)
	for _, kind := range []operations.Kind{operations.KindRegister, operations.KindUnregister} {
		result, err = service.Execute(ctx, operations.Request{Kind: kind, Target: target, PaseoHome: paseoHome, Apply: true}, nil)
		fmt.Printf("STEP %s exit=%d result=%+v error=%v\n", kind, exit(err), result, err)
	}
	after, err := real("sbx", "ls", "--json")
	must(err)
	must(json.Unmarshal(after, &rows))
	for _, row := range rows.Sandboxes {
		if row.Name == sandbox {
			fmt.Printf("REMAINING sandbox=%s state=%s (preserved)\n", row.Name, row.Status)
		}
	}
	must(config.ReadJSON(filepath.Join(runtime, "registry.json"), &reg))
	fmt.Printf("REMAINING runtime=%s active=%v profiles=%v\nREMAINING isolated PaseoHome=%s\n", runtime, reg.ActiveProfiles(), slices.Collect(func(yield func(string) bool) {
		for key := range reg.Profiles {
			if !yield(key) {
				return
			}
		}
	}), paseoHome)
}
func exit(err error) int {
	if err != nil {
		return 1
	}
	return 0
}
