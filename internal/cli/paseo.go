package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"paseo-sbx/internal/config"
	"path/filepath"
	"strings"
	"time"

	"github.com/coder/websocket"
)

// createPluginAgent uses the local daemon's existing authenticated API. The
// local credential is consumed on the host and never passed to the sandbox.
func createPluginAgent(ctx context.Context, home string, agentConfig map[string]any, prompt string) (string, error) {
	var state struct {
		Listen string `json:"listen"`
	}
	if err := config.ReadJSON(filepath.Join(home, "paseo.pid"), &state); err != nil {
		return "", fmt.Errorf("read local daemon state: %w", err)
	}
	endpoint := strings.TrimPrefix(state.Listen, "tcp://")
	client := &http.Client{}
	wsURL := ""
	if strings.HasPrefix(endpoint, "/") || strings.HasPrefix(endpoint, "unix://") {
		socket := strings.TrimPrefix(endpoint, "unix://")
		client.Transport = &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socket)
		}}
		wsURL = "ws://localhost/ws"
	} else {
		host, _, err := net.SplitHostPort(endpoint)
		if err != nil {
			return "", errors.New("invalid local daemon endpoint")
		}
		ip := net.ParseIP(host)
		if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return "", errors.New("run requires a local daemon endpoint")
		}
		wsURL = (&url.URL{Scheme: "ws", Host: endpoint, Path: "/ws"}).String()
	}
	credential, err := os.ReadFile(filepath.Join(home, "local-credential"))
	if err != nil {
		return "", fmt.Errorf("read local daemon credential: %w", err)
	}
	token := strings.TrimSpace(string(credential))
	if len(token) != 43 {
		return "", errors.New("invalid local daemon credential")
	}
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPClient: client})
	if err != nil {
		return "", errors.New("cannot connect to local Paseo daemon")
	}
	defer conn.CloseNow()
	conn.SetReadLimit(16 * 1024 * 1024)
	send := func(v any) error {
		b, e := json.Marshal(v)
		if e != nil {
			return e
		}
		return conn.Write(ctx, websocket.MessageText, b)
	}
	var random [16]byte
	if _, err = rand.Read(random[:]); err != nil {
		return "", err
	}
	id := hex.EncodeToString(random[:])
	if err = send(map[string]any{"type": "hello", "clientId": "paseo-sbx-" + id, "clientType": "cli", "protocolVersion": 1, "auth": map[string]any{"kind": "localCredential", "token": token}}); err != nil {
		return "", errors.New("cannot authenticate with local daemon")
	}
	request := map[string]any{"type": "create_agent_request", "requestId": id, "idempotencyKey": "paseo-sbx-" + id, "config": agentConfig}
	if prompt != "" {
		request["initialPrompt"] = prompt
	}
	sent := false
	for {
		_, b, err := conn.Read(ctx)
		if err != nil {
			return "", errors.New("daemon disconnected or timed out before confirming agent creation; inspect Paseo before retrying")
		}
		var message struct {
			Type    string `json:"type"`
			Reason  string `json:"reason"`
			Message struct {
				Type    string `json:"type"`
				Payload struct {
					RequestID string `json:"requestId"`
					Status    string `json:"status"`
					AgentID   string `json:"agentId"`
					Error     string `json:"error"`
				} `json:"payload"`
			} `json:"message"`
		}
		if err = json.Unmarshal(b, &message); err != nil {
			return "", errors.New("invalid daemon response")
		}
		if message.Type == "hello.rejected" {
			return "", fmt.Errorf("daemon authentication rejected: %s", message.Reason)
		}
		p := message.Message.Payload
		if !sent && message.Message.Type == "status" && p.Status == "server_info" {
			if err = send(map[string]any{"type": "session", "message": request}); err != nil {
				return "", errors.New("cannot send agent creation request")
			}
			sent = true
			continue
		}
		if p.RequestID != id {
			continue
		}
		if message.Message.Type == "rpc_error" || p.Status == "agent_create_failed" {
			return "", fmt.Errorf("agent creation failed: %s", p.Error)
		}
		if p.Status == "agent_created" && p.AgentID != "" {
			return p.AgentID, nil
		}
	}
}

func (a *App) runPlugin(args []string) error {
	f := flag.NewFlagSet("run", flag.ContinueOnError)
	f.SetOutput(a.Err)
	profile := f.String("profile", "antigravity-cli-native", "sandbox profile")
	workspace := f.String("workspace", "", "registered workspace (required)")
	home := f.String("paseo-home", "~/.paseo", "local Paseo daemon home")
	model := f.String("model", "", "native plugin model ID")
	mode := f.String("mode", "", "native plugin mode ID")
	title := f.String("title", "", "agent title")
	dry := f.Bool("dry-run", false, "print agent configuration without contacting the daemon")
	if err := f.Parse(args); err != nil {
		return err
	}
	if *workspace == "" {
		return errors.New("--workspace is required")
	}
	pair, ok := a.Catalog.Pair(*profile)
	if !ok {
		return errors.New("unknown sandbox profile")
	}
	agent, _ := a.Catalog.Agent(pair.Agent)
	if agent.Adapter != "antigravity-cli" {
		return errors.New("run selects the installed Antigravity plugin; use Paseo directly for Codex and Claude")
	}
	w, err := config.Canonical(*workspace)
	if err != nil {
		return err
	}
	base, err := config.BaseDir()
	if err != nil {
		return err
	}
	base, err = config.Canonical(base)
	if err != nil {
		return err
	}
	runtime, err := config.RuntimePath(w)
	if err != nil {
		return err
	}
	var reg config.Registry
	if err = config.ReadJSON(filepath.Join(runtime, "registry.json"), &reg); err != nil {
		return err
	}
	entry, ok := reg.Profiles[*profile]
	if !ok || !reg.IsActive(*profile) || entry.Agent != agent.Adapter || entry.Sandbox == "" || entry.Workspace != w {
		return errors.New("profile has no active sandbox in this workspace")
	}
	if err = a.installLauncher(base, []string{w}); err != nil {
		return err
	}
	settings := map[string]any{"provider": "antigravity-cli", "cwd": w, "providerOptions": map[string]any{"agyPath": config.LauncherPath(base), "extraArgs": []string{"--paseo-sbx-profile", *profile}}, "featureValues": map[string]any{"shareMcp": "off"}}
	if *model != "" {
		settings["model"] = *model
	}
	if *mode != "" {
		settings["modeId"] = *mode
	}
	if *title != "" {
		settings["title"] = *title
	}
	if *dry {
		b, _ := json.MarshalIndent(settings, "", "  ")
		fmt.Fprintln(a.Out, string(b))
		return nil
	}
	h, err := config.Canonical(*home)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	id, err := createPluginAgent(ctx, h, settings, strings.Join(f.Args(), " "))
	if err != nil {
		return err
	}
	fmt.Fprintln(a.Out, "Created Antigravity sandbox agent:", id)
	fmt.Fprintf(a.Out, "View in Paseo, or run: paseo agent attach %s --home %q\n", id, h)
	return nil
}
