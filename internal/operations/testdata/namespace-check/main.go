// namespace-check diagnoses only an existing, isolated acceptance container.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"paseo-sbx/internal/config"
	"paseo-sbx/internal/operationlock"
	"paseo-sbx/internal/operations"
)

func run() error {
	root := flag.String("root", "", "existing disposable acceptance root")
	login := flag.Bool("login", false, "sign in to the isolated guest using a device code")
	model := flag.Bool("model", false, "verify one authenticated model response without tools")
	flag.Parse()
	if *login && *model {
		return fmt.Errorf("select only one mode")
	}
	if filepath.Dir(*root) != "/private/tmp" || !strings.HasPrefix(filepath.Base(*root), "paseo-sbx-category-live-") {
		return fmt.Errorf("an existing acceptance root is required")
	}
	workspace, err := config.Canonical(filepath.Join(*root, "workspace"))
	if err != nil {
		return err
	}
	sumRuntime := filepath.Join(*root, "home", ".local", "share", "paseo-sbx")
	entries, err := os.ReadDir(sumRuntime)
	if err != nil {
		return err
	}
	var entry config.Entry
	var runtime string
	for _, dir := range entries {
		var reg config.Registry
		path := filepath.Join(sumRuntime, dir.Name())
		if config.ReadJSON(filepath.Join(path, "registry.json"), &reg) == nil && reg.Workspace == workspace {
			if runtime != "" {
				return fmt.Errorf("ambiguous acceptance registry")
			}
			entry, runtime = reg.Profiles["container/codex-superpowers"], path
		}
	}
	if runtime == "" || entry.ContainerID == "" || entry.Workspace != workspace {
		return fmt.Errorf("acceptance container identity missing")
	}
	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	timeout := time.Minute
	if *login {
		timeout = 10 * time.Minute
	}
	ctx, cancel := context.WithTimeout(signalCtx, timeout)
	defer cancel()
	release, err := operationlock.Acquire(ctx, runtime, []string{operationlock.EntryKey(entry)})
	if err != nil {
		return err
	}
	defer release()
	backend := &operations.DockerBackend{Root: filepath.Join(*root, "trusted")}
	state, err := backend.Inspect(ctx, entry)
	if err != nil {
		return err
	}
	if !state.Running {
		cmd := exec.CommandContext(ctx, "docker", "start", entry.ContainerID)
		if err := cmd.Run(); err != nil {
			return err
		}
		defer func() {
			cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if err := backend.Stop(cleanup, entry); err != nil {
				fmt.Fprintln(os.Stderr, "STOP FAILED:", err)
			}
		}()
	}
	fmt.Println("EXACT_CONTAINER", entry.ContainerID)
	if *login {
		if err := backend.Exec(ctx, entry, []string{"codex", "login", "--device-auth"}, os.Stdin, os.Stdout, os.Stderr); err != nil {
			return err
		}
		return backend.Exec(ctx, entry, []string{"codex", "login", "status"}, nil, os.Stdout, os.Stderr)
	}
	if *model {
		var output bytes.Buffer
		if err := backend.Exec(ctx, entry, []string{"codex", "exec", "--json", "--skip-git-repo-check", "--sandbox", "workspace-write", "Reply exactly CATEGORY_MODEL_OK. Do not use tools."}, nil, &output, os.Stderr); err != nil {
			return err
		}
		var textOK, usageOK bool
		for _, line := range bytes.Split(output.Bytes(), []byte("\n")) {
			var event struct {
				Type string `json:"type"`
				Item struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"item"`
				Usage struct {
					InputTokens  int `json:"input_tokens"`
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
			}
			if json.Unmarshal(line, &event) != nil {
				continue
			}
			if event.Item.Type != "" && event.Item.Type != "agent_message" && event.Item.Type != "reasoning" {
				return fmt.Errorf("unexpected item type %q in no-tool model probe", event.Item.Type)
			}
			if event.Type == "item.completed" && event.Item.Type == "agent_message" && strings.TrimSpace(event.Item.Text) == "CATEGORY_MODEL_OK" {
				textOK = true
			}
			if event.Type == "turn.completed" && event.Usage.InputTokens > 0 && event.Usage.OutputTokens > 0 {
				usageOK = true
				fmt.Printf("MODEL_USAGE input=%d output=%d\n", event.Usage.InputTokens, event.Usage.OutputTokens)
			}
		}
		if !textOK || !usageOK {
			return fmt.Errorf("actual model response/usage missing (payload withheld)")
		}
		fmt.Println("ACTUAL_MODEL_RESPONSE_PASSED_NO_TOOL_EXECUTION")
		return nil
	}
	for _, args := range [][]string{
		{"sh", "-c", "id; grep -E '^(CapEff|NoNewPrivs|Seccomp):' /proc/self/status; cat /proc/self/attr/current 2>/dev/null || true; cat /proc/sys/kernel/apparmor_restrict_unprivileged_userns 2>/dev/null || true; ls -l /usr/bin/bwrap; command -v strace || true"},
		{"unshare", "--user", "--map-root-user", "true"},
		{"unshare", "--user", "--map-root-user", "--mount", "true"},
		{"bwrap", "--unshare-user", "--ro-bind", "/", "/", "--", "true"},
		{"sh", "-c", "if test -s /home/agent/.codex/auth.json; then echo GUEST_AUTH_PRESENT; else echo GUEST_AUTH_ABSENT; fi; if test -s /home/agent/.codex/config.toml; then echo GUEST_CONFIG_PRESENT; else echo GUEST_CONFIG_ABSENT; fi"},
	} {
		fmt.Printf("PROBE %q\n", args)
		err := backend.Exec(ctx, entry, args, nil, os.Stdout, os.Stderr)
		fmt.Printf("RESULT %v\n", err)
	}
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
