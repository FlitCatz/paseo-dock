// Package launcher transports Paseo agent protocols into a trusted sandbox.
package launcher

import (
	"bufio"
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
	"path/filepath"
	"strings"
	"syscall"
)

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
				if raw, exists := r[key]; exists {
					m, ok := raw.(map[string]any)
					if !ok {
						return nil, errors.New("invalid MCP headers")
					}
					for k, x := range m {
						str, ok := x.(string)
						if !ok {
							return nil, errors.New("invalid MCP header value")
						}
						headers[k] = str
					}
				}
				next, h, err := bind(endpoint, headers)
				if err != nil {
					return nil, err
				}
				r["url"] = next
				r[key] = h
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
		if _, ok := i.Registry.Profiles[profile]; !ok || !i.Registry.IsActive(profile) || i.Missing() {
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
	entry, ok := registry.Profiles[profile]
	catalog := registry.EffectiveCatalog()
	pair, valid := catalog.Pair(profile)
	agent, known := catalog.Agent(pair.Agent)
	if !ok || !valid || !known || !registry.IsActive(profile) || agent.Adapter != entry.Agent || (entry.Agent != "codex" && entry.Agent != "claude") || entry.Sandbox == "" {
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
		cmd := exec.Command("sbx", "exec", entry.Sandbox, entry.Agent, "--version")
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
		if !explicit && entry.Agent == "codex" && len(args) > 0 && args[0] == "app-server" {
			cwd = workspace
		} else {
			return 1, errors.New("Paseo cwd is outside the registered sandbox workspace")
		}
	}
	profiles := []string{}
	for _, p := range registry.ActiveProfiles() {
		if _, ok := catalog.Pair(p); ok {
			profiles = append(profiles, p)
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
	relay, err := gateway.NewRelay(profiles, entry.Sandbox, runner)
	if err != nil {
		return 1, errors.New("cannot start local MCP relay")
	}
	defer relay.Close()
	if entry.Agent == "claude" {
		args, err = rewriteClaude(args, relay.Bind)
		if err != nil {
			return 1, err
		}
	}
	command := []string{"exec", "-i"}
	for _, name := range []string{"PASEO_AGENT_ID", "PASEO_PARENT_AGENT_ID"} {
		if v, ok := os.LookupEnv(name); ok {
			command = append(command, "--env", name+"="+v)
		}
	}
	command = append(command, "--workdir", cwd, entry.Sandbox, entry.Agent)
	command = append(command, args...)
	child := exec.Command("sbx", command...)
	child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	child.Stdout = out
	child.Stderr = stderr
	stdin, err := child.StdinPipe()
	if err != nil {
		return 1, errors.New("cannot open sandbox stdin")
	}
	if err = child.Start(); err != nil {
		stdin.Close()
		return 1, errors.New("cannot start sbx agent process")
	}
	signals := make(chan os.Signal, 8)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(signals)
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
				if entry.Agent == "codex" && len(strings.TrimSpace(string(line))) > 0 {
					var message map[string]any
					if json.Unmarshal(line, &message) != nil || message == nil {
						failures <- errors.New("failed to translate Paseo protocol: invalid JSON message")
						syscall.Kill(-child.Process.Pid, syscall.SIGTERM)
						return
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
					line, err = json.Marshal(next)
					if err != nil {
						failures <- errors.New("failed to encode Paseo protocol")
						syscall.Kill(-child.Process.Pid, syscall.SIGTERM)
						return
					}
					line = append(line, 10)
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
	// The shared launcher lives in <base>/launcher; legacy copies in <base>/<id>.
	code, err := Launch(filepath.Dir(filepath.Dir(exe)), os.Args[1], os.Args[2:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "paseo-sbx launcher:", err)
		return 1
	}
	return code
}
