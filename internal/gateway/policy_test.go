package gateway

import (
	"encoding/json"
	"strings"
	"testing"
)

func request(tool string, args map[string]any) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": tool, "arguments": args}}
}
func TestDelegationBoundary(t *testing.T) {
	p := NewPolicy([]string{"codex-superpowers", "claude-ponytail"}, "parent")
	for _, tc := range []struct {
		name string
		args map[string]any
		ok   bool
	}{
		{"create_agent", map[string]any{"provider": "claude-ponytail/model", "initialPrompt": "review"}, true},
		{"create_agent", map[string]any{"provider": "claude/model"}, false},
		{"create_terminal", map[string]any{}, false},
		{"start_workspace_script", map[string]any{}, false},
		{"send_agent_prompt", map[string]any{"agentId": "unrelated"}, false},
		{"send_agent_prompt", map[string]any{"agentId": "parent", "prompt": "continue", "background": true}, true},
		{"create_agent", map[string]any{"provider": "codex-superpowers/model", "settings": map[string]any{"mcpServers": map[string]any{}}}, false},
	} {
		if err := p.Check(request(tc.name, tc.args)); (err == nil) != tc.ok {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
	for _, key := range []string{"cwd", "workspace", "workspaceId", "mcpServers", "config", "relationship"} {
		if p.Check(request("create_agent", map[string]any{"provider": "codex-superpowers/model", key: "/tmp"})) == nil {
			t.Errorf("accepted %s", key)
		}
	}
	for _, bad := range []any{nil, []any{}, map[string]any{"method": "tools/call", "params": false}, request("list_models", map[string]any{"provider": 42}), request("get_agent_status", map[string]any{"agentId": []any{}})} {
		if p.Check(bad) == nil {
			t.Errorf("accepted malformed request %v", bad)
		}
	}
	p.Filter(map[string]any{"result": map[string]any{"structuredContent": map[string]any{"agentId": "child"}}}, "create_agent")
	if err := p.Check(request("send_agent_prompt", map[string]any{"agentId": "child"})); err != nil {
		t.Fatal(err)
	}
}
func TestFilteredResults(t *testing.T) {
	p := NewPolicy([]string{"codex-superpowers"}, "parent")
	for _, tc := range []struct {
		operation string
		result    map[string]any
		forbidden string
	}{
		{"tools/list", map[string]any{"tools": []any{map[string]any{"name": "create_agent", "inputSchema": map[string]any{"properties": map[string]any{"provider": map[string]any{}, "cwd": map[string]any{}}}}, map[string]any{"name": "create_terminal"}}}, "create_terminal"},
		{"list_providers", map[string]any{"structuredContent": map[string]any{"providers": []any{map[string]any{"id": "codex-superpowers"}, map[string]any{"id": "claude"}}}, "content": []any{map[string]any{"text": "HOST_SECRET"}}}, "HOST_SECRET"},
		{"get_agent_status", map[string]any{"structuredContent": map[string]any{"snapshot": map[string]any{"id": "parent", "status": "idle", "config": map[string]any{"headers": "HOST_SECRET"}}}}, "HOST_SECRET"},
		{"list_agents", map[string]any{"structuredContent": map[string]any{"agents": []any{map[string]any{"id": "parent"}, map[string]any{"id": "unrelated"}}}}, "unrelated"},
	} {
		input := map[string]any{"result": tc.result}
		before, _ := json.Marshal(input)
		out, err := p.Filter(input, tc.operation)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(out)
		if strings.Contains(string(b), tc.forbidden) {
			t.Errorf("leak: %s", b)
		}
		after, _ := json.Marshal(input)
		if string(before) != string(after) {
			t.Fatal("mutated input")
		}
	}
}
func TestSSEFiltering(t *testing.T) {
	p := NewPolicy([]string{"codex-superpowers"}, "parent")
	body := []byte("event: message\r\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"tools\":[{\"name\":\"create_terminal\"}]}}\r\n\r\n")
	b, err := RewriteResponse(body, "text/event-stream", p, "tools/list")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "create_terminal") || !strings.Contains(string(b), "event: message") {
		t.Fatalf("bad event %s", b)
	}
}
func TestSSEDropsServerMessages(t *testing.T) {
	p := NewPolicy([]string{"codex-superpowers"}, "parent")
	body := []byte("event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/message\",\"params\":{\"data\":\"other agent secret\"}}\n\n" +
		"event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":7,\"method\":\"elicitation/create\",\"params\":{}}\n\n" +
		"event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"structuredContent\":{\"agentId\":\"parent\"}}}\n\n")
	b, err := RewriteResponse(body, "text/event-stream", p, "get_agent_status")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "other agent secret") || strings.Contains(string(b), "elicitation") || strings.Count(string(b), "event: message") != 1 || !strings.Contains(string(b), `"agentId":"parent"`) {
		t.Fatalf("bad stream %s", b)
	}
	if _, err = RewriteResponse([]byte(`{"jsonrpc":"2.0","method":"notifications/message","params":{}}`), "application/json", p, "get_agent_status"); err == nil {
		t.Fatal("non-stream server message accepted")
	}
}
func TestToolErrorResultsAreGeneric(t *testing.T) {
	p := NewPolicy([]string{"codex-superpowers"}, "parent")
	for _, operation := range []string{"send_agent_prompt", "create_agent", "list_agents"} {
		out, err := p.Filter(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{"isError": true, "content": []any{map[string]any{"type": "text", "text": "host detail"}}}}, operation)
		if err != nil {
			t.Fatalf("%s: %v", operation, err)
		}
		b, _ := json.Marshal(out)
		if strings.Contains(string(b), "host detail") || !strings.Contains(string(b), `"isError":true`) || !strings.Contains(string(b), "Paseo MCP tool call failed") {
			t.Fatalf("%s: %s", operation, b)
		}
	}
	if _, err := p.Filter(map[string]any{"result": map[string]any{"isError": true}}, "tools/list"); err != nil {
		t.Fatal(err)
	}
}
func TestTextOnlyToolResultsFailClosed(t *testing.T) {
	p := NewPolicy([]string{"codex-superpowers"}, "parent")
	for _, operation := range []string{"list_agents", "list_providers", "get_agent_status", "create_agent"} {
		_, err := p.Filter(map[string]any{"result": map[string]any{"content": []any{map[string]any{"type": "text", "text": "unrelated agent and host config"}}}}, operation)
		if err == nil {
			t.Errorf("unfiltered %s result accepted", operation)
		}
	}
}
func TestRedactsShortAndEscapedCredentials(t *testing.T) {
	for _, credential := range []string{"Bearer abc", "Bearer host\"token\\key"} {
		body, _ := json.Marshal(map[string]any{"result": map[string]any{"structuredContent": map[string]any{"detail": credential + " " + strings.TrimPrefix(credential, "Bearer ")}}})
		b, err := redactResponse(body, "application/json", map[string]string{"Authorization": credential})
		if err != nil {
			t.Fatal(err)
		}
		var decoded any
		if err = json.Unmarshal(b, &decoded); err != nil {
			t.Fatalf("redaction corrupted JSON: %s", b)
		}
		text, _ := json.Marshal(decoded)
		token := strings.TrimPrefix(credential, "Bearer ")
		escaped, _ := json.Marshal(token)
		if strings.Contains(string(text), string(escaped[1:len(escaped)-1])) {
			t.Fatalf("credential escaped redaction: %s", text)
		}
	}
}
func TestMalformedSuccessfulResultsFailClosed(t *testing.T) {
	p := NewPolicy([]string{"codex-superpowers"}, "parent")
	for _, operation := range []string{"list_agents", "list_providers", "get_agent_status", "tools/list"} {
		for _, result := range []any{nil, "unrelated agent/config", []any{map[string]any{"config": "host"}}} {
			if _, err := p.Filter(map[string]any{"result": result}, operation); err == nil {
				t.Errorf("accepted %s malformed result %v", operation, result)
			}
		}
	}
}
func TestUpstreamRPCErrorIsSafe(t *testing.T) {
	p := NewPolicy([]string{"codex-superpowers"}, "parent")
	out, err := p.Filter(map[string]any{"jsonrpc": "2.0", "id": 1, "error": map[string]any{"code": -32601, "message": "HOST_SECRET", "data": map[string]any{"config": "host configuration"}}}, "list_agents")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	if strings.Contains(string(b), "HOST_SECRET") || strings.Contains(string(b), "configuration") {
		t.Fatal(string(b))
	}
	for _, bad := range []map[string]any{{"code": map[string]any{"config": "host"}, "message": "bad"}, {"code": -32601}} {
		if _, err := p.Filter(map[string]any{"error": bad}, "list_agents"); err == nil {
			t.Fatal("accepted malformed RPC error")
		}
	}
}
