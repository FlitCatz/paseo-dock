// Explicitly authorized live acceptance helper. Product source is unchanged.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"paseo-dock/internal/config"
	"paseo-dock/internal/containerpolicy"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"time"
)

const trial = "/private/tmp/paseo-dock-runtime-remove-live-20261013"
const evidence = "/private/tmp/paseo-dock-runtime-remove-evidence"
const template = "docker.io/paseo-dock-category-live-20261007/sandbox/codex:2026.09.30-3b32788af33b"
const imageTag = "paseo-runtime-remove-live:20261013"

var source, wrapper, cliBinary string
var sequence int

type ownership struct {
	Sandboxes  []string
	Containers []string
	Image      string
}
type record struct {
	Args []string
	Exit int
	Log  string
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
func assert(ok bool, message string) {
	if !ok {
		panic(message)
	}
}
func write(path string, data []byte) {
	must(os.MkdirAll(filepath.Dir(path), 0700))
	must(os.WriteFile(path, data, 0600))
}
func writeJSON(path string, value any) {
	b, e := json.MarshalIndent(value, "", "  ")
	must(e)
	write(path, append(b, '\n'))
}
func read(path string) []byte { b, e := os.ReadFile(path); must(e); return b }
func own() ownership {
	var o ownership
	must(json.Unmarshal(read(trial+"/ownership.json"), &o))
	return o
}
func flag(a []string, k string) string {
	for i := 0; i+1 < len(a); i++ {
		if a[i] == k {
			return a[i+1]
		}
	}
	return ""
}
func within(p string) bool {
	r, e := filepath.Rel(trial, p)
	return e == nil && r != ".." && !strings.HasPrefix(r, "../") && filepath.IsAbs(p)
}
func environment(m map[string]string) []string {
	out := []string{}
	for _, s := range os.Environ() {
		k, _, _ := strings.Cut(s, "=")
		if _, ok := m[k]; !ok {
			out = append(out, s)
		}
	}
	for k, v := range m {
		assert(k != "HOME" && k != "home" && k != "CODEX_HOME", "forbidden env override")
		out = append(out, k+"="+v)
	}
	return out
}
func guarded(name string, args []string) {
	o := own()
	ok := false
	switch name {
	case "sbx":
		if len(args) > 0 {
			switch args[0] {
			case "ls":
				ok = slices.Equal(args, []string{"ls", "--json"})
			case "template":
				ok = slices.Equal(args, []string{"template", "ls", "--json"})
			case "create":
				ok = slices.Contains(o.Sandboxes, flag(args, "--name")) && flag(args, "--template") == template && flag(args, "--pull") == "never" && flag(args, "--skills") == "off" && within(args[len(args)-1])
			case "exec":
				ok = len(args) == 3 && slices.Contains(o.Sandboxes, args[1]) && args[2] == "/bin/true"
			case "stop":
				ok = len(args) == 2 && slices.Contains(o.Sandboxes, args[1])
			case "rm":
				ok = len(args) == 3 && args[1] == "--force" && slices.Contains(o.Sandboxes, args[2])
			}
		}
	case "docker":
		if len(args) > 0 {
			switch args[0] {
			case "ps", "inspect":
				ok = true
			case "image":
				ok = len(args) > 1 && (args[1] == "ls" || args[1] == "inspect" || (args[1] == "rm" && len(args) == 3 && o.Image != "" && args[2] == o.Image))
			case "build":
				ok = flag(args, "--tag") == imageTag && slices.Contains(args, "--pull=false") && flag(args, "--network") == "none" && args[len(args)-1] == trial+"/image" && o.Image == ""
			case "create":
				ok = slices.Contains([]string{"paseo-remove-live-20261013-single", "paseo-remove-live-20261013-prune"}, flag(args, "--name")) && flag(args, "--network") == "bridge" && o.Image != "" && args[len(args)-1] == o.Image && len(o.Containers) < 2
				mount := flag(args, "--mount")
				ok = ok && (mount == "type=bind,src="+trial+"/workspace-remove,dst="+trial+"/workspace-remove" || mount == "type=bind,src="+trial+"/workspace-prune,dst="+trial+"/workspace-prune")
				for _, label := range []string{"io.paseo-dock.managed=true", "io.paseo-dock.category=container", "io.paseo-dock.profile=container/codex-native"} {
					ok = ok && slices.Contains(args, label)
				}
			case "start", "stop", "rm":
				ok = len(args) == 2 && slices.Contains(o.Containers, args[1])
			}
		}
	case "paseo":
		ok = len(args) >= 3 && args[0] == "--home" && args[1] == trial+"/paseo-home" && (args[2] == "status" || args[2] == "start" || args[2] == "reload" || slices.Equal(args[2:], []string{"daemon", "stop"}))
	}
	assert(ok, fmt.Sprintf("guard rejected %s %q", name, args))
	real := os.Getenv("REMOVE_LIVE_REAL_" + strings.ToUpper(name))
	assert(filepath.IsAbs(real), "real executable missing")
	c := exec.Command("rtk", append([]string{"proxy", real}, args...)...)
	c.Env = environment(map[string]string{"PATH": os.Getenv("REMOVE_LIVE_ORIGINAL_PATH")})
	c.Stdin = os.Stdin
	var out, errout bytes.Buffer
	c.Stdout = &out
	c.Stderr = &errout
	err := c.Run()
	code := 0
	if err != nil {
		code = 1
		if e, ok := err.(*exec.ExitError); ok {
			code = e.ExitCode()
		}
	}
	f, e := os.OpenFile(evidence+"/external-commands.jsonl", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	must(e)
	must(json.NewEncoder(f).Encode(map[string]any{"tool": name, "args": args, "exit": code, "stdout": out.String(), "stderr": errout.String()}))
	must(f.Close())
	_, _ = os.Stdout.Write(out.Bytes())
	_, _ = os.Stderr.Write(errout.Bytes())
	os.Exit(code)
}

func run(args ...string) ([]byte, int) {
	sequence++
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	c := exec.CommandContext(ctx, "rtk", append([]string{"proxy"}, args...)...)
	c.Dir = source
	c.Env = environment(map[string]string{"PATH": trial + "/guard:" + os.Getenv("PATH"), "PASEO_DOCK_ROOT": source, "DOCKER_HOST": "unix:///Users/dev-r/.orbstack/run/docker.sock"})
	var out, stderr bytes.Buffer
	c.Stdout = &out
	c.Stderr = &stderr
	err := c.Run()
	code := 0
	if err != nil {
		code = 1
		if e, ok := err.(*exec.ExitError); ok {
			code = e.ExitCode()
		}
	}
	path := fmt.Sprintf("%s/command-%03d.log", evidence, sequence)
	write(path, []byte("STDOUT\n"+out.String()+"\nSTDERR\n"+stderr.String()))
	f, e := os.OpenFile(evidence+"/commands.jsonl", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	must(e)
	must(json.NewEncoder(f).Encode(record{args, code, path}))
	must(f.Close())
	fmt.Printf("command %03d exit=%d %s\n", sequence, code, strings.Join(args, " "))
	return out.Bytes(), code
}
func good(args ...string) []byte {
	b, c := run(args...)
	assert(c == 0, fmt.Sprintf("command failed %q; see logs", args))
	return b
}
func cli(args ...string) ([]byte, int) { return run(append([]string{cliBinary}, args...)...) }
func cliGood(args ...string) []byte {
	b, c := cli(args...)
	assert(c == 0, "CLI failed; inspect command logs")
	return b
}
func rejected(args ...string) {
	_, c := cli(args...)
	assert(c != 0, "expected CLI refusal")
	log := read(fmt.Sprintf("%s/command-%03d.log", evidence, sequence))
	expected := "is running; stop first"
	if args[0] == "prune" {
		expected = "runtime pruning blocked"
	}
	assert(bytes.Contains(log, []byte(expected)), "unexpected refusal reason; inspect logs")
}
func snapshot(paths []string) map[string]string {
	out := map[string]string{}
	for _, root := range paths {
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, e error) error {
			if os.IsNotExist(e) {
				out[p] = "absent"
				return nil
			}
			if e != nil {
				return e
			}
			if d.Type()&os.ModeSymlink != 0 {
				v, e := os.Readlink(p)
				if e != nil {
					return e
				}
				out[p] = "symlink:" + v
				return nil
			}
			if !d.IsDir() {
				b, e := os.ReadFile(p)
				if e != nil {
					return e
				}
				out[p] = fmt.Sprintf("%x", sha256.Sum256(b))
			}
			return nil
		})
		must(err)
	}
	return out
}
func hostPaths() []string {
	return []string{"/Users/dev-r/.local/share/paseo-dock", "/Users/dev-r/.paseo/config.json", "/Users/dev-r/devsbx/paseo-dock/.worktrees/runtime-remove-fix/.generated"}
}
func decode(b []byte) any { var x any; must(json.Unmarshal(b, &x)); return x }
func sbxRows(b []byte) map[string]any {
	obj := decode(b).(map[string]any)
	out := map[string]any{}
	for _, r := range obj["sandboxes"].([]any) {
		m := r.(map[string]any)
		out[m["name"].(string)] = m
	}
	return out
}
func runtime(w string) string {
	sum := sha256.Sum256([]byte(w))
	return trial + "/runtime-base/" + fmt.Sprintf("%x", sum)[:10]
}
func reg(w string) config.Registry {
	var r config.Registry
	must(json.Unmarshal(read(runtime(w)+"/registry.json"), &r))
	return r
}
func saveReg(w string, r config.Registry) { writeJSON(runtime(w)+"/registry.json", r) }
func fixture(w, name, cid, image string) {
	e := config.Entry{Category: config.CategorySandbox, Agent: "codex", Workspace: w, Sandbox: name, Image: template}
	d := config.Entry{Category: config.CategoryContainer, Agent: "codex", Tool: "native", Workspace: w, ContainerID: cid, Image: imageTag, ImageID: image, Network: "bridge", SecurityPolicy: containerpolicy.ID, SecurityPolicyDigest: containerpolicy.SHA256}
	saveReg(w, config.Registry{Schema: 2, Workspace: w, Profiles: map[string]config.Entry{e.Key(): e, d.Key(): d}, Active: []string{e.Key(), d.Key()}})
	write(runtime(w)+"/agent-wrapper", []byte("trial obsolete launcher"))
}
func checkIsolation() {
	paths, e := filepath.Glob(trial + "/runtime-base/*/registry.json")
	must(e)
	assert(len(paths) <= 2, "unexpected registry count")
	for _, p := range paths {
		var r config.Registry
		must(json.Unmarshal(read(p), &r))
		assert(slices.Contains([]string{trial + "/workspace-remove", trial + "/workspace-prune"}, r.Workspace), "foreign registry")
		assert(p == runtime(r.Workspace)+"/registry.json", "registry outside runtime")
	}
}
func preserved(before map[string]any) {
	after := sbxRows(good("sbx", "ls", "--json"))
	for name, row := range before {
		assert(reflect.DeepEqual(row, after[name]), "existing sandbox changed: "+name)
	}
	writeJSON(evidence+"/sbx-after.json", map[string]any{"sandboxes": func() []any {
		r := []any{}
		for _, v := range after {
			r = append(r, v)
		}
		return r
	}()})
	assert(len(after) == len(before), "trial sandbox remains")
}
func prepare(checkout string) {
	_, err := os.Stat(trial + "/ownership.json")
	assert(os.IsNotExist(err), "trial root already used")
	must(os.MkdirAll(source, 0700))
	for _, name := range []string{"internal", "cmd", "go.mod", "go.sum", "versions.lock.json", "Dockerfile"} {
		origin := filepath.Join(checkout, name)
		info, e := os.Stat(origin)
		must(e)
		if !info.IsDir() {
			write(filepath.Join(source, name), read(origin))
			continue
		}
		must(filepath.WalkDir(origin, func(p string, d fs.DirEntry, e error) error {
			if e != nil {
				return e
			}
			rel, e := filepath.Rel(checkout, p)
			if e != nil {
				return e
			}
			if d.IsDir() {
				if d.Name() == "testdata" {
					return filepath.SkipDir
				}
				return os.MkdirAll(filepath.Join(source, rel), 0700)
			}
			if d.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("unexpected source symlink %s", p)
			}
			write(filepath.Join(source, rel), read(p))
			return nil
		}))
	}
	names := []string{"paseo-remove-live-20261013-single", "paseo-remove-live-20261013-prune"}
	writeJSON(trial+"/ownership.json", ownership{Sandboxes: names})
	original := source + "/internal/config/config.go"
	b := read(original)
	fset := token.NewFileSet()
	file, e := parser.ParseFile(fset, original, b, 0)
	must(e)
	found := false
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == "BaseDir" {
			begin, end := fset.Position(fn.Body.Pos()).Offset, fset.Position(fn.Body.End()).Offset
			b = append(append(append([]byte{}, b[:begin]...), []byte("{ return "+fmt.Sprintf("%q", trial+"/runtime-base")+", nil }")...), b[end:]...)
			found = true
			break
		}
	}
	assert(found, "BaseDir missing")
	replacement := trial + "/overlay/config.go"
	write(replacement, b)
	writeJSON(trial+"/overlay.json", map[string]any{"Replace": map[string]string{original: replacement}})
	write(evidence+"/overlay.json", read(trial+"/overlay.json"))
	out, code := run("diff", "-u", original, replacement)
	assert(code == 1, "overlay diff missing")
	write(evidence+"/overlay.diff", out)
	good("go", "build", "-overlay", trial+"/overlay.json", "-o", cliBinary, "./cmd/paseo-dock")
	good("go", "build", "-o", source+"/bin/agent-wrapper", "./cmd/agent-wrapper")
	must(os.MkdirAll(trial+"/guard", 0700))
	for _, name := range []string{"sbx", "docker", "paseo"} {
		must(os.Symlink(wrapper, trial+"/guard/"+name))
	}
	for _, p := range []string{trial + "/workspace-remove", trial + "/workspace-prune"} {
		must(os.MkdirAll(p, 0700))
		write(p+"/sentinel", []byte("preserve workspace\n"))
	}
	writeJSON(trial+"/paseo-home/config.json", map[string]any{"version": 1, "daemon": map[string]any{"listen": "127.0.0.1:0"}})
	write(trial+"/image/idle.go", []byte("package main\nimport (\"os\";\"os/signal\";\"syscall\")\nfunc main(){c:=make(chan os.Signal,1);signal.Notify(c,syscall.SIGTERM,syscall.SIGINT);<-c}\n"))
	finishPrepare()
}
func finishPrepare() {
	good("env", "GOOS=linux", "GOARCH=arm64", "CGO_ENABLED=0", "go", "build", "-o", trial+"/image/idle", trial+"/image/idle.go")
	write(trial+"/image/Dockerfile", []byte("FROM scratch\nCOPY idle /idle\nENTRYPOINT [\"/idle\"]\n"))
	p, e := containerpolicy.Profile()
	must(e)
	write(trial+"/policy.json", p)
}

func execute() {
	before := sbxRows(read(evidence + "/sbx-before.json"))
	assert(len(before) == 20, "expected 20 baseline sandboxes")
	o := own()
	for _, n := range o.Sandboxes {
		_, exists := before[n]
		assert(!exists, "sandbox name collision")
	}
	originalHashes := snapshot(hostPaths())
	writeJSON(evidence+"/host-hashes-before.json", originalHashes)
	current := sbxRows(good("sbx", "ls", "--json"))
	assert(reflect.DeepEqual(before, current), "baseline drift before mutation")
	assert(len(bytes.TrimSpace(good("docker", "ps", "-a", "--no-trunc", "--format", "{{json .}}"))) == 0, "Docker baseline drift")
	assert(len(bytes.TrimSpace(good("docker", "image", "ls", "--no-trunc", "--digests", "--format", "{{json .}}"))) == 0, "Docker images baseline drift")
	templateBefore := decode(read(evidence + "/templates-before.json"))
	assert(reflect.DeepEqual(templateBefore, decode(good("sbx", "template", "ls", "--json"))), "template baseline drift")
	good("docker", "build", "--pull=false", "--network", "none", "--tag", imageTag, trial+"/image")
	var images []struct {
		ID string `json:"Id"`
	}
	must(json.Unmarshal(good("docker", "image", "inspect", imageTag), &images))
	assert(len(images) == 1, "image missing")
	o.Image = images[0].ID
	writeJSON(trial+"/ownership.json", o)
	writeJSON(evidence+"/image-created.json", images)
	workspaces := []string{trial + "/workspace-remove", trial + "/workspace-prune"}
	for i, w := range workspaces {
		checkIsolation()
		good("sbx", "create", "--name", o.Sandboxes[i], "--pull", "never", "--skills", "off", "--cpus", "2", "--memory", "512m", "--template", template, "codex", w)
		cid := strings.TrimSpace(string(good("docker", "create", "--name", o.Sandboxes[i], "--label", "io.paseo-dock.managed=true", "--label", "io.paseo-dock.category=container", "--label", "io.paseo-dock.profile=container/codex-native", "--label", "io.paseo-dock.security-policy="+containerpolicy.ID, "--label", "io.paseo-dock.security-policy-digest="+containerpolicy.SHA256, "--network", "bridge", "--mount", "type=bind,src="+w+",dst="+w, "--workdir", w, "--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()), "--security-opt", "seccomp="+trial+"/policy.json", "--security-opt", "no-new-privileges", "--cap-drop", "SYS_ADMIN", o.Image)))
		assert(len(cid) == 64, "exact CID missing")
		o.Containers = append(o.Containers, cid)
		writeJSON(trial+"/ownership.json", o)
		fixture(w, o.Sandboxes[i], cid, o.Image)
		good("docker", "start", cid)
		good("sbx", "exec", o.Sandboxes[i], "/bin/true")
	}
	checkIsolation()
	writeJSON(evidence+"/isolation.json", map[string]any{"root": source, "runtime_base": trial + "/runtime-base", "launcher": trial + "/runtime-base/launcher/agent-wrapper", "paseo_home": trial + "/paseo-home", "registries": workspaces, "HOME": os.Getenv("HOME"), "overlay_only": "config.BaseDir body"})
	w := workspaces[0]
	p := trial + "/paseo-home"
	r := reg(w)
	registryBefore := read(runtime(w) + "/registry.json")
	paseoBefore := read(p + "/config.json")
	rejected("remove", "--workspace", w, "--profile", "sandbox/codex", "--paseo-home", p, "--yes")
	// This additional observation was added after the first live run; the report
	// distinguishes the executed first run from this future-run assertion.
	observed := good("sbx", "ls", "--json")
	write(evidence+"/running-after-sandbox-refusal.json", observed)
	assert(sbxRows(observed)[o.Sandboxes[0]].(map[string]any)["status"] == "running", "sandbox refusal stopped resource")
	assert(bytes.Equal(registryBefore, read(runtime(w)+"/registry.json")), "running sandbox refusal changed registry")
	assert(bytes.Equal(paseoBefore, read(p+"/config.json")), "running sandbox refusal changed providers")
	rejected("remove", "--workspace", w, "--profile", "container/codex-native", "--paseo-home", p, "--yes")
	containerObserved := good("docker", "inspect", "--type", "container", o.Containers[0])
	write(evidence+"/running-after-container-refusal.json", containerObserved)
	assert(decode(containerObserved).([]any)[0].(map[string]any)["State"].(map[string]any)["Running"] == true, "container refusal stopped resource")
	assert(bytes.Equal(registryBefore, read(runtime(w)+"/registry.json")), "running container refusal changed registry")
	assert(bytes.Equal(paseoBefore, read(p+"/config.json")), "running container refusal changed providers")
	cliGood("stop", "--workspace", w, "--profile", "sandbox/codex")
	otherBefore := read(runtime(workspaces[1]) + "/registry.json")
	cliGood("remove", "--workspace", w, "--profile", "sandbox/codex", "--paseo-home", p, "--yes")
	after := reg(w)
	assert(len(after.Profiles) == 1 && reflect.DeepEqual(r.Profiles["container/codex-native"], after.Profiles["container/codex-native"]), "other profile changed")
	assert(bytes.Equal(otherBefore, read(runtime(workspaces[1])+"/registry.json")), "other workspace registry changed")
	assert(bytes.Equal(read(w+"/sentinel"), []byte("preserve workspace\n")), "workspace changed")
	good("docker", "image", "inspect", o.Image)
	good("docker", "inspect", "--type", "container", o.Containers[0])
	cliGood("stop", "--workspace", w, "--profile", "container/codex-native")
	cliGood("remove", "--workspace", w, "--profile", "container/codex-native", "--paseo-home", p, "--yes")
	assert(len(reg(w).Profiles) == 0, "single registry profiles retained")
	assert(bytes.Equal(read(w+"/sentinel"), []byte("preserve workspace\n")), "workspace removed")
	good("docker", "image", "inspect", o.Image)
	w = workspaces[1]
	assert(within(w) && w == trial+"/workspace-prune", "unsafe workspace deletion")
	must(os.RemoveAll(w))
	checkIsolation()
	missingBefore := read(runtime(w) + "/registry.json")
	paseoBefore = read(p + "/config.json")
	preview := cliGood("prune", "--remove-runtimes", "--dry-run", "--paseo-home", p)
	assert(bytes.Contains(preview, []byte(w)) && !bytes.Contains(preview, []byte(workspaces[0])), "prune preview unexpected workspace")
	rejected("prune", "--remove-runtimes", "--paseo-home", p, "--yes")
	assert(bytes.Equal(missingBefore, read(runtime(w)+"/registry.json")), "blocked prune changed registry")
	assert(bytes.Equal(paseoBefore, read(p+"/config.json")), "blocked prune changed config")
	cliGood("stop", "--workspace", w, "--profile", "sandbox/codex")
	rejected("prune", "--remove-runtimes", "--paseo-home", p, "--yes")
	assert(bytes.Equal(missingBefore, read(runtime(w)+"/registry.json")), "mixed blocker changed registry")
	cliGood("stop", "--workspace", w, "--profile", "container/codex-native")
	cliGood("prune", "--remove-runtimes", "--dry-run", "--paseo-home", p)
	cliGood("prune", "--remove-runtimes", "--paseo-home", p, "--yes")
	_, err := os.Stat(runtime(w) + "/registry.json")
	assert(os.IsNotExist(err), "prune registry retained")
	good("docker", "image", "inspect", o.Image)
	write(evidence+"/image-preserved-before-cleanup.json", good("docker", "image", "inspect", o.Image))
	assert(len(bytes.TrimSpace(good("docker", "ps", "-a", "--no-trunc", "--format", "{{json .}}"))) == 0, "containers remain")
	preserved(before)
	templateAfter := good("sbx", "template", "ls", "--json")
	write(evidence+"/templates-after.json", templateAfter)
	assert(reflect.DeepEqual(templateBefore, decode(templateAfter)), "templates changed")
	good("paseo", "--home", p, "daemon", "stop")
	good("docker", "image", "rm", o.Image)
	write(evidence+"/docker-after.jsonl", good("docker", "ps", "-a", "--no-trunc", "--format", "{{json .}}"))
	imageAfter := good("docker", "image", "ls", "--no-trunc", "--digests", "--format", "{{json .}}")
	write(evidence+"/images-after.jsonl", imageAfter)
	assert(len(bytes.TrimSpace(imageAfter)) == 0, "trial image remains")
	afterHashes := snapshot(hostPaths())
	writeJSON(evidence+"/host-hashes-after.json", afterHashes)
	assert(reflect.DeepEqual(originalHashes, afterHashes), "original registry/launcher/config/generated changed")
	writeJSON(evidence+"/result.json", map[string]any{"passed": true, "existing_sandboxes_full_fields_compared": 20, "container_count_after": 0, "image_count_after": 0, "templates_unchanged": true, "host_hashes_unchanged": true, "HOME_unchanged": true, "limitation": "runtime base 경로만 검증용 치환; fixture는 직접 등록했으며 생성/relay 기능은 이번 검증 대상 아님"})
	fmt.Println("ALL LIVE ACCEPTANCE CHECKS PASSED")
}

// cleanup first preserves independently observed state, then removes only owned
// identities. It is also callable after a killed process: no success is inferred
// from a timeout or missing command response.
func cleanup() {
	o := own()
	sandboxes := good("sbx", "ls", "--json")
	containers := good("docker", "ps", "-a", "--no-trunc", "--format", "{{json .}}")
	write(evidence+"/cleanup-state-sbx.json", sandboxes)
	write(evidence+"/cleanup-state-docker.jsonl", containers)
	baseline := sbxRows(read(evidence + "/sbx-before.json"))
	rows := sbxRows(sandboxes)
	for i, name := range o.Sandboxes {
		assert(i < 2, "sandbox cap exceeded")
		if row, present := rows[name]; present {
			_, existed := baseline[name]
			assert(!existed, "cleanup would touch baseline sandbox")
			m := row.(map[string]any)
			workspace := trial + "/workspace-remove"
			if i == 1 {
				workspace = trial + "/workspace-prune"
			}
			assert(reflect.DeepEqual(m["workspaces"], []any{workspace}), "cleanup sandbox workspace mismatch")
			good("sbx", "stop", name)
			good("sbx", "rm", "--force", name)
		}
	}
	// Recover a create interrupted before CID enrollment only by the reserved
	// exact name, followed by image/labels/bind-mount checks before enrollment.
	for i, name := range o.Sandboxes {
		out, code := run("docker", "inspect", "--type", "container", name)
		if code != 0 {
			continue
		}
		m := decode(out).([]any)[0].(map[string]any)
		labels := m["Config"].(map[string]any)["Labels"].(map[string]any)
		assert(labels["io.paseo-dock.managed"] == "true" && labels["io.paseo-dock.category"] == "container" && labels["io.paseo-dock.profile"] == "container/codex-native", "cleanup container labels mismatch")
		workspace := trial + "/workspace-remove"
		if i == 1 {
			workspace = trial + "/workspace-prune"
		}
		mounts := m["Mounts"].([]any)
		assert(len(mounts) == 1, "cleanup container mounts mismatch")
		mount := mounts[0].(map[string]any)
		assert(mount["Source"] == workspace && mount["Destination"] == workspace && m["Image"] == o.Image && o.Image != "", "cleanup container identity mismatch")
		cid := m["Id"].(string)
		if !slices.Contains(o.Containers, cid) {
			assert(len(o.Containers) < 2, "container cap exceeded")
			o.Containers = append(o.Containers, cid)
			writeJSON(trial+"/ownership.json", o)
		}
		good("docker", "stop", cid)
		good("docker", "rm", cid)
	}
	status := good("paseo", "--home", trial+"/paseo-home", "status", "--json")
	if decode(status).(map[string]any)["localDaemon"] == "running" {
		good("paseo", "--home", trial+"/paseo-home", "daemon", "stop")
	}
	if o.Image != "" {
		_, code := run("docker", "image", "inspect", o.Image)
		if code == 0 {
			good("docker", "image", "rm", o.Image)
		}
	}
	status = good("paseo", "--home", trial+"/paseo-home", "status", "--json")
	write(evidence+"/daemon-final-status.json", status)
	assert(decode(status).(map[string]any)["localDaemon"] == "stopped", "trial daemon still running")
	preserved(baseline)
	write(evidence+"/docker-after.jsonl", good("docker", "ps", "-a", "--no-trunc", "--format", "{{json .}}"))
	assert(len(bytes.TrimSpace(read(evidence+"/docker-after.jsonl"))) == 0, "container cleanup incomplete")
	write(evidence+"/images-after.jsonl", good("docker", "image", "ls", "--no-trunc", "--digests", "--format", "{{json .}}"))
	assert(len(bytes.TrimSpace(read(evidence+"/images-after.jsonl"))) == 0, "image cleanup incomplete")
	after := good("sbx", "template", "ls", "--json")
	write(evidence+"/templates-after.json", after)
	assert(reflect.DeepEqual(decode(read(evidence+"/templates-before.json")), decode(after)), "cleanup template drift")
	hashes := snapshot(hostPaths())
	writeJSON(evidence+"/host-hashes-after.json", hashes)
	assert(reflect.DeepEqual(decode(read(evidence+"/host-hashes-before.json")), decode(func() []byte { b, e := json.Marshal(hashes); must(e); return b }())), "cleanup host hash drift")
}

func audit(checkout string) {
	manifest := map[string]string{}
	for _, root := range []string{"internal", "cmd", "go.mod", "go.sum", "versions.lock.json", "Dockerfile"} {
		must(filepath.WalkDir(source+"/"+root, func(p string, d fs.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if d.IsDir() {
				return nil
			}
			rel, e := filepath.Rel(source, p)
			if e != nil {
				return e
			}
			copied, original := read(p), read(filepath.Join(checkout, rel))
			assert(bytes.Equal(copied, original), "copied product source drift: "+rel)
			manifest[rel] = fmt.Sprintf("%x", sha256.Sum256(original))
			return nil
		}))
	}
	writeJSON(evidence+"/copied-source-hashes.json", manifest)
	out, code := run("git", "-C", checkout, "diff", "--", "internal/config/config.go", "internal/cli", "internal/operations/docker_backend.go", "internal/operations/remove.go", "internal/operations/prune_remove.go")
	assert(code == 0 && len(bytes.TrimSpace(out)) == 0, "original product diff nonempty")
	write(evidence+"/original-product.diff", out)
	cleanup()
}

func summarize() {
	counts := map[string]int{}
	top := map[string]int{}
	packages := map[string]bool{}
	for _, line := range bytes.Split(read(evidence+"/tests-final.jsonl"), []byte{'\n'}) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var event struct{ Action, Package, Test string }
		must(json.Unmarshal(line, &event))
		assert(event.Action != "fail", "test failure in final evidence")
		if event.Action == "pass" {
			if event.Test == "" {
				packages[event.Package] = true
			} else {
				counts[event.Package]++
				if !strings.Contains(event.Test, "/") {
					top[event.Package]++
				}
			}
		}
	}
	assert(len(packages) == 3, "incomplete final test packages")
	writeJSON(evidence+"/tests-summary.json", map[string]any{"packages_passed": packages, "passed_test_events_including_subtests": counts, "top_level_tests_passed": top, "failed": 0})
}

func main() {
	name := filepath.Base(os.Args[0])
	if slices.Contains([]string{"sbx", "docker", "paseo"}, name) {
		guarded(name, os.Args[1:])
		return
	}
	source = trial + "/source"
	cliBinary = trial + "/bin/paseo-dock"
	if b, e := os.ReadFile(evidence + "/commands.jsonl"); e == nil {
		sequence = bytes.Count(b, []byte{'\n'})
	}
	exe, e := os.Executable()
	must(e)
	wrapper = exe
	for _, tool := range []string{"sbx", "docker", "paseo"} {
		path, e := exec.LookPath(tool)
		must(e)
		must(os.Setenv("REMOVE_LIVE_REAL_"+strings.ToUpper(tool), path))
	}
	must(os.Setenv("REMOVE_LIVE_ORIGINAL_PATH", os.Getenv("PATH")))
	assert(len(os.Args) == 3, "usage: helper prepare|run CHECKOUT")
	switch os.Args[1] {
	case "prepare":
		prepare(os.Args[2])
	case "finish-prepare":
		finishPrepare()
	case "run":
		defer func() {
			if failure := recover(); failure != nil {
				writeJSON(evidence+"/failure.json", map[string]any{"failure": fmt.Sprint(failure), "last_command": sequence, "ownership": own()})
				func() {
					defer func() {
						if e := recover(); e != nil {
							fmt.Fprintln(os.Stderr, "cleanup incomplete:", e)
						}
					}()
					cleanup()
				}()
				fmt.Fprintln(os.Stderr, failure)
				os.Exit(1)
			}
		}()
		execute()
	case "cleanup":
		cleanup()
	case "audit":
		audit(os.Args[2])
	case "summarize":
		summarize()
	default:
		panic("unknown phase")
	}
}
