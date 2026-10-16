// Disposable category acceptance helper: go build -o /private/tmp/category-verify
// internal/tui/testdata/management-tui-live/category-verify.go; category-verify --run.
// Resources are preserved. Proxies reject mutations outside the isolated registry.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
	"paseo-dock/internal/config"
	"paseo-dock/internal/launcher"
	"paseo-dock/internal/operations"
	"paseo-dock/internal/process"
)

const namespace = "paseo-dock-category-live-20261007"

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "BLOCKER:", err)
		os.Exit(1)
	}
}
func environment(values map[string]string) []string {
	out := []string{}
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if _, ok := values[key]; !ok {
			out = append(out, value)
		}
	}
	for key, value := range values {
		out = append(out, key+"="+value)
	}
	return out
}
func flag(args []string, key string) string {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == key {
			return args[i+1]
		}
	}
	return ""
}

type initializeOutput struct {
	writer      io.Writer
	stdin       *io.PipeWriter
	buffer      bytes.Buffer
	once        sync.Once
	initialized bool
	failure     error
}

func (o *initializeOutput) Write(data []byte) (int, error) {
	n, err := o.writer.Write(data)
	o.buffer.Write(data[:n])
	for {
		end := bytes.IndexByte(o.buffer.Bytes(), '\n')
		if end < 0 {
			break
		}
		line := bytes.TrimSpace(o.buffer.Next(end + 1))
		if len(line) == 0 {
			continue
		}
		var response map[string]json.RawMessage
		if decodeErr := json.Unmarshal(line, &response); decodeErr != nil {
			o.failure = fmt.Errorf("invalid initialize response frame: %w", decodeErr)
		} else {
			var id float64
			if json.Unmarshal(response["id"], &id) != nil || id != 1 {
				continue
			}
			result, hasResult := response["result"]
			_, hasError := response["error"]
			if hasError || !hasResult || bytes.Equal(bytes.TrimSpace(result), []byte("null")) {
				o.failure = fmt.Errorf("initialize ID 1 did not return a successful result")
			} else {
				o.initialized = true
			}
		}
		if o.stdin != nil {
			o.once.Do(func() { o.stdin.Close() })
		}
	}
	return n, err
}

func (o *initializeOutput) resultError() error {
	if o.failure != nil {
		return o.failure
	}
	if len(bytes.TrimSpace(o.buffer.Bytes())) != 0 {
		return fmt.Errorf("incomplete initialize response frame")
	}
	if !o.initialized {
		return fmt.Errorf("initialize response absent")
	}
	return nil
}

func main() {
	if len(os.Args) == 4 && os.Args[1] == "--provider-session" {
		reader, writer := io.Pipe()
		go func() {
			fmt.Fprintln(writer, "{\"id\":1,\"method\":\"initialize\",\"params\":{\"clientInfo\":{\"name\":\"category-live\",\"version\":\"1\"}}}")
		}()
		output := &initializeOutput{writer: os.Stdout, stdin: writer}
		code, err := launcher.LaunchIO(os.Args[2], os.Args[3], []string{"app-server"}, reader, output, os.Stderr)
		reader.Close()
		if err == nil {
			err = output.resultError()
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			if code == 0 {
				code = 1
			}
		}
		os.Exit(code)
	}
	if slices.Contains(os.Args[1:], "--guest-port-server") {
		must(http.ListenAndServe("0.0.0.0:18765", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "CATEGORY_PORT_OK") })))
		return
	}
	if slices.Contains([]string{"sbx", "docker", "paseo"}, filepath.Base(os.Args[0])) {
		proxy()
		return
	}
	if os.Getenv("CATEGORY_LIVE_CHILD") == "1" {
		child()
		return
	}
	root, err := os.Getwd()
	must(err)
	temp := flag(os.Args[1:], "--resume")
	if resume := flag(os.Args[1:], "--resume"); resume != "" {
		if filepath.Dir(resume) != "/private/tmp" || !strings.HasPrefix(filepath.Base(resume), "paseo-dock-category-live-") {
			must(fmt.Errorf("invalid resume root"))
		}
		info, e := os.Stat(resume)
		must(e)
		if !info.IsDir() {
			must(fmt.Errorf("resume root is not a directory"))
		}
		for _, name := range []string{"home", "workspace", "paseo", "trusted", "commands"} {
			info, e := os.Stat(filepath.Join(resume, name))
			must(e)
			if !info.IsDir() {
				must(fmt.Errorf("missing resume directory %s", name))
			}
		}
	} else {
		temp, err = os.MkdirTemp("/private/tmp", "paseo-dock-category-live-")
		must(err)
	}
	trusted := filepath.Join(temp, "trusted")
	for _, path := range []string{"home", "workspace", "paseo", "commands", "trusted/bin", "trusted/.generated/images"} {
		must(os.MkdirAll(filepath.Join(temp, path), 0700))
	}
	probe := exec.Command("go", "build", "-o", filepath.Join(temp, "workspace", "category-port-probe"), "./internal/tui/testdata/management-tui-live/category-verify.go")
	probe.Env = environment(map[string]string{"GOOS": "linux", "GOARCH": "arm64", "CGO_ENABLED": "0"})
	probe.Stdout, probe.Stderr = os.Stdout, os.Stderr
	must(probe.Run())
	for _, path := range []string{"versions.lock.json", "Dockerfile", "Dockerfile.sandbox", "bin/agent-wrapper", "bin/install-runtime", ".generated/images/sandbox-codex.json", ".generated/images/sandbox-claude.json"} {
		data, e := os.ReadFile(filepath.Join(root, path))
		must(e)
		must(os.WriteFile(filepath.Join(trusted, path), data, 0700))
	}
	exe, err := os.Executable()
	must(err)
	values := map[string]string{"HOME": filepath.Join(temp, "home"), "PATH": filepath.Join(temp, "commands") + ":" + os.Getenv("PATH"), "PASEO_HOME": filepath.Join(temp, "paseo"), "CATEGORY_LIVE_CHILD": "1", "CATEGORY_LIVE_ROOT": temp, "CATEGORY_AUTH_HOME": os.Getenv("HOME"), "CATEGORY_AUTH_PATH": os.Getenv("PATH"), "CATEGORY_LIVE_RUN": fmt.Sprint(slices.Contains(os.Args[1:], "--run"))}
	values["CATEGORY_PENDING_ONLY"] = fmt.Sprint(slices.Contains(os.Args[1:], "--pending-only"))
	values["CATEGORY_PENDING_AGENT"] = flag(os.Args[1:], "--pending-agent")
	values["CATEGORY_PROVIDERS_ONLY"] = fmt.Sprint(slices.Contains(os.Args[1:], "--providers-only"))
	values["CATEGORY_SANDBOX_PROBE"] = fmt.Sprint(slices.Contains(os.Args[1:], "--sandbox-probe"))
	values["CATEGORY_DIAGNOSTICS"] = fmt.Sprint(slices.Contains(os.Args[1:], "--diagnostics"))
	for _, name := range []string{"sbx", "docker", "paseo"} {
		path, e := exec.LookPath(name)
		must(e)
		values["CATEGORY_REAL_"+strings.ToUpper(name)] = path
		link := filepath.Join(temp, "commands", name)
		if destination, e := os.Readlink(link); e == nil {
			if destination != exe {
				must(fmt.Errorf("unexpected proxy destination"))
			}
		} else {
			must(os.Symlink(exe, link))
		}
	}
	command := exec.Command(exe)
	command.Env = environment(values)
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	must(command.Run())
}
func registry() (config.Registry, string) {
	runtime, e := config.RuntimePath(filepath.Join(os.Getenv("CATEGORY_LIVE_ROOT"), "workspace"))
	must(e)
	var r config.Registry
	e = config.ReadJSON(filepath.Join(runtime, "registry.json"), &r)
	if e != nil && !os.IsNotExist(e) {
		must(e)
	}
	return r, runtime
}
func proxy() {
	name := filepath.Base(os.Args[0])
	args := os.Args[1:]
	root := os.Getenv("CATEGORY_LIVE_ROOT")
	workspace := filepath.Join(root, "workspace")
	reg, runtime := registry()
	ownedSBX, ownedDocker := []string{}, []string{}
	for _, e := range reg.Profiles {
		if e.EffectiveCategory() == config.CategoryContainer {
			ownedDocker = append(ownedDocker, e.ContainerID)
		} else {
			ownedSBX = append(ownedSBX, e.Sandbox)
		}
	}
	if pending, err := os.ReadFile(filepath.Join(root, "created-cid")); err == nil {
		ownedDocker = append(ownedDocker, strings.TrimSpace(string(pending)))
	}
	allowed := false
	if name == "paseo" {
		allowed = len(args) >= 3 && args[0] == "--home" && args[1] == filepath.Join(root, "paseo") && slices.Contains([]string{"status", "start", "reload"}, args[2])
	}
	if name == "sbx" && len(args) > 0 {
		switch args[0] {
		case "ls":
			allowed = slices.Equal(args, []string{"ls", "--json"})
		case "create":
			expected := []string{"paseo-sandbox-codex-" + filepath.Base(runtime), "paseo-sandbox-claude-" + filepath.Base(runtime)}
			allowed = slices.Contains(expected, flag(args, "--name")) && args[len(args)-1] == workspace && strings.HasPrefix(flag(args, "--template"), namespace+"/sandbox/") && flag(args, "--skills") == "off"
		case "exec":
			allowed = ownedExecTarget(args[1:], ownedSBX, false)
		case "stop":
			allowed = len(args) == 2 && ownedTarget(args[1], ownedSBX)
		case "policy":
			allowed = (len(args) == 6 && args[1] == "allow" && args[2] == "network" && args[3] == "--sandbox" && ownedTarget(args[4], ownedSBX) && validPolicyEndpoint(args[5])) || (len(args) == 8 && args[1] == "rm" && args[2] == "network" && args[3] == "--sandbox" && ownedTarget(args[4], ownedSBX) && args[5] == "--resource" && validPolicyEndpoint(args[6]) && args[7] == "--force")
		}
	}
	if name == "docker" && len(args) > 0 {
		switch args[0] {
		case "info":
			allowed = len(args) == 3 && args[1] == "--format"
		case "ps":
			allowed = slices.Contains(args, "label=io.paseo-dock.managed=true")
		case "image":
			allowed = len(args) >= 3 && args[1] == "inspect" && strings.HasPrefix(args[len(args)-1], namespace+"/container/")
		case "run":
			allowed = slices.Contains(args, "sha256:7760a63ba6a62611c4434c1208c1c87236cd37d36b691da206ebd0d7e77d6521") && slices.Contains(args, "type=bind,src="+workspace+",dst="+workspace) && flag(args, "--label") == "io.paseo-dock.managed=true" && flag(args, "--network") == "bridge"
		case "inspect":
			for _, id := range ownedDocker {
				if id != "" && args[len(args)-1] == id {
					allowed = true
				}
			}
			// Inspection is read-only; dashboard inventories may include other managed IDs.
			allowed = len(args) == 4 && args[1] == "--type" && args[2] == "container"
		case "exec":
			allowed = ownedExecTarget(args[1:], ownedDocker, true)
		case "start", "stop":
			allowed = len(args) > 1
			for _, target := range args[1:] {
				if !ownedTarget(target, ownedDocker) {
					allowed = false
				}
			}
		}
	}
	if !allowed {
		must(fmt.Errorf("isolation guard rejected %s verb=%s", name, strings.Join(args[:min(2, len(args))], " ")))
	}
	values := map[string]string{"HOME": os.Getenv("CATEGORY_AUTH_HOME"), "PATH": os.Getenv("CATEGORY_AUTH_PATH")}
	if name == "paseo" {
		values["HOME"] = os.Getenv("HOME")
	}
	c := exec.Command(os.Getenv("CATEGORY_REAL_"+strings.ToUpper(name)), args...)
	c.Env = environment(values)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	var output bytes.Buffer
	if name == "docker" && args[0] == "run" {
		c.Stdout = &output
	}
	err := c.Run()
	if name == "docker" && args[0] == "run" {
		os.Stdout.Write(output.Bytes())
		cid, readErr := os.ReadFile(flag(args, "--cidfile"))
		if readErr == nil {
			must(os.WriteFile(filepath.Join(root, "created-cid"), cid, 0600))
		}
	}
	code := 0
	if e, ok := err.(*exec.ExitError); ok {
		code = e.ExitCode()
	} else if err != nil {
		code = 1
	}
	fmt.Fprintf(os.Stderr, "EXTERNAL %s %s exit=%d\n", name, args[0], code)
	os.Exit(code)
}

func ownedTarget(target string, owned []string) bool {
	return target != "" && !strings.HasPrefix(target, "-") && slices.Contains(owned, target)
}

func ownedExecTarget(args, owned []string, docker bool) bool {
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		option := args[0]
		args = args[1:]
		if option == "--" {
			break
		}
		switch option {
		case "-i", "-t", "-it", "--interactive", "--tty":
		case "--env", "--workdir", "--user":
			if option == "--user" && !docker {
				return false
			}
			if len(args) == 0 || args[0] == "" || strings.HasPrefix(args[0], "-") {
				return false
			}
			args = args[1:]
		default:
			return false
		}
	}
	return len(args) >= 2 && ownedTarget(args[0], owned)
}

func validPolicyEndpoint(endpoint string) bool {
	host, port, err := net.SplitHostPort(endpoint)
	if err != nil || host != "localhost" {
		return false
	}
	value, err := strconv.Atoi(port)
	return err == nil && value > 0 && value <= 65535
}
func step(label string, err error) {
	fmt.Printf("STEP %s success=%t error=%v\n", label, err == nil, err)
}
func child() {
	root := os.Getenv("CATEGORY_LIVE_ROOT")
	workspace := filepath.Join(root, "workspace")
	trusted := filepath.Join(root, "trusted")
	paseoHome := filepath.Join(root, "paseo")
	base, err := config.BaseDir()
	must(err)
	for _, path := range []string{base, config.LauncherPath(base), trusted, paseoHome} {
		if !config.Within(path, root) {
			must(fmt.Errorf("unsafe isolated path %s", path))
		}
	}
	fmt.Printf("ISOLATION root=%s workspace=%s trusted=%s base=%s paseo=%s\n", root, workspace, trusted, base, paseoHome)
	if _, e := os.Stat(filepath.Join(paseoHome, "config.json")); os.IsNotExist(e) {
		must(config.WriteJSON(filepath.Join(paseoHome, "config.json"), map[string]any{"version": 1, "daemon": map[string]any{"listen": "127.0.0.1:0"}}))
	}
	if os.Getenv("CATEGORY_LIVE_RUN") != "true" {
		fmt.Println("PREFLIGHT ONLY")
		return
	}
	service, err := operations.New(trusted)
	must(err)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	if os.Getenv("CATEGORY_DIAGNOSTICS") == "true" {
		diagnostics(ctx, trusted)
		return
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	must(err)
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	prior, _ := registry()
	if entry, ok := prior.Profiles["container/codex-superpowers"]; ok && len(entry.Ports) == 1 {
		port = entry.Ports[0].HostPort
	}
	selections := []config.Selection{{Schema: 2, Category: config.CategorySandbox, Agent: "codex"}, {Schema: 2, Category: config.CategorySandbox, Agent: "claude"}, {Schema: 2, Category: config.CategoryContainer, Pairs: []config.Pair{{Agent: "codex", Methodology: "superpowers"}}, Utilities: []string{"rtk"}}}
	for _, options := range selections {
		pendingAgent := os.Getenv("CATEGORY_PENDING_AGENT")
		if pendingAgent == "" {
			pendingAgent = "claude"
		}
		if os.Getenv("CATEGORY_PENDING_ONLY") == "true" && options.Agent != pendingAgent {
			continue
		}
		selection, e := service.Catalog.Normalize(options)
		must(e)
		request := operations.Request{Kind: operations.KindCreate, Target: operations.Target{Workspace: workspace}, Selection: selection, Namespace: namespace, Memory: "2g", PaseoHome: paseoHome}
		if selection.Category == config.CategoryContainer {
			request.Ports = []config.PortMapping{{HostAddress: "127.0.0.1", HostPort: port, ContainerPort: 18765}}
		}
		result, e := service.Execute(ctx, request, nil)
		step("create "+string(selection.Category)+" "+options.Agent, resultError(result, e))
	}
	reg, runtime := registry()
	keys := slices.Collect(func(yield func(string) bool) {
		for k := range reg.Profiles {
			if !yield(k) {
				return
			}
		}
	})
	slices.Sort(keys)
	for _, key := range keys {
		entry := reg.Profiles[key]
		if os.Getenv("CATEGORY_PROVIDERS_ONLY") == "true" {
			continue
		}
		if os.Getenv("CATEGORY_PENDING_ONLY") == "true" && entry.Agent != "claude" {
			continue
		}
		target := operations.Target{Category: entry.EffectiveCategory(), Workspace: workspace, Runtime: runtime, Profile: key, Entry: entry}
		var backend operations.Backend = &operations.SBXBackend{Root: trusted}
		if entry.Category == config.CategoryContainer {
			backend = &operations.DockerBackend{Root: trusted}
		}
		state, e := backend.Inspect(ctx, entry)
		fmt.Printf("IDENTITY %s state=%+v error=%v\n", key, state, e)
		if e != nil {
			continue
		}
		inspect := "set -e; id; " + entry.Agent + " --version; cat /home/agent/.local/share/paseo-dock/agent.json; cat /home/agent/.local/share/paseo-dock/installed.json"
		if entry.Category == config.CategorySandbox {
			inspect += "; for x in rtk headroom openspec; do if command -v $x; then exit 21; fi; done; for x in /home/agent/.claude/plugins/cache/superpowers /home/agent/.claude/plugins/marketplaces/superpowers /home/agent/.agents/skills/superpowers /home/agent/.codex/superpowers /opt/paseo-dock/runtime/lib/node_modules/@fission-ai/openspec; do test ! -e $x || exit 22; done; echo AGENT_ONLY_CHECK_OK"
			if entry.Agent == "claude" {
				inspect += "; stat -c '%u:%g %n' /home/agent/.claude; test $(stat -c %u /home/agent/.claude) = $(id -u); test -w /home/agent/.claude/settings.json; echo CLAUDE_KIT_CONFIG_WRITABLE"
			}
		} else {
			inspect += "; rtk --version; test -d /home/agent/.agents/skills/superpowers; echo PINNED_STACK_CHECK_OK"
		}
		step("contents "+key, backend.Exec(ctx, entry, []string{"bash", "-lc", inspect}, nil, os.Stdout, os.Stderr))
		result, e := service.Execute(ctx, operations.Request{Kind: operations.KindVersions, Target: target}, nil)
		fmt.Printf("VERSIONS %s %+v\n", key, result)
		step("versions "+key, e)
		if entry.Category == config.CategoryContainer {
			dockerChecks(ctx, backend, entry, port)
		}
		step("shell "+key, shell(ctx, service, target))
		updateCtx, stopUpdate := context.WithTimeout(ctx, 4*time.Minute)
		result, e = service.Execute(updateCtx, operations.Request{Kind: operations.KindUpdate, Target: target}, nil)
		stopUpdate()
		step("update "+key, resultError(result, e))
	}
	snapshot, e := service.Snapshot(ctx, operations.Query{IncludeDocker: true})
	step("snapshot", e)
	for _, w := range snapshot.Workspaces {
		for _, s := range w.Sandboxes {
			fmt.Printf("ROW %s category=%s state=%s active=%t\n", s.Target.Profile, s.Target.Category, s.RuntimeState, s.Active)
		}
	}
	for _, key := range keys {
		entry := reg.Profiles[key]
		if entry.Category != config.CategoryContainer {
			continue
		}
		_, inspectErr := (&operations.DockerBackend{Root: trusted}).Inspect(ctx, entry)
		must(inspectErr)
		c := exec.CommandContext(ctx, "docker", "start", entry.ContainerID)
		c.Stdout, c.Stderr = os.Stdout, os.Stderr
		must(c.Run())
	}
	result, e := service.Execute(ctx, operations.Request{Kind: operations.KindRegister, Target: operations.Target{Workspace: workspace}, PaseoHome: paseoHome, Apply: true}, nil)
	step("register mixed relay", resultError(result, e))
	// A failed relay must leave the isolated config without a container provider.
	configData, readErr := os.ReadFile(filepath.Join(paseoHome, "config.json"))
	must(readErr)
	fmt.Printf("CONFIG container_provider_present=%t registration_success=%t\n", bytes.Contains(configData, []byte("container-codex-superpowers")), e == nil)
	for _, key := range keys {
		entry := reg.Profiles[key]
		if !slices.Contains(reg.ActiveProfiles(), key) {
			continue
		}
		id := operations.ProviderID(key, entry)
		if entry.Category == config.CategoryContainer {
			// Resume provider verification of the preserved, stopped exact resource.
			_, inspectErr := (&operations.DockerBackend{Root: trusted}).Inspect(ctx, entry)
			must(inspectErr)
			c := exec.CommandContext(ctx, "docker", "start", entry.ContainerID)
			c.Stdout, c.Stderr = os.Stdout, os.Stderr
			must(c.Run())
			state, inspectErr := (&operations.DockerBackend{Root: trusted}).Inspect(ctx, entry)
			must(inspectErr)
			if !state.Running {
				must(fmt.Errorf("Docker provider resource did not start"))
			}
			if os.Getenv("CATEGORY_SANDBOX_PROBE") == "true" {
				backend := &operations.DockerBackend{Root: trusted}
				step("Docker Codex sandbox help", backend.Exec(ctx, entry, []string{"codex", "sandbox", "--help"}, nil, os.Stdout, os.Stderr))
				step("Docker Codex workspace-write sandbox command", backend.Exec(ctx, entry, []string{"codex", "sandbox", "-c", "sandbox_mode=\"workspace-write\"", "--", "bash", "-lc", "printf CATEGORY_CODEX_SANDBOX_OK > category-codex-sandbox; cat category-codex-sandbox"}, nil, os.Stdout, os.Stderr))
			}
		}
		code, err := launcher.LaunchIO(runtime, id, []string{"--version"}, nil, os.Stdout, os.Stderr)
		if err == nil && code != 0 {
			err = fmt.Errorf("provider exit %d", code)
		}
		step("provider version route "+id, err)
		if entry.Agent == "codex" {
			exe, err := os.Executable()
			must(err)
			sessionCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			cmd := process.Command(sessionCtx, exe, "--provider-session", runtime, id)
			cmd.Dir = workspace
			var transcript bytes.Buffer
			cmd.Stdout = &transcript
			cmd.Stderr = os.Stderr
			err = cmd.Run()
			cancel()
			fmt.Printf("PROTOCOL %s %s\n", id, transcript.String())
			if err == nil {
				output := &initializeOutput{writer: io.Discard}
				output.Write(transcript.Bytes())
				err = output.resultError()
			}
			step("provider initialize and EOF termination "+id, err)
		}
	}
	for _, key := range keys {
		entry := reg.Profiles[key]
		target := operations.Target{Category: entry.EffectiveCategory(), Workspace: workspace, Runtime: runtime, Profile: key, Entry: entry}
		stateErr := error(nil)
		var b operations.Backend = &operations.SBXBackend{Root: trusted}
		if entry.Category == config.CategoryContainer {
			b = &operations.DockerBackend{Root: trusted}
		}
		_, stateErr = b.Inspect(ctx, entry)
		if stateErr != nil {
			step("stop preflight "+key, stateErr)
			continue
		}
		result, e := service.Execute(ctx, operations.Request{Kind: operations.KindStop, Target: target}, nil)
		step("stop "+key, resultError(result, e))
		state, e := b.Inspect(ctx, entry)
		fmt.Printf("PRESERVED %s sandbox=%s cid=%s running=%t known=%t error=%v\n", key, entry.Sandbox, entry.ContainerID, state.Running, state.Known, e)
	}
	fmt.Printf("PRESERVED root=%s runtime=%s paseo=%s\n", root, runtime, paseoHome)
}

func diagnostics(ctx context.Context, trusted string) {
	reg, _ := registry()
	entry := reg.Profiles["container/codex-superpowers"]
	docker := &operations.DockerBackend{Root: trusted}
	_, err := docker.Inspect(ctx, entry)
	must(err)
	c := exec.CommandContext(ctx, "docker", "start", entry.ContainerID)
	c.Stdout, c.Stderr = os.Stdout, os.Stderr
	must(c.Run())
	state, err := docker.Inspect(ctx, entry)
	must(err)
	if !state.Running {
		must(fmt.Errorf("diagnostic resource stopped"))
	}
	for _, args := range [][]string{
		{"sh", "-c", "cat /proc/sys/user/max_user_namespaces; sed -n '/^Seccomp/p' /proc/self/status; command -v bwrap || true; dpkg-query -W bubblewrap 2>/dev/null || true; uname -r"},
		{"codex", "--help"},
		{"codex", "features", "list"},
		{"codex", "sandbox", "--help"},
		{"codex", "sandbox", "-c", "sandbox_mode=\"workspace-write\"", "--", "bash", "-lc", "printf CATEGORY_CODEX_SANDBOX_OK > category-codex-sandbox; cat category-codex-sandbox"},
		{"codex", "sandbox", "--enable", "use_legacy_landlock", "-c", "sandbox_mode=\"workspace-write\"", "--", "bash", "-lc", "printf CATEGORY_COMPAT_OK > category-codex-compat; cat category-codex-compat; if printf forbidden > /home/agent/category-outside-workspace; then echo OUTSIDE_WRITE_ALLOWED; exit 41; else echo OUTSIDE_WRITE_DENIED; fi"},
	} {
		fmt.Printf("DIAGNOSTIC command=%q\n", args)
		step("Docker installed Codex diagnostic", docker.Exec(ctx, entry, args, nil, os.Stdout, os.Stderr))
	}
	step("Docker diagnostic stop", docker.Stop(ctx, entry))
	entry = reg.Profiles["sandbox/codex"]
	sbx := &operations.SBXBackend{Root: trusted}
	_, err = sbx.Inspect(ctx, entry)
	must(err)
	data, err := exec.CommandContext(ctx, "sbx", "ls", "--json").Output()
	must(err)
	var inventory any
	must(json.Unmarshal(data, &inventory))
	var selected func(any)
	selected = func(value any) {
		switch v := value.(type) {
		case []any:
			for _, child := range v {
				selected(child)
			}
		case map[string]any:
			if v["name"] == entry.Sandbox {
				data, _ := json.Marshal(v)
				fmt.Printf("SBX_EXACT_METADATA %s\n", data)
				return
			}
			for _, child := range v {
				selected(child)
			}
		}
	}
	selected(inventory)
	step("SBX direct PATH", sbx.Exec(ctx, entry, []string{"printenv", "PATH"}, nil, os.Stdout, os.Stderr))
	step("SBX direct Codex version", sbx.Exec(ctx, entry, []string{"codex", "--version"}, nil, os.Stdout, os.Stderr))
	step("SBX exact installed version paths", sbx.Exec(ctx, entry, []string{"bash", "-lc", "printf 'PATH=%s\\n' \"$PATH\"; type -a codex; codex --version; /home/agent/.local/bin/codex --version; /opt/paseo-dock/runtime/bin/codex --version; cat /home/agent/.local/share/paseo-dock/agent.json; ls -l /home/agent/.local/bin/codex"}, nil, os.Stdout, os.Stderr))
	step("SBX diagnostic stop", sbx.Stop(ctx, entry))
}
func resultError(r operations.Result, e error) error {
	fmt.Printf("RESULT changed=%t uncertain=%t remaining=%d\n", r.Changed, r.NeedsVerification, len(r.Remaining))
	return e
}
func shell(ctx context.Context, s *operations.Service, t operations.Target) error {
	command, e := s.ShellCommand(ctx, t)
	if e != nil {
		return e
	}
	master, slave, e := pty.Open()
	if e != nil {
		return e
	}
	defer master.Close()
	defer slave.Close()
	command.SetStdin(slave)
	command.SetStdout(slave)
	command.SetStderr(slave)
	done := make(chan error, 1)
	go func() { e := command.Run(); slave.Close(); done <- e }()
	master.Write([]byte("printf 'CATEGORY_LIVE_SHELL_OK\\n'; exit\n"))
	var transcript bytes.Buffer
	io.Copy(&transcript, master)
	fmt.Print(transcript.String())
	e = <-done
	if e == nil && !strings.Contains(transcript.String(), "CATEGORY_LIVE_SHELL_OK\r\n") {
		return fmt.Errorf("shell output marker absent")
	}
	return e
}
func dockerChecks(ctx context.Context, b operations.Backend, e config.Entry, port int) {
	checks := "set -e; test $(id -u) -ne 0; getent hosts registry.npmjs.org; curl --fail --max-time 20 -I https://registry.npmjs.org/; printf CATEGORY_WORKSPACE_RW > category-rw; printf CATEGORY_HOME_PERSIST > /home/agent/category-home; stat -c '%u:%g %n' category-rw /home/agent/category-home; nohup ./category-port-probe --guest-port-server >/tmp/category-port.log 2>&1 & echo $! > /tmp/category-port.pid"
	step("docker DNS HTTPS RW nonroot port server", b.Exec(ctx, e, []string{"bash", "-lc", checks}, nil, os.Stdout, os.Stderr))
	client := http.Client{Timeout: 10 * time.Second}
	var err error
	for i := 0; i < 20; i++ {
		var response *http.Response
		response, err = client.Get(fmt.Sprintf("http://127.0.0.1:%d", port))
		if err == nil {
			data, readErr := io.ReadAll(response.Body)
			response.Body.Close()
			err = readErr
			if string(data) != "CATEGORY_PORT_OK" {
				err = fmt.Errorf("unexpected explicit port response")
			}
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	step("docker explicit loopback HTTP", err)
	step("docker stop before restart", b.Stop(ctx, e))
	_, runtime := registry()
	var current config.Registry
	must(config.ReadJSON(filepath.Join(runtime, "registry.json"), &current))
	must(current.ValidateIdentity(e.Key(), e))
	c := exec.CommandContext(ctx, "docker", "start", e.ContainerID)
	c.Stdout, c.Stderr = os.Stdout, os.Stderr
	step("docker exact CID restart", c.Run())
	step("docker home persistence same CID", b.Exec(ctx, e, []string{"bash", "-lc", "test $(cat /home/agent/category-home) = CATEGORY_HOME_PERSIST; test $(cat category-rw) = CATEGORY_WORKSPACE_RW; echo CATEGORY_RESTART_PERSIST_OK"}, nil, os.Stdout, os.Stderr))
}
