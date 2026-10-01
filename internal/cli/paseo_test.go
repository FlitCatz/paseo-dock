package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestPluginAgentCreation(t *testing.T) {
	for _, reject := range []bool{false, true} {
		t.Run(map[bool]string{false: "create", true: "reject"}[reject], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			token := strings.Repeat("A", 43)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := websocket.Accept(w, r, nil)
				if err != nil {
					t.Error(err)
					return
				}
				defer conn.CloseNow()
				_, b, err := conn.Read(ctx)
				if err != nil {
					t.Error(err)
					return
				}
				var hello struct {
					Type string
					Auth struct {
						Kind  string
						Token string
					}
				}
				if err = json.Unmarshal(b, &hello); err != nil || hello.Type != "hello" || hello.Auth.Kind != "localCredential" || hello.Auth.Token != token {
					t.Errorf("invalid hello: %v", err)
					return
				}
				if reject {
					_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"hello.rejected","reason":"incorrect_password"}`))
					return
				}
				_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"session","message":{"type":"status","payload":{"status":"server_info"}}}`))
				_, b, err = conn.Read(ctx)
				if err != nil {
					t.Error(err)
					return
				}
				var request struct {
					Type    string
					Message struct {
						Type          string
						RequestID     string
						InitialPrompt string
						Config        map[string]any
					}
				}
				if err = json.Unmarshal(b, &request); err != nil {
					t.Error(err)
					return
				}
				if request.Type != "session" || request.Message.Type != "create_agent_request" || request.Message.InitialPrompt != "hello" || request.Message.Config["provider"] != "antigravity-cli" || strings.Contains(string(b), token) {
					t.Error("invalid agent creation request")
				}
				response, _ := json.Marshal(map[string]any{"type": "session", "message": map[string]any{"type": "status", "payload": map[string]any{"status": "agent_created", "requestId": request.Message.RequestID, "agentId": "test-agent"}}})
				_ = conn.Write(ctx, websocket.MessageText, response)
			}))
			defer server.Close()
			home := t.TempDir()
			state, _ := json.Marshal(map[string]string{"listen": strings.TrimPrefix(server.URL, "http://")})
			os.WriteFile(filepath.Join(home, "paseo.pid"), state, 0600)
			os.WriteFile(filepath.Join(home, "local-credential"), []byte(token), 0600)
			id, err := createPluginAgent(ctx, home, map[string]any{"provider": "antigravity-cli", "providerOptions": map[string]any{"agyPath": "/trusted/agent-wrapper"}}, "hello")
			if reject {
				if err == nil || !strings.Contains(err.Error(), "authentication rejected") {
					t.Fatalf("%s %v", id, err)
				}
			} else if err != nil || id != "test-agent" {
				t.Fatalf("%s %v", id, err)
			}
		})
	}
}

func TestRetireOwnedACPProvider(t *testing.T) {
	base := "/trusted/paseo-sbx"
	old := "antigravity-cli-native"
	providers := map[string]any{old: map[string]any{"command": []string{base + "/launcher/agent-wrapper", old, "--acp"}}, "custom": map[string]any{"command": []string{"/external/agent-wrapper", "custom", "--acp"}}}
	current := map[string]any{"agents": map[string]any{"providers": providers}}
	MergeManagedConfig(current, ProviderConfig(base+"/launcher/agent-wrapper", map[string]string{old: "antigravity-cli"}), base)
	if _, ok := providers[old]; ok {
		t.Fatal("obsolete owned ACP provider retained")
	}
	if _, ok := providers["custom"]; !ok {
		t.Fatal("external provider removed")
	}
}
