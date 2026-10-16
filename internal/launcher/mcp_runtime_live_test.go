package launcher

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"paseo-dock/internal/config"
	"paseo-dock/internal/operationlock"
	"paseo-dock/internal/operations"
)

// This opt-in probe creates its own isolated disposable acceptance container.
// It exercises Codex MCP discovery, not model inference or a real Paseo tool call.
func TestLiveCodexProviderMCPDiscovery(t *testing.T) {
	if os.Getenv("CATEGORY_MCP_RUNTIME") != "true" {
		t.Skip("requires local Docker access and the previously built acceptance image")
	}
	const approvedRoot = "/private/tmp/paseo-dock-category-live-1638524322"
	root, err := os.MkdirTemp("/private/tmp", "paseo-dock-mcp-live-")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("independent disposable root: %s", root)
	trusted := filepath.Join(root, "trusted")
	for _, name := range []string{"home", "workspace", "trusted/bin", "paseo"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"versions.lock.json", "Dockerfile", "bin/agent-wrapper"} {
		data, err := os.ReadFile(filepath.Join(approvedRoot, "trusted", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(trusted, name), data, 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("PASEO_HOME", filepath.Join(root, "paseo"))
	t.Setenv("PASEO_DOCK_AUTO_UPDATE", "0")
	service, err := operations.New(trusted)
	if err != nil {
		t.Fatal(err)
	}
	service.Out, service.Err = io.Discard, io.Discard
	selection, err := service.Catalog.Normalize(config.Selection{Schema: 2, Category: config.CategoryContainer, Pairs: []config.Pair{{Agent: "codex", Methodology: "superpowers"}}, Utilities: []string{"rtk"}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	_, err = service.Execute(ctx, operations.Request{Kind: operations.KindCreate, Target: operations.Target{Workspace: filepath.Join(root, "workspace")}, Selection: selection, Namespace: "paseo-dock-category-live-20261007", PaseoHome: filepath.Join(root, "paseo")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := config.RuntimePath(filepath.Join(root, "workspace"))
	if err != nil {
		t.Fatal(err)
	}
	var registry config.Registry
	if err := config.ReadJSON(filepath.Join(runtime, "registry.json"), &registry); err != nil {
		t.Fatal(err)
	}
	key := "container/codex-superpowers"
	entry, ok := registry.Profiles[key]
	if !ok || entry.ContainerID == "" || entry.ContainerID == "2ed04124ee17f7fd840f00695c0a84ba79438fa3b92a3fe16f8c2767bc418748" || entry.Workspace != filepath.Join(root, "workspace") || entry.ImageID != "sha256:7760a63ba6a62611c4434c1208c1c87236cd37d36b691da206ebd0d7e77d6521" || entry.Category != config.CategoryContainer || !registry.IsActive(key) {
		t.Fatal("acceptance registry identity or activation mismatch")
	}
	t.Logf("independent exact CID: %s", entry.ContainerID)
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer stopCancel()
		_, err := service.Execute(stopCtx, operations.Request{Kind: operations.KindStop, Target: operations.Target{Category: entry.Category, Workspace: entry.Workspace, Runtime: runtime, Profile: key, Entry: entry}}, nil)
		if err != nil {
			t.Error(err)
		}
	}()
	backend := &operations.DockerBackend{Root: trusted}
	release, err := operationlock.Acquire(ctx, runtime, []string{operationlock.EntryKey(entry)})
	if err != nil {
		t.Fatal(err)
	}
	if err := validateEntry(runtime, key, entry); err != nil {
		release()
		t.Fatal(err)
	}
	state, err := backend.Inspect(ctx, entry)
	if err != nil {
		release()
		t.Fatal(err)
	}
	release()
	if !state.Running {
		t.Fatal("independent acceptance container unexpectedly stopped; probe never restarts resources")
	}
	var version bytes.Buffer
	if err := backend.Exec(ctx, entry, []string{"codex", "--version"}, nil, &version, io.Discard); err != nil {
		t.Fatal(err)
	}
	t.Logf("actual independent guest version: %s", strings.TrimSpace(version.String()))
	t.Setenv("PASEO_AGENT_CWD", entry.Workspace)
	var initialized, catalog atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mcp/agents" || r.URL.Query().Get("callerAgentId") != "category-mcp-probe" || r.Header.Get("Authorization") != "Bearer SYNTHETIC_MCP_UPSTREAM" {
			t.Error("upstream route or synthetic credential mismatch")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		var result any
		switch request["method"] {
		case "initialize":
			initialized.Add(1)
			result = map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "category-mcp-probe", "version": "1"}}
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
			return
		case "tools/list":
			catalog.Add(1)
			result = map[string]any{"tools": []any{map[string]any{"name": "list_providers", "description": "Read available providers", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{}}}}}
		default:
			t.Errorf("unexpected upstream MCP method %v", request["method"])
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request["id"], "result": result})
	}))
	defer upstream.Close()
	in, input := io.Pipe()
	defer in.Close()
	defer input.Close()
	frames := make(chan map[string]json.RawMessage, 128)
	output := &mcpProbeFrames{frames: frames}
	done := make(chan error, 1)
	go func() {
		code, err := LaunchIO(runtime, operations.ProviderID(key, entry), []string{"app-server"}, in, output, io.Discard)
		if err == nil && code != 0 {
			err = fmt.Errorf("provider exit %d", code)
		}
		done <- err
	}()
	send := func(id int, method string, params any) {
		t.Helper()
		if err := json.NewEncoder(input).Encode(map[string]any{"id": id, "method": method, "params": params}); err != nil {
			t.Fatal(err)
		}
	}
	await := func(id int) map[string]json.RawMessage {
		t.Helper()
		for {
			select {
			case frame := <-frames:
				var got int
				if json.Unmarshal(frame["id"], &got) != nil || got != id {
					continue
				}
				if _, bad := frame["error"]; bad {
					t.Fatalf("RPC %d failed (payload withheld)", id)
				}
				if len(frame["result"]) == 0 || bytes.Equal(frame["result"], []byte("null")) {
					t.Fatalf("RPC %d has no result", id)
				}
				return frame
			case err := <-done:
				t.Fatalf("provider ended early: %v", err)
			case <-ctx.Done():
				t.Fatal("MCP discovery deadline exceeded")
			}
		}
	}
	send(1, "initialize", map[string]any{"clientInfo": map[string]any{"name": "category-mcp-probe", "version": "1"}})
	await(1)
	json.NewEncoder(input).Encode(map[string]any{"method": "initialized", "params": map[string]any{}})
	send(2, "thread/start", map[string]any{"cwd": entry.Workspace, "ephemeral": true, "config": map[string]any{"mcp_servers": map[string]any{"category_probe": map[string]any{"url": upstream.URL + "/mcp/agents?callerAgentId=category-mcp-probe", "http_headers": map[string]any{"Authorization": "Bearer SYNTHETIC_MCP_UPSTREAM"}}}}})
	threadFrame := await(2)
	var threadResult struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(threadFrame["result"], &threadResult); err != nil || threadResult.Thread.ID == "" {
		t.Fatal("thread/start missing thread identity")
	}
	send(3, "mcpServerStatus/list", map[string]any{"threadId": threadResult.Thread.ID, "serverName": "category_probe"})
	status := await(3)
	if initialized.Load() == 0 || catalog.Load() == 0 || !strings.Contains(string(status["result"]), "list_providers") {
		t.Fatal("guest did not discover the host relay tool catalog")
	}
	input.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("provider failed to terminate after EOF")
	}
	t.Logf("actual Docker provider MCP discovery passed: upstream initialize=%d tools/list=%d; no inference", initialized.Load(), catalog.Load())
}

type mcpProbeFrames struct {
	mu     sync.Mutex
	buffer bytes.Buffer
	frames chan map[string]json.RawMessage
}

func (w *mcpProbeFrames) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buffer.Write(p)
	for {
		end := bytes.IndexByte(w.buffer.Bytes(), '\n')
		if end < 0 {
			break
		}
		var frame map[string]json.RawMessage
		if json.Unmarshal(w.buffer.Next(end+1), &frame) == nil {
			w.frames <- frame
		}
	}
	return len(p), nil
}
