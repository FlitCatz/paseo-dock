// Package launcher transports Paseo agent protocols into a trusted sandbox.
package launcher

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"paseo-sbx/internal/config"
	"paseo-sbx/internal/gateway"
	"paseo-sbx/internal/operationlock"
	"paseo-sbx/internal/operations"
	"paseo-sbx/internal/process"
	"paseo-sbx/internal/sandboxupdate"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// prepareNative copies only plugin-prepared assets into an ephemeral guest
// directory. Authentication directories are never mounted or copied.
func prepareNative(args []string, workspace, sandbox string, stderr io.Writer) ([]string, map[string]string, func(), error) {
	return prepareNativeContext(context.Background(), "", "", args, workspace, config.Entry{Workspace: workspace, Sandbox: sandbox}, stderr)
}

type nativeAction uint8

const (
	nativeExec nativeAction = iota
	nativeCopy
	nativeExtract
)

func nativeCommand(ctx context.Context, entry config.Entry, action nativeAction, args ...string) (*exec.Cmd, error) {
	if entry.EffectiveCategory() != config.CategoryContainer {
		if action == nativeExtract {
			return nil, errors.New("native extraction requires Docker")
		}
		if action != nativeCopy {
			args = append([]string{"exec", entry.Sandbox}, args...)
		}
		return process.Command(ctx, "sbx", args...), nil
	}
	state, err := (&operations.DockerBackend{}).Inspect(ctx, entry)
	if err != nil {
		return nil, err
	}
	if !state.Running {
		return nil, errors.New("Docker container is stopped; start it explicitly before native assets")
	}
	if action == nativeCopy {
		return nil, errors.New("native Docker assets require agent extraction")
	}
	if action == nativeExtract {
		if len(args) != 1 || !validNativeGuestDirectory(args[0]) {
			return nil, errors.New("invalid native extraction directory")
		}
		return process.Command(ctx, "docker", "exec", "-i", "--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()), "--env", "HOME=/home/agent", entry.ContainerID, "/usr/bin/tar", "-xpf", "-", "--no-same-owner", "-C", args[0]), nil
	}
	command := []string{"exec", "--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()), "--env", "HOME=/home/agent", "--workdir", entry.Workspace, entry.ContainerID}
	return process.Command(ctx, "docker", append(command, args...)...), nil
}

func prepareNativeContext(ctx context.Context, runtime, key string, args []string, workspace string, entry config.Entry, stderr io.Writer) ([]string, map[string]string, func(), error) {
	// Legacy prepareNative has no registry parameter; production always supplies
	// runtime/key. A Docker mutation requires the frozen trusted target.
	if entry.EffectiveCategory() == config.CategoryContainer && runtime == "" {
		return nil, nil, nil, errors.New("native Docker assets require a trusted registry target")
	}
	runInput := func(ctx context.Context, action nativeAction, capture bool, input io.Reader, args ...string) ([]byte, error) {
		if runtime != "" {
			release, err := operationlock.Acquire(ctx, runtime, []string{operationlock.EntryKey(entry)})
			if err != nil {
				return nil, err
			}
			defer release()
			if err := validateEntry(runtime, key, entry); err != nil {
				return nil, err
			}
		}
		cmd, err := nativeCommand(ctx, entry, action, args...)
		if err != nil {
			return nil, err
		}
		cmd.Stderr = stderr
		cmd.Stdin = input
		var data []byte
		if capture {
			data, err = cmd.Output()
		} else {
			cmd.Stdout = stderr
			err = cmd.Run()
		}
		if ctx.Err() != nil {
			return data, ctx.Err()
		}
		return data, err
	}
	run := func(ctx context.Context, action nativeAction, capture bool, args ...string) ([]byte, error) {
		return runInput(ctx, action, capture, nil, args...)
	}
	sandbox := entry.Sandbox
	args = append([]string{}, args...)
	home := os.Getenv("PASEO_HOME")
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return nil, nil, nil, err
		}
		home = filepath.Join(h, ".paseo")
	}
	root, err := config.Canonical(filepath.Join(home, "plugin-data", "antigravity-cli"))
	if err != nil {
		return nil, nil, nil, err
	}
	var sourceRoot *os.Root
	defer func() {
		if sourceRoot != nil {
			sourceRoot.Close()
		}
	}()
	paths := map[string]string{}
	guest := ""
	cleanup := func() {
		if guest != "" {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if _, err := run(cleanupCtx, nativeExec, false, "rm", "-rf", "--", guest); err != nil {
				fmt.Fprintln(stderr, "paseo-sbx: native assets cleanup incomplete:", err)
			}
		}
	}
	fail := func(err error) ([]string, map[string]string, func(), error) { cleanup(); return nil, nil, nil, err }
	for i, arg := range args {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		if arg != "--add-dir" && arg != "--json-schema" {
			continue
		}
		if i+1 >= len(args) {
			return fail(fmt.Errorf("missing value for %s", arg))
		}
		path, e := config.Canonical(args[i+1])
		if e != nil {
			return fail(e)
		}
		if config.Within(path, workspace) {
			continue
		}
		if path == root || !config.Within(path, root) {
			return fail(fmt.Errorf("%s path must be in the workspace or native plugin assets: %s", arg, path))
		}
		var relativeSource string
		if entry.EffectiveCategory() == config.CategoryContainer {
			if sourceRoot == nil {
				sourceRoot, e = os.OpenRoot(root)
				if e != nil {
					return fail(e)
				}
			}
			raw, e := filepath.Abs(args[i+1])
			if e != nil {
				return fail(e)
			}
			relativeSource, e = filepath.Rel(root, raw)
			if e != nil {
				return fail(e)
			}
			if e = writeNativeArchive(ctx, sourceRoot, relativeSource, "asset-0", io.Discard); e != nil {
				return fail(e)
			}
		} else {
			var size int64
			e = filepath.WalkDir(path, func(p string, d os.DirEntry, e error) error {
				if err := ctx.Err(); err != nil {
					return err
				}
				if e != nil {
					return e
				}
				if d.Type()&os.ModeSymlink != 0 {
					return errors.New("plugin assets must not contain symbolic links")
				}
				info, e := d.Info()
				if e != nil {
					return e
				}
				if !info.IsDir() && !info.Mode().IsRegular() {
					return errors.New("plugin assets must contain regular files")
				}
				size += info.Size()
				if size > 128*1024*1024 {
					return errors.New("plugin assets exceed size limit")
				}
				return nil
			})
			if e != nil {
				return fail(e)
			}
		}
		if guest == "" {
			b, e := run(ctx, nativeExec, true, "mktemp", "-d", "/tmp/paseo-sbx-assets.XXXXXXXXXX")
			if e != nil {
				return fail(fmt.Errorf("cannot create guest assets directory: %w", e))
			}
			guest = strings.TrimSpace(string(b))
			if !validNativeGuestDirectory(guest) {
				guest = ""
				return fail(errors.New("invalid guest assets directory"))
			}
		}
		next, ok := paths[args[i+1]]
		if !ok {
			next = fmt.Sprintf("%s/asset-%d", guest, len(paths))
			resource := sandbox
			copyArgs := []string{"cp", path, resource + ":" + next}
			if entry.EffectiveCategory() == config.CategoryContainer {
				reader, writer := io.Pipe()
				done := make(chan error, 1)
				go func() {
					err := writeNativeArchive(ctx, sourceRoot, relativeSource, filepath.Base(next), writer)
					writer.CloseWithError(err)
					done <- err
				}()
				_, execErr := runInput(ctx, nativeExtract, false, reader, guest)
				reader.CloseWithError(execErr)
				if e = errors.Join(execErr, <-done); e != nil {
					return fail(fmt.Errorf("cannot extract native plugin assets: %w", e))
				}
			} else if _, e = run(ctx, nativeCopy, false, copyArgs...); e != nil {
				return fail(fmt.Errorf("cannot copy native plugin assets: %w", e))
			}
			paths[args[i+1]] = next
		}
		args[i+1] = next
	}
	return args, paths, cleanup, nil
}

func rewriteNativePaths(value any, paths map[string]string) any {
	switch v := value.(type) {
	case string:
		for from, to := range paths {
			v = strings.ReplaceAll(v, from, to)
		}
		return v
	case []any:
		for i, x := range v {
			v[i] = rewriteNativePaths(x, paths)
		}
		return v
	case map[string]any:
		for k, x := range v {
			v[k] = rewriteNativePaths(x, paths)
		}
		return v
	default:
		return value
	}
}

func RewriteTree(value any, bind gateway.BindFunc) (any, error) {
	switch v := value.(type) {
	case []any:
		r := make([]any, len(v))
		for i, x := range v {
			next, err := RewriteTree(x, bind)
			if err != nil {
				return nil, err
			}
			r[i] = next
		}
		return r, nil
	case map[string]any:
		r := map[string]any{}
		for k, x := range v {
			next, err := RewriteTree(x, bind)
			if err != nil {
				return nil, err
			}
			r[k] = next
		}
		if endpoint, ok := r["url"].(string); ok {
			u, err := url.Parse(endpoint)
			if err != nil {
				return nil, errors.New("invalid MCP URL")
			}
			if u.Path == "/mcp/agents" {
				key := "headers"
				if _, exists := r["http_headers"]; exists {
					key = "http_headers"
				}
				headers := map[string]string{}
				arrayHeaders := false
				if raw, exists := r[key]; exists {
					switch m := raw.(type) {
					case map[string]any:
						for k, x := range m {
							str, ok := x.(string)
							if !ok {
								return nil, errors.New("invalid MCP header value")
							}
							headers[k] = str
						}
					case []any:
						arrayHeaders = true
						for _, x := range m {
							header, ok := x.(map[string]any)
							if !ok {
								return nil, errors.New("invalid ACP MCP header")
							}
							name, nameOK := header["name"].(string)
							value, valueOK := header["value"].(string)
							if !nameOK || name == "" || !valueOK {
								return nil, errors.New("invalid ACP MCP header")
							}
							headers[name] = value
						}
					default:
						return nil, errors.New("invalid MCP headers")
					}
				}
				next, h, err := bind(endpoint, headers)
				if err != nil {
					return nil, err
				}
				r["url"] = next
				r[key] = h
				if arrayHeaders {
					list := []any{}
					for name, value := range h {
						list = append(list, map[string]string{"name": name, "value": value})
					}
					r[key] = list
				}
				if key == "http_headers" {
					delete(r, "headers")
				}
				delete(r, "bearer_token_env_var")
			}
		}
		return r, nil
	default:
		return value, nil
	}
}
func CheckWorkdirs(value any, workspace string) error {
	canonical, err := config.Canonical(workspace)
	if err != nil {
		return err
	}
	return checkWorkdirs(value, canonical)
}

func checkWorkdirs(value any, workspace string) error {
	switch v := value.(type) {
	case []any:
		for _, x := range v {
			if err := checkWorkdirs(x, workspace); err != nil {
				return err
			}
		}
	case map[string]any:
		for k, x := range v {
			if k == "cwd" && x != nil {
				path, ok := x.(string)
				if !ok {
					return errors.New("requested cwd must be a path")
				}
				if !filepath.IsAbs(path) {
					path = workspace + "/" + path
				}
				candidate, err := config.Canonical(path)
				if err != nil {
					return err
				}
				if !config.Within(candidate, workspace) {
					return errors.New("requested cwd is outside the registered sandbox workspace")
				}
			}
			if err := checkWorkdirs(x, workspace); err != nil {
				return err
			}
		}
	}
	return nil
}
func rewriteClaude(args []string, bind gateway.BindFunc) ([]string, error) {
	args = append([]string{}, args...)
	if len(args) > 0 && (strings.HasSuffix(args[0], "/cli.js") || strings.HasSuffix(args[0], "/cli.mjs")) {
		args = args[1:]
	}
	transform := func(raw string) (string, error) {
		var data []byte
		if strings.HasPrefix(strings.TrimSpace(raw), "{") {
			data = []byte(raw)
		} else {
			var err error
			data, err = os.ReadFile(raw)
			if err != nil {
				return "", errors.New("cannot read Claude MCP config")
			}
		}
		var v any
		if err := json.Unmarshal(data, &v); err != nil {
			return "", errors.New("invalid Claude MCP config")
		}
		r, err := RewriteTree(v, bind)
		if err != nil {
			return "", err
		}
		b, err := json.Marshal(r)
		return string(b), err
	}
	for i := 0; i < len(args); i++ {
		if args[i] == "--mcp-config" {
			if i+1 >= len(args) {
				return nil, errors.New("missing Claude MCP config")
			}
			next, err := transform(args[i+1])
			if err != nil {
				return nil, err
			}
			args[i+1] = next
			i++
		} else if strings.HasPrefix(args[i], "--mcp-config=") {
			next, err := transform(strings.TrimPrefix(args[i], "--mcp-config="))
			if err != nil {
				return nil, err
			}
			args[i] = "--mcp-config=" + next
		}
	}
	return args, nil
}

// sessionCwd prefers the project path Paseo gives each agent session; Codex
// app-server processes otherwise inherit the daemon cwd.
func sessionCwd() (string, bool, error) {
	if cwd := os.Getenv("PASEO_AGENT_CWD"); cwd != "" {
		return cwd, true, nil
	}
	cwd, err := os.Getwd()
	return cwd, false, err
}

// Resolve picks the runtime whose workspace most specifically contains the
// session cwd. Sessionless probes (model discovery, --version) fall back to
// the first workspace by path that has the profile active.
func Resolve(base, profile string, args []string) (string, error) {
	cwd, explicit, err := sessionCwd()
	if err != nil {
		return "", err
	}
	if cwd, err = config.Canonical(cwd); err != nil {
		return "", err
	}
	b, err := config.Canonical(base)
	if err != nil {
		return "", err
	}
	installed, err := config.Registries(b)
	if err != nil {
		return "", err
	}
	type candidate struct{ runtime, workspace string }
	var best, fallback *candidate
	for _, i := range installed {
		key, _, err := providerEntry(i.Registry, profile)
		if err != nil || !i.Registry.IsActive(key) || i.Missing() {
			continue
		}
		w, err := config.Canonical(i.Registry.Workspace)
		if err != nil || config.Within(b, w) || config.Within(w, b) {
			continue
		}
		c := &candidate{i.Runtime, w}
		if config.Within(cwd, w) && (best == nil || len(w) > len(best.workspace)) {
			best = c
		}
		if fallback == nil || w < fallback.workspace {
			fallback = c
		}
	}
	if best != nil {
		return best.runtime, nil
	}
	probe := len(args) > 0 && (args[0] == "app-server" || (len(args) == 1 && (args[0] == "--version" || args[0] == "-v")))
	if !explicit && probe && fallback != nil {
		return fallback.runtime, nil
	}
	return "", fmt.Errorf("Paseo cwd is outside every registered sandbox workspace for %s", profile)
}
func Launch(base, profile string, args []string) (int, error) {
	runtime, err := Resolve(base, profile, args)
	if err != nil {
		return 1, err
	}
	return LaunchIO(runtime, profile, args, os.Stdin, os.Stdout, os.Stderr)
}
func LaunchIO(runtime, profile string, args []string, in io.Reader, out, stderr io.Writer) (int, error) {
	var registry config.Registry
	if err := config.ReadJSON(filepath.Join(runtime, "registry.json"), &registry); err != nil {
		return 1, errors.New("cannot read trusted sandbox registry")
	}
	key, entry, err := providerEntry(registry, profile)
	if err != nil {
		return 1, err
	}
	if !registry.IsActive(key) || !config.SupportedAdapter(entry.Agent) || sandboxupdate.Identity(entry) == "" {
		return 1, errors.New("invalid sandbox profile")
	}
	workspace, err := config.Canonical(entry.Workspace)
	if err != nil {
		return 1, err
	}
	runtime, err = config.Canonical(runtime)
	if err != nil {
		return 1, err
	}
	if config.Within(runtime, workspace) {
		return 1, errors.New("host launcher must remain outside writable sandbox workspaces")
	}
	if len(args) == 1 && (args[0] == "--version" || args[0] == "-v") {
		if entry.EffectiveCategory() == config.CategoryContainer {
			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
			defer cancel()
			release, err := operationlock.Acquire(ctx, runtime, []string{operationlock.EntryKey(entry)})
			if err != nil {
				return 1, err
			}
			defer release()
			if err := validateEntry(runtime, key, entry); err != nil {
				return 1, err
			}
			err = (&operations.DockerBackend{}).Exec(ctx, entry, []string{config.AgentCommand(entry.Agent), "--version"}, in, out, stderr)
			return exitStatus(err), err
		}
		command := []string{"exec"}
		if entry.EffectiveCategory() == config.CategorySandbox {
			command = append(command, "--env", "PATH="+config.SandboxAgentPath)
		}
		command = append(command, entry.Sandbox, config.AgentCommand(entry.Agent), "--version")
		cmd := exec.Command("sbx", command...)
		cmd.Stdout = out
		cmd.Stderr = stderr
		return exitStatus(cmd.Run()), nil
	}
	cwd, explicit, err := sessionCwd()
	if err != nil {
		return 1, err
	}
	cwd, err = config.Canonical(cwd)
	if err != nil {
		return 1, err
	}
	if !config.Within(cwd, workspace) {
		if !explicit && len(args) > 0 && entry.Agent == "codex" && args[0] == "app-server" {
			cwd = workspace
		} else {
			return 1, errors.New("Paseo cwd is outside the registered sandbox workspace")
		}
	}
	profiles := []string{}
	ctx, stopUpdateSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer stopUpdateSignals()
	for _, p := range registry.ActiveProfiles() {
		if e, ok := registry.Profiles[p]; ok {
			profiles = append(profiles, operations.ProviderID(p, e))
		}
	}
	runner := func(args []string, check bool) error {
		cmd := exec.Command("sbx", args...)
		cmd.Stdout = stderr
		cmd.Stderr = stderr
		err := cmd.Run()
		if !check {
			return nil
		}
		return err
	}
	var relay *gateway.Relay
	if entry.EffectiveCategory() == config.CategoryContainer {
		relay, err = gateway.NewDockerRelay(profiles, func(endpoint string) error {
			ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			var out bytes.Buffer
			if err := (&operations.DockerBackend{}).Exec(ctx, entry, gateway.DockerProbeCommand(endpoint), nil, &out, stderr); err != nil {
				return err
			}
			if out.String() != gateway.RelayReady {
				return errors.New("unexpected Docker relay challenge response")
			}
			return nil
		})
	} else {
		relay, err = gateway.NewRelay(profiles, entry.Sandbox, runner)
	}
	if err != nil {
		return 1, errors.New("cannot start local MCP relay")
	}
	defer relay.Close()
	if entry.EffectiveCategory() == config.CategoryContainer {
		if err := func() error {
			release, err := operationlock.Acquire(ctx, runtime, []string{operationlock.EntryKey(entry)})
			if err != nil {
				return err
			}
			defer release()
			if err := validateEntry(runtime, key, entry); err != nil {
				return err
			}
			return relay.CheckReachability()
		}(); err != nil {
			return 1, err
		}
	}
	if entry.Agent == "claude" {
		args, err = rewriteClaude(args, relay.Bind)
		if err != nil {
			return 1, err
		}
	}
	nativePaths := map[string]string{}
	if entry.Agent == "antigravity-cli" {
		var cleanup func()
		args, nativePaths, cleanup, err = prepareNativeContext(ctx, runtime, key, args, workspace, entry, stderr)
		if err != nil {
			return 1, err
		}
		defer cleanup()
	}
	// Automatic updates use detached host process groups, so bridge the same
	// launcher signals to cancellation before any update subprocess starts.
	updateErr := updateTarget(ctx, runtime, key, entry, stderr)
	cancelErr := ctx.Err()
	if cancelErr != nil {
		return 1, cancelErr
	}
	if updateErr != nil {
		fmt.Fprintln(stderr, "paseo-sbx: update failed; launching installed agent:", updateErr)
	}
	command := []string{"exec", "-i"}
	releaseStartup, err := operationlock.Acquire(ctx, runtime, []string{operationlock.EntryKey(entry)})
	if err != nil {
		return 1, err
	}
	defer releaseStartup()
	executable, resource := "sbx", entry.Sandbox
	if entry.EffectiveCategory() == config.CategorySandbox {
		command = append(command, "--env", "PATH="+config.SandboxAgentPath)
	}
	if entry.EffectiveCategory() == config.CategoryContainer {
		executable, resource = "docker", entry.ContainerID
		command = append(command, "--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()), "--env", "HOME=/home/agent")
		state, err := (&operations.DockerBackend{}).Inspect(ctx, entry)
		if err != nil {
			return 1, err
		}
		if !state.Running {
			return 1, errors.New("Docker container is stopped; start it explicitly before launch")
		}
	}
	if err := validateEntry(runtime, key, entry); err != nil {
		return 1, err
	}
	for _, name := range []string{"PASEO_AGENT_ID", "PASEO_PARENT_AGENT_ID", "NO_BROWSER"} {
		if v, ok := os.LookupEnv(name); ok {
			command = append(command, "--env", name+"="+v)
		}
	}
	command = append(command, "--workdir", cwd, resource, config.AgentCommand(entry.Agent))
	command = append(command, args...)
	child := exec.Command(executable, command...)
	child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	child.Stdout = out
	child.Stderr = stderr
	stdin, err := child.StdinPipe()
	if err != nil {
		return 1, errors.New("cannot open sandbox stdin")
	}
	signals := make(chan os.Signal, 8)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(signals)
	if err = child.Start(); err != nil {
		stdin.Close()
		return 1, errors.New("cannot start sbx agent process")
	}
	releaseStartup()
	stopUpdateSignals()
	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			select {
			case sig := <-signals:
				if s, ok := sig.(syscall.Signal); ok {
					syscall.Kill(-child.Process.Pid, s)
				}
			case <-done:
				return
			}
		}
	}()
	failures := make(chan error, 1)
	go func() {
		defer stdin.Close()
		reader := bufio.NewReader(in)
		for {
			line, e := reader.ReadBytes(10)
			if len(line) > 0 {
				if (entry.Agent == "codex" || entry.Agent == "antigravity-cli") && len(strings.TrimSpace(string(line))) > 0 {
					var message map[string]any
					if json.Unmarshal(line, &message) != nil || message == nil {
						failures <- errors.New("failed to translate Paseo protocol: invalid JSON message")
						syscall.Kill(-child.Process.Pid, syscall.SIGTERM)
						return
					}
					original, _ := json.Marshal(message)
					if entry.Agent == "antigravity-cli" && len(nativePaths) > 0 {
						message = rewriteNativePaths(message, nativePaths).(map[string]any)
					}
					if err := CheckWorkdirs(message["params"], workspace); err != nil {
						failures <- err
						syscall.Kill(-child.Process.Pid, syscall.SIGTERM)
						return
					}
					next, err := RewriteTree(message, relay.Bind)
					if err != nil {
						failures <- err
						syscall.Kill(-child.Process.Pid, syscall.SIGTERM)
						return
					}
					encoded, err := json.Marshal(next)
					if err != nil {
						failures <- errors.New("failed to encode Paseo protocol")
						syscall.Kill(-child.Process.Pid, syscall.SIGTERM)
						return
					}
					if entry.Agent != "antigravity-cli" || !bytes.Equal(original, encoded) {
						line = append(encoded, 10)
					}
				}
				if _, err := stdin.Write(line); err != nil {
					return
				}
			}
			if e != nil {
				return
			}
			select {
			case <-done:
				return
			default:
			}
		}
	}()
	err = child.Wait()
	stdin.Close()
	select {
	case failure := <-failures:
		return 1, failure
	default:
	}
	return exitStatus(err), nil
}
func validateEntry(runtime, key string, entry config.Entry) error {
	var reg config.Registry
	if err := config.ReadJSON(filepath.Join(runtime, "registry.json"), &reg); err != nil {
		return err
	}
	if err := reg.ValidateIdentity(key, entry); err != nil {
		return err
	}
	if !reg.IsActive(key) {
		return errors.New("provider activation changed")
	}
	return nil
}

func providerEntry(reg config.Registry, id string) (string, config.Entry, error) {
	for key, entry := range reg.Profiles {
		if operations.ProviderID(key, entry) != id {
			continue
		}
		if entry.EffectiveCategory() != config.CategoryLegacySandbox && key != entry.Key() {
			return "", entry, errors.New("provider registry key mismatch")
		}
		if err := reg.ValidateIdentity(key, entry); err != nil {
			return "", entry, err
		}
		catalog := reg.EffectiveCatalog()
		if entry.EffectiveCategory() == config.CategorySandbox {
			agent, ok := catalog.Agent(entry.Agent)
			if !ok || agent.Adapter != entry.Agent || entry.Tool != "" || len(entry.Utilities) != 0 {
				return "", entry, errors.New("invalid sandbox provider")
			}
		} else {
			pair, ok := catalog.Pair(strings.TrimPrefix(key, "container/"))
			agent, known := catalog.Agent(pair.Agent)
			if !ok || !known || agent.Adapter != entry.Agent {
				return "", entry, errors.New("invalid provider profile")
			}
		}
		return key, entry, nil
	}
	return "", config.Entry{}, errors.New("provider missing from trusted registry")
}

func updateTarget(ctx context.Context, runtime, key string, entry config.Entry, stderr io.Writer) error {
	release, err := operationlock.Acquire(ctx, runtime, []string{operationlock.EntryKey(entry)})
	if err != nil {
		if errors.Is(err, operationlock.ErrConflict) {
			return nil
		}
		return err
	}
	defer release()
	var reg config.Registry
	if err := config.ReadJSON(filepath.Join(runtime, "registry.json"), &reg); err != nil {
		return err
	}
	if err := reg.ValidateIdentity(key, entry); err != nil {
		return err
	}
	if !reg.IsActive(key) {
		return errors.New("provider activation changed")
	}
	if entry.EffectiveCategory() != config.CategoryContainer {
		return sandboxupdate.UpdateLockedContext(ctx, runtime, entry, false, stderr, nil)
	}
	transport := func(ctx context.Context, args []string, capture bool) (string, error) {
		var out bytes.Buffer
		writer := stderr
		if capture {
			writer = &out
		}
		err := (&operations.DockerBackend{}).Exec(ctx, entry, append([]string{"env", "-C", "/tmp"}, args...), nil, writer, stderr)
		return strings.TrimSpace(out.String()), err
	}
	return sandboxupdate.UpdateTransportLockedContext(ctx, runtime, entry, false, stderr, nil, transport)
}

func exitStatus(err error) int {
	if err == nil {
		return 0
	}
	var e *exec.ExitError
	if errors.As(err, &e) {
		if e.ExitCode() < 0 {
			if status, ok := e.Sys().(syscall.WaitStatus); ok {
				return 128 + int(status.Signal())
			}
		}
		return e.ExitCode()
	}
	return 1
}
func Main() int {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "paseo-sbx launcher: profile is required")
		return 1
	}
	exe, err := os.Executable()
	if err != nil {
		return 1
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return 1
	}
	profile, args, err := LaunchArguments(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "paseo-sbx launcher:", err)
		return 1
	}
	// The shared launcher lives in <base>/launcher; legacy copies in <base>/<id>.
	code, err := Launch(filepath.Dir(filepath.Dir(exe)), profile, args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "paseo-sbx launcher:", err)
		return 1
	}
	return code
}

// LaunchArguments also accepts the metadata appended by a native plugin.
func LaunchArguments(args []string) (string, []string, error) {
	for i, arg := range args {
		if arg != "--paseo-sbx-profile" {
			continue
		}
		if i+1 >= len(args) || args[i+1] == "" {
			return "", nil, errors.New("profile is required")
		}
		profile := args[i+1]
		filtered := append(append([]string{}, args[:i]...), args[i+2:]...)
		for _, arg := range filtered {
			if arg == "--paseo-sbx-profile" {
				return "", nil, errors.New("duplicate profile")
			}
		}
		return profile, filtered, nil
	}
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return "", nil, errors.New("profile is required")
	}
	return args[0], args[1:], nil
}
